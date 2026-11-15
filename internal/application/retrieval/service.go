// Package retrieval implements Zero-Trust bitemporal memory reads, Point-in-Time
// reconstruction, untrusted XML enveloping, and opaque task request status checks.
package retrieval

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/romero-project/romero/internal/domain/governor"
	"github.com/romero-project/romero/internal/domain/identity"
	"github.com/romero-project/romero/internal/domain/memory"
	"github.com/romero-project/romero/internal/domain/security"
)

var (
	// ErrUnauthorized indicates missing or invalid caller identity.
	ErrUnauthorized = errors.New("unauthorized")
	// ErrNotFound mirrors memory.ErrNotFound when a memory or request is missing or outside scope.
	ErrNotFound = memory.ErrNotFound
	// ErrValidation indicates malformed query or temporal parameters.
	ErrValidation = errors.New("validation error")
	// ErrOperational indicates a required security or persistence dependency failed.
	ErrOperational = errors.New("operational dependency unavailable")
)

// TaskAccessSnapshot represents the single-query READ COMMITTED task permission state.
type TaskAccessSnapshot struct {
	CanRead  bool
	CanWrite bool
}

// TaskAccessChecker evaluates effective task_shared permissions without caching.
type TaskAccessChecker interface {
	EvaluateTaskAccess(ctx context.Context, q identity.TaskPermissionQuery) (TaskAccessSnapshot, error)
}

// OpaqueRequestMetadata contains status and scope fields for authorized status checks.
type OpaqueRequestMetadata struct {
	RequestID uuid.UUID
	TenantID  uuid.UUID
	ProjectID uuid.UUID
	TaskID    string
	Status    string
}

// OpaqueStatusReader reads minimal status metadata for an opaque async request.
type OpaqueStatusReader interface {
	GetStatus(ctx context.Context, tenantID, requestID uuid.UUID) (*OpaqueRequestMetadata, error)
}

// GetMemoryQuery requests either the current memory state or a Point-in-Time snapshot.
type GetMemoryQuery struct {
	MemoryID      uuid.UUID
	AsOf          *time.Time
	ValidAt       time.Time
	KnownAt       time.Time
	CorrelationID string
}

// MemoryDetail holds the authorized memory projection, history, relations, and XML envelope.
type MemoryDetail struct {
	Memory               memory.Memory
	Versions             []memory.MemoryVersion
	Relations            []memory.MemoryRelation
	PointInTimeVersion   *memory.MemoryVersion
	UntrustedXMLEnvelope string
}

// RequestStatus holds the minimal status projection returned by GetRequestStatus (AC-T11).
type RequestStatus struct {
	RequestID uuid.UUID
	Status    string
}

// Service orchestrates Zero-Trust memory retrieval and opaque request status checks.
type Service struct {
	uow          memory.UnitOfWork
	audit        memory.AuditLogger
	access       TaskAccessChecker
	opaqueStatus OpaqueStatusReader
	search       memory.SearchRepository
	embedder     memory.EmbeddingProvider
	reranker     memory.RerankerProvider
	profile      governor.Profile
	graphCache   GraphProjector
	clock        func() time.Time
}

// NewService constructs a RetrievalService with its required transactional and security ports.
func NewService(
	uow memory.UnitOfWork,
	audit memory.AuditLogger,
	access TaskAccessChecker,
	opaqueStatus OpaqueStatusReader,
	clock func() time.Time,
) (*Service, error) {
	if uow == nil || audit == nil {
		return nil, errors.New("uow and audit logger are required")
	}
	if clock == nil {
		clock = func() time.Time { return time.Now().UTC() }
	}
	return &Service{
		uow:          uow,
		audit:        audit,
		access:       access,
		opaqueStatus: opaqueStatus,
		clock:        clock,
	}, nil
}

// WithReranker attaches an optional Cross-Encoder RerankerProvider for the ultra profile.
func (s *Service) WithReranker(reranker memory.RerankerProvider, profile governor.Profile) *Service {
	s.reranker = reranker
	s.profile = profile
	return s
}

