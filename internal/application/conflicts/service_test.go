package conflicts_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/romero-project/romero/internal/application/conflicts"
	"github.com/romero-project/romero/internal/domain/identity"
	"github.com/romero-project/romero/internal/domain/memory"
)

type fakeUOW struct{ reg memory.RepositoryRegistry }

func (f *fakeUOW) ExecTx(_ context.Context, fn func(memory.RepositoryRegistry) error) error {
	return fn(f.reg)
}

type fakeReg struct {
	mems  *fakeMemRepo
	confs *fakeConflictRepo
	audit *fakeAudit
}

func (r *fakeReg) Sources() memory.SourceRepository     { return nil }
func (r *fakeReg) Memories() memory.MemoryRepository    { return r.mems }
func (r *fakeReg) Feedback() memory.FeedbackRepository  { return nil }
func (r *fakeReg) Jobs() memory.JobRepository           { return nil }
func (r *fakeReg) Audit() memory.AuditLogger            { return r.audit }
func (r *fakeReg) Conflicts() memory.ConflictRepository { return r.confs }

type fakeMemRepo struct {
	items    map[uuid.UUID]memory.Memory
	rels     []memory.MemoryRelation
	versions []memory.MemoryVersion
}

func (r *fakeMemRepo) GetMemoryByID(context.Context, uuid.UUID, uuid.UUID) (*memory.Memory, error) {
	return nil, memory.ErrNotFound
}
func (r *fakeMemRepo) GetMemoryByIdempotencyKey(context.Context, uuid.UUID, string) (*memory.Memory, error) {
	return nil, memory.ErrNotFound
}
func (r *fakeMemRepo) LockActiveTopicMemory(context.Context, identity.MemoryScopeTarget, string) (*memory.Memory, error) {
	return nil, memory.ErrNotFound
}
func (r *fakeMemRepo) InsertMemory(context.Context, memory.Memory) error { return nil }
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
func (r *fakeMemRepo) CloseMemoryVersionValidity(context.Context, uuid.UUID, int, time.Time) error {
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

type fakeConflictRepo struct {
	pairs       []memory.ConflictPairRow
	deactivated []uuid.UUID
}

func (f *fakeConflictRepo) ListActiveConflicts(context.Context, uuid.UUID, *uuid.UUID, int, int, time.Time) ([]memory.ConflictPairRow, error) {
	return f.pairs, nil
}
func (f *fakeConflictRepo) GetConflictForUpdate(_ context.Context, tenantID, conflictID uuid.UUID) (*memory.ConflictPairRow, error) {
	for _, p := range f.pairs {
		if p.Relation.ID == conflictID && p.Relation.TenantID == tenantID && p.Relation.IsActive {
			cp := p
			return &cp, nil
		}
	}
	return nil, memory.ErrNotFound
}
func (f *fakeConflictRepo) DeactivateConflictEdge(_ context.Context, _ uuid.UUID, conflictID uuid.UUID, _ time.Time) error {
	f.deactivated = append(f.deactivated, conflictID)
	return nil
}

type fakeAudit struct{ allowed, denied int }

func (a *fakeAudit) RecordAllowed(context.Context, memory.AuditEvent) error { a.allowed++; return nil }
func (a *fakeAudit) RecordDenied(context.Context, memory.AuditEvent) error  { a.denied++; return nil }

type fakePerms struct{ read, write bool }

func (p fakePerms) HasTaskPermission(_ context.Context, q identity.TaskPermissionQuery) (bool, error) {
	if q.Permission == identity.TaskRead {
		return p.read, nil
	}
	return p.write, nil
}

type fakeL1 struct {
	upserts     int
	statuses    int
	added       int
	deactivated int
}

func (l *fakeL1) ApplyMemoryUpsert(memory.Memory) { l.upserts++ }
func (l *fakeL1) ApplyMemoryStatusChange(uuid.UUID, *uuid.UUID, uuid.UUID, memory.MemoryStatus, *time.Time) {
	l.statuses++
}
func (l *fakeL1) ApplyRelationsAdded([]memory.MemoryRelation)                      { l.added++ }
func (l *fakeL1) ApplyEmbeddingUpsert(uuid.UUID, *uuid.UUID, uuid.UUID, []float32) {}
func (l *fakeL1) ApplyRelationDeactivated(uuid.UUID, *uuid.UUID, uuid.UUID)        { l.deactivated++ }

func TestListAndReviewConflict_ZeroTrustAndAtomicResolution(t *testing.T) {
	ctx := context.Background()
	tenantID, projID, agentA, agentB := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	now := time.Now().UTC()

	m1 := memory.Memory{
		ID: uuid.New(), TenantID: tenantID, ProjectID: &projID,
		Type: memory.TypeSemantic, ScopeType: identity.ScopeProjectShared, Status: memory.StatusActive,
		EpistemicStatus: memory.EpistemicObserved, AlphaScore: 2, BetaScore: 1, Confidence: 0.66, Version: 1,
		PrimarySourceID: uuid.New(), Clocks: memory.TriTemporalWindow{ObservedAt: now, ValidFrom: now, CreatedAt: now},
	}
	m2 := m1
	m2.ID = uuid.New()
	m2.Status = memory.StatusConflicted

	mPriv := m1
	mPriv.ID = uuid.New()
	mPriv.ScopeType = identity.ScopeAgentPrivate
	mPriv.OwnerAgentID = &agentB

	cVisible := memory.ConflictPairRow{
		Relation:   memory.MemoryRelation{ID: uuid.New(), TenantID: tenantID, ProjectID: &projID, FromMemoryID: m1.ID, ToMemoryID: m2.ID, RelationType: memory.RelationContradicts, Weight: 0.9, IsActive: true, ValidFrom: now, CreatedAt: now},
		FromMemory: m1,
		ToMemory:   m2,
	}
	cHidden := memory.ConflictPairRow{
		Relation:   memory.MemoryRelation{ID: uuid.New(), TenantID: tenantID, ProjectID: &projID, FromMemoryID: m1.ID, ToMemoryID: mPriv.ID, RelationType: memory.RelationContradicts, Weight: 0.9, IsActive: true, ValidFrom: now, CreatedAt: now},
		FromMemory: m1,
		ToMemory:   mPriv,
	}

	memRepo := &fakeMemRepo{items: map[uuid.UUID]memory.Memory{m1.ID: m1, m2.ID: m2, mPriv.ID: mPriv}}
	confRepo := &fakeConflictRepo{pairs: []memory.ConflictPairRow{cVisible, cHidden}}
	audit := &fakeAudit{}
	l1 := &fakeL1{}
	svc, err := conflicts.NewService(&fakeUOW{reg: &fakeReg{mems: memRepo, confs: confRepo, audit: audit}}, confRepo, audit, fakePerms{read: true, write: true}, l1, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}

	caller := identity.AuthContext{
		TenantID: tenantID, PrincipalID: uuid.New(), PrincipalType: identity.PrincipalAgent, AgentID: &agentA,
		AllowedProjects: []uuid.UUID{projID},
		AllowedScopes:   []identity.ScopeType{identity.ScopeProjectShared, identity.ScopeAgentPrivate},
	}

	// ListConflicts must return only cVisible (0.00% leakage of agentB's private endpoint).
	listed, err := svc.ListConflicts(ctx, caller, conflicts.ListConflictsFilter{ProjectID: &projID})
	if err != nil || len(listed) != 1 || listed[0].ConflictID != cVisible.Relation.ID {
		t.Fatalf("expected 1 visible conflict, got %+v (err=%v)", listed, err)
	}

	// Reviewing cHidden must return 404 opaque + audit denied.
	if _, err := svc.ReviewConflict(ctx, caller, conflicts.ReviewConflictCommand{ConflictID: cHidden.Relation.ID, WinnerMemoryID: m1.ID}); !errors.Is(err, conflicts.ErrNotFound) || audit.denied != 1 {
		t.Fatalf("expected ErrNotFound + denied audit, got err=%v denied=%d", err, audit.denied)
	}

	// Reviewing unknown conflict ID must also return ErrNotFound + audit denied (404 opacity).
	if _, err := svc.ReviewConflict(ctx, caller, conflicts.ReviewConflictCommand{ConflictID: uuid.New(), WinnerMemoryID: m1.ID}); !errors.Is(err, conflicts.ErrNotFound) || audit.denied != 2 {
		t.Fatalf("expected ErrNotFound + denied audit=2, got err=%v denied=%d", err, audit.denied)
	}

	// Invalid winner_memory_id must return ErrInvalidWinner (422).
	if _, err := svc.ReviewConflict(ctx, caller, conflicts.ReviewConflictCommand{ConflictID: cVisible.Relation.ID, WinnerMemoryID: uuid.New()}); !errors.Is(err, conflicts.ErrInvalidWinner) {
		t.Fatalf("expected ErrInvalidWinner, got %v", err)
	}

	// Valid resolution leaves winner active (+1.0 alpha), loser superseded, edge deactivated, supersedes added, L1 synced.
	res, err := svc.ReviewConflict(ctx, caller, conflicts.ReviewConflictCommand{ConflictID: cVisible.Relation.ID, WinnerMemoryID: m1.ID, Reason: "confirmed"})
	if err != nil {
		t.Fatalf("ReviewConflict failed: %v", err)
	}
	if res.IsActive || memRepo.items[m1.ID].AlphaScore != 3.0 || memRepo.items[m2.ID].Status != memory.StatusSuperseded {
		t.Fatalf("unexpected resolution state: res=%+v m1=%+v m2=%+v", res, memRepo.items[m1.ID], memRepo.items[m2.ID])
	}
	if len(confRepo.deactivated) != 1 || len(memRepo.rels) != 1 || l1.deactivated != 1 || l1.added != 1 {
		t.Fatalf("expected edge deactivation, supersedes relation, and L1 sync")
	}
}

func TestReviewConflict_VerifiedLoserRequiresAdmin(t *testing.T) {
	ctx := context.Background()
	tenantID, projID := uuid.New(), uuid.New()
	now := time.Now().UTC()

	m1 := memory.Memory{ID: uuid.New(), TenantID: tenantID, ProjectID: &projID, Type: memory.TypeSemantic, ScopeType: identity.ScopeProjectShared, Status: memory.StatusConflicted, EpistemicStatus: memory.EpistemicReported, AlphaScore: 2, BetaScore: 1, PrimarySourceID: uuid.New(), Clocks: memory.TriTemporalWindow{ObservedAt: now, ValidFrom: now, CreatedAt: now}}
	mVerified := m1
	mVerified.ID = uuid.New()
	mVerified.Status = memory.StatusActive
	mVerified.EpistemicStatus = memory.EpistemicVerified

	pair := memory.ConflictPairRow{
		Relation:   memory.MemoryRelation{ID: uuid.New(), TenantID: tenantID, ProjectID: &projID, FromMemoryID: m1.ID, ToMemoryID: mVerified.ID, RelationType: memory.RelationContradicts, IsActive: true, ValidFrom: now, CreatedAt: now},
		FromMemory: m1,
		ToMemory:   mVerified,
	}
	confRepo := &fakeConflictRepo{pairs: []memory.ConflictPairRow{pair}}
	audit := &fakeAudit{}
	svc, _ := conflicts.NewService(&fakeUOW{reg: &fakeReg{mems: &fakeMemRepo{items: map[uuid.UUID]memory.Memory{}}, confs: confRepo, audit: audit}}, confRepo, audit, nil, nil, func() time.Time { return now })

	nonAdmin := identity.AuthContext{TenantID: tenantID, PrincipalID: uuid.New(), PrincipalType: identity.PrincipalAgent, AllowedProjects: []uuid.UUID{projID}, AllowedScopes: []identity.ScopeType{identity.ScopeProjectShared}}
	if _, err := svc.ReviewConflict(ctx, nonAdmin, conflicts.ReviewConflictCommand{ConflictID: pair.Relation.ID, WinnerMemoryID: m1.ID}); !errors.Is(err, conflicts.ErrForbidden) {
		t.Fatalf("expected ErrForbidden when non-admin defeats verified memory, got %v", err)
	}
}

func TestConflicts_EdgeCasesAndTaskPermissions(t *testing.T) {
	ctx := context.Background()
	tenantID, projID := uuid.New(), uuid.New()
	now := time.Now().UTC()

	if _, err := conflicts.NewService(nil, &fakeConflictRepo{}, &fakeAudit{}, nil, nil, nil); err == nil {
		t.Fatalf("expected error on nil deps")
	}

	mTask1 := memory.Memory{ID: uuid.New(), TenantID: tenantID, ProjectID: &projID, TaskID: "t1", Type: memory.TypeSemantic, ScopeType: identity.ScopeTaskShared, Status: memory.StatusActive, EpistemicStatus: memory.EpistemicObserved, AlphaScore: 2, BetaScore: 1, PrimarySourceID: uuid.New(), Clocks: memory.TriTemporalWindow{ObservedAt: now, ValidFrom: now, CreatedAt: now}}
	mTask2 := mTask1
	mTask2.ID = uuid.New()
	mTask2.Status = memory.StatusConflicted

	pair := memory.ConflictPairRow{
		Relation:   memory.MemoryRelation{ID: uuid.New(), TenantID: tenantID, ProjectID: &projID, FromMemoryID: mTask1.ID, ToMemoryID: mTask2.ID, RelationType: memory.RelationContradicts, IsActive: true, ValidFrom: now, CreatedAt: now},
		FromMemory: mTask1,
		ToMemory:   mTask2,
	}
	confRepo := &fakeConflictRepo{pairs: []memory.ConflictPairRow{pair}}
	memRepo := &fakeMemRepo{items: map[uuid.UUID]memory.Memory{mTask1.ID: mTask1, mTask2.ID: mTask2}}
	audit := &fakeAudit{}

	// Read allowed, write denied on task_shared -> ListConflicts shows it, ReviewConflict returns ErrForbidden.
	svcReadOnly, _ := conflicts.NewService(&fakeUOW{reg: &fakeReg{mems: memRepo, confs: confRepo, audit: audit}}, confRepo, audit, fakePerms{read: true, write: false}, nil, nil)
	agentID := uuid.New()
	caller := identity.AuthContext{TenantID: tenantID, PrincipalID: uuid.New(), PrincipalType: identity.PrincipalAgent, AgentID: &agentID, AllowedProjects: []uuid.UUID{projID}, AllowedScopes: []identity.ScopeType{identity.ScopeTaskShared}}

	if _, err := svcReadOnly.ListConflicts(ctx, identity.AuthContext{}, conflicts.ListConflictsFilter{}); !errors.Is(err, conflicts.ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized, got %v", err)
	}
	otherProj := uuid.New()
	if got, err := svcReadOnly.ListConflicts(ctx, caller, conflicts.ListConflictsFilter{ProjectID: &otherProj}); err != nil || len(got) != 0 {
		t.Fatalf("expected empty list for non-allowed project, got %+v err=%v", got, err)
	}
	if got, err := svcReadOnly.ListConflicts(ctx, caller, conflicts.ListConflictsFilter{Limit: 999, Offset: -1}); err != nil || len(got) != 1 {
		t.Fatalf("expected 1 task_shared conflict, got %+v err=%v", got, err)
	}
	if _, err := svcReadOnly.ReviewConflict(ctx, caller, conflicts.ReviewConflictCommand{}); !errors.Is(err, conflicts.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
	if _, err := svcReadOnly.ReviewConflict(ctx, caller, conflicts.ReviewConflictCommand{ConflictID: pair.Relation.ID, WinnerMemoryID: mTask2.ID}); !errors.Is(err, conflicts.ErrForbidden) {
		t.Fatalf("expected ErrForbidden when task write denied, got %v", err)
	}

	// Read+write allowed with ToMemory as winner.
	svcRW, _ := conflicts.NewService(&fakeUOW{reg: &fakeReg{mems: memRepo, confs: confRepo, audit: audit}}, confRepo, audit, fakePerms{read: true, write: true}, nil, nil)
	res, err := svcRW.ReviewConflict(ctx, caller, conflicts.ReviewConflictCommand{ConflictID: pair.Relation.ID, WinnerMemoryID: mTask2.ID})
	if err != nil || res.ToMemory.Status != memory.StatusActive || res.FromMemory.Status != memory.StatusSuperseded {
		t.Fatalf("expected ToMemory winner resolution, got res=%+v err=%v", res, err)
	}
}

func TestReviewConflict_MergedSummaryAndContent(t *testing.T) {
	ctx := context.Background()
	tenantID, projID := uuid.New(), uuid.New()
	now := time.Now().UTC()

	m1 := memory.Memory{
		ID: uuid.New(), TenantID: tenantID, ProjectID: &projID,
		Type: memory.TypeSemantic, ScopeType: identity.ScopeProjectShared, Status: memory.StatusActive,
		EpistemicStatus: memory.EpistemicObserved, Summary: "Old summary", Content: "Old content",
		AlphaScore: 2, BetaScore: 1, PrimarySourceID: uuid.New(), Version: 1,
		Clocks: memory.TriTemporalWindow{ObservedAt: now, ValidFrom: now, CreatedAt: now},
	}
	m2 := m1
	m2.ID = uuid.New()
	m2.Summary = "Conflicting summary"
	m2.Content = "Conflicting content"

	pair := memory.ConflictPairRow{
		Relation:   memory.MemoryRelation{ID: uuid.New(), TenantID: tenantID, ProjectID: &projID, FromMemoryID: m1.ID, ToMemoryID: m2.ID, RelationType: memory.RelationContradicts, IsActive: true, ValidFrom: now, CreatedAt: now},
		FromMemory: m1,
		ToMemory:   m2,
	}

	// Whitespace-only merged summary fails validation without closing conflict.
	memRepo := &fakeMemRepo{items: map[uuid.UUID]memory.Memory{m1.ID: m1, m2.ID: m2}}
	confRepo := &fakeConflictRepo{pairs: []memory.ConflictPairRow{pair}}
	svc, _ := conflicts.NewService(&fakeUOW{reg: &fakeReg{mems: memRepo, confs: confRepo, audit: &fakeAudit{}}}, confRepo, &fakeAudit{}, nil, nil, nil)
	admin := identity.AuthContext{
		TenantID:        tenantID,
		PrincipalID:     uuid.New(),
		PrincipalType:   identity.PrincipalUser,
		IsAdmin:         true,
		AllowedProjects: []uuid.UUID{projID},
		AllowedScopes:   []identity.ScopeType{identity.ScopeProjectShared},
	}

	if _, err := svc.ReviewConflict(ctx, admin, conflicts.ReviewConflictCommand{
		ConflictID:     pair.Relation.ID,
		WinnerMemoryID: m1.ID,
		Summary:        "   ",
	}); !errors.Is(err, conflicts.ErrValidation) {
		t.Fatalf("expected ErrValidation for whitespace summary, got %v", err)
	}

	mergedSummary := "Merged Postgres port 54329"
	mergedContent := "Use Postgres on port 54329 for local verification."
	res, err := svc.ReviewConflict(ctx, admin, conflicts.ReviewConflictCommand{
		ConflictID:     pair.Relation.ID,
		WinnerMemoryID: m1.ID,
		Summary:        mergedSummary,
		Content:        mergedContent,
		Reason:         "merge both claims",
	})
	if err != nil {
		t.Fatalf("ReviewConflict merged failed: %v", err)
	}
	if res.FromMemory.Summary != mergedSummary {
		t.Fatalf("expected updated view summary %q, got %q", mergedSummary, res.FromMemory.Summary)
	}
	stored := memRepo.items[m1.ID]
	sum := sha256.Sum256([]byte(mergedContent))
	wantHash := hex.EncodeToString(sum[:])
	if stored.Summary != mergedSummary || stored.Content != mergedContent || stored.ContentSHA256 != wantHash || stored.Version != 2 {
		t.Fatalf("unexpected persisted merged winner: %+v", stored)
	}
	if len(memRepo.versions) != 2 || memRepo.versions[0].Summary != mergedSummary || memRepo.versions[0].Content != mergedContent {
		t.Fatalf("expected immutable winner version snapshot with merged text, got %+v", memRepo.versions)
	}
}
