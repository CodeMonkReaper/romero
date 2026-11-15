package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/romero-project/romero/internal/application/capture"
	contextbuilder "github.com/romero-project/romero/internal/application/context"
	"github.com/romero-project/romero/internal/application/feedback"
	"github.com/romero-project/romero/internal/application/retrieval"
	"github.com/romero-project/romero/internal/domain/identity"
	"github.com/romero-project/romero/internal/domain/memory"
	"github.com/romero-project/romero/internal/domain/provenance"
)

const maxGetBatchIDs = 20

var canonicalTools = []ToolDefinition{
	{
		Name:        "romero_memory_save",
		Description: "Capture a new Zero-Trust memory or enqueue an opaque task_shared write.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"project_id":{"type":"string","format":"uuid"},"task_id":{"type":"string"},"type":{"type":"string","enum":["episodic","semantic","procedural","relational"]},"scope_type":{"type":"string","enum":["agent_private","project_shared","task_shared","org_shared","global_public"]},"topic_key":{"type":"string"},"summary":{"type":"string"},"content":{"type":"string"},"tags":{"type":"array","items":{"type":"string"}},"entities":{"type":"array","items":{"type":"string"}},"epistemic_status":{"type":"string","enum":["verified","observed","reported","inferred","hypothesis"]},"importance":{"type":"number","minimum":0,"maximum":1},"source_type":{"type":"string","enum":["agent_execution","user_input","tool_output","document_ingest","rem_consolidation","human_review"]},"external_source_id":{"type":"string"},"uri":{"type":"string"},"raw_excerpt_hash":{"type":"string"},"idempotency_key":{"type":"string"},"relations":{"type":"array","items":{"type":"object","properties":{"to_memory_id":{"type":"string","format":"uuid"},"relation_type":{"type":"string"},"weight":{"type":"number"},"notes":{"type":"string"}},"required":["to_memory_id","relation_type"]}}},"required":["type","scope_type","summary","content"]}`),
	},
	{
		Name:        "romero_memory_search",
		Description: "Search visible memories using hybrid BM25 + vector + graph traversal returning compact views without raw content.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"},"project_ids":{"type":"array","items":{"type":"string","format":"uuid"}},"scopes":{"type":"array","items":{"type":"string"}},"task_id":{"type":"string"},"memory_types":{"type":"array","items":{"type":"string","enum":["episodic","semantic","procedural","relational"]}},"min_confidence":{"type":"number","minimum":0,"maximum":1},"limit":{"type":"integer","minimum":1},"hops":{"type":"integer","minimum":0},"include_superseded":{"type":"boolean"},"include_consolidated":{"type":"boolean"}},"required":["query"]}`),
	},
	{
		Name:        "romero_memory_get",
		Description: "Retrieve one memory or a batch (1..20) with untrusted XML data-plane wrapping and optional Point-in-Time timestamps.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"memory_id":{"type":"string","format":"uuid"},"memory_ids":{"type":"array","items":{"type":"string","format":"uuid"},"minItems":1,"maxItems":20},"as_of":{"type":"string","format":"date-time"},"valid_at":{"type":"string","format":"date-time"},"known_at":{"type":"string","format":"date-time"}}}`),
	},
	{
		Name:        "romero_memory_context",
		Description: "Build a token-budgeted <romero_context> XML prompt block from visible memories.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"},"project_ids":{"type":"array","items":{"type":"string","format":"uuid"}},"scopes":{"type":"array","items":{"type":"string"}},"task_id":{"type":"string"},"max_tokens":{"type":"integer","minimum":1},"hops":{"type":"integer","minimum":0},"include_superseded":{"type":"boolean"},"include_consolidated":{"type":"boolean"}},"required":["query","max_tokens"]}`),
	},
	{
		Name:        "romero_memory_feedback",
		Description: "Record Bayesian utility/confidence feedback on a memory or enqueue an opaque task_shared feedback event.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"memory_id":{"type":"string","format":"uuid"},"task_id":{"type":"string"},"signal":{"type":"string","enum":["retrieved","used_in_context","helpful_success","ignored","misleading_failure","corrected_by_user","confirmed_verified"]},"comment":{"type":"string"},"idempotency_key":{"type":"string"},"replacement_memory":{"type":"object"}},"required":["memory_id","signal"]}`),
	},
}

