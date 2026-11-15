package postgres_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/romero-project/romero/internal/domain/identity"
	"github.com/romero-project/romero/internal/domain/memory"
	"github.com/romero-project/romero/internal/domain/provenance"
	"github.com/romero-project/romero/internal/infrastructure/persistence/postgres"
	"github.com/romero-project/romero/migrations"
)

const defaultTestDSN = "postgres://romero:romero_local_secret@romero-t004-pg:5432/romero?sslmode=disable"

func testDSN() string {
	if v := strings.TrimSpace(os.Getenv("ROMERO_TEST_DSN")); v != "" {
		return v
	}
	return defaultTestDSN
}

func openTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, testDSN())
	if err != nil {
		t.Fatalf("pgxpool.New failed: %v", err)
	}
	t.Cleanup(pool.Close)

	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("postgres ping failed (%s): %v", testDSN(), err)
	}
	ensureCleanMigratedDB(t, ctx, pool)
	return pool
}

func ensureCleanMigratedDB(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	sqlDB := stdlib.OpenDBFromPool(pool)
	defer func() { _ = sqlDB.Close() }()

	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatalf("goose.SetDialect failed: %v", err)
	}
	if err := goose.UpContext(ctx, sqlDB, "."); err != nil {
		t.Fatalf("goose.UpContext failed: %v", err)
	}

	_, err := pool.Exec(ctx, `TRUNCATE TABLE
		opaque_memory_requests,
		task_permissions,
		audit_events,
		memory_jobs,
		memory_feedback,
		memory_relations,
		memory_embeddings,
		memory_versions,
		memories,
		memory_sources,
		principals,
		agents,
		projects,
		tenants
		CASCADE`)
	if err != nil {
		t.Fatalf("truncate fixtures failed: %v", err)
	}
}

type fixtureIDs struct {
	tenantID    uuid.UUID
	projectID   uuid.UUID
	agentID     uuid.UUID
	principalID uuid.UUID
}

