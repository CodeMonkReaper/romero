package memory

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/romero-project/romero/internal/domain/identity"
)

// Memory represents the core bitemporal cognitive entity.
type Memory struct {
	ID              uuid.UUID
	TenantID        uuid.UUID
	ProjectID       *uuid.UUID
	OwnerAgentID    *uuid.UUID
	TaskID          string
	Type            MemoryType
	ScopeType       identity.ScopeType
	Status          MemoryStatus
	TopicKey        string
	Summary         string
	Content         string
	ContentSHA256   string
	TokenCount      int
	Tags            []string
	Entities        []string
	EpistemicStatus EpistemicStatus
	AlphaScore      float32
	BetaScore       float32
	Confidence      float32
	Importance      float32
	UtilityScore    float32
	AccessCount     int
	Version         int
	NLIChecked      bool
	PrimarySourceID uuid.UUID
	IdempotencyKey  string
	Clocks          TriTemporalWindow
	UpdatedAt       time.Time
	LastAccessedAt  *time.Time
	Metadata        map[string]any
}

// MemoryVersion is an immutable self-contained snapshot of a memory at a given version.
type MemoryVersion struct {
	ID              uuid.UUID
	MemoryID        uuid.UUID
	Version         int
	Summary         string
	Content         string
	Tags            []string
	Entities        []string
	Metadata        map[string]any
	EpistemicStatus EpistemicStatus
	AlphaScore      float32
	BetaScore       float32
	Importance      float32
	UtilityScore    float32
	Status          MemoryStatus
	ValidFrom       time.Time
	ValidUntil      *time.Time
	ChangedBySource *uuid.UUID
	ChangeReason    string
	CreatedAt       time.Time
}

// MemoryRelation connects two distinct memories within a tenant.
type MemoryRelation struct {
	ID               uuid.UUID
	TenantID         uuid.UUID
	ProjectID        *uuid.UUID
	FromMemoryID     uuid.UUID
	ToMemoryID       uuid.UUID
	RelationType     RelationType
	Weight           float32
	NLIEntailment    *float32
	NLIContradiction *float32
	SourceID         *uuid.UUID
	ValidFrom        time.Time
	ValidUntil       *time.Time
	IsActive         bool
	Notes            string
	CreatedAt        time.Time
}

// MemoryFeedback captures an immutable empirical signal applied to a memory.
type MemoryFeedback struct {
	ID           uuid.UUID
	TenantID     uuid.UUID
	MemoryID     uuid.UUID
	AgentID      *uuid.UUID
	TaskID       string
	Signal       FeedbackSignal
	AlphaDelta   float32
	BetaDelta    float32
	UtilityDelta float32
	Comment      string
	CreatedAt    time.Time
}

// MemoryJob represents an asynchronous enrichment job enqueued inside a transaction.
type MemoryJob struct {
	ID          uuid.UUID
	TenantID    uuid.UUID
	ProjectID   *uuid.UUID
	MemoryID    *uuid.UUID
	JobType     string
	Priority    int16
	Status      JobStatus
	Payload     map[string]any
	Attempts    int
	MaxAttempts int
	LastError   string
	ScheduledAt time.Time
	LockedAt    *time.Time
	CompletedAt *time.Time
	CreatedAt   time.Time
}

// AuditEvent captures security decisions inside or outside business rollbacks.
type AuditEvent struct {
	ID            uuid.UUID
	TenantID      *uuid.UUID
	PrincipalID   *uuid.UUID
	AgentID       *uuid.UUID
	Operation     string
	ResourceType  string
	ResourceID    *uuid.UUID
	ScopeUsed     string
	CorrelationID string
	Outcome       AuditOutcome
	Details       map[string]any
	CreatedAt     time.Time
}

func validateSnapshotFields(
	version int,
	summary, content string,
	epistemic EpistemicStatus,
	status MemoryStatus,
	alpha, beta, importance, utility float32,
) error {
	if version < 1 {
		return ErrInvalidBounds
	}
	if strings.TrimSpace(summary) == "" || strings.TrimSpace(content) == "" {
		return fmt.Errorf("%w: summary and content must be non-empty", ErrInvalidBounds)
	}
	if !epistemic.Valid() {
		return ErrInvalidEpistemicStatus
	}
	if !status.Valid() {
		return ErrInvalidMemoryStatus
	}
	if !isFinitePositive32(alpha) || !isFinitePositive32(beta) ||
		!isFiniteInRange32(importance, 0.0, 1.0) || !isFiniteInRange32(utility, -1.0, 1.0) {
		return ErrInvalidBayesianParams
	}
	return nil
}