type relationArg struct {
	ToMemoryID   uuid.UUID           `json:"to_memory_id"`
	RelationType memory.RelationType `json:"relation_type"`
	Weight       float32             `json:"weight,omitempty"`
	Notes        string              `json:"notes,omitempty"`
}

type saveToolArgs struct {
	ProjectID        *uuid.UUID             `json:"project_id,omitempty"`
	TaskID           string                 `json:"task_id,omitempty"`
	Type             memory.MemoryType      `json:"type"`
	ScopeType        identity.ScopeType     `json:"scope_type"`
	TopicKey         string                 `json:"topic_key,omitempty"`
	Summary          string                 `json:"summary"`
	Content          string                 `json:"content"`
	Tags             []string               `json:"tags,omitempty"`
	Entities         []string               `json:"entities,omitempty"`
	EpistemicStatus  memory.EpistemicStatus `json:"epistemic_status,omitempty"`
	Importance       float32                `json:"importance,omitempty"`
	SourceType       provenance.SourceType  `json:"source_type,omitempty"`
	ExternalSourceID string                 `json:"external_source_id,omitempty"`
	URI              string                 `json:"uri,omitempty"`
	RawExcerptHash   string                 `json:"raw_excerpt_hash,omitempty"`
	IdempotencyKey   string                 `json:"idempotency_key,omitempty"`
	Relations        []relationArg          `json:"relations,omitempty"`
}

func (a saveToolArgs) toCommand() capture.CaptureCommand {
	var rels []memory.ExplicitRelationInput
	if len(a.Relations) > 0 {
		rels = make([]memory.ExplicitRelationInput, 0, len(a.Relations))
		for _, r := range a.Relations {
			rels = append(rels, memory.ExplicitRelationInput{
				ToMemoryID:   r.ToMemoryID,
				RelationType: r.RelationType,
				Weight:       r.Weight,
				Notes:        r.Notes,
			})
		}
	}
	return capture.CaptureCommand{
		ProjectID:        a.ProjectID,
		TaskID:           a.TaskID,
		Type:             a.Type,
		ScopeType:        a.ScopeType,
		TopicKey:         a.TopicKey,
		Summary:          a.Summary,
		Content:          a.Content,
		Tags:             a.Tags,
		Entities:         a.Entities,
		EpistemicStatus:  a.EpistemicStatus,
		Importance:       a.Importance,
		SourceType:       a.SourceType,
		ExternalSourceID: a.ExternalSourceID,
		URI:              a.URI,
		RawExcerptHash:   a.RawExcerptHash,
		IdempotencyKey:   a.IdempotencyKey,
		Relations:        rels,
	}
}

type searchToolArgs struct {
	Query               string               `json:"query"`
	ProjectIDs          []uuid.UUID          `json:"project_ids,omitempty"`
	Scopes              []identity.ScopeType `json:"scopes,omitempty"`
	TaskID              string               `json:"task_id,omitempty"`
	MemoryTypes         []memory.MemoryType  `json:"memory_types,omitempty"`
	MinConfidence       *float32             `json:"min_confidence,omitempty"`
	Limit               int                  `json:"limit,omitempty"`
	Hops                int                  `json:"hops,omitempty"`
	IncludeSuperseded   bool                 `json:"include_superseded,omitempty"`
	IncludeConsolidated bool                 `json:"include_consolidated,omitempty"`
}

type getToolArgs struct {
	MemoryID  string   `json:"memory_id,omitempty"`
	MemoryIDs []string `json:"memory_ids,omitempty"`
	AsOf      string   `json:"as_of,omitempty"`
	ValidAt   string   `json:"valid_at,omitempty"`
	KnownAt   string   `json:"known_at,omitempty"`
}

type getItemResult struct {
	MemoryID             uuid.UUID               `json:"memory_id"`
	Status               string                  `json:"status"`
	Memory               *memory.Memory          `json:"memory,omitempty"`
	Versions             []memory.MemoryVersion  `json:"versions,omitempty"`
	Relations            []memory.MemoryRelation `json:"relations,omitempty"`
	PointInTimeVersion   *memory.MemoryVersion   `json:"point_in_time_version,omitempty"`
	UntrustedXMLEnvelope string                  `json:"untrusted_xml_envelope,omitempty"`
}

type getBatchResult struct {
	Items []getItemResult `json:"items"`
}

