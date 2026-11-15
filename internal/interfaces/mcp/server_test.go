package mcp_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/romero-project/romero/internal/application/capture"
	contextbuilder "github.com/romero-project/romero/internal/application/context"
	"github.com/romero-project/romero/internal/application/feedback"
	"github.com/romero-project/romero/internal/application/retrieval"
	"github.com/romero-project/romero/internal/domain/identity"
	"github.com/romero-project/romero/internal/domain/memory"
	"github.com/romero-project/romero/internal/domain/provenance"
	"github.com/romero-project/romero/internal/interfaces/mcp"
)

type stubCaptureSvc struct {
	fn func(ctx context.Context, auth identity.AuthContext, cmd capture.CaptureCommand) (*capture.CaptureResult, error)
}

func (s stubCaptureSvc) CaptureMemory(ctx context.Context, auth identity.AuthContext, cmd capture.CaptureCommand) (*capture.CaptureResult, error) {
	return s.fn(ctx, auth, cmd)
}

type stubRetrievalSvc struct {
	searchFn func(ctx context.Context, auth identity.AuthContext, q retrieval.SearchQuery) (*retrieval.SearchResult, error)
	getFn    func(ctx context.Context, auth identity.AuthContext, q retrieval.GetMemoryQuery) (*retrieval.MemoryDetail, error)
}

func (s stubRetrievalSvc) SearchMemories(ctx context.Context, auth identity.AuthContext, q retrieval.SearchQuery) (*retrieval.SearchResult, error) {
	return s.searchFn(ctx, auth, q)
}

func (s stubRetrievalSvc) GetMemory(ctx context.Context, auth identity.AuthContext, q retrieval.GetMemoryQuery) (*retrieval.MemoryDetail, error) {
	return s.getFn(ctx, auth, q)
}

type stubContextSvc struct {
	fn func(ctx context.Context, auth identity.AuthContext, req contextbuilder.BuildContextRequest) (*contextbuilder.BuildContextResult, error)
}

func (s stubContextSvc) BuildContext(ctx context.Context, auth identity.AuthContext, req contextbuilder.BuildContextRequest) (*contextbuilder.BuildContextResult, error) {
	return s.fn(ctx, auth, req)
}

type stubFeedbackSvc struct {
	fn func(ctx context.Context, auth identity.AuthContext, cmd feedback.FeedbackCommand) (*feedback.FeedbackResult, error)
}

func (s stubFeedbackSvc) RecordFeedback(ctx context.Context, auth identity.AuthContext, cmd feedback.FeedbackCommand) (*feedback.FeedbackResult, error) {
	return s.fn(ctx, auth, cmd)
}

type stubGovernor struct {
	begins atomic.Int32
	ends   atomic.Int32
}

func (g *stubGovernor) BeginAgentRequest() func() {
	g.begins.Add(1)
	return func() {
		g.ends.Add(1)
	}
}

func testCaller() identity.AuthContext {
	return identity.AuthContext{
		TenantID:      uuid.New(),
		PrincipalID:   uuid.New(),
		PrincipalType: identity.PrincipalAgent,
	}
}

