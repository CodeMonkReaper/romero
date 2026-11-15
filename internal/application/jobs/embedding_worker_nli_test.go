package jobs_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/romero-project/romero/internal/application/jobs"
	"github.com/romero-project/romero/internal/domain/identity"
	"github.com/romero-project/romero/internal/domain/memory"
)

type fakeUOW struct {
	reg memory.RepositoryRegistry
}

func (f *fakeUOW) ExecTx(_ context.Context, fn func(memory.RepositoryRegistry) error) error {
	return fn(f.reg)
}

type fakeRegistry struct {
	mems *fakeMemRepo
}

func (r *fakeRegistry) Sources() memory.SourceRepository    { return nil }
func (r *fakeRegistry) Memories() memory.MemoryRepository   { return r.mems }
func (r *fakeRegistry) Feedback() memory.FeedbackRepository { return nil }
func (r *fakeRegistry) Jobs() memory.JobRepository          { return nil }
func (r *fakeRegistry) Audit() memory.AuditLogger           { return nil }

type fakeMemRepo struct {
	items    map[uuid.UUID]memory.Memory
	rels     []memory.MemoryRelation
	versions []memory.MemoryVersion
	closed   []uuid.UUID
}

func (r *fakeMemRepo) GetMemoryByID(_ context.Context, tenantID, memoryID uuid.UUID) (*memory.Memory, error) {
	m, ok := r.items[memoryID]
	if !ok || m.TenantID != tenantID {
		return nil, memory.ErrNotFound
	}
	cp := m
	return &cp, nil
}
func (r *fakeMemRepo) GetMemoryByIdempotencyKey(context.Context, uuid.UUID, string) (*memory.Memory, error) {
	return nil, memory.ErrNotFound
}
func (r *fakeMemRepo) LockActiveTopicMemory(context.Context, identity.MemoryScopeTarget, string) (*memory.Memory, error) {
	return nil, memory.ErrNotFound
}
func (r *fakeMemRepo) InsertMemory(_ context.Context, m memory.Memory) error {
	r.items[m.ID] = m
	return nil
}
func (r *fakeMemRepo) UpdateMemoryState(_ context.Context, m memory.Memory) error {
	r.items[m.ID] = m
	return nil
}
func (r *fakeMemRepo) ListVersionsByMemoryID(context.Context, uuid.UUID) ([]memory.MemoryVersion, error) {
	return nil, nil
}
func (r *fakeMemRepo) InsertMemoryVersion(_ context.Context, v memory.MemoryVersion) error {
	r.versions = append(r.versions, v)
	return nil
}
func (r *fakeMemRepo) CloseMemoryVersionValidity(_ context.Context, memoryID uuid.UUID, _ int, _ time.Time) error {
	r.closed = append(r.closed, memoryID)
	return nil
}
func (r *fakeMemRepo) GetMemoryVersionPointInTime(context.Context, uuid.UUID, time.Time, time.Time) (*memory.MemoryVersion, error) {
	return nil, memory.ErrNotFound
}
func (r *fakeMemRepo) ListActiveRelationsByMemoryID(context.Context, uuid.UUID, uuid.UUID) ([]memory.MemoryRelation, error) {
	return nil, nil
}
func (r *fakeMemRepo) InsertMemoryRelation(_ context.Context, rel memory.MemoryRelation) error {
	r.rels = append(r.rels, rel)
	return nil
}

type fakeNeighborRepo struct {
	neighbors []memory.Memory
	err       error
}

func (f *fakeNeighborRepo) FindSameScopeSemanticNeighbors(context.Context, memory.Memory, []float32, float32, int, time.Time) ([]memory.Memory, error) {
	return f.neighbors, f.err
}
func (f *fakeNeighborRepo) DeactivateConflictsByMemoryID(context.Context, uuid.UUID, uuid.UUID, time.Time) ([]uuid.UUID, error) {
	return nil, nil
}
func (f *fakeNeighborRepo) HasActiveConflicts(context.Context, uuid.UUID, uuid.UUID, time.Time) (bool, error) {
	return false, nil
}

