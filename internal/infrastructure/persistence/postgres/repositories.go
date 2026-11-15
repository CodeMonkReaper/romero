package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/romero-project/romero/internal/domain/identity"
	"github.com/romero-project/romero/internal/domain/memory"
	"github.com/romero-project/romero/internal/domain/provenance"
	"github.com/romero-project/romero/internal/infrastructure/persistence/sqlcgen"
)

type sourceRepo struct{ q *sqlcgen.Queries }

func (r *sourceRepo) InsertSource(ctx context.Context, src provenance.MemorySource) error {
	if err := src.Validate(); err != nil {
		return err
	}
	meta, err := marshalJSONMap(src.Metadata)
	if err != nil {
		return err
	}
	return r.q.InsertSource(ctx, sqlcgen.InsertSourceParams{
		ID:               src.ID,
		TenantID:         src.TenantID,
		ProjectID:        toNullUUID(src.ProjectID),
		SourceType:       sqlcgen.SourceTypeEnum(src.SourceType),
		ExternalSourceID: src.ExternalSourceID,
		ActorPrincipalID: toNullUUID(src.ActorPrincipalID),
		ActorAgentID:     toNullUUID(src.ActorAgentID),
		Uri:              src.URI,
		RawExcerptHash:   src.RawExcerptHash,
		Metadata:         meta,
		CreatedAt:        src.CreatedAt,
	})
}

type memoryRepo struct{ q *sqlcgen.Queries }

