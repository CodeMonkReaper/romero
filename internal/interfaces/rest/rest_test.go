package rest_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/romero-project/romero/internal/application/capture"
	contextbuilder "github.com/romero-project/romero/internal/application/context"
	"github.com/romero-project/romero/internal/application/feedback"
	"github.com/romero-project/romero/internal/application/retrieval"
	"github.com/romero-project/romero/internal/domain/identity"
	"github.com/romero-project/romero/internal/domain/memory"
	"github.com/romero-project/romero/internal/infrastructure/cachel1"
	"github.com/romero-project/romero/internal/interfaces/rest"
)

type fakeAuth struct {
	auth identity.AuthContext
	err  error
}

func (f fakeAuth) Authenticate(_ context.Context, rawKey string) (identity.AuthContext, error) {
	if f.err != nil || rawKey != "valid-key" {
		return identity.AuthContext{}, errors.New("unauthorized")
	}
	return f.auth, nil
}

type fakeCaptureSvc struct {
	res *capture.CaptureResult
	mut *capture.MutationResult
	sup *capture.SupersedeResult
	err error
}

func (f fakeCaptureSvc) CaptureMemory(_ context.Context, _ identity.AuthContext, _ capture.CaptureCommand) (*capture.CaptureResult, error) {
	return f.res, f.err
}
func (f fakeCaptureSvc) UpdateMemory(_ context.Context, _ identity.AuthContext, _ uuid.UUID, _ capture.UpdateCommand) (*capture.MutationResult, error) {
	return f.mut, f.err
}
func (f fakeCaptureSvc) SupersedeMemory(_ context.Context, _ identity.AuthContext, _ uuid.UUID, _ capture.ExplicitSupersedeCommand) (*capture.SupersedeResult, error) {
	return f.sup, f.err
}
func (f fakeCaptureSvc) ArchiveMemory(_ context.Context, _ identity.AuthContext, _ uuid.UUID, _ capture.ArchiveCommand) (*capture.MutationResult, error) {
	return f.mut, f.err
}

type fakeRetrievalSvc struct {
	detail *retrieval.MemoryDetail
	status *retrieval.RequestStatus
	err    error
}

func (f fakeRetrievalSvc) GetMemory(_ context.Context, _ identity.AuthContext, _ retrieval.GetMemoryQuery) (*retrieval.MemoryDetail, error) {
	return f.detail, f.err
}
func (f fakeRetrievalSvc) GetRequestStatus(_ context.Context, _ identity.AuthContext, _ uuid.UUID, _ string) (*retrieval.RequestStatus, error) {
	return f.status, f.err
}
func (f fakeRetrievalSvc) SearchMemories(_ context.Context, _ identity.AuthContext, _ retrieval.SearchQuery) (*retrieval.SearchResult, error) {
	return &retrieval.SearchResult{
		Candidates: []retrieval.SearchCandidate{{ID: uuid.New(), Summary: "hit"}},
		TotalFound: 1,
	}, f.err
}
func (f fakeRetrievalSvc) GetSubgraph(_ context.Context, _ identity.AuthContext, q retrieval.SubgraphQuery) (*retrieval.SubgraphResult, error) {
	return &retrieval.SubgraphResult{
		RootMemoryID: q.MemoryID,
		Nodes:        []retrieval.SubgraphNode{{ID: q.MemoryID, Summary: "root"}},
	}, f.err
}

type fakeContextSvc struct {
	res *contextbuilder.BuildContextResult
	err error
}

func (f fakeContextSvc) BuildContext(_ context.Context, _ identity.AuthContext, req contextbuilder.BuildContextRequest) (*contextbuilder.BuildContextResult, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.res != nil {
		return f.res, nil
	}
	return &contextbuilder.BuildContextResult{
		MaxTokens:   req.MaxTokens,
		CombinedXML: `<romero_memory untrusted_content="true">ok</romero_memory>`,
	}, nil
}

type fakeL1Stats struct{}

func (fakeL1Stats) Stats() cachel1.GraphStats {
	return cachel1.GraphStats{Enabled: true, NodesCount: 3, EdgesCount: 2, EmbeddingsCount: 3}
}

type fakeFeedbackSvc struct {
	res *feedback.FeedbackResult
	err error
}

func (f fakeFeedbackSvc) RecordFeedback(_ context.Context, _ identity.AuthContext, _ feedback.FeedbackCommand) (*feedback.FeedbackResult, error) {
	return f.res, f.err
}

type fakeDB struct{ err error }

func (f fakeDB) Ping(_ context.Context) error { return f.err }

