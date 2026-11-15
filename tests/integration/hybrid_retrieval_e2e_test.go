package integration_test

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/romero-project/romero/internal/domain/memory"
	"github.com/romero-project/romero/tests/internal/testpg"
)

// deterministicEmbedder generates normalized 1024-dim dense and sparse vectors
// based on keyword markers so semantic similarity and hybrid RRF behave predictably in E2E tests.
type deterministicEmbedder struct {
	fail bool
}

func (d *deterministicEmbedder) ModelName() string { return "bge-m3-test" }
func (d *deterministicEmbedder) Dimensions() int   { return 1024 }

func (d *deterministicEmbedder) Embed(_ context.Context, text string) (memory.EmbeddingOutput, error) {
	if d.fail {
		return memory.EmbeddingOutput{}, context.DeadlineExceeded
	}
	vec := make([]float32, 1024)
	lower := strings.ToLower(text)
	// Dimension buckets for deterministic cosine similarity.
	if strings.Contains(lower, "postgres") || strings.Contains(lower, "pool") || strings.Contains(lower, "pgbouncer") {
		vec[0] = 0.9
		vec[1] = 0.2
	}
	if strings.Contains(lower, "latency") || strings.Contains(lower, "timeout") || strings.Contains(lower, "incident") {
		vec[2] = 0.85
		vec[3] = 0.25
	}
	if strings.Contains(lower, "redis") || strings.Contains(lower, "cache") {
		vec[4] = 0.9
	}
	// Fallback unit direction so zero vectors never occur.
	vec[10] = 0.1
	var norm float64
	for _, v := range vec {
		norm += float64(v) * float64(v)
	}
	scale := float32(1.0 / math.Sqrt(norm))
	for i := range vec {
		vec[i] *= scale
	}

	var idx []int32
	var vals []float32
	if strings.Contains(lower, "pgbouncer") {
		idx = append(idx, 101)
		vals = append(vals, 1.5)
	}
	if strings.Contains(lower, "postgres") {
		idx = append(idx, 102)
		vals = append(vals, 1.2)
	}
	if strings.Contains(lower, "timeout") {
		idx = append(idx, 103)
		vals = append(vals, 1.1)
	}
	return memory.EmbeddingOutput{
		Dense:      vec,
		SparseIdx:  idx,
		SparseVals: vals,
	}, nil
}

func (d *deterministicEmbedder) EmbedBatch(ctx context.Context, texts []string) ([]memory.EmbeddingOutput, error) {
	out := make([]memory.EmbeddingOutput, 0, len(texts))
	for _, t := range texts {
		emb, err := d.Embed(ctx, t)
		if err != nil {
			return nil, err
		}
		out = append(out, emb)
	}
	return out, nil
}

