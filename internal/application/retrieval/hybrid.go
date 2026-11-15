package retrieval

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"

	"github.com/romero-project/romero/internal/domain/graph"
	"github.com/romero-project/romero/internal/domain/identity"
	"github.com/romero-project/romero/internal/domain/memory"
)

const (
	maxQueryRunes  = 2048
	defaultLimit   = 10
	maxSearchLimit = 100
	maxSearchHops  = 3
	topSeedsLimit  = 50
)

// ErrForbidden mirrors identity.ErrForbidden when scope/project/task authorization fails.
var ErrForbidden = identity.ErrForbidden

// GraphProjector projects a Zero-Trust pre-pruned L1 Cognitive Graph subgraph.
type GraphProjector interface {
	ProjectAuthorizedSubgraph(
		ctx context.Context,
		auth identity.AuthContext,
		filter memory.SearchFilter,
		allowedTasks map[string]bool,
		now time.Time,
	) (graph.AuthorizedSubgraph, map[uuid.UUID]memory.Memory, map[uuid.UUID][]float32, bool, error)
}

// SetHybridDeps configures the Phase 02 hybrid search, embedding, and L1 graph dependencies.
func (s *Service) SetHybridDeps(search memory.SearchRepository, embedder memory.EmbeddingProvider, graphCache GraphProjector) {
	s.search = search
	s.embedder = embedder
	s.graphCache = graphCache
}

// SearchQuery carries validated parameters for Zero-Trust tri-modal hybrid search.
type SearchQuery struct {
	Query               string
	ProjectIDs          []uuid.UUID
	Scopes              []identity.ScopeType
	TaskID              string
	Limit               int
	Hops                int
	IncludeSuperseded   bool
	IncludeConsolidated bool
	CorrelationID       string
}

// SearchCandidate is the compact progressive-disclosure search item (never exposes Content).
type SearchCandidate struct {
	ID               uuid.UUID              `json:"id"`
	Summary          string                 `json:"summary"`
	Type             memory.MemoryType      `json:"type"`
	ScopeType        identity.ScopeType     `json:"scope_type"`
	Status           memory.MemoryStatus    `json:"status"`
	EpistemicStatus  memory.EpistemicStatus `json:"epistemic_status"`
	Confidence       float32                `json:"confidence"`
	FinalScore       float32                `json:"final_score"`
	RelationsCount   int                    `json:"relations_count"`
	MinHopDepth      int                    `json:"min_hop_depth"`
	HasConflictAlert bool                   `json:"has_conflict_alert"`
	ObservedAt       time.Time              `json:"observed_at"`
}

// SearchResult holds ranked progressive-disclosure candidates and degradation metadata.
type SearchResult struct {
	Candidates   []SearchCandidate `json:"candidates"`
	DegradedMode bool              `json:"degraded_mode"`
	TotalFound   int               `json:"total_found"`
}

// ContextCandidate carries full memory, score, and optional dense vector for ContextBuilderService.
type ContextCandidate struct {
	ID               uuid.UUID
	Summary          string
	Content          string
	TokenCount       int
	FinalScore       float32
	DenseVec         []float32
	Type             memory.MemoryType
	ScopeType        identity.ScopeType
	Status           memory.MemoryStatus
	EpistemicStatus  memory.EpistemicStatus
	Confidence       float32
	PrimarySourceID  uuid.UUID
	HasConflictAlert bool
	Tags             []string
	Entities         []string
	TopicKey         string
	ObservedAt       time.Time
}

// SubgraphQuery carries parameters for neighborhood exploration around a seed memory.
type SubgraphQuery struct {
	MemoryID      uuid.UUID
	Hops          int
	CorrelationID string
}