// GetMemory retrieves a memory in current or Point-in-Time mode, enforcing Zero-Trust isolation.
func (s *Service) GetMemory(ctx context.Context, auth identity.AuthContext, query GetMemoryQuery) (*MemoryDetail, error) {
	if auth.TenantID == uuid.Nil || auth.PrincipalID == uuid.Nil {
		return nil, ErrUnauthorized
	}
	if query.MemoryID == uuid.Nil {
		return nil, fmt.Errorf("%w: memory_id is required", ErrValidation)
	}

	isPointInTime := query.AsOf != nil || !query.ValidAt.IsZero() || !query.KnownAt.IsZero()
	var validAt, knownAt time.Time
	if isPointInTime {
		var err error
		validAt, knownAt, err = (memory.PointInTimeQuery{
			AsOf:    query.AsOf,
			ValidAt: query.ValidAt,
			KnownAt: query.KnownAt,
		}).Resolve()
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrValidation, err)
		}
	}

	var detail *MemoryDetail
	var denied bool
	var deniedScope string
	err := s.uow.ExecTx(ctx, func(reg memory.RepositoryRegistry) error {
		mem, loadErr := reg.Memories().GetMemoryByID(ctx, auth.TenantID, query.MemoryID)
		if loadErr != nil {
			if errors.Is(loadErr, memory.ErrNotFound) {
				denied = true
				return ErrNotFound
			}
			return fmt.Errorf("%w: %v", ErrOperational, loadErr)
		}

		if accessErr := s.authorizeRead(ctx, auth, mem); accessErr != nil {
			if errors.Is(accessErr, ErrNotFound) {
				denied = true
				deniedScope = string(mem.ScopeType)
			}
			return accessErr
		}

		now := s.clock()
		if isPointInTime {
			pit, pitErr := reg.Memories().GetMemoryVersionPointInTime(ctx, mem.ID, validAt, knownAt)
			if pitErr != nil {
				if errors.Is(pitErr, memory.ErrNotFound) {
					return ErrNotFound
				}
				return fmt.Errorf("%w: %v", ErrOperational, pitErr)
			}
			allVersions, verErr := reg.Memories().ListVersionsByMemoryID(ctx, mem.ID)
			if verErr != nil {
				return fmt.Errorf("%w: %v", ErrOperational, verErr)
			}
			versions := make([]memory.MemoryVersion, 0, len(allVersions))
			for _, v := range allVersions {
				if !v.CreatedAt.After(knownAt) {
					versions = append(versions, v)
				}
			}
			rawRelations, relErr := reg.Memories().ListActiveRelationsByMemoryID(ctx, auth.TenantID, mem.ID)
			if relErr != nil {
				return fmt.Errorf("%w: %v", ErrOperational, relErr)
			}
			relations, visErr := s.filterVisibleRelations(ctx, auth, reg, mem.ID, rawRelations)
			if visErr != nil {
				return visErr
			}
			conf, confErr := memory.ExpectedConfidence(pit.AlphaScore, pit.BetaScore)
			if confErr != nil {
				conf = mem.Confidence
			}
			env := security.WrapUntrustedXML(mem.ID, string(mem.Type), string(pit.EpistemicStatus), conf, mem.PrimarySourceID.String(), pit.Content)
			if err := reg.Audit().RecordAllowed(ctx, memory.AuditEvent{
				ID:            uuid.New(),
				TenantID:      &auth.TenantID,
				PrincipalID:   &auth.PrincipalID,
				AgentID:       auth.AgentID,
				Operation:     "get_memory",
				ResourceType:  "memory",
				ResourceID:    &mem.ID,
				ScopeUsed:     string(mem.ScopeType),
				CorrelationID: query.CorrelationID,
				Outcome:       memory.AuditAllowed,
				Details:       map[string]any{"point_in_time": true},
				CreatedAt:     now,
			}); err != nil {
				return fmt.Errorf("%w: %v", ErrOperational, err)
			}
			detail = &MemoryDetail{
				Memory:               *mem,
				Versions:             versions,
				Relations:            relations,
				PointInTimeVersion:   pit,
				UntrustedXMLEnvelope: env,
			}
			return nil
		}

		versions, verErr := reg.Memories().ListVersionsByMemoryID(ctx, mem.ID)
		if verErr != nil {
			return fmt.Errorf("%w: %v", ErrOperational, verErr)
		}
		rawRelations, relErr := reg.Memories().ListActiveRelationsByMemoryID(ctx, auth.TenantID, mem.ID)
		if relErr != nil {
			return fmt.Errorf("%w: %v", ErrOperational, relErr)
		}
		relations, visErr := s.filterVisibleRelations(ctx, auth, reg, mem.ID, rawRelations)
		if visErr != nil {
			return visErr
		}
		env := security.WrapUntrustedXML(mem.ID, string(mem.Type), string(mem.EpistemicStatus), mem.Confidence, mem.PrimarySourceID.String(), mem.Content)
		if err := reg.Audit().RecordAllowed(ctx, memory.AuditEvent{
			ID:            uuid.New(),
			TenantID:      &auth.TenantID,
			PrincipalID:   &auth.PrincipalID,
			AgentID:       auth.AgentID,
			Operation:     "get_memory",
			ResourceType:  "memory",
			ResourceID:    &mem.ID,
			ScopeUsed:     string(mem.ScopeType),
			CorrelationID: query.CorrelationID,
			Outcome:       memory.AuditAllowed,
			CreatedAt:     now,
		}); err != nil {
			return fmt.Errorf("%w: %v", ErrOperational, err)
		}
		detail = &MemoryDetail{
			Memory:               *mem,
			Versions:             versions,
			Relations:            relations,
			UntrustedXMLEnvelope: env,
		}
		return nil
	})
	if denied {
		if auditErr := s.recordDenied(ctx, auth, "get_memory", "memory", &query.MemoryID, deniedScope, query.CorrelationID); auditErr != nil {
			return nil, auditErr
		}
	}
	if err != nil {
		return nil, err
	}
	return detail, nil
}

