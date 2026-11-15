// Package conflicts implements Zero-Trust conflict listing and synchronous atomic arbitration.
package conflicts

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/romero-project/romero/internal/domain/identity"
	"github.com/romero-project/romero/internal/domain/memory"
)

var (
	ErrValidation    = errors.New("validation failed")
	ErrUnauthorized  = errors.New("unauthorized")
	ErrForbidden     = errors.New("forbidden")
	ErrNotFound      = errors.New("conflict not found")
	ErrConflict      = errors.New("conflict state error")
	ErrInvalidWinner = errors.New("winner_memory_id does not belong to conflict")
	ErrOperational   = errors.New("operational persistence error")
)

const reviewWinnerAlphaDelta float32 = 1.0

type conflictTxProvider interface {
	Conflicts() memory.ConflictRepository
}

// L1ConflictNotifier combines graph mutation and relation deactivation notifications.
type L1ConflictNotifier interface {
	memory.GraphMutationNotifier
	memory.ConflictGraphNotifier
}

// ListConflictsFilter configures Zero-Trust pagination over active contradictions.
type ListConflictsFilter struct {
	ProjectID *uuid.UUID
	Limit     int
	Offset    int
}

// ReviewConflictCommand arbitrates an active conflict edge in favor of WinnerMemoryID.
type ReviewConflictCommand struct {
	ConflictID     uuid.UUID
	WinnerMemoryID uuid.UUID
	Reason         string
	CorrelationID  string
}

// ConflictRecordView is the transport-agnostic representation of an active or resolved conflict.
type ConflictRecordView struct {
	ConflictID       uuid.UUID
	TenantID         uuid.UUID
	ProjectID        *uuid.UUID
	FromMemory       memory.Memory
	ToMemory         memory.Memory
	Weight           float32
	NLIEntailment    *float32
	NLIContradiction *float32
	IsActive         bool
	ValidFrom        time.Time
	ValidUntil       *time.Time
	Notes            string
	CreatedAt        time.Time
}

// Service orchestrates Zero-Trust conflict listing and ReviewConflict arbitration.
type Service struct {
	uow       memory.UnitOfWork
	conflicts memory.ConflictRepository
	audit     memory.AuditLogger
	perms     identity.TaskPermissionReader
	l1        L1ConflictNotifier
	clock     func() time.Time
}

// NewService constructs a conflicts.Service.
func NewService(
	uow memory.UnitOfWork,
	conflicts memory.ConflictRepository,
	audit memory.AuditLogger,
	perms identity.TaskPermissionReader,
	l1 L1ConflictNotifier,
	clock func() time.Time,
) (*Service, error) {
	if uow == nil || conflicts == nil || audit == nil {
		return nil, errors.New("uow, conflicts repository, and audit logger are required")
	}
	if clock == nil {
		clock = func() time.Time { return time.Now().UTC() }
	}
	return &Service{
		uow:       uow,
		conflicts: conflicts,
		audit:     audit,
		perms:     perms,
		l1:        l1,
		clock:     clock,
	}, nil
}

// ListConflicts returns active 'contradicts' pairs where the caller has read access to BOTH endpoints.
func (s *Service) ListConflicts(ctx context.Context, auth identity.AuthContext, filter ListConflictsFilter) ([]ConflictRecordView, error) {
	if auth.TenantID == uuid.Nil || auth.PrincipalID == uuid.Nil {
		return nil, ErrUnauthorized
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}
	if filter.ProjectID != nil && !auth.IsAdmin {
		allowedProj := false
		for _, p := range auth.AllowedProjects {
			if p == *filter.ProjectID {
				allowedProj = true
				break
			}
		}
		if !allowedProj {
			return []ConflictRecordView{}, nil
		}
	}

	now := s.clock()
	rows, err := s.conflicts.ListActiveConflicts(ctx, auth.TenantID, filter.ProjectID, limit, offset, now)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrOperational, err)
	}

	out := make([]ConflictRecordView, 0, len(rows))
	for _, row := range rows {
		canFrom, err := s.canReadEndpoint(ctx, auth, row.FromMemory)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrOperational, err)
		}
		if !canFrom {
			continue
		}
		canTo, err := s.canReadEndpoint(ctx, auth, row.ToMemory)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrOperational, err)
		}
		if !canTo {
			continue
		}
		out = append(out, toConflictView(row))
	}
	return out, nil
}

