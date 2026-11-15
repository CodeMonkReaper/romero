package capture

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/romero-project/romero/internal/domain/identity"
	"github.com/romero-project/romero/internal/domain/memory"
	"github.com/romero-project/romero/internal/domain/provenance"
	"github.com/romero-project/romero/internal/domain/security"
)

// Service orchestrates Zero-Trust memory capture, bitemporal updates, explicit
// supersession, archival, and deferred opaque queuing for write-only task_shared actors.
type Service struct {
	uow    memory.UnitOfWork
	audit  memory.AuditLogger
	access TaskAccessChecker
	opaque OpaqueRequestEnqueuer
	clock  func() time.Time
}

// NewService constructs a CaptureService with its required transactional and security ports.
func NewService(
	uow memory.UnitOfWork,
	audit memory.AuditLogger,
	access TaskAccessChecker,
	opaque OpaqueRequestEnqueuer,
	clock func() time.Time,
) (*Service, error) {
	if uow == nil || audit == nil {
		return nil, errors.New("uow and audit logger are required")
	}
	if clock == nil {
		clock = func() time.Time { return time.Now().UTC() }
	}
	return &Service{
		uow:    uow,
		audit:  audit,
		access: access,
		opaque: opaque,
		clock:  clock,
	}, nil
}

// CaptureMemory validates, sanitizes, authorizes, and captures or queues a memory.
func (s *Service) CaptureMemory(ctx context.Context, auth identity.AuthContext, cmd CaptureCommand) (*CaptureResult, error) {
	if err := validateAuth(auth); err != nil {
		return nil, err
	}
	sanitized, err := validateCaptureCommand(cmd)
	if err != nil {
		return nil, err
	}

	if err := auth.CanAssignEpistemic(string(cmd.EpistemicStatus)); err != nil {
		if auditErr := s.recordDenied(ctx, auth, "capture_memory", "memory", nil, string(cmd.ScopeType), cmd.CorrelationID, map[string]any{"reason": "epistemic_authority"}); auditErr != nil {
			return nil, auditErr
		}
		return nil, ErrForbidden
	}

	if cmd.ScopeType == identity.ScopeTaskShared {
		snap, err := s.authorizeTaskWrite(ctx, auth, cmd.ProjectID, cmd.TaskID, "capture_memory", nil, cmd.CorrelationID)
		if err != nil {
			return nil, err
		}
		if !snap.CanRead {
			receipt, qErr := s.enqueueOpaque(ctx, auth, *cmd.ProjectID, cmd.TaskID, OpaqueOpCapture, nil, cmd.IdempotencyKey, map[string]any{
				"capture": cmd,
				"summary": sanitized.Summary,
				"content": sanitized.Content,
			})
			if qErr != nil {
				return nil, qErr
			}
			return &CaptureResult{Accepted: true, OpaqueReceipt: &receipt}, nil
		}
	} else if err := auth.CanWriteScope(cmd.ProjectID, cmd.ScopeType, cmd.TaskID); err != nil {
		if auditErr := s.recordDenied(ctx, auth, "capture_memory", "memory", nil, string(cmd.ScopeType), cmd.CorrelationID, map[string]any{"reason": "scope_forbidden"}); auditErr != nil {
			return nil, auditErr
		}
		return nil, mapDomainWriteErr(err)
	}

	var view *MemoryView
	var created bool
	err = s.uow.ExecTx(ctx, func(reg memory.RepositoryRegistry) error {
		v, cr, txErr := s.ExecuteCaptureTx(ctx, reg, auth, cmd)
		if txErr != nil {
			return txErr
		}
		view = v
		created = cr
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &CaptureResult{Created: created, Memory: view}, nil
}

// UpdateMemory creates a new bitemporal version of an open memory or queues an opaque update.
func (s *Service) UpdateMemory(ctx context.Context, auth identity.AuthContext, memoryID uuid.UUID, cmd UpdateCommand) (*MutationResult, error) {
	if err := validateAuth(auth); err != nil {
		return nil, err
	}
	if memoryID == uuid.Nil {
		return nil, fmt.Errorf("%w: memory_id is required", ErrValidation)
	}
	sanitized, err := security.SanitizeInput(cmd.Summary, cmd.Content)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrValidation, err)
	}
	if cmd.Importance != nil && !isValidUnitFloat(*cmd.Importance) {
		return nil, fmt.Errorf("%w: importance must be within [0, 1]", ErrValidation)
	}

	if opaqueRes, handled, err := s.tryOpaqueTargetMutation(ctx, auth, memoryID, cmd.TaskID, OpaqueOpUpdate, cmd.IdempotencyKey, cmd.CorrelationID, "update_memory", cmd); handled || err != nil {
		if err != nil {
			return nil, err
		}
		return &MutationResult{Accepted: true, OpaqueReceipt: opaqueRes}, nil
	}

	var out *MemoryView
	var denied bool
	var deniedReason string
	var deniedScope string
	err = s.uow.ExecTx(ctx, func(reg memory.RepositoryRegistry) error {
		mem, accessErr := s.loadAndAuthorizeTarget(ctx, reg, auth, memoryID, true)
		if accessErr != nil {
			if errors.Is(accessErr, ErrNotFound) || errors.Is(accessErr, ErrForbidden) {
				denied = true
				deniedReason = accessErr.Error()
				if mem != nil {
					deniedScope = string(mem.ScopeType)
				}
			}
			return accessErr
		}
		v, txErr := s.executeUpdateOnLoaded(ctx, reg, auth, mem, cmd, sanitized)
		if txErr != nil {
			return txErr
		}
		out = v
		return nil
	})
	if denied {
		if auditErr := s.recordDenied(ctx, auth, "update_memory", "memory", &memoryID, deniedScope, cmd.CorrelationID, map[string]any{"reason": deniedReason}); auditErr != nil {
			return nil, auditErr
		}
	}
	if err != nil {
		return nil, err
	}
	return &MutationResult{Memory: out}, nil
}