func seedTenantProjectAgentPrincipal(t *testing.T, ctx context.Context, pool *pgxpool.Pool, scopes []string) fixtureIDs {
	t.Helper()
	ids := fixtureIDs{
		tenantID:    uuid.New(),
		projectID:   uuid.New(),
		agentID:     uuid.New(),
		principalID: uuid.New(),
	}
	slug := "t-" + ids.tenantID.String()[:8]
	_, err := pool.Exec(ctx, `INSERT INTO tenants (id, slug, name) VALUES ($1, $2, $3)`, ids.tenantID, slug, "Tenant "+slug)
	if err != nil {
		t.Fatalf("insert tenant: %v", err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO projects (id, tenant_id, slug, name) VALUES ($1, $2, $3, $4)`, ids.projectID, ids.tenantID, "proj-"+slug, "Project "+slug)
	if err != nil {
		t.Fatalf("insert project: %v", err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO agents (id, tenant_id, external_key, name) VALUES ($1, $2, $3, $4)`, ids.agentID, ids.tenantID, "agent-"+slug, "Agent "+slug)
	if err != nil {
		t.Fatalf("insert agent: %v", err)
	}
	hash := sha256Hex("secret-" + ids.principalID.String())
	_, err = pool.Exec(ctx, `
		INSERT INTO principals (id, tenant_id, agent_id, principal_type, name, api_key_prefix, api_key_sha256, allowed_projects, allowed_scopes, is_admin)
		VALUES ($1, $2, $3, 'agent', $4, $5, $6, $7, $8, FALSE)`,
		ids.principalID, ids.tenantID, ids.agentID, "Principal "+slug, "rmr_ag_"+ids.principalID.String()[:8], hash,
		[]uuid.UUID{ids.projectID}, scopes,
	)
	if err != nil {
		t.Fatalf("insert principal: %v", err)
	}
	return ids
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func TestNewStore_NilPool(t *testing.T) {
	if _, err := postgres.NewStore(nil); !errors.Is(err, postgres.ErrNilPool) {
		t.Fatalf("expected ErrNilPool, got %v", err)
	}
}

func TestMigration00003_UpDownUpAndDuplicateAbort(t *testing.T) {
	pool := openTestPool(t)
	ctx := context.Background()
	sqlDB := stdlib.OpenDBFromPool(pool)
	t.Cleanup(func() { _ = sqlDB.Close() })

	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatalf("set dialect: %v", err)
	}

	// Step down to version 2 so we can test 00003 rollback when org_shared duplicates exist.
	if err := goose.DownToContext(ctx, sqlDB, ".", 2); err != nil {
		t.Fatalf("goose DownTo 2 failed: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `TRUNCATE TABLE memories, memory_sources, principals, agents, projects, tenants CASCADE`)
		_ = goose.UpContext(context.Background(), sqlDB, ".")
	})

	f := seedTenantProjectAgentPrincipal(t, ctx, pool, []string{"org_shared"})
	proj2 := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO projects (id, tenant_id, slug, name) VALUES ($1, $2, 'p2', 'Proj 2')`, proj2, f.tenantID); err != nil {
		t.Fatalf("insert proj2: %v", err)
	}
	srcID := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO memory_sources (id, tenant_id, source_type, external_source_id) VALUES ($1, $2, 'user_input', 'seed')`, srcID, f.tenantID); err != nil {
		t.Fatalf("insert source: %v", err)
	}

	// Under 00001/00002, two active org_shared rows with same topic_key across two projects are permitted.
	for i, pid := range []uuid.UUID{f.projectID, proj2} {
		_, err := pool.Exec(ctx, `
			INSERT INTO memories (id, tenant_id, project_id, memory_type, scope_type, status, topic_key, summary, content, content_sha256, primary_source_id)
			VALUES ($1, $2, $3, 'semantic', 'org_shared', 'active', 'shared-rule', $4, 'body', $5, $6)`,
			uuid.New(), f.tenantID, pid, fmt.Sprintf("summary-%d", i), sha256Hex(fmt.Sprintf("dup-%d", i)), srcID,
		)
		if err != nil {
			t.Fatalf("insert duplicate fixture %d under v2: %v", i, err)
		}
	}

	// Applying 00003 MUST abort and roll back cleanly leaving duplicate rows intact.
	if err := goose.UpContext(ctx, sqlDB, "."); err == nil {
		t.Fatalf("expected 00003 Up to fail when active org_shared topic_key duplicates exist across projects")
	}

	var rowCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM memories WHERE tenant_id = $1 AND topic_key = 'shared-rule'`, f.tenantID).Scan(&rowCount); err != nil {
		t.Fatalf("count memories after aborted migration: %v", err)
	}
	if rowCount != 2 {
		t.Fatalf("expected aborted migration to preserve 2 rows, got %d", rowCount)
	}

	// Clean duplicate so 00003 Up/Down/Up succeeds and enforces AC-02 tenant uniqueness.
	if _, err := pool.Exec(ctx, `DELETE FROM memories WHERE tenant_id = $1 AND project_id = $2`, f.tenantID, proj2); err != nil {
		t.Fatalf("delete duplicate: %v", err)
	}
	if err := goose.UpContext(ctx, sqlDB, "."); err != nil {
		t.Fatalf("00003 Up after cleanup failed: %v", err)
	}
	_, err := pool.Exec(ctx, `
		INSERT INTO memories (id, tenant_id, project_id, memory_type, scope_type, status, topic_key, summary, content, content_sha256, primary_source_id)
		VALUES ($1, $2, $3, 'semantic', 'org_shared', 'active', 'shared-rule', 'conflict', 'body2', $4, $5)`,
		uuid.New(), f.tenantID, proj2, sha256Hex("conflict"), srcID,
	)
	if err == nil {
		t.Fatalf("expected 00003 idx_memories_active_topic_key to reject second active org_shared topic across projects")
	}
}

func TestTaskPermissions_CompositeFKAndAtomicAudit(t *testing.T) {
	pool := openTestPool(t)
	ctx := context.Background()
	store, err := postgres.NewStore(pool)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	tA := seedTenantProjectAgentPrincipal(t, ctx, pool, []string{"task_shared"})
	tB := seedTenantProjectAgentPrincipal(t, ctx, pool, []string{"task_shared"})

	// 1. Composite FK must reject cross-tenant principal/project combination.
	_, fkErr := pool.Exec(ctx, `
		INSERT INTO task_permissions (tenant_id, project_id, task_id, principal_id, can_read, can_write)
		VALUES ($1, $2, 'task-1', $3, TRUE, TRUE)`,
		tA.tenantID, tA.projectID, tB.principalID,
	)
	if fkErr == nil {
		t.Fatalf("expected composite FK to reject principal from tenant B inside tenant A row")
	}

	// 2. Grant + invalid audit (empty Operation) must roll back grant atomically.
	invalidAudit := memory.AuditEvent{
		ID:           uuid.New(),
		TenantID:     &tA.tenantID,
		PrincipalID:  &tA.principalID,
		ResourceType: "task_permission",
		Outcome:      memory.AuditAllowed,
		CreatedAt:    time.Now().UTC(),
	}
	err = store.GrantOrRevokeTaskPermission(ctx, postgres.TaskPermissionGrant{
		TenantID: tA.tenantID, ProjectID: tA.projectID, TaskID: "task-1", PrincipalID: tA.principalID, CanRead: true, CanWrite: false,
	}, invalidAudit)
	if err == nil {
		t.Fatalf("expected GrantOrRevokeTaskPermission to fail when audit event is invalid")
	}
	snap, err := store.TaskPermissions().EvaluateTaskAccess(ctx, identity.TaskPermissionQuery{
		TenantID: tA.tenantID, ProjectID: tA.projectID, PrincipalID: tA.principalID, TaskID: "task-1",
	})
	if err != nil {
		t.Fatalf("EvaluateTaskAccess: %v", err)
	}
	if snap.CanRead || snap.CanWrite {
		t.Fatalf("expected rolled-back grant to leave no permissions, got %+v", snap)
	}

	// 3. Valid atomic grant commits both permission and audit event.
	validAudit := invalidAudit
	validAudit.Operation = "grant_task_permission"
	err = store.GrantOrRevokeTaskPermission(ctx, postgres.TaskPermissionGrant{
		TenantID: tA.tenantID, ProjectID: tA.projectID, TaskID: "task-1", PrincipalID: tA.principalID, CanRead: true, CanWrite: false,
	}, validAudit)
	if err != nil {
		t.Fatalf("valid GrantOrRevokeTaskPermission failed: %v", err)
	}
	canRead, err := store.TaskPermissions().HasTaskPermission(ctx, identity.TaskPermissionQuery{
		TenantID: tA.tenantID, ProjectID: tA.projectID, PrincipalID: tA.principalID, TaskID: "task-1", Permission: identity.TaskRead,
	})
	if err != nil || !canRead {
		t.Fatalf("expected canRead=true, nil; got %v, %v", canRead, err)
	}
	canWrite, err := store.TaskPermissions().HasTaskPermission(ctx, identity.TaskPermissionQuery{
		TenantID: tA.tenantID, ProjectID: tA.projectID, PrincipalID: tA.principalID, TaskID: "task-1", Permission: identity.TaskWrite,
	})
	if err != nil || canWrite {
		t.Fatalf("expected canWrite=false, nil; got %v, %v", canWrite, err)
	}
}

func TestUnitOfWorkAndAuditOutOfRollback(t *testing.T) {
	pool := openTestPool(t)
	ctx := context.Background()
	store, err := postgres.NewStore(pool)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	f := seedTenantProjectAgentPrincipal(t, ctx, pool, []string{"project_shared"})

	now := time.Now().UTC().Truncate(time.Microsecond)
	srcID := uuid.New()
	memID := uuid.New()
	rollbackErr := errors.New("forced rollback")

	// ExecTx with RecordDenied + failed transaction must keep denied audit while rolling back source.
	err = store.ExecTx(ctx, func(reg memory.RepositoryRegistry) error {
		if err := reg.Sources().InsertSource(ctx, provenance.MemorySource{
			ID: srcID, TenantID: f.tenantID, ProjectID: &f.projectID,
			SourceType: provenance.SourceAgentExecution, ExternalSourceID: "run-1",
			ActorPrincipalID: &f.principalID, ActorAgentID: &f.agentID, CreatedAt: now,
		}); err != nil {
			return err
		}
		if err := reg.Audit().RecordDenied(ctx, memory.AuditEvent{
			ID: uuid.New(), TenantID: &f.tenantID, PrincipalID: &f.principalID,
			Operation: "capture", ResourceType: "memory", ResourceID: &memID,
			ScopeUsed: "project_shared", Outcome: memory.AuditDenied, CreatedAt: now,
		}); err != nil {
			return err
		}
		return rollbackErr
	})
	if !errors.Is(err, rollbackErr) {
		t.Fatalf("expected forced rollback error, got %v", err)
	}

	var srcCount, deniedCount int
	_ = pool.QueryRow(ctx, `SELECT COUNT(*) FROM memory_sources WHERE id = $1`, srcID).Scan(&srcCount)
	_ = pool.QueryRow(ctx, `SELECT COUNT(*) FROM audit_events WHERE tenant_id = $1 AND outcome = 'denied'`, f.tenantID).Scan(&deniedCount)
	if srcCount != 0 || deniedCount != 1 {
		t.Fatalf("expected source rolled back (0) and denied audit persisted (1), got src=%d denied=%d", srcCount, deniedCount)
	}

	// Full commit of source, memory, two versions (Point-in-Time), relation, feedback, job, allowed audit.
	mem2ID := uuid.New()
	t0 := now.Add(-2 * time.Hour)
	t1 := now.Add(-1 * time.Hour)
	src := provenance.MemorySource{
		ID: srcID, TenantID: f.tenantID, ProjectID: &f.projectID,
		SourceType: provenance.SourceUserInput, ExternalSourceID: "cli-1",
		ActorPrincipalID: &f.principalID, CreatedAt: t0,
	}
	m1 := memory.Memory{
		ID: memID, TenantID: f.tenantID, ProjectID: &f.projectID,
		Type: memory.TypeSemantic, ScopeType: identity.ScopeProjectShared, Status: memory.StatusActive,
		TopicKey: "arch/db", Summary: "Postgres 17", Content: "Use READ COMMITTED",
		ContentSHA256: sha256Hex("Use READ COMMITTED"), TokenCount: 5, Tags: []string{"db"}, Entities: []string{"Postgres"},
		EpistemicStatus: memory.EpistemicObserved, AlphaScore: 8.5, BetaScore: 1.5, Importance: 0.7, UtilityScore: 0.1,
		Version: 2, PrimarySourceID: srcID, IdempotencyKey: "idem-1",
		Clocks: memory.TriTemporalWindow{ObservedAt: t0, CreatedAt: t0, ValidFrom: t0}, UpdatedAt: t1,
	}
	m2 := m1
	m2.ID = mem2ID
	m2.TopicKey = "arch/cache"
	m2.IdempotencyKey = ""
	m2.Version = 1

	err = store.ExecTx(ctx, func(reg memory.RepositoryRegistry) error {
		if err := reg.Sources().InsertSource(ctx, src); err != nil {
			return err
		}
		if err := reg.Memories().InsertMemory(ctx, m1); err != nil {
			return err
		}
		if err := reg.Memories().InsertMemory(ctx, m2); err != nil {
			return err
		}
		if err := reg.Memories().InsertMemoryVersion(ctx, memory.MemoryVersion{
			ID: uuid.New(), MemoryID: memID, Version: 1, Summary: "Old", Content: "v1",
			Tags: []string{"db"}, Entities: []string{}, EpistemicStatus: memory.EpistemicObserved,
			AlphaScore: 8.5, BetaScore: 1.5, Importance: 0.5, UtilityScore: 0.0, Status: memory.StatusActive,
			ValidFrom: t0, CreatedAt: t0,
		}); err != nil {
			return err
		}
		if err := reg.Memories().CloseMemoryVersionValidity(ctx, memID, 1, t1); err != nil {
			return err
		}
		if err := reg.Memories().InsertMemoryVersion(ctx, memory.MemoryVersion{
			ID: uuid.New(), MemoryID: memID, Version: 2, Summary: "Postgres 17", Content: "Use READ COMMITTED",
			Tags: []string{"db"}, Entities: []string{}, EpistemicStatus: memory.EpistemicObserved,
			AlphaScore: 8.5, BetaScore: 1.5, Importance: 0.7, UtilityScore: 0.1, Status: memory.StatusActive,
			ValidFrom: t1, CreatedAt: t1,
		}); err != nil {
			return err
		}
		if err := reg.Memories().InsertMemoryRelation(ctx, memory.MemoryRelation{
			ID: uuid.New(), TenantID: f.tenantID, ProjectID: &f.projectID,
			FromMemoryID: memID, ToMemoryID: mem2ID, RelationType: memory.RelationSupports,
			Weight: 0.9, ValidFrom: t1, IsActive: true, CreatedAt: t1,
		}); err != nil {
			return err
		}
		if err := reg.Feedback().InsertMemoryFeedback(ctx, memory.MemoryFeedback{
			ID: uuid.New(), TenantID: f.tenantID, MemoryID: memID,
			Signal: memory.SignalHelpfulSuccess, AlphaDelta: 1.0, UtilityDelta: 0.15, CreatedAt: t1,
		}); err != nil {
			return err
		}
		if err := reg.Jobs().EnqueueMemoryJob(ctx, memory.MemoryJob{
			ID: uuid.New(), TenantID: f.tenantID, ProjectID: &f.projectID, MemoryID: &memID,
			JobType: "embed_memory", Priority: 10, Status: memory.JobPending,
			MaxAttempts: 5, ScheduledAt: t1, CreatedAt: t1,
		}); err != nil {
			return err
		}
		if err := reg.Audit().RecordAllowed(ctx, memory.AuditEvent{
			ID: uuid.New(), TenantID: &f.tenantID, PrincipalID: &f.principalID,
			Operation: "capture", ResourceType: "memory", ResourceID: &memID,
			Outcome: memory.AuditAllowed, CreatedAt: t1,
		}); err != nil {
			return err
		}
		locked, err := reg.Memories().LockActiveTopicMemory(ctx, m1.ScopeTarget(), "arch/db")
		if err != nil || locked.ID != memID {
			return fmt.Errorf("LockActiveTopicMemory mismatch: locked=%v err=%w", locked, err)
		}
		pit1, err := reg.Memories().GetMemoryVersionPointInTime(ctx, memID, t0.Add(10*time.Minute), t0.Add(10*time.Minute))
		if err != nil || pit1.Version != 1 {
			return fmt.Errorf("PIT v1 mismatch: %+v err=%w", pit1, err)
		}
		pit2, err := reg.Memories().GetMemoryVersionPointInTime(ctx, memID, t1, t1)
		if err != nil || pit2.Version != 2 {
			return fmt.Errorf("PIT v2 mismatch: %+v err=%w", pit2, err)
		}
		rels, err := reg.Memories().ListActiveRelationsByMemoryID(ctx, f.tenantID, memID)
		if err != nil || len(rels) != 1 {
			return fmt.Errorf("ListActiveRelations mismatch: len=%d err=%w", len(rels), err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("full UnitOfWork tx failed: %v", err)
	}
}

func TestOpaqueRequestQueue_IdempotencyLeaseFencingRetryAndPurge(t *testing.T) {
	pool := openTestPool(t)
	ctx := context.Background()
	store, err := postgres.NewStore(pool)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	tA := seedTenantProjectAgentPrincipal(t, ctx, pool, []string{"task_shared"})
	// Second principal in same tenant to prove independent idempotency namespaces.
	p2ID := uuid.New()
	_, err = pool.Exec(ctx, `
		INSERT INTO principals (id, tenant_id, principal_type, name, api_key_prefix, api_key_sha256, allowed_projects, allowed_scopes)
		VALUES ($1, $2, 'agent', 'P2', 'rmr_ag_p2000000', $3, $4, $5)`,
		p2ID, tA.tenantID, sha256Hex("p2-secret"), []uuid.UUID{tA.projectID}, []string{"task_shared"},
	)
	if err != nil {
		t.Fatalf("insert p2: %v", err)
	}

	// Grant write+read to tA.principalID for task-alpha
	if err := store.GrantOrRevokeTaskPermission(ctx, postgres.TaskPermissionGrant{
		TenantID: tA.tenantID, ProjectID: tA.projectID, TaskID: "task-alpha", PrincipalID: tA.principalID, CanRead: true, CanWrite: true,
	}, memory.AuditEvent{
		ID: uuid.New(), TenantID: &tA.tenantID, PrincipalID: &tA.principalID,
		Operation: "grant_task_permission", ResourceType: "task_permission", Outcome: memory.AuditAllowed, CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("grant task-alpha: %v", err)
	}

	queue := store.OpaqueRequests(60 * time.Second)
	payload1 := json.RawMessage(`{"summary":"A"}`)
	hash1 := sha256Hex("payload-A")
	rec1, err := queue.Enqueue(ctx, postgres.OpaqueRequest{
		TenantID: tA.tenantID, ProjectID: tA.projectID, TaskID: "task-alpha", PrincipalID: tA.principalID,
		Operation: postgres.OpaqueOpCapture, PayloadVersion: 1, Payload: payload1,
		RequestSHA256: hash1, IdempotencyKey: "k-1",
	})
	if err != nil {
		t.Fatalf("Enqueue 1: %v", err)
	}

	// Identical retry -> same request_id
	recDup, err := queue.Enqueue(ctx, postgres.OpaqueRequest{
		TenantID: tA.tenantID, ProjectID: tA.projectID, TaskID: "task-alpha", PrincipalID: tA.principalID,
		Operation: postgres.OpaqueOpCapture, PayloadVersion: 1, Payload: payload1,
		RequestSHA256: hash1, IdempotencyKey: "k-1",
	})
	if err != nil || recDup.RequestID != rec1.RequestID {
		t.Fatalf("expected identical idempotency receipt %s, got %+v err=%v", rec1.RequestID, recDup, err)
	}

	// Same key, different canonical hash -> ErrIdempotencyConflict
	_, err = queue.Enqueue(ctx, postgres.OpaqueRequest{
		TenantID: tA.tenantID, ProjectID: tA.projectID, TaskID: "task-alpha", PrincipalID: tA.principalID,
		Operation: postgres.OpaqueOpCapture, PayloadVersion: 1, Payload: json.RawMessage(`{"summary":"B"}`),
		RequestSHA256: sha256Hex("payload-B"), IdempotencyKey: "k-1",
	})
	if !errors.Is(err, postgres.ErrIdempotencyConflict) {
		t.Fatalf("expected ErrIdempotencyConflict, got %v", err)
	}

	// Second principal with same key k-1 -> separate independent request
	recP2, err := queue.Enqueue(ctx, postgres.OpaqueRequest{
		TenantID: tA.tenantID, ProjectID: tA.projectID, TaskID: "task-alpha", PrincipalID: p2ID,
		Operation: postgres.OpaqueOpCapture, PayloadVersion: 1, Payload: json.RawMessage(`{"summary":"B"}`),
		RequestSHA256: sha256Hex("payload-B"), IdempotencyKey: "k-1",
	})
	if err != nil || recP2.RequestID == rec1.RequestID {
		t.Fatalf("expected distinct request for p2, got %+v err=%v", recP2, err)
	}

	// Concurrent Claim across 2 workers yields 2 distinct leases (SKIP LOCKED).
	var wg sync.WaitGroup
	claimed := make([]*postgres.Lease, 2)
	claimErrs := make([]error, 2)
	for i := range 2 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			claimed[idx], claimErrs[idx] = queue.Claim(ctx)
		}(i)
	}
	wg.Wait()
	if claimErrs[0] != nil || claimErrs[1] != nil || claimed[0] == nil || claimed[1] == nil {
		t.Fatalf("expected 2 concurrent claims, got leases=%+v errs=%+v", claimed, claimErrs)
	}
	if claimed[0].RequestID == claimed[1].RequestID {
		t.Fatalf("expected SKIP LOCKED to avoid claiming same request twice: %s", claimed[0].RequestID)
	}

	leaseByID := map[uuid.UUID]postgres.Lease{
		claimed[0].RequestID: *claimed[0],
		claimed[1].RequestID: *claimed[1],
	}
	authLease, ok := leaseByID[rec1.RequestID]
	if !ok {
		t.Fatalf("missing claimed lease for %s", rec1.RequestID)
	}
	retryLease, ok := leaseByID[recP2.RequestID]
	if !ok {
		t.Fatalf("missing claimed lease for %s", recP2.RequestID)
	}

	// Empty queue returns nil, nil
	emptyLease, err := queue.Claim(ctx)
	if err != nil || emptyLease != nil {
		t.Fatalf("expected nil, nil on empty queue, got %+v, %v", emptyLease, err)
	}

	// Stale/expired lease token cannot renew or commit Execute (must roll back side effect).
	staleLease := authLease
	staleLease.Token = uuid.New()
	renewed, err := queue.Renew(ctx, staleLease)
	if err != nil || renewed {
		t.Fatalf("expected false, nil for stale lease renew, got %v, %v", renewed, err)
	}

	if ok, err := queue.Renew(ctx, authLease); err != nil || !ok {
		t.Fatalf("expected valid lease Renew true, got %v, %v", ok, err)
	}

	// Simulate stale lease execution while callback writes allowed audit, ensuring rollback.
	staleAuditID := uuid.New()
	err = queue.Execute(ctx, staleLease, func(ctx context.Context, reg memory.RepositoryRegistry, perms identity.TaskPermissionReader, req postgres.OpaqueRequestRecord) (*uuid.UUID, error) {
		_ = reg.Audit().RecordAllowed(ctx, memory.AuditEvent{
			ID: staleAuditID, TenantID: &tA.tenantID, PrincipalID: &tA.principalID,
			Operation: "stale_should_rollback", ResourceType: "memory", Outcome: memory.AuditAllowed, CreatedAt: time.Now().UTC(),
		})
		return nil, nil
	})
	if err == nil {
		t.Fatalf("expected Execute with stale lease to fail")
	}
	var auditExists int
	_ = pool.QueryRow(ctx, `SELECT COUNT(*) FROM audit_events WHERE id = $1`, staleAuditID).Scan(&auditExists)
	if auditExists != 0 {
		t.Fatalf("expected rolled-back Execute to revert allowed audit, got %d", auditExists)
	}

	// Valid Execute completes authLease atomically with tx permission check + side effect.
	completedAuditID := uuid.New()
	err = queue.Execute(ctx, authLease, func(ctx context.Context, reg memory.RepositoryRegistry, perms identity.TaskPermissionReader, req postgres.OpaqueRequestRecord) (*uuid.UUID, error) {
		ok, err := perms.HasTaskPermission(ctx, identity.TaskPermissionQuery{
			TenantID: req.TenantID, ProjectID: req.ProjectID, PrincipalID: req.PrincipalID, TaskID: req.TaskID, Permission: identity.TaskWrite,
		})
		if err != nil || !ok {
			return nil, fmt.Errorf("unexpected tx perms denial: ok=%v err=%w", ok, err)
		}
		if err := reg.Audit().RecordAllowed(ctx, memory.AuditEvent{
			ID: completedAuditID, TenantID: &req.TenantID, PrincipalID: &req.PrincipalID,
			Operation: "opaque_exec", ResourceType: "memory", Outcome: memory.AuditAllowed, CreatedAt: time.Now().UTC(),
		}); err != nil {
			return nil, err
		}
		resID := uuid.New()
		return &resID, nil
	})
	if err != nil {
		t.Fatalf("valid Execute failed: %v", err)
	}
	meta, err := queue.GetStatus(ctx, tA.tenantID, authLease.RequestID)
	if err != nil || meta.Status != postgres.OpaqueStatusCompleted {
		t.Fatalf("expected completed status, got %+v err=%v", meta, err)
	}

	// Failure & retry flow on retryLease (recP2): 3 transient retries -> 4th attempt becomes failed terminal.
	for retryIdx := range 4 {
		if retryIdx > 0 {
			_, _ = pool.Exec(ctx, `UPDATE opaque_memory_requests SET next_attempt_at = NOW() - INTERVAL '1 second' WHERE request_id = $1`, retryLease.RequestID)
			claimedAgain, err := queue.Claim(ctx)
			if err != nil || claimedAgain == nil {
				t.Fatalf("reclaim retry %d failed: lease=%v err=%v", retryIdx, claimedAgain, err)
			}
			retryLease = *claimedAgain
		}
		if err := queue.RecordFailure(ctx, retryLease, postgres.FailureTransient, "TRANSIENT", "db hiccup"); err != nil {
			t.Fatalf("RecordFailure %d: %v", retryIdx, err)
		}
	}
	metaFailed, err := queue.GetStatus(ctx, tA.tenantID, retryLease.RequestID)
	if err != nil || metaFailed.Status != postgres.OpaqueStatusFailed {
		t.Fatalf("expected terminal failed status after 3 retries exhausted, got %+v err=%v", metaFailed, err)
	}

	// PurgeTerminal respects retention and never deletes active pending requests.
	pendingRec, err := queue.Enqueue(ctx, postgres.OpaqueRequest{
		TenantID: tA.tenantID, ProjectID: tA.projectID, TaskID: "task-alpha", PrincipalID: tA.principalID,
		Operation: postgres.OpaqueOpCapture, PayloadVersion: 1, Payload: payload1,
		RequestSHA256: hash1, IdempotencyKey: "active-pending",
	})
	if err != nil {
		t.Fatalf("Enqueue pending: %v", err)
	}
	_, _ = pool.Exec(ctx, `UPDATE opaque_memory_requests SET completed_at = NOW() - INTERVAL '31 days' WHERE request_id = $1`, authLease.RequestID)
	purged, err := queue.PurgeTerminal(ctx, 30*24*time.Hour)
	if err != nil || purged != 1 {
		t.Fatalf("expected 1 purged terminal row older than 30d, got purged=%d err=%v", purged, err)
	}
	if _, err := queue.GetStatus(ctx, tA.tenantID, pendingRec.RequestID); err != nil {
		t.Fatalf("expected pending request to survive purge, got %v", err)
	}
}
