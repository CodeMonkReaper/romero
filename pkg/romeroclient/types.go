package romeroclient

import (
	"time"

	"github.com/google/uuid"
)

// ExplicitRelationInput defines an explicit directed edge created alongside a memory.
type ExplicitRelationInput struct {
	ToMemoryID   uuid.UUID `json:"to_memory_id"`
	RelationType string    `json:"relation_type"`
	Weight       float32   `json:"weight,omitempty"`
	Notes        string    `json:"notes,omitempty"`
}

// CaptureRequest is the payload for POST /api/v1/memories.
type CaptureRequest struct {
	ProjectID        *uuid.UUID              `json:"project_id,omitempty"`
	TaskID           string                  `json:"task_id,omitempty"`
	Type             string                  `json:"type"`
	ScopeType        string                  `json:"scope_type"`
	TopicKey         string                  `json:"topic_key,omitempty"`
	Summary          string                  `json:"summary"`
	Content          string                  `json:"content"`
	Tags             []string                `json:"tags,omitempty"`
	Entities         []string                `json:"entities,omitempty"`
	EpistemicStatus  string                  `json:"epistemic_status"`
	Importance       float32                 `json:"importance"`
	SourceType       string                  `json:"source_type"`
	ExternalSourceID string                  `json:"external_source_id"`
	URI              string                  `json:"uri,omitempty"`
	RawExcerptHash   string                  `json:"raw_excerpt_hash,omitempty"`
	IdempotencyKey   string                  `json:"idempotency_key,omitempty"`
	Relations        []ExplicitRelationInput `json:"relations,omitempty"`
}

// MemoryView is the compact view returned on synchronous write/mutation endpoints.
// Field names match the server's PascalCase JSON serialization.
type MemoryView struct {
	ID              uuid.UUID  `json:"ID"`
	TenantID        uuid.UUID  `json:"TenantID"`
	ProjectID       *uuid.UUID `json:"ProjectID,omitempty"`
	OwnerAgentID    *uuid.UUID `json:"OwnerAgentID,omitempty"`
	TaskID          string     `json:"TaskID,omitempty"`
	Type            string     `json:"Type"`
	ScopeType       string     `json:"ScopeType"`
	Status          string     `json:"Status"`
	TopicKey        string     `json:"TopicKey,omitempty"`
	Summary         string     `json:"Summary"`
	Content         string     `json:"Content,omitempty"`
	EpistemicStatus string     `json:"EpistemicStatus"`
	Confidence      float32    `json:"Confidence"`
	Importance      float32    `json:"Importance"`
	UtilityScore    float32    `json:"UtilityScore"`
	Version         int        `json:"Version"`
	PrimarySourceID uuid.UUID  `json:"PrimarySourceID"`
}

// CaptureResponse models synchronous (200/201) or opaque queued (202 Accepted) capture outcomes.
type CaptureResponse struct {
	Accepted  bool        `json:"-"`
	RequestID *uuid.UUID  `json:"request_id,omitempty"`
	Memory    *MemoryView `json:"memory,omitempty"`
}

// Memory represents a full ROMERO memory node.
type Memory struct {
	ID              uuid.UUID  `json:"ID"`
	TenantID        uuid.UUID  `json:"TenantID"`
	ProjectID       *uuid.UUID `json:"ProjectID,omitempty"`
	OwnerAgentID    *uuid.UUID `json:"OwnerAgentID,omitempty"`
	TaskID          string     `json:"TaskID,omitempty"`
	Type            string     `json:"Type"`
	ScopeType       string     `json:"ScopeType"`
	Status          string     `json:"Status"`
	TopicKey        string     `json:"TopicKey,omitempty"`
	Summary         string     `json:"Summary"`
	Content         string     `json:"Content"`
	Tags            []string   `json:"Tags,omitempty"`
	Entities        []string   `json:"Entities,omitempty"`
	EpistemicStatus string     `json:"EpistemicStatus"`
	Confidence      float32    `json:"Confidence"`
	UtilityScore    float32    `json:"UtilityScore"`
	Importance      float32    `json:"Importance"`
	Version         int        `json:"Version"`
	PrimarySourceID uuid.UUID  `json:"PrimarySourceID"`
	UpdatedAt       time.Time  `json:"UpdatedAt"`
}