// SupersedeMemory explicitly supersedes an existing open memory or queues an opaque supersession.
func (s *Service) SupersedeMemory(ctx context.Context, auth identity.AuthContext, oldID uuid.UUID, cmd ExplicitSupersedeCommand) (*SupersedeResult, error) {
	if err := validateAuth(auth); err != nil {
		return nil, err
	}
	if oldID == uuid.Nil {
		return nil, fmt.Errorf("%w: memory_id is required", ErrValidation)
	}
	if _, err := validateCaptureCommand(cmd.Capture); err != nil {
		return nil, err
	}
	if err := auth.CanAssignEpistemic(string(cmd.Capture.EpistemicStatus)); err != nil {
		if auditErr := s.recordDenied(ctx, auth, "supersede_memory", "memory", &oldID, string(cmd.Capture.ScopeType), cmd.CorrelationID, map[string]any{"reason": "epistemic_authority"}); auditErr != nil {
			return nil, auditErr
		}
		return nil, ErrForbidden
	}

	taskSelector := cmd.TaskID
	if taskSelector == "" {
		taskSelector = cmd.Capture.TaskID
	}
	if opaqueRes, handled, qErr := s.tryOpaqueTargetMutation(ctx, auth, oldID, taskSelector, OpaqueOpSupersede, cmd.Capture.IdempotencyKey, cmd.CorrelationID, "supersede_memory", cmd); handled || qErr != nil {
		if qErr != nil {
			return nil, qErr
		}
		return &SupersedeResult{Accepted: true, OpaqueReceipt: opaqueRes}, nil
	}

	var res *SupersedeResult
	var denied bool
	var deniedReason string
	var deniedScope string
	err := s.uow.ExecTx(ctx, func(reg memory.RepositoryRegistry) error {
		oldMem, accessErr := s.loadAndAuthorizeTarget(ctx, reg, auth, oldID, true)
		if accessErr != nil {
			if errors.Is(accessErr, ErrNotFound) || errors.Is(accessErr, ErrForbidden) {
				denied = true
				deniedReason = accessErr.Error()
				if oldMem != nil {
					deniedScope = string(oldMem.ScopeType)
				}
			}
			return accessErr
		}
		if oldMem.Status.IsClosed() {
			return ErrConflict
		}
		decision, decErr := memory.DecideTopicSupersession(oldMem.EpistemicStatus, cmd.Capture.EpistemicStatus, true, auth.PrincipalType, cmd.ChangeReason)
		if decErr != nil {
			switch {
			case errors.Is(decErr, memory.ErrForbidden):
				denied = true
				deniedReason = "epistemic_override_forbidden"
				deniedScope = string(oldMem.ScopeType)
				return ErrForbidden
			case errors.Is(decErr, memory.ErrMissingChangeReason):
				return fmt.Errorf("%w: %v", ErrValidation, decErr)
			case errors.Is(decErr, memory.ErrEpistemicDowngradeNotAllowed):
				return fmt.Errorf("%w: %v", ErrConflict, decErr)
			default:
				return decErr
			}
		}

		// Ensure replacement stays within the same scope boundary when omitted, and authorize destination write.
		captureCmd := cmd.Capture
		if captureCmd.ProjectID == nil {
			captureCmd.ProjectID = oldMem.ProjectID
		}
		if captureCmd.TaskID == "" {
			captureCmd.TaskID = oldMem.TaskID
		}
		if captureCmd.TopicKey == "" {
			captureCmd.TopicKey = oldMem.TopicKey
		}

		if captureCmd.ScopeType == identity.ScopeTaskShared {
			if _, taskErr := s.authorizeTaskWrite(ctx, auth, captureCmd.ProjectID, captureCmd.TaskID, "supersede_memory", &oldMem.ID, cmd.CorrelationID); taskErr != nil {
				return taskErr
			}
		} else if scopeErr := auth.CanWriteScope(captureCmd.ProjectID, captureCmd.ScopeType, captureCmd.TaskID); scopeErr != nil {
			denied = true
			deniedReason = "destination_scope_forbidden"
			deniedScope = string(captureCmd.ScopeType)
			return mapDomainWriteErr(scopeErr)
		}

		now := s.clock()
		newID := uuid.New()
		if decision.CloseOldValidUntil {
			if err := s.closeAndSupersedeOldStateTx(ctx, reg, oldMem, newID, cmd.ChangeReason, now); err != nil {
				return err
			}
		}

		newView, _, txErr := s.insertMemoryWithSourceTx(ctx, reg, auth, captureCmd, memory.StatusActive, newID, now)
		if txErr != nil {
			return txErr
		}

		if decision.CloseOldValidUntil {
			if err := s.insertSupersedesRelationTx(ctx, reg, oldMem, newView.ID, newView.PrimarySourceID, cmd.ChangeReason, now); err != nil {
				return err
			}
		}

		if err := reg.Audit().RecordAllowed(ctx, memory.AuditEvent{
			ID:            uuid.New(),
			TenantID:      &auth.TenantID,
			PrincipalID:   &auth.PrincipalID,
			AgentID:       auth.AgentID,
			Operation:     "supersede_memory",
			ResourceType:  "memory",
			ResourceID:    &oldMem.ID,
			ScopeUsed:     string(oldMem.ScopeType),
			CorrelationID: cmd.CorrelationID,
			Outcome:       memory.AuditAllowed,
			Details:       map[string]any{"new_memory_id": newView.ID.String()},
			CreatedAt:     now,
		}); err != nil {
			return err
		}

		res = &SupersedeResult{
			OldMemory: MemoryViewFromDomain(oldMem),
			NewMemory: newView,
		}
		return nil
	})
	if denied {
		if auditErr := s.recordDenied(ctx, auth, "supersede_memory", "memory", &oldID, deniedScope, cmd.CorrelationID, map[string]any{"reason": deniedReason}); auditErr != nil {
			return nil, auditErr
		}
	}
	if err != nil {
		return nil, err
	}
	return res, nil
}

