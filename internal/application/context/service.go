// Package contextbuilder implements Zero-Trust submodular context packaging
// with progressive disclosure and untrusted XML enveloping.
package contextbuilder

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/romero-project/romero/internal/application/retrieval"
	"github.com/romero-project/romero/internal/domain/identity"
	"github.com/romero-project/romero/internal/domain/memory"
	"github.com/romero-project/romero/internal/domain/security"
)

const (
	minContextTokens = 64
	maxContextTokens = 32768
)

var (
	// ErrUnauthorized indicates missing or invalid caller identity.
	ErrUnauthorized = retrieval.ErrUnauthorized
	// ErrForbidden mirrors identity.ErrForbidden for policy denials.
	ErrForbidden = retrieval.ErrForbidden
	// ErrValidation indicates invalid query, hops, or max_tokens parameters.
	ErrValidation = retrieval.ErrValidation
)

// CandidateRetriever provides scored, authorized memory candidates for context packaging.
type CandidateRetriever interface {
	SearchForContext(ctx context.Context, auth identity.AuthContext, q retrieval.SearchQuery) ([]retrieval.ContextCandidate, bool, error)
}

// BuildContextRequest carries parameters for bounded submodular context packaging.
type BuildContextRequest struct {
	Query               string
	ProjectIDs          []uuid.UUID
	Scopes              []identity.ScopeType
	TaskID              string
	MaxTokens           int
	Hops                int
	IncludeSuperseded   bool
	IncludeConsolidated bool
	CorrelationID       string
}

// ContextItem represents a single selected memory wrapped in an untrusted XML envelope.
type ContextItem struct {
	MemoryID         uuid.UUID `json:"memory_id"`
	IsCompact        bool      `json:"is_compact"`
	TokenCost        int       `json:"token_cost"`
	FinalScore       float32   `json:"final_score"`
	HasConflictAlert bool      `json:"has_conflict_alert"`
	XMLEnvelope      string    `json:"xml_envelope"`
}

// BuildContextResult holds the bounded XML context package and degradation metadata.
type BuildContextResult struct {
	Items            []ContextItem `json:"items"`
	CombinedXML      string        `json:"combined_xml"`
	UsedTokens       int           `json:"used_tokens"`
	MaxTokens        int           `json:"max_tokens"`
	DegradedMode     bool          `json:"degraded_mode"`
	HasConflictAlert bool          `json:"has_conflict_alert"`
}

// Service orchestrates submodular context selection and untrusted XML packaging.
type Service struct {
	retriever CandidateRetriever
	audit     memory.AuditLogger
	clock     func() time.Time
}

// NewService constructs a ContextBuilderService.
func NewService(retriever CandidateRetriever, audit memory.AuditLogger, clock func() time.Time) (*Service, error) {
	if retriever == nil || audit == nil {
		return nil, errors.New("retriever and audit logger are required")
	}
	if clock == nil {
		clock = func() time.Time { return time.Now().UTC() }
	}
	return &Service{
		retriever: retriever,
		audit:     audit,
		clock:     clock,
	}, nil
}