// SubgraphNode represents a visible node within the explored Zero-Trust subgraph.
type SubgraphNode struct {
	ID               uuid.UUID              `json:"id"`
	Summary          string                 `json:"summary"`
	Type             memory.MemoryType      `json:"type"`
	ScopeType        identity.ScopeType     `json:"scope_type"`
	Status           memory.MemoryStatus    `json:"status"`
	EpistemicStatus  memory.EpistemicStatus `json:"epistemic_status"`
	Confidence       float32                `json:"confidence"`
	HopDepth         int                    `json:"hop_depth"`
	HasConflictAlert bool                   `json:"has_conflict_alert"`
}

// SubgraphEdge represents an active directed relation between two visible subgraph nodes.
type SubgraphEdge struct {
	FromMemoryID uuid.UUID           `json:"from_memory_id"`
	ToMemoryID   uuid.UUID           `json:"to_memory_id"`
	RelationType memory.RelationType `json:"relation_type"`
	Weight       float32             `json:"weight"`
}

// SubgraphResult holds the authorized causal neighborhood around RootMemoryID.
type SubgraphResult struct {
	RootMemoryID uuid.UUID      `json:"root_memory_id"`
	Nodes        []SubgraphNode `json:"nodes"`
	Edges        []SubgraphEdge `json:"edges"`
}

// SearchMemories executes tri-modal hybrid retrieval with Zero-Trust pre-pruning and pure read-only semantics.
func (s *Service) SearchMemories(ctx context.Context, auth identity.AuthContext, q SearchQuery) (*SearchResult, error) {
	ranked, _, degraded, err := s.executeHybridSearch(ctx, auth, q, "search_memories")
	if err != nil {
		return nil, err
	}
	limit := q.Limit
	if limit == 0 {
		limit = defaultLimit
	}
	totalFound := len(ranked)
	if len(ranked) > limit {
		ranked = ranked[:limit]
	}
	candidates := make([]SearchCandidate, 0, len(ranked))
	for _, sc := range ranked {
		candidates = append(candidates, SearchCandidate{
			ID:               sc.Memory.ID,
			Summary:          sc.Memory.Summary,
			Type:             sc.Memory.Type,
			ScopeType:        sc.Memory.ScopeType,
			Status:           sc.Memory.Status,
			EpistemicStatus:  sc.Memory.EpistemicStatus,
			Confidence:       sc.Memory.Confidence,
			FinalScore:       sc.FinalScore,
			RelationsCount:   sc.VisibleRelationsCount,
			MinHopDepth:      sc.MinHopDepth,
			HasConflictAlert: sc.HasConflictAlert,
			ObservedAt:       sc.Memory.Clocks.ObservedAt,
		})
	}
	return &SearchResult{
		Candidates:   candidates,
		DegradedMode: degraded,
		TotalFound:   totalFound,
	}, nil
}

// SearchForContext returns full scored candidates + embeddings for ContextBuilderService without double-auditing.
func (s *Service) SearchForContext(ctx context.Context, auth identity.AuthContext, q SearchQuery) ([]ContextCandidate, bool, error) {
	ranked, embeddings, degraded, err := s.executeHybridSearch(ctx, auth, q, "")
	if err != nil {
		return nil, false, err
	}
	limit := q.Limit
	if limit == 0 {
		limit = maxSearchLimit
	}
	if len(ranked) > limit {
		ranked = ranked[:limit]
	}
	out := make([]ContextCandidate, 0, len(ranked))
	for _, sc := range ranked {
		out = append(out, ContextCandidate{
			ID:               sc.Memory.ID,
			Summary:          sc.Memory.Summary,
			Content:          sc.Memory.Content,
			TokenCount:       sc.Memory.TokenCount,
			FinalScore:       sc.FinalScore,
			DenseVec:         embeddings[sc.Memory.ID],
			Type:             sc.Memory.Type,
			ScopeType:        sc.Memory.ScopeType,
			Status:           sc.Memory.Status,
			EpistemicStatus:  sc.Memory.EpistemicStatus,
			Confidence:       sc.Memory.Confidence,
			PrimarySourceID:  sc.Memory.PrimarySourceID,
			HasConflictAlert: sc.HasConflictAlert,
			Tags:             sc.Memory.Tags,
			Entities:         sc.Memory.Entities,
			TopicKey:         sc.Memory.TopicKey,
			ObservedAt:       sc.Memory.Clocks.ObservedAt,
		})
	}
	return out, degraded, nil
}