// ArchiveMemory closes an open memory as archived or queues an opaque archival.
func (s *Service) ArchiveMemory(ctx context.Context, auth identity.AuthContext, memoryID uuid.UUID, cmd ArchiveCommand) (*MutationResult, error) {
	if err := validateAuth(auth); err != nil {
		return nil, err
	}
	if memoryID == uuid.Nil {
		return nil, fmt.Errorf("%w: memory_id is required", ErrValidation)
	}

	if opaqueRes, handled, qErr := s.tryOpaqueTargetMutation(ctx, auth, memoryID, cmd.TaskID, OpaqueOpArchive, cmd.IdempotencyKey, cmd.CorrelationID, "archive_memory", cmd); handled || qErr != nil {
		if qErr != nil {
			return nil, qErr
		}
		return &MutationResult{Accepted: true, OpaqueReceipt: opaqueRes}, nil
	}

	var out *MemoryView
	var denied bool
	var deniedReason string
	var deniedScope string
	err := s.uow.ExecTx(ctx, func(reg memory.RepositoryRegistry) error {
		mem, accessErr := s.loadAndAuthorizeTarget(ctx, reg, auth, memoryID, true)
		if accessErr != nil {
			if errors.Is(accessErr, ErrNotFound) || errors.Is(accessErr, ErrForbidden) {
				denied = true
				deniedReason = accessErr.Error()
				if mem != nil {
					deniedScope = string(mem.ScopeType)
				}
			}
			return accessErr
		}
		if mem.Status.IsClosed() {
			return ErrConflict
		}

		now := s.clock()
		if err := reg.Memories().CloseMemoryVersionValidity(ctx, mem.ID, mem.Version, now); err != nil {
			return err
		}
		mem.Status = memory.StatusArchived
		mem.Version++
		mem.UpdatedAt = now
		mem.Clocks.ValidUntil = &now
		if err := reg.Memories().UpdateMemoryState(ctx, *mem); err != nil {
			return err
		}
		if err := reg.Memories().InsertMemoryVersion(ctx, memory.MemoryVersion{
			ID:              uuid.New(),
			MemoryID:        mem.ID,
			Version:         mem.Version,
			Summary:         mem.Summary,
			Content:         mem.Content,
			Tags:            append([]string(nil), mem.Tags...),
			Entities:        append([]string(nil), mem.Entities...),
			Metadata:        cloneMap(mem.Metadata),
			EpistemicStatus: mem.EpistemicStatus,
			AlphaScore:      mem.AlphaScore,
			BetaScore:       mem.BetaScore,
			Importance:      mem.Importance,
			UtilityScore:    mem.UtilityScore,
			Status:          memory.StatusArchived,
			ValidFrom:       now,
			ValidUntil:      &now,
			ChangedBySource: &mem.PrimarySourceID,
			ChangeReason:    cmd.Reason,
			CreatedAt:       now,
		}); err != nil {
			return err
		}
		if err := reg.Audit().RecordAllowed(ctx, memory.AuditEvent{
			ID:            uuid.New(),
			TenantID:      &auth.TenantID,
			PrincipalID:   &auth.PrincipalID,
			AgentID:       auth.AgentID,
			Operation:     "archive_memory",
			ResourceType:  "memory",
			ResourceID:    &mem.ID,
			ScopeUsed:     string(mem.ScopeType),
			CorrelationID: cmd.CorrelationID,
			Outcome:       memory.AuditAllowed,
			CreatedAt:     now,
		}); err != nil {
			return err
		}
		out = MemoryViewFromDomain(mem)
		return nil
	})
	if denied {
		if auditErr := s.recordDenied(ctx, auth, "archive_memory", "memory", &memoryID, deniedScope, cmd.CorrelationID, map[string]any{"reason": deniedReason}); auditErr != nil {
			return nil, auditErr
		}
	}
	if err != nil {
		return nil, err
	}
	return &MutationResult{Memory: out}, nil
}

