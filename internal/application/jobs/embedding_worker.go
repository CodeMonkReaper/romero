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
	defaultJobType      = "embed_and_nli"
	defaultBatchSize    = 16
	defaultPollInterval = 250 * time.Millisecond
	defaultBaseBackoff  = 2 * time.Second
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
