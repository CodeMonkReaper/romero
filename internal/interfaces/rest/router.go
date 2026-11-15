package rest

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/google/uuid"

	"github.com/romero-project/romero/internal/application/capture"
	contextbuilder "github.com/romero-project/romero/internal/application/context"
	"github.com/romero-project/romero/internal/application/feedback"
	"github.com/romero-project/romero/internal/application/retrieval"
	"github.com/romero-project/romero/internal/domain/governor"
	"github.com/romero-project/romero/internal/domain/identity"
	"github.com/romero-project/romero/internal/domain/memory"
	"github.com/romero-project/romero/internal/domain/provenance"
	"github.com/romero-project/romero/internal/infrastructure/cachel1"
)

// CaptureService defines capture operations exposed by the REST adapter.
type CaptureService interface {
	CaptureMemory(ctx context.Context, auth identity.AuthContext, cmd capture.CaptureCommand) (*capture.CaptureResult, error)
	UpdateMemory(ctx context.Context, auth identity.AuthContext, memoryID uuid.UUID, cmd capture.UpdateCommand) (*capture.MutationResult, error)
	SupersedeMemory(ctx context.Context, auth identity.AuthContext, oldID uuid.UUID, cmd capture.ExplicitSupersedeCommand) (*capture.SupersedeResult, error)
	ArchiveMemory(ctx context.Context, auth identity.AuthContext, memoryID uuid.UUID, cmd capture.ArchiveCommand) (*capture.MutationResult, error)
}

// RetrievalService defines read, hybrid search, subgraph, and opaque request status operations exposed by the REST adapter.
type RetrievalService interface {
	GetMemory(ctx context.Context, auth identity.AuthContext, query retrieval.GetMemoryQuery) (*retrieval.MemoryDetail, error)
	GetRequestStatus(ctx context.Context, auth identity.AuthContext, requestID uuid.UUID, correlationID string) (*retrieval.RequestStatus, error)
	SearchMemories(ctx context.Context, auth identity.AuthContext, query retrieval.SearchQuery) (*retrieval.SearchResult, error)
	GetSubgraph(ctx context.Context, auth identity.AuthContext, query retrieval.SubgraphQuery) (*retrieval.SubgraphResult, error)
}

// ContextService defines bounded submodular context packaging exposed by the REST adapter.
type ContextService interface {
	BuildContext(ctx context.Context, auth identity.AuthContext, req contextbuilder.BuildContextRequest) (*contextbuilder.BuildContextResult, error)
}

// L1StatsProvider exposes in-RAM Cognitive Graph metrics for system/status.
type L1StatsProvider interface {
	Stats() cachel1.GraphStats
}

// FeedbackService defines empirical feedback operations exposed by the REST adapter.
type FeedbackService interface {
	RecordFeedback(ctx context.Context, auth identity.AuthContext, cmd feedback.FeedbackCommand) (*feedback.FeedbackResult, error)
}

// StatusChecker probes backend readiness for GET /api/v1/system/status.
type StatusChecker interface {
	Ping(ctx context.Context) error
}

// RouterDeps groups dependencies required to build the ROMERO REST HTTP handler.
type RouterDeps struct {
	Auth          Authenticator
	Capture       CaptureService
	Retrieval     RetrievalService
	Context       ContextService
	Feedback      FeedbackService
	Conflicts     ConflictService
	Consolidation ConsolidationService
	Governor      GovernorProvider
	DB            StatusChecker
	L1Stats       L1StatsProvider
	Version       string
}

// NewRouter constructs the Huma v2 OpenAPI 3.1 router and wraps /api/v1/* with AuthMiddleware.
func NewRouter(deps RouterDeps) (http.Handler, huma.API, error) {
	if deps.Auth == nil || deps.Capture == nil || deps.Retrieval == nil || deps.Feedback == nil {
		return nil, nil, errors.New("auth, capture, retrieval, and feedback services are required")
	}
	mux := http.NewServeMux()
	cfg := huma.DefaultConfig("ROMERO API", "0.1.0")
	api := humago.New(mux, cfg)

	registerRoutes(api, deps)
	handler := governorPreemptionMiddleware(deps.Governor, mux)
	return AuthMiddleware(deps.Auth, handler), api, nil
}

