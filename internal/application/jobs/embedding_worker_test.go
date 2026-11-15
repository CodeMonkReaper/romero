package jobs_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/romero-project/romero/internal/application/jobs"
	"github.com/romero-project/romero/internal/domain/memory"
)

type fakeJobQueue struct {
	jobs       []memory.MemoryJob
	completed  []uuid.UUID
	failed     []uuid.UUID
	deadLetter []bool
}

func (f *fakeJobQueue) ClaimPendingMemoryJobs(ctx context.Context, jobType string, limit int, now time.Time) ([]memory.MemoryJob, error) {
	out := f.jobs
	f.jobs = nil
	return out, nil
}
func (f *fakeJobQueue) CompleteMemoryJob(ctx context.Context, jobID uuid.UUID, completedAt time.Time) error {
	f.completed = append(f.completed, jobID)
	return nil
}
func (f *fakeJobQueue) FailMemoryJob(ctx context.Context, jobID uuid.UUID, lastError string, nextScheduledAt time.Time, deadLetter bool) error {
	f.failed = append(f.failed, jobID)
	f.deadLetter = append(f.deadLetter, deadLetter)
	return nil
}

type fakeSearchMem struct {
	mems map[uuid.UUID]memory.Memory
}

func (f *fakeSearchMem) SearchByTrigram(context.Context, memory.SearchFilter) ([]uuid.UUID, error) {
	return nil, nil
}
func (f *fakeSearchMem) SearchByFTS(context.Context, memory.SearchFilter) ([]uuid.UUID, error) {
	return nil, nil
}
func (f *fakeSearchMem) SearchByVector(context.Context, memory.SearchFilter, []float32) ([]uuid.UUID, error) {
	return nil, nil
}
func (f *fakeSearchMem) GetMemoriesByIDs(ctx context.Context, tenantID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]memory.Memory, error) {
	return f.mems, nil
}
func (f *fakeSearchMem) LoadProjectGraphSnapshot(context.Context, uuid.UUID, *uuid.UUID, time.Time) ([]memory.Memory, []memory.MemoryRelation, map[uuid.UUID][]float32, error) {
	return nil, nil, nil, nil
}
func (f *fakeSearchMem) TraverseCausalSubgraph(context.Context, uuid.UUID, []uuid.UUID, int, time.Time) ([]memory.CausalWalkRow, error) {
	return nil, nil
}

type fakeEmbedRepo struct {
	upserted []memory.MemoryEmbedding
	err      error
}

func (f *fakeEmbedRepo) UpsertMemoryEmbedding(ctx context.Context, emb memory.MemoryEmbedding) error {
	if f.err != nil {
		return f.err
	}
	f.upserted = append(f.upserted, emb)
	return nil
}
func (f *fakeEmbedRepo) GetMemoryEmbeddingsByIDs(context.Context, uuid.UUID, []uuid.UUID) (map[uuid.UUID][]float32, error) {
	return nil, nil
}

type fakeProvider struct {
	err error
}

func (f *fakeProvider) ModelName() string { return "BAAI/bge-m3" }
func (f *fakeProvider) Dimensions() int   { return 1024 }
func (f *fakeProvider) Embed(ctx context.Context, text string) (memory.EmbeddingOutput, error) {
	if f.err != nil {
		return memory.EmbeddingOutput{}, f.err
	}
	v := make([]float32, 1024)
	v[0] = 1.0
	return memory.EmbeddingOutput{Dense: v}, nil
}
func (f *fakeProvider) EmbedBatch(ctx context.Context, texts []string) ([]memory.EmbeddingOutput, error) {
	return nil, nil
}

type fakeNotifier struct {
	upserted int
}

func (f *fakeNotifier) ApplyMemoryUpsert(memory.Memory) {}
func (f *fakeNotifier) ApplyMemoryStatusChange(uuid.UUID, *uuid.UUID, uuid.UUID, memory.MemoryStatus, *time.Time) {
}
func (f *fakeNotifier) ApplyRelationsAdded([]memory.MemoryRelation) {}
func (f *fakeNotifier) ApplyEmbeddingUpsert(uuid.UUID, *uuid.UUID, uuid.UUID, []float32) {
	f.upserted++
}