// GetSubgraph explores up to q.Hops visible causal relations around q.MemoryID with 404 Zero-Trust opacity.
func (s *Service) GetSubgraph(ctx context.Context, auth identity.AuthContext, q SubgraphQuery) (*SubgraphResult, error) {
	if auth.TenantID == uuid.Nil || auth.PrincipalID == uuid.Nil {
		return nil, ErrUnauthorized
	}
	if q.MemoryID == uuid.Nil || q.Hops < 0 || q.Hops > maxSearchHops {
		return nil, fmt.Errorf("%w: memory_id must be valid and hops within [1, 3]", ErrValidation)
	}
	hops := q.Hops
	if hops == 0 {
		hops = 2
	}
	if s.graphCache == nil {
		return nil, fmt.Errorf("%w: graph projector not configured", ErrOperational)
	}

	var rootMem *memory.Memory
	err := s.uow.ExecTx(ctx, func(reg memory.RepositoryRegistry) error {
		m, getErr := reg.Memories().GetMemoryByID(ctx, auth.TenantID, q.MemoryID)
		if getErr != nil {
			if errors.Is(getErr, memory.ErrNotFound) {
				return ErrNotFound
			}
			return fmt.Errorf("%w: %v", ErrOperational, getErr)
		}
		if accessErr := s.authorizeRead(ctx, auth, m); accessErr != nil {
			return accessErr
		}
		rootMem = m
		return nil
	})
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			if auditErr := s.recordDenied(ctx, auth, "get_subgraph", "memory", &q.MemoryID, "", q.CorrelationID); auditErr != nil {
				return nil, auditErr
			}
		}
		return nil, err
	}

	now := s.clock()
	var projectIDs []uuid.UUID
	if rootMem.ProjectID != nil {
		projectIDs = []uuid.UUID{*rootMem.ProjectID}
	}
	allowedTasks := make(map[string]bool)
	if rootMem.ScopeType == identity.ScopeTaskShared && rootMem.TaskID != "" {
		allowedTasks[rootMem.TaskID] = true
	}

	filter := memory.SearchFilter{
		TenantID:          auth.TenantID,
		ProjectIDs:        projectIDs,
		Scopes:            auth.AllowedScopes,
		CallerAgentID:     auth.AgentID,
		IncludeSuperseded: true,
		Now:               now,
	}
	sub, visibleMems, _, _, projErr := s.graphCache.ProjectAuthorizedSubgraph(ctx, auth, filter, allowedTasks, now)
	if projErr != nil {
		return nil, fmt.Errorf("%w: %v", ErrOperational, projErr)
	}

	// Bidirectional BFS over visible Zero-Trust edges up to hops without cycles.
	adj := make(map[uuid.UUID][]uuid.UUID)
	conflictNodes := make(map[uuid.UUID]bool)
	for _, e := range sub.Edges {
		adj[e.FromID] = append(adj[e.FromID], e.ToID)
		adj[e.ToID] = append(adj[e.ToID], e.FromID)
		if e.RelationType == memory.RelationContradicts {
			conflictNodes[e.FromID] = true
			conflictNodes[e.ToID] = true
		}
	}

	depthMap := map[uuid.UUID]int{q.MemoryID: 0}
	queue := []uuid.UUID{q.MemoryID}
	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]
		d := depthMap[curr]
		if d >= hops {
			continue
		}
		for _, neighborID := range adj[curr] {
			if _, ok := visibleMems[neighborID]; !ok {
				continue
			}
			if _, seen := depthMap[neighborID]; !seen {
				depthMap[neighborID] = d + 1
				queue = append(queue, neighborID)
			}
		}
	}

	nodes := make([]SubgraphNode, 0, len(depthMap))
	for id, depth := range depthMap {
		m, ok := visibleMems[id]
		if !ok {
			if id == rootMem.ID {
				m = *rootMem
			} else {
				continue
			}
		}
		nodes = append(nodes, SubgraphNode{
			ID:               m.ID,
			Summary:          m.Summary,
			Type:             m.Type,
			ScopeType:        m.ScopeType,
			Status:           m.Status,
			EpistemicStatus:  m.EpistemicStatus,
			Confidence:       m.Confidence,
			HopDepth:         depth,
			HasConflictAlert: conflictNodes[m.ID] || m.Status == memory.StatusConflicted,
		})
	}
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].HopDepth != nodes[j].HopDepth {
			return nodes[i].HopDepth < nodes[j].HopDepth
		}
		return nodes[i].ID.String() < nodes[j].ID.String()
	})

	edges := make([]SubgraphEdge, 0)
	for _, e := range sub.Edges {
		_, hasFrom := depthMap[e.FromID]
		_, hasTo := depthMap[e.ToID]
		if hasFrom && hasTo {
			edges = append(edges, SubgraphEdge{
				FromMemoryID: e.FromID,
				ToMemoryID:   e.ToID,
				RelationType: e.RelationType,
				Weight:       e.Weight,
			})
		}
	}

	if err := s.audit.RecordAllowed(ctx, memory.AuditEvent{
		ID:            uuid.New(),
		TenantID:      &auth.TenantID,
		PrincipalID:   &auth.PrincipalID,
		AgentID:       auth.AgentID,
		Operation:     "get_subgraph",
		ResourceType:  "memory",
		ResourceID:    &q.MemoryID,
		ScopeUsed:     string(rootMem.ScopeType),
		CorrelationID: q.CorrelationID,
		Outcome:       memory.AuditAllowed,
		CreatedAt:     now,
	}); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrOperational, err)
	}

	return &SubgraphResult{
		RootMemoryID: q.MemoryID,
		Nodes:        nodes,
		Edges:        edges,
	}, nil
}

