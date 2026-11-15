package memory

import (
	"math"
	"testing"

	"github.com/google/uuid"
)

func TestFuseCrossEncoderScore(t *testing.T) {
	// sigmoid(0) = 0.5 -> 0.70*0.5 + 0.30*0.8 = 0.35 + 0.24 = 0.59
	got := FuseCrossEncoderScore(0, 0.8, true)
	if math.Abs(float64(got)-0.59) > 1e-4 {
		t.Fatalf("expected 0.59, got %v", got)
	}

	// When unavailable, returns graph activation directly.
	if got := FuseCrossEncoderScore(2.0, 0.42, false); math.Abs(float64(got)-0.42) > 1e-4 {
		t.Fatalf("expected fallback 0.42, got %v", got)
	}

	// NaN / Inf / out-of-range graphActivation guards clamp cleanly.
	if got := FuseCrossEncoderScore(float32(math.NaN()), 0.5, true); math.Abs(float64(got)-0.5) > 1e-4 {
		t.Fatalf("expected fallback on NaN logit 0.5, got %v", got)
	}
	if got := FuseCrossEncoderScore(float32(math.Inf(1)), 1.5, true); math.Abs(float64(got)-1.0) > 1e-4 {
		t.Fatalf("expected fallback clamped to 1.0 on +Inf logit, got %v", got)
	}
	if got := FuseCrossEncoderScore(float32(math.Inf(-1)), -0.5, true); math.Abs(float64(got)-0.0) > 1e-4 {
		t.Fatalf("expected fallback clamped to 0.0 on -Inf logit, got %v", got)
	}
}

func TestEvaluateNLIDecision(t *testing.T) {
	newID := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	oldID := uuid.MustParse("00000000-0000-0000-0000-000000000001")

	newMem := Memory{
		ID:              newID,
		EpistemicStatus: EpistemicReported,
		AlphaScore:      5.25,
		BetaScore:       2.25,
		Confidence:      0.70,
	}
	oldMem := Memory{
		ID:              oldID,
		EpistemicStatus: EpistemicObserved,
		AlphaScore:      8.5,
		BetaScore:       1.5,
		Confidence:      0.85,
	}

	// Supports when entailment >= 0.85
	sup := EvaluateNLIDecision(newMem, oldMem, NLIScore{Entailment: 0.88, Neutral: 0.08, Contradiction: 0.04})
	if sup.Action != NLIDecisionSupports || sup.AlphaDelta != 0.5 || sup.SupportedMemoryID != oldID {
		t.Fatalf("unexpected supports decision: %+v", sup)
	}

	// Neutral when below thresholds
	neu := EvaluateNLIDecision(newMem, oldMem, NLIScore{Entailment: 0.50, Neutral: 0.40, Contradiction: 0.10})
	if neu.Action != NLIDecisionNeutral {
		t.Fatalf("expected neutral, got %+v", neu)
	}

	// Contradicts: lower epistemic rank becomes conflicted (newMem reported < oldMem observed)
	con := EvaluateNLIDecision(newMem, oldMem, NLIScore{Entailment: 0.05, Neutral: 0.10, Contradiction: 0.85})
	if con.Action != NLIDecisionContradicts || con.ConflictedMemoryID == nil || *con.ConflictedMemoryID != newID {
		t.Fatalf("expected newMem %s conflicted, got %+v", newID, con)
	}

	// Equal epistemic rank -> lower Bayesian confidence becomes conflicted
	oldLowConf := Memory{ID: oldID, EpistemicStatus: EpistemicObserved, AlphaScore: 6.0, BetaScore: 4.0, Confidence: 0.60}
	newHighConf := Memory{ID: newID, EpistemicStatus: EpistemicObserved, AlphaScore: 9.0, BetaScore: 1.0, Confidence: 0.90}
	conTie := EvaluateNLIDecision(newHighConf, oldLowConf, NLIScore{Contradiction: 0.82})
	if conTie.ConflictedMemoryID == nil || *conTie.ConflictedMemoryID != oldID {
		t.Fatalf("expected lower confidence oldMem %s conflicted, got %+v", oldID, conTie)
	}

	// Equal epistemic rank and equal confidence -> newly evaluated memory becomes conflicted
	oldSame := Memory{ID: oldID, EpistemicStatus: EpistemicObserved, AlphaScore: 8.5, BetaScore: 1.5, Confidence: 0.85}
	newSame := Memory{ID: newID, EpistemicStatus: EpistemicObserved, AlphaScore: 8.5, BetaScore: 1.5, Confidence: 0.85}
	conTie2 := EvaluateNLIDecision(newSame, oldSame, NLIScore{Contradiction: 0.82})
	if conTie2.ConflictedMemoryID == nil || *conTie2.ConflictedMemoryID != newID {
		t.Fatalf("expected newMem %s conflicted on full tie, got %+v", newID, conTie2)
	}

	// Both verified -> contradicts edge created, neither auto-degraded (ConflictedMemoryID == nil)
	v1 := Memory{ID: oldID, EpistemicStatus: EpistemicVerified, AlphaScore: 19, BetaScore: 1, Confidence: 0.95}
	v2 := Memory{ID: newID, EpistemicStatus: EpistemicVerified, AlphaScore: 19, BetaScore: 1, Confidence: 0.95}
	conVer := EvaluateNLIDecision(v2, v1, NLIScore{Contradiction: 0.91})
	if conVer.Action != NLIDecisionContradicts || conVer.ConflictedMemoryID != nil {
		t.Fatalf("expected ConflictedMemoryID nil for verified pair, got %+v", conVer)
	}
}