// MemoryVersion represents a bitemporal version of a memory.
type MemoryVersion struct {
	ID              uuid.UUID  `json:"ID"`
	MemoryID        uuid.UUID  `json:"MemoryID"`
	Version         int        `json:"Version"`
	Summary         string     `json:"Summary"`
	Content         string     `json:"Content"`
	EpistemicStatus string     `json:"EpistemicStatus"`
	Status          string     `json:"Status"`
	ChangeReason    string     `json:"ChangeReason,omitempty"`
	ValidFrom       time.Time  `json:"ValidFrom"`
	ValidUntil      *time.Time `json:"ValidUntil,omitempty"`
	CreatedAt       time.Time  `json:"CreatedAt"`
}

// MemoryRelation represents a visible directed edge between two memories.
type MemoryRelation struct {
	ID               uuid.UUID `json:"ID"`
	FromMemoryID     uuid.UUID `json:"FromMemoryID"`
	ToMemoryID       uuid.UUID `json:"ToMemoryID"`
	RelationType     string    `json:"RelationType"`
	Weight           float32   `json:"Weight"`
	NLIEntailment    *float32  `json:"NLIEntailment,omitempty"`
	NLIContradiction *float32  `json:"NLIContradiction,omitempty"`
	IsActive         bool      `json:"IsActive"`
	Notes            string    `json:"Notes,omitempty"`
}

// GetMemoryOptions configures optional bitemporal Point-in-Time query parameters.
type GetMemoryOptions struct {
	AsOf    string
	ValidAt string
	KnownAt string
}

// GetMemoryResponse is returned by GET /api/v1/memories/{id}.
type GetMemoryResponse struct {
	Memory               Memory           `json:"memory"`
	Versions             []MemoryVersion  `json:"versions,omitempty"`
	Relations            []MemoryRelation `json:"relations,omitempty"`
	PointInTimeVersion   *MemoryVersion   `json:"point_in_time_version,omitempty"`
	UntrustedXMLEnvelope string           `json:"untrusted_xml_envelope"`
}

// UpdateMemoryRequest is the payload for PATCH /api/v1/memories/{id}.
type UpdateMemoryRequest struct {
	TaskID         string   `json:"task_id,omitempty"`
	Summary        string   `json:"summary"`
	Content        string   `json:"content"`
	Tags           []string `json:"tags,omitempty"`
	Entities       []string `json:"entities,omitempty"`
	Importance     *float32 `json:"importance,omitempty"`
	ChangeReason   string   `json:"change_reason,omitempty"`
	IdempotencyKey string   `json:"idempotency_key,omitempty"`
}

// SupersedeMemoryRequest is the payload for POST /api/v1/memories/{id}/supersede.
type SupersedeMemoryRequest struct {
	TaskID       string         `json:"task_id,omitempty"`
	ChangeReason string         `json:"change_reason,omitempty"`
	Capture      CaptureRequest `json:"capture"`
}

// SupersedeMemoryResponse is returned by POST /api/v1/memories/{id}/supersede.
type SupersedeMemoryResponse struct {
	Accepted  bool        `json:"-"`
	RequestID *uuid.UUID  `json:"request_id,omitempty"`
	OldMemory *MemoryView `json:"old_memory,omitempty"`
	NewMemory *MemoryView `json:"new_memory,omitempty"`
}

// ArchiveMemoryRequest is the payload for DELETE /api/v1/memories/{id}.
type ArchiveMemoryRequest struct {
	TaskID         string `json:"task_id,omitempty"`
	Reason         string `json:"reason,omitempty"`
	IdempotencyKey string `json:"idempotency_key,omitempty"`
}

// MutationResponse models synchronous (200) or opaque queued (202 Accepted) update/archive outcomes.
type MutationResponse struct {
	Accepted  bool        `json:"-"`
	RequestID *uuid.UUID  `json:"request_id,omitempty"`
	Memory    *MemoryView `json:"memory,omitempty"`
}

// FeedbackRequest is the payload for POST /api/v1/feedback.
type FeedbackRequest struct {
	MemoryID          uuid.UUID       `json:"memory_id"`
	TaskID            string          `json:"task_id,omitempty"`
	Signal            string          `json:"signal"`
	Comment           string          `json:"comment,omitempty"`
	IdempotencyKey    string          `json:"idempotency_key,omitempty"`
	ReplacementMemory *CaptureRequest `json:"replacement_memory,omitempty"`
}