func (s *Service) executeHybridSearch(
	ctx context.Context,
	auth identity.AuthContext,
	q SearchQuery,
	auditOp string,
) ([]memory.ScoredCandidate, map[uuid.UUID][]float32, bool, error) {
	if auth.TenantID == uuid.Nil || auth.PrincipalID == uuid.Nil {
		return nil, nil, false, ErrUnauthorized
	}
	if err := validateSearchQuery(q); err != nil {
		return nil, nil, false, err
	}
	if s.search == nil || s.graphCache == nil {
		return nil, nil, false, fmt.Errorf("%w: hybrid search dependencies not configured", ErrOperational)
	}

	projects, scopes, allowedTasks, taskSlice, err := s.authorizeSearchScope(ctx, auth, q, auditOp)
	if err != nil {
		return nil, nil, false, err
	}

	limit := q.Limit
	if limit == 0 {
		limit = defaultLimit
	}
	now := s.clock()
	filter := memory.SearchFilter{
		TenantID:            auth.TenantID,
		ProjectIDs:          projects,
		Scopes:              scopes,
		CallerAgentID:       auth.AgentID,
		AllowedTasks:        taskSlice,
		QueryText:           strings.TrimSpace(q.Query),
		IncludeSuperseded:   q.IncludeSuperseded,
		IncludeConsolidated: q.IncludeConsolidated,
		Limit:               limit,
		Now:                 now,
	}

	var (
		queryVec     []float32
		embedFailed  bool
		trgmIDs      []uuid.UUID
		ftsIDs       []uuid.UUID
		sub          graph.AuthorizedSubgraph
		visibleMems  map[uuid.UUID]memory.Memory
		embeddings   map[uuid.UUID][]float32
		missingEmbed bool
	)

	g, gCtx := errgroup.WithContext(ctx)

	// Branch A: query embedding (gracefully degrades on error or nil provider).
	g.Go(func() error {
		if s.embedder == nil {
			embedFailed = true
			return nil
		}
		out, embErr := s.embedder.Embed(gCtx, filter.QueryText)
		if embErr != nil || len(out.Dense) == 0 {
			embedFailed = true
			return nil
		}
		queryVec = out.Dense
		return nil
	})

	// Branch B: lexical/exact PostgreSQL branches + L1 Zero-Trust subgraph projection.
	g.Go(func() error {
		var trgmErr, ftsErr, projErr error
		trgmIDs, trgmErr = s.search.SearchByTrigram(gCtx, filter)
		if trgmErr != nil {
			return fmt.Errorf("%w: trigram search: %v", ErrOperational, trgmErr)
		}
		ftsIDs, ftsErr = s.search.SearchByFTS(gCtx, filter)
		if ftsErr != nil {
			return fmt.Errorf("%w: fts search: %v", ErrOperational, ftsErr)
		}
		sub, visibleMems, embeddings, missingEmbed, projErr = s.graphCache.ProjectAuthorizedSubgraph(gCtx, auth, filter, allowedTasks, now)
		if projErr != nil {
			return fmt.Errorf("%w: graph projection: %v", ErrOperational, projErr)
		}
		return nil
	})

	if err := g.Wait(); err != nil {
		return nil, nil, false, err
	}

	degradedMode := embedFailed || missingEmbed
	var vecIDs []uuid.UUID
	if !embedFailed && len(queryVec) > 0 {
		var vecErr error
		vecIDs, vecErr = s.search.SearchByVector(ctx, filter, queryVec)
		if vecErr != nil {
			return nil, nil, false, fmt.Errorf("%w: vector search: %v", ErrOperational, vecErr)
		}
	}

	rrfScores := memory.ComputeRRFScores(trgmIDs, ftsIDs, vecIDs, degradedMode)
	seeds := topVisibleSeeds(rrfScores, visibleMems, topSeedsLimit)
	actResults := graph.PropagateActivation(seeds, sub, q.Hops, q.IncludeSuperseded)

	inputs := make([]memory.CandidateRankingInput, 0, len(actResults))
	for id, act := range actResults {
		mem, ok := visibleMems[id]
		if !ok {
			continue
		}
		inputs = append(inputs, memory.CandidateRankingInput{
			Memory:                mem,
			SemanticScore:         act.NormalizedScore,
			MinHopDepth:           act.MinHopDepth,
			VisibleRelationsCount: act.VisibleRelationsCount,
			HasConflictAlert:      act.HasConflictAlert,
		})
	}
	ranked := memory.RankCandidates(inputs, now, q.IncludeSuperseded, q.IncludeConsolidated)

	if auditOp != "" {
		if err := s.audit.RecordAllowed(ctx, memory.AuditEvent{
			ID:            uuid.New(),
			TenantID:      &auth.TenantID,
			PrincipalID:   &auth.PrincipalID,
			AgentID:       auth.AgentID,
			Operation:     auditOp,
			ResourceType:  "memory",
			CorrelationID: q.CorrelationID,
			Outcome:       memory.AuditAllowed,
			Details:       map[string]any{"degraded_mode": degradedMode, "hits": len(ranked)},
			CreatedAt:     now,
		}); err != nil {
			return nil, nil, false, fmt.Errorf("%w: %v", ErrOperational, err)
		}
	}

	return ranked, embeddings, degradedMode, nil
}

