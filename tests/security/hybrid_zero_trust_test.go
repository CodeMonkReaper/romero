package security_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/romero-project/romero/tests/internal/testpg"
)

func createMemoryViaREST(t *testing.T, env *testpg.Env, apiKey string, payload map[string]any) uuid.UUID {
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

func TestHybridZeroTrustGraphAndSubgraphIsolation(t *testing.T) {
	pool := testpg.OpenMigratedPool(t)
	env := testpg.NewEnv(t, pool)

	tA := seedTenantForIsolation(t, env, "hyb-sec-a")
	tB := seedTenantForIsolation(t, env, "hyb-sec-b")
	ctx := context.Background()

	// Seed multi-hop chain inside Tenant A:
	// M1 (project_shared, accessible to ag1 and ag2)
	// M_priv (agent_private owned by ag2 -> invisible to ag1)
	// M3 (project_shared, accessible to ag1 and ag2, linked ONLY through M_priv!)
	m1Payload := captureBody(tA.Project1ID, "project_shared", "Shared bridge start token alpha-zero-trust")
	m1ID := createMemoryViaREST(t, env, tA.Agent2Key, m1Payload)

	mPrivPayload := captureBody(tA.Project1ID, "agent_private", "Top-secret ag2 private credential alpha-zero-trust")
	mPrivPayload["relations"] = []map[string]any{
		{"target_id": m1ID.String(), "type": "depends_on", "weight": 1.0},
	}
	mPrivID := createMemoryViaREST(t, env, tA.Agent2Key, mPrivPayload)

	m3Payload := captureBody(tA.Project1ID, "project_shared", "Downstream shared node behind private hop")
	m3Payload["relations"] = []map[string]any{
		{"target_id": mPrivID.String(), "type": "depends_on", "weight": 1.0},
	}
	m3ID := createMemoryViaREST(t, env, tA.Agent2Key, m3Payload)

	// Also seed a foreign-tenant memory in Tenant B.
	mForeignPayload := captureBody(tB.Project1ID, "project_shared", "Foreign tenant B secret alpha-zero-trust")
	mForeignID := createMemoryViaREST(t, env, tB.Agent1Key, mForeignPayload)

	// 1a. Explicitly requesting an unauthorized foreign project_id must return 403 Forbidden.
	forbResp, _ := env.DoJSON(t, http.MethodPost, "/api/v1/memories/search", tA.Agent1Key, map[string]any{
		"query":       "alpha-zero-trust",
		"project_ids": []string{tA.Project1ID.String(), tB.Project1ID.String()},
		"limit":       20,
		"hops":        2,
	})
	if forbResp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden when requesting foreign project %s, got %d", tB.Project1ID, forbResp.StatusCode)
	}

	// 1b. ag1 searches within authorized Tenant A project with hops=2 for "alpha-zero-trust".
	// M1 must be present (non-vacuous) with relations_count == 0 (neighbor M_priv is unauthorized),
	// and neither M_priv, M3 (only reachable via M_priv), nor mForeignID may appear.
	searchResp, searchBody := env.DoJSON(t, http.MethodPost, "/api/v1/memories/search", tA.Agent1Key, map[string]any{
		"query":       "alpha-zero-trust",
		"project_ids": []string{tA.Project1ID.String()},
		"limit":       20,
		"hops":        2,
	})
	if searchResp.StatusCode != http.StatusOK {
		t.Fatalf("search status=%d body=%s", searchResp.StatusCode, string(searchBody))
	}
	var searchOut struct {
		Candidates []struct {
			ID             uuid.UUID `json:"id"`
			RelationsCount int       `json:"relations_count"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(searchBody, &searchOut); err != nil {
		t.Fatalf("unmarshal search: %v", err)
	}
	foundM1 := false
	for _, c := range searchOut.Candidates {
		if c.ID == m1ID {
			foundM1 = true
			if c.RelationsCount != 0 {
				t.Fatalf("expected authorized relations_count=0 for M1 (only edge points to unauthorized M_priv), got %d", c.RelationsCount)
			}
		}
	}
	if !foundM1 {
		t.Fatalf("expected authorized memory M1 (%s) in search results, got %s", m1ID, string(searchBody))
	}
	bodyStr := string(searchBody)
	if strings.Contains(bodyStr, mPrivID.String()) {
		t.Fatalf("zero-trust breach: foreign agent_private node %s leaked in search", mPrivID)
	}
	if strings.Contains(bodyStr, m3ID.String()) {
		t.Fatalf("zero-trust breach: node %s traversed through unauthorized private intermediate %s", m3ID, mPrivID)
	}
	if strings.Contains(bodyStr, mForeignID.String()) {
		t.Fatalf("zero-trust breach: cross-tenant node %s leaked in search", mForeignID)
	}

	// 2. ag1 builds context with hops=2 -> must include M1 and must not leak M_priv, M3, or mForeignID.
	ctxResp, ctxBody := env.DoJSON(t, http.MethodPost, "/api/v1/memories/context", tA.Agent1Key, map[string]any{
		"query":       "alpha-zero-trust",
		"project_ids": []string{tA.Project1ID.String()},
		"max_tokens":  500,
		"hops":        2,
	})
	if ctxResp.StatusCode != http.StatusOK {
		t.Fatalf("context status=%d body=%s", ctxResp.StatusCode, string(ctxBody))
	}
	if !strings.Contains(string(ctxBody), m1ID.String()) {
		t.Fatalf("expected authorized memory M1 (%s) in context response, got: %s", m1ID, string(ctxBody))
	}
	if strings.Contains(string(ctxBody), mPrivID.String()) || strings.Contains(string(ctxBody), m3ID.String()) || strings.Contains(string(ctxBody), mForeignID.String()) {
		t.Fatalf("zero-trust breach in context prompt/items: %s", string(ctxBody))
	}

	// 3. ag1 calls GET /api/v1/memories/{M_priv}/subgraph -> 404 opacity + denied audit.
	subPrivResp, _ := env.DoJSON(t, http.MethodGet, "/api/v1/memories/"+mPrivID.String()+"/subgraph?hops=2", tA.Agent1Key, nil)
	if subPrivResp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 opacity for unauthorized subgraph root %s, got %d", mPrivID, subPrivResp.StatusCode)
	}

	// 4. ag1 calls GET /api/v1/memories/{M1}/subgraph -> 200 OK, but M_priv and M3 must be absent from nodes/edges.
	subM1Resp, subM1Body := env.DoJSON(t, http.MethodGet, "/api/v1/memories/"+m1ID.String()+"/subgraph?hops=2", tA.Agent1Key, nil)
	if subM1Resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for authorized subgraph root %s, got %d", m1ID, subM1Resp.StatusCode)
	}
	if strings.Contains(string(subM1Body), mPrivID.String()) || strings.Contains(string(subM1Body), m3ID.String()) {
		t.Fatalf("zero-trust breach in subgraph expansion around %s: %s", m1ID, string(subM1Body))
	}

	// 5. ag1 attempts to capture a relation targeting unauthorized M_priv -> 404 opacity + denied audit.
	badRelPayload := captureBody(tA.Project1ID, "project_shared", "Attempted unauthorized link")
	badRelPayload["relations"] = []map[string]any{
		{"target_id": mPrivID.String(), "type": "depends_on", "weight": 0.5},
	}
	badRelResp, _ := env.DoJSON(t, http.MethodPost, "/api/v1/memories", tA.Agent1Key, badRelPayload)
	if badRelResp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 opacity when linking unauthorized target %s, got %d", mPrivID, badRelResp.StatusCode)
	}

	// Verify denied audit events were persisted for ag1's unauthorized subgraph/relation attempts.
	var deniedCount int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM audit_events WHERE tenant_id = $1 AND actor_principal_id = $2 AND decision = 'denied'`,
		tA.TenantID, tA.Agent1Princ,
	).Scan(&deniedCount); err != nil {
		t.Fatalf("query denied audit_events: %v", err)
	}
	if deniedCount < 2 {
		t.Fatalf("expected >= 2 denied audit events for unauthorized subgraph & relation attempts, got %d", deniedCount)
	}

	// 6. task_shared permission revocation (AC-03): grant task-rev read+write to ag2 and read to ag1,
	// capture mTaskShared, confirm ag1 sees it, then revoke ag1 and verify 0% leakage.
	for _, args := range [][]string{
		{"task-permission", "grant", "--dsn", testpg.TestDSN(), "--api-key", tA.OrchKey, "--tenant-id", tA.TenantID.String(), "--project-id", tA.Project1ID.String(), "--task-id", "task-rev", "--principal-id", tA.Agent2Princ.String(), "--read", "--write"},
		{"task-permission", "grant", "--dsn", testpg.TestDSN(), "--api-key", tA.OrchKey, "--tenant-id", tA.TenantID.String(), "--project-id", tA.Project1ID.String(), "--task-id", "task-rev", "--principal-id", tA.Agent1Princ.String(), "--read"},
	} {
		if out, err := env.RunCLI(t, args...); err != nil {
			t.Fatalf("task-permission grant failed: %v\n%s", err, out)
		}
	}
	taskPayload := captureBody(tA.Project1ID, "task_shared", "Revocable task secret beta-revoked")
	taskPayload["task_id"] = "task-rev"
	mTaskID := createMemoryViaREST(t, env, tA.Agent2Key, taskPayload)

	visResp, visBody := env.DoJSON(t, http.MethodPost, "/api/v1/memories/search", tA.Agent1Key, map[string]any{
		"query":       "beta-revoked",
		"project_ids": []string{tA.Project1ID.String()},
		"task_id":     "task-rev",
		"limit":       10,
	})
	if visResp.StatusCode != http.StatusOK || !strings.Contains(string(visBody), mTaskID.String()) {
		t.Fatalf("expected ag1 to see task_shared memory %s before revocation; status=%d body=%s", mTaskID, visResp.StatusCode, string(visBody))
	}

	if out, err := env.RunCLI(t, "task-permission", "revoke", "--dsn", testpg.TestDSN(), "--api-key", tA.OrchKey, "--tenant-id", tA.TenantID.String(), "--project-id", tA.Project1ID.String(), "--task-id", "task-rev", "--principal-id", tA.Agent1Princ.String()); err != nil {
		t.Fatalf("task-permission revoke failed: %v\n%s", err, out)
	}

	// Explicit task_id after revocation must return 403 Forbidden.
	revForbResp, _ := env.DoJSON(t, http.MethodPost, "/api/v1/memories/search", tA.Agent1Key, map[string]any{
		"query":       "beta-revoked",
		"project_ids": []string{tA.Project1ID.String()},
		"task_id":     "task-rev",
		"limit":       10,
	})
	if revForbResp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden for revoked task_id search, got %d", revForbResp.StatusCode)
	}

	// Unscoped project search after revocation returns 200 OK without leaking mTaskID.
	revResp, revBody := env.DoJSON(t, http.MethodPost, "/api/v1/memories/search", tA.Agent1Key, map[string]any{
		"query":       "beta-revoked",
		"project_ids": []string{tA.Project1ID.String()},
		"limit":       10,
	})
	if revResp.StatusCode != http.StatusOK {
		t.Fatalf("search after task revocation status=%d body=%s", revResp.StatusCode, string(revBody))
	}
	if strings.Contains(string(revBody), mTaskID.String()) {
		t.Fatalf("zero-trust breach: revoked task_shared memory %s still visible in search: %s", mTaskID, string(revBody))
	}
}