// ExecuteCaptureTx executes a capture inside an already-open UnitOfWork transaction.
// It is reused by CaptureMemory, T007 (corrected_by_user replacement), and T011 (opaque worker).
func (s *Service) ExecuteCaptureTx(
	ctx context.Context,
	reg memory.RepositoryRegistry,
	auth identity.AuthContext,
	cmd CaptureCommand,
) (*MemoryView, bool, error) {
	sanitized, err := validateCaptureCommand(cmd)
	if err != nil {
		return nil, false, err
	}

	if cmd.IdempotencyKey != "" {
		existing, lookupErr := reg.Memories().GetMemoryByIdempotencyKey(ctx, auth.TenantID, cmd.IdempotencyKey)
		if lookupErr == nil {
			if existing.ContentSHA256 == sanitized.ContentSHA256 && existing.ScopeType == cmd.ScopeType {
				return MemoryViewFromDomain(existing), false, nil
			}
			return nil, false, ErrConflict
		}
		if !errors.Is(lookupErr, memory.ErrNotFound) {
			return nil, false, lookupErr
		}
	}

	now := s.clock()
	newStatus := memory.StatusActive
	var conflictingOld *memory.Memory
	var supersedingOld *memory.Memory

	if strings.TrimSpace(cmd.TopicKey) != "" {
		scopeTarget := identity.MemoryScopeTarget{
			TenantID:     auth.TenantID,
			ProjectID:    cmd.ProjectID,
			ScopeType:    cmd.ScopeType,
			OwnerAgentID: ownerAgentForScope(auth, cmd.ScopeType),
			TaskID:       cmd.TaskID,
		}
		activeOld, lockErr := reg.Memories().LockActiveTopicMemory(ctx, scopeTarget, cmd.TopicKey)
		if lockErr != nil && !errors.Is(lockErr, memory.ErrNotFound) {
			return nil, false, lockErr
		}
		if lockErr == nil && activeOld != nil {
			decision, decErr := memory.DecideTopicSupersession(activeOld.EpistemicStatus, cmd.EpistemicStatus, false, auth.PrincipalType, "")
			if decErr != nil {
				return nil, false, decErr
			}
			if decision.Outcome == memory.OutcomeSupersedeOld {
				supersedingOld = activeOld
				newStatus = memory.StatusActive
			} else {
				conflictingOld = activeOld
				newStatus = memory.StatusConflicted
			}
		}
	}

	newID := uuid.New()
	if supersedingOld != nil {
		if err := s.closeAndSupersedeOldStateTx(ctx, reg, supersedingOld, newID, "superseded by topic_key", now); err != nil {
			return nil, false, err
		}
	}

	view, _, err := s.insertMemoryWithSourceTx(ctx, reg, auth, cmd, newStatus, newID, now)
	if err != nil {
		return nil, false, err
	}

	if supersedingOld != nil {
		if err := s.insertSupersedesRelationTx(ctx, reg, supersedingOld, view.ID, view.PrimarySourceID, "superseded by topic_key", now); err != nil {
			return nil, false, err
		}
	} else if conflictingOld != nil {
		if err := reg.Memories().InsertMemoryRelation(ctx, memory.MemoryRelation{
			ID:           uuid.New(),
			TenantID:     auth.TenantID,
			ProjectID:    cmd.ProjectID,
			FromMemoryID: view.ID,
			ToMemoryID:   conflictingOld.ID,
			RelationType: memory.RelationContradicts,
			Weight:       1.0,
			SourceID:     &view.PrimarySourceID,
			ValidFrom:    now,
			IsActive:     true,
			Notes:        "lower epistemic rank on active topic_key",
			CreatedAt:    now,
		}); err != nil {
			return nil, false, err
		}
	}

	if err := reg.Audit().RecordAllowed(ctx, memory.AuditEvent{
		ID:            uuid.New(),
		TenantID:      &auth.TenantID,
		PrincipalID:   &auth.PrincipalID,
		AgentID:       auth.AgentID,
		Operation:     "capture_memory",
		ResourceType:  "memory",
		ResourceID:    &view.ID,
		ScopeUsed:     string(cmd.ScopeType),
		CorrelationID: cmd.CorrelationID,
		Outcome:       memory.AuditAllowed,
		CreatedAt:     now,
	}); err != nil {
		return nil, false, err
	}

	return view, true, nil
}

// ExecuteSupersedeReplacementTx atomically closes oldMem as superseded and inserts a replacement
// memory inside an existing UnitOfWork transaction (reused by T007 corrected_by_user).
func (s *Service) ExecuteSupersedeReplacementTx(
	ctx context.Context,
	reg memory.RepositoryRegistry,
	auth identity.AuthContext,
	oldMem *memory.Memory,
	cmd CaptureCommand,
	reason string,
) (*MemoryView, error) {
	if oldMem == nil {
		return nil, ErrNotFound
	}
	if cmd.ProjectID == nil {
		cmd.ProjectID = oldMem.ProjectID
	}
	if cmd.TaskID == "" {
		cmd.TaskID = oldMem.TaskID
	}
	if cmd.TopicKey == "" {
		cmd.TopicKey = oldMem.TopicKey
	}
	if _, err := validateCaptureCommand(cmd); err != nil {
		return nil, err
	}
	if err := auth.CanAssignEpistemic(string(cmd.EpistemicStatus)); err != nil {
		return nil, ErrForbidden
	}
	if cmd.ScopeType == identity.ScopeTaskShared {
		if _, err := s.authorizeTaskWrite(ctx, auth, cmd.ProjectID, cmd.TaskID, "record_feedback", &oldMem.ID, cmd.CorrelationID); err != nil {
			return nil, err
		}
	} else if err := auth.CanWriteScope(cmd.ProjectID, cmd.ScopeType, cmd.TaskID); err != nil {
		return nil, mapDomainWriteErr(err)
	}
	return s.executeSupersedeTx(ctx, reg, auth, oldMem, cmd, reason, true, s.clock())
}

