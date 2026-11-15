package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
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