func TestEmbeddingWorker_ProcessOnce(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	memID := uuid.New()
	jobID := uuid.New()

	t.Run("completes job, persists embedding, and notifies L1", func(t *testing.T) {
		t.Parallel()
		q := &fakeJobQueue{jobs: []memory.MemoryJob{{ID: jobID, TenantID: tenantID, MemoryID: &memID, Attempts: 1, MaxAttempts: 5}}}
		sr := &fakeSearchMem{mems: map[uuid.UUID]memory.Memory{memID: {ID: memID, TenantID: tenantID, Summary: "sum", Content: "body"}}}
		er := &fakeEmbedRepo{}
		notif := &fakeNotifier{}
		w := jobs.NewEmbeddingWorker(q, sr, er, &fakeProvider{}, notif, jobs.EmbeddingWorkerConfig{}, nil)

		n, err := w.ProcessOnce(context.Background())
		if err != nil || n != 1 {
			t.Fatalf("ProcessOnce got n=%d err=%v, want 1 nil", n, err)
		}
		if len(q.completed) != 1 || len(er.upserted) != 1 || notif.upserted != 1 {
			t.Fatalf("expected completed=1 upserted=1 notif=1, got %d %d %d", len(q.completed), len(er.upserted), notif.upserted)
		}
	})

	t.Run("retries with backoff and dead-letters at max attempts", func(t *testing.T) {
		t.Parallel()
		q := &fakeJobQueue{jobs: []memory.MemoryJob{{ID: jobID, TenantID: tenantID, MemoryID: &memID, Attempts: 5, MaxAttempts: 5}}}
		sr := &fakeSearchMem{mems: map[uuid.UUID]memory.Memory{memID: {ID: memID, TenantID: tenantID, Summary: "sum", Content: "body"}}}
		w := jobs.NewEmbeddingWorker(q, sr, &fakeEmbedRepo{}, &fakeProvider{err: errors.New("down")}, nil, jobs.EmbeddingWorkerConfig{}, nil)

		if _, err := w.ProcessOnce(context.Background()); err != nil {
			t.Fatalf("ProcessOnce returned unexpected error: %v", err)
		}
		if len(q.failed) != 1 || !q.deadLetter[0] {
			t.Fatalf("expected 1 dead-lettered job, got failed=%v deadLetter=%v", q.failed, q.deadLetter)
		}
	})

	t.Run("incomplete deps, missing memory, and Run cancellation", func(t *testing.T) {
		t.Parallel()
		badW := jobs.NewEmbeddingWorker(nil, nil, nil, nil, nil, jobs.EmbeddingWorkerConfig{}, nil)
		if _, err := badW.ProcessOnce(context.Background()); err == nil {
			t.Fatal("expected error on incomplete dependencies")
		}

		q := &fakeJobQueue{jobs: []memory.MemoryJob{{ID: jobID, TenantID: tenantID, MemoryID: nil}}}
		sr := &fakeSearchMem{mems: map[uuid.UUID]memory.Memory{}}
		w := jobs.NewEmbeddingWorker(q, sr, &fakeEmbedRepo{}, &fakeProvider{}, nil, jobs.EmbeddingWorkerConfig{PollInterval: 5 * time.Millisecond}, nil)
		if n, err := w.ProcessOnce(context.Background()); err != nil || n != 1 || len(q.failed) != 1 || !q.deadLetter[0] {
			t.Fatalf("expected dead-letter for nil memory_id, got n=%d err=%v failed=%v", n, err, q.failed)
		}

		q2 := &fakeJobQueue{jobs: []memory.MemoryJob{{ID: jobID, TenantID: tenantID, MemoryID: &memID, Attempts: 1, MaxAttempts: 5}}}
		sr2 := &fakeSearchMem{mems: map[uuid.UUID]memory.Memory{memID: {ID: memID, TenantID: tenantID, Summary: "sum", Content: "body"}}}
		w2 := jobs.NewEmbeddingWorker(q2, sr2, &fakeEmbedRepo{err: errors.New("db err")}, &fakeProvider{}, nil, jobs.EmbeddingWorkerConfig{}, nil)
		if n, err := w2.ProcessOnce(context.Background()); err != nil || n != 1 || len(q2.failed) != 1 || q2.deadLetter[0] {
			t.Fatalf("expected non-dead-letter retry on upsert err, got n=%d err=%v failed=%v", n, err, q2.failed)
		}

		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			w.Run(ctx)
			close(done)
		}()
		time.Sleep(15 * time.Millisecond)
		cancel()
		<-done
	})
}