// GetRequestStatus returns minimal opaque request lifecycle status when the caller has task read permission (AC-T11).
func (s *Service) GetRequestStatus(ctx context.Context, auth identity.AuthContext, requestID uuid.UUID, correlationID string) (*RequestStatus, error) {
	if auth.TenantID == uuid.Nil || auth.PrincipalID == uuid.Nil {
		return nil, ErrUnauthorized
	}
	if requestID == uuid.Nil {
		return nil, fmt.Errorf("%w: request_id is required", ErrValidation)
	}
	if s.opaqueStatus == nil {
		return nil, ErrOperational
	}

	meta, err := s.opaqueStatus.GetStatus(ctx, auth.TenantID, requestID)
	if err != nil {
		if errors.Is(err, ErrNotFound) || errors.Is(err, memory.ErrNotFound) {
			if auditErr := s.recordDenied(ctx, auth, "get_request_status", "opaque_request", &requestID, string(identity.ScopeTaskShared), correlationID); auditErr != nil {
				return nil, auditErr
			}
			return nil, ErrNotFound
		}
		return nil, ErrOperational
	}
	if meta == nil {
		if auditErr := s.recordDenied(ctx, auth, "get_request_status", "opaque_request", &requestID, string(identity.ScopeTaskShared), correlationID); auditErr != nil {
			return nil, auditErr
		}
		return nil, ErrNotFound
	}

	if !slices.Contains(auth.AllowedScopes, identity.ScopeTaskShared) || !slices.Contains(auth.AllowedProjects, meta.ProjectID) {
		if auditErr := s.recordDenied(ctx, auth, "get_request_status", "opaque_request", &requestID, string(identity.ScopeTaskShared), correlationID); auditErr != nil {
			return nil, auditErr
		}
		return nil, ErrNotFound
	}
	if s.access == nil {
		return nil, ErrOperational
	}
	snap, accErr := s.access.EvaluateTaskAccess(ctx, identity.TaskPermissionQuery{
		TenantID:    auth.TenantID,
		ProjectID:   meta.ProjectID,
		PrincipalID: auth.PrincipalID,
		TaskID:      meta.TaskID,
		Permission:  identity.TaskRead,
	})
	if accErr != nil {
		return nil, ErrOperational
	}
	if !snap.CanRead {
		if auditErr := s.recordDenied(ctx, auth, "get_request_status", "opaque_request", &requestID, string(identity.ScopeTaskShared), correlationID); auditErr != nil {
			return nil, auditErr
		}
		return nil, ErrNotFound
	}

	if err := s.audit.RecordAllowed(ctx, memory.AuditEvent{
		ID:            uuid.New(),
		TenantID:      &auth.TenantID,
		PrincipalID:   &auth.PrincipalID,
		AgentID:       auth.AgentID,
		Operation:     "get_request_status",
		ResourceType:  "opaque_request",
		ResourceID:    &requestID,
		ScopeUsed:     string(identity.ScopeTaskShared),
		CorrelationID: correlationID,
		Outcome:       memory.AuditAllowed,
		CreatedAt:     s.clock(),
	}); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrOperational, err)
	}

	return &RequestStatus{
		RequestID: meta.RequestID,
		Status:    meta.Status,
	}, nil
}