func callerFromCtx(ctx context.Context) (identity.AuthContext, error) {
	auth, ok := AuthFromContext(ctx)
	if !ok {
		return identity.AuthContext{}, huma.Error401Unauthorized("unauthorized")
	}
	return auth, nil
}

type captureMemoryInput struct {
	Body captureBody
}

type explicitRelationBody struct {
	ToMemoryID   uuid.UUID           `json:"to_memory_id"`
	RelationType memory.RelationType `json:"relation_type"`
	Weight       float32             `json:"weight,omitempty"`
	Notes        string              `json:"notes,omitempty"`
}

type captureBody struct {
	ProjectID        *uuid.UUID             `json:"project_id,omitempty"`
	TaskID           string                 `json:"task_id,omitempty"`
	Type             memory.MemoryType      `json:"type"`
	ScopeType        identity.ScopeType     `json:"scope_type"`
	TopicKey         string                 `json:"topic_key,omitempty"`
	Summary          string                 `json:"summary"`
	Content          string                 `json:"content"`
	Tags             []string               `json:"tags,omitempty"`
	Entities         []string               `json:"entities,omitempty"`
	EpistemicStatus  memory.EpistemicStatus `json:"epistemic_status"`
	Importance       float32                `json:"importance"`
	SourceType       provenance.SourceType  `json:"source_type"`
	ExternalSourceID string                 `json:"external_source_id"`
	URI              string                 `json:"uri,omitempty"`
	RawExcerptHash   string                 `json:"raw_excerpt_hash,omitempty"`
	IdempotencyKey   string                 `json:"idempotency_key,omitempty"`
	Relations        []explicitRelationBody `json:"relations,omitempty"`
}

func (b captureBody) toCommand(corrID string) capture.CaptureCommand {
	var rels []memory.ExplicitRelationInput
	if len(b.Relations) > 0 {
		rels = make([]memory.ExplicitRelationInput, 0, len(b.Relations))
		for _, r := range b.Relations {
			rels = append(rels, memory.ExplicitRelationInput{
				ToMemoryID:   r.ToMemoryID,
				RelationType: r.RelationType,
				Weight:       r.Weight,
				Notes:        r.Notes,
			})
		}
	}
	return capture.CaptureCommand{
		ProjectID:        b.ProjectID,
		TaskID:           b.TaskID,
		Type:             b.Type,
		ScopeType:        b.ScopeType,
		TopicKey:         b.TopicKey,
		Summary:          b.Summary,
		Content:          b.Content,
		Tags:             b.Tags,
		Entities:         b.Entities,
		EpistemicStatus:  b.EpistemicStatus,
		Importance:       b.Importance,
		SourceType:       b.SourceType,
		ExternalSourceID: b.ExternalSourceID,
		URI:              b.URI,
		RawExcerptHash:   b.RawExcerptHash,
		IdempotencyKey:   b.IdempotencyKey,
		Relations:        rels,
		CorrelationID:    corrID,
	}
}

type captureMemoryOutput struct {
	Status int
	Body   struct {
		RequestID *uuid.UUID          `json:"request_id,omitempty"`
		Memory    *capture.MemoryView `json:"memory,omitempty"`
	}
}

type getMemoryInput struct {
	ID      string `path:"id"`
	AsOf    string `query:"as_of"`
	ValidAt string `query:"valid_at"`
	KnownAt string `query:"known_at"`
}

type getMemoryOutput struct {
	Body struct {
		Memory               memory.Memory           `json:"memory"`
		Versions             []memory.MemoryVersion  `json:"versions,omitempty"`
		Relations            []memory.MemoryRelation `json:"relations,omitempty"`
		PointInTimeVersion   *memory.MemoryVersion   `json:"point_in_time_version,omitempty"`
		UntrustedXMLEnvelope string                  `json:"untrusted_xml_envelope"`
	}
}

type updateMemoryInput struct {
	ID   string `path:"id"`
	Body struct {
		TaskID         string   `json:"task_id,omitempty"`
		Summary        string   `json:"summary"`
		Content        string   `json:"content"`
		Tags           []string `json:"tags,omitempty"`
		Entities       []string `json:"entities,omitempty"`
		Importance     *float32 `json:"importance,omitempty"`
		ChangeReason   string   `json:"change_reason,omitempty"`
		IdempotencyKey string   `json:"idempotency_key,omitempty"`
	}
}

