// Package jobs implements asynchronous priority workers for cognitive enrichment.
package jobs

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/romero-project/romero/internal/domain/memory"
)

const (
	defaultJobType           = "embed_and_nli"
	defaultBatchSize         = 16
	defaultPollInterval      = 250 * time.Millisecond
	defaultBaseBackoff       = 2 * time.Second
	defaultNLINeighborMinSim = float32(0.82)
	defaultNLINeighborLimit  = 5
)

// EmbeddingWorkerConfig tunes polling, batch size, and retry backoff for EmbeddingWorker.
type EmbeddingWorkerConfig struct {
	JobType      string
	BatchSize    int
	PollInterval time.Duration
	BaseBackoff  time.Duration
}

// EmbeddingWorker processes priority embed_and_nli jobs, persists halfvec(1024), and syncs L1.
type EmbeddingWorker struct {
	queue      memory.JobQueueRepository
	memories   memory.SearchRepository
	embeddings memory.EmbeddingRepository
	provider   memory.EmbeddingProvider
	notifier   memory.GraphMutationNotifier
	uow        memory.UnitOfWork
	neighbors  memory.SemanticNeighborRepository
	nli        memory.NLIProvider
	cfg        EmbeddingWorkerConfig
	logger     *slog.Logger
}

// NewEmbeddingWorker constructs an EmbeddingWorker.
func NewEmbeddingWorker(
	queue memory.JobQueueRepository,
	memories memory.SearchRepository,
	embeddings memory.EmbeddingRepository,
	provider memory.EmbeddingProvider,
	notifier memory.GraphMutationNotifier,
	cfg EmbeddingWorkerConfig,
	logger *slog.Logger,
) *EmbeddingWorker {
	if strings.TrimSpace(cfg.JobType) == "" {
		cfg.JobType = defaultJobType
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = defaultBatchSize
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = defaultPollInterval
	}
	if cfg.BaseBackoff <= 0 {
		cfg.BaseBackoff = defaultBaseBackoff
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &EmbeddingWorker{
		queue:      queue,
		memories:   memories,
		embeddings: embeddings,
		provider:   provider,
		notifier:   notifier,
		cfg:        cfg,
		logger:     logger,
	}
}

// WithNLI attaches optional UnitOfWork, SemanticNeighborRepository, and NLIProvider
// to enable asynchronous NLI neighbor evaluation after embedding persistence.
func (w *EmbeddingWorker) WithNLI(
	uow memory.UnitOfWork,
	neighbors memory.SemanticNeighborRepository,
	nli memory.NLIProvider,
) *EmbeddingWorker {
	w.uow = uow
	w.neighbors = neighbors
	w.nli = nli
	return w
}

// ProcessOnce claims and processes up to BatchSize pending embed_and_nli jobs.
func (w *EmbeddingWorker) ProcessOnce(ctx context.Context) (int, error) {
	if w.queue == nil || w.memories == nil || w.embeddings == nil || w.provider == nil {
		return 0, errors.New("embedding worker dependencies are incomplete")
	}

	now := time.Now().UTC()
	claimed, err := w.queue.ClaimPendingMemoryJobs(ctx, w.cfg.JobType, w.cfg.BatchSize, now)
	if err != nil {
		return 0, err
	}
	if len(claimed) == 0 {
		return 0, nil
	}

	byTenant := make(map[uuid.UUID][]uuid.UUID)
	for _, job := range claimed {
		if job.MemoryID != nil && *job.MemoryID != uuid.Nil {
			byTenant[job.TenantID] = append(byTenant[job.TenantID], *job.MemoryID)
		}
	}

	memMap := make(map[uuid.UUID]memory.Memory, len(claimed))
	for tenantID, ids := range byTenant {
		mems, mErr := w.memories.GetMemoriesByIDs(ctx, tenantID, ids)
		if mErr != nil {
			for _, job := range claimed {
				if job.TenantID == tenantID {
					w.failWithBackoff(ctx, job, mErr, now)
				}
			}
			continue
		}
		for id, m := range mems {
			memMap[id] = m
		}
	}

	processed := 0
	for _, job := range claimed {
		processed++
		if job.MemoryID == nil || *job.MemoryID == uuid.Nil {
			_ = w.queue.FailMemoryJob(ctx, job.ID, "missing memory_id", now, true)
			continue
		}
		mem, ok := memMap[*job.MemoryID]
		if !ok {
			_ = w.queue.FailMemoryJob(ctx, job.ID, "memory not found", now, true)
			continue
		}

		text := strings.TrimSpace(mem.Summary + "\n" + mem.Content)
		embOut, embErr := w.provider.Embed(ctx, text)
		if embErr != nil {
			w.failWithBackoff(ctx, job, embErr, now)
			continue
		}

		if err := w.embeddings.UpsertMemoryEmbedding(ctx, memory.MemoryEmbedding{
			MemoryID:  mem.ID,
			ModelName: w.provider.ModelName(),
			DenseVec:  embOut.Dense,
			UpdatedAt: now,
		}); err != nil {
			w.failWithBackoff(ctx, job, err, now)
			continue
		}

		if w.notifier != nil {
			w.notifier.ApplyEmbeddingUpsert(mem.TenantID, mem.ProjectID, mem.ID, embOut.Dense)
		}
		if w.nli != nil && w.neighbors != nil && w.uow != nil {
			w.evaluateNLINeighbors(ctx, mem, text, embOut.Dense, now)
		}
		if err := w.queue.CompleteMemoryJob(ctx, job.ID, time.Now().UTC()); err != nil {
			w.logger.Warn("complete memory job failed", slog.String("job_id", job.ID.String()), slog.Any("error", err))
		}
	}
	return processed, nil
}

func (w *EmbeddingWorker) failWithBackoff(ctx context.Context, job memory.MemoryJob, cause error, now time.Time) {
	shift := job.Attempts - 1
	if shift < 0 {
		shift = 0
	}
	if shift > 6 {
		shift = 6
	}
	backoff := w.cfg.BaseBackoff * time.Duration(1<<shift)
	deadLetter := job.MaxAttempts > 0 && job.Attempts >= job.MaxAttempts
	if err := w.queue.FailMemoryJob(ctx, job.ID, cause.Error(), now.Add(backoff), deadLetter); err != nil {
		w.logger.Warn("fail memory job update failed", slog.String("job_id", job.ID.String()), slog.Any("error", err))
	}
}

// Run polls and processes embedding jobs until ctx is cancelled.
func (w *EmbeddingWorker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.cfg.PollInterval)
	defer ticker.Stop()

	for {
		if _, err := w.ProcessOnce(ctx); err != nil && ctx.Err() == nil {
			w.logger.Warn("embedding worker cycle failed", slog.Any("error", err))
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (w *EmbeddingWorker) evaluateNLINeighbors(ctx context.Context, mem memory.Memory, memText string, denseVec []float32, now time.Time) {
	neighbors, err := w.neighbors.FindSameScopeSemanticNeighbors(
		ctx,
		mem,
		denseVec,
		defaultNLINeighborMinSim,
		defaultNLINeighborLimit,
		now,
	)
	if err != nil {
		w.logger.Warn("nli neighbor lookup failed", slog.String("memory_id", mem.ID.String()), slog.Any("error", err))
		return
	}

	var scores []memory.NLIScore
	if len(neighbors) > 0 {
		pairs := make([]memory.NLIPairInput, len(neighbors))
		for i, nb := range neighbors {
			pairs[i] = memory.NLIPairInput{
				Premise:    strings.TrimSpace(nb.Summary + "\n" + nb.Content),
				Hypothesis: memText,
			}
		}
		scores, err = w.nli.EvaluatePairs(ctx, pairs)
		if err != nil || len(scores) != len(neighbors) {
			w.logger.Warn("nli pair evaluation skipped", slog.String("memory_id", mem.ID.String()), slog.Any("error", err))
			return
		}
	}

	var addedRels []memory.MemoryRelation
	var upsertedMems []memory.Memory
	var statusChanges []memory.Memory

	txErr := w.uow.ExecTx(ctx, func(reg memory.RepositoryRegistry) error {
		curNew, err := reg.Memories().GetMemoryByID(ctx, mem.TenantID, mem.ID)
		if err != nil {
			return err
		}
		for i, nb := range neighbors {
			score := scores[i]
			dec := memory.EvaluateNLIDecision(*curNew, nb, score)
			ent := score.Entailment
			con := score.Contradiction

			switch dec.Action {
			case memory.NLIDecisionSupports:
				curNb, err := reg.Memories().GetMemoryByID(ctx, nb.TenantID, nb.ID)
				if err != nil {
					return err
				}
				curNb.AlphaScore += dec.AlphaDelta
				if curNb.AlphaScore+curNb.BetaScore > 0 {
					curNb.Confidence = curNb.AlphaScore / (curNb.AlphaScore + curNb.BetaScore)
				}
				curNb.UpdatedAt = now
				if err := reg.Memories().UpdateMemoryState(ctx, *curNb); err != nil {
					return err
				}
				upsertedMems = append(upsertedMems, *curNb)

				rel := memory.MemoryRelation{
					ID:               uuid.New(),
					TenantID:         curNew.TenantID,
					ProjectID:        curNew.ProjectID,
					FromMemoryID:     curNew.ID,
					ToMemoryID:       curNb.ID,
					RelationType:     memory.RelationSupports,
					Weight:           score.Entailment,
					NLIEntailment:    &ent,
					NLIContradiction: &con,
					SourceID:         &curNew.PrimarySourceID,
					ValidFrom:        now,
					IsActive:         true,
					CreatedAt:        now,
				}
				if err := reg.Memories().InsertMemoryRelation(ctx, rel); err != nil {
					return err
				}
				addedRels = append(addedRels, rel)

			case memory.NLIDecisionContradicts:
				rel := memory.MemoryRelation{
					ID:               uuid.New(),
					TenantID:         curNew.TenantID,
					ProjectID:        curNew.ProjectID,
					FromMemoryID:     curNew.ID,
					ToMemoryID:       nb.ID,
					RelationType:     memory.RelationContradicts,
					Weight:           score.Contradiction,
					NLIEntailment:    &ent,
					NLIContradiction: &con,
					SourceID:         &curNew.PrimarySourceID,
					ValidFrom:        now,
					IsActive:         true,
					CreatedAt:        now,
				}
				if err := reg.Memories().InsertMemoryRelation(ctx, rel); err != nil {
					return err
				}
				addedRels = append(addedRels, rel)

				if dec.ConflictedMemoryID != nil {
					target := curNew
					if *dec.ConflictedMemoryID != curNew.ID {
						target, err = reg.Memories().GetMemoryByID(ctx, nb.TenantID, *dec.ConflictedMemoryID)
						if err != nil {
							return err
						}
					}
					if target.EpistemicStatus != memory.EpistemicVerified && target.Status == memory.StatusActive {
						if err := markConflictedInTx(ctx, reg, target, now); err != nil {
							return err
						}
						statusChanges = append(statusChanges, *target)
					}
				}
			}
		}

		curNew.NLIChecked = true
		curNew.UpdatedAt = now
		if err := reg.Memories().UpdateMemoryState(ctx, *curNew); err != nil {
			return err
		}
		upsertedMems = append(upsertedMems, *curNew)
		return nil
	})
	if txErr != nil {
		w.logger.Warn("nli transaction failed", slog.String("memory_id", mem.ID.String()), slog.Any("error", txErr))
		return
	}

	if w.notifier != nil {
		for _, m := range upsertedMems {
			w.notifier.ApplyMemoryUpsert(m)
		}
		for _, m := range statusChanges {
			w.notifier.ApplyMemoryStatusChange(m.TenantID, m.ProjectID, m.ID, m.Status, m.Clocks.ValidUntil)
		}
		if len(addedRels) > 0 {
			w.notifier.ApplyRelationsAdded(addedRels)
		}
	}
}

func markConflictedInTx(ctx context.Context, reg memory.RepositoryRegistry, target *memory.Memory, now time.Time) error {
	if err := reg.Memories().CloseMemoryVersionValidity(ctx, target.ID, target.Version, now); err != nil {
		return err
	}
	target.Status = memory.StatusConflicted
	target.Version++
	target.UpdatedAt = now
	if err := reg.Memories().UpdateMemoryState(ctx, *target); err != nil {
		return err
	}
	return reg.Memories().InsertMemoryVersion(ctx, memory.NewVersionFromMemory(*target, "nli_contradiction", now))
}