// ReviewConflict atomically arbitrates an active conflict, enforcing 404 opacity, 403 write/admin guard,
// and 422 winner validation, followed by write-through L1 cache synchronization.
func (s *Service) ReviewConflict(ctx context.Context, auth identity.AuthContext, cmd ReviewConflictCommand) (*ConflictRecordView, error) {
	if auth.TenantID == uuid.Nil || auth.PrincipalID == uuid.Nil {
		return nil, ErrUnauthorized
	}
	if cmd.ConflictID == uuid.Nil || cmd.WinnerMemoryID == uuid.Nil {
		return nil, fmt.Errorf("%w: conflict_id and winner_memory_id are required", ErrValidation)
	}

	now := s.clock()
	var (
		denied       bool
		deniedReason string
		deniedScope  string
		result       *ConflictRecordView
		winnerMem    memory.Memory
		loserMem     memory.Memory
		supersedes   memory.MemoryRelation
	)

	err := s.uow.ExecTx(ctx, func(reg memory.RepositoryRegistry) error {
		conflictRepo := s.conflicts
		if p, ok := reg.(conflictTxProvider); ok {
			conflictRepo = p.Conflicts()
		}
		pair, err := conflictRepo.GetConflictForUpdate(ctx, auth.TenantID, cmd.ConflictID)
		if err != nil {
			if errors.Is(err, memory.ErrNotFound) {
				denied = true
				deniedReason = "conflict_not_found"
				return ErrNotFound
			}
			return fmt.Errorf("%w: %v", ErrOperational, err)
		}

		canFrom, err := s.canReadEndpoint(ctx, auth, pair.FromMemory)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrOperational, err)
		}
		canTo, err := s.canReadEndpoint(ctx, auth, pair.ToMemory)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrOperational, err)
		}
		if !canFrom || !canTo {
			denied = true
			deniedReason = "endpoint_read_opaque_denied"
			return ErrNotFound
		}

		var winner, loser memory.Memory
		switch cmd.WinnerMemoryID {
		case pair.FromMemory.ID:
			winner = pair.FromMemory
			loser = pair.ToMemory
		case pair.ToMemory.ID:
			winner = pair.ToMemory
			loser = pair.FromMemory
		default:
			return ErrInvalidWinner
		}

		if err := s.canWriteEndpoint(ctx, auth, winner); err != nil {
			denied = true
			deniedReason = "winner_write_forbidden"
			deniedScope = string(winner.ScopeType)
			return ErrForbidden
		}
		if err := s.canWriteEndpoint(ctx, auth, loser); err != nil {
			denied = true
			deniedReason = "loser_write_forbidden"
			deniedScope = string(loser.ScopeType)
			return ErrForbidden
		}
		if loser.EpistemicStatus == memory.EpistemicVerified && !auth.IsAdmin {
			denied = true
			deniedReason = "verified_loser_requires_admin"
			deniedScope = string(loser.ScopeType)
			return ErrForbidden
		}

		if err := reg.Memories().CloseMemoryVersionValidity(ctx, winner.ID, winner.Version, now); err != nil {
			return fmt.Errorf("%w: %v", ErrOperational, err)
		}
		winner.Status = memory.StatusActive
		winner.AlphaScore += reviewWinnerAlphaDelta
		if winner.AlphaScore+winner.BetaScore > 0 {
			winner.Confidence = winner.AlphaScore / (winner.AlphaScore + winner.BetaScore)
		}
		winner.Version++
		winner.UpdatedAt = now
		if err := reg.Memories().UpdateMemoryState(ctx, winner); err != nil {
			return fmt.Errorf("%w: %v", ErrOperational, err)
		}
		if err := reg.Memories().InsertMemoryVersion(ctx, buildReviewVersion(winner, "conflict_review_winner", now)); err != nil {
			return fmt.Errorf("%w: %v", ErrOperational, err)
		}

		if err := reg.Memories().CloseMemoryVersionValidity(ctx, loser.ID, loser.Version, now); err != nil {
			return fmt.Errorf("%w: %v", ErrOperational, err)
		}
		loser.Status = memory.StatusSuperseded
		loser.Clocks.ValidUntil = &now
		loser.Version++
		loser.UpdatedAt = now
		if err := reg.Memories().UpdateMemoryState(ctx, loser); err != nil {
			return fmt.Errorf("%w: %v", ErrOperational, err)
		}
		if err := reg.Memories().InsertMemoryVersion(ctx, buildReviewVersion(loser, "conflict_review_loser", now)); err != nil {
			return fmt.Errorf("%w: %v", ErrOperational, err)
		}

		if err := conflictRepo.DeactivateConflictEdge(ctx, auth.TenantID, cmd.ConflictID, now); err != nil {
			return fmt.Errorf("%w: %v", ErrOperational, err)
		}

		supersedes = memory.MemoryRelation{
			ID:           uuid.New(),
			TenantID:     auth.TenantID,
			ProjectID:    winner.ProjectID,
			FromMemoryID: winner.ID,
			ToMemoryID:   loser.ID,
			RelationType: memory.RelationSupersedes,
			Weight:       1.0,
			SourceID:     &winner.PrimarySourceID,
			ValidFrom:    now,
			IsActive:     true,
			Notes:        strings.TrimSpace(cmd.Reason),
			CreatedAt:    now,
		}
		if err := reg.Memories().InsertMemoryRelation(ctx, supersedes); err != nil {
			return fmt.Errorf("%w: %v", ErrOperational, err)
		}

		if err := reg.Audit().RecordAllowed(ctx, memory.AuditEvent{
			ID:            uuid.New(),
			TenantID:      &auth.TenantID,
			PrincipalID:   &auth.PrincipalID,
			AgentID:       auth.AgentID,
			Operation:     "review_conflict",
			ResourceType:  "memory_relation",
			ResourceID:    &cmd.ConflictID,
			ScopeUsed:     string(winner.ScopeType),
			CorrelationID: cmd.CorrelationID,
			Outcome:       memory.AuditAllowed,
			Details: map[string]any{
				"winner_memory_id": winner.ID.String(),
				"loser_memory_id":  loser.ID.String(),
			},
			CreatedAt: now,
		}); err != nil {
			return fmt.Errorf("%w: %v", ErrOperational, err)
		}

		pair.Relation.IsActive = false
		pair.Relation.ValidUntil = &now
		if pair.FromMemory.ID == winner.ID {
			pair.FromMemory = winner
			pair.ToMemory = loser
		} else {
			pair.FromMemory = loser
			pair.ToMemory = winner
		}
		view := toConflictView(*pair)
		result = &view
		winnerMem = winner
		loserMem = loser
		return nil
	})
	if denied {
		_ = s.audit.RecordDenied(ctx, memory.AuditEvent{
			ID:            uuid.New(),
			TenantID:      &auth.TenantID,
			PrincipalID:   &auth.PrincipalID,
			AgentID:       auth.AgentID,
			Operation:     "review_conflict",
			ResourceType:  "memory_relation",
			ResourceID:    &cmd.ConflictID,
			ScopeUsed:     deniedScope,
			CorrelationID: cmd.CorrelationID,
			Outcome:       memory.AuditDenied,
			Details:       map[string]any{"reason": deniedReason},
			CreatedAt:     now,
		})
	}
	if err != nil {
		return nil, err
	}

	if s.l1 != nil {
		s.l1.ApplyMemoryUpsert(winnerMem)
		s.l1.ApplyMemoryStatusChange(loserMem.TenantID, loserMem.ProjectID, loserMem.ID, loserMem.Status, loserMem.Clocks.ValidUntil)
		s.l1.ApplyRelationDeactivated(auth.TenantID, result.ProjectID, cmd.ConflictID)
		s.l1.ApplyRelationsAdded([]memory.MemoryRelation{supersedes})
	}
	return result, nil
}

