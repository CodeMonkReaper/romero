package memory_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/romero-project/romero/internal/domain/memory"
)

func TestSelectSubmodularContext(t *testing.T) {
	t.Parallel()

	id1 := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	id2 := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	id3 := uuid.MustParse("00000000-0000-0000-0000-000000000003")

	t.Run("prefers diverse candidate over near-duplicate under cosine", func(t *testing.T) {
		t.Parallel()
		candidates := []memory.SubmodularCandidate{
			{
				Memory:        memory.Memory{ID: id1, Summary: "pgxpool timeout"},
				FinalScore:    1.0,
				DenseVec:      []float32{1, 0, 0},
				FullTokens:    100,
				SummaryTokens: 20,
			},
			{
				Memory:        memory.Memory{ID: id2, Summary: "pgxpool timeout duplicate"},
				FinalScore:    0.92,
				DenseVec:      []float32{0.99, 0.14, 0},
				FullTokens:    100,
				SummaryTokens: 20,
			},
			{
				Memory:        memory.Memory{ID: id3, Summary: "HNSW iterative scan"},
				FinalScore:    0.88,
				DenseVec:      []float32{0, 0, 1},
				FullTokens:    100,
				SummaryTokens: 20,
			},
		}

		got := memory.SelectSubmodularContext(candidates, 200, false)
		if len(got) != 2 {
			t.Fatalf("got %d selected, want 2", len(got))
		}
		if got[0].MemoryID != id1 || got[1].MemoryID != id3 {
			t.Fatalf("selected order = [%s, %s], want [%s, %s]", got[0].MemoryID, got[1].MemoryID, id1, id3)
		}
	})

	t.Run("degrades to compact summary when full exceeds remaining budget and works with Jaccard", func(t *testing.T) {
		t.Parallel()
		candidates := []memory.SubmodularCandidate{
			{
				Memory:        memory.Memory{ID: id1, Tags: []string{"pg", "pool"}, Summary: "pool limit"},
				FinalScore:    1.0,
				FullTokens:    80,
				SummaryTokens: 15,
			},
			{
				Memory:        memory.Memory{ID: id2, Tags: []string{"hnsw", "scan"}, Summary: "hnsw relaxed order"},
				FinalScore:    0.9,
				FullTokens:    60,
				SummaryTokens: 15,
			},
		}
		got := memory.SelectSubmodularContext(candidates, 100, true)
		if len(got) != 2 {
			t.Fatalf("got %d selected, want 2 (1 full + 1 compact)", len(got))
		}
		if got[0].IsCompact || !got[1].IsCompact {
			t.Fatalf("expected first full and second compact, got compact=[%v, %v]", got[0].IsCompact, got[1].IsCompact)
		}
		total := got[0].TokenCost + got[1].TokenCost
		if total > 100 {
			t.Fatalf("total tokens %d exceeded maxTokens 100", total)
		}
	})
}