func mustCaptureViaREST(t *testing.T, env *testpg.Env, apiKey string, payload map[string]any) uuid.UUID {
	t.Helper()
	resp, body := env.DoJSON(t, http.MethodPost, "/api/v1/memories", apiKey, payload)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("capture status=%d body=%s", resp.StatusCode, string(body))
	}
	var out struct {
		Memory struct {
			ID uuid.UUID `json:"id"`
		} `json:"memory"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("unmarshal capture: %v", err)
	}
	return out.Memory.ID
}

func TestHybridRetrievalEndToEndLifecycle(t *testing.T) {
	pool := testpg.OpenMigratedPool(t)
	env := testpg.NewEnv(t, pool)
	env.SetEmbedder(&deterministicEmbedder{})

	boot := bootstrapTenant(t, env, "hybrid-e2e")
	agentID := uuid.New()
	agent := createAgentPrincipal(t, env, boot.Tenant.ID, boot.Project.ID, agentID, "hybrid-worker", "agent_private,project_shared,org_shared")

	ctx := context.Background()

	// 1. Capture root memory M1, linked hop M2 (depends_on M1), and an unrelated memory M_noise.
	m1Payload := capturePayload(boot.Project.ID, "project_shared", "db.pool", "PostgreSQL connection pool configuration with PgBouncer transaction pooling", "verified_fact", "agent_observed")
	m1ID := mustCaptureViaREST(t, env, agent.APIKey, m1Payload)

	m2Payload := capturePayload(boot.Project.ID, "project_shared", "incident.timeout", "Checkout service timeout root cause traced to upstream settings", "verified_fact", "agent_observed")
	m2Payload["relations"] = []map[string]any{
		{
			"target_id": m1ID.String(),
			"type":      "depends_on",
			"weight":    0.9,
		},
	}
	m2ID := mustCaptureViaREST(t, env, agent.APIKey, m2Payload)

	mNoisePayload := capturePayload(boot.Project.ID, "project_shared", "cache.redis", "Redis cluster eviction policy allkeys-lru", "verified_fact", "agent_observed")
	_ = mustCaptureViaREST(t, env, agent.APIKey, mNoisePayload)

	// 2. Process async embedding jobs so halfvec(1024) + sparsevec are persisted in PG17.
	processed, err := env.EmbWorker.ProcessOnce(ctx)
	if err != nil {
		t.Fatalf("EmbWorker.ProcessOnce: %v", err)
	}
	if processed < 3 {
		t.Fatalf("expected >= 3 embedding jobs processed, got %d", processed)
	}

	// 3. POST /api/v1/memories/search -> progressive disclosure (no content field) + multi-hop discovery.
	searchResp, searchBody := env.DoJSON(t, http.MethodPost, "/api/v1/memories/search", agent.APIKey, map[string]any{
		"query":       "PgBouncer PostgreSQL pool",
		"project_ids": []string{boot.Project.ID.String()},
		"limit":       10,
		"hops":        2,
	})
	if searchResp.StatusCode != http.StatusOK {
		t.Fatalf("search status=%d body=%s", searchResp.StatusCode, string(searchBody))
	}
	var searchRaw map[string]any
	if err := json.Unmarshal(searchBody, &searchRaw); err != nil {
		t.Fatalf("unmarshal searchRaw: %v", err)
	}
	if degraded, _ := searchRaw["degraded_mode"].(bool); degraded {
		t.Fatalf("expected degraded_mode=false when embedder is active")
	}
	itemsRaw, ok := searchRaw["candidates"].([]any)
	if !ok || len(itemsRaw) == 0 {
		t.Fatalf("expected non-empty search candidates, got %s", string(searchBody))
	}
	foundM1, foundM2 := false, false
	for _, raw := range itemsRaw {
		item := raw.(map[string]any)
		if _, hasContent := item["content"]; hasContent {
			t.Fatalf("progressive disclosure violation: search item exposed content field: %v", item)
		}
		if item["id"] == m1ID.String() {
			foundM1 = true
		}
		if item["id"] == m2ID.String() {
			foundM2 = true
			if hop, _ := item["min_hop_depth"].(float64); hop < 1 {
				t.Fatalf("expected multi-hop item M2 to have min_hop_depth >= 1, got %v", item["min_hop_depth"])
			}
		}
	}
	if !foundM1 || !foundM2 {
		t.Fatalf("expected both M1 (%s) and 1-hop M2 (%s) in search results; got %s", m1ID, m2ID, string(searchBody))
	}

	// 4. POST /api/v1/memories/context -> submodular selection + XML untrusted_content="true" wrapping.
	ctxResp, ctxBody := env.DoJSON(t, http.MethodPost, "/api/v1/memories/context", agent.APIKey, map[string]any{
		"query":       "PgBouncer PostgreSQL pool",
		"project_ids": []string{boot.Project.ID.String()},
		"max_tokens":  500,
		"hops":        2,
	})
	if ctxResp.StatusCode != http.StatusOK {
		t.Fatalf("context status=%d body=%s", ctxResp.StatusCode, string(ctxBody))
	}
	var ctxOut struct {
		CombinedXML  string `json:"combined_xml"`
		DegradedMode bool   `json:"degraded_mode"`
		Items        []struct {
			MemoryID uuid.UUID `json:"memory_id"`
		} `json:"items"`
	}
	if err := json.Unmarshal(ctxBody, &ctxOut); err != nil {
		t.Fatalf("unmarshal context: %v", err)
	}
	if len(ctxOut.Items) == 0 {
		t.Fatalf("expected context items, got 0")
	}
	if !strings.Contains(ctxOut.CombinedXML, `untrusted_content="true"`) || !strings.Contains(ctxOut.CombinedXML, "<romero_memory ") {
		t.Fatalf("expected XML untrusted_content wrapping in combined_xml, got: %s", ctxOut.CombinedXML)
	}

	// 5. GET /api/v1/memories/{id}/subgraph -> 1-hop edge between M1 and M2.
	subResp, subBody := env.DoJSON(t, http.MethodGet, "/api/v1/memories/"+m1ID.String()+"/subgraph?hops=2", agent.APIKey, nil)
	if subResp.StatusCode != http.StatusOK {
		t.Fatalf("subgraph status=%d body=%s", subResp.StatusCode, string(subBody))
	}
	var subOut struct {
		Nodes []struct {
			ID uuid.UUID `json:"id"`
		} `json:"nodes"`
		Edges []struct {
			FromMemoryID uuid.UUID `json:"from_memory_id"`
			ToMemoryID   uuid.UUID `json:"to_memory_id"`
			RelationType string    `json:"relation_type"`
		} `json:"edges"`
	}
	if err := json.Unmarshal(subBody, &subOut); err != nil {
		t.Fatalf("unmarshal subgraph: %v", err)
	}
	if len(subOut.Edges) == 0 {
		t.Fatalf("expected non-empty subgraph edges around M1")
	}

	// 6. Pure read-only invariance: access_count and version must NOT mutate from search/context/subgraph.
	var accessCount int64
	var version int64
	if err := pool.QueryRow(ctx, `SELECT access_count, version FROM memories WHERE id = $1`, m1ID).Scan(&accessCount, &version); err != nil {
		t.Fatalf("query memory counters: %v", err)
	}
	if accessCount != 0 || version != 1 {
		t.Fatalf("read-only invariance violated: access_count=%d version=%d", accessCount, version)
	}

	// 7. L1 Write-Through coherence: superseding M1 removes it from default search immediately.
	supResp, supBody := env.DoJSON(t, http.MethodPost, "/api/v1/memories/"+m1ID.String()+"/supersede", agent.APIKey, map[string]any{
		"summary":            "Summary: PostgreSQL PgBouncer v2 pool limits",
		"content":            "PostgreSQL connection pool updated to PgBouncer v2 limits",
		"epistemic_status":   "verified_fact",
		"importance":         0.9,
		"source_type":        "agent_observed",
		"external_source_id": "ext-src-2",
	})
	if supResp.StatusCode != http.StatusOK {
		t.Fatalf("supersede status=%d body=%s", supResp.StatusCode, string(supBody))
	}
	if _, err := env.EmbWorker.ProcessOnce(ctx); err != nil {
		t.Fatalf("EmbWorker after supersede: %v", err)
	}

	searchAfterSupResp, searchAfterSupBody := env.DoJSON(t, http.MethodPost, "/api/v1/memories/search", agent.APIKey, map[string]any{
		"query":       "PgBouncer PostgreSQL pool",
		"project_ids": []string{boot.Project.ID.String()},
		"limit":       10,
	})
	if searchAfterSupResp.StatusCode != http.StatusOK {
		t.Fatalf("search after supersede status=%d", searchAfterSupResp.StatusCode)
	}
	if strings.Contains(string(searchAfterSupBody), m1ID.String()) {
		t.Fatalf("superseded memory %s leaked into default hybrid search: %s", m1ID, string(searchAfterSupBody))
	}

	// 7b. Archive M2 and verify L1 write-through removes it from default hybrid search (AC-06).
	archResp, archBody := env.DoJSON(t, http.MethodPost, "/api/v1/memories/"+m2ID.String()+"/archive", agent.APIKey, map[string]any{
		"reason": "e2e archive verification",
	})
	if archResp.StatusCode != http.StatusOK {
		t.Fatalf("archive status=%d body=%s", archResp.StatusCode, string(archBody))
	}
	searchAfterArchResp, searchAfterArchBody := env.DoJSON(t, http.MethodPost, "/api/v1/memories/search", agent.APIKey, map[string]any{
		"query":       "Checkout service timeout root cause",
		"project_ids": []string{boot.Project.ID.String()},
		"limit":       10,
	})
	if searchAfterArchResp.StatusCode != http.StatusOK {
		t.Fatalf("search after archive status=%d", searchAfterArchResp.StatusCode)
	}
	if strings.Contains(string(searchAfterArchBody), m2ID.String()) {
		t.Fatalf("archived memory %s leaked into default hybrid search: %s", m2ID, string(searchAfterArchBody))
	}

	// 8. L1 System status metrics & degraded_mode resilience when BGE-M3 fails.
	statusResp, statusBody := env.DoJSON(t, http.MethodGet, "/api/v1/system/status", boot.APIKey, nil)
	if statusResp.StatusCode != http.StatusOK {
		t.Fatalf("system status=%d body=%s", statusResp.StatusCode, string(statusBody))
	}
	var sysOut struct {
		L1NodesCount int `json:"l1_nodes_count"`
		L1EdgesCount int `json:"l1_edges_count"`
	}
	if err := json.Unmarshal(statusBody, &sysOut); err != nil {
		t.Fatalf("unmarshal system status: %v", err)
	}
	if sysOut.L1NodesCount == 0 || sysOut.L1EdgesCount == 0 {
		t.Fatalf("expected non-zero L1 metrics, got %+v", sysOut)
	}

	env.SetEmbedder(&deterministicEmbedder{fail: true})
	degResp, degBody := env.DoJSON(t, http.MethodPost, "/api/v1/memories/search", agent.APIKey, map[string]any{
		"query":       "PgBouncer",
		"project_ids": []string{boot.Project.ID.String()},
		"limit":       10,
	})
	if degResp.StatusCode != http.StatusOK {
		t.Fatalf("degraded search status=%d body=%s", degResp.StatusCode, string(degBody))
	}
	var degOut struct {
		DegradedMode bool `json:"degraded_mode"`
		Candidates   []struct {
			ID uuid.UUID `json:"id"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(degBody, &degOut); err != nil {
		t.Fatalf("unmarshal degraded search: %v", err)
	}
	if !degOut.DegradedMode {
		t.Fatalf("expected degraded_mode=true when embedder fails")
	}
}
