// Package provenance defines immutable source identity and validation invariants for memories.
package provenance

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

// SourceType enumerates the valid origin channels for a memory source.
type SourceType string

const (
	SourceAgentExecution   SourceType = "agent_execution"
	SourceUserInput        SourceType = "user_input"
	SourceToolOutput       SourceType = "tool_output"
	SourceDocumentIngest   SourceType = "document_ingest"
	SourceREMConsolidation SourceType = "rem_consolidation"
	SourceHumanReview      SourceType = "human_review"
)

var (
	ErrMissingID               = errors.New("id is required")
	ErrMissingTenant           = errors.New("tenant_id is required")
	ErrInvalidSourceType       = errors.New("invalid source_type")
	ErrMissingExternalSourceID = errors.New("external_source_id is required")
	ErrInvalidExcerptHash      = errors.New("raw_excerpt_hash must be 64 lowercase hex characters")
	ErrMissingCreatedAt        = errors.New("created_at is required")
)

// MemorySource represents an immutable provenance record for one or more memories.
type MemorySource struct {
	ID               uuid.UUID
	TenantID         uuid.UUID
	ProjectID        *uuid.UUID
	SourceType       SourceType
	ExternalSourceID string
	ActorPrincipalID *uuid.UUID
	ActorAgentID     *uuid.UUID
	URI              string
	RawExcerptHash   string
	Metadata         map[string]any
	CreatedAt        time.Time
}

// Valid reports whether the source type is one of the canonical values.
func (s SourceType) Valid() bool {
	switch s {
	case SourceAgentExecution,
		SourceUserInput,
		SourceToolOutput,
		SourceDocumentIngest,
		SourceREMConsolidation,
		SourceHumanReview:
		return true
	default:
		return false
	}
}

// Validate checks immutable provenance invariants required by Principio 15.
func (m MemorySource) Validate() error {
	if m.ID == uuid.Nil {
		return ErrMissingID
	}
	if m.TenantID == uuid.Nil {
		return ErrMissingTenant
	}
	if !m.SourceType.Valid() {
		return ErrInvalidSourceType
	}
	if strings.TrimSpace(m.ExternalSourceID) == "" {
		return ErrMissingExternalSourceID
	}
	if m.RawExcerptHash != "" && !isLowerHexSHA256(m.RawExcerptHash) {
		return ErrInvalidExcerptHash
	}
	if m.CreatedAt.IsZero() {
		return ErrMissingCreatedAt
	}
	return nil
}

// SourceRef returns a compact, deterministic provenance reference string for XML envelopes.
func (m MemorySource) SourceRef() string {
	ext := strings.TrimSpace(m.ExternalSourceID)
	if !m.SourceType.Valid() {
		return ext
	}
	if ext == "" {
		return string(m.SourceType)
	}
	return string(m.SourceType) + ":" + ext
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