func (s *Service) authorizeRead(ctx context.Context, auth identity.AuthContext, mem *memory.Memory) error {
	if mem.ScopeType == identity.ScopeTaskShared {
		if !slices.Contains(auth.AllowedScopes, identity.ScopeTaskShared) || mem.ProjectID == nil || !slices.Contains(auth.AllowedProjects, *mem.ProjectID) {
			return ErrNotFound
		}
		if s.access == nil {
			return ErrOperational
		}
		snap, err := s.access.EvaluateTaskAccess(ctx, identity.TaskPermissionQuery{
			TenantID:    auth.TenantID,
			ProjectID:   *mem.ProjectID,
			PrincipalID: auth.PrincipalID,
			TaskID:      mem.TaskID,
			Permission:  identity.TaskRead,
		})
		if err != nil {
			return ErrOperational
		}
		if !snap.CanRead {
			return ErrNotFound
		}
		return nil
	}

	target := identity.MemoryScopeTarget{
		TenantID:     mem.TenantID,
		ProjectID:    mem.ProjectID,
		ScopeType:    mem.ScopeType,
		OwnerAgentID: mem.OwnerAgentID,
		TaskID:       mem.TaskID,
	}
	if !auth.CanReadMemory(target, "") {
		return ErrNotFound
	}
	return nil
}

func (s *Service) filterVisibleRelations(
	ctx context.Context,
	auth identity.AuthContext,
	reg memory.RepositoryRegistry,
	centerID uuid.UUID,
	rels []memory.MemoryRelation,
) ([]memory.MemoryRelation, error) {
	if len(rels) == 0 {
		return rels, nil
	}
	peerCache := make(map[uuid.UUID]bool, len(rels))
	visible := make([]memory.MemoryRelation, 0, len(rels))
	for _, rel := range rels {
		peerID := rel.ToMemoryID
		if peerID == centerID {
			peerID = rel.FromMemoryID
		}
		allowed, cached := peerCache[peerID]
		if !cached {
			peerMem, err := reg.Memories().GetMemoryByID(ctx, auth.TenantID, peerID)
			if err != nil {
				if errors.Is(err, memory.ErrNotFound) {
					peerCache[peerID] = false
					continue
				}
				return nil, fmt.Errorf("%w: %v", ErrOperational, err)
			}
			accessErr := s.authorizeRead(ctx, auth, peerMem)
			if accessErr != nil {
				if errors.Is(accessErr, ErrNotFound) {
					peerCache[peerID] = false
					continue
				}
				return nil, accessErr
			}
			allowed = true
			peerCache[peerID] = true
		}
		if allowed {
			visible = append(visible, rel)
		}
	}
	return visible, nil
}

func (s *Service) recordDenied(
	ctx context.Context,
	auth identity.AuthContext,
	operation string,
	resourceType string,
	resourceID *uuid.UUID,
	scopeUsed string,
	correlationID string,
) error {
	if err := s.audit.RecordDenied(ctx, memory.AuditEvent{
		ID:            uuid.New(),
		TenantID:      &auth.TenantID,
		PrincipalID:   &auth.PrincipalID,
		AgentID:       auth.AgentID,
		Operation:     operation,
		ResourceType:  resourceType,
		ResourceID:    resourceID,
		ScopeUsed:     scopeUsed,
		CorrelationID: correlationID,
		Outcome:       memory.AuditDenied,
		CreatedAt:     s.clock(),
	}); err != nil {
		return fmt.Errorf("%w: %v", ErrOperational, err)
	}
	return nil
}