func (s *Service) executeSupersedeTx(
	ctx context.Context,
	reg memory.RepositoryRegistry,
	auth identity.AuthContext,
	oldMem *memory.Memory,
	captureCmd CaptureCommand,
	reason string,
	closeOld bool,
	now time.Time,
) (*MemoryView, error) {
	newID := uuid.New()
	if closeOld {
		if err := s.closeAndSupersedeOldStateTx(ctx, reg, oldMem, newID, reason, now); err != nil {
			return nil, err
		}
	}
	newView, _, err := s.insertMemoryWithSourceTx(ctx, reg, auth, captureCmd, memory.StatusActive, newID, now)
	if err != nil {
		return nil, err
	}
	if closeOld {
		if err := s.insertSupersedesRelationTx(ctx, reg, oldMem, newView.ID, newView.PrimarySourceID, reason, now); err != nil {
			return nil, err
		}
	}
	return newView, nil
}

func (s *Service) insertMemoryWithSourceTx(
	ctx context.Context,
	reg memory.RepositoryRegistry,
	auth identity.AuthContext,
	cmd CaptureCommand,
	status memory.MemoryStatus,
	memID uuid.UUID,
	now time.Time,
) (*MemoryView, security.SanitizedPayload, error) {
	sanitized, err := security.SanitizeInput(cmd.Summary, cmd.Content)
	if err != nil {
		return nil, security.SanitizedPayload{}, fmt.Errorf("%w: %v", ErrValidation, err)
	}

	sourceID := uuid.New()
	src := provenance.MemorySource{
		ID:               sourceID,
		TenantID:         auth.TenantID,
		ProjectID:        cmd.ProjectID,
		SourceType:       cmd.SourceType,
		ExternalSourceID: cmd.ExternalSourceID,
		ActorPrincipalID: &auth.PrincipalID,
		ActorAgentID:     auth.AgentID,
		URI:              cmd.URI,
		RawExcerptHash:   cmd.RawExcerptHash,
		Metadata:         cloneMap(cmd.SourceMetadata),
		CreatedAt:        now,
	}
	if err := reg.Sources().InsertSource(ctx, src); err != nil {
		return nil, security.SanitizedPayload{}, err
	}

	alpha, beta, conf := memory.InitialPriors(cmd.EpistemicStatus)
	observedAt := now
	if cmd.ObservedAt != nil && !cmd.ObservedAt.IsZero() {
		observedAt = *cmd.ObservedAt
	}
	validFrom := now
	if cmd.ValidFrom != nil && !cmd.ValidFrom.IsZero() {
		validFrom = *cmd.ValidFrom
	}

	meta := cloneMap(cmd.MemoryMetadata)
	if sanitized.Sanitized {
		if meta == nil {
			meta = make(map[string]any, 1)
		}
		meta["sanitized"] = true
	}

	mem := memory.Memory{
		ID:              memID,
		TenantID:        auth.TenantID,
		ProjectID:       cmd.ProjectID,
		OwnerAgentID:    auth.AgentID,
		TaskID:          cmd.TaskID,
		Type:            cmd.Type,
		ScopeType:       cmd.ScopeType,
		Status:          status,
		TopicKey:        cmd.TopicKey,
		Summary:         sanitized.Summary,
		Content:         sanitized.Content,
		ContentSHA256:   sanitized.ContentSHA256,
		TokenCount:      sanitized.TokenCount,
		Tags:            append([]string(nil), cmd.Tags...),
		Entities:        append([]string(nil), cmd.Entities...),
		EpistemicStatus: cmd.EpistemicStatus,
		AlphaScore:      alpha,
		BetaScore:       beta,
		Confidence:      conf,
		Importance:      cmd.Importance,
		UtilityScore:    0,
		AccessCount:     0,
		Version:         1,
		NLIChecked:      false,
		PrimarySourceID: sourceID,
		IdempotencyKey:  cmd.IdempotencyKey,
		Clocks: memory.TriTemporalWindow{
			ObservedAt: observedAt,
			CreatedAt:  now,
			ValidFrom:  validFrom,
			ValidUntil: cmd.ValidUntil,
		},
		UpdatedAt: now,
		Metadata:  meta,
	}
	if err := reg.Memories().InsertMemory(ctx, mem); err != nil {
		return nil, security.SanitizedPayload{}, err
	}
	if err := reg.Memories().InsertMemoryVersion(ctx, memory.MemoryVersion{
		ID:              uuid.New(),
		MemoryID:        memID,
		Version:         1,
		Summary:         mem.Summary,
		Content:         mem.Content,
		Tags:            append([]string(nil), mem.Tags...),
		Entities:        append([]string(nil), mem.Entities...),
		Metadata:        cloneMap(mem.Metadata),
		EpistemicStatus: mem.EpistemicStatus,
		AlphaScore:      mem.AlphaScore,
		BetaScore:       mem.BetaScore,
		Importance:      mem.Importance,
		UtilityScore:    mem.UtilityScore,
		Status:          mem.Status,
		ValidFrom:       validFrom,
		ValidUntil:      cmd.ValidUntil,
		ChangedBySource: &sourceID,
		CreatedAt:       now,
	}); err != nil {
		return nil, security.SanitizedPayload{}, err
	}
	if err := reg.Jobs().EnqueueMemoryJob(ctx, memory.MemoryJob{
		ID:          uuid.New(),
		TenantID:    auth.TenantID,
		ProjectID:   cmd.ProjectID,
		MemoryID:    &memID,
		JobType:     "embed_and_nli",
		Priority:    100,
		Status:      memory.JobPending,
		Attempts:    0,
		MaxAttempts: 3,
		ScheduledAt: now,
		CreatedAt:   now,
	}); err != nil {
		return nil, security.SanitizedPayload{}, err
	}

	return MemoryViewFromDomain(&mem), sanitized, nil
}