type fakeNLIProvider struct {
	scores []memory.NLIScore
	err    error
}

func (f *fakeNLIProvider) EvaluatePairs(context.Context, []memory.NLIPairInput) ([]memory.NLIScore, error) {
	return f.scores, f.err
}

type fakeNLINotifier struct {
	upserted []memory.Memory
	statuses []memory.MemoryStatus
	rels     []memory.MemoryRelation
}

func (n *fakeNLINotifier) ApplyMemoryUpsert(m memory.Memory) { n.upserted = append(n.upserted, m) }
func (n *fakeNLINotifier) ApplyMemoryStatusChange(_ uuid.UUID, _ *uuid.UUID, _ uuid.UUID, st memory.MemoryStatus, _ *time.Time) {
	n.statuses = append(n.statuses, st)
}
func (n *fakeNLINotifier) ApplyRelationsAdded(rels []memory.MemoryRelation) {
	n.rels = append(n.rels, rels...)
}
func (n *fakeNLINotifier) ApplyEmbeddingUpsert(uuid.UUID, *uuid.UUID, uuid.UUID, []float32) {}

func TestEmbeddingWorker_NLISupportAndContradiction(t *testing.T) {
	ctx := context.Background()
	tenantID := uuid.New()
	projID := uuid.New()
	srcID := uuid.New()
	now := time.Now().UTC()

	newMem := memory.Memory{
		ID: uuid.New(), TenantID: tenantID, ProjectID: &projID,
		Type: memory.TypeSemantic, ScopeType: identity.ScopeProjectShared, Status: memory.StatusActive,
		TopicKey: "k.new", Summary: "Postgres port 5433", Content: "Postgres port 5433",
		EpistemicStatus: memory.EpistemicReported, AlphaScore: 2, BetaScore: 1, Confidence: 0.66,
		Version: 1, PrimarySourceID: srcID, Clocks: memory.TriTemporalWindow{ObservedAt: now, ValidFrom: now, CreatedAt: now},
		UpdatedAt: now,
	}
	verifiedNb := memory.Memory{
		ID: uuid.New(), TenantID: tenantID, ProjectID: &projID,
		Type: memory.TypeSemantic, ScopeType: identity.ScopeProjectShared, Status: memory.StatusActive,
		TopicKey: "k.verified", Summary: "Postgres port 5432", Content: "Postgres port 5432",
		EpistemicStatus: memory.EpistemicVerified, AlphaScore: 4, BetaScore: 1, Confidence: 0.80,
		Version: 1, PrimarySourceID: srcID, Clocks: memory.TriTemporalWindow{ObservedAt: now, ValidFrom: now, CreatedAt: now},
		UpdatedAt: now,
	}
	supportNb := memory.Memory{
		ID: uuid.New(), TenantID: tenantID, ProjectID: &projID,
		Type: memory.TypeSemantic, ScopeType: identity.ScopeProjectShared, Status: memory.StatusActive,
		TopicKey: "k.support", Summary: "Postgres is primary DB", Content: "Postgres is primary DB",
		EpistemicStatus: memory.EpistemicObserved, AlphaScore: 2, BetaScore: 1, Confidence: 0.66,
		Version: 1, PrimarySourceID: srcID, Clocks: memory.TriTemporalWindow{ObservedAt: now, ValidFrom: now, CreatedAt: now},
		UpdatedAt: now,
	}

	memRepo := &fakeMemRepo{items: map[uuid.UUID]memory.Memory{
		newMem.ID:     newMem,
		verifiedNb.ID: verifiedNb,
		supportNb.ID:  supportNb,
	}}
	q := &fakeJobQueue{jobs: []memory.MemoryJob{{ID: uuid.New(), TenantID: tenantID, MemoryID: &newMem.ID, Attempts: 1, MaxAttempts: 3}}}
	search := &fakeSearchMem{mems: map[uuid.UUID]memory.Memory{newMem.ID: newMem}}
	embRepo := &fakeEmbedRepo{}
	notifier := &fakeNLINotifier{}

	w := jobs.NewEmbeddingWorker(q, search, embRepo, &fakeProvider{}, notifier, jobs.EmbeddingWorkerConfig{}, nil).
		WithNLI(
			&fakeUOW{reg: &fakeRegistry{mems: memRepo}},
			&fakeNeighborRepo{neighbors: []memory.Memory{verifiedNb, supportNb}},
			&fakeNLIProvider{scores: []memory.NLIScore{
				{Entailment: 0.02, Neutral: 0.06, Contradiction: 0.92}, // contradicts verifiedNb -> newMem becomes conflicted
				{Entailment: 0.90, Neutral: 0.08, Contradiction: 0.02}, // supports supportNb -> supportNb alpha += 0.5
			}},
		)

	n, err := w.ProcessOnce(ctx)
	if err != nil || n != 1 || len(q.completed) != 1 {
		t.Fatalf("ProcessOnce failed: n=%d err=%v completed=%v", n, err, q.completed)
	}
	if memRepo.items[newMem.ID].Status != memory.StatusConflicted || !memRepo.items[newMem.ID].NLIChecked {
		t.Fatalf("expected newMem conflicted & NLIChecked, got %+v", memRepo.items[newMem.ID])
	}
	if memRepo.items[verifiedNb.ID].Status != memory.StatusActive {
		t.Fatalf("verified neighbor must remain active, got %s", memRepo.items[verifiedNb.ID].Status)
	}
	if memRepo.items[supportNb.ID].AlphaScore != 2.5 {
		t.Fatalf("expected supportNb alpha=2.5, got %v", memRepo.items[supportNb.ID].AlphaScore)
	}
	if len(memRepo.rels) != 2 || len(notifier.rels) != 2 {
		t.Fatalf("expected 2 relations in DB and L1, got db=%d l1=%d", len(memRepo.rels), len(notifier.rels))
	}
}