type contextToolArgs struct {
	Query               string               `json:"query"`
	ProjectIDs          []uuid.UUID          `json:"project_ids,omitempty"`
	Scopes              []identity.ScopeType `json:"scopes,omitempty"`
	TaskID              string               `json:"task_id,omitempty"`
	MaxTokens           int                  `json:"max_tokens"`
	Hops                int                  `json:"hops,omitempty"`
	IncludeSuperseded   bool                 `json:"include_superseded,omitempty"`
	IncludeConsolidated bool                 `json:"include_consolidated,omitempty"`
}

type feedbackToolArgs struct {
	MemoryID          uuid.UUID             `json:"memory_id"`
	TaskID            string                `json:"task_id,omitempty"`
	Signal            memory.FeedbackSignal `json:"signal"`
	Comment           string                `json:"comment,omitempty"`
	IdempotencyKey    string                `json:"idempotency_key,omitempty"`
	ReplacementMemory *saveToolArgs         `json:"replacement_memory,omitempty"`
}

func (s *Server) callTool(ctx context.Context, auth identity.AuthContext, name string, rawArgs json.RawMessage) CallToolResult {
	if len(rawArgs) == 0 {
		rawArgs = json.RawMessage(`{}`)
	}
	switch name {
	case "romero_memory_save":
		return s.handleSaveTool(ctx, auth, rawArgs)
	case "romero_memory_search":
		return s.handleSearchTool(ctx, auth, rawArgs)
	case "romero_memory_get":
		return s.handleGetTool(ctx, auth, rawArgs)
	case "romero_memory_context":
		return s.handleContextTool(ctx, auth, rawArgs)
	case "romero_memory_feedback":
		return s.handleFeedbackTool(ctx, auth, rawArgs)
	default:
		return toolErrorResult(fmt.Sprintf("unknown tool %q", name))
	}
}

func (s *Server) handleSaveTool(ctx context.Context, auth identity.AuthContext, rawArgs json.RawMessage) CallToolResult {
	if s.deps.Capture == nil {
		return toolErrorResult("capture service is not configured")
	}
	var args saveToolArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return toolErrorResult(fmt.Sprintf("invalid arguments: %v", err))
	}
	res, err := s.deps.Capture.CaptureMemory(ctx, auth, args.toCommand())
	if err != nil {
		return sanitizeToolErrorResult(err)
	}
	type saveOutput struct {
		Accepted  bool                `json:"accepted"`
		Created   bool                `json:"created,omitempty"`
		RequestID *uuid.UUID          `json:"request_id,omitempty"`
		Memory    *capture.MemoryView `json:"memory,omitempty"`
	}
	out := saveOutput{Created: res.Created, Memory: res.Memory}
	if res.Accepted && res.OpaqueReceipt != nil {
		out.Accepted = true
		id := res.OpaqueReceipt.RequestID
		out.RequestID = &id
	}
	return toolJSONResult(out)
}

func (s *Server) handleSearchTool(ctx context.Context, auth identity.AuthContext, rawArgs json.RawMessage) CallToolResult {
	if s.deps.Retrieval == nil {
		return toolErrorResult("retrieval service is not configured")
	}
	var args searchToolArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return toolErrorResult(fmt.Sprintf("invalid arguments: %v", err))
	}
	res, err := s.deps.Retrieval.SearchMemories(ctx, auth, retrieval.SearchQuery{
		Query:               args.Query,
		ProjectIDs:          args.ProjectIDs,
		Scopes:              args.Scopes,
		TaskID:              args.TaskID,
		MemoryTypes:         args.MemoryTypes,
		MinConfidence:       args.MinConfidence,
		Limit:               args.Limit,
		Hops:                args.Hops,
		IncludeSuperseded:   args.IncludeSuperseded,
		IncludeConsolidated: args.IncludeConsolidated,
	})
	if err != nil {
		return sanitizeToolErrorResult(err)
	}
	return toolJSONResult(res)
}