func testCaller() identity.AuthContext {
	return identity.AuthContext{
		TenantID:      uuid.New(),
		PrincipalID:   uuid.New(),
		PrincipalType: identity.PrincipalUser,
	}
}

func TestREST_AuthAndOpenAPISurface(t *testing.T) {
	h, _, err := rest.NewRouter(rest.RouterDeps{
		Auth:      fakeAuth{auth: testCaller()},
		Capture:   fakeCaptureSvc{},
		Retrieval: fakeRetrievalSvc{},
		Feedback:  fakeFeedbackSvc{},
		DB:        fakeDB{},
		Version:   "v0.1.0",
	})
	if err != nil {
		t.Fatalf("NewRouter failed: %v", err)
	}

	// 1. Missing auth -> 401
	req := httptest.NewRequest(http.MethodGet, "/api/v1/system/status", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without key, got %d", rec.Code)
	}

	// 2. Bearer auth -> 200
	req = httptest.NewRequest(http.MethodGet, "/api/v1/system/status", nil)
	req.Header.Set("Authorization", "Bearer valid-key")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with Bearer key, got %d (%s)", rec.Code, rec.Body.String())
	}

	// 3. X-API-Key auth -> 200
	req = httptest.NewRequest(http.MethodGet, "/api/v1/system/status", nil)
	req.Header.Set("X-API-Key", "valid-key")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with X-API-Key, got %d", rec.Code)
	}

	// 4. OpenAPI 3.1 surface has zero admin routes (AC-10)
	req = httptest.NewRequest(http.MethodGet, "/openapi.json", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on /openapi.json, got %d", rec.Code)
	}
	spec := rec.Body.String()
	for _, forbidden := range []string{"/tenants", "/projects", "/principals", "/task-permissions"} {
		if strings.Contains(spec, forbidden) {
			t.Fatalf("AC-10 violation: OpenAPI spec exposes admin path %q", forbidden)
		}
	}
	for _, required := range []string{"/api/v1/memories", "/api/v1/memory-requests/{request_id}", "/api/v1/system/status"} {
		if !strings.Contains(spec, required) {
			t.Fatalf("expected OpenAPI spec to contain %q", required)
		}
	}
}