func (r *memoryRepo) GetMemoryByID(ctx context.Context, tenantID, memoryID uuid.UUID) (*memory.Memory, error) {
	if tenantID == uuid.Nil || memoryID == uuid.Nil {
		return nil, memory.ErrInvalidIdentifier
	}
	row, err := r.q.GetMemoryByID(ctx, sqlcgen.GetMemoryByIDParams{ID: memoryID, TenantID: tenantID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, memory.ErrNotFound
		}
		return nil, err
	}
	return toDomainMemory(memoryRowFields{
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
}

func (r *memoryRepo) GetMemoryByIdempotencyKey(ctx context.Context, tenantID uuid.UUID, key string) (*memory.Memory, error) {
	if tenantID == uuid.Nil || key == "" {
		return nil, memory.ErrInvalidIdentifier
	}
	row, err := r.q.GetMemoryByIdempotencyKey(ctx, sqlcgen.GetMemoryByIdempotencyKeyParams{
		TenantID:       tenantID,
		IdempotencyKey: toPgText(key),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, memory.ErrNotFound
		}
		return nil, err
	}
	return toDomainMemory(memoryRowFields{
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
}

func (r *memoryRepo) LockActiveTopicMemory(ctx context.Context, scope identity.MemoryScopeTarget, topicKey string) (*memory.Memory, error) {
	if scope.TenantID == uuid.Nil || topicKey == "" {
		return nil, memory.ErrInvalidIdentifier
	}
	row, err := r.q.LockActiveTopicMemory(ctx, sqlcgen.LockActiveTopicMemoryParams{
		TenantID:     scope.TenantID,
		ScopeType:    sqlcgen.ScopeTypeEnum(scope.ScopeType),
		TopicKey:     toPgText(topicKey),
		ProjectID:    toNullUUID(scope.ProjectID),
		OwnerAgentID: toNullUUID(scope.OwnerAgentID),
		TaskID:       scope.TaskID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, memory.ErrNotFound
		}
		return nil, err
	}
	return toDomainMemory(memoryRowFields{
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
}

func (r *memoryRepo) InsertMemory(ctx context.Context, m memory.Memory) error {
	if err := m.Validate(); err != nil {
		return err
	}
	meta, err := marshalJSONMap(m.Metadata)
	if err != nil {
		return err
	}
	return r.q.InsertMemory(ctx, sqlcgen.InsertMemoryParams{
		ID:              m.ID,
		TenantID:        m.TenantID,
		ProjectID:       toNullUUID(m.ProjectID),
		OwnerAgentID:    toNullUUID(m.OwnerAgentID),
		TaskID:          m.TaskID,
		MemoryType:      sqlcgen.MemoryTypeEnum(m.Type),
		ScopeType:       sqlcgen.ScopeTypeEnum(m.ScopeType),
		Status:          sqlcgen.MemoryStatusEnum(m.Status),
		TopicKey:        m.TopicKey,
		Summary:         m.Summary,
		Content:         m.Content,
		ContentSha256:   m.ContentSHA256,
		TokenCount:      int32(m.TokenCount),
		Tags:            nonNilSlice(m.Tags),
		Entities:        nonNilSlice(m.Entities),
		EpistemicStatus: sqlcgen.EpistemicStatusEnum(m.EpistemicStatus),
		AlphaScore:      m.AlphaScore,
		BetaScore:       m.BetaScore,
		Importance:      m.Importance,
		UtilityScore:    m.UtilityScore,
		AccessCount:     int32(m.AccessCount),
		Version:         int32(m.Version),
		NliChecked:      m.NLIChecked,
		PrimarySourceID: m.PrimarySourceID,
		IdempotencyKey:  m.IdempotencyKey,
		ObservedAt:      m.Clocks.ObservedAt,
		ValidFrom:       m.Clocks.ValidFrom,
		ValidUntil:      m.Clocks.ValidUntil,
		CreatedAt:       m.Clocks.CreatedAt,
		UpdatedAt:       m.UpdatedAt,
		LastAccessedAt:  m.LastAccessedAt,
		Metadata:        meta,
	})
}

func (r *memoryRepo) UpdateMemoryState(ctx context.Context, m memory.Memory) error {
	if err := m.Validate(); err != nil {
		return err
	}
	meta, err := marshalJSONMap(m.Metadata)
	if err != nil {
		return err
	}
	rows, err := r.q.UpdateMemoryState(ctx, sqlcgen.UpdateMemoryStateParams{
		Status:          sqlcgen.MemoryStatusEnum(m.Status),
		Summary:         m.Summary,
		Content:         m.Content,
		ContentSha256:   m.ContentSHA256,
		TokenCount:      int32(m.TokenCount),
		Tags:            nonNilSlice(m.Tags),
		Entities:        nonNilSlice(m.Entities),
		EpistemicStatus: sqlcgen.EpistemicStatusEnum(m.EpistemicStatus),
		AlphaScore:      m.AlphaScore,
		BetaScore:       m.BetaScore,
		Importance:      m.Importance,
		UtilityScore:    m.UtilityScore,
		AccessCount:     int32(m.AccessCount),
		Version:         int32(m.Version),
		NliChecked:      m.NLIChecked,
		ValidFrom:       m.Clocks.ValidFrom,
		ValidUntil:      m.Clocks.ValidUntil,
		UpdatedAt:       m.UpdatedAt,
		LastAccessedAt:  m.LastAccessedAt,
		Metadata:        meta,
		ID:              m.ID,
		TenantID:        m.TenantID,
	})
	if err != nil {
		return err
	}
	if rows == 0 {
		return memory.ErrNotFound
	}
	return nil
}

func (r *memoryRepo) ListVersionsByMemoryID(ctx context.Context, memoryID uuid.UUID) ([]memory.MemoryVersion, error) {
	if memoryID == uuid.Nil {
		return nil, memory.ErrInvalidIdentifier
	}
	rows, err := r.q.ListVersionsByMemoryID(ctx, memoryID)
	if err != nil {
		return nil, err
	}
	out := make([]memory.MemoryVersion, 0, len(rows))
	for _, row := range rows {
		mv, err := toDomainMemoryVersion(row)
		if err != nil {
			return nil, err
		}
		out = append(out, *mv)
	}
	return out, nil
}

func (r *memoryRepo) InsertMemoryVersion(ctx context.Context, v memory.MemoryVersion) error {
	if err := v.Validate(); err != nil {
		return err
	}
	meta, err := marshalJSONMap(v.Metadata)
	if err != nil {
		return err
	}
	return r.q.InsertMemoryVersion(ctx, sqlcgen.InsertMemoryVersionParams{
		ID:              v.ID,
		MemoryID:        v.MemoryID,
		Version:         int32(v.Version),
		Summary:         v.Summary,
		Content:         v.Content,
		Tags:            nonNilSlice(v.Tags),
		Entities:        nonNilSlice(v.Entities),
		Metadata:        meta,
		EpistemicStatus: sqlcgen.EpistemicStatusEnum(v.EpistemicStatus),
		AlphaScore:      v.AlphaScore,
		BetaScore:       v.BetaScore,
		Importance:      v.Importance,
		UtilityScore:    v.UtilityScore,
		Status:          sqlcgen.MemoryStatusEnum(v.Status),
		ValidFrom:       v.ValidFrom,
		ValidUntil:      v.ValidUntil,
		ChangedBySource: toNullUUID(v.ChangedBySource),
		ChangeReason:    v.ChangeReason,
		CreatedAt:       v.CreatedAt,
	})
}

func (r *memoryRepo) CloseMemoryVersionValidity(ctx context.Context, memoryID uuid.UUID, version int, validUntil time.Time) error {
	if memoryID == uuid.Nil || version < 1 {
		return memory.ErrInvalidIdentifier
	}
	if validUntil.IsZero() {
		return memory.ErrInvalidTimestamp
	}
	rows, err := r.q.CloseMemoryVersionValidity(ctx, sqlcgen.CloseMemoryVersionValidityParams{
		ValidUntil: validUntil,
		MemoryID:   memoryID,
		Version:    int32(version),
	})
	if err != nil {
		return err
	}
	if rows == 0 {
		return memory.ErrNotFound
	}
	return nil
}

func (r *memoryRepo) GetMemoryVersionPointInTime(ctx context.Context, memoryID uuid.UUID, validAt, knownAt time.Time) (*memory.MemoryVersion, error) {
	if memoryID == uuid.Nil {
		return nil, memory.ErrInvalidIdentifier
	}
	if validAt.IsZero() || knownAt.IsZero() {
		return nil, memory.ErrInvalidPointInTimeQuery
	}
	row, err := r.q.GetMemoryVersionPointInTime(ctx, sqlcgen.GetMemoryVersionPointInTimeParams{
		MemoryID: memoryID,
		KnownAt:  knownAt,
		ValidAt:  validAt,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, memory.ErrNotFound
		}
		return nil, err
	}
	return toDomainMemoryVersion(row)
}

func (r *memoryRepo) ListActiveRelationsByMemoryID(ctx context.Context, tenantID, memoryID uuid.UUID) ([]memory.MemoryRelation, error) {
	if tenantID == uuid.Nil || memoryID == uuid.Nil {
		return nil, memory.ErrInvalidIdentifier
	}
	rows, err := r.q.ListActiveRelationsByMemoryID(ctx, sqlcgen.ListActiveRelationsByMemoryIDParams{
		TenantID: tenantID,
		MemoryID: memoryID,
	})
	if err != nil {
		return nil, err
	}
	out := make([]memory.MemoryRelation, 0, len(rows))
	for _, row := range rows {
		out = append(out, toDomainMemoryRelation(row))
	}
	return out, nil
}

func (r *memoryRepo) InsertMemoryRelation(ctx context.Context, rel memory.MemoryRelation) error {
	if err := rel.Validate(); err != nil {
		return err
	}
	return r.q.InsertMemoryRelation(ctx, sqlcgen.InsertMemoryRelationParams{
		ID:               rel.ID,
		TenantID:         rel.TenantID,
		ProjectID:        toNullUUID(rel.ProjectID),
		FromMemoryID:     rel.FromMemoryID,
		ToMemoryID:       rel.ToMemoryID,
		RelationType:     sqlcgen.RelationTypeEnum(rel.RelationType),
		Weight:           rel.Weight,
		NliEntailment:    toFloat4(rel.NLIEntailment),
		NliContradiction: toFloat4(rel.NLIContradiction),
		SourceID:         toNullUUID(rel.SourceID),
		ValidFrom:        rel.ValidFrom,
		ValidUntil:       rel.ValidUntil,
		IsActive:         rel.IsActive,
		Notes:            rel.Notes,
		CreatedAt:        rel.CreatedAt,
	})
}

type feedbackRepo struct{ q *sqlcgen.Queries }

func (r *feedbackRepo) InsertMemoryFeedback(ctx context.Context, fb memory.MemoryFeedback) error {
	if err := fb.Validate(); err != nil {
		return err
	}
	return r.q.InsertMemoryFeedback(ctx, sqlcgen.InsertMemoryFeedbackParams{
		ID:           fb.ID,
		TenantID:     fb.TenantID,
		MemoryID:     fb.MemoryID,
		AgentID:      toNullUUID(fb.AgentID),
		TaskID:       fb.TaskID,
		Signal:       sqlcgen.FeedbackSignalEnum(fb.Signal),
		AlphaDelta:   fb.AlphaDelta,
		BetaDelta:    fb.BetaDelta,
		UtilityDelta: fb.UtilityDelta,
		Comment:      fb.Comment,
		CreatedAt:    fb.CreatedAt,
	})
}

type jobRepo struct{ q *sqlcgen.Queries }

func (r *jobRepo) EnqueueMemoryJob(ctx context.Context, job memory.MemoryJob) error {
	if err := job.Validate(); err != nil {
		return err
	}
	payload, err := marshalJSONMap(job.Payload)
	if err != nil {
		return err
	}
	return r.q.EnqueueMemoryJob(ctx, sqlcgen.EnqueueMemoryJobParams{
		ID:          job.ID,
		TenantID:    job.TenantID,
		ProjectID:   toNullUUID(job.ProjectID),
		MemoryID:    toNullUUID(job.MemoryID),
		JobType:     job.JobType,
		Priority:    job.Priority,
		Status:      sqlcgen.JobStatusEnum(job.Status),
		Payload:     payload,
		Attempts:    int32(job.Attempts),
		MaxAttempts: int32(job.MaxAttempts),
		LastError:   job.LastError,
		ScheduledAt: job.ScheduledAt,
		LockedAt:    job.LockedAt,
		CompletedAt: job.CompletedAt,
		CreatedAt:   job.CreatedAt,
	})
}

type auditRepo struct {
	allowedQueries *sqlcgen.Queries
	deniedQueries  *sqlcgen.Queries
}

func (r *auditRepo) RecordAllowed(ctx context.Context, ev memory.AuditEvent) error {
	if ev.Outcome != memory.AuditAllowed {
		return fmt.Errorf("%w: RecordAllowed requires allowed outcome", memory.ErrInvalidAuditOutcome)
	}
	return insertAudit(ctx, r.allowedQueries, ev)
}

func (r *auditRepo) RecordDenied(ctx context.Context, ev memory.AuditEvent) error {
	if ev.Outcome != memory.AuditDenied {
		return fmt.Errorf("%w: RecordDenied requires denied outcome", memory.ErrInvalidAuditOutcome)
	}
	return insertAudit(ctx, r.deniedQueries, ev)
}

func insertAudit(ctx context.Context, q *sqlcgen.Queries, ev memory.AuditEvent) error {
	if err := ev.Validate(); err != nil {
		return err
	}
	details, err := marshalJSONMap(ev.Details)
	if err != nil {
		return err
	}
	return q.InsertAuditEvent(ctx, sqlcgen.InsertAuditEventParams{
		ID:            ev.ID,
		TenantID:      toNullUUID(ev.TenantID),
		PrincipalID:   toNullUUID(ev.PrincipalID),
		AgentID:       toNullUUID(ev.AgentID),
		Operation:     ev.Operation,
		ResourceType:  ev.ResourceType,
		ResourceID:    toNullUUID(ev.ResourceID),
		ScopeUsed:     ev.ScopeUsed,
		CorrelationID: ev.CorrelationID,
		Outcome:       string(ev.Outcome),
		Details:       details,
		CreatedAt:     ev.CreatedAt,
	})
}
