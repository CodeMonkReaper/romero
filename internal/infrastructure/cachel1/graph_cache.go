// Package cachel1 implements the thread-safe in-RAM L1 Cognitive Graph Cache with Zero-Trust pruning.
package cachel1

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/romero-project/romero/internal/domain/graph"
	"github.com/romero-project/romero/internal/domain/identity"
	"github.com/romero-project/romero/internal/domain/memory"
)

// GraphStats reports current in-RAM L1 Cognitive Graph occupancy.
type GraphStats struct {
	Enabled         bool
	NodesCount      int
	EdgesCount      int
	EmbeddingsCount int
}

type partitionKey struct {
	tenantID  uuid.UUID
	projectID uuid.UUID
}

// CognitiveGraph maintains an in-RAM write-through graph synchronized with PostgreSQL.
type CognitiveGraph struct {
	mu     sync.RWMutex
	loader memory.SearchRepository
	loaded map[partitionKey]bool
	nodes  map[uuid.UUID]memory.Memory
	edges  map[uuid.UUID]memory.MemoryRelation
	vecs   map[uuid.UUID][]float32
}

// NewCognitiveGraph creates an L1 Cognitive Graph backed by an optional SearchRepository snapshot loader.
func NewCognitiveGraph(loader memory.SearchRepository) *CognitiveGraph {
	return &CognitiveGraph{
		loader: loader,
		loaded: make(map[partitionKey]bool),
		nodes:  make(map[uuid.UUID]memory.Memory),
		edges:  make(map[uuid.UUID]memory.MemoryRelation),
		vecs:   make(map[uuid.UUID][]float32),
	}
}

func (g *CognitiveGraph) ApplyMemoryUpsert(m memory.Memory) {
	if m.ID == uuid.Nil || m.TenantID == uuid.Nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.nodes[m.ID] = m
}

func (g *CognitiveGraph) ApplyMemoryStatusChange(tenantID uuid.UUID, projectID *uuid.UUID, memoryID uuid.UUID, status memory.MemoryStatus, validUntil *time.Time) {
	if memoryID == uuid.Nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if m, ok := g.nodes[memoryID]; ok {
		m.Status = status
		if validUntil != nil {
			v := *validUntil
			m.Clocks.ValidUntil = &v
		}
		g.nodes[memoryID] = m
	}
}