type supersedeMemoryInput struct {
	ID   string `path:"id"`
	Body struct {
		TaskID       string      `json:"task_id,omitempty"`
		ChangeReason string      `json:"change_reason,omitempty"`
		Capture      captureBody `json:"capture"`
	}
}

type supersedeMemoryOutput struct {
	Status int
	Body   struct {
		RequestID *uuid.UUID          `json:"request_id,omitempty"`
		OldMemory *capture.MemoryView `json:"old_memory,omitempty"`
		NewMemory *capture.MemoryView `json:"new_memory,omitempty"`
	}
}

type archiveMemoryInput struct {
	ID   string `path:"id"`
	Body struct {
		TaskID         string `json:"task_id,omitempty"`
		Reason         string `json:"reason,omitempty"`
		IdempotencyKey string `json:"idempotency_key,omitempty"`
	}
}

type mutationOutput struct {
	Status int
	Body   struct {
		RequestID *uuid.UUID          `json:"request_id,omitempty"`
		Memory    *capture.MemoryView `json:"memory,omitempty"`
	}
}

type feedbackInput struct {
	Body struct {
		MemoryID          uuid.UUID             `json:"memory_id"`
		TaskID            string                `json:"task_id,omitempty"`
		Signal            memory.FeedbackSignal `json:"signal"`
		Comment           string                `json:"comment,omitempty"`
		IdempotencyKey    string                `json:"idempotency_key,omitempty"`
		ReplacementMemory *captureBody          `json:"replacement_memory,omitempty"`
	}
}

