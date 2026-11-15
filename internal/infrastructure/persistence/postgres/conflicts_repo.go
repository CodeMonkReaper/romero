package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/romero-project/romero/internal/domain/memory"
	"github.com/romero-project/romero/internal/infrastructure/persistence/sqlcgen"
)

type semanticNeighborRepo struct {
	pool *pgxpool.Pool
	q    *sqlcgen.Queries
	tx   pgx.Tx
}

// SemanticNeighbors returns a non-transactional SemanticNeighborRepository backed by the Store pool.
func (s *Store) SemanticNeighbors() memory.SemanticNeighborRepository {
	return &semanticNeighborRepo{pool: s.pool, q: s.queries}
}

func (r *semanticNeighborRepo) FindSameScopeSemanticNeighbors(
	ctx context.Context,
	mem memory.Memory,
	queryVec []float32,
	minSimilarity float32,
	limit int,
	now time.Time,
) ([]memory.Memory, error) {
	if mem.TenantID == uuid.Nil || mem.ID == uuid.Nil {
		return nil, memory.ErrInvalidIdentifier
	}
	vecText, err := formatHalfVec(queryVec)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 5
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}

	var excludeTopic pgtype.Text
	if mem.TopicKey != "" {
		excludeTopic = pgtype.Text{String: mem.TopicKey, Valid: true}
	}

	params := sqlcgen.FindSameScopeSemanticNeighborsParams{
		TenantID:        mem.TenantID,
		ExcludeMemoryID: mem.ID,
		ExcludeTopicKey: excludeTopic,
		ScopeType:       sqlcgen.ScopeTypeEnum(mem.ScopeType),
		ProjectID:       toNullUUID(mem.ProjectID),
		OwnerAgentID:    toNullUUID(mem.OwnerAgentID),
		TaskID:          mem.TaskID,
		Now:             now,
		QueryVecText:    vecText,
		MinSimilarity:   float64(minSimilarity),
		ResultLimit:     int32(limit),
	}

	var rows []sqlcgen.FindSameScopeSemanticNeighborsRow
	if r.tx != nil {
		if err := setHNSWSessionParams(ctx, r.tx); err != nil {
			return nil, err
		}
		rows, err = r.q.FindSameScopeSemanticNeighbors(ctx, params)
		if err != nil {
			return nil, err
		}
	} else {
		tx, beginErr := r.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
		if beginErr != nil {
			return nil, beginErr
		}
		committed := false
		defer rollbackTx(&committed, tx)

		if err := setHNSWSessionParams(ctx, tx); err != nil {
			return nil, err
		}
		txQ := r.q.WithTx(tx)
		rows, err = txQ.FindSameScopeSemanticNeighbors(ctx, params)
		if err != nil {
			return nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		committed = true
	}

	out := make([]memory.Memory, 0, len(rows))
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
		out = append(out, *m)
	}
	return out, nil
}

func setHNSWSessionParams(ctx context.Context, tx pgx.Tx) error {
	if _, err := tx.Exec(ctx, "SET LOCAL hnsw.iterative_scan = relaxed_order"); err != nil {
		return fmt.Errorf("set hnsw.iterative_scan: %w", err)
	}
	if _, err := tx.Exec(ctx, fmt.Sprintf("SET LOCAL hnsw.max_scan_tuples = %d", hnswMaxScanTuples)); err != nil {
		return fmt.Errorf("set hnsw.max_scan_tuples: %w", err)
	}
	return nil
}

func (r *semanticNeighborRepo) DeactivateConflictsByMemoryID(ctx context.Context, tenantID, memoryID uuid.UUID, validUntil time.Time) ([]uuid.UUID, error) {
	if tenantID == uuid.Nil || memoryID == uuid.Nil {
		return nil, memory.ErrInvalidIdentifier
	}
	if validUntil.IsZero() {
		validUntil = time.Now().UTC()
	}
	return r.q.DeactivateConflictsByMemoryID(ctx, sqlcgen.DeactivateConflictsByMemoryIDParams{
		ValidUntil: validUntil,
		TenantID:   tenantID,
		MemoryID:   memoryID,
	})
}

