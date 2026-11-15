// Package testpg provides a live PostgreSQL 17 + pgvector test harness for
// integration and Zero-Trust security suites without mocked database repositories.
package testpg

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/romero-project/romero/internal/application/capture"
	contextbuilder "github.com/romero-project/romero/internal/application/context"
	"github.com/romero-project/romero/internal/application/feedback"
	"github.com/romero-project/romero/internal/application/jobs"
	"github.com/romero-project/romero/internal/application/provisioning"
	"github.com/romero-project/romero/internal/application/requests"
	"github.com/romero-project/romero/internal/application/retrieval"
	"github.com/romero-project/romero/internal/domain/identity"
	"github.com/romero-project/romero/internal/domain/memory"
	"github.com/romero-project/romero/internal/infrastructure/cachel1"
	"github.com/romero-project/romero/internal/infrastructure/neural"
	"github.com/romero-project/romero/internal/infrastructure/persistence/postgres"
	"github.com/romero-project/romero/internal/interfaces/cli"
	"github.com/romero-project/romero/internal/interfaces/rest"
	"github.com/romero-project/romero/migrations"
)

const defaultTestDSN = "postgres://romero:romero@romero-t009-pg:5432/romero?sslmode=disable"

// TestDSN returns ROMERO_TEST_DSN or the ephemeral Docker PG17 default.
func TestDSN() string {
	if v := strings.TrimSpace(os.Getenv("ROMERO_TEST_DSN")); v != "" {
		return v
	}
	return defaultTestDSN
}

