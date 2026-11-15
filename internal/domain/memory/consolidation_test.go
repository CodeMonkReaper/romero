package memory

import (
	"testing"

	"github.com/google/uuid"
	"github.com/romero-project/romero/internal/domain/identity"
)

func TestClusterEpisodicMemories(t *testing.T) {
	tenantID := uuid.New()
	projID := uuid.New()
	agentA := uuid.New()
	agentB := uuid.New()

	mkEp := func(scope identity.ScopeType, owner *uuid.UUID, task string, status MemoryStatus, conf float32) Memory {
		return Memory{
			ID:           uuid.New(),
			TenantID:     tenantID,
			ProjectID:    &projID,
			OwnerAgentID: owner,
			TaskID:       task,
			Type:         TypeEpisodic,
			ScopeType:    scope,
			Status:       status,
			Confidence:   conf,
		}
	}

	// 3 high-confidence episodic memories in project_shared with high cosine similarity
	p1 := mkEp(identity.ScopeProjectShared, nil, "", StatusActive, 0.85)
	p2 := mkEp(identity.ScopeProjectShared, nil, "", StatusActive, 0.80)
	p3 := mkEp(identity.ScopeProjectShared, nil, "", StatusActive, 0.75)

	// 1 low-confidence episodic (must be excluded)
	pLow := mkEp(identity.ScopeProjectShared, nil, "", StatusActive, 0.60)
	// 1 non-active episodic (must be excluded)
	pCons := mkEp(identity.ScopeProjectShared, nil, "", StatusConsolidated, 0.90)
	// 2 agent_private for agentA and 1 for agentB (must NEVER merge across agents)
	a1 := mkEp(identity.ScopeAgentPrivate, &agentA, "", StatusActive, 0.90)
	a2 := mkEp(identity.ScopeAgentPrivate, &agentA, "", StatusActive, 0.90)
	b1 := mkEp(identity.ScopeAgentPrivate, &agentB, "", StatusActive, 0.90)

	vecSimilar := []float32{1, 0, 0}
	vecNear := []float32{0.9, 0.4358899, 0} // cos with vecSimilar = 0.90 >= 0.80

	vectors := map[uuid.UUID][]float32{
		p1.ID:    vecSimilar,
		p2.ID:    vecSimilar,
		p3.ID:    vecNear,
		pLow.ID:  vecSimilar,
		pCons.ID: vecSimilar,
		a1.ID:    vecSimilar,
		a2.ID:    vecSimilar,
		b1.ID:    vecSimilar,
	}

	clusters := ClusterEpisodicMemories([]Memory{p1, p2, p3, pLow, pCons, a1, a2, b1}, vectors)
	if len(clusters) != 1 {
		t.Fatalf("expected exactly 1 cluster (project_shared only), got %d", len(clusters))
	}
	if len(clusters[0].Episodes) != 3 {
		t.Fatalf("expected 3 episodes in cluster, got %d", len(clusters[0].Episodes))
	}
	if clusters[0].MeanSimilarity < 0.80 {
		t.Fatalf("expected mean similarity >= 0.80, got %v", clusters[0].MeanSimilarity)
	}
}

func TestValidateDistilledNLI(t *testing.T) {
	ok := ValidateDistilledNLI(
		[]NLIScore{{Entailment: 0.84}, {Entailment: 0.81}, {Entailment: 0.90}},
		[]NLIScore{{Contradiction: 0.12}, {Contradiction: 0.40}},
	)
	if !ok {
		t.Fatal("expected distilled NLI validation to pass")
	}

	// Fails if any source entailment < 0.80
	if ValidateDistilledNLI(
		[]NLIScore{{Entailment: 0.85}, {Entailment: 0.75}},
		[]NLIScore{{Contradiction: 0.05}},
	) {
		t.Fatal("expected failure when source entailment < 0.80")
	}

	// Fails if any verified memory contradiction >= 0.80
	if ValidateDistilledNLI(
		[]NLIScore{{Entailment: 0.90}},
		[]NLIScore{{Contradiction: 0.81}},
	) {
		t.Fatal("expected failure when verified contradiction >= 0.80")
	}
}