func (s *Server) handleGetTool(ctx context.Context, auth identity.AuthContext, rawArgs json.RawMessage) CallToolResult {
	if s.deps.Retrieval == nil {
		return toolErrorResult("retrieval service is not configured")
	}
	var args getToolArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return toolErrorResult(fmt.Sprintf("invalid arguments: %v", err))
	}
	ids, baseQuery, err := parseGetArgs(args)
	if err != nil {
		return toolErrorResult(err.Error())
	}

	items := make([]getItemResult, 0, len(ids))
	for _, id := range ids {
		q := baseQuery
		q.MemoryID = id
		detail, err := s.deps.Retrieval.GetMemory(ctx, auth, q)
		if err != nil {
			if errors.Is(err, retrieval.ErrNotFound) || errors.Is(err, memory.ErrNotFound) || errors.Is(err, identity.ErrForbidden) {
				items = append(items, getItemResult{
					MemoryID: id,
					Status:   "not_found",
				})
				continue
			}
			return sanitizeToolErrorResult(err)
		}
		memCopy := detail.Memory
		items = append(items, getItemResult{
			MemoryID:             id,
			Status:               "found",
			Memory:               &memCopy,
			Versions:             detail.Versions,
			Relations:            detail.Relations,
			PointInTimeVersion:   detail.PointInTimeVersion,
			UntrustedXMLEnvelope: detail.UntrustedXMLEnvelope,
		})
	}
	return toolJSONResult(getBatchResult{Items: items})
}

func parseGetArgs(args getToolArgs) ([]uuid.UUID, retrieval.GetMemoryQuery, error) {
	var rawIDs []string
	if id := strings.TrimSpace(args.MemoryID); id != "" {
		rawIDs = append(rawIDs, id)
	}
	rawIDs = append(rawIDs, args.MemoryIDs...)
	if len(rawIDs) == 0 {
		return nil, retrieval.GetMemoryQuery{}, errors.New("memory_id or memory_ids is required")
	}

	seen := make(map[uuid.UUID]struct{}, len(rawIDs))
	ids := make([]uuid.UUID, 0, len(rawIDs))
	for _, raw := range rawIDs {
		parsed, err := uuid.Parse(strings.TrimSpace(raw))
		if err != nil {
			return nil, retrieval.GetMemoryQuery{}, fmt.Errorf("invalid memory_id %q", raw)
		}
		if _, exists := seen[parsed]; exists {
			continue
		}
		seen[parsed] = struct{}{}
		ids = append(ids, parsed)
	}
	if len(ids) > maxGetBatchIDs {
		return nil, retrieval.GetMemoryQuery{}, fmt.Errorf("memory_ids exceeds maximum batch size of %d", maxGetBatchIDs)
	}

	var q retrieval.GetMemoryQuery
	if strings.TrimSpace(args.AsOf) != "" {
		t, err := time.Parse(time.RFC3339, strings.TrimSpace(args.AsOf))
		if err != nil {
			return nil, retrieval.GetMemoryQuery{}, errors.New("invalid as_of timestamp")
		}
		q.AsOf = &t
	}
	if strings.TrimSpace(args.ValidAt) != "" {
		t, err := time.Parse(time.RFC3339, strings.TrimSpace(args.ValidAt))
		if err != nil {
			return nil, retrieval.GetMemoryQuery{}, errors.New("invalid valid_at timestamp")
		}
		q.ValidAt = t
	}
	if strings.TrimSpace(args.KnownAt) != "" {
		t, err := time.Parse(time.RFC3339, strings.TrimSpace(args.KnownAt))
		if err != nil {
			return nil, retrieval.GetMemoryQuery{}, errors.New("invalid known_at timestamp")
		}
		q.KnownAt = t
	}
	return ids, q, nil
}

func (s *Server) handleContextTool(ctx context.Context, auth identity.AuthContext, rawArgs json.RawMessage) CallToolResult {
	if s.deps.Context == nil {
		return toolErrorResult("context service is not configured")
	}
	var args contextToolArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return toolErrorResult(fmt.Sprintf("invalid arguments: %v", err))
	}
	res, err := s.deps.Context.BuildContext(ctx, auth, contextbuilder.BuildContextRequest{
		Query:               args.Query,
		ProjectIDs:          args.ProjectIDs,
		Scopes:              args.Scopes,
		TaskID:              args.TaskID,
		MaxTokens:           args.MaxTokens,
		Hops:                args.Hops,
		IncludeSuperseded:   args.IncludeSuperseded,
		IncludeConsolidated: args.IncludeConsolidated,
	})
	if err != nil {
		return sanitizeToolErrorResult(err)
	}
	return toolJSONResult(res)
}

