package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/romero-project/romero/internal/application/provisioning"
	"github.com/romero-project/romero/internal/application/requests"
	"github.com/romero-project/romero/internal/domain/identity"
	"github.com/romero-project/romero/tests/internal/testpg"
)

func TestTaskSharedAuthorizationMatrix(t *testing.T) {
	pool := testpg.OpenMigratedPool(t)
	env := testpg.NewEnv(t, pool)
	ctx := context.Background()

	type tenantSetup struct {
		boot    bootstrapCLIOut
		proj2ID uuid.UUID
		reader  principalCLIOut
		writer  principalCLIOut
		seedMem uuid.UUID
	}
	setupTenant := func(slug string) tenantSetup {
		boot := bootstrapTenant(t, env, slug)
		var proj2ID uuid.UUID
		if err := pool.QueryRow(ctx, `INSERT INTO projects (tenant_id, slug, name) VALUES ($1, $2, $3) RETURNING id`, boot.Tenant.ID, "p2-"+slug, "P2 "+slug).Scan(&proj2ID); err != nil {
			t.Fatalf("insert proj2: %v", err)
		}
		// Grant orchestrator read+write on task-A, then seed a task_shared memory.
		if _, err := env.RunCLI(t, "task-permission", "grant", "--dsn", testpg.TestDSN(), "--api-key", boot.APIKey, "--tenant-id", boot.Tenant.ID.String(), "--project-id", boot.Project.ID.String(), "--task-id", "task-A", "--principal-id", boot.Principal.ID.String(), "--read", "--write"); err != nil {
			t.Fatalf("grant orchestrator task-A: %v", err)
		}
		seedBody := capturePayload(boot.Project.ID, "task_shared", "", "Seed task-A memory for "+slug, "reported", "user_input")
		seedBody["task_id"] = "task-A"
		resp, raw := env.DoJSON(t, http.MethodPost, "/api/v1/memories", boot.APIKey, seedBody)
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("seed task_shared failed (%d): %s", resp.StatusCode, string(raw))
		}
		var out struct {
			Memory struct {
				ID uuid.UUID `json:"id"`
			} `json:"memory"`
		}
		_ = json.Unmarshal(raw, &out)

		reader := createAgentPrincipal(t, env, boot.Tenant.ID, boot.Project.ID, uuid.New(), "reader-"+slug, "task_shared")
		writer := createAgentPrincipal(t, env, boot.Tenant.ID, boot.Project.ID, uuid.New(), "writer-"+slug, "task_shared")
		return tenantSetup{boot: boot, proj2ID: proj2ID, reader: reader, writer: writer, seedMem: out.Memory.ID}
	}

	t1 := setupTenant("t1")
	t2 := setupTenant("t2")

	// 4 permission combinations on t1.Project.ID / task-A:
	if _, err := env.RunCLI(t, "task-permission", "grant", "--dsn", testpg.TestDSN(), "--api-key", t1.boot.APIKey, "--tenant-id", t1.boot.Tenant.ID.String(), "--project-id", t1.boot.Project.ID.String(), "--task-id", "task-A", "--principal-id", t1.reader.Principal.ID.String(), "--read"); err != nil {
		t.Fatalf("grant read-only: %v", err)
	}
	if _, err := env.RunCLI(t, "task-permission", "grant", "--dsn", testpg.TestDSN(), "--api-key", t1.boot.APIKey, "--tenant-id", t1.boot.Tenant.ID.String(), "--project-id", t1.boot.Project.ID.String(), "--task-id", "task-A", "--principal-id", t1.writer.Principal.ID.String(), "--write"); err != nil {
		t.Fatalf("grant write-only: %v", err)
	}

	// 1. None: t2 principals get 404 on read and 403 on write.
	for _, key := range []string{t2.reader.APIKey, t2.writer.APIKey} {
		r, _ := env.DoJSON(t, http.MethodGet, "/api/v1/memories/"+t1.seedMem.String(), key, nil)
		if r.StatusCode != http.StatusNotFound {
			t.Fatalf("expected 404 for cross-tenant task_shared GET, got %d", r.StatusCode)
		}
		wBody := capturePayload(t1.boot.Project.ID, "task_shared", "", "cross tenant write", "inferred", "agent_execution")
		wBody["task_id"] = "task-A"
		w, _ := env.DoJSON(t, http.MethodPost, "/api/v1/memories", key, wBody)
		if w.StatusCode != http.StatusForbidden {
			t.Fatalf("expected 403 for cross-tenant task_shared POST, got %d", w.StatusCode)
		}
	}

	// 2. Read-only: GET -> 200, POST -> 403.
	rRO, _ := env.DoJSON(t, http.MethodGet, "/api/v1/memories/"+t1.seedMem.String(), t1.reader.APIKey, nil)
	if rRO.StatusCode != http.StatusOK {
		t.Fatalf("read-only GET expected 200, got %d", rRO.StatusCode)
	}
	wROBody := capturePayload(t1.boot.Project.ID, "task_shared", "", "read-only write attempt", "inferred", "agent_execution")
	wROBody["task_id"] = "task-A"
	wRO, _ := env.DoJSON(t, http.MethodPost, "/api/v1/memories", t1.reader.APIKey, wROBody)
	if wRO.StatusCode != http.StatusForbidden {
		t.Fatalf("read-only POST expected 403, got %d", wRO.StatusCode)
	}

	// 3. Write-only: GET -> 404, POST -> 202 Accepted (opaque queue).
	rWO, _ := env.DoJSON(t, http.MethodGet, "/api/v1/memories/"+t1.seedMem.String(), t1.writer.APIKey, nil)
	if rWO.StatusCode != http.StatusNotFound {
		t.Fatalf("write-only GET expected 404, got %d", rWO.StatusCode)
	}
	wWOBody := capturePayload(t1.boot.Project.ID, "task_shared", "", "write-only opaque capture", "inferred", "agent_execution")
	wWOBody["task_id"] = "task-A"
	wWO, wWORaw := env.DoJSON(t, http.MethodPost, "/api/v1/memories", t1.writer.APIKey, wWOBody)
	if wWO.StatusCode != http.StatusAccepted {
		t.Fatalf("write-only POST expected 202 Accepted, got %d: %s", wWO.StatusCode, string(wWORaw))
	}

	// 4. Different task-B or proj2: denied cleanly.
	wOtherTaskBody := capturePayload(t1.boot.Project.ID, "task_shared", "", "wrong task", "inferred", "agent_execution")
	wOtherTaskBody["task_id"] = "task-B"
	wOtherTask, _ := env.DoJSON(t, http.MethodPost, "/api/v1/memories", t1.writer.APIKey, wOtherTaskBody)
	if wOtherTask.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 on task-B, got %d", wOtherTask.StatusCode)
	}
	wOtherProjBody := capturePayload(t1.proj2ID, "task_shared", "", "wrong project", "inferred", "agent_execution")
	wOtherProjBody["task_id"] = "task-A"
	wOtherProj, _ := env.DoJSON(t, http.MethodPost, "/api/v1/memories", t1.writer.APIKey, wOtherProjBody)
	if wOtherProj.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 on proj2, got %d", wOtherProj.StatusCode)
	}
}

