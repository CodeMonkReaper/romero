// Package graph implements pure domain Spreading Activation over pre-pruned authorized subgraphs.
package graph

import (
	"math"

	"github.com/google/uuid"
	"github.com/romero-project/romero/internal/domain/memory"
)

const (
	activationGamma = 0.35
	maxHops         = 3
)

// AuthorizedNode represents a memory node already verified as readable by the caller.
type AuthorizedNode struct {
	ID     uuid.UUID
	Status memory.MemoryStatus
}

// AuthorizedEdge represents an active directed relation whose endpoints are both readable by the caller.
type AuthorizedEdge struct {
	FromID       uuid.UUID
	ToID         uuid.UUID
	RelationType memory.RelationType
	Weight       float32
}

// AuthorizedSubgraph contains only nodes and edges within the caller's Zero-Trust read boundary.
type AuthorizedSubgraph struct {
	Nodes map[uuid.UUID]AuthorizedNode
	Edges []AuthorizedEdge
}

// ActivationResult holds the normalized spreading activation energy and neighborhood metadata for a node.
type ActivationResult struct {
	NormalizedScore       float32
	MinHopDepth           int
	VisibleRelationsCount int
	HasConflictAlert      bool
}

// Conductance returns the canonical propagation weight psi(relation_type) for Spreading Activation.
func Conductance(rel memory.RelationType, includeSuperseded bool) float32 {
	switch rel {
	case memory.RelationDependsOn:
		return 1.00
	case memory.RelationCausedBy:
		return 0.95
	case memory.RelationSupports:
		return 0.85
	case memory.RelationDerivedFrom:
		return 0.80
	case memory.RelationContradicts:
		return 0.60
	case memory.RelationRelatedTo:
		return 0.50
	case memory.RelationSupersedes:
		if includeSuperseded {
			return 0.40
		}
		return 0.00
	default:
		return 0.00
	}
}

// PropagateActivation executes 0..3 hops of damped bidirectional spreading activation over sub.
func PropagateActivation(seeds map[uuid.UUID]float32, sub AuthorizedSubgraph, hops int, includeSuperseded bool) map[uuid.UUID]ActivationResult {
	if len(sub.Nodes) == 0 || len(seeds) == 0 {
		return map[uuid.UUID]ActivationResult{}
	}
	if hops < 0 {
		hops = 0
	} else if hops > maxHops {
		hops = maxHops
	}

	activeEdges := make([]AuthorizedEdge, 0, len(sub.Edges))
	visibleDeg := make(map[uuid.UUID]int, len(sub.Nodes))
	for _, e := range sub.Edges {
		if e.FromID == e.ToID {
			continue
		}
		if _, ok := sub.Nodes[e.FromID]; !ok {
			continue
		}
		if _, ok := sub.Nodes[e.ToID]; !ok {
			continue
		}
		if e.Weight <= 0 || Conductance(e.RelationType, includeSuperseded) <= 0 {
			continue
		}
		activeEdges = append(activeEdges, e)
		visibleDeg[e.FromID]++
		visibleDeg[e.ToID]++
	}

	maxSeed := 0.0
	for id, s := range seeds {
		if _, ok := sub.Nodes[id]; !ok {
			continue
		}
		v := float64(s)
		if !math.IsNaN(v) && !math.IsInf(v, 0) && v > maxSeed {
			maxSeed = v
		}
	}
	if maxSeed <= 0 {
		return map[uuid.UUID]ActivationResult{}
	}

	energy := make(map[uuid.UUID]float64, len(sub.Nodes))
	minHop := make(map[uuid.UUID]int, len(sub.Nodes))
	conflict := make(map[uuid.UUID]bool, len(sub.Nodes))

	for id, node := range sub.Nodes {
		minHop[id] = -1
		if node.Status == memory.StatusConflicted {
			conflict[id] = true
		}
	}
	for id, s := range seeds {
		if _, ok := sub.Nodes[id]; !ok {
			continue
		}
		v := float64(s)
		if math.IsNaN(v) || math.IsInf(v, 0) || v <= 0 {
			continue
		}
		energy[id] = v / maxSeed
		minHop[id] = 0
	}

	for h := 1; h <= hops; h++ {
		delta := make(map[uuid.UUID]float64)
		for _, e := range activeEdges {
			psi := float64(Conductance(e.RelationType, includeSuperseded))
			w := math.Min(1.0, math.Max(0.0, float64(e.Weight)))
			if eFrom := energy[e.FromID]; eFrom > 0 {
				deg := math.Max(1, float64(visibleDeg[e.FromID]))
				delta[e.ToID] += activationGamma * eFrom * (psi * w) / deg
				if minHop[e.ToID] == -1 {
					minHop[e.ToID] = h
				}
				if e.RelationType == memory.RelationContradicts {
					conflict[e.ToID] = true
				}
			}
			if eTo := energy[e.ToID]; eTo > 0 {
				deg := math.Max(1, float64(visibleDeg[e.ToID]))
				delta[e.FromID] += activationGamma * eTo * (psi * w) / deg
				if minHop[e.FromID] == -1 {
					minHop[e.FromID] = h
				}
				if e.RelationType == memory.RelationContradicts {
					conflict[e.FromID] = true
				}
			}
		}
		for id, d := range delta {
			energy[id] += d
		}
	}

	maxEnergy := 0.0
	for _, e := range energy {
		if e > maxEnergy {
			maxEnergy = e
		}
	}
	if maxEnergy <= 0 {
		return map[uuid.UUID]ActivationResult{}
	}

	out := make(map[uuid.UUID]ActivationResult, len(energy))
	for id, e := range energy {
		if e <= 0 {
			continue
		}
		out[id] = ActivationResult{
			NormalizedScore:       float32(e / maxEnergy),
			MinHopDepth:           minHop[id],
			VisibleRelationsCount: visibleDeg[id],
			HasConflictAlert:      conflict[id],
		}
	}
	return out
}