func validateSearchQuery(q SearchQuery) error {
	trimmed := strings.TrimSpace(q.Query)
	if trimmed == "" || utf8.RuneCountInString(trimmed) > maxQueryRunes {
		return fmt.Errorf("%w: query must be non-empty and <= %d characters", ErrValidation, maxQueryRunes)
	}
	if q.Limit < 0 || q.Limit > maxSearchLimit {
		return fmt.Errorf("%w: limit must be within [1, %d]", ErrValidation, maxSearchLimit)
	}
	if q.Hops < 0 || q.Hops > maxSearchHops {
		return fmt.Errorf("%w: hops must be within [0, %d]", ErrValidation, maxSearchHops)
	}
	return nil
}

func (s *Service) authorizeSearchScope(
	ctx context.Context,
	auth identity.AuthContext,
	q SearchQuery,
	auditOp string,
) ([]uuid.UUID, []identity.ScopeType, map[string]bool, []string, error) {
	projects := q.ProjectIDs
	if len(projects) == 0 {
		projects = append([]uuid.UUID(nil), auth.AllowedProjects...)
	} else if !auth.IsAdmin {
		for _, pid := range projects {
			if !slices.Contains(auth.AllowedProjects, pid) {
				_ = s.recordDenied(ctx, auth, auditOpOrDefault(auditOp), "memory", nil, "", q.CorrelationID)
				return nil, nil, nil, nil, ErrForbidden
			}
		}
	}

	scopes := q.Scopes
	if len(scopes) == 0 {
		scopes = append([]identity.ScopeType(nil), auth.AllowedScopes...)
	} else {
		for _, sc := range scopes {
			if !slices.Contains(auth.AllowedScopes, sc) {
				_ = s.recordDenied(ctx, auth, auditOpOrDefault(auditOp), "memory", nil, string(sc), q.CorrelationID)
				return nil, nil, nil, nil, ErrForbidden
			}
		}
	}

	allowedTasks := make(map[string]bool)
	var taskSlice []string
	if slices.Contains(scopes, identity.ScopeTaskShared) {
		taskID := strings.TrimSpace(q.TaskID)
		if taskID != "" {
			if s.access == nil {
				return nil, nil, nil, nil, ErrOperational
			}
			authorizedTask := false
			for _, pid := range projects {
				snap, err := s.access.EvaluateTaskAccess(ctx, identity.TaskPermissionQuery{
					TenantID:    auth.TenantID,
					ProjectID:   pid,
					PrincipalID: auth.PrincipalID,
					TaskID:      taskID,
					Permission:  identity.TaskRead,
				})
				if err != nil {
					return nil, nil, nil, nil, ErrOperational
				}
				if snap.CanRead {
					authorizedTask = true
					break
				}
			}
			if !authorizedTask {
				_ = s.recordDenied(ctx, auth, auditOpOrDefault(auditOp), "memory", nil, string(identity.ScopeTaskShared), q.CorrelationID)
				return nil, nil, nil, nil, ErrForbidden
			}
			allowedTasks[taskID] = true
			taskSlice = []string{taskID}
		}
	}

	return projects, scopes, allowedTasks, taskSlice, nil
}

func auditOpOrDefault(op string) string {
	if op != "" {
		return op
	}
	return "build_context"
}

func topVisibleSeeds(rrfScores map[uuid.UUID]float32, visibleMems map[uuid.UUID]memory.Memory, maxSeeds int) map[uuid.UUID]float32 {
	type pair struct {
		id    uuid.UUID
		score float32
	}
	pairs := make([]pair, 0, len(rrfScores))
	for id, score := range rrfScores {
		if _, ok := visibleMems[id]; ok && score > 0 {
			pairs = append(pairs, pair{id: id, score: score})
		}
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].score != pairs[j].score {
			return pairs[i].score > pairs[j].score
		}
		return pairs[i].id.String() < pairs[j].id.String()
	})
	if len(pairs) > maxSeeds {
		pairs = pairs[:maxSeeds]
	}
	out := make(map[uuid.UUID]float32, len(pairs))
	for _, p := range pairs {
		out[p.id] = p.score
	}
	return out
}
