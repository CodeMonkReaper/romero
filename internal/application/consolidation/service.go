// Package consolidation implements REM sleep episodic clustering, SLM distillation,
// NLI cross-validation, and preemptible ACID persistence.
package consolidation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/romero-project/romero/internal/domain/governor"
	"github.com/romero-project/romero/internal/domain/identity"
	"github.com/romero-project/romero/internal/domain/memory"
	"github.com/romero-project/romero/internal/domain/provenance"
)

var (
	ErrUnauthorized      = errors.New("unauthorized")
	ErrForbidden         = errors.New("forbidden")
	ErrEngineUnavailable = errors.New("consolidation engine unavailable")
	ErrOperational       = errors.New("operational consolidation error")
)

// ConsolidateCommand configures a REM sleep consolidation run.
type ConsolidateCommand struct {
	ProjectID     *uuid.UUID
	DryRun        bool
	CorrelationID string
}

// ConsolidateResult summarizes a REM consolidation execution.
type ConsolidateResult struct {
	ClustersFound        int
	DistilledCount       int
	ConsolidatedEpisodes int
	Preempted            bool
	CreatedMemoryIDs     []uuid.UUID
	Drafts               []memory.DistilledMemoryDraft
}

// Service orchestrates REM sleep consolidation under PowerGovernor preemption.
type Service struct {
	uow      memory.UnitOfWork
	repo     memory.ConsolidationRepository
	slm      memory.SLMConsolidationProvider
	nli      memory.NLIProvider
	audit    memory.AuditLogger
	l1       memory.GraphMutationNotifier
	governor *governor.PowerGovernor
	clock    func() time.Time
}

// NewService constructs a consolidation.Service.
func NewService(
	uow memory.UnitOfWork,
	repo memory.ConsolidationRepository,
	slm memory.SLMConsolidationProvider,
	nli memory.NLIProvider,
	audit memory.AuditLogger,
	l1 memory.GraphMutationNotifier,
	gov *governor.PowerGovernor,
	clock func() time.Time,
) (*Service, error) {
	if uow == nil || repo == nil || audit == nil {
		return nil, errors.New("uow, consolidation repository, and audit logger are required")
	}
	if clock == nil {
		clock = func() time.Time { return time.Now().UTC() }
	}
	return &Service{
		uow:      uow,
		repo:     repo,
		slm:      slm,
		nli:      nli,
		audit:    audit,
		l1:       l1,
		governor: gov,
		clock:    clock,
	}, nil
}

// Consolidate runs REM sleep clustering, SLM distillation, NLI cross-validation,
// and short per-cluster ACID commits, honoring <10ms PowerGovernor preemption.
func (s *Service) Consolidate(ctx context.Context, auth identity.AuthContext, cmd ConsolidateCommand) (*ConsolidateResult, error) {
	if auth.TenantID == uuid.Nil || auth.PrincipalID == uuid.Nil {
		return nil, ErrUnauthorized
	}
	now := s.clock()
	if !auth.IsAdmin && auth.PrincipalType != identity.PrincipalOrchestrator {
		s.recordDenied(ctx, auth, cmd, "requires_admin_or_orchestrator", now)
		return nil, ErrForbidden
	}
	if !auth.IsAdmin {
		if cmd.ProjectID == nil {
			s.recordDenied(ctx, auth, cmd, "project_id_required_for_non_admin", now)
			return nil, ErrForbidden
		}
		allowed := false
		for _, p := range auth.AllowedProjects {
			if p == *cmd.ProjectID {
				allowed = true
				break
			}
		}
		if !allowed {
			s.recordDenied(ctx, auth, cmd, "project_forbidden", now)
			return nil, ErrForbidden
		}
	}
	if !cmd.DryRun && (s.slm == nil || s.nli == nil) {
		return nil, ErrEngineUnavailable
	}

	res := &ConsolidateResult{}
	runFn := func(remCtx context.Context) error {
		return s.runConsolidate(remCtx, auth, cmd, now, res)
	}

	var err error
	if s.governor != nil {
		err = s.governor.RunInREMSleep(ctx, runFn)
	} else {
		err = runFn(ctx)
	}
	if errors.Is(err, governor.ErrREMPreempted) || errors.Is(err, context.Canceled) {
		res.Preempted = true
		return res, nil
	}
	if err != nil {
		return nil, err
	}
	return res, nil
}

