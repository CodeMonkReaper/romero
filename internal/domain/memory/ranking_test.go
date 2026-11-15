package memory_test

import (
	"math"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/romero-project/romero/internal/domain/memory"
)

func TestComputeRRFScores(t *testing.T) {
	t.Parallel()

	idA := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	idB := uuid.MustParse("00000000-0000-0000-0000-000000000002")

	t.Run("normal weights 0.25 0.30 0.45", func(t *testing.T) {
		t.Parallel()
		scores := memory.ComputeRRFScores(
			[]uuid.UUID{idA, idB},
			[]uuid.UUID{idB, idA},
			[]uuid.UUID{idA},
			false,
		)
		wantA := float32(0.25/61.0 + 0.30/62.0 + 0.45/61.0)
		wantB := float32(0.25/62.0 + 0.30/61.0)
		if math.Abs(float64(scores[idA]-wantA)) > 1e-6 {
			t.Fatalf("scores[idA] = %v, want %v", scores[idA], wantA)
		}
		if math.Abs(float64(scores[idB]-wantB)) > 1e-6 {
			t.Fatalf("scores[idB] = %v, want %v", scores[idB], wantB)
		}
	})

	t.Run("degraded weights 0.45 0.55 0.00", func(t *testing.T) {
		t.Parallel()
		scores := memory.ComputeRRFScores(
			[]uuid.UUID{idA},
			[]uuid.UUID{idB},
			[]uuid.UUID{idB, idA},
			true,
		)
		wantA := float32(0.45 / 61.0)
		wantB := float32(0.55 / 61.0)
		if math.Abs(float64(scores[idA]-wantA)) > 1e-6 {
			t.Fatalf("degraded scores[idA] = %v, want %v", scores[idA], wantA)
		}
		if math.Abs(float64(scores[idB]-wantB)) > 1e-6 {
			t.Fatalf("degraded scores[idB] = %v, want %v", scores[idB], wantB)
		}
	})
}

func TestRankCandidates(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	id1 := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	id2 := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	idSup := uuid.MustParse("00000000-0000-0000-0000-000000000003")
	idArch := uuid.MustParse("00000000-0000-0000-0000-000000000004")
	idConf := uuid.MustParse("00000000-0000-0000-0000-000000000005")
	idCons := uuid.MustParse("00000000-0000-0000-0000-000000000006")

	makeMem := func(id uuid.UUID, status memory.MemoryStatus, ep memory.EpistemicStatus, typ memory.MemoryType, obs time.Time) memory.Memory {
		return memory.Memory{
			ID:              id,
			Status:          status,
			EpistemicStatus: ep,
			Type:            typ,
			AlphaScore:      8.0,
			BetaScore:       2.0,
			Confidence:      0.8,
			Importance:      0.5,
			UtilityScore:    0.0,
			Clocks:          memory.TriTemporalWindow{ObservedAt: obs},
		}
	}

	t.Run("excludes superseded consolidated archived and sets conflict alert", func(t *testing.T) {
		t.Parallel()
		inputs := []memory.CandidateRankingInput{
			{Memory: makeMem(id1, memory.StatusActive, memory.EpistemicVerified, memory.TypeSemantic, now), SemanticScore: 1.0},
			{Memory: makeMem(id2, memory.StatusActive, memory.EpistemicHypothesis, memory.TypeEpisodic, now.Add(-30*24*time.Hour)), SemanticScore: 1.0},
			{Memory: makeMem(idSup, memory.StatusSuperseded, memory.EpistemicVerified, memory.TypeSemantic, now), SemanticScore: 1.0},
			{Memory: makeMem(idArch, memory.StatusArchived, memory.EpistemicVerified, memory.TypeSemantic, now), SemanticScore: 1.0},
			{Memory: makeMem(idConf, memory.StatusConflicted, memory.EpistemicObserved, memory.TypeSemantic, now), SemanticScore: 1.0},
			{Memory: makeMem(idCons, memory.StatusConsolidated, memory.EpistemicObserved, memory.TypeSemantic, now), SemanticScore: 1.0},
		}

		got := memory.RankCandidates(inputs, now, false, false)
		if len(got) != 3 {
			t.Fatalf("got %d candidates, want 3 (active x2 + conflicted)", len(got))
		}
		if got[0].Memory.ID != id1 {
			t.Fatalf("top candidate = %s, want verified active %s", got[0].Memory.ID, id1)
		}
		var foundConf bool
		for _, c := range got {
			if c.Memory.ID == idConf {
				foundConf = true
				if !c.HasConflictAlert || c.StateFactor != 0.55 {
					t.Fatalf("conflicted candidate alert=%v stateFactor=%v", c.HasConflictAlert, c.StateFactor)
				}
			}
		}
		if !foundConf {
			t.Fatal("expected conflicted candidate in default results")
		}
	})

	t.Run("includeSuperseded and includeConsolidated flags", func(t *testing.T) {
		t.Parallel()
		inputs := []memory.CandidateRankingInput{
			{Memory: makeMem(idSup, memory.StatusSuperseded, memory.EpistemicVerified, memory.TypeSemantic, now), SemanticScore: 1.0},
			{Memory: makeMem(idCons, memory.StatusConsolidated, memory.EpistemicObserved, memory.TypeSemantic, now), SemanticScore: 1.0},
			{Memory: makeMem(idArch, memory.StatusRejected, memory.EpistemicVerified, memory.TypeSemantic, now), SemanticScore: 1.0},
		}
		got := memory.RankCandidates(inputs, now, true, true)
		if len(got) != 2 {
			t.Fatalf("got %d candidates, want 2 (superseded + consolidated, never rejected)", len(got))
		}
	})

	t.Run("deterministic tie-breaker ObservedAt DESC then ID ASC", func(t *testing.T) {
		t.Parallel()
		mA := makeMem(id2, memory.StatusActive, memory.EpistemicObserved, memory.TypeSemantic, now)
		mB := makeMem(id1, memory.StatusActive, memory.EpistemicObserved, memory.TypeSemantic, now)
		mOlder := makeMem(uuid.MustParse("00000000-0000-0000-0000-000000000000"), memory.StatusActive, memory.EpistemicObserved, memory.TypeProcedural, now.Add(-1*time.Hour))
		got := memory.RankCandidates([]memory.CandidateRankingInput{
			{Memory: mA, SemanticScore: 0.8},
			{Memory: mB, SemanticScore: 0.8},
			{Memory: mOlder, SemanticScore: 0.8},
		}, now, false, false)
		if len(got) != 3 || got[0].Memory.ID != id1 || got[1].Memory.ID != id2 {
			t.Fatalf("unexpected tie-break order: %v, %v, %v", got[0].Memory.ID, got[1].Memory.ID, got[2].Memory.ID)
		}
	})
}
