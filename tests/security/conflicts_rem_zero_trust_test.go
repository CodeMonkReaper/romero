package security_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/romero-project/romero/internal/application/capture"
	"github.com/romero-project/romero/internal/application/conflicts"
	"github.com/romero-project/romero/internal/application/consolidation"
	"github.com/romero-project/romero/internal/domain/governor"
	"github.com/romero-project/romero/internal/domain/identity"
	"github.com/romero-project/romero/internal/domain/memory"
	"github.com/romero-project/romero/internal/domain/provenance"
	"github.com/romero-project/romero/internal/infrastructure/neural"
	"github.com/romero-project/romero/internal/interfaces/rest"
	"github.com/romero-project/romero/tests/internal/testpg"
)

type pgSecTaskAccess struct {
	env *testpg.Env
}

func (a pgSecTaskAccess) HasTaskPermission(ctx context.Context, q identity.TaskPermissionQuery) (bool, error) {
	snap, err := a.env.Store.TaskPermissions().EvaluateTaskAccess(ctx, q)
	if err != nil {
		return false, err
	}
	if q.Permission == identity.TaskWrite {
		return snap.CanWrite, nil
	}
	return snap.CanRead, nil
}

func doSecPhase03REST(t *testing.T, handler http.Handler, method, path, apiKey string, body any) (*http.Response, []byte) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec.Result(), rec.Body.Bytes()
}

func newSecPhase03Router(t *testing.T, env *testpg.Env, gov *governor.PowerGovernor) http.Handler {
	t.Helper()
	conflictSvc, err := conflicts.NewService(
		env.Store,
		env.Store.Conflicts(),
		env.Store.Audit(),
		pgSecTaskAccess{env: env},
		env.L1Graph,
		nil,
	)
	if err != nil {
		t.Fatalf("conflicts.NewService: %v", err)
	}
	consSvc, err := consolidation.NewService(
		env.Store,
		env.Store.Consolidation(),
		&neural.DeterministicSLMConsolidator{},
		neural.DeterministicNLIProvider{},
		env.Store.Audit(),
		env.L1Graph,
		gov,
		nil,
	)
	if err != nil {
		t.Fatalf("consolidation.NewService: %v", err)
	}
	h, _, err := rest.NewRouter(rest.RouterDeps{
		Auth:          env.Auth,
		Capture:       env.CapSvc,
		Retrieval:     env.RetSvc,
		Context:       env.CtxSvc,
		Feedback:      env.FbSvc,
		Conflicts:     conflictSvc,
		Consolidation: consSvc,
		Governor:      gov,
		DB:            env.Pool,
		L1Stats:       env.L1Graph,
		Version:       "0.3.0-sec",
	})
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	return h
}

func insertActiveConflictEdge(t *testing.T, env *testpg.Env, tenantID, fromID, toID uuid.UUID) uuid.UUID {
	t.Helper()
	relID := uuid.New()
	_, err := env.Pool.Exec(context.Background(), `
		INSERT INTO memory_relations (
			id, tenant_id, from_memory_id, to_memory_id, relation_type,
			weight, nli_entailment, nli_contradiction, is_active, notes
		) VALUES ($1, $2, $3, $4, 'contradicts', 0.9, 0.03, 0.92, true, 'test conflict')
	`, relID, tenantID, fromID, toID)
	if err != nil {
		t.Fatalf("insert conflict edge: %v", err)
	}
	return relID
}