// BuildContext selects a diverse, non-redundant subset of authorized memories within MaxTokens.
func (s *Service) BuildContext(ctx context.Context, auth identity.AuthContext, req BuildContextRequest) (*BuildContextResult, error) {
	if auth.TenantID == uuid.Nil || auth.PrincipalID == uuid.Nil {
		return nil, ErrUnauthorized
	}
	if req.MaxTokens < minContextTokens || req.MaxTokens > maxContextTokens {
		return nil, fmt.Errorf("%w: max_tokens must be within [%d, %d]", ErrValidation, minContextTokens, maxContextTokens)
	}

	candidates, degraded, err := s.retriever.SearchForContext(ctx, auth, retrieval.SearchQuery{
		Query:               req.Query,
		ProjectIDs:          req.ProjectIDs,
		Scopes:              req.Scopes,
		TaskID:              req.TaskID,
		Limit:               100,
		Hops:                req.Hops,
		IncludeSuperseded:   req.IncludeSuperseded,
		IncludeConsolidated: req.IncludeConsolidated,
		CorrelationID:       req.CorrelationID,
	})
	if err != nil {
		return nil, err
	}

	byID := make(map[uuid.UUID]retrieval.ContextCandidate, len(candidates))
	subInputs := make([]memory.SubmodularCandidate, 0, len(candidates))
	for _, c := range candidates {
		byID[c.ID] = c
		fullTokens := c.TokenCount
		if fullTokens <= 0 {
			fullTokens = estimateTokens(c.Content)
		}
		summaryTokens := estimateTokens(c.Summary)
		subInputs = append(subInputs, memory.SubmodularCandidate{
			Memory: memory.Memory{
				ID:              c.ID,
				Type:            c.Type,
				ScopeType:       c.ScopeType,
				Status:          c.Status,
				EpistemicStatus: c.EpistemicStatus,
				Confidence:      c.Confidence,
				TopicKey:        c.TopicKey,
				Summary:         c.Summary,
				Content:         c.Content,
				TokenCount:      fullTokens,
				Tags:            c.Tags,
				Entities:        c.Entities,
				PrimarySourceID: c.PrimarySourceID,
			},
			FinalScore:       c.FinalScore,
			DenseVec:         c.DenseVec,
			HasConflictAlert: c.HasConflictAlert,
			SummaryTokens:    summaryTokens,
			FullTokens:       fullTokens,
		})
	}

	selected := memory.SelectSubmodularContext(subInputs, req.MaxTokens, degraded)

	items := make([]ContextItem, 0, len(selected))
	xmlParts := make([]string, 0, len(selected))
	usedTokens := 0
	hasConflict := false

	for _, sel := range selected {
		c := byID[sel.MemoryID]
		payload := c.Content
		if sel.IsCompact || strings.TrimSpace(payload) == "" {
			payload = c.Summary
		}
		envelope := security.WrapUntrustedXML(
			c.ID,
			string(c.Type),
			string(c.EpistemicStatus),
			c.Confidence,
			c.PrimarySourceID.String(),
			payload,
		)
		items = append(items, ContextItem{
			MemoryID:         sel.MemoryID,
			IsCompact:        sel.IsCompact,
			TokenCost:        sel.TokenCost,
			FinalScore:       sel.FinalScore,
			HasConflictAlert: sel.HasConflictAlert,
			XMLEnvelope:      envelope,
		})
		xmlParts = append(xmlParts, envelope)
		usedTokens += sel.TokenCost
		if sel.HasConflictAlert {
			hasConflict = true
		}
	}

	now := s.clock()
	if err := s.audit.RecordAllowed(ctx, memory.AuditEvent{
		ID:            uuid.New(),
		TenantID:      &auth.TenantID,
		PrincipalID:   &auth.PrincipalID,
		AgentID:       auth.AgentID,
		Operation:     "build_context",
		ResourceType:  "memory",
		CorrelationID: req.CorrelationID,
		Outcome:       memory.AuditAllowed,
		Details:       map[string]any{"items": len(items), "used_tokens": usedTokens, "degraded_mode": degraded},
		CreatedAt:     now,
	}); err != nil {
		return nil, fmt.Errorf("%w: %v", retrieval.ErrOperational, err)
	}

	return &BuildContextResult{
		Items:            items,
		CombinedXML:      strings.Join(xmlParts, "\n"),
		UsedTokens:       usedTokens,
		MaxTokens:        req.MaxTokens,
		DegradedMode:     degraded,
		HasConflictAlert: hasConflict,
	}, nil
}

func estimateTokens(s string) int {
	n := (utf8.RuneCountInString(s) + 3) / 4
	if n < 1 && strings.TrimSpace(s) != "" {
		return 1
	}
	return n
}
