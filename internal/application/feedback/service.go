// Package feedback implements Zero-Trust Bayesian Beta(alpha, beta) feedback calibration,
// synchronous < 0.30 quarantine, atomic corrected_by_user replacement, and opaque queuing.
package feedback

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/romero-project/romero/internal/application/capture"
	"github.com/romero-project/romero/internal/domain/identity"
	"github.com/romero-project/romero/internal/domain/memory"
)

var (
	// ErrUnauthorized indicates missing or invalid caller identity.
	ErrUnauthorized = errors.New("unauthorized")
	// ErrForbidden mirrors identity.ErrForbidden for policy or epistemic denials.
	ErrForbidden = identity.ErrForbidden
	// ErrNotFound mirrors memory.ErrNotFound when a target memory is missing or outside scope.
	ErrNotFound = memory.ErrNotFound
	// ErrConflict indicates a closed memory mutation or idempotency conflict.
	ErrConflict = errors.New("conflict")
	// ErrValidation indicates malformed feedback input.
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

// OpaqueFeedbackRequest carries a feedback payload queued for a non-reader task_shared writer.
type OpaqueFeedbackRequest struct {
	RequestID      uuid.UUID
	TenantID       uuid.UUID
	ProjectID      uuid.UUID
	TaskID         string
	PrincipalID    uuid.UUID
	TargetMemoryID uuid.UUID
	Payload        []byte
	RequestSHA256  string
	IdempotencyKey string
}

// OpaqueReceipt holds only the server-assigned opaque request identifier for HTTP 202 responses.
type OpaqueReceipt struct {
	RequestID uuid.UUID
}

// OpaqueFeedbackEnqueuer persists opaque feedback requests before returning 202.
type OpaqueFeedbackEnqueuer interface {
	EnqueueFeedback(ctx context.Context, req OpaqueFeedbackRequest) (OpaqueReceipt, error)
}

// ReplacementExecutor atomically replaces a corrected memory inside the same transaction.
type ReplacementExecutor interface {
	ExecuteSupersedeReplacementTx(ctx context.Context, reg memory.RepositoryRegistry, auth identity.AuthContext, oldMem *memory.Memory, cmd capture.CaptureCommand, reason string) (*capture.MemoryView, error)
}

// FeedbackCommand carries inputs for recording an empirical feedback signal against a memory.
type FeedbackCommand struct {
	MemoryID          uuid.UUID
	TaskID            string
	Signal            memory.FeedbackSignal
	Comment           string
	IdempotencyKey    string
	CorrelationID     string
	ReplacementMemory *capture.CaptureCommand
}

// FeedbackResult summarizes synchronous Bayesian recalibration or opaque acceptance (202).
type FeedbackResult struct {
	Accepted            bool
	OpaqueReceipt       *OpaqueReceipt
	MemoryID            uuid.UUID
	NewAlpha            float32
	NewBeta             float32
	NewConfidence       float32
	NewUtility          float32
	NewEpistemic        memory.EpistemicStatus
	NewStatus           memory.MemoryStatus
	Quarantined         bool
	ReplacementMemoryID *uuid.UUID
}

// Service orchestrates Zero-Trust feedback recording, quarantine, and atomic replacement.
type Service struct {
	uow      memory.UnitOfWork
	audit    memory.AuditLogger
	access   TaskAccessChecker
	opaque   OpaqueFeedbackEnqueuer
	replacer ReplacementExecutor
	clock    func() time.Time
}

// NewService constructs a FeedbackService with its required transactional and security ports.
func NewService(
	uow memory.UnitOfWork,
	audit memory.AuditLogger,
	access TaskAccessChecker,
	opaque OpaqueFeedbackEnqueuer,
	replacer ReplacementExecutor,
	clock func() time.Time,
) (*Service, error) {
	if uow == nil || audit == nil {
		return nil, errors.New("uow and audit logger are required")
	}
	if clock == nil {
		clock = func() time.Time { return time.Now().UTC() }
	}
	return &Service{
		uow:      uow,
		audit:    audit,
		access:   access,
		opaque:   opaque,
		replacer: replacer,
		clock:    clock,
	}, nil
}

// RecordFeedback validates, authorizes, and records empirical feedback or queues an opaque 202 request.
func (s *Service) RecordFeedback(ctx context.Context, auth identity.AuthContext, cmd FeedbackCommand) (*FeedbackResult, error) {
	if auth.TenantID == uuid.Nil || auth.PrincipalID == uuid.Nil {
		return nil, ErrUnauthorized
	}
	if cmd.MemoryID == uuid.Nil {
		return nil, fmt.Errorf("%w: memory_id is required", ErrValidation)
	}
	if !cmd.Signal.Valid() {
		return nil, fmt.Errorf("%w: %v", ErrValidation, memory.ErrInvalidFeedbackSignal)
	}
	if err := memory.CanApplyFeedbackSignal(auth.PrincipalType, cmd.Signal); err != nil {
		if auditErr := s.recordDenied(ctx, auth, "record_feedback", &cmd.MemoryID, "", cmd.CorrelationID, map[string]any{"reason": "signal_authority"}); auditErr != nil {
			return nil, auditErr
		}
		return nil, ErrForbidden
	}

	if opaqueRes, handled, err := s.tryOpaqueFeedback(ctx, auth, cmd); handled || err != nil {
		if err != nil {
			return nil, err
		}
		return &FeedbackResult{Accepted: true, OpaqueReceipt: opaqueRes, MemoryID: cmd.MemoryID}, nil
	}

	var result *FeedbackResult
	var denied bool
	var deniedReason string
	var deniedScope string
	err := s.uow.ExecTx(ctx, func(reg memory.RepositoryRegistry) error {
		res, txErr := s.ExecuteFeedbackTx(ctx, reg, auth, cmd)
		if txErr != nil {
			if errors.Is(txErr, ErrNotFound) || errors.Is(txErr, ErrForbidden) {
				denied = true
				deniedReason = txErr.Error()
			}
			return txErr
		}
		result = res
		return nil
	})
	if denied {
		if auditErr := s.recordDenied(ctx, auth, "record_feedback", &cmd.MemoryID, deniedScope, cmd.CorrelationID, map[string]any{"reason": deniedReason}); auditErr != nil {
			return nil, auditErr
		}
	}
	if err != nil {
		return nil, err
	}
	return result, nil
}

// ExecuteFeedbackTx applies feedback inside an already-open UnitOfWork transaction.
func (s *Service) ExecuteFeedbackTx(
	ctx context.Context,
	reg memory.RepositoryRegistry,
	auth identity.AuthContext,
	cmd FeedbackCommand,
) (*FeedbackResult, error) {
	mem, err := reg.Memories().GetMemoryByID(ctx, auth.TenantID, cmd.MemoryID)
	if err != nil {
		if errors.Is(err, memory.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	if accessErr := s.authorizeTarget(ctx, auth, mem); accessErr != nil {
		return nil, accessErr
	}

	return s.executeFeedbackOnLoaded(ctx, reg, auth, mem, cmd)
}

// ExecuteOpaqueFeedbackTx applies deferred opaque feedback inside the worker's READ COMMITTED
// transaction after the worker has already reauthorized TaskWrite and verified task_shared scope.
func (s *Service) ExecuteOpaqueFeedbackTx(
	ctx context.Context,
	reg memory.RepositoryRegistry,
	auth identity.AuthContext,
	cmd FeedbackCommand,
) (*FeedbackResult, error) {
	mem, err := reg.Memories().GetMemoryByID(ctx, auth.TenantID, cmd.MemoryID)
	if err != nil {
		if errors.Is(err, memory.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return s.executeFeedbackOnLoaded(ctx, reg, auth, mem, cmd)
}

func (s *Service) executeFeedbackOnLoaded(
	ctx context.Context,
	reg memory.RepositoryRegistry,
	auth identity.AuthContext,
	mem *memory.Memory,
	cmd FeedbackCommand,
) (*FeedbackResult, error) {

	delta, err := memory.ApplyFeedback(mem.AlphaScore, mem.BetaScore, mem.UtilityScore, mem.EpistemicStatus, mem.Status, cmd.Signal)
	if err != nil {
		if errors.Is(err, memory.ErrClosedMemoryConflict) {
			return nil, ErrConflict
		}
		return nil, err
	}

	now := s.clock()
	fb := memory.MemoryFeedback{
		ID:           uuid.New(),
		TenantID:     auth.TenantID,
		MemoryID:     mem.ID,
		AgentID:      auth.AgentID,
		TaskID:       mem.TaskID,
		Signal:       cmd.Signal,
		AlphaDelta:   delta.AlphaDelta,
		BetaDelta:    delta.BetaDelta,
		UtilityDelta: delta.UtilityDelta,
		Comment:      cmd.Comment,
		CreatedAt:    now,
	}
	if err := reg.Feedback().InsertMemoryFeedback(ctx, fb); err != nil {
		return nil, err
	}

	mem.AlphaScore = delta.NewAlpha
	mem.BetaScore = delta.NewBeta
	mem.Confidence = delta.NewConfidence
	mem.UtilityScore = delta.NewUtility
	mem.EpistemicStatus = delta.NewEpistemic
	mem.Status = delta.NewStatus
	mem.AccessCount += delta.AccessIncrement
	if delta.TouchLastAccessed {
		mem.LastAccessedAt = &now
	}
	mem.UpdatedAt = now

	if delta.Quarantined || delta.EpistemicChanged {
		if err := reg.Memories().CloseMemoryVersionValidity(ctx, mem.ID, mem.Version, now); err != nil {
			return nil, err
		}
		mem.Version++
		validUntil := mem.Clocks.ValidUntil
		changeReason := "confirmed_verified"
		if delta.Quarantined {
			validUntil = &now
			mem.Clocks.ValidUntil = &now
			changeReason = "quarantined by feedback"
		}
		if err := reg.Memories().InsertMemoryVersion(ctx, memory.MemoryVersion{
			ID:              uuid.New(),
			MemoryID:        mem.ID,
			Version:         mem.Version,
			Summary:         mem.Summary,
			Content:         mem.Content,
			Tags:            append([]string(nil), mem.Tags...),
			Entities:        append([]string(nil), mem.Entities...),
			Metadata:        mem.Metadata,
			EpistemicStatus: mem.EpistemicStatus,
			AlphaScore:      mem.AlphaScore,
			BetaScore:       mem.BetaScore,
			Importance:      mem.Importance,
			UtilityScore:    mem.UtilityScore,
			Status:          mem.Status,
			ValidFrom:       now,
			ValidUntil:      validUntil,
			ChangedBySource: &mem.PrimarySourceID,
			ChangeReason:    changeReason,
			CreatedAt:       now,
		}); err != nil {
			return nil, err
		}
	}

	if err := reg.Memories().UpdateMemoryState(ctx, *mem); err != nil {
		return nil, err
	}

	var replacementID *uuid.UUID
	if cmd.Signal == memory.SignalCorrectedByUser && cmd.ReplacementMemory != nil {
		if s.replacer == nil {
			return nil, ErrOperational
		}
		repView, repErr := s.replacer.ExecuteSupersedeReplacementTx(ctx, reg, auth, mem, *cmd.ReplacementMemory, "corrected_by_user")
		if repErr != nil {
			return nil, repErr
		}
		replacementID = &repView.ID
	}

	if err := reg.Audit().RecordAllowed(ctx, memory.AuditEvent{
		ID:            uuid.New(),
		TenantID:      &auth.TenantID,
		PrincipalID:   &auth.PrincipalID,
		AgentID:       auth.AgentID,
		Operation:     "record_feedback",
		ResourceType:  "memory",
		ResourceID:    &mem.ID,
		ScopeUsed:     string(mem.ScopeType),
		CorrelationID: cmd.CorrelationID,
		Outcome:       memory.AuditAllowed,
		Details:       map[string]any{"signal": string(cmd.Signal), "quarantined": delta.Quarantined},
		CreatedAt:     now,
	}); err != nil {
		return nil, err
	}

	return &FeedbackResult{
		MemoryID:            mem.ID,
		NewAlpha:            delta.NewAlpha,
		NewBeta:             delta.NewBeta,
		NewConfidence:       delta.NewConfidence,
		NewUtility:          delta.NewUtility,
		NewEpistemic:        delta.NewEpistemic,
		NewStatus:           mem.Status,
		Quarantined:         delta.Quarantined,
		ReplacementMemoryID: replacementID,
	}, nil
}

func (s *Service) tryOpaqueFeedback(ctx context.Context, auth identity.AuthContext, cmd FeedbackCommand) (*OpaqueReceipt, bool, error) {
	if strings.TrimSpace(cmd.TaskID) == "" || len(auth.AllowedProjects) == 0 || !slices.Contains(auth.AllowedScopes, identity.ScopeTaskShared) || s.access == nil {
		return nil, false, nil
	}
	for _, pid := range auth.AllowedProjects {
		snap, err := s.access.EvaluateTaskAccess(ctx, identity.TaskPermissionQuery{
			TenantID:    auth.TenantID,
			ProjectID:   pid,
			PrincipalID: auth.PrincipalID,
			TaskID:      cmd.TaskID,
			Permission:  identity.TaskWrite,
		})
		if err != nil {
			return nil, false, ErrOperational
		}
		if snap.CanWrite && !snap.CanRead {
			if s.opaque == nil {
				return nil, false, ErrOperational
			}
			raw, mErr := json.Marshal(cmd)
			if mErr != nil {
				return nil, false, fmt.Errorf("%w: %v", ErrValidation, mErr)
			}
			sum := sha256.Sum256(raw)
			receipt, qErr := s.opaque.EnqueueFeedback(ctx, OpaqueFeedbackRequest{
				RequestID:      uuid.New(),
				TenantID:       auth.TenantID,
				ProjectID:      pid,
				TaskID:         cmd.TaskID,
				PrincipalID:    auth.PrincipalID,
				TargetMemoryID: cmd.MemoryID,
				Payload:        raw,
				RequestSHA256:  hex.EncodeToString(sum[:]),
				IdempotencyKey: cmd.IdempotencyKey,
			})
			if qErr != nil {
				if errors.Is(qErr, ErrConflict) || strings.Contains(strings.ToLower(qErr.Error()), "idempotency") || strings.Contains(strings.ToLower(qErr.Error()), "conflict") {
					return nil, false, ErrConflict
				}
				return nil, false, ErrOperational
			}
			return &receipt, true, nil
		}
		if snap.CanWrite && snap.CanRead {
			return nil, false, nil
		}
	}
	if auditErr := s.recordDenied(ctx, auth, "record_feedback", &cmd.MemoryID, string(identity.ScopeTaskShared), cmd.CorrelationID, map[string]any{"reason": "task_write_forbidden"}); auditErr != nil {
		return nil, false, auditErr
	}
	return nil, false, ErrForbidden
}

func (s *Service) authorizeTarget(ctx context.Context, auth identity.AuthContext, mem *memory.Memory) error {
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
		if !snap.CanWrite {
			return ErrForbidden
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
	if err := auth.CanWriteScope(mem.ProjectID, mem.ScopeType, mem.TaskID); err != nil {
		return ErrForbidden
	}
	return nil
}

func (s *Service) recordDenied(
	ctx context.Context,
	auth identity.AuthContext,
	operation string,
	resourceID *uuid.UUID,
	scopeUsed string,
	correlationID string,
	details map[string]any,
) error {
	if err := s.audit.RecordDenied(ctx, memory.AuditEvent{
		ID:            uuid.New(),
		TenantID:      &auth.TenantID,
		PrincipalID:   &auth.PrincipalID,
		AgentID:       auth.AgentID,
		Operation:     operation,
		ResourceType:  "memory",
		ResourceID:    resourceID,
		ScopeUsed:     scopeUsed,
		CorrelationID: correlationID,
		Outcome:       memory.AuditDenied,
		Details:       details,
		CreatedAt:     s.clock(),
	}); err != nil {
		return fmt.Errorf("%w: %v", ErrOperational, err)
	}
	return nil
}
