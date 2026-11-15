package retrieval_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/romero-project/romero/internal/application/retrieval"
	"github.com/romero-project/romero/internal/domain/governor"
	"github.com/romero-project/romero/internal/domain/graph"
	"github.com/romero-project/romero/internal/domain/identity"
	"github.com/romero-project/romero/internal/domain/memory"
)

type fakeReranker struct {
	scores map[uuid.UUID]float32
	err    error
	calls  int
}

func (f *fakeReranker) Rerank(_ context.Context, _ string, _ []memory.RerankCandidate) (map[uuid.UUID]float32, error) {
	f.calls++
	return f.scores, f.err
}

func TestSearchMemories_CrossEncoderRerankAndFallback(t *testing.T) {
	ctx := context.Background()
	tenantID, projID := uuid.New(), uuid.New()
	now := time.Now().UTC()

	m1 := memory.Memory{
		ID: uuid.New(), TenantID: tenantID, ProjectID: &projID,
		Type: memory.TypeSemantic, ScopeType: identity.ScopeProjectShared, Status: memory.StatusActive,
		Summary: "lexical noise", Content: "lexical noise",
		EpistemicStatus: memory.EpistemicObserved, AlphaScore: 2, BetaScore: 1, Confidence: 0.66,
		Clocks: memory.TriTemporalWindow{ObservedAt: now, ValidFrom: now, CreatedAt: now},
	}
	m2 := m1
	m2.ID = uuid.New()
	m2.Summary = "logical answer"
	m2.Content = "logical answer"

	searchRepo := &fakeHybridSearchRepo{
		trgmIDs:  []uuid.UUID{m1.ID, m2.ID},
		ftsIDs:   []uuid.UUID{m1.ID},
		memories: map[uuid.UUID]memory.Memory{m1.ID: m1, m2.ID: m2},
	}
	uow := newFakeRetrievalStore()
	svc, err := retrieval.NewService(uow, uow, nil, nil, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}
	proj := fakeGraphProjector{
		sub: graph.AuthorizedSubgraph{
			Nodes: map[uuid.UUID]graph.AuthorizedNode{
				m1.ID: {ID: m1.ID, Status: m1.Status},
				m2.ID: {ID: m2.ID, Status: m2.Status},
			},
		},
		mems: map[uuid.UUID]memory.Memory{m1.ID: m1, m2.ID: m2},
	}
	svc.SetHybridDeps(searchRepo, &fakeEmbedder{}, proj)

	reranker := &fakeReranker{scores: map[uuid.UUID]float32{m1.ID: -4.0, m2.ID: 4.0}}
	svc.WithReranker(reranker, governor.ProfileUltra)

	auth := identity.AuthContext{
		TenantID: tenantID, PrincipalID: uuid.New(), PrincipalType: identity.PrincipalAgent,
		AllowedProjects: []uuid.UUID{projID}, AllowedScopes: []identity.ScopeType{identity.ScopeProjectShared},
	}
	res, err := svc.SearchMemories(ctx, auth, retrieval.SearchQuery{Query: "answer", Limit: 5})
	if err != nil {
		t.Fatalf("SearchMemories failed: %v", err)
	}
	if reranker.calls != 1 || len(res.Candidates) != 2 || res.Candidates[0].ID != m2.ID {
		t.Fatalf("expected Cross-Encoder to promote m2 over m1: calls=%d candidates=%+v", reranker.calls, res.Candidates)
	}

	// Fallback when reranker errors: search still succeeds cleanly.
	reranker.err = errors.New("reranker down")
	res2, err := svc.SearchMemories(ctx, auth, retrieval.SearchQuery{Query: "answer", Limit: 5})
	if err != nil || len(res2.Candidates) != 2 {
		t.Fatalf("expected clean fallback when reranker fails, got err=%v candidates=%d", err, len(res2.Candidates))
	}

	// Non-ultra profile bypasses Cross-Encoder reranking completely.
	reranker.err = nil
	callsBefore := reranker.calls
	svc.WithReranker(reranker, governor.ProfileBalanced)
	if _, err := svc.SearchMemories(ctx, auth, retrieval.SearchQuery{Query: "answer", Limit: 5}); err != nil || reranker.calls != callsBefore {
		t.Fatalf("expected balanced profile to bypass reranker, got err=%v calls=%d->%d", err, callsBefore, reranker.calls)
	}
}