func (s *Server) handleFeedbackTool(ctx context.Context, auth identity.AuthContext, rawArgs json.RawMessage) CallToolResult {
	if s.deps.Feedback == nil {
		return toolErrorResult("feedback service is not configured")
	}
	var args feedbackToolArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return toolErrorResult(fmt.Sprintf("invalid arguments: %v", err))
	}
	cmd := feedback.FeedbackCommand{
		MemoryID:       args.MemoryID,
		TaskID:         args.TaskID,
		Signal:         args.Signal,
		Comment:        args.Comment,
		IdempotencyKey: args.IdempotencyKey,
	}
	if args.ReplacementMemory != nil {
		rep := args.ReplacementMemory.toCommand()
		cmd.ReplacementMemory = &rep
	}
	res, err := s.deps.Feedback.RecordFeedback(ctx, auth, cmd)
	if err != nil {
		return sanitizeToolErrorResult(err)
	}

	type feedbackOutput struct {
		Accepted            bool                   `json:"accepted"`
		RequestID           *uuid.UUID             `json:"request_id,omitempty"`
		MemoryID            uuid.UUID              `json:"memory_id,omitempty"`
		NewAlpha            float32                `json:"new_alpha,omitempty"`
		NewBeta             float32                `json:"new_beta,omitempty"`
		NewConfidence       float32                `json:"new_confidence,omitempty"`
		NewUtility          float32                `json:"new_utility,omitempty"`
		NewEpistemic        memory.EpistemicStatus `json:"new_epistemic,omitempty"`
		NewStatus           memory.MemoryStatus    `json:"new_status,omitempty"`
		Quarantined         bool                   `json:"quarantined,omitempty"`
		ReplacementMemoryID *uuid.UUID             `json:"replacement_memory_id,omitempty"`
	}
	if res.Accepted && res.OpaqueReceipt != nil {
		id := res.OpaqueReceipt.RequestID
		return toolJSONResult(feedbackOutput{
			Accepted:  true,
			RequestID: &id,
		})
	}
	return toolJSONResult(feedbackOutput{
		Accepted:            false,
		MemoryID:            res.MemoryID,
		NewAlpha:            res.NewAlpha,
		NewBeta:             res.NewBeta,
		NewConfidence:       res.NewConfidence,
		NewUtility:          res.NewUtility,
		NewEpistemic:        res.NewEpistemic,
		NewStatus:           res.NewStatus,
		Quarantined:         res.Quarantined,
		ReplacementMemoryID: res.ReplacementMemoryID,
	})
}

func toolJSONResult(v any) CallToolResult {
	b, err := json.Marshal(v)
	if err != nil {
		return toolErrorResult("failed to serialize tool result")
	}
	return CallToolResult{
		Content:           []TextContent{{Type: "text", Text: string(b)}},
		StructuredContent: v,
	}
}

func toolErrorResult(msg string) CallToolResult {
	return toolCodedErrorResult("validation_error", msg)
}

func toolCodedErrorResult(code, msg string) CallToolResult {
	payload := struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}{}
	payload.Error.Code = code
	payload.Error.Message = msg
	b, _ := json.Marshal(payload)
	return CallToolResult{
		Content:           []TextContent{{Type: "text", Text: string(b)}},
		StructuredContent: payload,
		IsError:           true,
	}
}

func sanitizeToolErrorResult(err error) CallToolResult {
	switch {
	case errors.Is(err, capture.ErrValidation),
		errors.Is(err, feedback.ErrValidation),
		errors.Is(err, retrieval.ErrValidation):
		return toolCodedErrorResult("validation_error", err.Error())
	case errors.Is(err, capture.ErrNotFound),
		errors.Is(err, feedback.ErrNotFound),
		errors.Is(err, retrieval.ErrNotFound),
		errors.Is(err, memory.ErrNotFound):
		return toolCodedErrorResult("not_found", "resource not found")
	case errors.Is(err, capture.ErrForbidden),
		errors.Is(err, feedback.ErrForbidden),
		errors.Is(err, identity.ErrForbidden):
		return toolCodedErrorResult("forbidden", "forbidden")
	case errors.Is(err, capture.ErrUnauthorized),
		errors.Is(err, feedback.ErrUnauthorized),
		errors.Is(err, retrieval.ErrUnauthorized):
		return toolCodedErrorResult("unauthorized", "unauthorized")
	case errors.Is(err, capture.ErrConflict),
		errors.Is(err, feedback.ErrConflict):
		return toolCodedErrorResult("conflict", "conflict")
	case errors.Is(err, capture.ErrOperational),
		errors.Is(err, feedback.ErrOperational),
		errors.Is(err, retrieval.ErrOperational):
		return toolCodedErrorResult("service_unavailable", "service temporarily unavailable")
	default:
		return toolCodedErrorResult("internal_error", "internal error")
	}
}
