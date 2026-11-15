package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/romero-project/romero/internal/domain/memory"
	"github.com/romero-project/romero/internal/infrastructure/persistence/sqlcgen"
)

type jobQueueRepo struct {
	q *sqlcgen.Queries
}

// JobQueue returns a non-transactional JobQueueRepository backed by the Store pool.
func (s *Store) JobQueue() memory.JobQueueRepository {
	return &jobQueueRepo{q: s.queries}
}

func (r *jobQueueRepo) ClaimPendingMemoryJobs(ctx context.Context, jobType string, limit int, now time.Time) ([]memory.MemoryJob, error) {
	jobType = strings.TrimSpace(jobType)
	if jobType == "" {
		return nil, fmt.Errorf("%w: job_type is required", ErrInvalidInput)
	}
	if limit <= 0 {
		limit = 10
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}

	rows, err := r.q.ClaimPendingMemoryJobs(ctx, sqlcgen.ClaimPendingMemoryJobsParams{
		JobType:    jobType,
		NowTs:      &now,
		BatchLimit: int32(limit),
	})
	if err != nil {
		return nil, err
	}

	out := make([]memory.MemoryJob, 0, len(rows))
	for _, row := range rows {
		payload, uErr := unmarshalJSONMap(row.Payload)
		if uErr != nil {
			return nil, uErr
		}
		out = append(out, memory.MemoryJob{
			ID:          row.ID,
			TenantID:    row.TenantID,
			ProjectID:   fromNullUUID(row.ProjectID),
			MemoryID:    fromNullUUID(row.MemoryID),
			JobType:     row.JobType,
			Priority:    row.Priority,
			Status:      memory.JobStatus(row.Status),
			Payload:     payload,
			Attempts:    int(row.Attempts),
			MaxAttempts: int(row.MaxAttempts),
			LastError:   fromPgText(row.LastError),
			ScheduledAt: row.ScheduledAt,
			LockedAt:    row.LockedAt,
			CompletedAt: row.CompletedAt,
			CreatedAt:   row.CreatedAt,
		})
	}
	return out, nil
}

func (r *jobQueueRepo) CompleteMemoryJob(ctx context.Context, jobID uuid.UUID, completedAt time.Time) error {
	if jobID == uuid.Nil {
		return memory.ErrInvalidIdentifier
	}
	if completedAt.IsZero() {
		completedAt = time.Now().UTC()
	}
	return r.q.CompleteMemoryJob(ctx, sqlcgen.CompleteMemoryJobParams{
		ID:          jobID,
		CompletedAt: &completedAt,
	})
}

func (r *jobQueueRepo) FailMemoryJob(ctx context.Context, jobID uuid.UUID, lastError string, nextScheduledAt time.Time, deadLetter bool) error {
	if jobID == uuid.Nil {
		return memory.ErrInvalidIdentifier
	}
	if nextScheduledAt.IsZero() {
		nextScheduledAt = time.Now().UTC()
	}
	return r.q.FailMemoryJob(ctx, sqlcgen.FailMemoryJobParams{
		ID:              jobID,
		DeadLetter:      deadLetter,
		LastError:       strings.TrimSpace(lastError),
		NextScheduledAt: nextScheduledAt,
	})
}
