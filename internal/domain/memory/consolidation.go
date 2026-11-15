package memory

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/romero-project/romero/internal/domain/identity"
)

const (
	// REMMinClusterSize is the minimum number of episodic memories required to form a cluster.
	REMMinClusterSize = 3
	// REMMinEpisodeConfidence is the minimum Bayesian confidence for episodic eligibility.
	REMMinEpisodeConfidence float32 = 0.70
	// REMMinMeanPairwiseSimilarity is the minimum average cosine similarity inside a cluster.
	REMMinMeanPairwiseSimilarity float32 = 0.80
	// REMMinSourceEntailment is the minimum NLI entailment required against source episodes.
	REMMinSourceEntailment float32 = 0.80
)

// EpisodicCluster groups >= 3 dense episodic memories sharing an exact scope partition.
type EpisodicCluster struct {
	PartitionKey   string
	ScopeTarget    identity.MemoryScopeTarget
	Episodes       []Memory
	MeanSimilarity float32
}

// DistilledMemoryDraft holds the structured output produced by the local SLM for a cluster.
type DistilledMemoryDraft struct {
	MemoryType MemoryType
	TopicKey   string
	Summary    string
	Content    string
	Tags       []string
	Entities   []string
}

// SLMConsolidationProvider distills episodic clusters into procedural/semantic drafts
// and supports out-of-band VRAM unloading on preemption.
type SLMConsolidationProvider interface {
	DistillCluster(ctx context.Context, episodes []Memory) (DistilledMemoryDraft, error)
	UnloadVRAM(ctx context.Context) error
}