func (s *Service) canReadEndpoint(ctx context.Context, auth identity.AuthContext, m memory.Memory) (bool, error) {
	target := identity.MemoryScopeTarget{
		TenantID:     m.TenantID,
		ProjectID:    m.ProjectID,
		OwnerAgentID: m.OwnerAgentID,
		ScopeType:    m.ScopeType,
		TaskID:       m.TaskID,
	}
	if m.ScopeType == identity.ScopeTaskShared {
		if auth.IsAdmin {
			return m.TenantID == auth.TenantID, nil
		}
		if s.perms == nil {
			return false, nil
		}
		err := auth.AuthorizeTaskMemory(ctx, s.perms, target, identity.TaskRead)
		if err != nil {
			if errors.Is(err, identity.ErrForbidden) {
				return false, nil
			}
			return false, err
		}
		return true, nil
	}
	return auth.CanReadMemory(target, m.TaskID), nil
}

func (s *Service) canWriteEndpoint(ctx context.Context, auth identity.AuthContext, m memory.Memory) error {
	if m.ScopeType == identity.ScopeTaskShared {
		if auth.IsAdmin && m.TenantID == auth.TenantID {
			return nil
		}
		if s.perms == nil {
			return identity.ErrForbidden
		}
		target := identity.MemoryScopeTarget{
			TenantID:     m.TenantID,
			ProjectID:    m.ProjectID,
			OwnerAgentID: m.OwnerAgentID,
			ScopeType:    m.ScopeType,
			TaskID:       m.TaskID,
		}
		return auth.AuthorizeTaskMemory(ctx, s.perms, target, identity.TaskWrite)
	}
	return auth.CanWriteScope(m.ProjectID, m.ScopeType, m.TaskID)
}

