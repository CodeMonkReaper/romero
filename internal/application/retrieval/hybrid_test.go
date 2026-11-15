package retrieval_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/romero-project/romero/internal/application/retrieval"
	"github.com/romero-project/romero/internal/domain/graph"
	"github.com/romero-project/romero/internal/domain/identity"
	"github.com/romero-project/romero/internal/domain/memory"
)

type fakeHybridSearchRepo struct {
	trgmIDs     []uuid.UUID
	ftsIDs      []uuid.UUID
	vecIDs      []uuid.UUID
	memories    map[uuid.UUID]memory.Memory
	relations   []memory.MemoryRelation
	embeddings  map[uuid.UUID][]float32
	vecQueries  int
	trgmQueries int
	ftsQueries  int
}

func (f *fakeHybridSearchRepo) SearchByTrigram(_ context.Context, _ memory.SearchFilter) ([]uuid.UUID, error) {
	f.trgmQueries++
	return f.trgmIDs, nil
}
func (f *fakeHybridSearchRepo) SearchByFTS(_ context.Context, _ memory.SearchFilter) ([]uuid.UUID, error) {
	f.ftsQueries++
	return f.ftsIDs, nil
}
func (f *fakeHybridSearchRepo) SearchByVector(_ context.Context, _ memory.SearchFilter, _ []float32) ([]uuid.UUID, error) {
	f.vecQueries++
	return f.vecIDs, nil
}
func (f *fakeHybridSearchRepo) GetMemoriesByIDs(_ context.Context, _ uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]memory.Memory, error) {
	out := make(map[uuid.UUID]memory.Memory, len(ids))
	for _, id := range ids {
		if m, ok := f.memories[id]; ok {
			out[id] = m
		}
	}
	return out, nil
}
func (f *fakeHybridSearchRepo) LoadProjectGraphSnapshot(_ context.Context, _ uuid.UUID, _ *uuid.UUID, _ time.Time) ([]memory.Memory, []memory.MemoryRelation, map[uuid.UUID][]float32, error) {
	mems := make([]memory.Memory, 0, len(f.memories))
	for _, m := range f.memories {
		mems = append(mems, m)
	}
	return mems, f.relations, f.embeddings, nil
}
func (f *fakeHybridSearchRepo) TraverseCausalSubgraph(_ context.Context, _ uuid.UUID, _ []uuid.UUID, _ int, _ time.Time) ([]memory.CausalWalkRow, error) {
	return nil, nil
}

type fakeEmbedder struct {
	vec []float32
	err error
}

func (f fakeEmbedder) Embed(_ context.Context, _ string) (memory.EmbeddingOutput, error) {
	if f.err != nil {
		return memory.EmbeddingOutput{}, f.err
	}
	return memory.EmbeddingOutput{Dense: f.vec}, nil
}
func (f fakeEmbedder) EmbedBatch(_ context.Context, texts []string) ([]memory.EmbeddingOutput, error) {
	if f.err != nil {
		return nil, f.err
	}
	out := make([]memory.EmbeddingOutput, len(texts))
	for i := range texts {
		out[i] = memory.EmbeddingOutput{Dense: f.vec}
	}
	return out, nil
}
func (f fakeEmbedder) ModelName() string { return "BAAI/bge-m3" }
func (f fakeEmbedder) Dimensions() int   { return 1024 }

type fakeGraphProjector struct {
	sub        graph.AuthorizedSubgraph
	mems       map[uuid.UUID]memory.Memory
	embeddings map[uuid.UUID][]float32
	degraded   bool
}

func (f fakeGraphProjector) ProjectAuthorizedSubgraph(_ context.Context, _ identity.AuthContext, _ memory.SearchFilter, _ map[string]bool, _ time.Time) (graph.AuthorizedSubgraph, map[uuid.UUID]memory.Memory, map[uuid.UUID][]float32, bool, error) {
	return f.sub, f.mems, f.embeddings, f.degraded, nil
}