func (s *Service) closeAndSupersedeOldStateTx(
	ctx context.Context,
	reg memory.RepositoryRegistry,
	oldMem *memory.Memory,
	_ uuid.UUID,
	reason string,
	now time.Time,
) error {
	if err := reg.Memories().CloseMemoryVersionValidity(ctx, oldMem.ID, oldMem.Version, now); err != nil {
		return err
	}
	oldMem.Status = memory.StatusSuperseded
	oldMem.Version++
	oldMem.UpdatedAt = now
	oldMem.Clocks.ValidUntil = &now
	if err := reg.Memories().UpdateMemoryState(ctx, *oldMem); err != nil {
		return err
	}
	return reg.Memories().InsertMemoryVersion(ctx, memory.MemoryVersion{
		ID:              uuid.New(),
		MemoryID:        oldMem.ID,
		Version:         oldMem.Version,
		Summary:         oldMem.Summary,
		Content:         oldMem.Content,
		Tags:            append([]string(nil), oldMem.Tags...),
		Entities:        append([]string(nil), oldMem.Entities...),
		Metadata:        cloneMap(oldMem.Metadata),
		EpistemicStatus: oldMem.EpistemicStatus,
		AlphaScore:      oldMem.AlphaScore,
		BetaScore:       oldMem.BetaScore,
		Importance:      oldMem.Importance,
		UtilityScore:    oldMem.UtilityScore,
		Status:          memory.StatusSuperseded,
		ValidFrom:       now,
		ValidUntil:      &now,
		ChangedBySource: &oldMem.PrimarySourceID,
		ChangeReason:    reason,
		CreatedAt:       now,
	})
}

func (s *Service) insertSupersedesRelationTx(
	ctx context.Context,
	reg memory.RepositoryRegistry,
	oldMem *memory.Memory,
	newID uuid.UUID,
	sourceID uuid.UUID,
	reason string,
	now time.Time,
) error {
	return reg.Memories().InsertMemoryRelation(ctx, memory.MemoryRelation{
		ID:           uuid.New(),
		TenantID:     oldMem.TenantID,
		ProjectID:    oldMem.ProjectID,
		FromMemoryID: newID,
		ToMemoryID:   oldMem.ID,
		RelationType: memory.RelationSupersedes,
		Weight:       1.0,
		SourceID:     &sourceID,
		ValidFrom:    now,
		IsActive:     true,
		Notes:        reason,
		CreatedAt:    now,
	})
}

func (s *Service) executeUpdateOnLoaded(
	ctx context.Context,
	reg memory.RepositoryRegistry,
	auth identity.AuthContext,
	mem *memory.Memory,
	cmd UpdateCommand,
	sanitized security.SanitizedPayload,
) (*MemoryView, error) {
	if mem.Status.IsClosed() {
		return nil, ErrConflict
	}

	now := s.clock()
	if err := reg.Memories().CloseMemoryVersionValidity(ctx, mem.ID, mem.Version, now); err != nil {
		return nil, err
	}

	meta := cloneMap(cmd.Metadata)
	if meta == nil {
		meta = cloneMap(mem.Metadata)
	}
	if sanitized.Sanitized {
		if meta == nil {
			meta = make(map[string]any, 1)
		}
		meta["sanitized"] = true
	}

	mem.Version++
	mem.Summary = sanitized.Summary
	mem.Content = sanitized.Content
	mem.ContentSHA256 = sanitized.ContentSHA256
	mem.TokenCount = sanitized.TokenCount
	if cmd.Tags != nil {
		mem.Tags = append([]string(nil), cmd.Tags...)
	}
	if cmd.Entities != nil {
		mem.Entities = append([]string(nil), cmd.Entities...)
	}
	if cmd.Importance != nil {
		mem.Importance = *cmd.Importance
	}
	mem.Metadata = meta
	mem.Clocks.ValidFrom = now
	mem.UpdatedAt = now

	if err := reg.Memories().UpdateMemoryState(ctx, *mem); err != nil {
		return nil, err
	}
	if err := reg.Memories().InsertMemoryVersion(ctx, memory.MemoryVersion{
		ID:              uuid.New(),
		MemoryID:        mem.ID,
		Version:         mem.Version,
		Summary:         mem.Summary,
		Content:         mem.Content,
		Tags:            append([]string(nil), mem.Tags...),
		Entities:        append([]string(nil), mem.Entities...),
		Metadata:        cloneMap(mem.Metadata),
		EpistemicStatus: mem.EpistemicStatus,
		AlphaScore:      mem.AlphaScore,
		BetaScore:       mem.BetaScore,
		Importance:      mem.Importance,
		UtilityScore:    mem.UtilityScore,
		Status:          mem.Status,
		ValidFrom:       now,
		ValidUntil:      mem.Clocks.ValidUntil,
		ChangedBySource: &mem.PrimarySourceID,
		ChangeReason:    cmd.ChangeReason,
		CreatedAt:       now,
	}); err != nil {
		return nil, err
	}
	if err := reg.Jobs().EnqueueMemoryJob(ctx, memory.MemoryJob{
		ID:          uuid.New(),
		TenantID:    auth.TenantID,
		ProjectID:   mem.ProjectID,
		MemoryID:    &mem.ID,
		JobType:     "embed_and_nli",
		Priority:    100,
		Status:      memory.JobPending,
		Attempts:    0,
		MaxAttempts: 3,
		ScheduledAt: now,
		CreatedAt:   now,
	}); err != nil {
		return nil, err
	}
	if err := reg.Audit().RecordAllowed(ctx, memory.AuditEvent{
		ID:            uuid.New(),
		TenantID:      &auth.TenantID,
		PrincipalID:   &auth.PrincipalID,
		AgentID:       auth.AgentID,
		Operation:     "update_memory",
		ResourceType:  "memory",
		ResourceID:    &mem.ID,
		ScopeUsed:     string(mem.ScopeType),
		CorrelationID: cmd.CorrelationID,
		Outcome:       memory.AuditAllowed,
		CreatedAt:     now,
	}); err != nil {
		return nil, err
	}
	return MemoryViewFromDomain(mem), nil
}