func buildReviewVersion(m memory.Memory, reason string, now time.Time) memory.MemoryVersion {
	return memory.MemoryVersion{
		ID:              uuid.New(),
		MemoryID:        m.ID,
		Version:         m.Version,
		Summary:         m.Summary,
		Content:         m.Content,
		Tags:            append([]string(nil), m.Tags...),
		Entities:        append([]string(nil), m.Entities...),
		Metadata:        m.Metadata,
		EpistemicStatus: m.EpistemicStatus,
		AlphaScore:      m.AlphaScore,
		BetaScore:       m.BetaScore,
		Importance:      m.Importance,
		UtilityScore:    m.UtilityScore,
		Status:          m.Status,
		ValidFrom:       m.Clocks.ValidFrom,
		ValidUntil:      m.Clocks.ValidUntil,
		ChangedBySource: &m.PrimarySourceID,
		ChangeReason:    reason,
		CreatedAt:       now,
	}
}

func toConflictView(row memory.ConflictPairRow) ConflictRecordView {
	return ConflictRecordView{
		ConflictID:       row.Relation.ID,
		TenantID:         row.Relation.TenantID,
		ProjectID:        row.Relation.ProjectID,
		FromMemory:       row.FromMemory,
		ToMemory:         row.ToMemory,
		Weight:           row.Relation.Weight,
		NLIEntailment:    row.Relation.NLIEntailment,
		NLIContradiction: row.Relation.NLIContradiction,
		IsActive:         row.Relation.IsActive,
		ValidFrom:        row.Relation.ValidFrom,
		ValidUntil:       row.Relation.ValidUntil,
		Notes:            row.Relation.Notes,
		CreatedAt:        row.Relation.CreatedAt,
	}
}