func TestRetrievalService_SearchMemories_TriModalAndMultiHop(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	projectID := uuid.New()
	now := time.Now().UTC()

	m1 := memory.Memory{
		ID: uuid.New(), TenantID: tenantID, ProjectID: &projectID,
		Type: memory.TypeSemantic, ScopeType: identity.ScopeProjectShared,
		Status: memory.StatusActive, EpistemicStatus: memory.EpistemicVerified,
		Confidence: 0.95, Importance: 0.8, UtilityScore: 0.7,
		Summary: "JWT rotation seed", Content: "SECRET_CONTENT_SHOULD_NOT_APPEAR",
		Clocks: memory.TriTemporalWindow{ObservedAt: now, ValidFrom: now, CreatedAt: now},
	}
	m2 := memory.Memory{
		ID: uuid.New(), TenantID: tenantID, ProjectID: &projectID,
		Type: memory.TypeSemantic, ScopeType: identity.ScopeProjectShared,
		Status: memory.StatusActive, EpistemicStatus: memory.EpistemicObserved,
		Confidence: 0.75, Importance: 0.6, UtilityScore: 0.5,
		Summary: "Root cause 1-hop", Content: "ROOT_CAUSE_CONTENT",
		Clocks: memory.TriTemporalWindow{ObservedAt: now, ValidFrom: now, CreatedAt: now},
	}

	sub := graph.AuthorizedSubgraph{
		Nodes: map[uuid.UUID]graph.AuthorizedNode{
			m1.ID: {ID: m1.ID, Status: m1.Status},
			m2.ID: {ID: m2.ID, Status: m2.Status},
		},
		Edges: []graph.AuthorizedEdge{
			{FromID: m1.ID, ToID: m2.ID, RelationType: memory.RelationCausedBy, Weight: 1.0},
		},
	}

	repo := &fakeHybridSearchRepo{
		trgmIDs:  []uuid.UUID{m1.ID},
		ftsIDs:   []uuid.UUID{m1.ID},
		vecIDs:   []uuid.UUID{m1.ID},
		memories: map[uuid.UUID]memory.Memory{m1.ID: m1, m2.ID: m2},
	}
	uow := newFakeRetrievalStore()
	uow.memories[m1.ID] = m1
	uow.memories[m2.ID] = m2

	svc, err := retrieval.NewService(uow, uow, nil, nil, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}
	svc.SetHybridDeps(repo, fakeEmbedder{vec: []float32{0.1, 0.2}}, fakeGraphProjector{
		sub:        sub,
		mems:       repo.memories,
		embeddings: map[uuid.UUID][]float32{m1.ID: {0.1, 0.2}, m2.ID: {0.2, 0.1}},
	})

	auth := identity.AuthContext{
		TenantID: tenantID, PrincipalID: uuid.New(), PrincipalType: identity.PrincipalAgent,
		AllowedProjects: []uuid.UUID{projectID}, AllowedScopes: []identity.ScopeType{identity.ScopeProjectShared},
	}

	res, err := svc.SearchMemories(context.Background(), auth, retrieval.SearchQuery{
		Query: "JWT rotation", Limit: 10, Hops: 2,
	})
	if err != nil {
		t.Fatalf("SearchMemories failed: %v", err)
	}
	if res.DegradedMode {
		t.Fatal("expected DegradedMode=false when embedder and all embeddings are present")
	}
	if len(res.Candidates) != 2 {
		t.Fatalf("expected 2 candidates (seed + 1-hop), got %d", len(res.Candidates))
	}
	if res.Candidates[0].ID != m1.ID || res.Candidates[0].MinHopDepth != 0 || res.Candidates[0].RelationsCount != 1 {
		t.Fatalf("unexpected top candidate: %+v", res.Candidates[0])
	}
	if res.Candidates[1].ID != m2.ID || res.Candidates[1].MinHopDepth != 1 {
		t.Fatalf("unexpected 1-hop rescued candidate: %+v", res.Candidates[1])
	}
	if len(uow.allowed) != 1 || uow.allowed[0].Operation != "search_memories" {
		t.Fatalf("expected allowed audit event search_memories, got %+v", uow.allowed)
	}

	// Degraded mode when embedder fails: still returns 200 equivalent with DegradedMode=true.
	svc.SetHybridDeps(repo, fakeEmbedder{err: errors.New("bge-m3 down")}, fakeGraphProjector{
		sub: sub, mems: repo.memories,
	})
	resDeg, err := svc.SearchMemories(context.Background(), auth, retrieval.SearchQuery{
		Query: "JWT rotation", Limit: 10, Hops: 1,
	})
	if err != nil {
		t.Fatalf("SearchMemories degraded failed: %v", err)
	}
	if !resDeg.DegradedMode || len(resDeg.Candidates) == 0 {
		t.Fatalf("expected DegradedMode=true with lexical/graph candidates, got %+v", resDeg)
	}
}