type feedbackOutput struct {
	Status int
	Body   struct {
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
}

type getRequestStatusInput struct {
	RequestID string `path:"request_id"`
}

type getRequestStatusOutput struct {
	Body struct {
		RequestID uuid.UUID `json:"request_id"`
		Status    string    `json:"status"`
	}
}

type searchMemoriesInput struct {
	Body struct {
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
}

type searchMemoriesOutput struct {
	Body retrieval.SearchResult
}

type buildContextInput struct {
	Body struct {
		Query               string               `json:"query"`
		ProjectIDs          []uuid.UUID          `json:"project_ids,omitempty"`
		Scopes              []identity.ScopeType `json:"scopes,omitempty"`
		TaskID              string               `json:"task_id,omitempty"`
		MaxTokens           int                  `json:"max_tokens"`
		Hops                int                  `json:"hops,omitempty"`
		IncludeSuperseded   bool                 `json:"include_superseded,omitempty"`
		IncludeConsolidated bool                 `json:"include_consolidated,omitempty"`
	}
}

type buildContextOutput struct {
	Body contextbuilder.BuildContextResult
}

type getSubgraphInput struct {
	ID   string `path:"id"`
	Hops int    `query:"hops"`
}

type getSubgraphOutput struct {
	Body retrieval.SubgraphResult
}

type systemStatusOutput struct {
	Status int
	Body   struct {
		Status              string             `json:"status"`
		Version             string             `json:"version"`
		Database            string             `json:"database"`
		ActiveProfile       string             `json:"active_profile,omitempty"`
		L1GraphCacheEnabled bool               `json:"l1_graph_cache_enabled"`
		L1NodesCount        int                `json:"l1_nodes_count"`
		L1EdgesCount        int                `json:"l1_edges_count"`
		L1EmbeddingsCount   int                `json:"l1_embeddings_count,omitempty"`
		Governor            *governor.Snapshot `json:"governor,omitempty"`
	}
}

func registerRoutes(api huma.API, deps RouterDeps) {
	huma.Register(api, huma.Operation{
		OperationID: "capture-memory",
		Method:      http.MethodPost,
		Path:        "/api/v1/memories",
		Summary:     "Capture a new memory or enqueue an opaque task_shared write",
	}, func(ctx context.Context, in *captureMemoryInput) (*captureMemoryOutput, error) {
		auth, err := callerFromCtx(ctx)
		if err != nil {
			return nil, err
		}
		res, err := deps.Capture.CaptureMemory(ctx, auth, in.Body.toCommand(""))
		if err != nil {
			return nil, mapAppError(err)
		}
		out := &captureMemoryOutput{Status: http.StatusOK}
		if res.Accepted && res.OpaqueReceipt != nil {
			out.Status = http.StatusAccepted
			id := res.OpaqueReceipt.RequestID
			out.Body.RequestID = &id
			return out, nil
		}
		if res.Created {
			out.Status = http.StatusCreated
		}
		out.Body.Memory = res.Memory
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-memory",
		Method:      http.MethodGet,
		Path:        "/api/v1/memories/{id}",
		Summary:     "Retrieve a memory in current or Point-in-Time mode",
	}, func(ctx context.Context, in *getMemoryInput) (*getMemoryOutput, error) {
		auth, err := callerFromCtx(ctx)
		if err != nil {
			return nil, err
		}
		id, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, huma.Error422UnprocessableEntity("invalid memory id")
		}
		q := retrieval.GetMemoryQuery{MemoryID: id}
		if in.AsOf != "" {
			t, pErr := time.Parse(time.RFC3339, in.AsOf)
			if pErr != nil {
				return nil, huma.Error422UnprocessableEntity("invalid as_of timestamp")
			}
			q.AsOf = &t
		}
		if in.ValidAt != "" {
			t, pErr := time.Parse(time.RFC3339, in.ValidAt)
			if pErr != nil {
				return nil, huma.Error422UnprocessableEntity("invalid valid_at timestamp")
			}
			q.ValidAt = t
		}
		if in.KnownAt != "" {
			t, pErr := time.Parse(time.RFC3339, in.KnownAt)
			if pErr != nil {
				return nil, huma.Error422UnprocessableEntity("invalid known_at timestamp")
			}
			q.KnownAt = t
		}
		detail, err := deps.Retrieval.GetMemory(ctx, auth, q)
		if err != nil {
			return nil, mapAppError(err)
		}
		out := &getMemoryOutput{}
		out.Body.Memory = detail.Memory
		out.Body.Versions = detail.Versions
		out.Body.Relations = detail.Relations
		out.Body.PointInTimeVersion = detail.PointInTimeVersion
		out.Body.UntrustedXMLEnvelope = detail.UntrustedXMLEnvelope
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "update-memory",
		Method:      http.MethodPatch,
		Path:        "/api/v1/memories/{id}",
		Summary:     "Create a new immutable version of an active memory",
	}, func(ctx context.Context, in *updateMemoryInput) (*mutationOutput, error) {
		auth, err := callerFromCtx(ctx)
		if err != nil {
			return nil, err
		}
		id, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, huma.Error422UnprocessableEntity("invalid memory id")
		}
		res, err := deps.Capture.UpdateMemory(ctx, auth, id, capture.UpdateCommand{
			TaskID:         in.Body.TaskID,
			Summary:        in.Body.Summary,
			Content:        in.Body.Content,
			Tags:           in.Body.Tags,
			Entities:       in.Body.Entities,
			Importance:     in.Body.Importance,
			ChangeReason:   in.Body.ChangeReason,
			IdempotencyKey: in.Body.IdempotencyKey,
		})
		if err != nil {
			return nil, mapAppError(err)
		}
		return toMutationOutput(res), nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "supersede-memory",
		Method:      http.MethodPost,
		Path:        "/api/v1/memories/{id}/supersede",
		Summary:     "Explicitly supersede an open memory",
	}, func(ctx context.Context, in *supersedeMemoryInput) (*supersedeMemoryOutput, error) {
		auth, err := callerFromCtx(ctx)
		if err != nil {
			return nil, err
		}
		id, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, huma.Error422UnprocessableEntity("invalid memory id")
		}
		res, err := deps.Capture.SupersedeMemory(ctx, auth, id, capture.ExplicitSupersedeCommand{
			TaskID:       in.Body.TaskID,
			ChangeReason: in.Body.ChangeReason,
			Capture:      in.Body.Capture.toCommand(""),
		})
		if err != nil {
			return nil, mapAppError(err)
		}
		out := &supersedeMemoryOutput{Status: http.StatusOK}
		if res.Accepted && res.OpaqueReceipt != nil {
			out.Status = http.StatusAccepted
			rid := res.OpaqueReceipt.RequestID
			out.Body.RequestID = &rid
			return out, nil
		}
		out.Body.OldMemory = res.OldMemory
		out.Body.NewMemory = res.NewMemory
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "archive-memory",
		Method:      http.MethodPost,
		Path:        "/api/v1/memories/{id}/archive",
		Summary:     "Archive an open memory",
	}, func(ctx context.Context, in *archiveMemoryInput) (*mutationOutput, error) {
		auth, err := callerFromCtx(ctx)
		if err != nil {
			return nil, err
		}
		id, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, huma.Error422UnprocessableEntity("invalid memory id")
		}
		res, err := deps.Capture.ArchiveMemory(ctx, auth, id, capture.ArchiveCommand{
			TaskID:         in.Body.TaskID,
			Reason:         in.Body.Reason,
			IdempotencyKey: in.Body.IdempotencyKey,
		})
		if err != nil {
			return nil, mapAppError(err)
		}
		return toMutationOutput(res), nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "record-feedback",
		Method:      http.MethodPost,
		Path:        "/api/v1/memories/feedback",
		Summary:     "Record empirical feedback against a memory",
	}, func(ctx context.Context, in *feedbackInput) (*feedbackOutput, error) {
		auth, err := callerFromCtx(ctx)
		if err != nil {
			return nil, err
		}
		cmd := feedback.FeedbackCommand{
			MemoryID:       in.Body.MemoryID,
			TaskID:         in.Body.TaskID,
			Signal:         in.Body.Signal,
			Comment:        in.Body.Comment,
			IdempotencyKey: in.Body.IdempotencyKey,
		}
		if in.Body.ReplacementMemory != nil {
			c := in.Body.ReplacementMemory.toCommand("")
			cmd.ReplacementMemory = &c
		}
		res, err := deps.Feedback.RecordFeedback(ctx, auth, cmd)
		if err != nil {
			return nil, mapAppError(err)
		}
		out := &feedbackOutput{Status: http.StatusOK}
		if res.Accepted && res.OpaqueReceipt != nil {
			out.Status = http.StatusAccepted
			rid := res.OpaqueReceipt.RequestID
			out.Body.RequestID = &rid
			return out, nil
		}
		out.Body.MemoryID = res.MemoryID
		out.Body.NewAlpha = res.NewAlpha
		out.Body.NewBeta = res.NewBeta
		out.Body.NewConfidence = res.NewConfidence
		out.Body.NewUtility = res.NewUtility
		out.Body.NewEpistemic = res.NewEpistemic
		out.Body.NewStatus = res.NewStatus
		out.Body.Quarantined = res.Quarantined
		out.Body.ReplacementMemoryID = res.ReplacementMemoryID
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-memory-request-status",
		Method:      http.MethodGet,
		Path:        "/api/v1/memory-requests/{request_id}",
		Summary:     "Check status of an opaque task_shared memory request",
	}, func(ctx context.Context, in *getRequestStatusInput) (*getRequestStatusOutput, error) {
		auth, err := callerFromCtx(ctx)
		if err != nil {
			return nil, err
		}
		id, err := uuid.Parse(in.RequestID)
		if err != nil {
			return nil, huma.Error422UnprocessableEntity("invalid request_id")
		}
		st, err := deps.Retrieval.GetRequestStatus(ctx, auth, id, "")
		if err != nil {
			return nil, mapAppError(err)
		}
		out := &getRequestStatusOutput{}
		out.Body.RequestID = st.RequestID
		out.Body.Status = st.Status
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "search-memories",
		Method:      http.MethodPost,
		Path:        "/api/v1/memories/search",
		Summary:     "Execute Zero-Trust tri-modal hybrid search with spreading activation",
	}, func(ctx context.Context, in *searchMemoriesInput) (*searchMemoriesOutput, error) {
		auth, err := callerFromCtx(ctx)
		if err != nil {
			return nil, err
		}
		res, err := deps.Retrieval.SearchMemories(ctx, auth, retrieval.SearchQuery{
			Query:               in.Body.Query,
			ProjectIDs:          in.Body.ProjectIDs,
			Scopes:              in.Body.Scopes,
			TaskID:              in.Body.TaskID,
			MemoryTypes:         in.Body.MemoryTypes,
			MinConfidence:       in.Body.MinConfidence,
			Limit:               in.Body.Limit,
			Hops:                in.Body.Hops,
			IncludeSuperseded:   in.Body.IncludeSuperseded,
			IncludeConsolidated: in.Body.IncludeConsolidated,
		})
		if err != nil {
			return nil, mapAppError(err)
		}
		return &searchMemoriesOutput{Body: *res}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "build-context",
		Method:      http.MethodPost,
		Path:        "/api/v1/memories/context",
		Summary:     "Build a token-bounded submodular XML context package",
	}, func(ctx context.Context, in *buildContextInput) (*buildContextOutput, error) {
		auth, err := callerFromCtx(ctx)
		if err != nil {
			return nil, err
		}
		if deps.Context == nil {
			return nil, huma.Error503ServiceUnavailable("context service unavailable")
		}
		res, err := deps.Context.BuildContext(ctx, auth, contextbuilder.BuildContextRequest{
			Query:               in.Body.Query,
			ProjectIDs:          in.Body.ProjectIDs,
			Scopes:              in.Body.Scopes,
			TaskID:              in.Body.TaskID,
			MaxTokens:           in.Body.MaxTokens,
			Hops:                in.Body.Hops,
			IncludeSuperseded:   in.Body.IncludeSuperseded,
			IncludeConsolidated: in.Body.IncludeConsolidated,
		})
		if err != nil {
			return nil, mapAppError(err)
		}
		return &buildContextOutput{Body: *res}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-memory-subgraph",
		Method:      http.MethodGet,
		Path:        "/api/v1/memories/{id}/subgraph",
		Summary:     "Explore authorized 1-3 hop causal neighborhood around a memory",
	}, func(ctx context.Context, in *getSubgraphInput) (*getSubgraphOutput, error) {
		auth, err := callerFromCtx(ctx)
		if err != nil {
			return nil, err
		}
		id, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, huma.Error422UnprocessableEntity("invalid memory id")
		}
		res, err := deps.Retrieval.GetSubgraph(ctx, auth, retrieval.SubgraphQuery{
			MemoryID: id,
			Hops:     in.Hops,
		})
		if err != nil {
			return nil, mapAppError(err)
		}
		return &getSubgraphOutput{Body: *res}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-system-status",
		Method:      http.MethodGet,
		Path:        "/api/v1/system/status",
		Summary:     "Check system version, database connectivity, and L1 Cognitive Graph metrics",
	}, func(ctx context.Context, _ *struct{}) (*systemStatusOutput, error) {
		out := &systemStatusOutput{Status: http.StatusOK}
		out.Body.Version = deps.Version
		out.Body.Status = "ok"
		out.Body.Database = "connected"
		out.Body.ActiveProfile = "balanced"
		if deps.DB != nil {
			if err := deps.DB.Ping(ctx); err != nil {
				return nil, huma.Error503ServiceUnavailable("database unavailable")
			}
		}
		if deps.L1Stats != nil {
			st := deps.L1Stats.Stats()
			out.Body.L1GraphCacheEnabled = st.Enabled
			out.Body.L1NodesCount = st.NodesCount
			out.Body.L1EdgesCount = st.EdgesCount
			out.Body.L1EmbeddingsCount = st.EmbeddingsCount
		}
		if deps.Governor != nil {
			snap := deps.Governor.Status()
			out.Body.ActiveProfile = string(snap.Profile)
			out.Body.Governor = &snap
		}
		return out, nil
	})

	registerPhase03Routes(api, deps)
}

func toMutationOutput(res *capture.MutationResult) *mutationOutput {
	out := &mutationOutput{Status: http.StatusOK}
	if res.Accepted && res.OpaqueReceipt != nil {
		out.Status = http.StatusAccepted
		rid := res.OpaqueReceipt.RequestID
		out.Body.RequestID = &rid
		return out
	}
	out.Body.Memory = res.Memory
	return out
}
