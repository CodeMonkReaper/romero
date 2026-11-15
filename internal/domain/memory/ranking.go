package memory

import (
	"bytes"
	"math"
	"sort"
	"time"

	"github.com/google/uuid"
)

const rrfK = 60.0

// CandidateRankingInput carries a candidate memory and its graph-activated semantic score.
type CandidateRankingInput struct {
	Memory                Memory
	SemanticScore         float32
	MinHopDepth           int
	VisibleRelationsCount int
	HasConflictAlert      bool
}

// ScoredCandidate holds the decomposed governed ranking factors and deterministic FinalScore.
type ScoredCandidate struct {
	Memory                Memory
	SemanticScore         float32
	EpistemicModifier     float32
	TemporalModifier      float32
	StateFactor           float32
	FinalScore            float32
	MinHopDepth           int
	VisibleRelationsCount int
	HasConflictAlert      bool
}

// ComputeRRFScores computes tri-modal Reciprocal Rank Fusion scores (k_rrf = 60).
func ComputeRRFScores(trgmOrder, ftsOrder, vecOrder []uuid.UUID, degradedMode bool) map[uuid.UUID]float32 {
	wTrgm, wFts, wVec := 0.25, 0.30, 0.45
	if degradedMode {
		wTrgm, wFts, wVec = 0.45, 0.55, 0.00
	}

	acc := make(map[uuid.UUID]float64)
	addBranch := func(ids []uuid.UUID, weight float64) {
		if weight <= 0 || len(ids) == 0 {
			return
		}
		seen := make(map[uuid.UUID]struct{}, len(ids))
		rank := 0
		for _, id := range ids {
			if id == uuid.Nil {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			rank++
			acc[id] += weight / (rrfK + float64(rank))
		}
	}

	addBranch(trgmOrder, wTrgm)
	addBranch(ftsOrder, wFts)
	addBranch(vecOrder, wVec)

	out := make(map[uuid.UUID]float32, len(acc))
	for id, score := range acc {
		out[id] = float32(score)
	}
	return out
}

func epistemicWeight(status EpistemicStatus) float64 {
	switch status {
	case EpistemicVerified:
		return 1.20
	case EpistemicObserved:
		return 1.00
	case EpistemicReported:
		return 0.90
	case EpistemicInferred:
		return 0.75
	case EpistemicHypothesis:
		return 0.50
	default:
		return 0.50
	}
}

func temporalLambda(memType MemoryType) float64 {
	switch memType {
	case TypeEpisodic:
		return 0.015
	case TypeSemantic:
		return 0.002
	case TypeProcedural:
		return 0.001
	case TypeRelational:
		return 0.002
	default:
		return 0.002
	}
}

func stateFactor(status MemoryStatus, includeSuperseded, includeConsolidated bool) float64 {
	switch status {
	case StatusActive:
		return 1.00
	case StatusConflicted:
		return 0.55
	case StatusConsolidated:
		if includeConsolidated {
			return 0.65
		}
		return 0.00
	case StatusSuperseded:
		if includeSuperseded {
			return 0.35
		}
		return 0.00
	default:
		return 0.00
	}
}

// RankCandidates applies governed epistemic, Bayesian, temporal, importance, utility, and state scoring.
func RankCandidates(inputs []CandidateRankingInput, now time.Time, includeSuperseded, includeConsolidated bool) []ScoredCandidate {
	out := make([]ScoredCandidate, 0, len(inputs))
	for _, in := range inputs {
		pState := stateFactor(in.Memory.Status, includeSuperseded, includeConsolidated)
		if pState == 0 {
			continue
		}

		theta := float64(in.Memory.Confidence)
		if denom := float64(in.Memory.AlphaScore) + float64(in.Memory.BetaScore); denom > 0 {
			theta = float64(in.Memory.AlphaScore) / denom
		}
		theta = clampFloat64(theta, 0, 1)
		mEpist := epistemicWeight(in.Memory.EpistemicStatus) * (0.45 + 0.55*theta)

		deltaDays := 0.0
		if !in.Memory.Clocks.ObservedAt.IsZero() && now.After(in.Memory.Clocks.ObservedAt) {
			deltaDays = now.Sub(in.Memory.Clocks.ObservedAt).Seconds() / 86400.0
		}
		mTime := 0.70 + 0.30*math.Exp(-temporalLambda(in.Memory.Type)*deltaDays)

		importance := clampFloat64(float64(in.Memory.Importance), 0, 1)
		utility := clampFloat64(float64(in.Memory.UtilityScore), -1, 1)
		sem := math.Max(0, float64(in.SemanticScore))
		finalScore := sem * mEpist * mTime * (0.85 + 0.30*importance) * (1.0 + 0.20*utility) * pState

		hasConflict := in.HasConflictAlert || in.Memory.Status == StatusConflicted
		out = append(out, ScoredCandidate{
			Memory:                in.Memory,
			SemanticScore:         float32(sem),
			EpistemicModifier:     float32(mEpist),
			TemporalModifier:      float32(mTime),
			StateFactor:           float32(pState),
			FinalScore:            float32(finalScore),
			MinHopDepth:           in.MinHopDepth,
			VisibleRelationsCount: in.VisibleRelationsCount,
			HasConflictAlert:      hasConflict,
		})
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].FinalScore != out[j].FinalScore {
			return out[i].FinalScore > out[j].FinalScore
		}
		if !out[i].Memory.Clocks.ObservedAt.Equal(out[j].Memory.Clocks.ObservedAt) {
			return out[i].Memory.Clocks.ObservedAt.After(out[j].Memory.Clocks.ObservedAt)
		}
		return bytes.Compare(out[i].Memory.ID[:], out[j].Memory.ID[:]) < 0
	})
	return out
}

func clampFloat64(v, minV, maxV float64) float64 {
	if math.IsNaN(v) {
		return minV
	}
	if v < minV {
		return minV
	}
	if v > maxV {
		return maxV
	}
	return v
}
