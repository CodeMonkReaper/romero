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

	// Under 00001/00002, two active org_shared/global_public rows with same topic_key across two projects are permitted.
	for _, scope := range []string{"org_shared", "global_public"} {
		for i, pid := range []uuid.UUID{f.projectID, proj2} {
			_, err := pool.Exec(ctx, `
				INSERT INTO memories (id, tenant_id, project_id, memory_type, scope_type, status, topic_key, summary, content, content_sha256, primary_source_id)
				VALUES ($1, $2, $3, 'semantic', $4::scope_type_enum, 'active', 'shared-rule', $5, 'body', $6, $7)`,
				uuid.New(), f.tenantID, pid, scope, fmt.Sprintf("%s-summary-%d", scope, i), sha256Hex(fmt.Sprintf("%s-dup-%d", scope, i)), srcID,
			)
			if err != nil {
				t.Fatalf("insert duplicate %s fixture %d under v2: %v", scope, i, err)
			}
		}

		// Applying 00003 MUST abort and roll back cleanly leaving duplicate rows intact.
		if err := goose.UpContext(ctx, sqlDB, "."); err == nil {
			t.Fatalf("expected 00003 Up to fail when active %s topic_key duplicates exist across projects", scope)
		}

		var rowCount int
		if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM memories WHERE tenant_id = $1 AND scope_type = $2::scope_type_enum AND topic_key = 'shared-rule'`, f.tenantID, scope).Scan(&rowCount); err != nil {
			t.Fatalf("count %s memories after aborted migration: %v", scope, err)
		}
		if rowCount != 2 {
			t.Fatalf("expected aborted migration to preserve 2 %s rows, got %d", scope, rowCount)
		}

		if _, err := pool.Exec(ctx, `DELETE FROM memories WHERE tenant_id = $1 AND scope_type = $2::scope_type_enum`, f.tenantID, scope); err != nil {
			t.Fatalf("clean %s duplicates: %v", scope, err)
		}
	}

	// Once clean, 00003 Up/Down/Up succeeds and enforces tenant uniqueness across projects in BOTH org_shared and global_public.
	if err := goose.UpContext(ctx, sqlDB, "."); err != nil {
		t.Fatalf("00003 Up after cleanup failed: %v", err)
	}
	for _, scope := range []string{"org_shared", "global_public"} {
		_, err := pool.Exec(ctx, `
			INSERT INTO memories (id, tenant_id, project_id, memory_type, scope_type, status, topic_key, summary, content, content_sha256, primary_source_id)
			VALUES ($1, $2, $3, 'semantic', $4::scope_type_enum, 'active', 'shared-rule', 'first', 'body1', $5, $6)`,
			uuid.New(), f.tenantID, f.projectID, scope, sha256Hex(scope+"-1"), srcID,
		)
		if err != nil {
			t.Fatalf("insert first %s row under v3: %v", scope, err)
		}
		_, err = pool.Exec(ctx, `
			INSERT INTO memories (id, tenant_id, project_id, memory_type, scope_type, status, topic_key, summary, content, content_sha256, primary_source_id)
			VALUES ($1, $2, $3, 'semantic', $4::scope_type_enum, 'active', 'shared-rule', 'conflict', 'body2', $5, $6)`,
			uuid.New(), f.tenantID, proj2, scope, sha256Hex(scope+"-2"), srcID,
		)
		if err == nil {
			t.Fatalf("expected 00003 idx_memories_active_topic_key to reject second active %s topic across projects", scope)
		}
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

	// 1. Composite FKs must reject cross-tenant principal/project combinations on BOTH task_permissions and opaque_memory_requests.
	if _, err := pool.Exec(ctx, `
		INSERT INTO task_permissions (tenant_id, project_id, task_id, principal_id, can_read, can_write)
		VALUES ($1, $2, 'task-1', $3, TRUE, TRUE)`,
		tA.tenantID, tA.projectID, tB.principalID,
	); err == nil {
		t.Fatalf("expected composite FK on task_permissions to reject cross-tenant principal")
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO task_permissions (tenant_id, project_id, task_id, principal_id, can_read, can_write)
		VALUES ($1, $2, 'task-1', $3, TRUE, TRUE)`,
		tA.tenantID, tB.projectID, tA.principalID,
	); err == nil {
		t.Fatalf("expected composite FK on task_permissions to reject cross-tenant project")
	}
	queue := store.OpaqueRequests(60 * time.Second)
	if _, err := queue.Enqueue(ctx, postgres.OpaqueRequest{
		TenantID: tA.tenantID, ProjectID: tB.projectID, TaskID: "task-1", PrincipalID: tA.principalID,
		Operation: postgres.OpaqueOpCapture, RequestSHA256: sha256Hex("fk-proj"),
	}); err == nil {
		t.Fatalf("expected composite FK on opaque_memory_requests to reject cross-tenant project")
	}
	if _, err := queue.Enqueue(ctx, postgres.OpaqueRequest{
		TenantID: tA.tenantID, ProjectID: tA.projectID, TaskID: "task-1", PrincipalID: tB.principalID,
		Operation: postgres.OpaqueOpCapture, RequestSHA256: sha256Hex("fk-principal"),
	}); err == nil {
		t.Fatalf("expected composite FK on opaque_memory_requests to reject cross-tenant principal")
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

	// 4. Write-without-read snapshot + project/scope/active gates inside EvaluateTaskAccessSnapshot (AC-T2).
	validAudit.ID = uuid.New()
	if err := store.GrantOrRevokeTaskPermission(ctx, postgres.TaskPermissionGrant{
		TenantID: tA.tenantID, ProjectID: tA.projectID, TaskID: "task-wonly", PrincipalID: tA.principalID, CanRead: false, CanWrite: true,
	}, validAudit); err != nil {
		t.Fatalf("grant write-only: %v", err)
	}
	wOnlySnap, err := store.TaskPermissions().EvaluateTaskAccess(ctx, identity.TaskPermissionQuery{
		TenantID: tA.tenantID, ProjectID: tA.projectID, PrincipalID: tA.principalID, TaskID: "task-wonly",
	})
	if err != nil || wOnlySnap.CanRead || !wOnlySnap.CanWrite {
		t.Fatalf("expected {CanRead:false CanWrite:true}, got %+v err=%v", wOnlySnap, err)
	}

	// Even with full task row grants, removing scope/project/active flags MUST deny.
	validAudit.ID = uuid.New()
	if err := store.GrantOrRevokeTaskPermission(ctx, postgres.TaskPermissionGrant{
		TenantID: tA.tenantID, ProjectID: tA.projectID, TaskID: "task-gates", PrincipalID: tA.principalID, CanRead: true, CanWrite: true,
	}, validAudit); err != nil {
		t.Fatalf("grant task-gates: %v", err)
	}
	gatesQuery := identity.TaskPermissionQuery{
		TenantID: tA.tenantID, ProjectID: tA.projectID, PrincipalID: tA.principalID, TaskID: "task-gates",
	}
	gateStmts := []struct {
		name string
		up   string
		down string
	}{
		{"missing_task_shared_scope", `UPDATE principals SET allowed_scopes = ARRAY['project_shared'] WHERE id = $1`, `UPDATE principals SET allowed_scopes = ARRAY['task_shared'] WHERE id = $1`},
		{"missing_project_grant", `UPDATE principals SET allowed_projects = '{}' WHERE id = $1`, fmt.Sprintf(`UPDATE principals SET allowed_projects = ARRAY['%s']::uuid[] WHERE id = $1`, tA.projectID)},
		{"inactive_principal", `UPDATE principals SET is_active = FALSE WHERE id = $1`, `UPDATE principals SET is_active = TRUE WHERE id = $1`},
	}
	for _, tc := range gateStmts {
		if _, err := pool.Exec(ctx, tc.up, tA.principalID); err != nil {
			t.Fatalf("%s setup: %v", tc.name, err)
		}
		s, err := store.TaskPermissions().EvaluateTaskAccess(ctx, gatesQuery)
		if err != nil || s.CanRead || s.CanWrite {
			t.Fatalf("%s expected denied, got %+v err=%v", tc.name, s, err)
		}
		if _, err := pool.Exec(ctx, tc.down, tA.principalID); err != nil {
			t.Fatalf("%s restore: %v", tc.name, err)
		}
	}
	if _, err := pool.Exec(ctx, `UPDATE projects SET is_active = FALSE WHERE id = $1`, tA.projectID); err != nil {
		t.Fatalf("deactivate project: %v", err)
	}
	if s, err := store.TaskPermissions().EvaluateTaskAccess(ctx, gatesQuery); err != nil || s.CanRead || s.CanWrite {
		t.Fatalf("inactive_project expected denied, got %+v err=%v", s, err)
	}
	_, _ = pool.Exec(ctx, `UPDATE projects SET is_active = TRUE WHERE id = $1`, tA.projectID)
	if _, err := pool.Exec(ctx, `UPDATE tenants SET is_active = FALSE WHERE id = $1`, tA.tenantID); err != nil {
		t.Fatalf("deactivate tenant: %v", err)
	}
	if s, err := store.TaskPermissions().EvaluateTaskAccess(ctx, gatesQuery); err != nil || s.CanRead || s.CanWrite {
		t.Fatalf("inactive_tenant expected denied, got %+v err=%v", s, err)
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

	// ExecTx calling RecordAllowed AND RecordDenied inside a failing transaction must roll back source + allowed audit while keeping denied audit.
	err = store.ExecTx(ctx, func(reg memory.RepositoryRegistry) error {
		if err := reg.Sources().InsertSource(ctx, provenance.MemorySource{
			ID: srcID, TenantID: f.tenantID, ProjectID: &f.projectID,
			SourceType: provenance.SourceAgentExecution, ExternalSourceID: "run-1",
			ActorPrincipalID: &f.principalID, ActorAgentID: &f.agentID, CreatedAt: now,
		}); err != nil {
			return err
		}
		if err := reg.Audit().RecordAllowed(ctx, memory.AuditEvent{
			ID: uuid.New(), TenantID: &f.tenantID, PrincipalID: &f.principalID,
			Operation: "allowed_should_rollback", ResourceType: "memory", ResourceID: &memID,
			ScopeUsed: "project_shared", Outcome: memory.AuditAllowed, CreatedAt: now,
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

	var srcCount, allowedCount, deniedCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM memory_sources WHERE id = $1`, srcID).Scan(&srcCount); err != nil {
		t.Fatalf("scan source count: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM audit_events WHERE tenant_id = $1 AND outcome = 'allowed'`, f.tenantID).Scan(&allowedCount); err != nil {
		t.Fatalf("scan allowed count: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM audit_events WHERE tenant_id = $1 AND outcome = 'denied'`, f.tenantID).Scan(&deniedCount); err != nil {
		t.Fatalf("scan denied count: %v", err)
	}
	if srcCount != 0 || allowedCount != 0 || deniedCount != 1 {
		t.Fatalf("expected source=0 allowed=0 denied=1, got src=%d allowed=%d denied=%d", srcCount, allowedCount, deniedCount)
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

	// Concurrent -race Enqueue with identical key and hash yields the same request_id.
	const concurrentEnqueues = 6
	var enqueueWG sync.WaitGroup
	receipts := make([]postgres.OpaqueReceipt, concurrentEnqueues)
	enqueueErrs := make([]error, concurrentEnqueues)
	for i := range concurrentEnqueues {
		enqueueWG.Add(1)
		go func(idx int) {
			defer enqueueWG.Done()
			receipts[idx], enqueueErrs[idx] = queue.Enqueue(ctx, postgres.OpaqueRequest{
				TenantID: tA.tenantID, ProjectID: tA.projectID, TaskID: "task-alpha", PrincipalID: tA.principalID,
				Operation: postgres.OpaqueOpCapture, PayloadVersion: 1, Payload: payload1,
				RequestSHA256: hash1, IdempotencyKey: "k-1",
			})
		}(i)
	}
	enqueueWG.Wait()
	for i := range concurrentEnqueues {
		if enqueueErrs[i] != nil || receipts[i].RequestID != rec1.RequestID {
			t.Fatalf("concurrent Enqueue[%d] expected %s, got %+v err=%v", i, rec1.RequestID, receipts[i], enqueueErrs[i])
		}
	}

	// Cross-tenant GetStatus is isolated (returns memory.ErrNotFound).
	if _, err := queue.GetStatus(ctx, uuid.New(), rec1.RequestID); !errors.Is(err, memory.ErrNotFound) {
		t.Fatalf("expected cross-tenant GetStatus ErrNotFound, got %v", err)
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

	// Wrong lease token cannot renew, execute, or record failure (must roll back side effect).
	wrongTokenLease := authLease
	wrongTokenLease.Token = uuid.New()
	renewed, err := queue.Renew(ctx, wrongTokenLease)
	if err != nil || renewed {
		t.Fatalf("expected false, nil for wrong token renew, got %v, %v", renewed, err)
	}
	if err := queue.RecordFailure(ctx, wrongTokenLease, postgres.FailureTransient, "STALE", "wrong token"); !errors.Is(err, postgres.ErrLeaseLost) {
		t.Fatalf("expected ErrLeaseLost on wrong token RecordFailure, got %v", err)
	}

	wrongTokenAuditID := uuid.New()
	err = queue.Execute(ctx, wrongTokenLease, func(ctx context.Context, reg memory.RepositoryRegistry, perms identity.TaskPermissionReader, req postgres.OpaqueRequestRecord) (*uuid.UUID, error) {
		if err := reg.Audit().RecordAllowed(ctx, memory.AuditEvent{
			ID: wrongTokenAuditID, TenantID: &tA.tenantID, PrincipalID: &tA.principalID,
			Operation: "stale_should_rollback", ResourceType: "memory", Outcome: memory.AuditAllowed, CreatedAt: time.Now().UTC(),
		}); err != nil {
			return nil, err
		}
		return nil, nil
	})
	if !errors.Is(err, postgres.ErrLeaseLost) {
		t.Fatalf("expected ErrLeaseLost executing wrong token lease, got %v", err)
	}
	var auditExists int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM audit_events WHERE id = $1`, wrongTokenAuditID).Scan(&auditExists); err != nil {
		t.Fatalf("count wrongTokenAuditID: %v", err)
	}
	if auditExists != 0 {
		t.Fatalf("expected rolled-back Execute to revert allowed audit, got %d", auditExists)
	}

	if ok, err := queue.Renew(ctx, authLease); err != nil || !ok {
		t.Fatalf("expected valid lease Renew true, got %v, %v", ok, err)
	}

	// Mid-flight lease expiry inside Execute rolls back both memory_sources and audit_events with ErrLeaseLost.
	midFlightSourceID := uuid.New()
	midFlightAuditID := uuid.New()
	err = queue.Execute(ctx, authLease, func(ctx context.Context, reg memory.RepositoryRegistry, perms identity.TaskPermissionReader, req postgres.OpaqueRequestRecord) (*uuid.UUID, error) {
		if err := reg.Sources().InsertSource(ctx, provenance.MemorySource{
			ID: midFlightSourceID, TenantID: req.TenantID, ProjectID: &req.ProjectID,
			SourceType: provenance.SourceUserInput, ExternalSourceID: "mid-flight-expire",
			ActorPrincipalID: &req.PrincipalID, CreatedAt: time.Now().UTC(),
		}); err != nil {
			return nil, err
		}
		if err := reg.Audit().RecordAllowed(ctx, memory.AuditEvent{
			ID: midFlightAuditID, TenantID: &req.TenantID, PrincipalID: &req.PrincipalID,
			Operation: "mid_flight_should_rollback", ResourceType: "memory", Outcome: memory.AuditAllowed, CreatedAt: time.Now().UTC(),
		}); err != nil {
			return nil, err
		}
		// Expire the lease from an external connection while apply is in flight.
		if _, err := pool.Exec(ctx, `UPDATE opaque_memory_requests SET lease_expires_at = clock_timestamp() - INTERVAL '2 seconds' WHERE request_id = $1`, req.RequestID); err != nil {
			return nil, err
		}
		resID := uuid.New()
		return &resID, nil
	})
	if !errors.Is(err, postgres.ErrLeaseLost) {
		t.Fatalf("expected mid-flight lease expiry to fail with ErrLeaseLost, got %v", err)
	}
	var sourceCount, midAuditCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM memory_sources WHERE id = $1`, midFlightSourceID).Scan(&sourceCount); err != nil {
		t.Fatalf("count midFlightSourceID: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM audit_events WHERE id = $1`, midFlightAuditID).Scan(&midAuditCount); err != nil {
		t.Fatalf("count midFlightAuditID: %v", err)
	}
	if sourceCount != 0 || midAuditCount != 0 {
		t.Fatalf("expected mid-flight expiry rollback of source and audit, got source=%d audit=%d", sourceCount, midAuditCount)
	}

	// Expired-but-unreclaimed lease with original token fails Renew, Execute, and RecordFailure with ErrLeaseLost.
	if ok, err := queue.Renew(ctx, authLease); err != nil || ok {
		t.Fatalf("expected false, nil on expired lease Renew, got %v, %v", ok, err)
	}
	if err := queue.Execute(ctx, authLease, func(ctx context.Context, reg memory.RepositoryRegistry, perms identity.TaskPermissionReader, req postgres.OpaqueRequestRecord) (*uuid.UUID, error) {
		return nil, fmt.Errorf("apply should not run for expired lease")
	}); !errors.Is(err, postgres.ErrLeaseLost) {
		t.Fatalf("expected ErrLeaseLost executing expired lease, got %v", err)
	}
	if err := queue.RecordFailure(ctx, authLease, postgres.FailureTransient, "EXPIRED", "expired lease"); !errors.Is(err, postgres.ErrLeaseLost) {
		t.Fatalf("expected ErrLeaseLost recording failure on expired lease, got %v", err)
	}

	// Reclaim the expired lease and complete authLease atomically with tx permission check + side effect.
	reclaimedAuthLease, err := queue.Claim(ctx)
	if err != nil || reclaimedAuthLease == nil || reclaimedAuthLease.RequestID != authLease.RequestID {
		t.Fatalf("expected Claim to reclaim expired authLease %s, got %+v err=%v", authLease.RequestID, reclaimedAuthLease, err)
	}
	completedAuditID := uuid.New()
	err = queue.Execute(ctx, *reclaimedAuthLease, func(ctx context.Context, reg memory.RepositoryRegistry, perms identity.TaskPermissionReader, req postgres.OpaqueRequestRecord) (*uuid.UUID, error) {
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

	// Failure & retry flow on retryLease (recP2): 3 transient retries -> backoff 1s, 2s, 4s -> 4th attempt becomes terminal failed.
	expectedBackoffs := []time.Duration{1 * time.Second, 2 * time.Second, 4 * time.Second}
	for retryIdx := range 4 {
		if retryIdx > 0 {
			// Ensure pre-due item cannot be claimed prematurely before due window opens.
			preDueClaim, err := queue.Claim(ctx)
			if err != nil || preDueClaim != nil {
				t.Fatalf("expected nil claim while retry %d is in backoff window, got %+v err=%v", retryIdx, preDueClaim, err)
			}
			if _, err := pool.Exec(ctx, `UPDATE opaque_memory_requests SET next_attempt_at = clock_timestamp() - INTERVAL '1 second' WHERE request_id = $1`, retryLease.RequestID); err != nil {
				t.Fatalf("expire backoff %d: %v", retryIdx, err)
			}
			claimedAgain, err := queue.Claim(ctx)
			if err != nil || claimedAgain == nil {
				t.Fatalf("reclaim retry %d failed: lease=%v err=%v", retryIdx, claimedAgain, err)
			}
			retryLease = *claimedAgain
		}
		if err := queue.RecordFailure(ctx, retryLease, postgres.FailureTransient, "TRANSIENT", "db hiccup"); err != nil {
			t.Fatalf("RecordFailure %d: %v", retryIdx, err)
		}
		if retryIdx < 3 {
			var nextAttemptAt, updatedAt time.Time
			if err := pool.QueryRow(ctx, `SELECT next_attempt_at, updated_at FROM opaque_memory_requests WHERE request_id = $1`, retryLease.RequestID).Scan(&nextAttemptAt, &updatedAt); err != nil {
				t.Fatalf("read retry %d backoff: %v", retryIdx, err)
			}
			delta := nextAttemptAt.Sub(updatedAt)
			want := expectedBackoffs[retryIdx]
			if delta < want-100*time.Millisecond || delta > want+200*time.Millisecond {
				t.Fatalf("retry %d expected backoff ~%s, got %s", retryIdx, want, delta)
			}
		}
	}
	metaFailed, err := queue.GetStatus(ctx, tA.tenantID, retryLease.RequestID)
	if err != nil || metaFailed.Status != postgres.OpaqueStatusFailed {
		t.Fatalf("expected terminal failed status after 3 retries exhausted, got %+v err=%v", metaFailed, err)
	}
	if unclaimable, err := queue.Claim(ctx); err != nil || unclaimable != nil {
		t.Fatalf("expected terminal failed request to be unclaimable, got %+v err=%v", unclaimable, err)
	}

	// PurgeTerminal: deletes aged completed + aged failed, keeps recent completed + pending + aged running, allows idempotency_key reuse.
	recentCompletedRec, err := queue.Enqueue(ctx, postgres.OpaqueRequest{
		TenantID: tA.tenantID, ProjectID: tA.projectID, TaskID: "task-alpha", PrincipalID: tA.principalID,
		Operation: postgres.OpaqueOpCapture, PayloadVersion: 1, Payload: payload1,
		RequestSHA256: hash1, IdempotencyKey: "recent-completed",
	})
	if err != nil {
		t.Fatalf("Enqueue recentCompleted: %v", err)
	}
	recentLease, err := queue.Claim(ctx)
	if err != nil || recentLease == nil || recentLease.RequestID != recentCompletedRec.RequestID {
		t.Fatalf("Claim recentCompleted: lease=%+v err=%v", recentLease, err)
	}
	if err := queue.Execute(ctx, *recentLease, func(ctx context.Context, reg memory.RepositoryRegistry, perms identity.TaskPermissionReader, req postgres.OpaqueRequestRecord) (*uuid.UUID, error) {
		return nil, nil
	}); err != nil {
		t.Fatalf("Execute recentCompleted: %v", err)
	}

	agedRunningRec, err := queue.Enqueue(ctx, postgres.OpaqueRequest{
		TenantID: tA.tenantID, ProjectID: tA.projectID, TaskID: "task-alpha", PrincipalID: tA.principalID,
		Operation: postgres.OpaqueOpCapture, PayloadVersion: 1, Payload: payload1,
		RequestSHA256: hash1, IdempotencyKey: "aged-running",
	})
	if err != nil {
		t.Fatalf("Enqueue agedRunning: %v", err)
	}
	if _, err := queue.Claim(ctx); err != nil {
		t.Fatalf("Claim agedRunning: %v", err)
	}

	pendingRec, err := queue.Enqueue(ctx, postgres.OpaqueRequest{
		TenantID: tA.tenantID, ProjectID: tA.projectID, TaskID: "task-alpha", PrincipalID: tA.principalID,
		Operation: postgres.OpaqueOpCapture, PayloadVersion: 1, Payload: payload1,
		RequestSHA256: hash1, IdempotencyKey: "active-pending",
	})
	if err != nil {
		t.Fatalf("Enqueue pending: %v", err)
	}

	if _, err := pool.Exec(ctx, `UPDATE opaque_memory_requests SET completed_at = clock_timestamp() - INTERVAL '31 days' WHERE request_id = ANY($1)`, []uuid.UUID{authLease.RequestID, retryLease.RequestID}); err != nil {
		t.Fatalf("age terminal rows: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE opaque_memory_requests SET created_at = clock_timestamp() - INTERVAL '45 days', updated_at = clock_timestamp() - INTERVAL '45 days' WHERE request_id = $1`, agedRunningRec.RequestID); err != nil {
		t.Fatalf("age running row: %v", err)
	}

	purged, err := queue.PurgeTerminal(ctx, 30*24*time.Hour)
	if err != nil || purged != 2 {
		t.Fatalf("expected 2 purged terminal rows older than 30d (completed + failed), got purged=%d err=%v", purged, err)
	}
	if _, err := queue.GetStatus(ctx, tA.tenantID, authLease.RequestID); !errors.Is(err, memory.ErrNotFound) {
		t.Fatalf("expected purged completed request ErrNotFound, got %v", err)
	}
	if _, err := queue.GetStatus(ctx, tA.tenantID, retryLease.RequestID); !errors.Is(err, memory.ErrNotFound) {
		t.Fatalf("expected purged failed request ErrNotFound, got %v", err)
	}
	if _, err := queue.GetStatus(ctx, tA.tenantID, recentCompletedRec.RequestID); err != nil {
		t.Fatalf("expected recent completed request to survive purge, got %v", err)
	}
	if _, err := queue.GetStatus(ctx, tA.tenantID, pendingRec.RequestID); err != nil {
		t.Fatalf("expected pending request to survive purge, got %v", err)
	}
	if _, err := queue.GetStatus(ctx, tA.tenantID, agedRunningRec.RequestID); err != nil {
		t.Fatalf("expected aged running request to survive purge, got %v", err)
	}

	// After purge, idempotency_key "k-1" can be reused for tA.principalID to produce a brand-new request_id.
	reusedRec, err := queue.Enqueue(ctx, postgres.OpaqueRequest{
		TenantID: tA.tenantID, ProjectID: tA.projectID, TaskID: "task-alpha", PrincipalID: tA.principalID,
		Operation: postgres.OpaqueOpCapture, PayloadVersion: 1, Payload: json.RawMessage(`{"summary":"reused"}`),
		RequestSHA256: sha256Hex("payload-reused"), IdempotencyKey: "k-1",
	})
	if err != nil || reusedRec.RequestID == rec1.RequestID {
		t.Fatalf("expected new request_id after purged idempotency key reuse, old=%s new=%+v err=%v", rec1.RequestID, reusedRec, err)
	}
}
