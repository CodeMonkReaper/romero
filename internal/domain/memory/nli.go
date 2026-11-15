package memory

import (
	"context"
	"math"

	"github.com/google/uuid"
)

const (
	// NLIEntailmentThreshold is the minimum entailment probability to auto-link supports.
	NLIEntailmentThreshold float32 = 0.85
	// NLIContradictionThreshold is the minimum contradiction probability to auto-link contradicts.
	NLIContradictionThreshold float32 = 0.80
	// NLISupportsAlphaDelta is the Bayesian positive evidence boost on entailment.
	NLISupportsAlphaDelta float32 = 0.50
)

// RerankCandidate carries a memory ID and text for joint Cross-Encoder scoring.
type RerankCandidate struct {
	MemoryID uuid.UUID
	Text     string
}

// RerankerProvider evaluates query-memory pairs using a neural Cross-Encoder.
type RerankerProvider interface {
	Rerank(ctx context.Context, query string, candidates []RerankCandidate) (map[uuid.UUID]float32, error)
}

// FuseCrossEncoderScore combines normalized Cross-Encoder logit sigmoid(z) with
// graph activation a_hat (0.70*sigmoid(z) + 0.30*a_hat), falling back cleanly
// to graphActivation when reranking is disabled, unavailable, or invalid.
func FuseCrossEncoderScore(rerankLogit, graphActivation float32, rerankAvailable bool) float32 {
	aHat := clampFloat64(float64(graphActivation), 0, 1)
	if !rerankAvailable {
		return float32(aHat)
	}
	z := float64(rerankLogit)
	if math.IsNaN(z) || math.IsInf(z, 0) {
		return float32(aHat)
	}
	sig := 1.0 / (1.0 + math.Exp(-z))
	return float32(0.70*sig + 0.30*aHat)
}

// NLIPairInput represents a directed (Premise, Hypothesis) pair for NLI classification.
type NLIPairInput struct {
	Premise    string
	Hypothesis string
}

// NLIScore holds normalized probabilities over {Entailment, Neutral, Contradiction}.
type NLIScore struct {
	Entailment    float32
	Neutral       float32
	Contradiction float32
}

// NLIProvider classifies logical relationships between pairs of memories.
type NLIProvider interface {
	EvaluatePairs(ctx context.Context, pairs []NLIPairInput) ([]NLIScore, error)
}

// NLIDecisionAction identifies the outcome of an NLI evaluation.
type NLIDecisionAction string

const (
	NLIDecisionNeutral     NLIDecisionAction = "neutral"
	NLIDecisionSupports    NLIDecisionAction = "supports"
	NLIDecisionContradicts NLIDecisionAction = "contradicts"
)

// NLIDecision captures the deterministic governance action for an evaluated pair.
type NLIDecision struct {
	Action             NLIDecisionAction
	SupportedMemoryID  uuid.UUID
	AlphaDelta         float32
	ConflictedMemoryID *uuid.UUID
}

// EvaluateNLIDecision applies canonical NLI thresholds and epistemic authority rules.
// Verified memories are never automatically downgraded to conflicted.
func EvaluateNLIDecision(newMem, existingMem Memory, score NLIScore) NLIDecision {
	if score.Contradiction >= NLIContradictionThreshold {
		conflictedID := selectConflictedMemory(newMem, existingMem)
		return NLIDecision{
			Action:             NLIDecisionContradicts,
			ConflictedMemoryID: conflictedID,
		}
	}
	if score.Entailment >= NLIEntailmentThreshold {
		return NLIDecision{
			Action:            NLIDecisionSupports,
			SupportedMemoryID: existingMem.ID,
			AlphaDelta:        NLISupportsAlphaDelta,
		}
	}
	return NLIDecision{Action: NLIDecisionNeutral}
}

func selectConflictedMemory(newMem, existingMem Memory) *uuid.UUID {
	newVerified := newMem.EpistemicStatus == EpistemicVerified
	oldVerified := existingMem.EpistemicStatus == EpistemicVerified
	if newVerified && oldVerified {
		return nil
	}
	if oldVerified {
		id := newMem.ID
		return &id
	}
	if newVerified {
		id := existingMem.ID
		return &id
	}

	newRank := newMem.EpistemicStatus.Rank()
	oldRank := existingMem.EpistemicStatus.Rank()
	if newRank < oldRank {
		id := newMem.ID
		return &id
	}
	if oldRank < newRank {
		id := existingMem.ID
		return &id
	}

	newConf := effectiveBayesianConfidence(newMem)
	oldConf := effectiveBayesianConfidence(existingMem)
	if newConf < oldConf {
		id := newMem.ID
		return &id
	}
	if oldConf < newConf {
		id := existingMem.ID
		return &id
	}

	id := newMem.ID
	return &id
}

func effectiveBayesianConfidence(m Memory) float64 {
	if denom := float64(m.AlphaScore) + float64(m.BetaScore); denom > 0 {
		return clampFloat64(float64(m.AlphaScore)/denom, 0, 1)
	}
	return clampFloat64(float64(m.Confidence), 0, 1)
}