func TestPhase03_ConflictsZeroTrustIsolation(t *testing.T) {
	pool := testpg.OpenMigratedPool(t)
	env := testpg.NewEnv(t, pool)
	ctx := context.Background()

	tA := seedTenantForIsolation(t, env, "p3-zt-a-"+uuid.NewString()[:5])
	tB := seedTenantForIsolation(t, env, "p3-zt-b-"+uuid.NewString()[:5])

	gov := governor.NewPowerGovernor(governor.Config{Profile: governor.ProfileBalanced})
	handler := newSecPhase03Router(t, env, gov)

	// 1. Seed a conflict in Tenant B (must never be visible or resolvable by Tenant A).
	b1 := createMemoryViaREST(t, env, tB.OrchKey, captureBody(tB.Project1ID, "org_shared", "Tenant B secret config A"))
	b2 := createMemoryViaREST(t, env, tB.OrchKey, captureBody(tB.Project1ID, "org_shared", "Tenant B secret config B"))
	conflictB := insertActiveConflictEdge(t, env, tB.TenantID, b1, b2)

	// 2. Seed an agent_private conflict owned by Agent 2 inside Tenant A.
	a2p1 := createMemoryViaREST(t, env, tA.Agent2Key, captureBody(tA.Project2ID, "agent_private", "Agent2 private secret 1"))
	a2p2 := createMemoryViaREST(t, env, tA.Agent2Key, captureBody(tA.Project2ID, "agent_private", "Agent2 private secret 2"))
	conflictA2Priv := insertActiveConflictEdge(t, env, tA.TenantID, a2p1, a2p2)

	// 3. Seed a task_shared conflict in Tenant A for task-secret (Project2, owned by Agent 2; Agent 1 has NO permission).
	if _, err := env.RunCLI(t, "task-permission", "grant", "--dsn", testpg.TestDSN(), "--api-key", tA.OrchKey, "--tenant-id", tA.TenantID.String(), "--project-id", tA.Project2ID.String(), "--task-id", "task-secret", "--principal-id", tA.Agent2Princ.String(), "--read", "--write"); err != nil {
		t.Fatalf("grant ag2 task-secret: %v", err)
	}
	tSecret1 := createMemoryViaREST(t, env, tA.Agent2Key, map[string]any{
		"project_id":         tA.Project2ID,
		"task_id":            "task-secret",
		"type":               "semantic",
		"scope_type":         "task_shared",
		"topic_key":          "sec/task-1",
		"summary":            "Task secret 1",
		"content":            "Task secret content 1",
		"epistemic_status":   "reported",
		"importance":         0.8,
		"source_type":        "agent_execution",
		"external_source_id": "zt-ts-1",
	})
	tSecret2 := createMemoryViaREST(t, env, tA.Agent2Key, map[string]any{
		"project_id":         tA.Project2ID,
		"task_id":            "task-secret",
		"type":               "semantic",
		"scope_type":         "task_shared",
		"topic_key":          "sec/task-2",
		"summary":            "Task secret 2",
		"content":            "Task secret content 2",
		"epistemic_status":   "reported",
		"importance":         0.8,
		"source_type":        "agent_execution",
		"external_source_id": "zt-ts-2",
	})
	conflictTaskSecret := insertActiveConflictEdge(t, env, tA.TenantID, tSecret1, tSecret2)

	// Agent 1 lists conflicts -> 0.00% leakage (none of Tenant B, Agent 2 private, or task-secret).
	resp, body := doSecPhase03REST(t, handler, http.MethodGet, "/api/v1/conflicts", tA.Agent1Key, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/v1/conflicts status=%d body=%s", resp.StatusCode, string(body))
	}
	var listOut struct {
		Conflicts []struct {
			ConflictID uuid.UUID `json:"conflict_id"`
		} `json:"conflicts"`
	}
	if err := json.Unmarshal(body, &listOut); err != nil {
		t.Fatalf("unmarshal conflicts: %v", err)
	}
	for _, c := range listOut.Conflicts {
		if c.ConflictID == conflictB || c.ConflictID == conflictA2Priv || c.ConflictID == conflictTaskSecret {
			t.Fatalf("Zero-Trust conflict leakage detected: %s", c.ConflictID)
		}
	}

	// Cross-tenant resolve attempt -> 404 NotFound (no tenant existence oracle).
	respB, _ := doSecPhase03REST(t, handler, http.MethodPost, "/api/v1/conflicts/"+conflictB.String()+"/resolve", tA.Agent1Key, map[string]any{
		"winner_memory_id": b2,
	})
	if respB.StatusCode != http.StatusNotFound {
		t.Fatalf("cross-tenant resolve status = %d, want 404", respB.StatusCode)
	}

	// Unauthorized task_shared / project resolve attempt -> 403 or 404 (fail-closed, no oracle).
	respTS, _ := doSecPhase03REST(t, handler, http.MethodPost, "/api/v1/conflicts/"+conflictTaskSecret.String()+"/resolve", tA.Agent1Key, map[string]any{
		"winner_memory_id": tSecret2,
	})
	if respTS.StatusCode != http.StatusForbidden && respTS.StatusCode != http.StatusNotFound {
		t.Fatalf("unauthorized task_shared resolve status = %d, want 403 or 404", respTS.StatusCode)
	}

	// Verify Tenant B conflict remains active in PG17.
	var activeB bool
	if err := pool.QueryRow(ctx, `SELECT is_active FROM memory_relations WHERE id = $1`, conflictB).Scan(&activeB); err != nil {
		t.Fatalf("query conflictB: %v", err)
	}
	if !activeB {
		t.Fatal("Tenant B conflict was mutated by cross-tenant resolve attempt")
	}
}