func (r *semanticNeighborRepo) HasActiveConflicts(ctx context.Context, tenantID, memoryID uuid.UUID, now time.Time) (bool, error) {
	if tenantID == uuid.Nil || memoryID == uuid.Nil {
		return false, memory.ErrInvalidIdentifier
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return r.q.HasActiveConflicts(ctx, sqlcgen.HasActiveConflictsParams{
		TenantID: tenantID,
		Now:      now,
		MemoryID: memoryID,
	})
}

type conflictRepo struct {
	q *sqlcgen.Queries
}

// Conflicts returns a non-transactional ConflictRepository backed by the Store pool.
func (s *Store) Conflicts() memory.ConflictRepository {
	return &conflictRepo{q: s.queries}
}

func (r *conflictRepo) ListActiveConflicts(ctx context.Context, tenantID uuid.UUID, projectID *uuid.UUID, limit, offset int, now time.Time) ([]memory.ConflictPairRow, error) {
	if tenantID == uuid.Nil {
		return nil, memory.ErrInvalidIdentifier
	}
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	rows, err := r.q.ListActiveConflicts(ctx, sqlcgen.ListActiveConflictsParams{
		TenantID:     tenantID,
		Now:          now,
		ProjectID:    toNullUUID(projectID),
		ResultLimit:  int32(limit),
		ResultOffset: int32(offset),
	})
	if err != nil {
		return nil, err
	}
	out := make([]memory.ConflictPairRow, 0, len(rows))
	for _, row := range rows {
		pair, mapErr := mapConflictPair(conflictPairFields{
			rID: row.RID, rTenantID: row.RTenantID, rProjectID: row.RProjectID,
			rFromMemoryID: row.RFromMemoryID, rToMemoryID: row.RToMemoryID, rRelationType: row.RRelationType,
			rWeight: row.RWeight, rNliEntailment: row.RNliEntailment, rNliContradiction: row.RNliContradiction,
			rSourceID: row.RSourceID, rValidFrom: row.RValidFrom, rValidUntil: row.RValidUntil,
			rIsActive: row.RIsActive, rNotes: row.RNotes, rCreatedAt: row.RCreatedAt,
			fm: memoryRowFields{
				ID: row.FmID, TenantID: row.FmTenantID, ProjectID: row.FmProjectID, OwnerAgentID: row.FmOwnerAgentID,
				TaskID: row.FmTaskID, MemoryType: row.FmMemoryType, ScopeType: row.FmScopeType, Status: row.FmStatus,
				TopicKey: row.FmTopicKey, Summary: row.FmSummary, Content: row.FmContent, ContentSha256: row.FmContentSha256,
				TokenCount: row.FmTokenCount, Tags: row.FmTags, Entities: row.FmEntities, EpistemicStatus: row.FmEpistemicStatus,
				AlphaScore: row.FmAlphaScore, BetaScore: row.FmBetaScore, Confidence: row.FmConfidence, Importance: row.FmImportance,
				UtilityScore: row.FmUtilityScore, AccessCount: row.FmAccessCount, Version: row.FmVersion, NliChecked: row.FmNliChecked,
				PrimarySourceID: row.FmPrimarySourceID, IdempotencyKey: row.FmIdempotencyKey, ObservedAt: row.FmObservedAt,
				ValidFrom: row.FmValidFrom, ValidUntil: row.FmValidUntil, CreatedAt: row.FmCreatedAt, UpdatedAt: row.FmUpdatedAt,
				LastAccessedAt: row.FmLastAccessedAt, Metadata: row.FmMetadata,
			},
			tm: memoryRowFields{
				ID: row.TmID, TenantID: row.TmTenantID, ProjectID: row.TmProjectID, OwnerAgentID: row.TmOwnerAgentID,
				TaskID: row.TmTaskID, MemoryType: row.TmMemoryType, ScopeType: row.TmScopeType, Status: row.TmStatus,
				TopicKey: row.TmTopicKey, Summary: row.TmSummary, Content: row.TmContent, ContentSha256: row.TmContentSha256,
				TokenCount: row.TmTokenCount, Tags: row.TmTags, Entities: row.TmEntities, EpistemicStatus: row.TmEpistemicStatus,
				AlphaScore: row.TmAlphaScore, BetaScore: row.TmBetaScore, Confidence: row.TmConfidence, Importance: row.TmImportance,
				UtilityScore: row.TmUtilityScore, AccessCount: row.TmAccessCount, Version: row.TmVersion, NliChecked: row.TmNliChecked,
				PrimarySourceID: row.TmPrimarySourceID, IdempotencyKey: row.TmIdempotencyKey, ObservedAt: row.TmObservedAt,
				ValidFrom: row.TmValidFrom, ValidUntil: row.TmValidUntil, CreatedAt: row.TmCreatedAt, UpdatedAt: row.TmUpdatedAt,
				LastAccessedAt: row.TmLastAccessedAt, Metadata: row.TmMetadata,
			},
		})
		if mapErr != nil {
			return nil, mapErr
		}
		out = append(out, *pair)
	}
	return out, nil
}

func (r *conflictRepo) GetConflictForUpdate(ctx context.Context, tenantID, conflictID uuid.UUID) (*memory.ConflictPairRow, error) {
	if tenantID == uuid.Nil || conflictID == uuid.Nil {
		return nil, memory.ErrInvalidIdentifier
	}
	row, err := r.q.GetConflictForUpdate(ctx, sqlcgen.GetConflictForUpdateParams{
		ConflictID: conflictID,
		TenantID:   tenantID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, memory.ErrNotFound
		}
		return nil, err
	}
	return mapConflictPair(conflictPairFields{
		rID: row.RID, rTenantID: row.RTenantID, rProjectID: row.RProjectID,
		rFromMemoryID: row.RFromMemoryID, rToMemoryID: row.RToMemoryID, rRelationType: row.RRelationType,
		rWeight: row.RWeight, rNliEntailment: row.RNliEntailment, rNliContradiction: row.RNliContradiction,
		rSourceID: row.RSourceID, rValidFrom: row.RValidFrom, rValidUntil: row.RValidUntil,
		rIsActive: row.RIsActive, rNotes: row.RNotes, rCreatedAt: row.RCreatedAt,
		fm: memoryRowFields{
			ID: row.FmID, TenantID: row.FmTenantID, ProjectID: row.FmProjectID, OwnerAgentID: row.FmOwnerAgentID,
			TaskID: row.FmTaskID, MemoryType: row.FmMemoryType, ScopeType: row.FmScopeType, Status: row.FmStatus,
			TopicKey: row.FmTopicKey, Summary: row.FmSummary, Content: row.FmContent, ContentSha256: row.FmContentSha256,
			TokenCount: row.FmTokenCount, Tags: row.FmTags, Entities: row.FmEntities, EpistemicStatus: row.FmEpistemicStatus,
			AlphaScore: row.FmAlphaScore, BetaScore: row.FmBetaScore, Confidence: row.FmConfidence, Importance: row.FmImportance,
			UtilityScore: row.FmUtilityScore, AccessCount: row.FmAccessCount, Version: row.FmVersion, NliChecked: row.FmNliChecked,
			PrimarySourceID: row.FmPrimarySourceID, IdempotencyKey: row.FmIdempotencyKey, ObservedAt: row.FmObservedAt,
			ValidFrom: row.FmValidFrom, ValidUntil: row.FmValidUntil, CreatedAt: row.FmCreatedAt, UpdatedAt: row.FmUpdatedAt,
			LastAccessedAt: row.FmLastAccessedAt, Metadata: row.FmMetadata,
		},
		tm: memoryRowFields{
			ID: row.TmID, TenantID: row.TmTenantID, ProjectID: row.TmProjectID, OwnerAgentID: row.TmOwnerAgentID,
			TaskID: row.TmTaskID, MemoryType: row.TmMemoryType, ScopeType: row.TmScopeType, Status: row.TmStatus,
			TopicKey: row.TmTopicKey, Summary: row.TmSummary, Content: row.TmContent, ContentSha256: row.TmContentSha256,
			TokenCount: row.TmTokenCount, Tags: row.TmTags, Entities: row.TmEntities, EpistemicStatus: row.TmEpistemicStatus,
			AlphaScore: row.TmAlphaScore, BetaScore: row.TmBetaScore, Confidence: row.TmConfidence, Importance: row.TmImportance,
			UtilityScore: row.TmUtilityScore, AccessCount: row.TmAccessCount, Version: row.TmVersion, NliChecked: row.TmNliChecked,
			PrimarySourceID: row.TmPrimarySourceID, IdempotencyKey: row.TmIdempotencyKey, ObservedAt: row.TmObservedAt,
			ValidFrom: row.TmValidFrom, ValidUntil: row.TmValidUntil, CreatedAt: row.TmCreatedAt, UpdatedAt: row.TmUpdatedAt,
			LastAccessedAt: row.TmLastAccessedAt, Metadata: row.TmMetadata,
		},
	})
}

func (r *conflictRepo) DeactivateConflictEdge(ctx context.Context, tenantID, conflictID uuid.UUID, validUntil time.Time) error {
	if tenantID == uuid.Nil || conflictID == uuid.Nil {
		return memory.ErrInvalidIdentifier
	}
	if validUntil.IsZero() {
		validUntil = time.Now().UTC()
	}
	rows, err := r.q.DeactivateConflictEdge(ctx, sqlcgen.DeactivateConflictEdgeParams{
		ValidUntil: validUntil,
		ConflictID: conflictID,
		TenantID:   tenantID,
	})
	if err != nil {
		return err
	}
	if rows == 0 {
		return memory.ErrNotFound
	}
	return nil
}

type conflictPairFields struct {
	rID               uuid.UUID
	rTenantID         uuid.UUID
	rProjectID        uuid.NullUUID
	rFromMemoryID     uuid.UUID
	rToMemoryID       uuid.UUID
	rRelationType     sqlcgen.RelationTypeEnum
	rWeight           float32
	rNliEntailment    pgtype.Float4
	rNliContradiction pgtype.Float4
	rSourceID         uuid.NullUUID
	rValidFrom        time.Time
	rValidUntil       *time.Time
	rIsActive         bool
	rNotes            pgtype.Text
	rCreatedAt        time.Time
	fm                memoryRowFields
	tm                memoryRowFields
}

func mapConflictPair(f conflictPairFields) (*memory.ConflictPairRow, error) {
	fromMem, err := toDomainMemory(f.fm)
	if err != nil {
		return nil, err
	}
	toMem, err := toDomainMemory(f.tm)
	if err != nil {
		return nil, err
	}
	return &memory.ConflictPairRow{
		Relation: memory.MemoryRelation{
			ID:               f.rID,
			TenantID:         f.rTenantID,
			ProjectID:        fromNullUUID(f.rProjectID),
			FromMemoryID:     f.rFromMemoryID,
			ToMemoryID:       f.rToMemoryID,
			RelationType:     memory.RelationType(f.rRelationType),
			Weight:           f.rWeight,
			NLIEntailment:    fromFloat4(f.rNliEntailment),
			NLIContradiction: fromFloat4(f.rNliContradiction),
			SourceID:         fromNullUUID(f.rSourceID),
			ValidFrom:        f.rValidFrom,
			ValidUntil:       f.rValidUntil,
			IsActive:         f.rIsActive,
			Notes:            fromPgText(f.rNotes),
			CreatedAt:        f.rCreatedAt,
		},
		FromMemory: *fromMem,
		ToMemory:   *toMem,
	}, nil
}