func (s *Service) tryOpaqueTargetMutation(
	ctx context.Context,
	auth identity.AuthContext,
	targetID uuid.UUID,
	taskID string,
	op OpaqueOperation,
	idempotencyKey string,
	correlationID string,
	auditOp string,
	payload any,
) (*OpaqueReceipt, bool, error) {
	if strings.TrimSpace(taskID) == "" || len(auth.AllowedProjects) == 0 || !slices.Contains(auth.AllowedScopes, identity.ScopeTaskShared) || s.access == nil {
		return nil, false, nil
	}
	// When a task_shared principal supplies a task selector, check if they are a write-only
	// non-reader on that task; if so, enqueue an opaque 202 without resolving targetID.
	for _, projectID := range auth.AllowedProjects {
		pid := projectID
		snap, err := s.access.EvaluateTaskAccess(ctx, identity.TaskPermissionQuery{
			TenantID:    auth.TenantID,
			ProjectID:   pid,
			PrincipalID: auth.PrincipalID,
			TaskID:      taskID,
			Permission:  identity.TaskWrite,
		})
		if err != nil {
			return nil, false, ErrOperational
		}
		if snap.CanWrite && !snap.CanRead {
			receipt, qErr := s.enqueueOpaque(ctx, auth, pid, taskID, op, &targetID, idempotencyKey, payload)
			if qErr != nil {
				return nil, false, qErr
			}
			return &receipt, true, nil
		}
		if snap.CanWrite && snap.CanRead {
			return nil, false, nil
		}
	}
	if auditErr := s.recordDenied(ctx, auth, auditOp, "memory", &targetID, string(identity.ScopeTaskShared), correlationID, map[string]any{"reason": "task_write_forbidden"}); auditErr != nil {
		return nil, false, auditErr
	}
	return nil, false, ErrForbidden
}