// OpenMigratedPool connects to PostgreSQL 17, applies Goose migrations, and truncates all tables.
func OpenMigratedPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, TestDSN())
	if err != nil {
		t.Fatalf("pgxpool.New failed: %v", err)
	}
	t.Cleanup(pool.Close)

	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("postgres ping failed (%s): %v", TestDSN(), err)
	}

	sqlDB := stdlib.OpenDBFromPool(pool)
	defer func() { _ = sqlDB.Close() }()

	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatalf("goose.SetDialect failed: %v", err)
	}
	if err := goose.UpContext(ctx, sqlDB, "."); err != nil {
		t.Fatalf("goose.UpContext failed: %v", err)
	}

	_, err = pool.Exec(ctx, `TRUNCATE TABLE
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
		t.Fatalf("truncate test database failed: %v", err)
	}
	return pool
}

// Env wires the full live-Postgres application stack, HTTP REST server, and opaque worker.
type Env struct {
	Pool      *pgxpool.Pool
	Store     *postgres.Store
	ProvSvc   *provisioning.Service
	Auth      *provisioning.Authenticator
	CapSvc    *capture.Service
	RetSvc    *retrieval.Service
	CtxSvc    *contextbuilder.Service
	FbSvc     *feedback.Service
	L1Graph   *cachel1.CognitiveGraph
	EmbWorker *jobs.EmbeddingWorker
	Worker    *requests.Worker
	Queue     *pgWorkerQueue
	Server    *httptest.Server

	nowMu sync.Mutex
	now   time.Time
}

// NewEnv constructs a full live-Postgres test environment with controllable clock.
func NewEnv(t *testing.T, pool *pgxpool.Pool) *Env {
	t.Helper()
	store, err := postgres.NewStore(pool)
	if err != nil {
		t.Fatalf("postgres.NewStore: %v", err)
	}

	env := &Env{
		Pool:  pool,
		Store: store,
		now:   time.Now().UTC().Truncate(time.Microsecond),
	}
	clock := func() time.Time {
		env.nowMu.Lock()
		defer env.nowMu.Unlock()
		return env.now
	}

	repo := &pgProvisioningAdapter{store: pgStoreWrapper{ProvisioningStore: store.Provisioning()}}
	audit := store.Audit()
	taskPerms := store.TaskPermissions()
	opaqueStore := store.OpaqueRequests(60 * time.Second)

	env.ProvSvc = provisioning.NewService(repo, repo, audit)
	env.Auth = provisioning.NewAuthenticator(repo, audit)

	capSvc, err := capture.NewService(store, audit, pgCaptureAccess{r: taskPerms}, pgCaptureOpaque{o: opaqueStore}, clock)
	if err != nil {
		t.Fatalf("capture.NewService: %v", err)
	}
	retSvc, err := retrieval.NewService(store, audit, pgRetrievalAccess{r: taskPerms}, pgOpaqueStatus{o: opaqueStore}, clock)
	if err != nil {
		t.Fatalf("retrieval.NewService: %v", err)
	}
	fbSvc, err := feedback.NewService(store, audit, pgFeedbackAccess{r: taskPerms}, pgFeedbackOpaque{o: opaqueStore}, capSvc, clock)
	if err != nil {
		t.Fatalf("feedback.NewService: %v", err)
	}

	l1Graph := cachel1.NewCognitiveGraph(store.Search())
	defaultEmbedder := neural.NewBGEM3Client(neural.BGEM3Config{Enabled: false})

	capSvc.SetGraphNotifier(l1Graph)
	fbSvc.SetGraphNotifier(l1Graph)
	retSvc.SetHybridDeps(store.Search(), defaultEmbedder, l1Graph)

	ctxSvc, err := contextbuilder.NewService(retSvc, audit, clock)
	if err != nil {
		t.Fatalf("contextbuilder.NewService: %v", err)
	}

	env.CapSvc = capSvc
	env.RetSvc = retSvc
	env.CtxSvc = ctxSvc
	env.FbSvc = fbSvc
	env.L1Graph = l1Graph
	env.EmbWorker = jobs.NewEmbeddingWorker(store.JobQueue(), store.Search(), store.Embeddings(), defaultEmbedder, l1Graph, jobs.EmbeddingWorkerConfig{
		PollInterval: 25 * time.Millisecond,
	}, nil)

	env.Queue = &pgWorkerQueue{o: opaqueStore}
	exec := &requests.ServiceExecutor{Capture: capSvc, Feedback: fbSvc}
	worker, err := requests.NewWorker(
		env.Queue,
		pgPrincipalResolver{repo: repo},
		exec,
		requests.Config{
			PollInterval:  25 * time.Millisecond,
			RenewInterval: 20 * time.Second,
			PurgeInterval: time.Hour,
			Retention:     30 * 24 * time.Hour,
		},
	)
	if err != nil {
		t.Fatalf("requests.NewWorker: %v", err)
	}
	env.Worker = worker

	handler, _, err := rest.NewRouter(rest.RouterDeps{
		Auth:      env.Auth,
		Capture:   capSvc,
		Retrieval: retSvc,
		Context:   ctxSvc,
		Feedback:  fbSvc,
		DB:        pool,
		L1Stats:   l1Graph,
		Version:   "0.2.0-test",
	})
	if err != nil {
		t.Fatalf("rest.NewRouter: %v", err)
	}
	env.Server = httptest.NewServer(handler)
	t.Cleanup(env.Server.Close)
	return env
}

// SetEmbedder reconfigures RetrievalService and EmbeddingWorker with a live or mock EmbeddingProvider.
func (e *Env) SetEmbedder(p memory.EmbeddingProvider) {
	e.RetSvc.SetHybridDeps(e.Store.Search(), p, e.L1Graph)
	e.EmbWorker = jobs.NewEmbeddingWorker(e.Store.JobQueue(), e.Store.Search(), e.Store.Embeddings(), p, e.L1Graph, jobs.EmbeddingWorkerConfig{
		PollInterval: 25 * time.Millisecond,
	}, nil)
}

// Now returns the current controllable test clock.
func (e *Env) Now() time.Time {
	e.nowMu.Lock()
	defer e.nowMu.Unlock()
	return e.now
}

// SetNow sets the controllable test clock.
func (e *Env) SetNow(ts time.Time) {
	e.nowMu.Lock()
	defer e.nowMu.Unlock()
	e.now = ts.UTC()
}

// Advance moves the controllable test clock forward by d.
func (e *Env) Advance(d time.Duration) time.Time {
	e.nowMu.Lock()
	defer e.nowMu.Unlock()
	e.now = e.now.Add(d)
	return e.now
}

// RunCLI executes the real Cobra CLI against the live PostgreSQL store.
func (e *Env) RunCLI(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out, errBuf bytes.Buffer
	deps := cli.Dependencies{
		OpenProvisioner: func(context.Context, string) (cli.ProvisioningClient, func(), error) {
			return cli.ProvisioningClient{
				Service: e.ProvSvc,
				Auth:    e.Auth,
			}, func() {}, nil
		},
	}
	cmd := cli.NewRootCmd(deps)
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	if err != nil {
		return out.String() + errBuf.String(), err
	}
	return out.String(), nil
}

// DoJSON sends an HTTP request to the live REST server and returns the response and body.
func (e *Env) DoJSON(t *testing.T, method, path, apiKey string, payload any) (*http.Response, []byte) {
	t.Helper()
	var body io.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("json.Marshal: %v", err)
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, e.Server.URL+path, body)
	if err != nil {
		t.Fatalf("http.NewRequest: %v", err)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := e.Server.Client().Do(req)
	if err != nil {
		t.Fatalf("http.Do %s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp, raw
}

type pgBackend interface {
	ExecTx(ctx context.Context, fn func(tx pgBackend, audit memory.AuditLogger) error) error
	CreateTenant(ctx context.Context, arg postgres.CreateTenantParams) (postgres.TenantRecord, error)
	GetTenantByID(ctx context.Context, id uuid.UUID) (*postgres.TenantRecord, error)
	CreateProject(ctx context.Context, arg postgres.CreateProjectParams) (postgres.ProjectRecord, error)
	GetProjectByID(ctx context.Context, tenantID, id uuid.UUID) (*postgres.ProjectRecord, error)
	CountTenantProjects(ctx context.Context, tenantID uuid.UUID, ids []uuid.UUID) (int64, error)
	CreateAgent(ctx context.Context, arg postgres.CreateAgentParams) (postgres.AgentRecord, error)
	GetAgentByID(ctx context.Context, tenantID, id uuid.UUID) (*postgres.AgentRecord, error)
	CreatePrincipal(ctx context.Context, arg postgres.CreatePrincipalParams) (postgres.PrincipalRecord, error)
	GetPrincipalByID(ctx context.Context, tenantID, id uuid.UUID) (*postgres.PrincipalRecord, error)
	LookupPrincipalsByPrefix(ctx context.Context, prefix string) ([]postgres.PrincipalAuthCandidate, error)
	UpsertTaskPermission(ctx context.Context, grant postgres.TaskPermissionGrant) error
}

type pgStoreWrapper struct{ *postgres.ProvisioningStore }

func (w pgStoreWrapper) ExecTx(ctx context.Context, fn func(tx pgBackend, audit memory.AuditLogger) error) error {
	return w.ProvisioningStore.ExecTx(ctx, func(tx *postgres.ProvisioningStore, audit memory.AuditLogger) error {
		return fn(pgStoreWrapper{ProvisioningStore: tx}, audit)
	})
}

type pgProvisioningAdapter struct{ store pgBackend }

func mapPGError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, postgres.ErrAlreadyExists):
		return fmt.Errorf("%w: %v", provisioning.ErrAlreadyExists, err)
	case errors.Is(err, postgres.ErrInvalidInput):
		return fmt.Errorf("%w: %v", provisioning.ErrInvalidInput, err)
	default:
		return err
	}
}

func (a *pgProvisioningAdapter) ExecTx(ctx context.Context, fn func(provisioning.Repository, memory.AuditLogger) error) error {
	return mapPGError(a.store.ExecTx(ctx, func(tx pgBackend, audit memory.AuditLogger) error {
		return fn(&pgProvisioningAdapter{store: tx}, audit)
	}))
}

func (a *pgProvisioningAdapter) CreateTenant(ctx context.Context, arg provisioning.CreateTenantParams) (provisioning.Tenant, error) {
	rec, err := a.store.CreateTenant(ctx, postgres.CreateTenantParams{
		ID: arg.ID, Slug: arg.Slug, Name: arg.Name, IsActive: arg.IsActive, Settings: arg.Settings,
	})
	if err != nil {
		return provisioning.Tenant{}, mapPGError(err)
	}
	return provisioning.Tenant{ID: rec.ID, Slug: rec.Slug, Name: rec.Name, IsActive: rec.IsActive}, nil
}

func (a *pgProvisioningAdapter) GetTenantByID(ctx context.Context, id uuid.UUID) (*provisioning.Tenant, error) {
	rec, err := a.store.GetTenantByID(ctx, id)
	if err != nil {
		return nil, mapPGError(err)
	}
	return &provisioning.Tenant{ID: rec.ID, Slug: rec.Slug, Name: rec.Name, IsActive: rec.IsActive}, nil
}

func (a *pgProvisioningAdapter) CreateProject(ctx context.Context, arg provisioning.CreateProjectParams) (provisioning.Project, error) {
	rec, err := a.store.CreateProject(ctx, postgres.CreateProjectParams{
		ID: arg.ID, TenantID: arg.TenantID, Slug: arg.Slug, Name: arg.Name,
		RepositoryURL: arg.RepositoryURL, Description: arg.Description, IsActive: arg.IsActive, Metadata: arg.Metadata,
	})
	if err != nil {
		return provisioning.Project{}, mapPGError(err)
	}
	return provisioning.Project{ID: rec.ID, TenantID: rec.TenantID, Slug: rec.Slug, Name: rec.Name, IsActive: rec.IsActive}, nil
}

func (a *pgProvisioningAdapter) GetProjectByID(ctx context.Context, tenantID, id uuid.UUID) (*provisioning.Project, error) {
	rec, err := a.store.GetProjectByID(ctx, tenantID, id)
	if err != nil {
		return nil, mapPGError(err)
	}
	return &provisioning.Project{ID: rec.ID, TenantID: rec.TenantID, Slug: rec.Slug, Name: rec.Name, IsActive: rec.IsActive}, nil
}

func (a *pgProvisioningAdapter) CountTenantProjects(ctx context.Context, tenantID uuid.UUID, ids []uuid.UUID) (int64, error) {
	c, err := a.store.CountTenantProjects(ctx, tenantID, ids)
	return c, mapPGError(err)
}

func (a *pgProvisioningAdapter) CreateAgent(ctx context.Context, arg provisioning.CreateAgentParams) (provisioning.Agent, error) {
	rec, err := a.store.CreateAgent(ctx, postgres.CreateAgentParams{
		ID: arg.ID, TenantID: arg.TenantID, ExternalKey: arg.ExternalKey, Name: arg.Name,
		RoleType: arg.RoleType, Capabilities: arg.Capabilities, IsActive: arg.IsActive, Metadata: arg.Metadata,
	})
	if err != nil {
		return provisioning.Agent{}, mapPGError(err)
	}
	return provisioning.Agent{ID: rec.ID, TenantID: rec.TenantID, ExternalKey: rec.ExternalKey, Name: rec.Name, RoleType: rec.RoleType, IsActive: rec.IsActive}, nil
}

func (a *pgProvisioningAdapter) GetAgentByID(ctx context.Context, tenantID, id uuid.UUID) (*provisioning.Agent, error) {
	rec, err := a.store.GetAgentByID(ctx, tenantID, id)
	if err != nil {
		return nil, mapPGError(err)
	}
	return &provisioning.Agent{ID: rec.ID, TenantID: rec.TenantID, ExternalKey: rec.ExternalKey, Name: rec.Name, RoleType: rec.RoleType, IsActive: rec.IsActive}, nil
}

func (a *pgProvisioningAdapter) CreatePrincipal(ctx context.Context, arg provisioning.CreatePrincipalParams) (provisioning.Principal, error) {
	rec, err := a.store.CreatePrincipal(ctx, postgres.CreatePrincipalParams{
		ID: arg.ID, TenantID: arg.TenantID, AgentID: arg.AgentID, PrincipalType: arg.PrincipalType,
		Name: arg.Name, APIKeyPrefix: arg.APIKeyPrefix, APIKeySHA256: arg.APIKeySHA256,
		AllowedProjects: arg.AllowedProjects, AllowedScopes: arg.AllowedScopes,
		IsActive: arg.IsActive, ExpiresAt: arg.ExpiresAt, IsAdmin: arg.IsAdmin,
	})
	if err != nil {
		return provisioning.Principal{}, mapPGError(err)
	}
	return mapPrincipal(rec), nil
}

func (a *pgProvisioningAdapter) GetPrincipalByID(ctx context.Context, tenantID, id uuid.UUID) (*provisioning.Principal, error) {
	rec, err := a.store.GetPrincipalByID(ctx, tenantID, id)
	if err != nil {
		return nil, mapPGError(err)
	}
	p := mapPrincipal(*rec)
	return &p, nil
}

func (a *pgProvisioningAdapter) LookupPrincipalsByPrefix(ctx context.Context, prefix string) ([]provisioning.PrincipalAuthCandidate, error) {
	rows, err := a.store.LookupPrincipalsByPrefix(ctx, prefix)
	if err != nil {
		return nil, mapPGError(err)
	}
	out := make([]provisioning.PrincipalAuthCandidate, 0, len(rows))
	for _, r := range rows {
		out = append(out, provisioning.PrincipalAuthCandidate{
			Principal:    mapPrincipal(r.Principal),
			APIKeySHA256: r.APIKeySHA256,
			TenantActive: r.TenantActive,
			AgentActive:  r.AgentActive,
		})
	}
	return out, nil
}

func (a *pgProvisioningAdapter) UpsertTaskPermission(ctx context.Context, grant provisioning.TaskPermissionGrant) error {
	return mapPGError(a.store.UpsertTaskPermission(ctx, postgres.TaskPermissionGrant{
		TenantID: grant.TenantID, ProjectID: grant.ProjectID, TaskID: grant.TaskID,
		PrincipalID: grant.PrincipalID, CanRead: grant.CanRead, CanWrite: grant.CanWrite,
	}))
}

func mapPrincipal(r postgres.PrincipalRecord) provisioning.Principal {
	return provisioning.Principal{
		ID: r.ID, TenantID: r.TenantID, AgentID: r.AgentID, PrincipalType: r.PrincipalType,
		Name: r.Name, APIKeyPrefix: r.APIKeyPrefix, AllowedProjects: r.AllowedProjects,
		AllowedScopes: r.AllowedScopes, IsActive: r.IsActive, ExpiresAt: r.ExpiresAt,
		CreatedAt: r.CreatedAt, IsAdmin: r.IsAdmin,
	}
}

type pgCaptureAccess struct {
	r *postgres.TaskPermissionRepository
}

func (a pgCaptureAccess) EvaluateTaskAccess(ctx context.Context, q identity.TaskPermissionQuery) (capture.TaskAccessSnapshot, error) {
	s, err := a.r.EvaluateTaskAccess(ctx, q)
	return capture.TaskAccessSnapshot{CanRead: s.CanRead, CanWrite: s.CanWrite}, err
}

type pgRetrievalAccess struct {
	r *postgres.TaskPermissionRepository
}

func (a pgRetrievalAccess) EvaluateTaskAccess(ctx context.Context, q identity.TaskPermissionQuery) (retrieval.TaskAccessSnapshot, error) {
	s, err := a.r.EvaluateTaskAccess(ctx, q)
	return retrieval.TaskAccessSnapshot{CanRead: s.CanRead, CanWrite: s.CanWrite}, err
}

type pgFeedbackAccess struct {
	r *postgres.TaskPermissionRepository
}

func (a pgFeedbackAccess) EvaluateTaskAccess(ctx context.Context, q identity.TaskPermissionQuery) (feedback.TaskAccessSnapshot, error) {
	s, err := a.r.EvaluateTaskAccess(ctx, q)
	return feedback.TaskAccessSnapshot{CanRead: s.CanRead, CanWrite: s.CanWrite}, err
}

type pgCaptureOpaque struct{ o *postgres.OpaqueRequestStore }

func (a pgCaptureOpaque) Enqueue(ctx context.Context, req capture.OpaqueRequest) (capture.OpaqueReceipt, error) {
	rec, err := a.o.Enqueue(ctx, postgres.OpaqueRequest{
		RequestID:      req.RequestID,
		TenantID:       req.TenantID,
		ProjectID:      req.ProjectID,
		TaskID:         req.TaskID,
		PrincipalID:    req.PrincipalID,
		Operation:      postgres.OpaqueOperation(req.Operation),
		TargetMemoryID: req.TargetMemoryID,
		PayloadVersion: req.PayloadVersion,
		Payload:        req.Payload,
		RequestSHA256:  req.RequestSHA256,
		IdempotencyKey: req.IdempotencyKey,
	})
	if errors.Is(err, postgres.ErrIdempotencyConflict) {
		return capture.OpaqueReceipt{}, capture.ErrConflict
	}
	return capture.OpaqueReceipt{RequestID: rec.RequestID}, err
}

type pgFeedbackOpaque struct{ o *postgres.OpaqueRequestStore }

func (a pgFeedbackOpaque) EnqueueFeedback(ctx context.Context, req feedback.OpaqueFeedbackRequest) (feedback.OpaqueReceipt, error) {
	rec, err := a.o.Enqueue(ctx, postgres.OpaqueRequest{
		RequestID:      req.RequestID,
		TenantID:       req.TenantID,
		ProjectID:      req.ProjectID,
		TaskID:         req.TaskID,
		PrincipalID:    req.PrincipalID,
		Operation:      postgres.OpaqueOpFeedback,
		TargetMemoryID: &req.TargetMemoryID,
		PayloadVersion: 1,
		Payload:        json.RawMessage(req.Payload),
		RequestSHA256:  req.RequestSHA256,
		IdempotencyKey: req.IdempotencyKey,
	})
	if errors.Is(err, postgres.ErrIdempotencyConflict) {
		return feedback.OpaqueReceipt{}, feedback.ErrConflict
	}
	return feedback.OpaqueReceipt{RequestID: rec.RequestID}, err
}

type pgOpaqueStatus struct{ o *postgres.OpaqueRequestStore }

func (a pgOpaqueStatus) GetStatus(ctx context.Context, tenantID, requestID uuid.UUID) (*retrieval.OpaqueRequestMetadata, error) {
	m, err := a.o.GetStatus(ctx, tenantID, requestID)
	if err != nil {
		if errors.Is(err, memory.ErrNotFound) {
			return nil, retrieval.ErrNotFound
		}
		return nil, err
	}
	return &retrieval.OpaqueRequestMetadata{
		RequestID: m.RequestID,
		TenantID:  m.TenantID,
		ProjectID: m.ProjectID,
		TaskID:    m.TaskID,
		Status:    string(m.Status),
	}, nil
}

type pgPrincipalResolver struct{ repo *pgProvisioningAdapter }

func (r pgPrincipalResolver) ResolveAuthContext(ctx context.Context, tenantID, principalID uuid.UUID) (identity.AuthContext, error) {
	p, err := r.repo.GetPrincipalByID(ctx, tenantID, principalID)
	if err != nil {
		return identity.AuthContext{}, err
	}
	if !p.IsActive || (p.ExpiresAt != nil && p.ExpiresAt.Before(time.Now().UTC())) {
		return identity.AuthContext{}, identity.ErrForbidden
	}
	return identity.AuthContext{
		TenantID:        p.TenantID,
		PrincipalID:     p.ID,
		PrincipalType:   p.PrincipalType,
		AgentID:         p.AgentID,
		AllowedProjects: p.AllowedProjects,
		AllowedScopes:   p.AllowedScopes,
		IsAdmin:         p.IsAdmin,
	}, nil
}

type pgWorkerQueue struct{ o *postgres.OpaqueRequestStore }

func (q pgWorkerQueue) Claim(ctx context.Context) (*requests.Lease, error) {
	l, err := q.o.Claim(ctx)
	if err != nil || l == nil {
		return nil, err
	}
	return &requests.Lease{RequestID: l.RequestID, Token: l.Token, ExpiresAt: l.ExpiresAt}, nil
}

func (q pgWorkerQueue) Renew(ctx context.Context, lease requests.Lease) (bool, error) {
	return q.o.Renew(ctx, postgres.Lease{RequestID: lease.RequestID, Token: lease.Token, ExpiresAt: lease.ExpiresAt})
}

func (q pgWorkerQueue) Execute(ctx context.Context, lease requests.Lease, apply requests.ApplyMutation) error {
	err := q.o.Execute(ctx, postgres.Lease{RequestID: lease.RequestID, Token: lease.Token, ExpiresAt: lease.ExpiresAt},
		func(ctx context.Context, reg memory.RepositoryRegistry, perms identity.TaskPermissionReader, rec postgres.OpaqueRequestRecord) (*uuid.UUID, error) {
			return apply(ctx, reg, perms, requests.OpaqueRequestRecord{
				RequestID:      rec.RequestID,
				TenantID:       rec.TenantID,
				ProjectID:      rec.ProjectID,
				TaskID:         rec.TaskID,
				PrincipalID:    rec.PrincipalID,
				Operation:      requests.OpaqueOperation(rec.Operation),
				TargetMemoryID: rec.TargetMemoryID,
				PayloadVersion: rec.PayloadVersion,
				Payload:        rec.Payload,
				RequestSHA256:  rec.RequestSHA256,
				IdempotencyKey: rec.IdempotencyKey,
				AttemptCount:   rec.AttemptCount,
				MaxRetries:     rec.MaxRetries,
			})
		},
	)
	if errors.Is(err, postgres.ErrLeaseLost) {
		return requests.ErrLeaseLost
	}
	return err
}

func (q pgWorkerQueue) RecordFailure(ctx context.Context, lease requests.Lease, class requests.FailureClass, code, message string) error {
	err := q.o.RecordFailure(ctx, postgres.Lease{RequestID: lease.RequestID, Token: lease.Token, ExpiresAt: lease.ExpiresAt}, postgres.FailureClass(class), code, message)
	if errors.Is(err, postgres.ErrLeaseLost) {
		return requests.ErrLeaseLost
	}
	return err
}

func (q pgWorkerQueue) PurgeTerminal(ctx context.Context, retention time.Duration) (int64, error) {
	return q.o.PurgeTerminal(ctx, retention)
}