func TestPhase03_REMCrossScopeBarrierAndAuthBoundary(t *testing.T) {
	pool := testpg.OpenMigratedPool(t)
	env := testpg.NewEnv(t, pool)
	ctx := context.Background()

	tA := seedTenantForIsolation(t, env, "p3-rem-zt-"+uuid.NewString()[:5])
	gov := governor.NewPowerGovernor(governor.Config{Profile: governor.ProfileBalanced})
	handler := newSecPhase03Router(t, env, gov)

	// 1. Unauthenticated request must return 401 and must NOT trigger PowerGovernor preemption.
	beforePreempt := gov.Status().PreemptionsCount
	respUnauth, _ := doSecPhase03REST(t, handler, http.MethodGet, "/api/v1/conflicts", "", nil)
	if respUnauth.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want 401", respUnauth.StatusCode)
	}
	if gov.Status().PreemptionsCount != beforePreempt {
		t.Fatalf("unauthenticated request triggered governor preemption")
	}

	// 2. Non-admin principal calling POST /api/v1/admin/consolidate -> 403 Forbidden.
	respNonAdmin, _ := doSecPhase03REST(t, handler, http.MethodPost, "/api/v1/admin/consolidate", tA.Agent1Key, map[string]any{"dry_run": false})
	if respNonAdmin.StatusCode != http.StatusForbidden {
		t.Fatalf("non-admin consolidate status = %d, want 403", respNonAdmin.StatusCode)
	}

	// 3. Cross-scope barrier: 2 project_shared episodes + 2 agent_private episodes with identical vectors.
	// Neither partition reaches REMMinClusterSize=3 on its own, so 0 clusters may form.
	adminCaller, err := env.Auth.Authenticate(ctx, tA.OrchKey)
	if err != nil {
		t.Fatalf("auth admin: %v", err)
	}
	agent1Caller, err := env.Auth.Authenticate(ctx, tA.Agent1Key)
	if err != nil {
		t.Fatalf("auth agent1: %v", err)
	}

	vec := make([]float32, 1024)
	vec[0] = 0.9
	vec[1] = 0.1
	seedEp := func(caller identity.AuthContext, scope identity.ScopeType, key string) {
		res, err := env.CapSvc.CaptureMemory(ctx, caller, capture.CaptureCommand{
			ProjectID:        &tA.Project1ID,
			Type:             memory.TypeEpisodic,
			ScopeType:        scope,
			TopicKey:         key,
			Summary:          "Shared barrier episode " + key,
			Content:          "Identical content for cross-scope barrier check",
			EpistemicStatus:  memory.EpistemicReported,
			Importance:       0.8,
			SourceType:       provenance.SourceAgentExecution,
			ExternalSourceID: key,
		})
		if err != nil {
			t.Fatalf("capture %s: %v", key, err)
		}
		if err := env.Store.Embeddings().UpsertMemoryEmbedding(ctx, memory.MemoryEmbedding{
			MemoryID:  res.Memory.ID,
			ModelName: "test-model",
			DenseVec:  vec,
			UpdatedAt: time.Now().UTC(),
		}); err != nil {
			t.Fatalf("upsert embedding %s: %v", key, err)
		}
		if _, err := pool.Exec(ctx, `UPDATE memories SET alpha_score = 9.0, beta_score = 1.0 WHERE id = $1`, res.Memory.ID); err != nil {
			t.Fatalf("boost alpha %s: %v", key, err)
		}
	}

	seedEp(adminCaller, identity.ScopeOrgShared, "barrier-os-1")
	seedEp(adminCaller, identity.ScopeOrgShared, "barrier-os-2")
	seedEp(agent1Caller, identity.ScopeAgentPrivate, "barrier-ap-1")
	seedEp(agent1Caller, identity.ScopeAgentPrivate, "barrier-ap-2")

	respCons, bodyCons := doSecPhase03REST(t, handler, http.MethodPost, "/api/v1/admin/consolidate", tA.OrchKey, map[string]any{
		"project_id": tA.Project1ID,
		"dry_run":    false,
	})
	if respCons.StatusCode != http.StatusOK {
		t.Fatalf("admin consolidate status=%d body=%s", respCons.StatusCode, string(bodyCons))
	}
	var consOut struct {
		ClustersFound        int `json:"clusters_found"`
		ConsolidatedEpisodes int `json:"consolidated_episodes"`
	}
	if err := json.Unmarshal(bodyCons, &consOut); err != nil {
		t.Fatalf("unmarshal consolidate: %v", err)
	}
	if consOut.ClustersFound != 0 || consOut.ConsolidatedEpisodes != 0 {
		t.Fatalf("cross-scope barrier violated: merged disparate scopes into %+v", consOut)
	}
}
