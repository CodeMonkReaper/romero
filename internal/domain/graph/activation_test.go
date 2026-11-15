package graph_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/romero-project/romero/internal/domain/graph"
	"github.com/romero-project/romero/internal/domain/memory"
)

func TestConductance(t *testing.T) {
	t.Parallel()

	cases := []struct {
		rel               memory.RelationType
		includeSuperseded bool
		want              float32
	}{
		{memory.RelationDependsOn, false, 1.00},
		{memory.RelationCausedBy, false, 0.95},
		{memory.RelationSupports, false, 0.85},
		{memory.RelationDerivedFrom, false, 0.80},
		{memory.RelationContradicts, false, 0.60},
		{memory.RelationRelatedTo, false, 0.50},
		{memory.RelationSupersedes, false, 0.00},
		{memory.RelationSupersedes, true, 0.40},
		{memory.RelationType("unknown"), false, 0.00},
	}

	for _, tc := range cases {
		if got := graph.Conductance(tc.rel, tc.includeSuperseded); got != tc.want {
			t.Errorf("Conductance(%q, %v) = %v, want %v", tc.rel, tc.includeSuperseded, got, tc.want)
		}
	}
}

func TestPropagateActivation(t *testing.T) {
	t.Parallel()

	seedID := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	hop1ID := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	hop2ID := uuid.MustParse("00000000-0000-0000-0000-000000000003")
	hiddenID := uuid.MustParse("00000000-0000-0000-0000-000000000004")

	sub := graph.AuthorizedSubgraph{
		Nodes: map[uuid.UUID]graph.AuthorizedNode{
			seedID: {ID: seedID, Status: memory.StatusActive},
			hop1ID: {ID: hop1ID, Status: memory.StatusActive},
			hop2ID: {ID: hop2ID, Status: memory.StatusActive},
		},
		Edges: []graph.AuthorizedEdge{
			{FromID: seedID, ToID: hop1ID, RelationType: memory.RelationDependsOn, Weight: 1.0},
			{FromID: hop2ID, ToID: hop1ID, RelationType: memory.RelationContradicts, Weight: 1.0},
			// Edge to unauthorized node not in sub.Nodes must be ignored.
			{FromID: seedID, ToID: hiddenID, RelationType: memory.RelationSupports, Weight: 1.0},
		},
	}

	res := graph.PropagateActivation(map[uuid.UUID]float32{seedID: 0.9}, sub, 2, false)
	if _, ok := res[hiddenID]; ok {
		t.Fatal("unauthorized node must never receive activation")
	}
	if res[seedID].VisibleRelationsCount != 1 {
		t.Fatalf("seed VisibleRelationsCount = %d, want 1 (hidden edge excluded)", res[seedID].VisibleRelationsCount)
	}
	if res[hop2ID].NormalizedScore <= 0 || res[hop2ID].MinHopDepth != 2 {
		t.Fatalf("expected 2-hop bidirectional rescue on hop2ID, got %+v", res[hop2ID])
	}
	if !res[hop2ID].HasConflictAlert {
		t.Fatal("expected HasConflictAlert=true when reached via contradicts edge")
	}

	t.Run("edge cases empty zero-seeds conflicted-status and hop clamping", func(t *testing.T) {
		t.Parallel()
		if got := graph.PropagateActivation(nil, sub, 2, false); len(got) != 0 {
			t.Fatalf("expected empty result for nil seeds, got %d", len(got))
		}
		if got := graph.PropagateActivation(map[uuid.UUID]float32{seedID: 0}, sub, 2, false); len(got) != 0 {
			t.Fatalf("expected empty result for zero seed, got %d", len(got))
		}
		confSub := graph.AuthorizedSubgraph{
			Nodes: map[uuid.UUID]graph.AuthorizedNode{
				seedID: {ID: seedID, Status: memory.StatusConflicted},
				hop1ID: {ID: hop1ID, Status: memory.StatusActive},
			},
			Edges: []graph.AuthorizedEdge{
				{FromID: seedID, ToID: seedID, RelationType: memory.RelationSupports, Weight: 1.0}, // self-loop ignored
				{FromID: seedID, ToID: hop1ID, RelationType: memory.RelationContradicts, Weight: 1.0},
				{FromID: seedID, ToID: hop1ID, RelationType: memory.RelationRelatedTo, Weight: 0.0}, // zero weight ignored
			},
		}
		res0 := graph.PropagateActivation(map[uuid.UUID]float32{seedID: 1.0}, confSub, -1, false)
		if !res0[seedID].HasConflictAlert || res0[hop1ID].NormalizedScore != 0 {
			t.Fatalf("expected 0-hop conflict alert only on seed, got %+v", res0)
		}
		res3 := graph.PropagateActivation(map[uuid.UUID]float32{seedID: 1.0}, confSub, 10, false)
		if !res3[hop1ID].HasConflictAlert || res3[hop1ID].MinHopDepth != 1 {
			t.Fatalf("expected clamped hop propagation with conflict alert, got %+v", res3[hop1ID])
		}
	})
}