func TestRetrievalService_GetSubgraph_Opaque404AndAudit(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	projectID := uuid.New()
	agentA := uuid.New()
	agentB := uuid.New()
	now := time.Now().UTC()

	mPublic := memory.Memory{
		ID: uuid.New(), TenantID: tenantID, ProjectID: &projectID,
		Type: memory.TypeSemantic, ScopeType: identity.ScopeProjectShared,
		Status: memory.StatusActive, EpistemicStatus: memory.EpistemicObserved,
		Confidence: 0.8, Summary: "root",
		Clocks: memory.TriTemporalWindow{ObservedAt: now, ValidFrom: now, CreatedAt: now},
	}
	mPrivate := memory.Memory{
		ID: uuid.New(), TenantID: tenantID, ProjectID: &projectID, OwnerAgentID: &agentB,
		Type: memory.TypeSemantic, ScopeType: identity.ScopeAgentPrivate,
		Status: memory.StatusActive, EpistemicStatus: memory.EpistemicObserved,
		Confidence: 0.8, Summary: "foreign private",
	}

	uow := newFakeRetrievalStore()
	uow.memories[mPublic.ID] = mPublic
	uow.memories[mPrivate.ID] = mPrivate

	sub := graph.AuthorizedSubgraph{
		Nodes: map[uuid.UUID]graph.AuthorizedNode{
			mPublic.ID: {ID: mPublic.ID, Status: mPublic.Status},
		},
	}

	svc, _ := retrieval.NewService(uow, uow, nil, nil, func() time.Time { return now })
	svc.SetHybridDeps(&fakeHybridSearchRepo{memories: uow.memories}, nil, fakeGraphProjector{
		sub: sub, mems: map[uuid.UUID]memory.Memory{mPublic.ID: mPublic},
	})

	authA := identity.AuthContext{
		TenantID: tenantID, PrincipalID: uuid.New(), PrincipalType: identity.PrincipalAgent,
		AgentID: &agentA, AllowedProjects: []uuid.UUID{projectID}, AllowedScopes: []identity.ScopeType{identity.ScopeProjectShared, identity.ScopeAgentPrivate},
	}

	// Foreign agent_private root -> 404 opaque ErrNotFound + denied audit.
	if _, err := svc.GetSubgraph(context.Background(), authA, retrieval.SubgraphQuery{MemoryID: mPrivate.ID, Hops: 2}); !errors.Is(err, retrieval.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for foreign agent_private subgraph, got %v", err)
	}
	if len(uow.denied) == 0 {
		t.Fatal("expected denied audit event for unreadable GetSubgraph")
	}

	// Authorized root -> succeeds with allowed audit.
	sg, err := svc.GetSubgraph(context.Background(), authA, retrieval.SubgraphQuery{MemoryID: mPublic.ID, Hops: 2})
	if err != nil {
		t.Fatalf("GetSubgraph failed: %v", err)
	}
	if sg.RootMemoryID != mPublic.ID || len(sg.Nodes) != 1 {
		t.Fatalf("unexpected subgraph result: %+v", sg)
	}
}

type fakeRetrievalTaskAccess struct {
	canRead bool
	err     error
}

func (f fakeRetrievalTaskAccess) EvaluateTaskAccess(_ context.Context, _ identity.TaskPermissionQuery) (retrieval.TaskAccessSnapshot, error) {
	return retrieval.TaskAccessSnapshot{CanRead: f.canRead}, f.err
}