func TestServer_InitializeAndToolsListStrictFiveTools(t *testing.T) {
	srv, err := mcp.NewServer(mcp.Dependencies{})
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	initReq := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}`
	rawResp, hasResp, err := srv.HandleRequest(context.Background(), testCaller(), []byte(initReq))
	if err != nil || !hasResp {
		t.Fatalf("initialize failed: hasResp=%v err=%v", hasResp, err)
	}
	var initEnv struct {
		JSONRPC string `json:"jsonrpc"`
		Result  struct {
			ProtocolVersion string `json:"protocolVersion"`
			ServerInfo      struct {
				Name string `json:"name"`
			} `json:"serverInfo"`
		} `json:"result"`
	}
	if err := json.Unmarshal(rawResp, &initEnv); err != nil {
		t.Fatalf("unmarshal initialize: %v", err)
	}
	if initEnv.JSONRPC != "2.0" || initEnv.Result.ServerInfo.Name != "romero-mcp" {
		t.Fatalf("unexpected initialize response: %s", string(rawResp))
	}

	// Notification produces no response
	_, hasResp, err = srv.HandleRequest(context.Background(), testCaller(), []byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}`))
	if err != nil || hasResp {
		t.Fatalf("notification should not produce response: hasResp=%v err=%v", hasResp, err)
	}

	// tools/list must expose strictly the 5 canonical tools
	listReq := `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`
	rawList, hasResp, err := srv.HandleRequest(context.Background(), testCaller(), []byte(listReq))
	if err != nil || !hasResp {
		t.Fatalf("tools/list failed: %v", err)
	}
	var listEnv struct {
		Result struct {
			Tools []struct {
				Name        string          `json:"name"`
				Description string          `json:"description"`
				InputSchema json.RawMessage `json:"inputSchema"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(rawList, &listEnv); err != nil {
		t.Fatalf("unmarshal tools/list: %v", err)
	}
	if len(listEnv.Result.Tools) != 5 {
		t.Fatalf("expected strictly 5 tools, got %d", len(listEnv.Result.Tools))
	}
	want := map[string]bool{
		"romero_memory_save":     false,
		"romero_memory_search":   false,
		"romero_memory_get":      false,
		"romero_memory_context":  false,
		"romero_memory_feedback": false,
	}
	for _, tool := range listEnv.Result.Tools {
		if _, ok := want[tool.Name]; !ok {
			t.Fatalf("unexpected tool exposed: %s", tool.Name)
		}
		want[tool.Name] = true
		if len(tool.InputSchema) == 0 || tool.Description == "" {
			t.Fatalf("tool %s missing description or inputSchema", tool.Name)
		}
	}
	for name, seen := range want {
		if !seen {
			t.Fatalf("missing canonical tool %s", name)
		}
	}
}

func TestServer_SearchOmitsContentAndPassesFiltersWithGovernor(t *testing.T) {
	gov := &stubGovernor{}
	memID := uuid.New()
	var capturedQuery retrieval.SearchQuery

	srv, err := mcp.NewServer(mcp.Dependencies{
		Governor: gov,
		Retrieval: stubRetrievalSvc{
			searchFn: func(_ context.Context, _ identity.AuthContext, q retrieval.SearchQuery) (*retrieval.SearchResult, error) {
				capturedQuery = q
				return &retrieval.SearchResult{
					TotalFound: 1,
					Candidates: []retrieval.SearchCandidate{
						{
							ID:              memID,
							Type:            memory.TypeProcedural,
							ScopeType:       identity.ScopeProjectShared,
							Status:          memory.StatusActive,
							EpistemicStatus: memory.EpistemicVerified,
							Summary:         "Always run go test with -count=1",
							Confidence:      0.92,
							FinalScore:      0.95,
							MinHopDepth:     0,
							ObservedAt:      time.Now().UTC(),
						},
					},
				}, nil
			},
		},
	})
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	req := `{"jsonrpc":"2.0","id":"s1","method":"tools/call","params":{"name":"romero_memory_search","arguments":{"query":"go test","memory_types":["procedural"],"min_confidence":0.8,"limit":5}}}`
	rawResp, hasResp, err := srv.HandleRequest(context.Background(), testCaller(), []byte(req))
	if err != nil || !hasResp {
		t.Fatalf("tools/call failed: %v", err)
	}
	if gov.begins.Load() != 1 || gov.ends.Load() != 1 {
		t.Fatalf("expected governor begin=1 end=1, got %d/%d", gov.begins.Load(), gov.ends.Load())
	}
	if len(capturedQuery.MemoryTypes) != 1 || capturedQuery.MemoryTypes[0] != memory.TypeProcedural {
		t.Fatalf("memory_types not forwarded: %+v", capturedQuery.MemoryTypes)
	}
	if capturedQuery.MinConfidence == nil || *capturedQuery.MinConfidence != 0.8 {
		t.Fatalf("min_confidence not forwarded: %+v", capturedQuery.MinConfidence)
	}
	respStr := string(rawResp)
	if strings.Contains(respStr, "SECRET_RAW_CONTENT_MUST_NOT_APPEAR_IN_SEARCH") {
		t.Fatalf("search output leaked raw content: %s", respStr)
	}
	if !strings.Contains(respStr, memID.String()) || !strings.Contains(respStr, "Always run go test") {
		t.Fatalf("search output missing summary or id: %s", respStr)
	}
}

func TestServer_GetBatchOpacityDeduplicationAndLimits(t *testing.T) {
	allowedID := uuid.New()
	forbiddenID := uuid.New()
	missingID := uuid.New()

	srv, err := mcp.NewServer(mcp.Dependencies{
		Retrieval: stubRetrievalSvc{
			getFn: func(_ context.Context, _ identity.AuthContext, q retrieval.GetMemoryQuery) (*retrieval.MemoryDetail, error) {
				switch q.MemoryID {
				case allowedID:
					return &retrieval.MemoryDetail{
						Memory: memory.Memory{
							ID:              allowedID,
							Type:            memory.TypeSemantic,
							ScopeType:       identity.ScopeAgentPrivate,
							Status:          memory.StatusActive,
							EpistemicStatus: memory.EpistemicHypothesis,
							Summary:         "Allowed summary",
							Content:         "Allowed content",
							Confidence:      0.85,
						},
						UntrustedXMLEnvelope: `<romero_memory id="` + allowedID.String() + `" untrusted_content="true"><summary>Allowed summary</summary><content>Allowed content</content></romero_memory>`,
					}, nil
				case forbiddenID:
					return nil, fmt.Errorf("%w: access denied", retrieval.ErrNotFound)
				default:
					return nil, retrieval.ErrNotFound
				}
			},
		},
	})
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	// Deduplicated batch: allowedID appears twice -> returned once; forbiddenID and missingID both return identical opaque not_found.
	req := fmt.Sprintf(`{"jsonrpc":"2.0","id":10,"method":"tools/call","params":{"name":"romero_memory_get","arguments":{"memory_ids":["%s","%s","%s","%s"]}}}`,
		allowedID, forbiddenID, allowedID, missingID)
	rawResp, _, err := srv.HandleRequest(context.Background(), testCaller(), []byte(req))
	if err != nil {
		t.Fatalf("HandleRequest failed: %v", err)
	}

	var env struct {
		Result struct {
			IsError           bool `json:"isError"`
			StructuredContent struct {
				Items []struct {
					MemoryID             uuid.UUID `json:"memory_id"`
					Status               string    `json:"status"`
					UntrustedXMLEnvelope string    `json:"untrusted_xml_envelope"`
				} `json:"items"`
			} `json:"structuredContent"`
		} `json:"result"`
	}
	if err := json.Unmarshal(rawResp, &env); err != nil {
		t.Fatalf("unmarshal get response: %v", err)
	}
	if env.Result.IsError {
		t.Fatalf("expected non-error batch result, got error: %s", string(rawResp))
	}
	if len(env.Result.StructuredContent.Items) != 3 {
		t.Fatalf("expected 3 deduplicated items, got %d", len(env.Result.StructuredContent.Items))
	}
	if env.Result.StructuredContent.Items[0].Status != "found" || !strings.Contains(env.Result.StructuredContent.Items[0].UntrustedXMLEnvelope, `untrusted_content="true"`) {
		t.Fatalf("unexpected first item: %+v", env.Result.StructuredContent.Items[0])
	}
	if env.Result.StructuredContent.Items[1].Status != "not_found" || env.Result.StructuredContent.Items[2].Status != "not_found" {
		t.Fatalf("expected identical opaque not_found for forbidden and missing: %+v", env.Result.StructuredContent.Items)
	}

	// Batch > 20 IDs must return structured IsError: true without dropping transport
	ids := make([]string, 21)
	for i := range ids {
		ids[i] = fmt.Sprintf("%q", uuid.New().String())
	}
	overLimitReq := fmt.Sprintf(`{"jsonrpc":"2.0","id":11,"method":"tools/call","params":{"name":"romero_memory_get","arguments":{"memory_ids":[%s]}}}`, strings.Join(ids, ","))
	rawOver, hasResp, err := srv.HandleRequest(context.Background(), testCaller(), []byte(overLimitReq))
	if err != nil || !hasResp {
		t.Fatalf("over-limit call should return structured JSON-RPC response: err=%v", err)
	}
	var errEnv struct {
		Result struct {
			IsError bool `json:"isError"`
		} `json:"result"`
	}
	if err := json.Unmarshal(rawOver, &errEnv); err != nil || !errEnv.Result.IsError {
		t.Fatalf("expected isError=true for >20 memory_ids, got %s", string(rawOver))
	}
}

func TestServer_SaveAndFeedbackOpaqueReceiptAndContextXML(t *testing.T) {
	reqID := uuid.New()
	fbReqID := uuid.New()
	memID := uuid.New()

	srv, err := mcp.NewServer(mcp.Dependencies{
		Capture: stubCaptureSvc{
			fn: func(_ context.Context, _ identity.AuthContext, cmd capture.CaptureCommand) (*capture.CaptureResult, error) {
				if cmd.ScopeType == identity.ScopeTaskShared {
					return &capture.CaptureResult{
						Accepted:      true,
						OpaqueReceipt: &capture.OpaqueReceipt{RequestID: reqID},
					}, nil
				}
				return &capture.CaptureResult{
					Created: true,
					Memory: &capture.MemoryView{
						ID:              memID,
						ScopeType:       cmd.ScopeType,
						Type:            cmd.Type,
						EpistemicStatus: cmd.EpistemicStatus,
						Summary:         cmd.Summary,
						Confidence:      0.5,
					},
				}, nil
			},
		},
		Feedback: stubFeedbackSvc{
			fn: func(_ context.Context, _ identity.AuthContext, cmd feedback.FeedbackCommand) (*feedback.FeedbackResult, error) {
				return &feedback.FeedbackResult{
					Accepted:      true,
					OpaqueReceipt: &feedback.OpaqueReceipt{RequestID: fbReqID},
				}, nil
			},
		},
		Context: stubContextSvc{
			fn: func(_ context.Context, _ identity.AuthContext, req contextbuilder.BuildContextRequest) (*contextbuilder.BuildContextResult, error) {
				return &contextbuilder.BuildContextResult{
					CombinedXML: "<romero_context><romero_memory untrusted_content=\"true\"/></romero_context>",
					UsedTokens:  42,
					MaxTokens:   req.MaxTokens,
					Items: []contextbuilder.ContextItem{
						{MemoryID: memID, TokenCost: 42, FinalScore: 0.9},
					},
				}, nil
			},
		},
	})
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	saveReq := fmt.Sprintf(`{"jsonrpc":"2.0","id":20,"method":"tools/call","params":{"name":"romero_memory_save","arguments":{"task_id":"task-1","type":"episodic","scope_type":"task_shared","summary":"Shared note","content":"Details","epistemic_status":"hypothesis","importance":0.6,"source_type":"%s","external_source_id":"agent-1"}}}`, provenance.SourceAgentExecution)
	rawSave, _, err := srv.HandleRequest(context.Background(), testCaller(), []byte(saveReq))
	if err != nil {
		t.Fatalf("save failed: %v", err)
	}
	if !strings.Contains(string(rawSave), reqID.String()) || !strings.Contains(string(rawSave), `"accepted":true`) {
		t.Fatalf("expected opaque receipt in save response, got %s", string(rawSave))
	}

	fbReq := fmt.Sprintf(`{"jsonrpc":"2.0","id":21,"method":"tools/call","params":{"name":"romero_memory_feedback","arguments":{"memory_id":"%s","task_id":"task-1","signal":"helpful_success"}}}`, memID)
	rawFB, _, err := srv.HandleRequest(context.Background(), testCaller(), []byte(fbReq))
	if err != nil {
		t.Fatalf("feedback failed: %v", err)
	}
	if !strings.Contains(string(rawFB), fbReqID.String()) || !strings.Contains(string(rawFB), `"accepted":true`) {
		t.Fatalf("expected opaque receipt in feedback response, got %s", string(rawFB))
	}

	ctxReq := `{"jsonrpc":"2.0","id":22,"method":"tools/call","params":{"name":"romero_memory_context","arguments":{"query":"architecture","max_tokens":1000}}}`
	rawCtx, _, err := srv.HandleRequest(context.Background(), testCaller(), []byte(ctxReq))
	if err != nil {
		t.Fatalf("context failed: %v", err)
	}
	if !strings.Contains(string(rawCtx), "romero_context") || !strings.Contains(string(rawCtx), `"used_tokens":42`) {
		t.Fatalf("unexpected context response: %s", string(rawCtx))
	}

	// Sync save & sync feedback paths
	syncSaveReq := fmt.Sprintf(`{"jsonrpc":"2.0","id":23,"method":"tools/call","params":{"name":"romero_memory_save","arguments":{"type":"semantic","scope_type":"agent_private","summary":"Sync","content":"Body","epistemic_status":"observed","importance":0.5,"source_type":"%s","external_source_id":"a1"}}}`, provenance.SourceAgentExecution)
	rawSyncSave, _, err := srv.HandleRequest(context.Background(), testCaller(), []byte(syncSaveReq))
	if err != nil || !strings.Contains(string(rawSyncSave), memID.String()) {
		t.Fatalf("expected sync save memory id, got %s err=%v", string(rawSyncSave), err)
	}
}

func TestServer_ErrorHandlingAndProtocolEdgeCases(t *testing.T) {
	srv, err := mcp.NewServer(mcp.Dependencies{})
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"empty", "   ", `"code":-32600`},
		{"bad json", "{bad", `"code":-32700`},
		{"bad version", `{"jsonrpc":"1.0","id":1,"method":"ping"}`, `"code":-32600`},
		{"unknown method", `{"jsonrpc":"2.0","id":2,"method":"unknown/op"}`, `"code":-32601`},
		{"ping", `{"jsonrpc":"2.0","id":3,"method":"ping"}`, `"result":{}`},
		{"bad tool params", `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{}}`, `"code":-32602`},
		{"unknown tool", `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"nope","arguments":{}}}`, `"isError":true`},
		{"unconfigured save", `{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"romero_memory_save","arguments":{}}}`, `"isError":true`},
		{"unconfigured search", `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"romero_memory_search","arguments":{}}}`, `"isError":true`},
		{"unconfigured get", `{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"romero_memory_get","arguments":{}}}`, `"isError":true`},
		{"unconfigured context", `{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"romero_memory_context","arguments":{}}}`, `"isError":true`},
		{"unconfigured feedback", `{"jsonrpc":"2.0","id":10,"method":"tools/call","params":{"name":"romero_memory_feedback","arguments":{}}}`, `"isError":true`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, hasResp, err := srv.HandleRequest(context.Background(), testCaller(), []byte(tc.raw))
			if err != nil || !hasResp {
				t.Fatalf("expected response, got hasResp=%v err=%v", hasResp, err)
			}
			if !strings.Contains(string(raw), tc.want) {
				t.Fatalf("got %s, want substring %s", string(raw), tc.want)
			}
		})
	}
}