// Validate verifies structural, provenance, scope, Bayesian, and tri-temporal invariants.
func (m Memory) Validate() error {
	if m.ID == uuid.Nil || m.TenantID == uuid.Nil {
		return ErrInvalidIdentifier
	}
	if m.PrimarySourceID == uuid.Nil {
		return ErrMissingProvenance
	}
	if !m.Type.Valid() {
		return ErrInvalidMemoryType
	}
	if err := validateScopeOwnership(m.ScopeType, m.ProjectID, m.OwnerAgentID, m.TaskID); err != nil {
		return err
	}
	if !isLowerHexSHA256(m.ContentSHA256) {
		return ErrInvalidHash
	}
	if m.TokenCount < 0 || m.AccessCount < 0 {
		return ErrInvalidBounds
	}
	if err := validateSnapshotFields(m.Version, m.Summary, m.Content, m.EpistemicStatus, m.Status, m.AlphaScore, m.BetaScore, m.Importance, m.UtilityScore); err != nil {
		return err
	}
	if err := m.Clocks.Validate(); err != nil {
		return err
	}
	if m.UpdatedAt.IsZero() {
		return ErrInvalidTimestamp
	}
	return nil
}

// ScopeTarget maps the memory into the identity package's partition selector.
func (m Memory) ScopeTarget() identity.MemoryScopeTarget {
	return identity.MemoryScopeTarget{
		TenantID:     m.TenantID,
		ProjectID:    m.ProjectID,
		ScopeType:    m.ScopeType,
		OwnerAgentID: m.OwnerAgentID,
		TaskID:       m.TaskID,
	}
}

// ValidateMutable rejects mutations on closed lifecycle states with ErrClosedMemoryConflict.
func (m Memory) ValidateMutable() error {
	if !m.Status.Valid() {
		return ErrInvalidMemoryStatus
	}
	if m.Status.IsClosed() {
		return ErrClosedMemoryConflict
	}
	return nil
}

// CoversPointInTime reports whether this snapshot existed by knownAt and covered validAt.
func (v MemoryVersion) CoversPointInTime(validAt, knownAt time.Time) bool {
	if v.CreatedAt.IsZero() || v.ValidFrom.IsZero() || v.CreatedAt.After(knownAt) {
		return false
	}
	return (TriTemporalWindow{
		ObservedAt: v.CreatedAt,
		CreatedAt:  v.CreatedAt,
		ValidFrom:  v.ValidFrom,
		ValidUntil: v.ValidUntil,
	}).Covers(validAt)
}

// Validate checks immutable version snapshot invariants.
func (v MemoryVersion) Validate() error {
	if v.ID == uuid.Nil || v.MemoryID == uuid.Nil {
		return ErrInvalidIdentifier
	}
	if err := validateSnapshotFields(v.Version, v.Summary, v.Content, v.EpistemicStatus, v.Status, v.AlphaScore, v.BetaScore, v.Importance, v.UtilityScore); err != nil {
		return err
	}
	if v.CreatedAt.IsZero() || v.ValidFrom.IsZero() {
		return ErrInvalidTimestamp
	}
	if v.ValidUntil != nil && v.ValidUntil.Before(v.ValidFrom) {
		return ErrInvalidTemporalWindow
	}
	return nil
}

// Validate enforces bitemporal, weight, and anti-self-loop invariants on a relation.
func (r MemoryRelation) Validate() error {
	if r.ID == uuid.Nil || r.TenantID == uuid.Nil || r.FromMemoryID == uuid.Nil || r.ToMemoryID == uuid.Nil {
		return ErrInvalidIdentifier
	}
	if r.FromMemoryID == r.ToMemoryID {
		return ErrSelfLoopRelation
	}
	if !r.RelationType.Valid() {
		return ErrInvalidRelationType
	}
	if !isFiniteInRange32(r.Weight, 0.0, 1.0) {
		return ErrInvalidBounds
	}
	if r.NLIEntailment != nil && !isFiniteInRange32(*r.NLIEntailment, 0.0, 1.0) {
		return ErrInvalidBounds
	}
	if r.NLIContradiction != nil && !isFiniteInRange32(*r.NLIContradiction, 0.0, 1.0) {
		return ErrInvalidBounds
	}
	if r.ValidFrom.IsZero() || r.CreatedAt.IsZero() {
		return ErrInvalidTimestamp
	}
	if r.ValidUntil != nil && r.ValidUntil.Before(r.ValidFrom) {
		return ErrInvalidTemporalWindow
	}
	return nil
}