func TestTaskRevocationConcurrencyBarrier(t *testing.T) {
	pool := testpg.OpenMigratedPool(t)
	env := testpg.NewEnv(t, pool)
	ctx := context.Background()

	boot := bootstrapTenant(t, env, "barrier")
	agent := createAgentPrincipal(t, env, boot.Tenant.ID, boot.Project.ID, uuid.New(), "ag-barrier", "task_shared")
	if _, err := env.RunCLI(t, "task-permission", "grant", "--dsn", testpg.TestDSN(), "--api-key", boot.APIKey, "--tenant-id", boot.Tenant.ID.String(), "--project-id", boot.Project.ID.String(), "--task-id", "task-barrier", "--principal-id", agent.Principal.ID.String(), "--read", "--write"); err != nil {
		t.Fatalf("grant: %v", err)
	}

	c1, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire c1: %v", err)
	}
	defer c1.Release()

	tx1, err := c1.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx1: %v", err)
	}
	defer func() { _ = tx1.Rollback(ctx) }()

	var canRead, canWrite bool
	if err := tx1.QueryRow(ctx, `SELECT can_read, can_write FROM task_permissions WHERE tenant_id = $1 AND project_id = $2 AND task_id = 'task-barrier' AND principal_id = $3`, boot.Tenant.ID, boot.Project.ID, agent.Principal.ID).Scan(&canRead, &canWrite); err != nil {
		t.Fatalf("tx1 pre-revoke select: %v", err)
	}
	if !canRead || !canWrite {
		t.Fatalf("expected read+write before revoke, got read=%v write=%v", canRead, canWrite)
	}

	actor := identity.AuthContext{
		TenantID:        boot.Tenant.ID,
		PrincipalID:     boot.Principal.ID,
		PrincipalType:   identity.PrincipalOrchestrator,
		AllowedProjects: []uuid.UUID{boot.Project.ID},
		AllowedScopes:   []identity.ScopeType{identity.ScopeTaskShared},
		IsAdmin:         true,
	}
	if err := env.ProvSvc.ManageTaskPermission(ctx, actor, provisioning.ManageTaskPermissionCommand{
		TenantID:    boot.Tenant.ID,
		ProjectID:   boot.Project.ID,
		TaskID:      "task-barrier",
		PrincipalID: agent.Principal.ID,
		CanRead:     false,
		CanWrite:    false,
	}); err != nil {
		t.Fatalf("ManageTaskPermission revoke: %v", err)
	}

	var activeAfter int
	if err := tx1.QueryRow(ctx, `SELECT count(*) FROM task_permissions WHERE tenant_id = $1 AND project_id = $2 AND task_id = 'task-barrier' AND principal_id = $3 AND (can_read OR can_write)`, boot.Tenant.ID, boot.Project.ID, agent.Principal.ID).Scan(&activeAfter); err != nil {
		t.Fatalf("tx1 post-revoke select: %v", err)
	}
	if activeAfter != 0 {
		t.Fatalf("expected 0 active grants after committed revoke under READ COMMITTED, got %d", activeAfter)
	}
}

