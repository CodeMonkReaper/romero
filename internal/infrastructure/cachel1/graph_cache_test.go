package cachel1_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/romero-project/romero/internal/domain/identity"
	"github.com/romero-project/romero/internal/domain/memory"
	"github.com/romero-project/romero/internal/infrastructure/cachel1"
)

func TestCognitiveGraph_ZeroTrustPruningAndWriteThrough(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	tenantID := uuid.New()
	projID := uuid.New()
	agentA := uuid.New()
	agentB := uuid.New()

	memPub := memory.Memory{
		ID:        uuid.New(),
		TenantID:  tenantID,
		ProjectID: &projID,
		ScopeType: identity.ScopeProjectShared,
		Status:    memory.StatusActive,
		Summary:   "public node",
		Clocks:    memory.TriTemporalWindow{ObservedAt: now, ValidFrom: now.Add(-time.Hour)},
	}
	memPrivB := memory.Memory{
		ID:           uuid.New(),
		TenantID:     tenantID,
		ProjectID:    &projID,
		OwnerAgentID: &agentB,
		ScopeType:    identity.ScopeAgentPrivate,
		Status:       memory.StatusActive,
		Summary:      "agentB private bridge",
		Clocks:       memory.TriTemporalWindow{ObservedAt: now, ValidFrom: now.Add(-time.Hour)},
	}
	memTaskAllowed := memory.Memory{
		ID:        uuid.New(),
		TenantID:  tenantID,
		ProjectID: &projID,
		TaskID:    "task-ok",
		ScopeType: identity.ScopeTaskShared,
		Status:    memory.StatusActive,
		Summary:   "task node",
		Clocks:    memory.TriTemporalWindow{ObservedAt: now, ValidFrom: now.Add(-time.Hour)},
	}

	g := cachel1.NewCognitiveGraph(nil)
	g.ApplyMemoryUpsert(memPub)
	g.ApplyMemoryUpsert(memPrivB)
	g.ApplyMemoryUpsert(memTaskAllowed)
	g.ApplyRelationsAdded([]memory.MemoryRelation{
		{ID: uuid.New(), TenantID: tenantID, ProjectID: &projID, FromMemoryID: memPub.ID, ToMemoryID: memPrivB.ID, RelationType: memory.RelationSupports, Weight: 1.0, IsActive: true, ValidFrom: now.Add(-time.Hour)},
		{ID: uuid.New(), TenantID: tenantID, ProjectID: &projID, FromMemoryID: memPub.ID, ToMemoryID: memTaskAllowed.ID, RelationType: memory.RelationDependsOn, Weight: 0.9, IsActive: true, ValidFrom: now.Add(-time.Hour)},
	})
	g.ApplyEmbeddingUpsert(tenantID, &projID, memPub.ID, []float32{1, 0})

	authA := identity.AuthContext{
		TenantID:        tenantID,
		PrincipalID:     uuid.New(),
		PrincipalType:   identity.PrincipalAgent,
		AgentID:         &agentA,
		AllowedProjects: []uuid.UUID{projID},
		AllowedScopes:   []identity.ScopeType{identity.ScopeProjectShared, identity.ScopeTaskShared, identity.ScopeAgentPrivate},
	}
	filter := memory.SearchFilter{
		TenantID:      tenantID,
		ProjectIDs:    []uuid.UUID{projID},
		Scopes:        []identity.ScopeType{identity.ScopeProjectShared, identity.ScopeTaskShared, identity.ScopeAgentPrivate},
		CallerAgentID: &agentA,
	}

	sub, mems, vecs, missingVec, err := g.ProjectAuthorizedSubgraph(context.Background(), authA, filter, map[string]bool{"task-ok": true}, now)
	if err != nil {
		t.Fatalf("ProjectAuthorizedSubgraph failed: %v", err)
	}
	if _, ok := sub.Nodes[memPrivB.ID]; ok {
		t.Fatal("agentB private node leaked into agentA authorized subgraph")
	}
	if len(sub.Edges) != 1 || sub.Edges[0].ToID != memTaskAllowed.ID {
		t.Fatalf("expected only 1 authorized edge to memTaskAllowed, got %+v", sub.Edges)
	}
	if len(mems) != 2 || len(vecs) != 1 || !missingVec {
		t.Fatalf("unexpected counts: mems=%d vecs=%d missingVec=%v", len(mems), len(vecs), missingVec)
	}

	// Write-through status change removes superseded node from default projection.
	until := now
	g.ApplyMemoryStatusChange(tenantID, &projID, memTaskAllowed.ID, memory.StatusSuperseded, &until)
	sub2, _, _, _, err := g.ProjectAuthorizedSubgraph(context.Background(), authA, filter, map[string]bool{"task-ok": true}, now)
	if err != nil {
		t.Fatalf("ProjectAuthorizedSubgraph after status change failed: %v", err)
	}
	if _, ok := sub2.Nodes[memTaskAllowed.ID]; ok {
		t.Fatal("superseded node remained visible after write-through status change")
	}
	if stats := g.Stats(); !stats.Enabled || stats.NodesCount != 3 || stats.EdgesCount != 2 {
		t.Fatalf("unexpected graph stats: %+v", stats)
	}
}

