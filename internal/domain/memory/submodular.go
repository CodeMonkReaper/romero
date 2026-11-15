package memory

import (
	"bytes"
	"math"
	"strings"
	"unicode"

	"github.com/google/uuid"
)

const (
	submodularEta = 0.35
	submodularRho = 0.65
)

// SubmodularCandidate represents a scored candidate eligible for greedy context packaging.
type SubmodularCandidate struct {
	Memory           Memory
	FinalScore       float32
	DenseVec         []float32
	HasConflictAlert bool
	SummaryTokens    int
	FullTokens       int
}

// SelectedContextCandidate represents a candidate chosen for the bounded context window.
type SelectedContextCandidate struct {
	MemoryID         uuid.UUID
	IsCompact        bool
	TokenCost        int
	FinalScore       float32
	HasConflictAlert bool
}

// SelectSubmodularContext selects an optimal diverse subset of candidates under maxTokens.
func SelectSubmodularContext(candidates []SubmodularCandidate, maxTokens int, degradedMode bool) []SelectedContextCandidate {
	if len(candidates) == 0 || maxTokens <= 0 {
		return nil
	}

	n := len(candidates)
	tokenSets := make([]map[string]struct{}, n)
	for i := range candidates {
		tokenSets[i] = buildLexicalTokenSet(candidates[i].Memory)
	}

	simMatrix := make([][]float64, n)
	for i := range simMatrix {
		simMatrix[i] = make([]float64, n)
		simMatrix[i][i] = 1.0
	}
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			s := candidateSimilarity(candidates[i], candidates[j], tokenSets[i], tokenSets[j], degradedMode)
			simMatrix[i][j] = s
			simMatrix[j][i] = s
		}
	}

	selected := make([]bool, n)
	maxCovered := make([]float64, n)
	var out []SelectedContextCandidate
	usedTokens := 0

	for {
		bestIdx := -1
		bestRatio := -1.0
		bestCost := 0
		bestCompact := false

		for i := 0; i < n; i++ {
			if selected[i] {
				continue
			}
			cost, compact, fits := chooseTokenCost(candidates[i], maxTokens-usedTokens)
			if !fits {
				continue
			}

			scoreI := math.Max(0, float64(candidates[i].FinalScore))
			marginal := scoreI
			for j := 0; j < n; j++ {
				cov := scoreI * simMatrix[i][j]
				if cov > maxCovered[j] {
					marginal += submodularEta * (cov - maxCovered[j])
				}
			}

			denomCost := math.Max(1, float64(cost))
			ratio := marginal / math.Pow(denomCost, submodularRho)
			if bestIdx == -1 || ratio > bestRatio ||
				(ratio == bestRatio && candidates[i].FinalScore > candidates[bestIdx].FinalScore) ||
				(ratio == bestRatio && candidates[i].FinalScore == candidates[bestIdx].FinalScore && bytes.Compare(candidates[i].Memory.ID[:], candidates[bestIdx].Memory.ID[:]) < 0) {
				bestIdx = i
				bestRatio = ratio
				bestCost = cost
				bestCompact = compact
			}
		}

		if bestIdx == -1 {
			break
		}

		selected[bestIdx] = true
		usedTokens += bestCost
		scoreBest := math.Max(0, float64(candidates[bestIdx].FinalScore))
		for j := 0; j < n; j++ {
			cov := scoreBest * simMatrix[bestIdx][j]
			if cov > maxCovered[j] {
				maxCovered[j] = cov
			}
		}

		out = append(out, SelectedContextCandidate{
			MemoryID:         candidates[bestIdx].Memory.ID,
			IsCompact:        bestCompact,
			TokenCost:        bestCost,
			FinalScore:       candidates[bestIdx].FinalScore,
			HasConflictAlert: candidates[bestIdx].HasConflictAlert,
		})
	}

	return out
}

func chooseTokenCost(c SubmodularCandidate, remaining int) (cost int, compact bool, fits bool) {
	full := c.FullTokens
	if full < 0 {
		full = 0
	}
	if full > 0 && full <= remaining {
		return full, false, true
	}
	sum := c.SummaryTokens
	if sum < 0 {
		sum = 0
	}
	if sum > 0 && sum <= remaining {
		return sum, true, true
	}
	if full == 0 && sum == 0 && remaining >= 0 {
		return 0, false, true
	}
	return 0, false, false
}

func candidateSimilarity(a, b SubmodularCandidate, setA, setB map[string]struct{}, degradedMode bool) float64 {
	if !degradedMode && len(a.DenseVec) > 0 && len(a.DenseVec) == len(b.DenseVec) {
		return cosineSimilarity(a.DenseVec, b.DenseVec)
	}
	return jaccardSimilarity(setA, setB)
}

func cosineSimilarity(a, b []float32) float64 {
	var dot, normA, normB float64
	for i := range a {
		va := float64(a[i])
		vb := float64(b[i])
		dot += va * vb
		normA += va * va
		normB += vb * vb
	}
	if normA <= 0 || normB <= 0 {
		return 0
	}
	sim := dot / (math.Sqrt(normA) * math.Sqrt(normB))
	if sim < 0 {
		return 0
	}
	if sim > 1 {
		return 1
	}
	return sim
}

func buildLexicalTokenSet(m Memory) map[string]struct{} {
	set := make(map[string]struct{})
	addTokens := func(s string) {
		fields := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsNumber(r)
		})
		for _, f := range fields {
			if f != "" {
				set[f] = struct{}{}
			}
		}
	}
	for _, t := range m.Tags {
		addTokens(t)
	}
	for _, e := range m.Entities {
		addTokens(e)
	}
	addTokens(m.Summary)
	return set
}

func jaccardSimilarity(a, b map[string]struct{}) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 0
	}
	inter := 0
	for k := range a {
		if _, ok := b[k]; ok {
			inter++
		}
	}
	union := len(a) + len(b) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}