func (s *Service) loadAndAuthorizeTarget(
	ctx context.Context,
	reg memory.RepositoryRegistry,
	auth identity.AuthContext,
	memoryID uuid.UUID,
	requireWrite bool,
) (*memory.Memory, error) {
	mem, err := reg.Memories().GetMemoryByID(ctx, auth.TenantID, memoryID)
	if err != nil {
		if errors.Is(err, memory.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	target := identity.MemoryScopeTarget{
		TenantID:     mem.TenantID,
		ProjectID:    mem.ProjectID,
		ScopeType:    mem.ScopeType,
		OwnerAgentID: mem.OwnerAgentID,
		TaskID:       mem.TaskID,
	}

	if mem.ScopeType == identity.ScopeTaskShared {
		if !slices.Contains(auth.AllowedScopes, identity.ScopeTaskShared) || mem.ProjectID == nil || !slices.Contains(auth.AllowedProjects, *mem.ProjectID) {
			return mem, ErrNotFound
		}
		if s.access == nil {
			return mem, ErrOperational
		}
		snap, accErr := s.access.EvaluateTaskAccess(ctx, identity.TaskPermissionQuery{
			TenantID:    auth.TenantID,
			ProjectID:   *mem.ProjectID,
			PrincipalID: auth.PrincipalID,
			TaskID:      mem.TaskID,
			Permission:  identity.TaskRead,
		})
		if accErr != nil {
			return mem, ErrOperational
		}
		if !snap.CanRead {
			return mem, ErrNotFound
		}
		if requireWrite && !snap.CanWrite {
			return mem, ErrForbidden
		}
		return mem, nil
	}

	if !auth.CanReadMemory(target, "") {
		return mem, ErrNotFound
	}
	if requireWrite {
		if err := auth.CanWriteScope(mem.ProjectID, mem.ScopeType, mem.TaskID); err != nil {
			return mem, mapDomainWriteErr(err)
		}
	}
	return mem, nil
}

func (s *Service) authorizeTaskWrite(
	ctx context.Context,
	auth identity.AuthContext,
	projectID *uuid.UUID,
	taskID string,
	auditOp string,
	resourceID *uuid.UUID,
	correlationID string,
) (TaskAccessSnapshot, error) {
	if !slices.Contains(auth.AllowedScopes, identity.ScopeTaskShared) || projectID == nil || *projectID == uuid.Nil || !slices.Contains(auth.AllowedProjects, *projectID) {
		if auditErr := s.recordDenied(ctx, auth, auditOp, "memory", resourceID, string(identity.ScopeTaskShared), correlationID, map[string]any{"reason": "project_or_scope_forbidden"}); auditErr != nil {
			return TaskAccessSnapshot{}, auditErr
		}
		return TaskAccessSnapshot{}, ErrForbidden
	}
	if s.access == nil {
		return TaskAccessSnapshot{}, ErrOperational
	}
	snap, err := s.access.EvaluateTaskAccess(ctx, identity.TaskPermissionQuery{
		TenantID:    auth.TenantID,
		ProjectID:   *projectID,
		PrincipalID: auth.PrincipalID,
		TaskID:      taskID,
		Permission:  identity.TaskWrite,
	})
	if err != nil {
		return TaskAccessSnapshot{}, ErrOperational
	}
	if !snap.CanWrite {
		if auditErr := s.recordDenied(ctx, auth, auditOp, "memory", resourceID, string(identity.ScopeTaskShared), correlationID, map[string]any{"reason": "task_write_forbidden"}); auditErr != nil {
			return TaskAccessSnapshot{}, auditErr
		}
		return TaskAccessSnapshot{}, ErrForbidden
	}
	return snap, nil
}

func (s *Service) enqueueOpaque(
	ctx context.Context,
	auth identity.AuthContext,
	projectID uuid.UUID,
	taskID string,
	op OpaqueOperation,
	targetID *uuid.UUID,
	idempotencyKey string,
	payloadObj any,
) (OpaqueReceipt, error) {
	if s.opaque == nil {
		return OpaqueReceipt{}, ErrOperational
	}
	raw, err := json.Marshal(payloadObj)
	if err != nil {
		return OpaqueReceipt{}, fmt.Errorf("%w: %v", ErrValidation, err)
	}
	sum := sha256.Sum256(raw)
	req := OpaqueRequest{
		RequestID:      uuid.New(),
		TenantID:       auth.TenantID,
		ProjectID:      projectID,
		TaskID:         taskID,
		PrincipalID:    auth.PrincipalID,
		Operation:      op,
		TargetMemoryID: targetID,
		PayloadVersion: 1,
		Payload:        raw,
		RequestSHA256:  hex.EncodeToString(sum[:]),
		IdempotencyKey: idempotencyKey,
	}
	receipt, err := s.opaque.Enqueue(ctx, req)
	if err != nil {
		if errors.Is(err, ErrConflict) || strings.Contains(strings.ToLower(err.Error()), "idempotency") || strings.Contains(strings.ToLower(err.Error()), "conflict") {
			return OpaqueReceipt{}, ErrConflict
		}
		return OpaqueReceipt{}, ErrOperational
	}
	return receipt, nil
}

func (s *Service) recordDenied(
	ctx context.Context,
	auth identity.AuthContext,
	operation string,
	resourceType string,
	resourceID *uuid.UUID,
	scopeUsed string,
	correlationID string,
	details map[string]any,
) error {
	ev := memory.AuditEvent{
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
		Details:       details,
		CreatedAt:     s.clock(),
	}
	if err := s.audit.RecordDenied(ctx, ev); err != nil {
		return fmt.Errorf("%w: %v", ErrOperational, err)
	}
	return nil
}

func validateAuth(auth identity.AuthContext) error {
	if auth.TenantID == uuid.Nil || auth.PrincipalID == uuid.Nil {
		return ErrUnauthorized
	}
	return nil
}

func validateCaptureCommand(cmd CaptureCommand) (security.SanitizedPayload, error) {
	if !cmd.Type.Valid() {
		return security.SanitizedPayload{}, fmt.Errorf("%w: %v", ErrValidation, memory.ErrInvalidMemoryType)
	}
	if !cmd.EpistemicStatus.Valid() {
		return security.SanitizedPayload{}, fmt.Errorf("%w: %v", ErrValidation, memory.ErrInvalidEpistemicStatus)
	}
	if !validScopeType(cmd.ScopeType) {
		return security.SanitizedPayload{}, fmt.Errorf("%w: invalid scope_type", ErrValidation)
	}
	if cmd.ScopeType == identity.ScopeTaskShared && strings.TrimSpace(cmd.TaskID) == "" {
		return security.SanitizedPayload{}, fmt.Errorf("%w: %v", ErrValidation, identity.ErrMissingTaskID)
	}
	if !cmd.SourceType.Valid() {
		return security.SanitizedPayload{}, fmt.Errorf("%w: %v", ErrValidation, provenance.ErrInvalidSourceType)
	}
	if strings.TrimSpace(cmd.ExternalSourceID) == "" {
		return security.SanitizedPayload{}, fmt.Errorf("%w: %v", ErrValidation, provenance.ErrMissingExternalSourceID)
	}
	if !isValidUnitFloat(cmd.Importance) {
		return security.SanitizedPayload{}, fmt.Errorf("%w: importance must be within [0, 1]", ErrValidation)
	}
	sanitized, err := security.SanitizeInput(cmd.Summary, cmd.Content)
	if err != nil {
		return security.SanitizedPayload{}, fmt.Errorf("%w: %v", ErrValidation, err)
	}
	return sanitized, nil
}

func validScopeType(s identity.ScopeType) bool {
	switch s {
	case identity.ScopeAgentPrivate,
		identity.ScopeTaskShared,
		identity.ScopeProjectShared,
		identity.ScopeOrgShared,
		identity.ScopeGlobalPublic:
		return true
	default:
		return false
	}
}

func isValidUnitFloat(v float32) bool {
	f := float64(v)
	return !math.IsNaN(f) && !math.IsInf(f, 0) && v >= 0 && v <= 1
}

func ownerAgentForScope(auth identity.AuthContext, _ identity.ScopeType) *uuid.UUID {
	return auth.AgentID
}

func mapDomainWriteErr(err error) error {
	if errors.Is(err, identity.ErrMissingTaskID) {
		return fmt.Errorf("%w: %v", ErrValidation, err)
	}
	return ErrForbidden
}
