package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/romero-project/romero/internal/domain/identity"
	"github.com/romero-project/romero/internal/domain/memory"
	"github.com/romero-project/romero/internal/domain/provenance"
	"github.com/romero-project/romero/internal/infrastructure/persistence/postgres"
)

func TestMemoryRepository_CRUDIsolationAndErrorPaths(t *testing.T) {
	pool := openTestPool(t)
	ctx := context.Background()
	store, err := postgres.NewStore(pool)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	tA := seedTenantProjectAgentPrincipal(t, ctx, pool, []string{"agent_private", "task_shared", "project_shared", "org_shared"})
	tB := seedTenantProjectAgentPrincipal(t, ctx, pool, []string{"project_shared"})

	now := time.Now().UTC().Truncate(time.Microsecond)
	srcID := uuid.New()
	memID := uuid.New()

	// Validation & NotFound checks outside tx
	if _, err := store.Memories().GetMemoryByID(ctx, uuid.Nil, memID); !errors.Is(err, memory.ErrInvalidIdentifier) {
		t.Fatalf("expected ErrInvalidIdentifier for nil tenant, got %v", err)
	}
	if _, err := store.Memories().GetMemoryByID(ctx, tA.tenantID, memID); !errors.Is(err, memory.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for missing memory, got %v", err)
	}
	if _, err := store.Memories().GetMemoryByIdempotencyKey(ctx, tA.tenantID, ""); !errors.Is(err, memory.ErrInvalidIdentifier) {
		t.Fatalf("expected ErrInvalidIdentifier for empty key, got %v", err)
	}
	if _, err := store.Memories().GetMemoryByIdempotencyKey(ctx, tA.tenantID, "absent"); !errors.Is(err, memory.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for absent key, got %v", err)
	}
	if _, err := store.Memories().ListVersionsByMemoryID(ctx, uuid.Nil); !errors.Is(err, memory.ErrInvalidIdentifier) {
		t.Fatalf("expected ErrInvalidIdentifier, got %v", err)
	}
	if _, err := store.Memories().GetMemoryVersionPointInTime(ctx, memID, time.Time{}, now); !errors.Is(err, memory.ErrInvalidPointInTimeQuery) {
		t.Fatalf("expected ErrInvalidPointInTimeQuery, got %v", err)
	}
	if err := store.Audit().RecordAllowed(ctx, memory.AuditEvent{Outcome: memory.AuditDenied}); !errors.Is(err, memory.ErrInvalidAuditOutcome) {
		t.Fatalf("expected ErrInvalidAuditOutcome on RecordAllowed(denied), got %v", err)
	}
	if err := store.Audit().RecordDenied(ctx, memory.AuditEvent{Outcome: memory.AuditAllowed}); !errors.Is(err, memory.ErrInvalidAuditOutcome) {
		t.Fatalf("expected ErrInvalidAuditOutcome on RecordDenied(allowed), got %v", err)
	}
	if _, err := store.NewTxTaskPermissions(nil); !errors.Is(err, postgres.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput on nil tx, got %v", err)
	}
	if err := store.ExecTx(ctx, nil); !errors.Is(err, postgres.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput on nil callback, got %v", err)
	}

	m := memory.Memory{
		ID: memID, TenantID: tA.tenantID, ProjectID: &tA.projectID, OwnerAgentID: &tA.agentID,
		Type: memory.TypeSemantic, ScopeType: identity.ScopeAgentPrivate, Status: memory.StatusActive,
		TopicKey: "agent/k", Summary: "Initial summary", Content: "Initial body",
		ContentSHA256: sha256Hex("Initial body"), TokenCount: 3, Tags: []string{"go"}, Entities: []string{"Romero"},
		EpistemicStatus: memory.EpistemicObserved, AlphaScore: 8.5, BetaScore: 1.5, Importance: 0.6, UtilityScore: 0.2,
		Version: 1, PrimarySourceID: srcID, IdempotencyKey: "k-sync-1",
		Clocks: memory.TriTemporalWindow{ObservedAt: now, CreatedAt: now, ValidFrom: now}, UpdatedAt: now,
		Metadata: map[string]any{"origin": "unit"},
	}

	err = store.ExecTx(ctx, func(reg memory.RepositoryRegistry) error {
		if err := reg.Sources().InsertSource(ctx, provenance.MemorySource{
			ID: srcID, TenantID: tA.tenantID, ProjectID: &tA.projectID,
			SourceType: provenance.SourceAgentExecution, ExternalSourceID: "ext-1", CreatedAt: now,
		}); err != nil {
			return err
		}
		if err := reg.Memories().InsertMemory(ctx, m); err != nil {
			return err
		}
		if err := reg.Memories().InsertMemoryVersion(ctx, memory.MemoryVersion{
			ID: uuid.New(), MemoryID: memID, Version: 1, Summary: m.Summary, Content: m.Content,
			EpistemicStatus: m.EpistemicStatus, AlphaScore: m.AlphaScore, BetaScore: m.BetaScore,
			Importance: m.Importance, UtilityScore: m.UtilityScore, Status: m.Status,
			ValidFrom: now, CreatedAt: now,
		}); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatalf("seed memory failed: %v", err)
	}

	// Cross-tenant GetMemoryByID must return ErrNotFound
	if _, err := store.Memories().GetMemoryByID(ctx, tB.tenantID, memID); !errors.Is(err, memory.ErrNotFound) {
		t.Fatalf("expected cross-tenant GetMemoryByID ErrNotFound, got %v", err)
	}

	got, err := store.Memories().GetMemoryByID(ctx, tA.tenantID, memID)
	if err != nil || got.Summary != "Initial summary" || got.Metadata["origin"] != "unit" {
		t.Fatalf("unexpected GetMemoryByID result: %+v err=%v", got, err)
	}
	gotByKey, err := store.Memories().GetMemoryByIdempotencyKey(ctx, tA.tenantID, "k-sync-1")
	if err != nil || gotByKey.ID != memID {
		t.Fatalf("unexpected GetMemoryByIdempotencyKey: %+v err=%v", gotByKey, err)
	}

	// UpdateMemoryState + ListVersionsByMemoryID + CloseMemoryVersionValidity error paths
	m.Summary = "Updated summary"
	m.Version = 2
	m.UpdatedAt = now.Add(time.Minute)
	if err := store.Memories().UpdateMemoryState(ctx, m); err != nil {
		t.Fatalf("UpdateMemoryState failed: %v", err)
	}
	missingM := m
	missingM.ID = uuid.New()
	if err := store.Memories().UpdateMemoryState(ctx, missingM); !errors.Is(err, memory.ErrNotFound) {
		t.Fatalf("expected ErrNotFound updating missing memory, got %v", err)
	}

	vers, err := store.Memories().ListVersionsByMemoryID(ctx, memID)
	if err != nil || len(vers) != 1 || vers[0].Version != 1 {
		t.Fatalf("unexpected ListVersionsByMemoryID: %+v err=%v", vers, err)
	}
	if err := store.Memories().CloseMemoryVersionValidity(ctx, memID, 99, now.Add(time.Minute)); !errors.Is(err, memory.ErrNotFound) {
		t.Fatalf("expected ErrNotFound closing non-existent version, got %v", err)
	}
	if _, err := store.Memories().GetMemoryVersionPointInTime(ctx, memID, now.Add(-time.Hour), now.Add(-time.Hour)); !errors.Is(err, memory.ErrNotFound) {
		t.Fatalf("expected ErrNotFound before history start, got %v", err)
	}
}

func TestReadCommittedRevocationAndLeaseRecovery(t *testing.T) {
	pool := openTestPool(t)
	ctx := context.Background()
	store, err := postgres.NewStore(pool)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	f := seedTenantProjectAgentPrincipal(t, ctx, pool, []string{"task_shared"})
	grantAudit := memory.AuditEvent{
		ID: uuid.New(), TenantID: &f.tenantID, PrincipalID: &f.principalID,
		Operation: "grant", ResourceType: "task_permission", Outcome: memory.AuditAllowed, CreatedAt: time.Now().UTC(),
	}
	if err := store.GrantOrRevokeTaskPermission(ctx, postgres.TaskPermissionGrant{
		TenantID: f.tenantID, ProjectID: f.projectID, TaskID: "task-rc", PrincipalID: f.principalID, CanRead: true, CanWrite: true,
	}, grantAudit); err != nil {
		t.Fatalf("initial grant failed: %v", err)
	}

	// Open an explicit READ COMMITTED transaction before revocation commits, and check after commit:
	// In PostgreSQL READ COMMITTED, statement inside open tx started AFTER revocation commit MUST observe revocation.
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	txPerms, err := store.NewTxTaskPermissions(tx)
	if err != nil {
		t.Fatalf("NewTxTaskPermissions: %v", err)
	}
	// Pre-revocation snapshot in open tx -> allowed
	okBefore, err := txPerms.HasTaskPermission(ctx, identity.TaskPermissionQuery{
		TenantID: f.tenantID, ProjectID: f.projectID, PrincipalID: f.principalID, TaskID: "task-rc", Permission: identity.TaskWrite,
	})
	if err != nil || !okBefore {
		t.Fatalf("expected allowed before revoke commit, got %v err=%v", okBefore, err)
	}

	// Revoke on another connection and commit
	grantAudit.ID = uuid.New()
	grantAudit.Operation = "revoke"
	if err := store.GrantOrRevokeTaskPermission(ctx, postgres.TaskPermissionGrant{
		TenantID: f.tenantID, ProjectID: f.projectID, TaskID: "task-rc", PrincipalID: f.principalID, CanRead: false, CanWrite: false,
	}, grantAudit); err != nil {
		t.Fatalf("revoke failed: %v", err)
	}

	// Re-check inside SAME still-open READ COMMITTED tx -> MUST observe committed revocation!
	okAfter, err := txPerms.HasTaskPermission(ctx, identity.TaskPermissionQuery{
		TenantID: f.tenantID, ProjectID: f.projectID, PrincipalID: f.principalID, TaskID: "task-rc", Permission: identity.TaskWrite,
	})
	if err != nil || okAfter {
		t.Fatalf("expected READ COMMITTED statement to observe committed revocation, got ok=%v err=%v", okAfter, err)
	}

	// Expired lease recovery by a second worker + permanent failure + validation checks
	queue := store.OpaqueRequests(1 * time.Second)
	rec, err := queue.Enqueue(ctx, postgres.OpaqueRequest{
		TenantID: f.tenantID, ProjectID: f.projectID, TaskID: "task-rc", PrincipalID: f.principalID,
		Operation: postgres.OpaqueOpUpdate, PayloadVersion: 1, Payload: json.RawMessage(`{"x":1}`),
		RequestSHA256: sha256Hex("opaque-without-key"),
	})
	if err != nil {
		t.Fatalf("Enqueue without idempotency key: %v", err)
	}
	rec2, err := queue.Enqueue(ctx, postgres.OpaqueRequest{
		TenantID: f.tenantID, ProjectID: f.projectID, TaskID: "task-rc", PrincipalID: f.principalID,
		Operation: postgres.OpaqueOpUpdate, PayloadVersion: 1, Payload: json.RawMessage(`{"x":1}`),
		RequestSHA256: sha256Hex("opaque-without-key"),
	})
	if err != nil || rec2.RequestID == rec.RequestID {
		t.Fatalf("expected nil-idempotency-key requests to be independent, got rec=%s rec2=%s err=%v", rec.RequestID, rec2.RequestID, err)
	}

	lease1, err := queue.Claim(ctx)
	if err != nil || lease1 == nil {
		t.Fatalf("Claim worker 1: %+v err=%v", lease1, err)
	}
	// Expire worker 1 lease in DB
	if _, err := pool.Exec(ctx, `UPDATE opaque_memory_requests SET lease_expires_at = NOW() - INTERVAL '5 seconds' WHERE request_id = $1`, lease1.RequestID); err != nil {
		t.Fatalf("expire lease1: %v", err)
	}
	// Worker 2 recovers expired lease with a NEW token
	lease2, err := queue.Claim(ctx)
	if err != nil || lease2 == nil || lease2.RequestID != lease1.RequestID || lease2.Token == lease1.Token {
		t.Fatalf("expected worker 2 recovery with new token, got l1=%+v l2=%+v err=%v", lease1, lease2, err)
	}
	// Worker 1 late permanent fail / execute must fail with ErrLeaseLost
	if err := queue.RecordFailure(ctx, *lease1, postgres.FailurePermanent, "DENIED", "stale"); !errors.Is(err, postgres.ErrLeaseLost) {
		t.Fatalf("expected ErrLeaseLost for stale worker RecordFailure, got %v", err)
	}
	if err := queue.RecordFailure(ctx, *lease2, postgres.FailurePermanent, "REVOKED", "permission revoked"); err != nil {
		t.Fatalf("worker 2 permanent RecordFailure failed: %v", err)
	}
	meta, err := queue.GetStatus(ctx, f.tenantID, lease2.RequestID)
	if err != nil || meta.Status != postgres.OpaqueStatusFailed {
		t.Fatalf("expected failed status, got %+v err=%v", meta, err)
	}
	if _, err := queue.PurgeTerminal(ctx, -time.Second); !errors.Is(err, postgres.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for negative retention, got %v", err)
	}
}

func TestEdgeGuardsAndTopicPartitionLocking(t *testing.T) {
	pool := openTestPool(t)
	ctx := context.Background()
	store, err := postgres.NewStore(pool)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	f := seedTenantProjectAgentPrincipal(t, ctx, pool, []string{"task_shared", "org_shared", "project_shared"})
	// Missing/invalid permission queries
	if snap, err := store.TaskPermissions().EvaluateTaskAccess(ctx, identity.TaskPermissionQuery{}); err != nil || snap.CanRead || snap.CanWrite {
		t.Fatalf("expected denied nil query, got %+v err=%v", snap, err)
	}
	if ok, err := store.TaskPermissions().HasTaskPermission(ctx, identity.TaskPermissionQuery{
		TenantID: f.tenantID, ProjectID: f.projectID, PrincipalID: f.principalID, TaskID: "t1", Permission: identity.TaskPermission("unknown"),
	}); err != nil || ok {
		t.Fatalf("expected false for unknown permission, got %v err=%v", ok, err)
	}
	// Expired principal evaluates as denied (ErrNoRows -> false, nil)
	if _, err := pool.Exec(ctx, `UPDATE principals SET expires_at = NOW() - INTERVAL '1 minute' WHERE id = $1`, f.principalID); err != nil {
		t.Fatalf("expire principal: %v", err)
	}
	if ok, err := store.TaskPermissions().HasTaskPermission(ctx, identity.TaskPermissionQuery{
		TenantID: f.tenantID, ProjectID: f.projectID, PrincipalID: f.principalID, TaskID: "t1", Permission: identity.TaskRead,
	}); err != nil || ok {
		t.Fatalf("expected false, nil for expired principal, got %v err=%v", ok, err)
	}
	// Invalid GrantOrRevokeTaskPermission params
	if err := store.GrantOrRevokeTaskPermission(ctx, postgres.TaskPermissionGrant{}, memory.AuditEvent{}); !errors.Is(err, postgres.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput, got %v", err)
	}
	if err := store.GrantOrRevokeTaskPermission(ctx, postgres.TaskPermissionGrant{
		TenantID: f.tenantID, ProjectID: f.projectID, TaskID: "t1", PrincipalID: f.principalID,
	}, memory.AuditEvent{
		ID: uuid.New(), Operation: "rev", ResourceType: "tp", Outcome: memory.AuditDenied, CreatedAt: time.Now().UTC(),
	}); !errors.Is(err, memory.ErrInvalidAuditOutcome) {
		t.Fatalf("expected ErrInvalidAuditOutcome, got %v", err)
	}

	// Enqueue/GetStatus/Renew/Execute/RecordFailure validation guards
	queue := store.OpaqueRequests(0)
	if _, err := queue.Enqueue(ctx, postgres.OpaqueRequest{}); !errors.Is(err, postgres.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for empty enqueue, got %v", err)
	}
	if _, err := queue.Enqueue(ctx, postgres.OpaqueRequest{
		TenantID: f.tenantID, ProjectID: f.projectID, TaskID: "t1", PrincipalID: f.principalID, Operation: "invalid",
	}); !errors.Is(err, postgres.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for invalid op, got %v", err)
	}
	if _, err := queue.Enqueue(ctx, postgres.OpaqueRequest{
		TenantID: f.tenantID, ProjectID: f.projectID, TaskID: "t1", PrincipalID: f.principalID, Operation: postgres.OpaqueOpCapture, RequestSHA256: "short",
	}); !errors.Is(err, postgres.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for bad sha, got %v", err)
	}
	if _, err := queue.GetStatus(ctx, uuid.Nil, uuid.New()); !errors.Is(err, memory.ErrInvalidIdentifier) {
		t.Fatalf("expected ErrInvalidIdentifier on nil tenant, got %v", err)
	}
	if _, err := queue.GetStatus(ctx, f.tenantID, uuid.New()); !errors.Is(err, memory.ErrNotFound) {
		t.Fatalf("expected ErrNotFound on absent request, got %v", err)
	}
	if ok, err := queue.Renew(ctx, postgres.Lease{}); !errors.Is(err, postgres.ErrLeaseLost) || ok {
		t.Fatalf("expected ErrLeaseLost on nil lease Renew, got %v err=%v", ok, err)
	}
	if err := queue.Execute(ctx, postgres.Lease{}, nil); !errors.Is(err, postgres.ErrLeaseLost) {
		t.Fatalf("expected ErrLeaseLost on nil Execute, got %v", err)
	}
	if err := queue.RecordFailure(ctx, postgres.Lease{}, postgres.FailureTransient, "C", "M"); !errors.Is(err, postgres.ErrLeaseLost) {
		t.Fatalf("expected ErrLeaseLost on nil RecordFailure, got %v", err)
	}

	// LockActiveTopicMemory ErrNotFound + ErrInvalidIdentifier + NLI score relation mapping
	if _, err := store.Memories().LockActiveTopicMemory(ctx, identity.MemoryScopeTarget{}, ""); !errors.Is(err, memory.ErrInvalidIdentifier) {
		t.Fatalf("expected ErrInvalidIdentifier, got %v", err)
	}
	if _, err := store.Memories().LockActiveTopicMemory(ctx, identity.MemoryScopeTarget{
		TenantID: f.tenantID, ProjectID: &f.projectID, ScopeType: identity.ScopeTaskShared, TaskID: "t-missing",
	}, "no-such-topic"); !errors.Is(err, memory.ErrNotFound) {
		t.Fatalf("expected ErrNotFound locking missing topic, got %v", err)
	}
}

func TestTopicPartitionLocking_ScopesReadCommittedAndZeroRetry(t *testing.T) {
	pool := openTestPool(t)
	ctx := context.Background()
	store, err := postgres.NewStore(pool)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	f := seedTenantProjectAgentPrincipal(t, ctx, pool, []string{"agent_private", "task_shared", "project_shared", "org_shared", "global_public"})
	p2ID := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO projects (id, tenant_id, slug, name) VALUES ($1, $2, $3, $4)`, p2ID, f.tenantID, "p2-"+p2ID.String()[:8], "Project Two"); err != nil {
		t.Fatalf("insert project 2: %v", err)
	}
	agentBID := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO agents (id, tenant_id, external_key, name) VALUES ($1, $2, $3, 'Agent-B')`, agentBID, f.tenantID, "agent-b-"+agentBID.String()[:8]); err != nil {
		t.Fatalf("insert agent B: %v", err)
	}

	srcID := uuid.New()
	now := time.Now().UTC().Truncate(time.Microsecond)
	const sharedTopic = "shared/topic"

	makeMem := func(scope identity.ScopeType, projectID *uuid.UUID, ownerAgentID *uuid.UUID, taskID string) memory.Memory {
		return memory.Memory{
			ID: uuid.New(), TenantID: f.tenantID, ProjectID: projectID, OwnerAgentID: ownerAgentID, TaskID: taskID,
			Type: memory.TypeSemantic, ScopeType: scope, Status: memory.StatusActive,
			TopicKey: sharedTopic, Summary: string(scope), Content: "content-" + string(scope),
			ContentSHA256: sha256Hex("content-" + string(scope)), TokenCount: 2,
			EpistemicStatus: memory.EpistemicObserved, AlphaScore: 8.5, BetaScore: 1.5, Importance: 0.6,
			Version: 1, PrimarySourceID: srcID,
			Clocks:    memory.TriTemporalWindow{ObservedAt: now, CreatedAt: now, ValidFrom: now},
			UpdatedAt: now,
		}
	}

	mAgentA := makeMem(identity.ScopeAgentPrivate, &f.projectID, &f.agentID, "")
	mAgentB := makeMem(identity.ScopeAgentPrivate, &f.projectID, &agentBID, "")
	mTask1 := makeMem(identity.ScopeTaskShared, &f.projectID, nil, "task-1")
	mTask2 := makeMem(identity.ScopeTaskShared, &f.projectID, nil, "task-2")
	mProj1 := makeMem(identity.ScopeProjectShared, &f.projectID, nil, "")
	mProj2 := makeMem(identity.ScopeProjectShared, &p2ID, nil, "")
	mOrg := makeMem(identity.ScopeOrgShared, &f.projectID, nil, "")
	mGlobal := makeMem(identity.ScopeGlobalPublic, &f.projectID, nil, "")

	if err := store.ExecTx(ctx, func(reg memory.RepositoryRegistry) error {
		if err := reg.Sources().InsertSource(ctx, provenance.MemorySource{
			ID: srcID, TenantID: f.tenantID, ProjectID: &f.projectID,
			SourceType: provenance.SourceUserInput, ExternalSourceID: "scope-seed", CreatedAt: now,
		}); err != nil {
			return err
		}
		for _, m := range []memory.Memory{mAgentA, mAgentB, mTask1, mTask2, mProj1, mProj2, mOrg, mGlobal} {
			if err := reg.Memories().InsertMemory(ctx, m); err != nil {
				return fmt.Errorf("insert scope %s: %w", m.ScopeType, err)
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("seed competing topic_key memories: %v", err)
	}

	assertLocked := func(name string, target identity.MemoryScopeTarget, wantID uuid.UUID) {
		t.Helper()
		err := store.ExecTx(ctx, func(reg memory.RepositoryRegistry) error {
			got, err := reg.Memories().LockActiveTopicMemory(ctx, target, sharedTopic)
			if err != nil {
				return err
			}
			if got.ID != wantID {
				return fmt.Errorf("%s: got %s, want %s", name, got.ID, wantID)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("LockActiveTopicMemory(%s): %v", name, err)
		}
	}

	assertLocked("agent_private/A", identity.MemoryScopeTarget{TenantID: f.tenantID, ProjectID: &f.projectID, OwnerAgentID: &f.agentID, ScopeType: identity.ScopeAgentPrivate}, mAgentA.ID)
	assertLocked("agent_private/B", identity.MemoryScopeTarget{TenantID: f.tenantID, ProjectID: &f.projectID, OwnerAgentID: &agentBID, ScopeType: identity.ScopeAgentPrivate}, mAgentB.ID)
	assertLocked("task_shared/1", identity.MemoryScopeTarget{TenantID: f.tenantID, ProjectID: &f.projectID, ScopeType: identity.ScopeTaskShared, TaskID: "task-1"}, mTask1.ID)
	assertLocked("task_shared/2", identity.MemoryScopeTarget{TenantID: f.tenantID, ProjectID: &f.projectID, ScopeType: identity.ScopeTaskShared, TaskID: "task-2"}, mTask2.ID)
	assertLocked("project_shared/1", identity.MemoryScopeTarget{TenantID: f.tenantID, ProjectID: &f.projectID, ScopeType: identity.ScopeProjectShared}, mProj1.ID)
	assertLocked("project_shared/2", identity.MemoryScopeTarget{TenantID: f.tenantID, ProjectID: &p2ID, ScopeType: identity.ScopeProjectShared}, mProj2.ID)
	assertLocked("org_shared/via_p2", identity.MemoryScopeTarget{TenantID: f.tenantID, ProjectID: &p2ID, ScopeType: identity.ScopeOrgShared}, mOrg.ID)
	assertLocked("org_shared/via_nil", identity.MemoryScopeTarget{TenantID: f.tenantID, ProjectID: nil, ScopeType: identity.ScopeOrgShared}, mOrg.ID)
	assertLocked("global_public/via_p2", identity.MemoryScopeTarget{TenantID: f.tenantID, ProjectID: &p2ID, ScopeType: identity.ScopeGlobalPublic}, mGlobal.ID)
	assertLocked("global_public/via_nil", identity.MemoryScopeTarget{TenantID: f.tenantID, ProjectID: nil, ScopeType: identity.ScopeGlobalPublic}, mGlobal.ID)

	// READ COMMITTED visibility directly inside store.ExecTx: committed concurrent update becomes visible to open tx.
	if err := store.ExecTx(ctx, func(reg memory.RepositoryRegistry) error {
		before, err := reg.Memories().GetMemoryByID(ctx, f.tenantID, mOrg.ID)
		if err != nil {
			return err
		}
		if before.Version != 1 {
			return fmt.Errorf("expected version 1 before concurrent commit, got %d", before.Version)
		}
		if err := store.ExecTx(ctx, func(ext memory.RepositoryRegistry) error {
			updated := *before
			updated.Version = 2
			updated.Summary = "updated-concurrently"
			updated.UpdatedAt = now.Add(time.Minute)
			return ext.Memories().UpdateMemoryState(ctx, updated)
		}); err != nil {
			return fmt.Errorf("external UpdateMemoryState: %w", err)
		}
		after, err := reg.Memories().GetMemoryByID(ctx, f.tenantID, mOrg.ID)
		if err != nil {
			return err
		}
		if after.Version != 2 || after.Summary != "updated-concurrently" {
			return fmt.Errorf("expected open ExecTx READ COMMITTED to observe version 2, got v%d (%q)", after.Version, after.Summary)
		}
		return nil
	}); err != nil {
		t.Fatalf("ExecTx READ COMMITTED visibility test failed: %v", err)
	}

	// READ COMMITTED permission revocation directly inside OpaqueRequestStore.Execute.
	auditGrant := memory.AuditEvent{
		ID: uuid.New(), TenantID: &f.tenantID, PrincipalID: &f.principalID,
		Operation: "grant", ResourceType: "task_permission", Outcome: memory.AuditAllowed, CreatedAt: now,
	}
	if err := store.GrantOrRevokeTaskPermission(ctx, postgres.TaskPermissionGrant{
		TenantID: f.tenantID, ProjectID: f.projectID, TaskID: "task-exec-rc", PrincipalID: f.principalID, CanRead: true, CanWrite: true,
	}, auditGrant); err != nil {
		t.Fatalf("grant task-exec-rc: %v", err)
	}

	queue := store.OpaqueRequests(30 * time.Second)
	execRec, err := queue.Enqueue(ctx, postgres.OpaqueRequest{
		TenantID: f.tenantID, ProjectID: f.projectID, TaskID: "task-exec-rc", PrincipalID: f.principalID,
		Operation: postgres.OpaqueOpCapture, PayloadVersion: 1, Payload: json.RawMessage(`{"k":1}`),
		RequestSHA256: sha256Hex("exec-rc"), IdempotencyKey: "exec-rc-key",
	})
	if err != nil {
		t.Fatalf("Enqueue exec-rc: %v", err)
	}
	execLease, err := queue.Claim(ctx)
	if err != nil || execLease == nil || execLease.RequestID != execRec.RequestID {
		t.Fatalf("Claim exec-rc: lease=%+v err=%v", execLease, err)
	}

	errRevokedInTx := errors.New("revoked during execute")
	err = queue.Execute(ctx, *execLease, func(ctx context.Context, reg memory.RepositoryRegistry, perms identity.TaskPermissionReader, req postgres.OpaqueRequestRecord) (*uuid.UUID, error) {
		okBefore, err := perms.HasTaskPermission(ctx, identity.TaskPermissionQuery{
			TenantID: req.TenantID, ProjectID: req.ProjectID, PrincipalID: req.PrincipalID, TaskID: req.TaskID, Permission: identity.TaskWrite,
		})
		if err != nil || !okBefore {
			return nil, fmt.Errorf("expected allowed before external revoke, got ok=%v err=%w", okBefore, err)
		}
		revokeAudit := auditGrant
		revokeAudit.ID = uuid.New()
		revokeAudit.Operation = "revoke"
		if err := store.GrantOrRevokeTaskPermission(ctx, postgres.TaskPermissionGrant{
			TenantID: req.TenantID, ProjectID: req.ProjectID, TaskID: req.TaskID, PrincipalID: req.PrincipalID, CanRead: false, CanWrite: false,
		}, revokeAudit); err != nil {
			return nil, err
		}
		okAfter, err := perms.HasTaskPermission(ctx, identity.TaskPermissionQuery{
			TenantID: req.TenantID, ProjectID: req.ProjectID, PrincipalID: req.PrincipalID, TaskID: req.TaskID, Permission: identity.TaskWrite,
		})
		if err != nil {
			return nil, err
		}
		if okAfter {
			return nil, errors.New("expected READ COMMITTED Execute tx to observe committed revocation")
		}
		return nil, errRevokedInTx
	})
	if !errors.Is(err, errRevokedInTx) {
		t.Fatalf("expected Execute to return errRevokedInTx after mid-tx revocation check, got %v", err)
	}

	// Zero-retry OpaqueRequest fails permanently on the first transient RecordFailure.
	zero := int32(0)
	zeroRec, err := queue.Enqueue(ctx, postgres.OpaqueRequest{
		TenantID: f.tenantID, ProjectID: f.projectID, TaskID: "task-exec-rc", PrincipalID: f.principalID,
		Operation: postgres.OpaqueOpCapture, PayloadVersion: 1, Payload: json.RawMessage(`{"zero":true}`),
		RequestSHA256: sha256Hex("zero-retry"), IdempotencyKey: "zero-retry-key", MaxRetries: &zero,
	})
	if err != nil {
		t.Fatalf("Enqueue zero retry: %v", err)
	}
	// Mark execRec failed permanently first so queue.Claim picks zeroRec cleanly.
	if err := queue.RecordFailure(ctx, *execLease, postgres.FailurePermanent, "REVOKED", "aborted by test"); err != nil {
		t.Fatalf("fail execRec cleanly: %v", err)
	}
	zeroLease, err := queue.Claim(ctx)
	if err != nil || zeroLease == nil || zeroLease.RequestID != zeroRec.RequestID {
		t.Fatalf("Claim zeroRec: lease=%+v err=%v", zeroLease, err)
	}
	if err := queue.RecordFailure(ctx, *zeroLease, postgres.FailureTransient, "TRANSIENT_ZERO", "fail fast"); err != nil {
		t.Fatalf("RecordFailure zero retry: %v", err)
	}
	zeroMeta, err := queue.GetStatus(ctx, f.tenantID, zeroRec.RequestID)
	if err != nil || zeroMeta.Status != postgres.OpaqueStatusFailed {
		t.Fatalf("expected terminal failed immediately for MaxRetries=0, got %+v err=%v", zeroMeta, err)
	}
}