func (s *Service) runConsolidate(
	remCtx context.Context,
	auth identity.AuthContext,
	cmd ConsolidateCommand,
	now time.Time,
	res *ConsolidateResult,
) error {
	candidates, vecs, err := s.repo.ListREMConsolidationCandidates(remCtx, auth.TenantID, cmd.ProjectID, memory.REMMinEpisodeConfidence)
	if err != nil {
		if remCtx.Err() != nil {
			res.Preempted = true
			return remCtx.Err()
		}
		return fmt.Errorf("%w: %v", ErrOperational, err)
	}

	clusters := memory.ClusterEpisodicMemories(candidates, vecs)
	res.ClustersFound = len(clusters)
	if cmd.DryRun || len(clusters) == 0 || s.slm == nil || s.nli == nil {
		return nil
	}
	defer func() {
		unloadCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.slm.UnloadVRAM(unloadCtx)
	}()

	verifiedMems, err := s.repo.ListActiveVerifiedByProject(remCtx, auth.TenantID, cmd.ProjectID, now)
	if err != nil {
		if remCtx.Err() != nil {
			res.Preempted = true
			return remCtx.Err()
		}
		return fmt.Errorf("%w: %v", ErrOperational, err)
	}

	for _, cluster := range clusters {
		if remCtx.Err() != nil {
			res.Preempted = true
			return remCtx.Err()
		}

		draft, err := s.slm.DistillCluster(remCtx, cluster.Episodes)
		if remCtx.Err() != nil {
			res.Preempted = true
			return remCtx.Err()
		}
		if err != nil {
			if errors.Is(err, memory.ErrProviderUnavailable) {
				return fmt.Errorf("%w: %v", ErrEngineUnavailable, err)
			}
			continue
		}

		draftText := strings.TrimSpace(draft.Summary + "\n" + draft.Content)
		if draftText == "" {
			continue
		}

		relevantVerified := memory.FilterVerifiedForScope(verifiedMems, cluster.ScopeTarget)
		allPairs := make([]memory.NLIPairInput, 0, len(cluster.Episodes)+len(relevantVerified))
		for _, ep := range cluster.Episodes {
			allPairs = append(allPairs, memory.NLIPairInput{
				Premise:    draftText,
				Hypothesis: strings.TrimSpace(ep.Summary + "\n" + ep.Content),
			})
		}
		for _, v := range relevantVerified {
			allPairs = append(allPairs, memory.NLIPairInput{
				Premise:    draftText,
				Hypothesis: strings.TrimSpace(v.Summary + "\n" + v.Content),
			})
		}

		scores, err := s.nli.EvaluatePairs(remCtx, allPairs)
		if remCtx.Err() != nil {
			res.Preempted = true
			return remCtx.Err()
		}
		if err != nil {
			if errors.Is(err, memory.ErrProviderUnavailable) {
				return fmt.Errorf("%w: %v", ErrEngineUnavailable, err)
			}
			continue
		}
		if len(scores) != len(allPairs) {
			continue
		}

		sourceScores := scores[:len(cluster.Episodes)]
		verifiedScores := scores[len(cluster.Episodes):]
		if !memory.ValidateDistilledNLI(sourceScores, verifiedScores) {
			continue
		}

		// Final preemption guard before opening ACID transaction.
		if remCtx.Err() != nil {
			res.Preempted = true
			return remCtx.Err()
		}

		newMem, derivedRels, consolidatedEps, commitErr := s.commitClusterTx(remCtx, auth, cmd, cluster, draft, now)
		if commitErr != nil {
			if remCtx.Err() != nil {
				res.Preempted = true
				return remCtx.Err()
			}
			return fmt.Errorf("%w: %v", ErrOperational, commitErr)
		}

		res.DistilledCount++
		res.ConsolidatedEpisodes += len(consolidatedEps)
		res.CreatedMemoryIDs = append(res.CreatedMemoryIDs, newMem.ID)
		res.Drafts = append(res.Drafts, draft)

		if s.l1 != nil {
			s.l1.ApplyMemoryUpsert(newMem)
			for _, ep := range consolidatedEps {
				s.l1.ApplyMemoryStatusChange(ep.TenantID, ep.ProjectID, ep.ID, ep.Status, ep.Clocks.ValidUntil)
			}
			if len(derivedRels) > 0 {
				s.l1.ApplyRelationsAdded(derivedRels)
			}
		}
	}
	return nil
}

