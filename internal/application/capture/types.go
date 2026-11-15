// Package capture implements Zero-Trust memory capture, update, explicit supersession,
// and archival use cases, including deferred opaque queuing for write-only task_shared actors.
package capture

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/romero-project/romero/internal/domain/identity"
	"github.com/romero-project/romero/internal/domain/memory"
	"github.com/romero-project/romero/internal/domain/provenance"
)

var (
	// ErrUnauthorized indicates missing or invalid caller identity.
	ErrUnauthorized = errors.New("unauthorized")
	// ErrForbidden mirrors identity.ErrForbidden for policy or epistemic denials.
	ErrForbidden = identity.ErrForbidden
	// ErrNotFound mirrors memory.ErrNotFound when a target memory is missing or outside scope.
	ErrNotFound = memory.ErrNotFound
	// ErrConflict indicates an idempotency mismatch, closed-memory mutation, or invalid downgrade.
	ErrConflict = errors.New("conflict")
	// ErrValidation indicates malformed or out-of-bounds command input.
	ErrValidation = errors.New("validation error")
	// ErrOperational indicates a required security or persistence dependency failed.
	ErrOperational = errors.New("operational dependency unavailable")
)

// TaskAccessSnapshot represents the single-query READ COMMITTED task permission state.
type TaskAccessSnapshot struct {
	CanRead  bool
	CanWrite bool
}

// TaskAccessChecker evaluates effective task_shared permissions without caching.
type TaskAccessChecker interface {
	EvaluateTaskAccess(ctx context.Context, q identity.TaskPermissionQuery) (TaskAccessSnapshot, error)
}

// OpaqueOperation enumerates capture-family operations eligible for deferred opaque queuing.
type OpaqueOperation string

const (
	OpaqueOpCapture   OpaqueOperation = "capture"
	OpaqueOpUpdate    OpaqueOperation = "update"
	OpaqueOpSupersede OpaqueOperation = "supersede"
	OpaqueOpArchive   OpaqueOperation = "archive"
)

// OpaqueRequest carries a sanitized write request queued for a non-reader task_shared writer.
type OpaqueRequest struct {
	RequestID      uuid.UUID
	TenantID       uuid.UUID
	ProjectID      uuid.UUID
	TaskID         string
	PrincipalID    uuid.UUID
	Operation      OpaqueOperation
	TargetMemoryID *uuid.UUID
	PayloadVersion int16
	Payload        json.RawMessage
	RequestSHA256  string
	IdempotencyKey string
}

// OpaqueReceipt holds only the server-assigned opaque request identifier for HTTP 202 responses.
type OpaqueReceipt struct {
	RequestID uuid.UUID
}

// OpaqueRequestEnqueuer persists opaque task_shared write requests before returning 202.
type OpaqueRequestEnqueuer interface {
	Enqueue(ctx context.Context, req OpaqueRequest) (OpaqueReceipt, error)
}

// CaptureCommand carries inputs for capturing a new memory and its provenance source.
type CaptureCommand struct {
	ProjectID        *uuid.UUID
	TaskID           string
	Type             memory.MemoryType
	ScopeType        identity.ScopeType
	TopicKey         string
	Summary          string
	Content          string
	Tags             []string
	Entities         []string
	EpistemicStatus  memory.EpistemicStatus
	Importance       float32
	SourceType       provenance.SourceType
	ExternalSourceID string
	URI              string
	RawExcerptHash   string
	SourceMetadata   map[string]any
	MemoryMetadata   map[string]any
	ObservedAt       *time.Time
	ValidFrom        *time.Time
	ValidUntil       *time.Time
	IdempotencyKey   string
	CorrelationID    string
}

// CaptureResult reports either synchronous memory persistence (201/200) or opaque acceptance (202).
type CaptureResult struct {
	Created       bool
	Accepted      bool
	OpaqueReceipt *OpaqueReceipt
	Memory        *MemoryView
}

// UpdateCommand carries inputs for creating a new immutable version of an active memory.
type UpdateCommand struct {
	TaskID         string
	Summary        string
	Content        string
	Tags           []string
	Entities       []string
	Importance     *float32
	Metadata       map[string]any
	ChangeReason   string
	IdempotencyKey string
	CorrelationID  string
}

// ExplicitSupersedeCommand carries inputs for explicitly superseding an existing memory.
type ExplicitSupersedeCommand struct {
	TaskID        string
	Capture       CaptureCommand
	ChangeReason  string
	CorrelationID string
}

// SupersedeResult reports either synchronous explicit supersession or opaque acceptance (202).
type SupersedeResult struct {
	Accepted      bool
	OpaqueReceipt *OpaqueReceipt
	OldMemory     *MemoryView
	NewMemory     *MemoryView
}

// ArchiveCommand carries inputs for archiving an open memory.
type ArchiveCommand struct {
	TaskID         string
	Reason         string
	IdempotencyKey string
	CorrelationID  string
}

// MutationResult reports either a synchronous memory mutation or opaque acceptance (202).
type MutationResult struct {
	Accepted      bool
	OpaqueReceipt *OpaqueReceipt
	Memory        *MemoryView
}

// MemoryView is the application-layer projection of a persisted memory record.
type MemoryView struct {
	ID              uuid.UUID
	TenantID        uuid.UUID
	ProjectID       *uuid.UUID
	OwnerAgentID    *uuid.UUID
	TaskID          string
	Type            memory.MemoryType
	ScopeType       identity.ScopeType
	Status          memory.MemoryStatus
	TopicKey        string
	Summary         string
	Content         string
	ContentSHA256   string
	TokenCount      int
	Tags            []string
	Entities        []string
	EpistemicStatus memory.EpistemicStatus
	AlphaScore      float32
	BetaScore       float32
	Confidence      float32
	Importance      float32
	UtilityScore    float32
	AccessCount     int
	Version         int
	NLIChecked      bool
	PrimarySourceID uuid.UUID
	ObservedAt      time.Time
	ValidFrom       time.Time
	ValidUntil      *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
	Metadata        map[string]any
}

// MemoryViewFromDomain maps a domain Memory entity into a MemoryView projection.
func MemoryViewFromDomain(m *memory.Memory) *MemoryView {
	if m == nil {
		return nil
	}
	return &MemoryView{
		ID:              m.ID,
		TenantID:        m.TenantID,
		ProjectID:       m.ProjectID,
		OwnerAgentID:    m.OwnerAgentID,
		TaskID:          m.TaskID,
		Type:            m.Type,
		ScopeType:       m.ScopeType,
		Status:          m.Status,
		TopicKey:        m.TopicKey,
		Summary:         m.Summary,
		Content:         m.Content,
		ContentSHA256:   m.ContentSHA256,
		TokenCount:      m.TokenCount,
		Tags:            append([]string(nil), m.Tags...),
		Entities:        append([]string(nil), m.Entities...),
		EpistemicStatus: m.EpistemicStatus,
		AlphaScore:      m.AlphaScore,
		BetaScore:       m.BetaScore,
		Confidence:      m.Confidence,
		Importance:      m.Importance,
		UtilityScore:    m.UtilityScore,
		AccessCount:     m.AccessCount,
		Version:         m.Version,
		NLIChecked:      m.NLIChecked,
		PrimarySourceID: m.PrimarySourceID,
		ObservedAt:      m.Clocks.ObservedAt,
		ValidFrom:       m.Clocks.ValidFrom,
		ValidUntil:      m.Clocks.ValidUntil,
		CreatedAt:       m.Clocks.CreatedAt,
		UpdatedAt:       m.UpdatedAt,
		Metadata:        cloneMap(m.Metadata),
	}
}

func cloneMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
