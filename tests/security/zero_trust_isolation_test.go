package security_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/romero-project/romero/internal/application/provisioning"
	"github.com/romero-project/romero/internal/domain/identity"
	"github.com/romero-project/romero/tests/internal/testpg"
)

type idHolder struct {
	ID uuid.UUID `json:"ID"`
}

type seededTenant struct {
	TenantID      uuid.UUID
	Project1ID    uuid.UUID
	Project2ID    uuid.UUID
	OrchKey       string
	OrchPrincipal uuid.UUID
	Agent1ID      uuid.UUID
	Agent1Key     string
	Agent1Princ   uuid.UUID
	Agent2ID      uuid.UUID
	Agent2Key     string
	Agent2Princ   uuid.UUID
	Priv1MemID    uuid.UUID
	Task1MemID    uuid.UUID
	OrgMemID      uuid.UUID
	OpaqueReqID   uuid.UUID
}

func captureBody(projectID uuid.UUID, scope, content string) map[string]any {
	return map[string]any{
		"project_id":         projectID.String(),
		"type":               "semantic",
		"scope_type":         scope,
		"summary":            "Summary: " + content,
		"content":            content,
		"epistemic_status":   "reported",
		"importance":         0.8,
		"source_type":        "user_input",
		"external_source_id": "sec-src-1",
	}
}

func seedTenantForIsolation(t *testing.T, env *testpg.Env, slug string) seededTenant {
	t.Helper()
	ctx := context.Background()

	out, err := env.RunCLI(t,
		"bootstrap",
		"--dsn", testpg.TestDSN(),
		"--tenant-slug", slug,
		"--tenant-name", "Sec "+slug,
		"--project-slug", "p1-"+slug,
		"--project-name", "P1 "+slug,
		"--principal-name", "orch-"+slug,
		"--principal-type", "orchestrator",
		"--scopes", "agent_private,task_shared,org_shared,global_public",
		"--admin",
	)
	if err != nil {
		t.Fatalf("bootstrap %s: %v\n%s", slug, err, out)
	}
	var boot struct {
		Tenant    idHolder `json:"tenant"`
		Project   idHolder `json:"project"`
		Principal idHolder `json:"principal"`
		APIKey    string   `json:"api_key"`
	}
	if err := json.Unmarshal([]byte(out), &boot); err != nil {
		t.Fatalf("unmarshal bootstrap: %v", err)
	}

	var proj2ID uuid.UUID
	if err := env.Pool.QueryRow(ctx, `INSERT INTO projects (tenant_id, slug, name) VALUES ($1, $2, $3) RETURNING id`, boot.Tenant.ID, "p2-"+slug, "P2 "+slug).Scan(&proj2ID); err != nil {
		t.Fatalf("insert proj2: %v", err)
	}

	createAgent := func(name string, agID uuid.UUID, proj uuid.UUID) (uuid.UUID, string) {
		if _, err := env.Pool.Exec(ctx,
			`INSERT INTO agents (id, tenant_id, external_key, name, role_type) VALUES ($1, $2, $3, $4, 'worker')`,
			agID, boot.Tenant.ID, "ext-"+name+"-"+agID.String()[:8], name,
		); err != nil {
			t.Fatalf("insert agent %s: %v", name, err)
		}
		pOut, err := env.RunCLI(t,
			"principal", "create",
			"--dsn", testpg.TestDSN(),
			"--tenant-id", boot.Tenant.ID.String(),
			"--agent-id", agID.String(),
			"--type", "agent",
			"--name", name,
			"--projects", proj.String(),
			"--scopes", "agent_private,task_shared,org_shared",
		)
		if err != nil {
			t.Fatalf("create agent %s: %v\n%s", name, err, pOut)
		}
		var res struct {
			Principal idHolder `json:"principal"`
			APIKey    string   `json:"api_key"`
		}
		_ = json.Unmarshal([]byte(pOut), &res)
		return res.Principal.ID, res.APIKey
	}

	ag1ID, ag2ID := uuid.New(), uuid.New()
	ag1Princ, ag1Key := createAgent("ag1-"+slug, ag1ID, boot.Project.ID)
	ag2Princ, ag2Key := createAgent("ag2-"+slug, ag2ID, proj2ID)

	if _, err := env.RunCLI(t, "task-permission", "grant", "--dsn", testpg.TestDSN(), "--api-key", boot.APIKey, "--tenant-id", boot.Tenant.ID.String(), "--project-id", boot.Project.ID.String(), "--task-id", "task-1", "--principal-id", ag1Princ.String(), "--read", "--write"); err != nil {
		t.Fatalf("grant ag1 task-1: %v", err)
	}
	if _, err := env.RunCLI(t, "task-permission", "grant", "--dsn", testpg.TestDSN(), "--api-key", boot.APIKey, "--tenant-id", boot.Tenant.ID.String(), "--project-id", boot.Project.ID.String(), "--task-id", "task-2", "--principal-id", ag2Princ.String(), "--write"); err != nil {
		t.Fatalf("grant ag2 task-2: %v", err)
	}

	captureMem := func(key string, payload map[string]any) uuid.UUID {
		resp, raw := env.DoJSON(t, http.MethodPost, "/api/v1/memories", key, payload)
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("capture failed (%d): %s", resp.StatusCode, string(raw))
		}
		var c struct {
			Memory struct {
				ID uuid.UUID `json:"id"`
			} `json:"memory"`
		}
		_ = json.Unmarshal(raw, &c)
		return c.Memory.ID
	}

	priv1ID := captureMem(ag1Key, captureBody(boot.Project.ID, "agent_private", "Private secret "+slug))

	task1Body := captureBody(boot.Project.ID, "task_shared", "Task-1 secret "+slug)
	task1Body["task_id"] = "task-1"
	task1ID := captureMem(ag1Key, task1Body)

	orgID := captureMem(boot.APIKey, captureBody(boot.Project.ID, "org_shared", "Org secret "+slug))

	// Enqueue an opaque task-2 request as write-only ag2 (who has proj2 in AllowedProjects, grant on proj1/task-2? Grant on proj2/task-2!).
	if _, err := env.RunCLI(t, "task-permission", "grant", "--dsn", testpg.TestDSN(), "--api-key", boot.APIKey, "--tenant-id", boot.Tenant.ID.String(), "--project-id", proj2ID.String(), "--task-id", "task-2", "--principal-id", ag2Princ.String(), "--write"); err != nil {
		t.Fatalf("grant ag2 proj2 task-2: %v", err)
	}
	opBody := captureBody(proj2ID, "task_shared", "Opaque request "+slug)
	opBody["task_id"] = "task-2"
	opBody["epistemic_status"] = "inferred"
	opBody["source_type"] = "agent_execution"
	reqResp, reqRaw := env.DoJSON(t, http.MethodPost, "/api/v1/memories", ag2Key, opBody)
	if reqResp.StatusCode != http.StatusAccepted {
		t.Fatalf("opaque enqueue failed (%d): %s", reqResp.StatusCode, string(reqRaw))
	}
	var reqOut struct {
		RequestID uuid.UUID `json:"request_id"`
	}
	_ = json.Unmarshal(reqRaw, &reqOut)

	return seededTenant{
		TenantID:      boot.Tenant.ID,
		Project1ID:    boot.Project.ID,
		Project2ID:    proj2ID,
		OrchKey:       boot.APIKey,
		OrchPrincipal: boot.Principal.ID,
		Agent1ID:      ag1ID,
		Agent1Key:     ag1Key,
		Agent1Princ:   ag1Princ,
		Agent2ID:      ag2ID,
		Agent2Key:     ag2Key,
		Agent2Princ:   ag2Princ,
		Priv1MemID:    priv1ID,
		Task1MemID:    task1ID,
		OrgMemID:      orgID,
		OpaqueReqID:   reqOut.RequestID,
	}
}

