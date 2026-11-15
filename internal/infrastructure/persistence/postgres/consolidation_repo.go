package postgres

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/romero-project/romero/internal/domain/memory"
	"github.com/romero-project/romero/internal/infrastructure/persistence/sqlcgen"
)

type consolidationRepo struct {
	q *sqlcgen.Queries
}

// Consolidation returns a non-transactional ConsolidationRepository backed by the Store pool.
func (s *Store) Consolidation() memory.ConsolidationRepository {
	return &consolidationRepo{q: s.queries}
}

func (r *consolidationRepo) ListREMConsolidationCandidates(
	ctx context.Context,
	tenantID uuid.UUID,
	projectID *uuid.UUID,
	minConfidence float32,
) ([]memory.Memory, map[uuid.UUID][]float32, error) {
	if tenantID == uuid.Nil {
		return nil, nil, memory.ErrInvalidIdentifier
	}
	rows, err := r.q.ListREMConsolidationCandidates(ctx, sqlcgen.ListREMConsolidationCandidatesParams{
		TenantID:      tenantID,
		ProjectID:     toNullUUID(projectID),
		MinConfidence: minConfidence,
	})
	if err != nil {
		return nil, nil, err
	}

	mems := make([]memory.Memory, 0, len(rows))
	vecs := make(map[uuid.UUID][]float32, len(rows))
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
			return nil, nil, mErr
		}
		vec, parseErr := parseHalfVec(row.DenseVecText)
		if parseErr != nil {
			return nil, nil, parseErr
		}
		mems = append(mems, *m)
		vecs[m.ID] = vec
	}
	return mems, vecs, nil
}

func (r *consolidationRepo) ListActiveVerifiedByProject(
	ctx context.Context,
	tenantID uuid.UUID,
	projectID *uuid.UUID,
	now time.Time,
) ([]memory.Memory, error) {
	if tenantID == uuid.Nil {
		return nil, memory.ErrInvalidIdentifier
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	rows, err := r.q.ListActiveVerifiedByProject(ctx, sqlcgen.ListActiveVerifiedByProjectParams{
		TenantID:  tenantID,
		ProjectID: toNullUUID(projectID),
		Now:       now,
	})
	if err != nil {
		return nil, err
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
