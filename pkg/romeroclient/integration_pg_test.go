package romeroclient_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/romero-project/romero/internal/application/capture"
	"github.com/romero-project/romero/internal/application/conflicts"
	"github.com/romero-project/romero/internal/application/consolidation"
	contextbuilder "github.com/romero-project/romero/internal/application/context"
	"github.com/romero-project/romero/internal/application/feedback"
	"github.com/romero-project/romero/internal/application/provisioning"
	"github.com/romero-project/romero/internal/application/retrieval"
	"github.com/romero-project/romero/internal/domain/governor"
	"github.com/romero-project/romero/internal/domain/identity"
	"github.com/romero-project/romero/internal/domain/memory"
	"github.com/romero-project/romero/internal/infrastructure/cachel1"
	"github.com/romero-project/romero/internal/infrastructure/persistence/postgres"
	"github.com/romero-project/romero/internal/interfaces/rest"
	"github.com/romero-project/romero/migrations"
	"github.com/romero-project/romero/pkg/romeroclient"
)

const defaultClientPGDSN = "postgres://romero:romero_local_secret@127.0.0.1:54329/romero?sslmode=disable"

func openClientPGPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("ROMERO_TEST_DSN"))
	explicit := dsn != ""
	if !explicit {
		dsn = defaultClientPGDSN
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		if !explicit {
			t.Skipf("skipping PG client integration test: %v", err)
		}
		t.Fatalf("pgxpool.New failed: %v", err)
	}
	t.Cleanup(pool.Close)

	if err := pool.Ping(ctx); err != nil {
		if !explicit {
			t.Skipf("skipping PG client integration test (postgres unreachable): %v", err)
		}
		t.Fatalf("postgres ping failed: %v", err)
	}

	sqlDB := stdlib.OpenDBFromPool(pool)
	defer func() { _ = sqlDB.Close() }()
	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatalf("goose.SetDialect failed: %v", err)
	}
	if err := goose.Up(sqlDB, "."); err != nil {
		t.Fatalf("goose.Up failed: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		TRUNCATE TABLE
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
		CASCADE
	`); err != nil {
		t.Fatalf("truncate tables failed: %v", err)
	}
	return pool
}

type clientPrefixLookup struct{ s *postgres.ProvisioningStore }

func (l clientPrefixLookup) LookupPrincipalsByPrefix(ctx context.Context, prefix string) ([]provisioning.PrincipalAuthCandidate, error) {
	rows, err := l.s.LookupPrincipalsByPrefix(ctx, prefix)
	if err != nil {
		return nil, err
	}
	out := make([]provisioning.PrincipalAuthCandidate, 0, len(rows))
	for _, r := range rows {
		out = append(out, provisioning.PrincipalAuthCandidate{
			Principal: provisioning.Principal{
				ID: r.Principal.ID, TenantID: r.Principal.TenantID, AgentID: r.Principal.AgentID,
				PrincipalType: r.Principal.PrincipalType, Name: r.Principal.Name, APIKeyPrefix: r.Principal.APIKeyPrefix,
				AllowedProjects: r.Principal.AllowedProjects, AllowedScopes: r.Principal.AllowedScopes,
				IsActive: r.Principal.IsActive, ExpiresAt: r.Principal.ExpiresAt,
				CreatedAt: r.Principal.CreatedAt, IsAdmin: r.Principal.IsAdmin,
			},
			APIKeySHA256: r.APIKeySHA256,
			TenantActive: r.TenantActive,
			AgentActive:  r.AgentActive,
		})
	}
	return out, nil
}

type cCapTask struct {
	r *postgres.TaskPermissionRepository
}

func (a cCapTask) EvaluateTaskAccess(ctx context.Context, q identity.TaskPermissionQuery) (capture.TaskAccessSnapshot, error) {
	s, err := a.r.EvaluateTaskAccess(ctx, q)
	return capture.TaskAccessSnapshot{CanRead: s.CanRead, CanWrite: s.CanWrite}, err
}

type cRetTask struct {
	r *postgres.TaskPermissionRepository
}

func (a cRetTask) EvaluateTaskAccess(ctx context.Context, q identity.TaskPermissionQuery) (retrieval.TaskAccessSnapshot, error) {
	s, err := a.r.EvaluateTaskAccess(ctx, q)
	return retrieval.TaskAccessSnapshot{CanRead: s.CanRead, CanWrite: s.CanWrite}, err
}

type cFbTask struct {
	r *postgres.TaskPermissionRepository
}

func (a cFbTask) EvaluateTaskAccess(ctx context.Context, q identity.TaskPermissionQuery) (feedback.TaskAccessSnapshot, error) {
	s, err := a.r.EvaluateTaskAccess(ctx, q)
	return feedback.TaskAccessSnapshot{CanRead: s.CanRead, CanWrite: s.CanWrite}, err
}

type cCapOpaque struct{ o *postgres.OpaqueRequestStore }

func (a cCapOpaque) Enqueue(ctx context.Context, req capture.OpaqueRequest) (capture.OpaqueReceipt, error) {
	rec, err := a.o.Enqueue(ctx, postgres.OpaqueRequest{
		RequestID: req.RequestID, TenantID: req.TenantID, ProjectID: req.ProjectID, TaskID: req.TaskID,
		PrincipalID: req.PrincipalID, Operation: postgres.OpaqueOperation(req.Operation), TargetMemoryID: req.TargetMemoryID,
		PayloadVersion: req.PayloadVersion, Payload: req.Payload, RequestSHA256: req.RequestSHA256,
		IdempotencyKey: req.IdempotencyKey,
	})
	return capture.OpaqueReceipt{RequestID: rec.RequestID}, err
}

type cFbOpaque struct{ o *postgres.OpaqueRequestStore }

func (a cFbOpaque) EnqueueFeedback(ctx context.Context, req feedback.OpaqueFeedbackRequest) (feedback.OpaqueReceipt, error) {
	rec, err := a.o.Enqueue(ctx, postgres.OpaqueRequest{
		RequestID: req.RequestID, TenantID: req.TenantID, ProjectID: req.ProjectID, TaskID: req.TaskID,
		PrincipalID: req.PrincipalID, Operation: postgres.OpaqueOpFeedback, TargetMemoryID: &req.TargetMemoryID,
		PayloadVersion: 1, Payload: json.RawMessage(req.Payload), RequestSHA256: req.RequestSHA256,
		IdempotencyKey: req.IdempotencyKey,
	})
	return feedback.OpaqueReceipt{RequestID: rec.RequestID}, err
}

type cOpaqueStatus struct{ o *postgres.OpaqueRequestStore }

func (a cOpaqueStatus) GetStatus(ctx context.Context, tenantID, requestID uuid.UUID) (*retrieval.OpaqueRequestMetadata, error) {
	m, err := a.o.GetStatus(ctx, tenantID, requestID)
	if err != nil {
		if errors.Is(err, memory.ErrNotFound) {
			return nil, retrieval.ErrNotFound
		}
		return nil, err
	}
	return &retrieval.OpaqueRequestMetadata{
		RequestID: m.RequestID, TenantID: m.TenantID, ProjectID: m.ProjectID, TaskID: m.TaskID, Status: string(m.Status),
	}, nil
}

func TestClient_IntegrationPostgresRealRouter(t *testing.T) {
	pool := openClientPGPool(t)
	ctx := context.Background()

	store, err := postgres.NewStore(pool)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	provStore := store.Provisioning()
	tenant, err := provStore.CreateTenant(ctx, postgres.CreateTenantParams{
		ID: uuid.New(), Slug: "client-pg-tenant", Name: "Client PG Tenant", IsActive: true,
	})
	if err != nil {
		t.Fatalf("CreateTenant failed: %v", err)
	}
	project, err := provStore.CreateProject(ctx, postgres.CreateProjectParams{
		ID: uuid.New(), TenantID: tenant.ID, Slug: "client-proj", Name: "Client Project", IsActive: true,
	})
	if err != nil {
		t.Fatalf("CreateProject failed: %v", err)
	}
	agent, err := provStore.CreateAgent(ctx, postgres.CreateAgentParams{
		ID: uuid.New(), TenantID: tenant.ID, ExternalKey: "client-agent", Name: "Client Agent", RoleType: "worker", IsActive: true,
	})
	if err != nil {
		t.Fatalf("CreateAgent failed: %v", err)
	}

	adminID := uuid.New()
	adminKey, adminPrefix, adminHash, err := provisioning.GenerateAPIKey(identity.PrincipalUser, adminID)
	if err != nil {
		t.Fatalf("GenerateAPIKey admin failed: %v", err)
	}
	agentPID := uuid.New()
	agentKey, agentPrefix, agentHash, err := provisioning.GenerateAPIKey(identity.PrincipalAgent, agentPID)
	if err != nil {
		t.Fatalf("GenerateAPIKey agent failed: %v", err)
	}
	if _, err := provStore.CreatePrincipal(ctx, postgres.CreatePrincipalParams{
		ID: adminID, TenantID: tenant.ID, PrincipalType: identity.PrincipalUser,
		Name: "Admin", APIKeyPrefix: adminPrefix, APIKeySHA256: adminHash,
		AllowedProjects: []uuid.UUID{project.ID},
		AllowedScopes:   []identity.ScopeType{identity.ScopeProjectShared, identity.ScopeOrgShared},
		IsActive:        true, IsAdmin: true,
	}); err != nil {
		t.Fatalf("CreatePrincipal admin failed: %v", err)
	}
	agentPrincipal, err := provStore.CreatePrincipal(ctx, postgres.CreatePrincipalParams{
		ID: agentPID, TenantID: tenant.ID, AgentID: &agent.ID, PrincipalType: identity.PrincipalAgent,
		Name: "Client Agent", APIKeyPrefix: agentPrefix, APIKeySHA256: agentHash,
		AllowedProjects: []uuid.UUID{project.ID},
		AllowedScopes:   []identity.ScopeType{identity.ScopeAgentPrivate, identity.ScopeTaskShared, identity.ScopeProjectShared},
		IsActive:        true,
	})
	if err != nil {
		t.Fatalf("CreatePrincipal agent failed: %v", err)
	}
	if err := provStore.UpsertTaskPermission(ctx, postgres.TaskPermissionGrant{
		TenantID: tenant.ID, ProjectID: project.ID, TaskID: "t-client-opaque",
		PrincipalID: agentPrincipal.ID, CanRead: false, CanWrite: true,
	}); err != nil {
		t.Fatalf("UpsertTaskPermission failed: %v", err)
	}

	auth := provisioning.NewAuthenticator(clientPrefixLookup{s: provStore}, store.Audit())
	taskPerms := store.TaskPermissions()
	opaqueStore := store.OpaqueRequests(60 * time.Second)
	l1 := cachel1.NewCognitiveGraph(store.Search())
	gov := governor.NewPowerGovernor(governor.Config{Profile: governor.ProfileBalanced})

	capSvc, _ := capture.NewService(store, store.Audit(), cCapTask{r: taskPerms}, cCapOpaque{o: opaqueStore}, nil)
	capSvc.SetGraphNotifier(l1)
	retSvc, _ := retrieval.NewService(store, store.Audit(), cRetTask{r: taskPerms}, cOpaqueStatus{o: opaqueStore}, nil)
	retSvc.SetHybridDeps(store.Search(), nil, l1)
	ctxSvc, _ := contextbuilder.NewService(retSvc, store.Audit(), nil)
	fbSvc, _ := feedback.NewService(store, store.Audit(), cFbTask{r: taskPerms}, cFbOpaque{o: opaqueStore}, capSvc, nil)
	fbSvc.SetGraphNotifier(l1)
	conflictSvc, _ := conflicts.NewService(store, store.Conflicts(), store.Audit(), taskPerms, l1, nil)
	consSvc, _ := consolidation.NewService(store, store.Consolidation(), nil, nil, store.Audit(), l1, gov, nil)

	router, _, err := rest.NewRouter(rest.RouterDeps{
		Auth: auth, Capture: capSvc, Retrieval: retSvc, Context: ctxSvc, Feedback: fbSvc,
		Conflicts: conflictSvc, Consolidation: consSvc, Governor: gov, DB: pool, L1Stats: l1, Version: "test",
	})
	if err != nil {
		t.Fatalf("rest.NewRouter failed: %v", err)
	}
	ts := httptest.NewServer(router)
	defer ts.Close()

	// Unauthenticated client returns *APIError 401
	bad := romeroclient.New(ts.URL, "invalid-key")
	_, err = bad.SystemStatus(ctx)
	var apiErr *romeroclient.APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 *APIError, got %v", err)
	}

	c := romeroclient.New(ts.URL, agentKey)
	status, err := c.SystemStatus(ctx)
	if err != nil || status.Status != "ok" || status.Governor == nil || status.Governor.Profile == "" {
		t.Fatalf("SystemStatus failed: status=%+v err=%v", status, err)
	}

	// Opaque task_shared capture -> 202 Accepted; status remains opaque (404) while caller has CanRead=false
	opaqueCap, err := c.CaptureMemory(ctx, romeroclient.CaptureRequest{
		ProjectID: &project.ID, TaskID: "t-client-opaque", Type: "episodic", ScopeType: "task_shared",
		Summary: "Opaque write", Content: "Opaque body", EpistemicStatus: "hypothesis",
		Importance: 0.5, SourceType: "agent_execution", ExternalSourceID: "cli-1",
	})
	if err != nil || !opaqueCap.Accepted || opaqueCap.RequestID == nil {
		t.Fatalf("expected 202 Accepted opaque receipt, got %+v err=%v", opaqueCap, err)
	}
	_, reqErr := c.GetRequestStatus(ctx, *opaqueCap.RequestID)
	var reqAPIErr *romeroclient.APIError
	if !errors.As(reqErr, &reqAPIErr) || reqAPIErr.StatusCode != http.StatusNotFound {
		t.Fatalf("expected opaque 404 on GetRequestStatus without CanRead, got %v", reqErr)
	}

	// Sync capture + GetMemory + Update + Search + Context + Subgraph + Feedback + Archive
	syncCap, err := c.CaptureMemory(ctx, romeroclient.CaptureRequest{
		ProjectID: &project.ID, Type: "semantic", ScopeType: "project_shared",
		Summary: "ROMERO client wire verification", Content: "Real PostgreSQL 17 wire check",
		EpistemicStatus: "observed", Importance: 0.85, SourceType: "agent_execution", ExternalSourceID: "cli-2",
	})
	if err != nil || syncCap.Accepted || syncCap.Memory == nil || syncCap.Memory.ScopeType != "project_shared" {
		t.Fatalf("sync CaptureMemory failed: %+v err=%v", syncCap, err)
	}
	memID := syncCap.Memory.ID

	gotMem, err := c.GetMemory(ctx, memID, nil)
	if err != nil || gotMem.Memory.ScopeType != "project_shared" || !strings.Contains(gotMem.UntrustedXMLEnvelope, `untrusted_content="true"`) {
		t.Fatalf("GetMemory wire decode failed: %+v err=%v", gotMem, err)
	}

	if _, err := c.UpdateMemory(ctx, memID, romeroclient.UpdateMemoryRequest{
		Summary: "ROMERO client wire verification v2", Content: "Updated PG17 body", ChangeReason: "test",
	}); err != nil {
		t.Fatalf("UpdateMemory failed: %v", err)
	}
	if sr, err := c.SearchMemories(ctx, romeroclient.SearchRequest{Query: "ROMERO client wire", Limit: 5}); err != nil || sr.TotalFound == 0 {
		t.Fatalf("SearchMemories failed: sr=%+v err=%v", sr, err)
	}
	if bc, err := c.BuildContext(ctx, romeroclient.BuildContextRequest{Query: "ROMERO client", MaxTokens: 1000}); err != nil || !strings.Contains(bc.CombinedXML, "romero_memory") {
		t.Fatalf("BuildContext failed: bc=%+v err=%v", bc, err)
	}
	if sg, err := c.GetSubgraph(ctx, memID, 1); err != nil || sg.RootMemoryID != memID {
		t.Fatalf("GetSubgraph failed: sg=%+v err=%v", sg, err)
	}
	if fb, err := c.RecordFeedback(ctx, romeroclient.FeedbackRequest{MemoryID: memID, Signal: "helpful_success"}); err != nil || fb.NewConfidence <= 0 {
		t.Fatalf("RecordFeedback failed: fb=%+v err=%v", fb, err)
	}
	if _, err := c.ListConflicts(ctx, nil); err != nil {
		t.Fatalf("ListConflicts failed: %v", err)
	}
	if _, err := c.ArchiveMemory(ctx, memID, romeroclient.ArchiveMemoryRequest{Reason: "cleanup"}); err != nil {
		t.Fatalf("ArchiveMemory failed: %v", err)
	}

	// Admin client Consolidate dry-run
	adminClient := romeroclient.New(ts.URL, adminKey)
	if _, err := adminClient.Consolidate(ctx, romeroclient.ConsolidateRequest{DryRun: true}); err != nil {
		t.Fatalf("Consolidate dry-run failed: %v", err)
	}
}