type fakeSnapshotLoader struct {
	calls int
	mems  []memory.Memory
	rels  []memory.MemoryRelation
	vecs  map[uuid.UUID][]float32
	err   error
}

func (f *fakeSnapshotLoader) SearchByTrigram(context.Context, memory.SearchFilter) ([]uuid.UUID, error) {
	return nil, nil
}
func (f *fakeSnapshotLoader) SearchByFTS(context.Context, memory.SearchFilter) ([]uuid.UUID, error) {
	return nil, nil
}
func (f *fakeSnapshotLoader) SearchByVector(context.Context, memory.SearchFilter, []float32) ([]uuid.UUID, error) {
	return nil, nil
}
func (f *fakeSnapshotLoader) GetMemoriesByIDs(context.Context, uuid.UUID, []uuid.UUID) (map[uuid.UUID]memory.Memory, error) {
	return nil, nil
}
func (f *fakeSnapshotLoader) LoadProjectGraphSnapshot(ctx context.Context, tenantID uuid.UUID, projectID *uuid.UUID, now time.Time) ([]memory.Memory, []memory.MemoryRelation, map[uuid.UUID][]float32, error) {
	f.calls++
	if f.err != nil {
		return nil, nil, nil, f.err
	}
	return f.mems, f.rels, f.vecs, nil
}
func (f *fakeSnapshotLoader) TraverseCausalSubgraph(context.Context, uuid.UUID, []uuid.UUID, int, time.Time) ([]memory.CausalWalkRow, error) {
	return nil, nil
}

func TestCognitiveGraph_EnsureLoaded(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	tenantID := uuid.New()
	projID := uuid.New()
	mID := uuid.New()

	loader := &fakeSnapshotLoader{
		mems: []memory.Memory{{
			ID:        mID,
			TenantID:  tenantID,
			ProjectID: &projID,
			ScopeType: identity.ScopeProjectShared,
			Status:    memory.StatusActive,
			Clocks:    memory.TriTemporalWindow{ObservedAt: now, ValidFrom: now.Add(-time.Hour)},
		}},
		vecs: map[uuid.UUID][]float32{mID: {1, 0}},
	}
	g := cachel1.NewCognitiveGraph(loader)
	auth := identity.AuthContext{
		TenantID:        tenantID,
		PrincipalID:     uuid.New(),
		PrincipalType:   identity.PrincipalUser,
		AllowedProjects: []uuid.UUID{projID},
		AllowedScopes:   []identity.ScopeType{identity.ScopeProjectShared},
	}

	sub, _, _, missing, err := g.ProjectAuthorizedSubgraph(context.Background(), auth, memory.SearchFilter{TenantID: tenantID}, nil, now)
	if err != nil || len(sub.Nodes) != 1 || missing {
		t.Fatalf("expected 1 loaded node without missing vec, got nodes=%d missing=%v err=%v", len(sub.Nodes), missing, err)
	}
	// Second call hits cached partition without calling loader again.
	_, _, _, _, _ = g.ProjectAuthorizedSubgraph(context.Background(), auth, memory.SearchFilter{TenantID: tenantID}, nil, now)
	if loader.calls != 1 {
		t.Fatalf("expected 1 loader call, got %d", loader.calls)
	}
}