// FeedbackResponse models synchronous recalibration or opaque queued (202 Accepted) feedback outcomes.
type FeedbackResponse struct {
	Accepted            bool       `json:"-"`
	RequestID           *uuid.UUID `json:"request_id,omitempty"`
	MemoryID            uuid.UUID  `json:"memory_id,omitempty"`
	NewAlpha            float32    `json:"new_alpha,omitempty"`
	NewBeta             float32    `json:"new_beta,omitempty"`
	NewConfidence       float32    `json:"new_confidence,omitempty"`
	NewUtility          float32    `json:"new_utility,omitempty"`
	NewEpistemic        string     `json:"new_epistemic,omitempty"`
	NewStatus           string     `json:"new_status,omitempty"`
	Quarantined         bool       `json:"quarantined,omitempty"`
	ReplacementMemoryID *uuid.UUID `json:"replacement_memory_id,omitempty"`
}

// RequestStatusResponse is returned by GET /api/v1/requests/{request_id}.
type RequestStatusResponse struct {
	RequestID uuid.UUID `json:"request_id"`
	Status    string    `json:"status"`
}

// SearchRequest is the payload for POST /api/v1/memories/search.
type SearchRequest struct {
	Query               string      `json:"query"`
	ProjectIDs          []uuid.UUID `json:"project_ids,omitempty"`
	Scopes              []string    `json:"scopes,omitempty"`
	TaskID              string      `json:"task_id,omitempty"`
	MemoryTypes         []string    `json:"memory_types,omitempty"`
	MinConfidence       *float32    `json:"min_confidence,omitempty"`
	Limit               int         `json:"limit,omitempty"`
	Hops                int         `json:"hops,omitempty"`
	IncludeSuperseded   bool        `json:"include_superseded,omitempty"`
	IncludeConsolidated bool        `json:"include_consolidated,omitempty"`
}

// SearchCandidate is the compact progressive-disclosure search item (never exposes content).
type SearchCandidate struct {
	ID               uuid.UUID `json:"id"`
	Summary          string    `json:"summary"`
	Type             string    `json:"type"`
	ScopeType        string    `json:"scope_type"`
	Status           string    `json:"status"`
	EpistemicStatus  string    `json:"epistemic_status"`
	Confidence       float32   `json:"confidence"`
	FinalScore       float32   `json:"final_score"`
	RelationsCount   int       `json:"relations_count"`
	MinHopDepth      int       `json:"min_hop_depth"`
	HasConflictAlert bool      `json:"has_conflict_alert"`
	ObservedAt       time.Time `json:"observed_at"`
}

// SearchResponse is returned by POST /api/v1/memories/search.
type SearchResponse struct {
	Candidates   []SearchCandidate `json:"candidates"`
	DegradedMode bool              `json:"degraded_mode"`
	TotalFound   int               `json:"total_found"`
}