// ClusterEpisodicMemories partitions eligible active episodic memories by exact scope
// boundary and forms deterministic clusters of >= 3 episodes with mean cosine similarity >= 0.80.
func ClusterEpisodicMemories(episodes []Memory, vectors map[uuid.UUID][]float32) []EpisodicCluster {
	byPart := make(map[string][]Memory)
	targets := make(map[string]identity.MemoryScopeTarget)

	for _, ep := range episodes {
		if ep.Type != TypeEpisodic || ep.Status != StatusActive {
			continue
		}
		if effectiveBayesianConfidence(ep) < float64(REMMinEpisodeConfidence) {
			continue
		}
		vec := vectors[ep.ID]
		if len(vec) == 0 {
			continue
		}
		target := identity.MemoryScopeTarget{
			TenantID:     ep.TenantID,
			ProjectID:    ep.ProjectID,
			ScopeType:    ep.ScopeType,
			OwnerAgentID: ep.OwnerAgentID,
			TaskID:       ep.TaskID,
		}
		if !validScopeTarget(target) {
			continue
		}
		key := partitionKey(target)
		byPart[key] = append(byPart[key], ep)
		targets[key] = target
	}

	keys := make([]string, 0, len(byPart))
	for k := range byPart {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var clusters []EpisodicCluster
	for _, key := range keys {
		eps := byPart[key]
		if len(eps) < REMMinClusterSize {
			continue
		}
		sort.Slice(eps, func(i, j int) bool {
			return bytes.Compare(eps[i].ID[:], eps[j].ID[:]) < 0
		})

		used := make([]bool, len(eps))
		for i := 0; i < len(eps); i++ {
			if used[i] {
				continue
			}
			group := []Memory{eps[i]}
			groupIdx := []int{i}
			for j := i + 1; j < len(eps); j++ {
				if used[j] {
					continue
				}
				simSum := 0.0
				for _, m := range group {
					simSum += cosineSim(vectors[m.ID], vectors[eps[j].ID])
				}
				if simSum/float64(len(group)) >= float64(REMMinMeanPairwiseSimilarity) {
					group = append(group, eps[j])
					groupIdx = append(groupIdx, j)
				}
			}
			if len(group) < REMMinClusterSize {
				continue
			}
			meanSim := meanPairwiseCosine(group, vectors)
			if meanSim < float64(REMMinMeanPairwiseSimilarity) {
				continue
			}
			for _, idx := range groupIdx {
				used[idx] = true
			}
			clusters = append(clusters, EpisodicCluster{
				PartitionKey:   key,
				ScopeTarget:    targets[key],
				Episodes:       group,
				MeanSimilarity: float32(meanSim),
			})
		}
	}
	return clusters
}

// ValidateDistilledNLI verifies that a distilled draft entails every source episode (>= 0.80)
// and contradicts no active verified memory (< 0.80), failing closed on NaN values.
func ValidateDistilledNLI(sourceScores, verifiedScores []NLIScore) bool {
	if len(sourceScores) == 0 {
		return false
	}
	for _, s := range sourceScores {
		if !(s.Entailment >= REMMinSourceEntailment) {
			return false
		}
	}
	for _, v := range verifiedScores {
		if !(v.Contradiction < NLIContradictionThreshold) {
			return false
		}
	}
	return true
}

// FilterVerifiedForScope filters active verified memories that share the target partition
// so distilled drafts can be NLI-checked against canonical facts without cross-partition leaks.
func FilterVerifiedForScope(verified []Memory, target identity.MemoryScopeTarget) []Memory {
	out := make([]Memory, 0, len(verified))
	for _, v := range verified {
		if v.TenantID != target.TenantID {
			continue
		}
		if v.ProjectID != nil && target.ProjectID != nil && *v.ProjectID != *target.ProjectID {
			continue
		}
		if v.ScopeType == identity.ScopeAgentPrivate || v.ScopeType == identity.ScopeTaskShared {
			vTarget := identity.MemoryScopeTarget{
				TenantID:     v.TenantID,
				ProjectID:    v.ProjectID,
				OwnerAgentID: v.OwnerAgentID,
				ScopeType:    v.ScopeType,
				TaskID:       v.TaskID,
			}
			if !vTarget.SameTopicPartition(target) {
				continue
			}
		}
		out = append(out, v)
	}
	return out
}

func validScopeTarget(t identity.MemoryScopeTarget) bool {
	if t.TenantID == uuid.Nil {
		return false
	}
	switch t.ScopeType {
	case identity.ScopeAgentPrivate:
		return t.ProjectID != nil && *t.ProjectID != uuid.Nil && t.OwnerAgentID != nil && *t.OwnerAgentID != uuid.Nil
	case identity.ScopeTaskShared:
		return t.ProjectID != nil && *t.ProjectID != uuid.Nil && strings.TrimSpace(t.TaskID) != ""
	case identity.ScopeProjectShared:
		return t.ProjectID != nil && *t.ProjectID != uuid.Nil
	case identity.ScopeOrgShared, identity.ScopeGlobalPublic:
		return true
	default:
		return false
	}
}

func partitionKey(t identity.MemoryScopeTarget) string {
	proj := uuid.Nil.String()
	if t.ProjectID != nil {
		proj = t.ProjectID.String()
	}
	owner := uuid.Nil.String()
	if t.ScopeType == identity.ScopeAgentPrivate && t.OwnerAgentID != nil {
		owner = t.OwnerAgentID.String()
	}
	task := ""
	if t.ScopeType == identity.ScopeTaskShared {
		task = t.TaskID
	}
	return fmt.Sprintf("%s|%s|%s|%s|%s", t.TenantID.String(), proj, t.ScopeType, owner, task)
}

func cosineSim(a, b []float32) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		fa := float64(a[i])
		fb := float64(b[i])
		dot += fa * fb
		normA += fa * fa
		normB += fb * fb
	}
	if normA <= 0 || normB <= 0 {
		return 0
	}
	sim := dot / (math.Sqrt(normA) * math.Sqrt(normB))
	return clampFloat64(sim, -1, 1)
}

func meanPairwiseCosine(group []Memory, vectors map[uuid.UUID][]float32) float64 {
	if len(group) < 2 {
		return 0
	}
	var total float64
	var pairs int
	for i := 0; i < len(group); i++ {
		for j := i + 1; j < len(group); j++ {
			total += cosineSim(vectors[group[i].ID], vectors[group[j].ID])
			pairs++
		}
	}
	if pairs == 0 {
		return 0
	}
	return total / float64(pairs)
}