func TestOpaqueQueueAndDurableWorkerLifecycle(t *testing.T) {
	pool := testpg.OpenMigratedPool(t)
	env := testpg.NewEnv(t, pool)
	ctx := context.Background()

	boot := bootstrapTenant(t, env, "opaque")
	writer := createAgentPrincipal(t, env, boot.Tenant.ID, boot.Project.ID, uuid.New(), "wo-agent", "task_shared")
	if _, err := env.RunCLI(t, "task-permission", "grant", "--dsn", testpg.TestDSN(), "--api-key", boot.APIKey, "--tenant-id", boot.Tenant.ID.String(), "--project-id", boot.Project.ID.String(), "--task-id", "task-op", "--principal-id", writer.Principal.ID.String(), "--write"); err != nil {
		t.Fatalf("grant write-only: %v", err)
	}

	// Grant orchestrator read+write on task-op, then seed an active memory.
	if _, err := env.RunCLI(t, "task-permission", "grant", "--dsn", testpg.TestDSN(), "--api-key", boot.APIKey, "--tenant-id", boot.Tenant.ID.String(), "--project-id", boot.Project.ID.String(), "--task-id", "task-op", "--principal-id", boot.Principal.ID.String(), "--read", "--write"); err != nil {
		t.Fatalf("grant orchestrator task-op: %v", err)
	}
	seedBody := capturePayload(boot.Project.ID, "task_shared", "", "Active target", "reported", "user_input")
	seedBody["task_id"] = "task-op"
	rSeed, rawSeed := env.DoJSON(t, http.MethodPost, "/api/v1/memories", boot.APIKey, seedBody)
	if rSeed.StatusCode != http.StatusCreated {
		t.Fatalf("seed failed: %s", string(rawSeed))
	}
	var seedOut struct {
		Memory struct {
			ID uuid.UUID `json:"id"`
		} `json:"memory"`
	}
	_ = json.Unmarshal(rawSeed, &seedOut)

	// Write-only caller gets identical 202 Accepted for existing target AND non-existent target.
	missingID := uuid.New()
	rExist, rawExist := env.DoJSON(t, http.MethodPatch, "/api/v1/memories/"+seedOut.Memory.ID.String(), writer.APIKey, map[string]any{
		"task_id": "task-op",
		"summary": "Updated summary",
		"content": "Updated by write-only",
	})
	rMiss, rawMiss := env.DoJSON(t, http.MethodPatch, "/api/v1/memories/"+missingID.String(), writer.APIKey, map[string]any{
		"task_id": "task-op",
		"summary": "Missing summary",
		"content": "Updated missing target",
	})
	if rExist.StatusCode != http.StatusAccepted || rMiss.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202 for both existing (%d: %s) and missing (%d: %s) target", rExist.StatusCode, string(rawExist), rMiss.StatusCode, string(rawMiss))
	}
	var recExist, recMiss struct {
		RequestID uuid.UUID `json:"request_id"`
	}
	_ = json.Unmarshal(rawExist, &recExist)
	_ = json.Unmarshal(rawMiss, &recMiss)

	// Status endpoint: writer without read gets 404; orchestrator with read gets 200 {request_id, status}.
	rStatDenied, _ := env.DoJSON(t, http.MethodGet, "/api/v1/memory-requests/"+recExist.RequestID.String(), writer.APIKey, nil)
	if rStatDenied.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 on request status without read grant, got %d", rStatDenied.StatusCode)
	}
	rStatOK, rawStatOK := env.DoJSON(t, http.MethodGet, "/api/v1/memory-requests/"+recExist.RequestID.String(), boot.APIKey, nil)
	if rStatOK.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on request status for orchestrator, got %d: %s", rStatOK.StatusCode, string(rawStatOK))
	}

	// Competing claims via FOR UPDATE SKIP LOCKED: two claims claim distinct requests, third returns nil.
	l1, err := env.Queue.Claim(ctx)
	if err != nil || l1 == nil {
		t.Fatalf("claim 1 failed: %v, lease=%v", err, l1)
	}
	l2, err := env.Queue.Claim(ctx)
	if err != nil || l2 == nil {
		t.Fatalf("claim 2 failed: %v, lease=%v", err, l2)
	}
	if l1.RequestID == l2.RequestID {
		t.Fatalf("expected distinct requests claimed via SKIP LOCKED, both got %s", l1.RequestID)
	}
	l3, err := env.Queue.Claim(ctx)
	if err != nil || l3 != nil {
		t.Fatalf("expected nil on empty queue claim 3, got lease=%v err=%v", l3, err)
	}

	// Lease renewal succeeds for valid token, stale token fails with ErrLeaseLost.
	renewed, err := env.Queue.Renew(ctx, *l1)
	if err != nil || !renewed {
		t.Fatalf("expected Renew=true, got %v err=%v", renewed, err)
	}
	stale := requests.Lease{RequestID: l1.RequestID, Token: uuid.New(), ExpiresAt: l1.ExpiresAt}
	if err := env.Queue.RecordFailure(ctx, stale, requests.FailureTransient, "t", "stale"); !errors.Is(err, requests.ErrLeaseLost) {
		t.Fatalf("expected ErrLeaseLost on stale token, got %v", err)
	}

	// Reset leases to pending so Worker.ProcessOne runs both end-to-end.
	if _, err := pool.Exec(ctx, `UPDATE opaque_memory_requests SET status = 'pending', lease_token = NULL, lease_expires_at = NULL, next_attempt_at = now() - interval '1 second'`); err != nil {
		t.Fatalf("reset leases: %v", err)
	}
	if processed, err := env.Worker.ProcessOne(ctx); err != nil || !processed {
		t.Fatalf("ProcessOne 1: processed=%v err=%v", processed, err)
	}
	if processed, err := env.Worker.ProcessOne(ctx); err != nil || !processed {
		t.Fatalf("ProcessOne 2: processed=%v err=%v", processed, err)
	}

	// Existing target request -> succeeded; missing target request -> failed (permanent).
	var stExist, stMiss string
	if err := pool.QueryRow(ctx, `SELECT status::text FROM opaque_memory_requests WHERE request_id = $1`, recExist.RequestID).Scan(&stExist); err != nil {
		t.Fatalf("query stExist (%s): %v", recExist.RequestID, err)
	}
	if err := pool.QueryRow(ctx, `SELECT status::text FROM opaque_memory_requests WHERE request_id = $1`, recMiss.RequestID).Scan(&stMiss); err != nil {
		t.Fatalf("query stMiss (%s): %v", recMiss.RequestID, err)
	}
	if stExist != "completed" || stMiss != "failed" {
		t.Fatalf("expected exist=completed miss=failed, got exist=%q miss=%q", stExist, stMiss)
	}

	// Grant read post-submit -> writer can now read request status (200).
	if _, err := env.RunCLI(t, "task-permission", "grant", "--dsn", testpg.TestDSN(), "--api-key", boot.APIKey, "--tenant-id", boot.Tenant.ID.String(), "--project-id", boot.Project.ID.String(), "--task-id", "task-op", "--principal-id", writer.Principal.ID.String(), "--read", "--write"); err != nil {
		t.Fatalf("grant read post-submit: %v", err)
	}
	rStatPost, _ := env.DoJSON(t, http.MethodGet, "/api/v1/memory-requests/"+recExist.RequestID.String(), writer.APIKey, nil)
	if rStatPost.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 after post-submit read grant, got %d", rStatPost.StatusCode)
	}

	// 30d purge deletes old terminal rows while preserving recent/pending rows.
	if _, err := pool.Exec(ctx, `UPDATE opaque_memory_requests SET completed_at = now() - interval '31 days', updated_at = now() - interval '31 days' WHERE request_id = $1`, recExist.RequestID); err != nil {
		t.Fatalf("age terminal row: %v", err)
	}
	purged, err := env.Queue.PurgeTerminal(ctx, 30*24*time.Hour)
	if err != nil || purged != 1 {
		t.Fatalf("expected 1 purged row >30d, got purged=%d err=%v", purged, err)
	}
	var remaining int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM opaque_memory_requests WHERE request_id = $1`, recMiss.RequestID).Scan(&remaining); err != nil {
		t.Fatalf("count remaining: %v", err)
	}
	if remaining != 1 {
		t.Fatalf("expected recent terminal row preserved, got count=%d", remaining)
	}
}