func (g *CognitiveGraph) ApplyRelationsAdded(rels []memory.MemoryRelation) {
	if len(rels) == 0 {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, r := range rels {
		if r.ID == uuid.Nil {
			continue
		}
		g.edges[r.ID] = r
	}
}

func (g *CognitiveGraph) ApplyEmbeddingUpsert(tenantID uuid.UUID, projectID *uuid.UUID, memoryID uuid.UUID, denseVec []float32) {
	if memoryID == uuid.Nil || len(denseVec) == 0 {
		return
	}
	cp := make([]float32, len(denseVec))
	copy(cp, denseVec)
	g.mu.Lock()
	defer g.mu.Unlock()
	g.vecs[memoryID] = cp
}

func (g *CognitiveGraph) ensureLoaded(ctx context.Context, tenantID uuid.UUID, projectID *uuid.UUID, now time.Time) error {
	if g.loader == nil {
		return nil
	}
	var projUUID uuid.UUID
	if projectID != nil {
		projUUID = *projectID
	}
	key := partitionKey{tenantID: tenantID, projectID: projUUID}

	g.mu.RLock()
	already := g.loaded[key]
	g.mu.RUnlock()
	if already {
		return nil
	}

	mems, rels, vecs, err := g.loader.LoadProjectGraphSnapshot(ctx, tenantID, projectID, now)
	if err != nil {
		return err
	}

	g.mu.Lock()
	defer g.mu.Unlock()
	if g.loaded[key] {
		return nil
	}
	for _, m := range mems {
		if _, exists := g.nodes[m.ID]; !exists {
			g.nodes[m.ID] = m
		}
	}
	for _, r := range rels {
		if _, exists := g.edges[r.ID]; !exists {
			g.edges[r.ID] = r
		}
	}
	for id, v := range vecs {
		if _, exists := g.vecs[id]; !exists {
			cp := make([]float32, len(v))
			copy(cp, v)
			g.vecs[id] = cp
		}
	}
	g.loaded[key] = true
	return nil
}

// ProjectAuthorizedSubgraph returns a pre-pruned Zero-Trust subgraph, readable memory map, and embeddings.
func (g *CognitiveGraph) ProjectAuthorizedSubgraph(
	ctx context.Context,
	auth identity.AuthContext,
	filter memory.SearchFilter,
	allowedTasks map[string]bool,
	now time.Time,
) (graph.AuthorizedSubgraph, map[uuid.UUID]memory.Memory, map[uuid.UUID][]float32, bool, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if len(filter.ProjectIDs) == 0 {
		if err := g.ensureLoaded(ctx, auth.TenantID, nil, now); err != nil {
			return graph.AuthorizedSubgraph{}, nil, nil, false, err
		}
	} else {
		for _, pid := range filter.ProjectIDs {
			pidCopy := pid
			if err := g.ensureLoaded(ctx, auth.TenantID, &pidCopy, now); err != nil {
				return graph.AuthorizedSubgraph{}, nil, nil, false, err
			}
		}
	}

	scopeFilter := make(map[identity.ScopeType]bool, len(filter.Scopes))
	for _, s := range filter.Scopes {
		scopeFilter[s] = true
	}
	projFilter := make(map[uuid.UUID]bool, len(filter.ProjectIDs))
	for _, p := range filter.ProjectIDs {
		projFilter[p] = true
	}

	g.mu.RLock()
	defer g.mu.RUnlock()

	sub := graph.AuthorizedSubgraph{
		Nodes: make(map[uuid.UUID]graph.AuthorizedNode),
	}
	mems := make(map[uuid.UUID]memory.Memory)
	vecs := make(map[uuid.UUID][]float32)
	hasMissingVec := false

	for id, m := range g.nodes {
		if m.TenantID != auth.TenantID {
			continue
		}
		if !isStatusEligible(m.Status, filter.IncludeSuperseded, filter.IncludeConsolidated) {
			continue
		}
		if m.Status != memory.StatusSuperseded {
			if m.Clocks.ValidFrom.After(now) || (m.Clocks.ValidUntil != nil && !m.Clocks.ValidUntil.After(now)) {
				continue
			}
		}
		if len(scopeFilter) > 0 && !scopeFilter[m.ScopeType] {
			continue
		}
		if len(projFilter) > 0 && m.ProjectID != nil && !projFilter[*m.ProjectID] {
			continue
		}
		target := identity.MemoryScopeTarget{
			TenantID:     m.TenantID,
			ProjectID:    m.ProjectID,
			OwnerAgentID: m.OwnerAgentID,
			ScopeType:    m.ScopeType,
			TaskID:       m.TaskID,
		}
		if !canReadInGraph(auth, target, allowedTasks) {
			continue
		}

		sub.Nodes[id] = graph.AuthorizedNode{ID: id, Status: m.Status}
		mems[id] = m
		if v, ok := g.vecs[id]; ok && len(v) > 0 {
			vecs[id] = v
		} else {
			hasMissingVec = true
		}
	}

	for _, r := range g.edges {
		if r.TenantID != auth.TenantID || !r.IsActive {
			continue
		}
		if r.ValidFrom.After(now) || (r.ValidUntil != nil && !r.ValidUntil.After(now)) {
			continue
		}
		if _, ok := sub.Nodes[r.FromMemoryID]; !ok {
			continue
		}
		if _, ok := sub.Nodes[r.ToMemoryID]; !ok {
			continue
		}
		sub.Edges = append(sub.Edges, graph.AuthorizedEdge{
			FromID:       r.FromMemoryID,
			ToID:         r.ToMemoryID,
			RelationType: r.RelationType,
			Weight:       r.Weight,
		})
	}

	return sub, mems, vecs, hasMissingVec, nil
}

func canReadInGraph(auth identity.AuthContext, target identity.MemoryScopeTarget, allowedTasks map[string]bool) bool {
	if target.ScopeType == identity.ScopeTaskShared {
		if target.TenantID != auth.TenantID || target.ProjectID == nil || target.TaskID == "" {
			return false
		}
		if !slices.Contains(auth.AllowedScopes, identity.ScopeTaskShared) {
			return false
		}
		if !auth.IsAdmin && !slices.Contains(auth.AllowedProjects, *target.ProjectID) {
			return false
		}
		return allowedTasks[target.TaskID]
	}
	return auth.CanReadMemory(target, "")
}

func isStatusEligible(status memory.MemoryStatus, includeSuperseded, includeConsolidated bool) bool {
	switch status {
	case memory.StatusActive, memory.StatusConflicted:
		return true
	case memory.StatusSuperseded:
		return includeSuperseded
	case memory.StatusConsolidated:
		return includeConsolidated
	default:
		return false
	}
}

// Stats returns current in-RAM L1 Cognitive Graph occupancy.
func (g *CognitiveGraph) Stats() GraphStats {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return GraphStats{
		Enabled:         true,
		NodesCount:      len(g.nodes),
		EdgesCount:      len(g.edges),
		EmbeddingsCount: len(g.vecs),
	}
}