func (s *Service) commitClusterTx(
	ctx context.Context,
	auth identity.AuthContext,
	cmd ConsolidateCommand,
	cluster memory.EpisodicCluster,
	draft memory.DistilledMemoryDraft,
	now time.Time,
) (memory.Memory, []memory.MemoryRelation, []memory.Memory, error) {
	newID := uuid.New()
	sourceID := uuid.New()
	memType := draft.MemoryType
	if memType != memory.TypeProcedural && memType != memory.TypeSemantic {
		memType = memory.TypeProcedural
	}
	summary := strings.TrimSpace(draft.Summary)
	if len(summary) > 600 {
		summary = summary[:600]
	}
	content := strings.TrimSpace(draft.Content)
	if content == "" {
		content = summary
	}
	sum := sha256.Sum256([]byte(content))
	shaHex := hex.EncodeToString(sum[:])

	epistemic := memory.EpistemicObserved
	for _, ep := range cluster.Episodes {
		if ep.EpistemicStatus != memory.EpistemicVerified && ep.EpistemicStatus != memory.EpistemicObserved {
			epistemic = memory.EpistemicInferred
			break
		}
	}

	newMem := memory.Memory{
		ID:              newID,
		TenantID:        auth.TenantID,
		ProjectID:       cluster.ScopeTarget.ProjectID,
		OwnerAgentID:    cluster.ScopeTarget.OwnerAgentID,
		TaskID:          cluster.ScopeTarget.TaskID,
		Type:            memType,
		ScopeType:       cluster.ScopeTarget.ScopeType,
		Status:          memory.StatusActive,
		TopicKey:        strings.TrimSpace(draft.TopicKey),
		Summary:         summary,
		Content:         content,
		ContentSHA256:   shaHex,
		TokenCount:      max(1, len(strings.Fields(content))),
		Tags:            append([]string(nil), draft.Tags...),
		Entities:        append([]string(nil), draft.Entities...),
		EpistemicStatus: epistemic,
		AlphaScore:      3.0,
		BetaScore:       1.0,
		Confidence:      0.75,
		Importance:      0.8,
		UtilityScore:    0.7,
		Version:         1,
		PrimarySourceID: sourceID,
		Clocks: memory.TriTemporalWindow{
			ObservedAt: now,
			ValidFrom:  now,
			CreatedAt:  now,
		},
		UpdatedAt: now,
	}

	derivedRels := make([]memory.MemoryRelation, 0, len(cluster.Episodes))
	consolidatedEps := make([]memory.Memory, 0, len(cluster.Episodes))

	err := s.uow.ExecTx(ctx, func(reg memory.RepositoryRegistry) error {
		if err := reg.Sources().InsertSource(ctx, provenance.MemorySource{
			ID:               sourceID,
			TenantID:         auth.TenantID,
			ProjectID:        cluster.ScopeTarget.ProjectID,
			SourceType:       provenance.SourceREMConsolidation,
			ExternalSourceID: "rem:" + newID.String(),
			ActorPrincipalID: &auth.PrincipalID,
			ActorAgentID:     auth.AgentID,
			CreatedAt:        now,
		}); err != nil {
			return err
		}

		if err := reg.Memories().InsertMemory(ctx, newMem); err != nil {
			return err
		}
		if err := reg.Memories().InsertMemoryVersion(ctx, memory.NewVersionFromMemory(newMem, "rem_consolidation", now)); err != nil {
			return err
		}

		for _, ep := range cluster.Episodes {
			rel := memory.MemoryRelation{
				ID:           uuid.New(),
				TenantID:     auth.TenantID,
				ProjectID:    cluster.ScopeTarget.ProjectID,
				FromMemoryID: newID,
				ToMemoryID:   ep.ID,
				RelationType: memory.RelationDerivedFrom,
				Weight:       1.0,
				SourceID:     &sourceID,
				ValidFrom:    now,
				IsActive:     true,
				CreatedAt:    now,
			}
			if err := reg.Memories().InsertMemoryRelation(ctx, rel); err != nil {
				return err
			}
			derivedRels = append(derivedRels, rel)

			if err := reg.Memories().CloseMemoryVersionValidity(ctx, ep.ID, ep.Version, now); err != nil {
				return err
			}
			ep.Status = memory.StatusConsolidated
			ep.Version++
			ep.UpdatedAt = now
			if err := reg.Memories().UpdateMemoryState(ctx, ep); err != nil {
				return err
			}
			if err := reg.Memories().InsertMemoryVersion(ctx, memory.NewVersionFromMemory(ep, "rem_consolidated", now)); err != nil {
				return err
			}
			consolidatedEps = append(consolidatedEps, ep)
		}

		if err := reg.Jobs().EnqueueMemoryJob(ctx, memory.MemoryJob{
			ID:          uuid.New(),
			TenantID:    auth.TenantID,
			MemoryID:    &newID,
			JobType:     "embed_and_nli",
			Priority:    50,
			Status:      "pending",
			MaxAttempts: 5,
			ScheduledAt: now,
			CreatedAt:   now,
		}); err != nil {
			return err
		}

		return reg.Audit().RecordAllowed(ctx, memory.AuditEvent{
			ID:            uuid.New(),
			TenantID:      &auth.TenantID,
			PrincipalID:   &auth.PrincipalID,
			AgentID:       auth.AgentID,
			Operation:     "rem_consolidate",
			ResourceType:  "memory",
			ResourceID:    &newID,
			ScopeUsed:     string(cluster.ScopeTarget.ScopeType),
			CorrelationID: cmd.CorrelationID,
			Outcome:       memory.AuditAllowed,
			Details:       map[string]any{"episodes": len(cluster.Episodes)},
			CreatedAt:     now,
		})
	})
	return newMem, derivedRels, consolidatedEps, err
}

func (s *Service) recordDenied(ctx context.Context, auth identity.AuthContext, cmd ConsolidateCommand, reason string, now time.Time) {
	_ = s.audit.RecordDenied(ctx, memory.AuditEvent{
		ID:            uuid.New(),
		TenantID:      &auth.TenantID,
		PrincipalID:   &auth.PrincipalID,
		AgentID:       auth.AgentID,
		Operation:     "rem_consolidate",
		ResourceType:  "project",
		ResourceID:    cmd.ProjectID,
		CorrelationID: cmd.CorrelationID,
		Outcome:       memory.AuditDenied,
		Details:       map[string]any{"reason": reason},
		CreatedAt:     now,
	})
}
