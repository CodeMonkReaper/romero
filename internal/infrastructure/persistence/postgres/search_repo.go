package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/romero-project/romero/internal/domain/memory"
	"github.com/romero-project/romero/internal/infrastructure/persistence/sqlcgen"
)

const hnswMaxScanTuples = 20000

type searchRepo struct {
	pool *pgxpool.Pool
	q    *sqlcgen.Queries
}

// Search returns a non-transactional SearchRepository backed by the Store pool.
func (s *Store) Search() memory.SearchRepository {
	return &searchRepo{pool: s.pool, q: s.queries}
}

type normalizedSearchScope struct {
	tenantID            uuid.UUID
	projectIDs          []uuid.UUID
	scopes              []string
	callerAgentID       uuid.NullUUID
	allowedTasks        []string
	queryText           string
	includeSuperseded   bool
	includeConsolidated bool
	limit               int32
	now                 time.Time
}

func normalizeSearchFilter(f memory.SearchFilter) (normalizedSearchScope, error) {
	if f.TenantID == uuid.Nil {
		return normalizedSearchScope{}, memory.ErrInvalidIdentifier
	}
	scopes := make([]string, 0, len(f.Scopes))
	for _, s := range f.Scopes {
		scopes = append(scopes, string(s))
	}
	projectIDs := f.ProjectIDs
	if projectIDs == nil {
		projectIDs = []uuid.UUID{}
	}
	allowedTasks := f.AllowedTasks
	if allowedTasks == nil {
		allowedTasks = []string{}
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	now := f.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return normalizedSearchScope{
		tenantID:            f.TenantID,
		projectIDs:          projectIDs,
		scopes:              scopes,
		callerAgentID:       toNullUUID(f.CallerAgentID),
		allowedTasks:        allowedTasks,
		queryText:           strings.TrimSpace(f.QueryText),
		includeSuperseded:   f.IncludeSuperseded,
		includeConsolidated: f.IncludeConsolidated,
		limit:               int32(limit),
		now:                 now,
	}, nil
}

func (r *searchRepo) SearchByTrigram(ctx context.Context, f memory.SearchFilter) ([]uuid.UUID, error) {
	nf, err := normalizeSearchFilter(f)
	if err != nil {
		return nil, err
	}
	if len(nf.scopes) == 0 || nf.queryText == "" {
		return []uuid.UUID{}, nil
	}
	return r.q.SearchMemoriesByTrigram(ctx, sqlcgen.SearchMemoriesByTrigramParams{
		TenantID:            nf.tenantID,
		Scopes:              nf.scopes,
		ProjectIds:          nf.projectIDs,
		AllowedTasks:        nf.allowedTasks,
		CallerAgentID:       nf.callerAgentID,
		IncludeSuperseded:   nf.includeSuperseded,
		IncludeConsolidated: nf.includeConsolidated,
		Now:                 nf.now,
		QueryText:           nf.queryText,
		ResultLimit:         nf.limit,
	})
}

func (r *searchRepo) SearchByFTS(ctx context.Context, f memory.SearchFilter) ([]uuid.UUID, error) {
	nf, err := normalizeSearchFilter(f)
	if err != nil {
		return nil, err
	}
	if len(nf.scopes) == 0 || nf.queryText == "" {
		return []uuid.UUID{}, nil
	}
	return r.q.SearchMemoriesByFTS(ctx, sqlcgen.SearchMemoriesByFTSParams{
		TenantID:            nf.tenantID,
		Scopes:              nf.scopes,
		ProjectIds:          nf.projectIDs,
		AllowedTasks:        nf.allowedTasks,
		CallerAgentID:       nf.callerAgentID,
		IncludeSuperseded:   nf.includeSuperseded,
		IncludeConsolidated: nf.includeConsolidated,
		Now:                 nf.now,
		QueryText:           nf.queryText,
		ResultLimit:         nf.limit,
	})
}

// SearchByVector runs HNSW cosine search inside a read-only transaction with mandatory
// pgvector 0.8.0+ iterative scan parameters (Principle 14).
func (r *searchRepo) SearchByVector(ctx context.Context, f memory.SearchFilter, queryVec []float32) ([]uuid.UUID, error) {
	nf, err := normalizeSearchFilter(f)
	if err != nil {
		return nil, err
	}
	if len(nf.scopes) == 0 {
		return []uuid.UUID{}, nil
	}
	vecText, err := formatHalfVec(queryVec)
	if err != nil {
		return nil, err
	}

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	committed := false
	defer rollbackTx(&committed, tx)

	if _, err := tx.Exec(ctx, "SET LOCAL hnsw.iterative_scan = relaxed_order"); err != nil {
		return nil, fmt.Errorf("set hnsw.iterative_scan: %w", err)
	}
	if _, err := tx.Exec(ctx, fmt.Sprintf("SET LOCAL hnsw.max_scan_tuples = %d", hnswMaxScanTuples)); err != nil {
		return nil, fmt.Errorf("set hnsw.max_scan_tuples: %w", err)
	}

	txQ := r.q.WithTx(tx)
	ids, err := txQ.SearchMemoriesByVector(ctx, sqlcgen.SearchMemoriesByVectorParams{
		TenantID:            nf.tenantID,
		Scopes:              nf.scopes,
		ProjectIds:          nf.projectIDs,
		AllowedTasks:        nf.allowedTasks,
		CallerAgentID:       nf.callerAgentID,
		IncludeSuperseded:   nf.includeSuperseded,
		IncludeConsolidated: nf.includeConsolidated,
		Now:                 nf.now,
		QueryVecText:        vecText,
		ResultLimit:         nf.limit,
	})
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	committed = true
	return ids, nil
}

func (r *searchRepo) GetMemoriesByIDs(ctx context.Context, tenantID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]memory.Memory, error) {
	if tenantID == uuid.Nil {
		return nil, memory.ErrInvalidIdentifier
	}
	if len(ids) == 0 {
		return map[uuid.UUID]memory.Memory{}, nil
	}
	rows, err := r.q.GetMemoriesByIDs(ctx, sqlcgen.GetMemoriesByIDsParams{
		TenantID: tenantID,
		Ids:      ids,
	})
	if err != nil {
		return nil, err
	}
	out := make(map[uuid.UUID]memory.Memory, len(rows))
	for _, row := range rows {
		m, mErr := toDomainMemory(memoryRowFields{
			ID: row.ID, TenantID: row.TenantID, ProjectID: row.ProjectID, OwnerAgentID: row.OwnerAgentID,
			TaskID: row.TaskID, MemoryType: row.MemoryType, ScopeType: row.ScopeType, Status: row.Status,
			TopicKey: row.TopicKey, Summary: row.Summary, Content: row.Content, ContentSha256: row.ContentSha256,
			TokenCount: row.TokenCount, Tags: row.Tags, Entities: row.Entities, EpistemicStatus: row.EpistemicStatus,
			AlphaScore: row.AlphaScore, BetaScore: row.BetaScore, Confidence: row.Confidence, Importance: row.Importance,
			UtilityScore: row.UtilityScore, AccessCount: row.AccessCount, Version: row.Version, NliChecked: row.NliChecked,
			PrimarySourceID: row.PrimarySourceID, IdempotencyKey: row.IdempotencyKey, ObservedAt: row.ObservedAt,
			ValidFrom: row.ValidFrom, ValidUntil: row.ValidUntil, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
			LastAccessedAt: row.LastAccessedAt, Metadata: row.Metadata,
		})
		if mErr != nil {
			return nil, mErr
		}
		out[m.ID] = *m
	}
	return out, nil
}

