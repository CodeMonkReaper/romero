package postgres

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/romero-project/romero/internal/domain/identity"
	"github.com/romero-project/romero/internal/domain/memory"
	"github.com/romero-project/romero/internal/infrastructure/persistence/sqlcgen"
)

func toNullUUID(id *uuid.UUID) uuid.NullUUID {
	if id == nil || *id == uuid.Nil {
		return uuid.NullUUID{}
	}
	return uuid.NullUUID{UUID: *id, Valid: true}
}

func fromNullUUID(n uuid.NullUUID) *uuid.UUID {
	if !n.Valid || n.UUID == uuid.Nil {
		return nil
	}
	v := n.UUID
	return &v
}

func fromPgText(t pgtype.Text) string {
	if !t.Valid {
		return ""
	}
	return t.String
}

func toPgText(s string) pgtype.Text {
	if s == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: s, Valid: true}
}

func toFloat4(v *float32) pgtype.Float4 {
	if v == nil {
		return pgtype.Float4{}
	}
	return pgtype.Float4{Float32: *v, Valid: true}
}

func fromFloat4(v pgtype.Float4) *float32 {
	if !v.Valid {
		return nil
	}
	out := v.Float32
	return &out
}

func nonNilSlice(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func marshalJSONMap(m map[string]any) ([]byte, error) {
	if len(m) == 0 {
		return []byte("{}"), nil
	}
	return json.Marshal(m)
}

func unmarshalJSONMap(raw []byte) (map[string]any, error) {
	if len(raw) == 0 {
		return map[string]any{}, nil
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	if out == nil {
		return map[string]any{}, nil
	}
	return out, nil
}

type memoryRowFields struct {
	ID              uuid.UUID
	TenantID        uuid.UUID
	ProjectID       uuid.NullUUID
	OwnerAgentID    uuid.NullUUID
	TaskID          pgtype.Text
	MemoryType      sqlcgen.MemoryTypeEnum
	ScopeType       sqlcgen.ScopeTypeEnum
	Status          sqlcgen.MemoryStatusEnum
	TopicKey        pgtype.Text
	Summary         string
	Content         string
	ContentSha256   string
	TokenCount      int32
	Tags            []string
	Entities        []string
	EpistemicStatus sqlcgen.EpistemicStatusEnum
	AlphaScore      float32
	BetaScore       float32
	Confidence      pgtype.Float4
	Importance      float32
	UtilityScore    float32
	AccessCount     int32
	Version         int32
	NliChecked      bool
	PrimarySourceID uuid.UUID
	IdempotencyKey  pgtype.Text
	ObservedAt      time.Time
	ValidFrom       time.Time
	ValidUntil      *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
	LastAccessedAt  *time.Time
	Metadata        []byte
}

func toDomainMemory(r memoryRowFields) (*memory.Memory, error) {
	meta, err := unmarshalJSONMap(r.Metadata)
	if err != nil {
		return nil, fmt.Errorf("unmarshal memory metadata: %w", err)
	}
	var conf float32
	if r.Confidence.Valid {
		conf = r.Confidence.Float32
	}
	return &memory.Memory{
		ID:              r.ID,
		TenantID:        r.TenantID,
		ProjectID:       fromNullUUID(r.ProjectID),
		OwnerAgentID:    fromNullUUID(r.OwnerAgentID),
		TaskID:          fromPgText(r.TaskID),
		Type:            memory.MemoryType(r.MemoryType),
		ScopeType:       identity.ScopeType(r.ScopeType),
		Status:          memory.MemoryStatus(r.Status),
		TopicKey:        fromPgText(r.TopicKey),
		Summary:         r.Summary,
		Content:         r.Content,
		ContentSHA256:   r.ContentSha256,
		TokenCount:      int(r.TokenCount),
		Tags:            nonNilSlice(r.Tags),
		Entities:        nonNilSlice(r.Entities),
		EpistemicStatus: memory.EpistemicStatus(r.EpistemicStatus),
		AlphaScore:      r.AlphaScore,
		BetaScore:       r.BetaScore,
		Confidence:      conf,
		Importance:      r.Importance,
		UtilityScore:    r.UtilityScore,
		AccessCount:     int(r.AccessCount),
		Version:         int(r.Version),
		NLIChecked:      r.NliChecked,
		PrimarySourceID: r.PrimarySourceID,
		IdempotencyKey:  fromPgText(r.IdempotencyKey),
		Clocks: memory.TriTemporalWindow{
			ObservedAt: r.ObservedAt,
			CreatedAt:  r.CreatedAt,
			ValidFrom:  r.ValidFrom,
			ValidUntil: r.ValidUntil,
		},
		UpdatedAt:      r.UpdatedAt,
		LastAccessedAt: r.LastAccessedAt,
		Metadata:       meta,
	}, nil
}

func toDomainMemoryVersion(v sqlcgen.MemoryVersion) (*memory.MemoryVersion, error) {
	meta, err := unmarshalJSONMap(v.Metadata)
	if err != nil {
		return nil, fmt.Errorf("unmarshal memory version metadata: %w", err)
	}
	return &memory.MemoryVersion{
		ID:              v.ID,
		MemoryID:        v.MemoryID,
		Version:         int(v.Version),
		Summary:         v.Summary,
		Content:         v.Content,
		Tags:            nonNilSlice(v.Tags),
		Entities:        nonNilSlice(v.Entities),
		Metadata:        meta,
		EpistemicStatus: memory.EpistemicStatus(v.EpistemicStatus),
		AlphaScore:      v.AlphaScore,
		BetaScore:       v.BetaScore,
		Importance:      v.Importance,
		UtilityScore:    v.UtilityScore,
		Status:          memory.MemoryStatus(v.Status),
		ValidFrom:       v.ValidFrom,
		ValidUntil:      v.ValidUntil,
		ChangedBySource: fromNullUUID(v.ChangedBySource),
		ChangeReason:    fromPgText(v.ChangeReason),
		CreatedAt:       v.CreatedAt,
	}, nil
}

func toDomainMemoryRelation(r sqlcgen.MemoryRelation) memory.MemoryRelation {
	return memory.MemoryRelation{
		ID:               r.ID,
		TenantID:         r.TenantID,
		ProjectID:        fromNullUUID(r.ProjectID),
		FromMemoryID:     r.FromMemoryID,
		ToMemoryID:       r.ToMemoryID,
		RelationType:     memory.RelationType(r.RelationType),
		Weight:           r.Weight,
		NLIEntailment:    fromFloat4(r.NliEntailment),
		NLIContradiction: fromFloat4(r.NliContradiction),
		SourceID:         fromNullUUID(r.SourceID),
		ValidFrom:        r.ValidFrom,
		ValidUntil:       r.ValidUntil,
		IsActive:         r.IsActive,
		Notes:            fromPgText(r.Notes),
		CreatedAt:        r.CreatedAt,
	}
}