func TestEmbeddingWorker_NLIFallbackLeavesUnchecked(t *testing.T) {
	ctx := context.Background()
	tenantID := uuid.New()
	memID := uuid.New()
	now := time.Now().UTC()
	mem := memory.Memory{
		ID: memID, TenantID: tenantID, Type: memory.TypeSemantic, ScopeType: identity.ScopeGlobalPublic,
		Status: memory.StatusActive, Summary: "s", Content: "c", PrimarySourceID: uuid.New(),
		Clocks: memory.TriTemporalWindow{ObservedAt: now, ValidFrom: now, CreatedAt: now},
	}
	memRepo := &fakeMemRepo{items: map[uuid.UUID]memory.Memory{memID: mem}}
	q := &fakeJobQueue{jobs: []memory.MemoryJob{{ID: uuid.New(), TenantID: tenantID, MemoryID: &memID, Attempts: 1, MaxAttempts: 3}}}
	w := jobs.NewEmbeddingWorker(q, &fakeSearchMem{mems: map[uuid.UUID]memory.Memory{memID: mem}}, &fakeEmbedRepo{}, &fakeProvider{}, nil, jobs.EmbeddingWorkerConfig{}, nil).
		WithNLI(
			&fakeUOW{reg: &fakeRegistry{mems: memRepo}},
			&fakeNeighborRepo{neighbors: []memory.Memory{mem}},
			&fakeNLIProvider{err: errors.New("nli down")},
		)

	n, err := w.ProcessOnce(ctx)
	if err != nil || n != 1 || len(q.completed) != 1 {
		t.Fatalf("expected job to complete cleanly on NLI failure: n=%d err=%v completed=%v", n, err, q.completed)
	}
	if memRepo.items[memID].NLIChecked {
		t.Fatalf("expected NLIChecked=false on NLI failure")
	}
}