func TestZeroTrustCrossScopeIsolationMatrix(t *testing.T) {
	pool := testpg.OpenMigratedPool(t)
	env := testpg.NewEnv(t, pool)
	ctx := context.Background()

	tA := seedTenantForIsolation(t, env, "alpha")
	tB := seedTenantForIsolation(t, env, "beta")

	unknownMem := uuid.New()
	unknownReq := uuid.New()

	foreignTargetsForAgent2A := []uuid.UUID{
		tA.Priv1MemID, // same tenant, different owner_agent_id
		tA.Task1MemID, // same tenant, different task_id
		tB.Priv1MemID, // foreign tenant agent_private
		tB.Task1MemID, // foreign tenant task_shared
		tB.OrgMemID,   // foreign tenant org_shared
		unknownMem,    // non-existent UUID (indistinguishability check)
	}

	for _, targetID := range foreignTargetsForAgent2A {
		idStr := targetID.String()
		checks := []struct {
			method string
			path   string
			body   any
		}{
			{http.MethodGet, "/api/v1/memories/" + idStr, nil},
			{http.MethodPatch, "/api/v1/memories/" + idStr, map[string]any{"summary": "hijack", "content": "hijack"}},
			{http.MethodPost, "/api/v1/memories/" + idStr + "/supersede", map[string]any{"change_reason": "hijack", "capture": captureBody(tA.Project2ID, "org_shared", "hijack")}},
			{http.MethodPost, "/api/v1/memories/" + idStr + "/archive", map[string]any{"reason": "hijack"}},
			{http.MethodPost, "/api/v1/memories/feedback", map[string]any{"memory_id": idStr, "signal": "helpful_success"}},
		}
		for _, chk := range checks {
			resp, raw := env.DoJSON(t, chk.method, chk.path, tA.Agent2Key, chk.body)
			if resp.StatusCode != http.StatusNotFound {
				t.Fatalf("LEAK or oracle on %s %s (target=%s): expected 404, got %d: %s", chk.method, chk.path, idStr, resp.StatusCode, string(raw))
			}
		}
	}

	// Cross-tenant and unauthorized opaque request status -> 404 indistinguishable.
	for _, reqID := range []uuid.UUID{tB.OpaqueReqID, unknownReq} {
		resp, _ := env.DoJSON(t, http.MethodGet, "/api/v1/memory-requests/"+reqID.String(), tA.OrchKey, nil)
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("expected 404 on foreign/unknown opaque request %s, got %d", reqID, resp.StatusCode)
		}
	}

	// Verify audit_events persisted denied outcomes outside rollback.
	var deniedCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE tenant_id = $1 AND outcome = 'denied'`, tA.TenantID).Scan(&deniedCount); err != nil {
		t.Fatalf("count denied audit events: %v", err)
	}
	if deniedCount == 0 {
		t.Fatal("expected denied audit events persisted in audit_events")
	}
}

func TestZeroTrustFailClosedOnAuditFailure(t *testing.T) {
	pool := testpg.OpenMigratedPool(t)
	env := testpg.NewEnv(t, pool)
	ctx := context.Background()

	tA := seedTenantForIsolation(t, env, "auditfail")

	// Install a trigger that fails every INSERT into audit_events to prove fail-closed 503 + rollback.
	if _, err := pool.Exec(ctx, `
		CREATE OR REPLACE FUNCTION fail_audit_insert() RETURNS trigger AS $$
		BEGIN
			RAISE EXCEPTION 'simulated audit sink outage';
		END;
		$$ LANGUAGE plpgsql;
		CREATE TRIGGER trg_fail_audit BEFORE INSERT ON audit_events FOR EACH ROW EXECUTE FUNCTION fail_audit_insert();
	`); err != nil {
		t.Fatalf("create audit failure trigger: %v", err)
	}
	defer func() {
		_, _ = pool.Exec(context.Background(), `DROP TRIGGER IF EXISTS trg_fail_audit ON audit_events; DROP FUNCTION IF EXISTS fail_audit_insert();`)
	}()

	// Both denied foreign read and missing UUID read fail closed with 503 equivalently (no oracle).
	rForeign, _ := env.DoJSON(t, http.MethodGet, "/api/v1/memories/"+tA.Priv1MemID.String(), tA.Agent2Key, nil)
	rMissing, _ := env.DoJSON(t, http.MethodGet, "/api/v1/memories/"+uuid.New().String(), tA.Agent2Key, nil)
	if rForeign.StatusCode != http.StatusServiceUnavailable || rMissing.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 fail-closed for both denied (%d) and missing (%d) when audit fails", rForeign.StatusCode, rMissing.StatusCode)
	}

	// ManageTaskPermission rolls back atomically if audit fails.
	actor := identity.AuthContext{
		TenantID:        tA.TenantID,
		PrincipalID:     tA.OrchPrincipal,
		PrincipalType:   identity.PrincipalOrchestrator,
		AllowedProjects: []uuid.UUID{tA.Project1ID},
		AllowedScopes:   []identity.ScopeType{identity.ScopeTaskShared},
		IsAdmin:         true,
	}
	if err := env.ProvSvc.ManageTaskPermission(ctx, actor, provisioning.ManageTaskPermissionCommand{
		TenantID:    tA.TenantID,
		ProjectID:   tA.Project1ID,
		TaskID:      "task-rollback",
		PrincipalID: tA.Agent1Princ,
		CanRead:     true,
		CanWrite:    true,
	}); err == nil {
		t.Fatal("expected ManageTaskPermission to fail when audit sink fails")
	}
	var grantCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM task_permissions WHERE tenant_id = $1 AND task_id = 'task-rollback'`, tA.TenantID).Scan(&grantCount); err != nil {
		t.Fatalf("count task-rollback grants: %v", err)
	}
	if grantCount != 0 {
		t.Fatalf("expected ManageTaskPermission rollback on audit failure, found %d rows", grantCount)
	}
}