// Validate checks immutable feedback log invariants.
func (f MemoryFeedback) Validate() error {
	if f.ID == uuid.Nil || f.TenantID == uuid.Nil || f.MemoryID == uuid.Nil {
		return ErrInvalidIdentifier
	}
	if !f.Signal.Valid() {
		return ErrInvalidFeedbackSignal
	}
	if !isFiniteInRange32(f.AlphaDelta, 0.0, 100.0) || !isFiniteInRange32(f.BetaDelta, 0.0, 100.0) ||
		!isFiniteInRange32(f.UtilityDelta, -2.0, 2.0) {
		return ErrInvalidBayesianParams
	}
	if f.CreatedAt.IsZero() {
		return ErrInvalidTimestamp
	}
	return nil
}

// Validate checks asynchronous memory_jobs invariants.
func (j MemoryJob) Validate() error {
	if j.ID == uuid.Nil || j.TenantID == uuid.Nil {
		return ErrInvalidIdentifier
	}
	if strings.TrimSpace(j.JobType) == "" {
		return fmt.Errorf("%w: job_type is required", ErrInvalidBounds)
	}
	if !j.Status.Valid() {
		return ErrInvalidJobStatus
	}
	if j.Attempts < 0 || j.MaxAttempts < 1 {
		return ErrInvalidBounds
	}
	if j.ScheduledAt.IsZero() || j.CreatedAt.IsZero() {
		return ErrInvalidTimestamp
	}
	return nil
}

// Validate checks security audit event invariants. TenantID may be nil for 401 unauthenticated events.
func (a AuditEvent) Validate() error {
	if a.ID == uuid.Nil {
		return ErrInvalidIdentifier
	}
	if strings.TrimSpace(a.Operation) == "" || strings.TrimSpace(a.ResourceType) == "" {
		return fmt.Errorf("%w: operation and resource_type are required", ErrInvalidBounds)
	}
	if !a.Outcome.Valid() {
		return ErrInvalidAuditOutcome
	}
	if a.CreatedAt.IsZero() {
		return ErrInvalidTimestamp
	}
	return nil
}

func validateScopeOwnership(scope identity.ScopeType, projectID, ownerAgentID *uuid.UUID, taskID string) error {
	switch scope {
	case identity.ScopeAgentPrivate:
		if !validUUIDPtr(projectID) || !validUUIDPtr(ownerAgentID) {
			return ErrInvalidIdentifier
		}
	case identity.ScopeTaskShared:
		if !validUUIDPtr(projectID) {
			return ErrInvalidIdentifier
		}
		if strings.TrimSpace(taskID) == "" {
			return identity.ErrMissingTaskID
		}
	case identity.ScopeProjectShared:
		if !validUUIDPtr(projectID) {
			return ErrInvalidIdentifier
		}
	case identity.ScopeOrgShared, identity.ScopeGlobalPublic:
	default:
		return ErrInvalidBounds
	}
	return nil
}

func validUUIDPtr(id *uuid.UUID) bool {
	return id != nil && *id != uuid.Nil
}

// NewVersionFromMemory builds an immutable MemoryVersion snapshot from m for state transitions.
func NewVersionFromMemory(m Memory, reason string, createdAt time.Time) MemoryVersion {
	var changedBy *uuid.UUID
	if m.PrimarySourceID != uuid.Nil {
		src := m.PrimarySourceID
		changedBy = &src
	}
	return MemoryVersion{
		ID:              uuid.New(),
		MemoryID:        m.ID,
		Version:         m.Version,
		Summary:         m.Summary,
		Content:         m.Content,
		Tags:            append([]string(nil), m.Tags...),
		Entities:        append([]string(nil), m.Entities...),
		Metadata:        m.Metadata,
		EpistemicStatus: m.EpistemicStatus,
		AlphaScore:      m.AlphaScore,
		BetaScore:       m.BetaScore,
		Importance:      m.Importance,
		UtilityScore:    m.UtilityScore,
		Status:          m.Status,
		ValidFrom:       m.Clocks.ValidFrom,
		ValidUntil:      m.Clocks.ValidUntil,
		ChangedBySource: changedBy,
		ChangeReason:    reason,
		CreatedAt:       createdAt,
	}
}

func isLowerHexSHA256(s string) bool {
	if len(s) != 64 {
		return false
	}
	for i := range len(s) {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