// BuildContextRequest is the payload for POST /api/v1/context.
type BuildContextRequest struct {
	Query               string      `json:"query"`
	ProjectIDs          []uuid.UUID `json:"project_ids,omitempty"`
	Scopes              []string    `json:"scopes,omitempty"`
	TaskID              string      `json:"task_id,omitempty"`
	MaxTokens           int         `json:"max_tokens"`
	Hops                int         `json:"hops,omitempty"`
	IncludeSuperseded   bool        `json:"include_superseded,omitempty"`
	IncludeConsolidated bool        `json:"include_consolidated,omitempty"`
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

// BuildContextResponse is returned by POST /api/v1/context.
type BuildContextResponse struct {
	Items            []ContextItem `json:"items"`
	CombinedXML      string        `json:"combined_xml"`
	UsedTokens       int           `json:"used_tokens"`
	MaxTokens        int           `json:"max_tokens"`
	DegradedMode     bool          `json:"degraded_mode"`
	HasConflictAlert bool          `json:"has_conflict_alert"`
}

// SubgraphNode represents a node in a bounded graph neighborhood.
type SubgraphNode struct {
	ID               uuid.UUID `json:"id"`
	Summary          string    `json:"summary"`
	Type             string    `json:"type"`
	ScopeType        string    `json:"scope_type"`
	Status           string    `json:"status"`
	EpistemicStatus  string    `json:"epistemic_status"`
	Confidence       float32   `json:"confidence"`
	HopDepth         int       `json:"hop_depth"`
	HasConflictAlert bool      `json:"has_conflict_alert"`
}

// SubgraphEdge represents an edge in a bounded graph neighborhood.
type SubgraphEdge struct {
	FromMemoryID uuid.UUID `json:"from_memory_id"`
	ToMemoryID   uuid.UUID `json:"to_memory_id"`
	RelationType string    `json:"relation_type"`
	Weight       float32   `json:"weight"`
}

// SubgraphResponse is returned by GET /api/v1/memories/{id}/subgraph.
type SubgraphResponse struct {
	RootMemoryID uuid.UUID      `json:"root_memory_id"`
	Nodes        []SubgraphNode `json:"nodes"`
	Edges        []SubgraphEdge `json:"edges"`
}

// ListConflictsOptions filters active NLI contradiction pairs.
type ListConflictsOptions struct {
	ProjectID *uuid.UUID
	Limit     int
	Offset    int
}

// ConflictMemory summarizes one endpoint of a contradiction pair.
type ConflictMemory struct {
	ID              uuid.UUID `json:"id"`
	ScopeType       string    `json:"scope_type"`
	Status          string    `json:"status"`
	EpistemicStatus string    `json:"epistemic_status"`
	TopicKey        string    `json:"topic_key,omitempty"`
	Summary         string    `json:"summary"`
	Confidence      float32   `json:"confidence"`
}

// ConflictRecord models an active or resolved NLI contradiction pair.
type ConflictRecord struct {
	ConflictID       uuid.UUID      `json:"conflict_id"`
	TenantID         uuid.UUID      `json:"tenant_id"`
	ProjectID        *uuid.UUID     `json:"project_id,omitempty"`
	FromMemory       ConflictMemory `json:"from_memory"`
	ToMemory         ConflictMemory `json:"to_memory"`
	Weight           float32        `json:"weight"`
	NLIEntailment    *float32       `json:"nli_entailment,omitempty"`
	NLIContradiction *float32       `json:"nli_contradiction,omitempty"`
	IsActive         bool           `json:"is_active"`
	Notes            string         `json:"notes,omitempty"`
}

// ResolveConflictRequest is the payload for POST /api/v1/conflicts/{id}/resolve.
type ResolveConflictRequest struct {
	WinnerMemoryID uuid.UUID `json:"winner_memory_id"`
	Summary        string    `json:"summary,omitempty"`
	Content        string    `json:"content,omitempty"`
	Reason         string    `json:"reason,omitempty"`
}

// ConsolidateRequest is the payload for POST /api/v1/admin/consolidate.
type ConsolidateRequest struct {
	ProjectID *uuid.UUID `json:"project_id,omitempty"`
	DryRun    bool       `json:"dry_run,omitempty"`
}

// DistilledMemoryDraft previews a distilled semantic memory in dry-run consolidation.
type DistilledMemoryDraft struct {
	TopicKey   string   `json:"topic_key,omitempty"`
	Summary    string   `json:"summary"`
	Content    string   `json:"content"`
	Tags       []string `json:"tags,omitempty"`
	Entities   []string `json:"entities,omitempty"`
	SourceIDs  []string `json:"source_ids,omitempty"`
	Importance float32  `json:"importance"`
}

// ConsolidateResponse is returned by POST /api/v1/admin/consolidate.
type ConsolidateResponse struct {
	ClustersFound        int                    `json:"clusters_found"`
	DistilledCount       int                    `json:"distilled_count"`
	ConsolidatedEpisodes int                    `json:"consolidated_episodes"`
	Preempted            bool                   `json:"preempted"`
	CreatedMemoryIDs     []uuid.UUID            `json:"created_memory_ids,omitempty"`
	Drafts               []DistilledMemoryDraft `json:"drafts,omitempty"`
}

// GovernorSnapshot reports PowerGovernor resource scheduling state.
type GovernorSnapshot struct {
	Profile           string    `json:"profile"`
	State             string    `json:"state"`
	InFlightRequests  int64     `json:"in_flight_requests"`
	LastActivityAt    time.Time `json:"last_activity_at"`
	PreemptionsCount  int64     `json:"preemptions_count"`
	RerankerAvailable bool      `json:"reranker_available"`
	NLIAvailable      bool      `json:"nli_available"`
	SLMAvailable      bool      `json:"slm_available"`
}

// SystemStatusResponse is returned by GET /api/v1/system/status.
type SystemStatusResponse struct {
	Status              string            `json:"status"`
	Version             string            `json:"version"`
	Database            string            `json:"database"`
	ActiveProfile       string            `json:"active_profile,omitempty"`
	L1GraphCacheEnabled bool              `json:"l1_graph_cache_enabled"`
	L1NodesCount        int               `json:"l1_nodes_count"`
	L1EdgesCount        int               `json:"l1_edges_count"`
	L1EmbeddingsCount   int               `json:"l1_embeddings_count,omitempty"`
	Governor            *GovernorSnapshot `json:"governor,omitempty"`
}