func (r *searchRepo) LoadProjectGraphSnapshot(ctx context.Context, tenantID uuid.UUID, projectID *uuid.UUID, now time.Time) ([]memory.Memory, []memory.MemoryRelation, map[uuid.UUID][]float32, error) {
	if tenantID == uuid.Nil {
		return nil, nil, nil, memory.ErrInvalidIdentifier
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	projNull := toNullUUID(projectID)

	memRows, err := r.q.LoadProjectGraphMemories(ctx, sqlcgen.LoadProjectGraphMemoriesParams{
		TenantID:  tenantID,
		ProjectID: projNull,
		Now:       now,
	})
	if err != nil {
		return nil, nil, nil, err
	}
	mems := make([]memory.Memory, 0, len(memRows))
	for _, row := range memRows {
		m, mErr := toDomainMemory(memoryRowFields{
			ID: row.ID, TenantID: row.TenantID, ProjectID: row.ProjectID, OwnerAgentID: row.OwnerAgentID,
			TaskID: row.TaskID, MemoryType: row.MemoryType, ScopeType: row.ScopeType, Status: row.Status,
			TopicKey: row.TopicKey, Summary: row.Summary, Content: row.Content, ContentSha256: row.ContentSha256,
			TokenCount: row.TokenCount, Tags: row.Tags, Entities: row.Entities, EpistemicStatus: row.EpistemicStatus,
			AlphaScore: row.AlphaScore, BetaScore: row.BetaScore, Confidence: row.Confidence, Importance: row.Importance,
			UtilityScore: row.UtilityScore, AccessCount: row.AccessCount, Version: row.Version, NliChecked: row.NliChecked,
			PrimarySourceID: row.PrimarySourceID, IdempotencyKey: row.IdempotencyKey, ObservedAt: row.ObservedAt,
			ValidFrom: row.ValidFrom, ValidUntil: row.ValidUntil, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
			LastAccessedAt: row.LastAccessedAt, Metadata: row.Metadata,
		})
		if mErr != nil {
			return nil, nil, nil, mErr
		}
		mems = append(mems, *m)
	}

	relRows, err := r.q.LoadProjectGraphRelations(ctx, sqlcgen.LoadProjectGraphRelationsParams{
		TenantID:  tenantID,
		ProjectID: projNull,
		Now:       now,
	})
	if err != nil {
		return nil, nil, nil, err
	}
	rels := make([]memory.MemoryRelation, 0, len(relRows))
	for _, row := range relRows {
		rels = append(rels, memory.MemoryRelation{
			ID:               row.ID,
			TenantID:         row.TenantID,
			ProjectID:        fromNullUUID(row.ProjectID),
			FromMemoryID:     row.FromMemoryID,
			ToMemoryID:       row.ToMemoryID,
			RelationType:     memory.RelationType(row.RelationType),
			Weight:           row.Weight,
			NLIEntailment:    fromFloat4(row.NliEntailment),
			NLIContradiction: fromFloat4(row.NliContradiction),
			SourceID:         fromNullUUID(row.SourceID),
			ValidFrom:        row.ValidFrom,
			ValidUntil:       row.ValidUntil,
			IsActive:         row.IsActive,
			Notes:            fromPgText(row.Notes),
			CreatedAt:        row.CreatedAt,
		})
	}

	embRows, err := r.q.LoadProjectGraphEmbeddings(ctx, sqlcgen.LoadProjectGraphEmbeddingsParams{
		TenantID:  tenantID,
		ProjectID: projNull,
	})
	if err != nil {
		return nil, nil, nil, err
	}
	vecs := make(map[uuid.UUID][]float32, len(embRows))
	for _, row := range embRows {
		vec, parseErr := parseHalfVec(row.DenseVecText)
		if parseErr != nil {
			return nil, nil, nil, parseErr
		}
		vecs[row.MemoryID] = vec
	}

	return mems, rels, vecs, nil
}

func (r *searchRepo) TraverseCausalSubgraph(ctx context.Context, tenantID uuid.UUID, seeds []uuid.UUID, hops int, now time.Time) ([]memory.CausalWalkRow, error) {
	if tenantID == uuid.Nil {
		return nil, memory.ErrInvalidIdentifier
	}
	if len(seeds) == 0 || hops <= 0 {
		return []memory.CausalWalkRow{}, nil
	}
	if hops > 3 {
		hops = 3
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}

	rows, err := r.q.TraverseCausalSubgraph(ctx, sqlcgen.TraverseCausalSubgraphParams{
		TenantID: tenantID,
		Now:      now,
		Seeds:    seeds,
		MaxHops:  int32(hops),
	})
	if err != nil {
		return nil, err
	}
	out := make([]memory.CausalWalkRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, memory.CausalWalkRow{
			FromID:       row.FromMemoryID,
			ToID:         row.ToMemoryID,
			RelationType: memory.RelationType(row.RelationType),
			Weight:       row.Weight,
			Depth:        int(row.Depth),
		})
	}
	return out, nil
}