func TestRetrievalService_ValidationScopeAuthTaskAccessAndPureRead(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	projectID := uuid.New()
	foreignProject := uuid.New()
	now := time.Now().UTC()

	m1 := memory.Memory{
		ID: uuid.New(), TenantID: tenantID, ProjectID: &projectID, TaskID: "task-1",
		Type: memory.TypeSemantic, ScopeType: identity.ScopeTaskShared,
		Status: memory.StatusActive, EpistemicStatus: memory.EpistemicObserved,
		Confidence: 0.8, Importance: 0.7, Summary: "task fact", Content: "full body",
		AccessCount: 0, Version: 1,
		Clocks: memory.TriTemporalWindow{ObservedAt: now, ValidFrom: now, CreatedAt: now},
	}
	m2 := memory.Memory{
		ID: uuid.New(), TenantID: tenantID, ProjectID: &projectID, TaskID: "task-1",
		Type: memory.TypeSemantic, ScopeType: identity.ScopeTaskShared,
		Status: memory.StatusConflicted, EpistemicStatus: memory.EpistemicObserved,
		Confidence: 0.6, Importance: 0.5, Summary: "conflicting fact", Content: "conflict body",
		Clocks: memory.TriTemporalWindow{ObservedAt: now, ValidFrom: now, CreatedAt: now},
	}

	uow := newFakeRetrievalStore()
	uow.memories[m1.ID] = m1
	uow.memories[m2.ID] = m2
	sub := graph.AuthorizedSubgraph{
		Nodes: map[uuid.UUID]graph.AuthorizedNode{
			m1.ID: {ID: m1.ID, Status: m1.Status},
			m2.ID: {ID: m2.ID, Status: m2.Status},
		},
		Edges: []graph.AuthorizedEdge{
			{FromID: m1.ID, ToID: m2.ID, RelationType: memory.RelationContradicts, Weight: 1.0},
		},
	}
	repo := &fakeHybridSearchRepo{
		trgmIDs:  []uuid.UUID{m1.ID},
		memories: uow.memories,
	}

	svc, _ := retrieval.NewService(uow, uow, fakeRetrievalTaskAccess{canRead: true}, nil, func() time.Time { return now })
	svc.SetHybridDeps(repo, fakeEmbedder{vec: []float32{0.1, 0.2}}, fakeGraphProjector{
		sub: sub, mems: uow.memories, embeddings: map[uuid.UUID][]float32{m1.ID: {1, 0}, m2.ID: {0, 1}},
	})

	auth := identity.AuthContext{
		TenantID: tenantID, PrincipalID: uuid.New(), PrincipalType: identity.PrincipalAgent,
		AllowedProjects: []uuid.UUID{projectID}, AllowedScopes: []identity.ScopeType{identity.ScopeTaskShared, identity.ScopeProjectShared},
	}

	// Validation & auth failures
	if _, err := svc.SearchMemories(context.Background(), identity.AuthContext{}, retrieval.SearchQuery{Query: "x"}); !errors.Is(err, retrieval.ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized, got %v", err)
	}
	if _, err := svc.SearchMemories(context.Background(), auth, retrieval.SearchQuery{Query: "   "}); !errors.Is(err, retrieval.ErrValidation) {
		t.Fatalf("expected ErrValidation for blank query, got %v", err)
	}
	if _, err := svc.SearchMemories(context.Background(), auth, retrieval.SearchQuery{Query: "x", Limit: 101}); !errors.Is(err, retrieval.ErrValidation) {
		t.Fatalf("expected ErrValidation for limit > 100, got %v", err)
	}
	if _, err := svc.SearchMemories(context.Background(), auth, retrieval.SearchQuery{Query: "x", Hops: 4}); !errors.Is(err, retrieval.ErrValidation) {
		t.Fatalf("expected ErrValidation for hops > 3, got %v", err)
	}
	if _, err := svc.SearchMemories(context.Background(), auth, retrieval.SearchQuery{Query: "x", ProjectIDs: []uuid.UUID{foreignProject}}); !errors.Is(err, retrieval.ErrForbidden) {
		t.Fatalf("expected ErrForbidden for foreign project, got %v", err)
	}
	if _, err := svc.SearchMemories(context.Background(), auth, retrieval.SearchQuery{Query: "x", Scopes: []identity.ScopeType{identity.ScopeOrgShared}}); !errors.Is(err, retrieval.ErrForbidden) {
		t.Fatalf("expected ErrForbidden for disallowed scope, got %v", err)
	}
	if _, err := svc.GetSubgraph(context.Background(), auth, retrieval.SubgraphQuery{MemoryID: uuid.Nil, Hops: 1}); !errors.Is(err, retrieval.ErrValidation) {
		t.Fatalf("expected ErrValidation for nil MemoryID in GetSubgraph, got %v", err)
	}

	// SearchForContext returns full Content & DenseVec without double-auditing search_memories.
	ctxCands, deg, err := svc.SearchForContext(context.Background(), auth, retrieval.SearchQuery{
		Query: "task fact", TaskID: "task-1", Hops: 2,
	})
	if err != nil || deg || len(ctxCands) == 0 || ctxCands[0].Content != "full body" {
		t.Fatalf("unexpected SearchForContext result: cands=%+v deg=%v err=%v", ctxCands, deg, err)
	}
	if len(uow.allowed) != 0 {
		t.Fatalf("expected 0 allowed audit events from SearchForContext, got %d", len(uow.allowed))
	}

	// GetSubgraph with conflict edge returns both nodes and edge.
	sg, err := svc.GetSubgraph(context.Background(), auth, retrieval.SubgraphQuery{MemoryID: m1.ID, Hops: 2})
	if err != nil || len(sg.Nodes) != 2 || len(sg.Edges) != 1 || !sg.Nodes[0].HasConflictAlert {
		t.Fatalf("unexpected conflict subgraph: %+v err=%v", sg, err)
	}

	// Pure read-only invariant: AccessCount and Version unchanged.
	if uow.memories[m1.ID].AccessCount != 0 || uow.memories[m1.ID].Version != 1 {
		t.Fatalf("pure read mutated memory: %+v", uow.memories[m1.ID])
	}

	// Denied task access -> ErrForbidden.
	denySvc, _ := retrieval.NewService(uow, uow, fakeRetrievalTaskAccess{canRead: false}, nil, func() time.Time { return now })
	denySvc.SetHybridDeps(repo, nil, fakeGraphProjector{sub: sub, mems: uow.memories})
	if _, err := denySvc.SearchMemories(context.Background(), auth, retrieval.SearchQuery{Query: "x", TaskID: "task-1"}); !errors.Is(err, retrieval.ErrForbidden) {
		t.Fatalf("expected ErrForbidden when task_shared CanRead=false, got %v", err)
	}
}