func TestREST_CaptureOpaqueAndErrorMapping(t *testing.T) {
	reqID := uuid.New()
	capBody := []byte(`{"type":"semantic","scope_type":"task_shared","task_id":"t-1","summary":"s","content":"c","epistemic_status":"observed","importance":0.8,"source_type":"agent_execution","external_source_id":"e-1"}`)

	// 202 Opaque Accepted
	h202, _, _ := rest.NewRouter(rest.RouterDeps{
		Auth:      fakeAuth{auth: testCaller()},
		Capture:   fakeCaptureSvc{res: &capture.CaptureResult{Accepted: true, OpaqueReceipt: &capture.OpaqueReceipt{RequestID: reqID}}},
		Retrieval: fakeRetrievalSvc{status: &retrieval.RequestStatus{RequestID: reqID, Status: "pending"}},
		Feedback:  fakeFeedbackSvc{},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/memories", bytes.NewReader(capBody))
	req.Header.Set("X-API-Key", "valid-key")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h202.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202 Accepted, got %d (%s)", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["request_id"] != reqID.String() || resp["memory"] != nil {
		t.Fatalf("expected opaque {request_id} only, got %#v", resp)
	}

	// GET /api/v1/memory-requests/{request_id} -> 200
	req = httptest.NewRequest(http.MethodGet, "/api/v1/memory-requests/"+reqID.String(), nil)
	req.Header.Set("X-API-Key", "valid-key")
	rec = httptest.NewRecorder()
	h202.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on request status, got %d", rec.Code)
	}

	// Error mappings: 404, 409, 422, 503
	for _, tc := range []struct {
		err  error
		code int
	}{
		{retrieval.ErrNotFound, http.StatusNotFound},
		{capture.ErrConflict, http.StatusConflict},
		{capture.ErrValidation, http.StatusUnprocessableEntity},
		{retrieval.ErrOperational, http.StatusServiceUnavailable},
	} {
		hErr, _, _ := rest.NewRouter(rest.RouterDeps{
			Auth:      fakeAuth{auth: testCaller()},
			Capture:   fakeCaptureSvc{err: tc.err},
			Retrieval: fakeRetrievalSvc{err: tc.err},
			Feedback:  fakeFeedbackSvc{err: tc.err},
		})
		req := httptest.NewRequest(http.MethodGet, "/api/v1/memories/"+uuid.New().String(), nil)
		req.Header.Set("X-API-Key", "valid-key")
		rec := httptest.NewRecorder()
		hErr.ServeHTTP(rec, req)
		if rec.Code != tc.code {
			t.Fatalf("err %v: expected status %d, got %d (%s)", tc.err, tc.code, rec.Code, rec.Body.String())
		}
	}

	// Invalid UUID -> 422 before service call
	req = httptest.NewRequest(http.MethodGet, "/api/v1/memories/not-a-uuid", nil)
	req.Header.Set("X-API-Key", "valid-key")
	rec = httptest.NewRecorder()
	h202.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 on invalid UUID, got %d", rec.Code)
	}

	// 200 Feedback
	hFB, _, _ := rest.NewRouter(rest.RouterDeps{
		Auth:      fakeAuth{auth: testCaller()},
		Capture:   fakeCaptureSvc{},
		Retrieval: fakeRetrievalSvc{},
		Feedback:  fakeFeedbackSvc{res: &feedback.FeedbackResult{MemoryID: uuid.New(), NewStatus: memory.StatusActive}},
	})
	fbBody, _ := json.Marshal(map[string]any{"memory_id": uuid.New().String(), "signal": "helpful_success"})
	req = httptest.NewRequest(http.MethodPost, "/api/v1/memories/feedback", bytes.NewReader(fbBody))
	req.Header.Set("X-API-Key", "valid-key")
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	hFB.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on feedback, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestREST_AllRoutesAndEdgeCases(t *testing.T) {
	memID := uuid.New()
	reqID := uuid.New()
	view := &capture.MemoryView{ID: memID, Status: memory.StatusActive}

	h, _, _ := rest.NewRouter(rest.RouterDeps{
		Auth: fakeAuth{auth: testCaller()},
		Capture: fakeCaptureSvc{
			res: &capture.CaptureResult{Created: true, Memory: view},
			mut: &capture.MutationResult{Memory: view},
			sup: &capture.SupersedeResult{OldMemory: view, NewMemory: view},
		},
		Retrieval: fakeRetrievalSvc{
			detail: &retrieval.MemoryDetail{
				Memory:               memory.Memory{ID: memID, Status: memory.StatusActive},
				UntrustedXMLEnvelope: `<romero_memory untrusted_content="true">ok</romero_memory>`,
			},
			status: &retrieval.RequestStatus{RequestID: reqID, Status: "completed"},
		},
		Feedback: fakeFeedbackSvc{
			res: &feedback.FeedbackResult{Accepted: true, OpaqueReceipt: &feedback.OpaqueReceipt{RequestID: reqID}},
		},
		DB:      fakeDB{err: errors.New("pg down")},
		Version: "v0.1.0",
	})

	doReq := func(method, path string, body any) *httptest.ResponseRecorder {
		var rBody *bytes.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			rBody = bytes.NewReader(b)
		} else {
			rBody = bytes.NewReader(nil)
		}
		req := httptest.NewRequest(method, path, rBody)
		req.Header.Set("X-API-Key", "valid-key")
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	// POST /memories -> 201
	capBody := map[string]any{
		"type": "semantic", "scope_type": "project_shared", "summary": "s", "content": "c",
		"epistemic_status": "observed", "importance": 0.8, "source_type": "user_input", "external_source_id": "u-1",
	}
	if rec := doReq(http.MethodPost, "/api/v1/memories", capBody); rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d (%s)", rec.Code, rec.Body.String())
	}

	// GET /memories/{id} -> 200 (current + as_of + valid_at/known_at)
	if rec := doReq(http.MethodGet, "/api/v1/memories/"+memID.String(), nil); rec.Code != http.StatusOK {
		t.Fatalf("expected 200 GET memory, got %d", rec.Code)
	}
	if rec := doReq(http.MethodGet, "/api/v1/memories/"+memID.String()+"?as_of=2026-11-14T12:00:00Z", nil); rec.Code != http.StatusOK {
		t.Fatalf("expected 200 GET as_of, got %d", rec.Code)
	}
	if rec := doReq(http.MethodGet, "/api/v1/memories/"+memID.String()+"?valid_at=2026-11-14T12:00:00Z&known_at=2026-11-14T12:00:00Z", nil); rec.Code != http.StatusOK {
		t.Fatalf("expected 200 GET valid_at/known_at, got %d", rec.Code)
	}
	if rec := doReq(http.MethodGet, "/api/v1/memories/"+memID.String()+"?as_of=bad-date", nil); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 bad as_of, got %d", rec.Code)
	}

	// PATCH /memories/{id}, POST /supersede, POST /archive -> 200
	if rec := doReq(http.MethodPatch, "/api/v1/memories/"+memID.String(), map[string]any{"summary": "s2", "content": "c2"}); rec.Code != http.StatusOK {
		t.Fatalf("expected 200 PATCH, got %d (%s)", rec.Code, rec.Body.String())
	}
	if rec := doReq(http.MethodPost, "/api/v1/memories/"+memID.String()+"/supersede", map[string]any{"change_reason": "r", "capture": capBody}); rec.Code != http.StatusOK {
		t.Fatalf("expected 200 supersede, got %d (%s)", rec.Code, rec.Body.String())
	}
	if rec := doReq(http.MethodPost, "/api/v1/memories/"+memID.String()+"/archive", map[string]any{"reason": "done"}); rec.Code != http.StatusOK {
		t.Fatalf("expected 200 archive, got %d (%s)", rec.Code, rec.Body.String())
	}

	// POST /memories/feedback -> 202 opaque with replacement_memory
	if rec := doReq(http.MethodPost, "/api/v1/memories/feedback", map[string]any{
		"memory_id": memID.String(), "signal": "corrected_by_user", "replacement_memory": capBody,
	}); rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202 opaque feedback, got %d (%s)", rec.Code, rec.Body.String())
	}

	// GET /memory-requests/bad -> 422
	if rec := doReq(http.MethodGet, "/api/v1/memory-requests/bad", nil); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 bad request_id, got %d", rec.Code)
	}

	// GET /system/status with DB error -> 503
	if rec := doReq(http.MethodGet, "/api/v1/system/status", nil); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when DB ping fails, got %d", rec.Code)
	}
}

