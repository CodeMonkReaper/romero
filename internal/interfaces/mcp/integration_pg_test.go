package mcp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
	contextbuilder "github.com/romero-project/romero/internal/application/context"
	"github.com/romero-project/romero/internal/application/feedback"
	"github.com/romero-project/romero/internal/application/provisioning"
	"github.com/romero-project/romero/internal/application/retrieval"
	"github.com/romero-project/romero/internal/domain/governor"
	"github.com/romero-project/romero/internal/domain/identity"
	"github.com/romero-project/romero/internal/infrastructure/cachel1"
	"github.com/romero-project/romero/internal/infrastructure/persistence/postgres"
	"github.com/romero-project/romero/internal/interfaces/mcp"
	"github.com/romero-project/romero/migrations"
)

const defaultPGTestDSN = "postgres://romero:romero_local_secret@127.0.0.1:54329/romero?sslmode=disable"

func openMCPPGPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("ROMERO_TEST_DSN"))
	explicit := dsn != ""
	if !explicit {
		dsn = defaultPGTestDSN
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		if !explicit {
			t.Skipf("skipping PG integration test: %v", err)
		}
		t.Fatalf("pgxpool.New failed: %v", err)
	}
	t.Cleanup(pool.Close)

	if err := pool.Ping(ctx); err != nil {
		if !explicit {
			t.Skipf("skipping PG integration test (postgres unreachable): %v", err)
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

type pgPrefixLookup struct{ s *postgres.ProvisioningStore }

func (l pgPrefixLookup) LookupPrincipalsByPrefix(ctx context.Context, prefix string) ([]provisioning.PrincipalAuthCandidate, error) {
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

type pgCapTask struct {
	r *postgres.TaskPermissionRepository
}

func (a pgCapTask) EvaluateTaskAccess(ctx context.Context, q identity.TaskPermissionQuery) (capture.TaskAccessSnapshot, error) {
	s, err := a.r.EvaluateTaskAccess(ctx, q)
	return capture.TaskAccessSnapshot{CanRead: s.CanRead, CanWrite: s.CanWrite}, err
}

type pgRetTask struct {
	r *postgres.TaskPermissionRepository
}

func (a pgRetTask) EvaluateTaskAccess(ctx context.Context, q identity.TaskPermissionQuery) (retrieval.TaskAccessSnapshot, error) {
	s, err := a.r.EvaluateTaskAccess(ctx, q)
	return retrieval.TaskAccessSnapshot{CanRead: s.CanRead, CanWrite: s.CanWrite}, err
}

type pgFbTask struct {
	r *postgres.TaskPermissionRepository
}

func (a pgFbTask) EvaluateTaskAccess(ctx context.Context, q identity.TaskPermissionQuery) (feedback.TaskAccessSnapshot, error) {
	s, err := a.r.EvaluateTaskAccess(ctx, q)
	return feedback.TaskAccessSnapshot{CanRead: s.CanRead, CanWrite: s.CanWrite}, err
}

type pgCapOpaque struct{ o *postgres.OpaqueRequestStore }

func (a pgCapOpaque) Enqueue(ctx context.Context, req capture.OpaqueRequest) (capture.OpaqueReceipt, error) {
	rec, err := a.o.Enqueue(ctx, postgres.OpaqueRequest{
		RequestID: req.RequestID, TenantID: req.TenantID, ProjectID: req.ProjectID, TaskID: req.TaskID,
		PrincipalID: req.PrincipalID, Operation: postgres.OpaqueOperation(req.Operation), TargetMemoryID: req.TargetMemoryID,
		PayloadVersion: req.PayloadVersion, Payload: req.Payload, RequestSHA256: req.RequestSHA256,
		IdempotencyKey: req.IdempotencyKey,
	})
	return capture.OpaqueReceipt{RequestID: rec.RequestID}, err
}

type pgFbOpaque struct{ o *postgres.OpaqueRequestStore }

func (a pgFbOpaque) EnqueueFeedback(ctx context.Context, req feedback.OpaqueFeedbackRequest) (feedback.OpaqueReceipt, error) {
	rec, err := a.o.Enqueue(ctx, postgres.OpaqueRequest{
		RequestID: req.RequestID, TenantID: req.TenantID, ProjectID: req.ProjectID, TaskID: req.TaskID,
		PrincipalID: req.PrincipalID, Operation: postgres.OpaqueOpFeedback, TargetMemoryID: &req.TargetMemoryID,
		PayloadVersion: 1, Payload: json.RawMessage(req.Payload), RequestSHA256: req.RequestSHA256,
		IdempotencyKey: req.IdempotencyKey,
	})
	return feedback.OpaqueReceipt{RequestID: rec.RequestID}, err
}

type pgOpaqueStatus struct{ o *postgres.OpaqueRequestStore }

func (a pgOpaqueStatus) GetStatus(ctx context.Context, tenantID, requestID uuid.UUID) (*retrieval.OpaqueRequestMetadata, error) {
	m, err := a.o.GetStatus(ctx, tenantID, requestID)
	if err != nil {
		return nil, err
	}
	return &retrieval.OpaqueRequestMetadata{
		RequestID: m.RequestID, TenantID: m.TenantID, ProjectID: m.ProjectID, TaskID: m.TaskID, Status: string(m.Status),
	}, nil
}

func TestMCP_IntegrationPostgresZeroTrustAndTools(t *testing.T) {
	pool := openMCPPGPool(t)
	ctx := context.Background()

	store, err := postgres.NewStore(pool)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	provStore := store.Provisioning()

	tenant, err := provStore.CreateTenant(ctx, postgres.CreateTenantParams{
		ID: uuid.New(), Slug: "mcp-pg-tenant", Name: "MCP PG Tenant", IsActive: true,
	})
	if err != nil {
		t.Fatalf("CreateTenant failed: %v", err)
	}
	project, err := provStore.CreateProject(ctx, postgres.CreateProjectParams{
		ID: uuid.New(), TenantID: tenant.ID, Slug: "mcp-proj", Name: "MCP Project", IsActive: true,
	})
	if err != nil {
		t.Fatalf("CreateProject failed: %v", err)
	}
	agentA, err := provStore.CreateAgent(ctx, postgres.CreateAgentParams{
		ID: uuid.New(), TenantID: tenant.ID, ExternalKey: "agent-a", Name: "Agent A", RoleType: "worker", IsActive: true,
	})
	if err != nil {
		t.Fatalf("CreateAgent A failed: %v", err)
	}
	agentB, err := provStore.CreateAgent(ctx, postgres.CreateAgentParams{
		ID: uuid.New(), TenantID: tenant.ID, ExternalKey: "agent-b", Name: "Agent B", RoleType: "worker", IsActive: true,
	})
	if err != nil {
		t.Fatalf("CreateAgent B failed: %v", err)
	}

	pAID := uuid.New()
	keyA, prefixA, hashA, err := provisioning.GenerateAPIKey(identity.PrincipalAgent, pAID)
	if err != nil {
		t.Fatalf("GenerateAPIKey A failed: %v", err)
	}
	pBID := uuid.New()
	keyB, prefixB, hashB, err := provisioning.GenerateAPIKey(identity.PrincipalAgent, pBID)
	if err != nil {
		t.Fatalf("GenerateAPIKey B failed: %v", err)
	}
	pA, err := provStore.CreatePrincipal(ctx, postgres.CreatePrincipalParams{
		ID: pAID, TenantID: tenant.ID, AgentID: &agentA.ID, PrincipalType: identity.PrincipalAgent,
		Name: "Principal A", APIKeyPrefix: prefixA, APIKeySHA256: hashA,
		AllowedProjects: []uuid.UUID{project.ID},
		AllowedScopes:   []identity.ScopeType{identity.ScopeAgentPrivate, identity.ScopeTaskShared, identity.ScopeProjectShared},
		IsActive:        true,
	})
	if err != nil {
		t.Fatalf("CreatePrincipal A failed: %v", err)
	}
	if _, err := provStore.CreatePrincipal(ctx, postgres.CreatePrincipalParams{
		ID: pBID, TenantID: tenant.ID, AgentID: &agentB.ID, PrincipalType: identity.PrincipalAgent,
		Name: "Principal B", APIKeyPrefix: prefixB, APIKeySHA256: hashB,
		AllowedProjects: []uuid.UUID{project.ID},
		AllowedScopes:   []identity.ScopeType{identity.ScopeAgentPrivate, identity.ScopeProjectShared},
		IsActive:        true,
	}); err != nil {
		t.Fatalf("CreatePrincipal B failed: %v", err)
	}

	if err := provStore.UpsertTaskPermission(ctx, postgres.TaskPermissionGrant{
		TenantID: tenant.ID, ProjectID: project.ID, TaskID: "t-opaque",
		PrincipalID: pA.ID, CanRead: false, CanWrite: true,
	}); err != nil {
		t.Fatalf("UpsertTaskPermission failed: %v", err)
	}

	auth := provisioning.NewAuthenticator(pgPrefixLookup{s: provStore}, store.Audit())
	taskPerms := store.TaskPermissions()
	opaqueStore := store.OpaqueRequests(60 * time.Second)
	l1 := cachel1.NewCognitiveGraph(store.Search())
	gov := governor.NewPowerGovernor(governor.Config{Profile: governor.ProfileBalanced})

	capSvc, _ := capture.NewService(store, store.Audit(), pgCapTask{r: taskPerms}, pgCapOpaque{o: opaqueStore}, nil)
	capSvc.SetGraphNotifier(l1)
	retSvc, _ := retrieval.NewService(store, store.Audit(), pgRetTask{r: taskPerms}, pgOpaqueStatus{o: opaqueStore}, nil)
	retSvc.SetHybridDeps(store.Search(), nil, l1)
	ctxSvc, _ := contextbuilder.NewService(retSvc, store.Audit(), nil)
	fbSvc, _ := feedback.NewService(store, store.Audit(), pgFbTask{r: taskPerms}, pgFbOpaque{o: opaqueStore}, capSvc, nil)
	fbSvc.SetGraphNotifier(l1)

	mcpSrv, err := mcp.NewServer(mcp.Dependencies{
		Authenticator: auth, Capture: capSvc, Retrieval: retSvc, Context: ctxSvc, Feedback: fbSvc, Governor: gov,
	})
	if err != nil {
		t.Fatalf("mcp.NewServer failed: %v", err)
	}
	ts := httptest.NewServer(mcpSrv.HTTPHandler())
	defer ts.Close()

	callMCP := func(apiKey, payload string) (int, string) {
		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/mcp", strings.NewReader(payload))
		req.Header.Set("Authorization", "Bearer "+apiKey)
		resp, err := ts.Client().Do(req)
		if err != nil {
			t.Fatalf("POST /mcp failed: %v", err)
		}
		defer resp.Body.Close()
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(resp.Body)
		return resp.StatusCode, buf.String()
	}

	// 1. Opaque task_shared capture (AC-02/AC-03)
	code, body := callMCP(keyA, fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"romero_memory_save","arguments":{"project_id":%q,"task_id":"t-opaque","type":"episodic","scope_type":"task_shared","summary":"Opaque task note","content":"Secret task write","epistemic_status":"hypothesis","importance":0.6,"source_type":"agent_execution","external_source_id":"a1"}}}`, project.ID))
	if code != http.StatusOK || !strings.Contains(body, `"accepted":true`) {
		t.Fatalf("expected opaque accepted=true, got code=%d body=%s", code, body)
	}

	// 2. Agent A private + Agent B project_shared saves
	code, bodyPriv := callMCP(keyA, fmt.Sprintf(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"romero_memory_save","arguments":{"project_id":%q,"type":"procedural","scope_type":"agent_private","summary":"Agent A private rule","content":"Private detail","epistemic_status":"observed","importance":0.9,"source_type":"agent_execution","external_source_id":"a2"}}}`, project.ID))
	if code != http.StatusOK || !strings.Contains(bodyPriv, `"created":true`) {
		t.Fatalf("expected agent_private created=true, got code=%d body=%s", code, bodyPriv)
	}
	var privEnv struct {
		Result struct {
			StructuredContent struct {
				Memory struct {
					ID uuid.UUID `json:"ID"`
				} `json:"memory"`
			} `json:"structuredContent"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(bodyPriv), &privEnv); err != nil || privEnv.Result.StructuredContent.Memory.ID == uuid.Nil {
		t.Fatalf("failed to parse private memory ID: %v body=%s", err, bodyPriv)
	}
	privID := privEnv.Result.StructuredContent.Memory.ID

	code, bodyShared := callMCP(keyB, fmt.Sprintf(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"romero_memory_save","arguments":{"project_id":%q,"type":"semantic","scope_type":"project_shared","summary":"Shared Postgres architecture note","content":"PostgreSQL 17 handles hybrid search","epistemic_status":"observed","importance":0.8,"source_type":"agent_execution","external_source_id":"b1"}}}`, project.ID))
	if code != http.StatusOK || !strings.Contains(bodyShared, `"created":true`) {
		t.Fatalf("expected project_shared created=true, got code=%d body=%s", code, bodyShared)
	}
	var sharedEnv struct {
		Result struct {
			StructuredContent struct {
				Memory struct {
					ID uuid.UUID `json:"ID"`
				} `json:"memory"`
			} `json:"structuredContent"`
		} `json:"result"`
	}
	_ = json.Unmarshal([]byte(bodyShared), &sharedEnv)
	sharedID := sharedEnv.Result.StructuredContent.Memory.ID

	// 3. Search as Agent B: cross-agent agent_private isolation + memory_types/min_confidence filters without content (AC-04)
	code, privSearch := callMCP(keyB, `{"jsonrpc":"2.0","id":40,"method":"tools/call","params":{"name":"romero_memory_search","arguments":{"query":"Agent A private rule","limit":5}}}`)
	if code != http.StatusOK || strings.Contains(privSearch, privID.String()) || strings.Contains(privSearch, "Private detail") {
		t.Fatalf("search leaked cross-agent private memory: code=%d body=%s", code, privSearch)
	}
	code, searchBody := callMCP(keyB, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"romero_memory_search","arguments":{"query":"Postgres architecture","memory_types":["semantic"],"min_confidence":0.5,"limit":5}}}`)
	if code != http.StatusOK || strings.Contains(searchBody, "Private detail") || strings.Contains(searchBody, "PostgreSQL 17 handles hybrid search") {
		t.Fatalf("search leaked content or failed: code=%d body=%s", code, searchBody)
	}
	if !strings.Contains(searchBody, sharedID.String()) {
		t.Fatalf("expected sharedID in search hits, got %s", searchBody)
	}

	// 4. Batch get as Agent B: [sharedID, privID, randomMissingID] -> opacity + untrusted_content + audit_events denied (AC-05)
	missingID := uuid.New()
	code, getBody := callMCP(keyB, fmt.Sprintf(`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"romero_memory_get","arguments":{"memory_ids":[%q,%q,%q]}}}`, sharedID, privID, missingID))
	if code != http.StatusOK || !strings.Contains(getBody, `untrusted_content=\"true\"`) || strings.Contains(getBody, "Private detail") || strings.Count(getBody, `"status":"not_found"`) != 2 {
		t.Fatalf("unexpected get batch response: code=%d body=%s", code, getBody)
	}
	var deniedCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM audit_events WHERE outcome = 'denied'`).Scan(&deniedCount); err != nil || deniedCount == 0 {
		t.Fatalf("expected denied audit_events entry for cross-agent private read, got count=%d err=%v", deniedCount, err)
	}

	// 5. Context + Feedback + Stdio round-trip (AC-07)
	code, ctxBody := callMCP(keyB, `{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"romero_memory_context","arguments":{"query":"Postgres","max_tokens":1000}}}`)
	if code != http.StatusOK || !strings.Contains(ctxBody, "romero_memory") || !strings.Contains(ctxBody, `untrusted_content=\"true\"`) {
		t.Fatalf("context failed: code=%d body=%s", code, ctxBody)
	}
	code, fbBody := callMCP(keyB, fmt.Sprintf(`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"romero_memory_feedback","arguments":{"memory_id":%q,"signal":"helpful_success"}}}`, sharedID))
	if code != http.StatusOK || !strings.Contains(fbBody, `"new_confidence"`) {
		t.Fatalf("feedback failed: code=%d body=%s", code, fbBody)
	}

	var stdioOut bytes.Buffer
	if err := mcpSrv.ServeStdio(ctx, strings.NewReader(`{"jsonrpc":"2.0","id":8,"method":"tools/list"}`+"\n"), &stdioOut, keyB); err != nil {
		t.Fatalf("ServeStdio PG test failed: %v", err)
	}
	if !strings.Contains(stdioOut.String(), "romero_memory_get") {
		t.Fatalf("unexpected stdio output: %s", stdioOut.String())
	}
}
