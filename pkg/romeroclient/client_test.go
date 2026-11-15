package romeroclient_test

import (
	"context"
	"encoding/json"
	"errors"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/romero-project/romero/pkg/romeroclient"
)

func TestPackageIsolation_NoInternalImports(t *testing.T) {
	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}
	parsedFiles := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(".", e.Name()), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("ParseFile %s failed: %v", e.Name(), err)
		}
		parsedFiles++
		for _, imp := range f.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			if strings.Contains(path, "/internal/") || strings.HasPrefix(path, "internal/") {
				t.Fatalf("pkg/romeroclient file %s illegally imports internal package %q", e.Name(), path)
			}
		}
	}
	if parsedFiles == 0 {
		t.Fatal("expected at least one non-test Go file in pkg/romeroclient")
	}
}

func TestClient_All14MethodsOpaqueReceiptsAndTypedErrors(t *testing.T) {
	memID := uuid.New()
	reqID := uuid.New()
	conflictID := uuid.New()

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/memories", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]any{"title": "Unauthorized", "status": 401, "detail": "invalid key"})
			return
		}
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]any{"request_id": reqID})
	})
	mux.HandleFunc("GET /api/v1/memories/{id}", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("known_at") == "" {
			t.Errorf("expected known_at query param")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"memory":                 map[string]any{"ID": memID, "Summary": "S", "ScopeType": "agent_private"},
			"untrusted_xml_envelope": `<romero_memory untrusted_content="true"/>`,
		})
	})
	mux.HandleFunc("PATCH /api/v1/memories/{id}", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"memory": map[string]any{"ID": memID, "Summary": "Updated"}})
	})
	mux.HandleFunc("POST /api/v1/memories/{id}/supersede", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"old_memory": map[string]any{"ID": memID}, "new_memory": map[string]any{"ID": uuid.New()}})
	})
	mux.HandleFunc("POST /api/v1/memories/{id}/archive", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]any{"request_id": reqID})
	})
	mux.HandleFunc("POST /api/v1/memories/feedback", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"memory_id": memID, "new_confidence": 0.9})
	})
	mux.HandleFunc("GET /api/v1/memory-requests/{request_id}", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"request_id": reqID, "status": "completed"})
	})
	mux.HandleFunc("POST /api/v1/memories/search", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"total_found": 1, "candidates": []map[string]any{{"id": memID, "summary": "hit", "final_score": 0.9}}})
	})
	mux.HandleFunc("POST /api/v1/memories/context", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"combined_xml": "<romero_context/>", "used_tokens": 10, "max_tokens": 500, "items": []any{}})
	})
	mux.HandleFunc("GET /api/v1/memories/{id}/subgraph", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"root_memory_id": memID, "nodes": []any{}, "edges": []any{}})
	})
	mux.HandleFunc("GET /api/v1/conflicts", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"conflicts": []map[string]any{{"conflict_id": conflictID, "is_active": true}}})
	})
	mux.HandleFunc("POST /api/v1/conflicts/{id}/resolve", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"conflict_id": conflictID, "is_active": false})
	})
	mux.HandleFunc("POST /api/v1/admin/consolidate", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"clusters_found": 2, "distilled_count": 1, "consolidated_episodes": 4})
	})
	mux.HandleFunc("GET /api/v1/system/status", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "version": "1.0.0", "database": "up"})
	})

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]any{"title": "Unauthorized", "status": 401, "detail": "invalid key"})
			return
		}
		mux.ServeHTTP(w, r)
	}))
	defer ts.Close()

	ctx := context.Background()
	badClient := romeroclient.New(ts.URL, "wrong-key")
	_, err := badClient.CaptureMemory(ctx, romeroclient.CaptureRequest{Summary: "s", Content: "c"})
	var apiErr *romeroclient.APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 *APIError, got %v", err)
	}

	c := romeroclient.New(ts.URL, "test-key")
	capResp, err := c.CaptureMemory(ctx, romeroclient.CaptureRequest{Summary: "s", Content: "c"})
	if err != nil || !capResp.Accepted || capResp.RequestID == nil || *capResp.RequestID != reqID {
		t.Fatalf("CaptureMemory 202 mismatch: resp=%+v err=%v", capResp, err)
	}

	if g, err := c.GetMemory(ctx, memID, &romeroclient.GetMemoryOptions{KnownAt: "2026-01-01T00:00:00Z"}); err != nil || g.Memory.ScopeType != "agent_private" {
		t.Fatalf("GetMemory failed: g=%+v err=%v", g, err)
	}
	if _, err := c.UpdateMemory(ctx, memID, romeroclient.UpdateMemoryRequest{Summary: "U", Content: "C"}); err != nil {
		t.Fatalf("UpdateMemory failed: %v", err)
	}
	if _, err := c.SupersedeMemory(ctx, memID, romeroclient.SupersedeMemoryRequest{}); err != nil {
		t.Fatalf("SupersedeMemory failed: %v", err)
	}
	archResp, err := c.ArchiveMemory(ctx, memID, romeroclient.ArchiveMemoryRequest{Reason: "done"})
	if err != nil || !archResp.Accepted {
		t.Fatalf("ArchiveMemory 202 failed: resp=%+v err=%v", archResp, err)
	}
	if _, err := c.RecordFeedback(ctx, romeroclient.FeedbackRequest{MemoryID: memID, Signal: "useful"}); err != nil {
		t.Fatalf("RecordFeedback failed: %v", err)
	}
	if st, err := c.GetRequestStatus(ctx, reqID); err != nil || st.Status != "completed" {
		t.Fatalf("GetRequestStatus failed: st=%+v err=%v", st, err)
	}
	if sr, err := c.SearchMemories(ctx, romeroclient.SearchRequest{Query: "q"}); err != nil || sr.TotalFound != 1 {
		t.Fatalf("SearchMemories failed: sr=%+v err=%v", sr, err)
	}
	if bc, err := c.BuildContext(ctx, romeroclient.BuildContextRequest{Query: "q", MaxTokens: 500}); err != nil || bc.UsedTokens != 10 {
		t.Fatalf("BuildContext failed: bc=%+v err=%v", bc, err)
	}
	if sg, err := c.GetSubgraph(ctx, memID, 2); err != nil || sg.RootMemoryID != memID {
		t.Fatalf("GetSubgraph failed: sg=%+v err=%v", sg, err)
	}
	if cs, err := c.ListConflicts(ctx, nil); err != nil || len(cs) != 1 {
		t.Fatalf("ListConflicts failed: cs=%+v err=%v", cs, err)
	}
	if rc, err := c.ResolveConflict(ctx, conflictID, romeroclient.ResolveConflictRequest{WinnerMemoryID: memID}); err != nil || rc.IsActive {
		t.Fatalf("ResolveConflict failed: rc=%+v err=%v", rc, err)
	}
	if con, err := c.Consolidate(ctx, romeroclient.ConsolidateRequest{DryRun: true}); err != nil || con.ClustersFound != 2 {
		t.Fatalf("Consolidate failed: con=%+v err=%v", con, err)
	}
	if sys, err := c.SystemStatus(ctx); err != nil || sys.Status != "ok" {
		t.Fatalf("SystemStatus failed: sys=%+v err=%v", sys, err)
	}

	// Verify options & error formatting
	cCustom := romeroclient.New(ts.URL, "test-key", romeroclient.WithHTTPClient(ts.Client()), romeroclient.WithUserAgent("custom-agent/1.0"))
	projID := uuid.New()
	if _, err := cCustom.ListConflicts(ctx, &romeroclient.ListConflictsOptions{ProjectID: &projID, Limit: 5, Offset: 1}); err != nil {
		t.Fatalf("ListConflicts with options failed: %v", err)
	}
	if _, err := cCustom.GetSubgraph(ctx, memID, 2); err != nil {
		t.Fatalf("GetSubgraph failed: %v", err)
	}
	if !strings.Contains(apiErr.Error(), "401") || !strings.Contains(apiErr.Error(), "invalid key") {
		t.Fatalf("unexpected APIError.Error(): %s", apiErr.Error())
	}

	// Verify error propagation across remaining methods on 401
	if _, err := badClient.GetMemory(ctx, memID, nil); err == nil {
		t.Fatal("expected error from GetMemory without auth")
	}
	if _, err := badClient.SearchMemories(ctx, romeroclient.SearchRequest{Query: "q"}); err == nil {
		t.Fatal("expected error from SearchMemories without auth")
	}
	if _, err := badClient.BuildContext(ctx, romeroclient.BuildContextRequest{Query: "q", MaxTokens: 100}); err == nil {
		t.Fatal("expected error from BuildContext without auth")
	}
	if _, err := badClient.RecordFeedback(ctx, romeroclient.FeedbackRequest{MemoryID: memID, Signal: "useful"}); err == nil {
		t.Fatal("expected error from RecordFeedback without auth")
	}
	if _, err := badClient.UpdateMemory(ctx, memID, romeroclient.UpdateMemoryRequest{}); err == nil {
		t.Fatal("expected error from UpdateMemory without auth")
	}
	if _, err := badClient.SupersedeMemory(ctx, memID, romeroclient.SupersedeMemoryRequest{}); err == nil {
		t.Fatal("expected error from SupersedeMemory without auth")
	}
	if _, err := badClient.ArchiveMemory(ctx, memID, romeroclient.ArchiveMemoryRequest{}); err == nil {
		t.Fatal("expected error from ArchiveMemory without auth")
	}
	if _, err := badClient.GetRequestStatus(ctx, reqID); err == nil {
		t.Fatal("expected error from GetRequestStatus without auth")
	}
	if _, err := badClient.GetSubgraph(ctx, memID, 1); err == nil {
		t.Fatal("expected error from GetSubgraph without auth")
	}
	if _, err := badClient.ListConflicts(ctx, nil); err == nil {
		t.Fatal("expected error from ListConflicts without auth")
	}
	if _, err := badClient.ResolveConflict(ctx, conflictID, romeroclient.ResolveConflictRequest{}); err == nil {
		t.Fatal("expected error from ResolveConflict without auth")
	}
	if _, err := badClient.Consolidate(ctx, romeroclient.ConsolidateRequest{}); err == nil {
		t.Fatal("expected error from Consolidate without auth")
	}
	if _, err := badClient.SystemStatus(ctx); err == nil {
		t.Fatal("expected error from SystemStatus without auth")
	}
}