func TestREST_Phase02HybridContextSubgraphAndRelations(t *testing.T) {
	memID := uuid.New()
	h, _, err := rest.NewRouter(rest.RouterDeps{
		Auth:      fakeAuth{auth: testCaller()},
		Capture:   fakeCaptureSvc{res: &capture.CaptureResult{Created: true, Memory: &capture.MemoryView{ID: memID}}},
		Retrieval: fakeRetrievalSvc{},
		Context:   fakeContextSvc{},
		Feedback:  fakeFeedbackSvc{},
		DB:        fakeDB{},
		L1Stats:   fakeL1Stats{},
		Version:   "v0.2.0",
	})
	if err != nil {
		t.Fatalf("NewRouter failed: %v", err)
	}

	doReq := func(method, path string, body any) *httptest.ResponseRecorder {
		var rBody *bytes.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			rBody = bytes.NewReader(b)
		} else {
			rBody = bytes.NewReader(nil)
		}
		req := httptest.NewRequest(method, path, rBody)
		req.Header.Set("Authorization", "Bearer valid-key")
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	// POST /memories with explicit relations -> 201
	if rec := doReq(http.MethodPost, "/api/v1/memories", map[string]any{
		"type": "semantic", "scope_type": "project_shared", "summary": "s", "content": "c",
		"epistemic_status": "observed", "importance": 0.7, "source_type": "agent_execution", "external_source_id": "e1",
		"relations": []map[string]any{{"to_memory_id": uuid.New().String(), "relation_type": "depends_on"}},
	}); rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 capture with relations, got %d (%s)", rec.Code, rec.Body.String())
	}

	// POST /memories/search -> 200
	if rec := doReq(http.MethodPost, "/api/v1/memories/search", map[string]any{"query": "jwt", "limit": 5, "hops": 2}); rec.Code != http.StatusOK {
		t.Fatalf("expected 200 search, got %d (%s)", rec.Code, rec.Body.String())
	}

	// POST /memories/context -> 200
	if rec := doReq(http.MethodPost, "/api/v1/memories/context", map[string]any{"query": "jwt", "max_tokens": 512, "hops": 2}); rec.Code != http.StatusOK {
		t.Fatalf("expected 200 context, got %d (%s)", rec.Code, rec.Body.String())
	}

	// GET /memories/{id}/subgraph?hops=2 -> 200 & bad id -> 422
	if rec := doReq(http.MethodGet, "/api/v1/memories/"+memID.String()+"/subgraph?hops=2", nil); rec.Code != http.StatusOK {
		t.Fatalf("expected 200 subgraph, got %d (%s)", rec.Code, rec.Body.String())
	}
	if rec := doReq(http.MethodGet, "/api/v1/memories/bad-id/subgraph", nil); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 bad subgraph id, got %d", rec.Code)
	}

	// GET /system/status includes l1_graph metrics
	rec := doReq(http.MethodGet, "/api/v1/system/status", nil)
	if rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte(`"l1_graph"`)) {
		t.Fatalf("expected 200 with l1_graph metrics, got %d (%s)", rec.Code, rec.Body.String())
	}
}
