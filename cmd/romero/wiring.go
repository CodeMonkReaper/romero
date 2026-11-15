package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/romero-project/romero/internal/application/capture"
	"github.com/romero-project/romero/internal/application/feedback"
	"github.com/romero-project/romero/internal/application/provisioning"
	"github.com/romero-project/romero/internal/application/requests"
	"github.com/romero-project/romero/internal/application/retrieval"
	"github.com/romero-project/romero/internal/domain/identity"
	"github.com/romero-project/romero/internal/domain/memory"
	"github.com/romero-project/romero/internal/infrastructure/persistence/postgres"
	"github.com/romero-project/romero/internal/interfaces/cli"
	"github.com/romero-project/romero/internal/interfaces/rest"
	"github.com/romero-project/romero/migrations"
)

func defaultDependencies() cli.Dependencies {
	return cli.Dependencies{
		Migrator:        gooseMigrator{},
		OpenProvisioner: openPostgresProvisioner,
		Serve:           servePostgresServer,
	}
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

type pgStoreWrapper struct {
	*postgres.ProvisioningStore
}

func (w pgStoreWrapper) ExecTx(ctx context.Context, fn func(tx pgBackend, audit memory.AuditLogger) error) error {
	return w.ProvisioningStore.ExecTx(ctx, func(txStore *postgres.ProvisioningStore, audit memory.AuditLogger) error {
		return fn(pgStoreWrapper{ProvisioningStore: txStore}, audit)
	})
}

type gooseMigrator struct{}

func (gooseMigrator) Run(ctx context.Context, dsn, direction string) (int64, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return 0, fmt.Errorf("connect postgres: %w", err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		return 0, fmt.Errorf("ping postgres: %w", err)
	}

	sqlDB := stdlib.OpenDBFromPool(pool)
	defer func() { _ = sqlDB.Close() }()

	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("postgres"); err != nil {
		return 0, err
	}

	switch direction {
	case "", "up":
		if err := goose.UpContext(ctx, sqlDB, "."); err != nil {
			return 0, err
		}
	case "status":
		if err := goose.StatusContext(ctx, sqlDB, "."); err != nil {
			return 0, err
		}
	default:
		return 0, fmt.Errorf("unsupported migrate direction %q (expected up or status)", direction)
	}
	return goose.GetDBVersionContext(ctx, sqlDB)
}

func openPostgresProvisioner(ctx context.Context, dsn string) (cli.ProvisioningClient, func(), error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return cli.ProvisioningClient{}, nil, fmt.Errorf("connect postgres: %w", err)
	}
	cleanup := pool.Close
	if err := pool.Ping(ctx); err != nil {
		cleanup()
		return cli.ProvisioningClient{}, nil, fmt.Errorf("ping postgres: %w", err)
	}

	store, err := postgres.NewStore(pool)
	if err != nil {
		cleanup()
		return cli.ProvisioningClient{}, nil, err
	}

	repo := newPostgresAdapter(pgStoreWrapper{ProvisioningStore: store.Provisioning()})
	audit := store.Audit()
	return cli.ProvisioningClient{
		Service: provisioning.NewService(repo, repo, audit),
		Auth:    provisioning.NewAuthenticator(repo, audit),
	}, cleanup, nil
}

type pgProvisioningAdapter struct {
	store pgBackend
}

func newPostgresAdapter(backend pgBackend) *pgProvisioningAdapter {
	return &pgProvisioningAdapter{store: backend}
}

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
	return mapPGError(a.store.ExecTx(ctx, func(txStore pgBackend, audit memory.AuditLogger) error {
		return fn(&pgProvisioningAdapter{store: txStore}, audit)
	}))
}

func (a *pgProvisioningAdapter) CreateTenant(ctx context.Context, arg provisioning.CreateTenantParams) (provisioning.Tenant, error) {
	rec, err := a.store.CreateTenant(ctx, postgres.CreateTenantParams{
		ID: arg.ID, Slug: arg.Slug, Name: arg.Name, IsActive: arg.IsActive, Settings: arg.Settings,
	})
	if err != nil {
		return provisioning.Tenant{}, mapPGError(err)
	}
	return mapTenant(rec), nil
}

func (a *pgProvisioningAdapter) GetTenantByID(ctx context.Context, id uuid.UUID) (*provisioning.Tenant, error) {
	rec, err := a.store.GetTenantByID(ctx, id)
	if err != nil {
		return nil, mapPGError(err)
	}
	out := mapTenant(*rec)
	return &out, nil
}

func (a *pgProvisioningAdapter) CreateProject(ctx context.Context, arg provisioning.CreateProjectParams) (provisioning.Project, error) {
	rec, err := a.store.CreateProject(ctx, postgres.CreateProjectParams{
		ID: arg.ID, TenantID: arg.TenantID, Slug: arg.Slug, Name: arg.Name,
		RepositoryURL: arg.RepositoryURL, Description: arg.Description, IsActive: arg.IsActive, Metadata: arg.Metadata,
	})
	if err != nil {
		return provisioning.Project{}, mapPGError(err)
	}
	return mapProject(rec), nil
}

func (a *pgProvisioningAdapter) GetProjectByID(ctx context.Context, tenantID, id uuid.UUID) (*provisioning.Project, error) {
	rec, err := a.store.GetProjectByID(ctx, tenantID, id)
	if err != nil {
		return nil, mapPGError(err)
	}
	out := mapProject(*rec)
	return &out, nil
}

func (a *pgProvisioningAdapter) CountTenantProjects(ctx context.Context, tenantID uuid.UUID, ids []uuid.UUID) (int64, error) {
	count, err := a.store.CountTenantProjects(ctx, tenantID, ids)
	if err != nil {
		return 0, mapPGError(err)
	}
	return count, nil
}

func (a *pgProvisioningAdapter) CreateAgent(ctx context.Context, arg provisioning.CreateAgentParams) (provisioning.Agent, error) {
	rec, err := a.store.CreateAgent(ctx, postgres.CreateAgentParams{
		ID: arg.ID, TenantID: arg.TenantID, ExternalKey: arg.ExternalKey, Name: arg.Name,
		RoleType: arg.RoleType, Capabilities: arg.Capabilities, IsActive: arg.IsActive, Metadata: arg.Metadata,
	})
	if err != nil {
		return provisioning.Agent{}, mapPGError(err)
	}
	return mapAgent(rec), nil
}

func (a *pgProvisioningAdapter) GetAgentByID(ctx context.Context, tenantID, id uuid.UUID) (*provisioning.Agent, error) {
	rec, err := a.store.GetAgentByID(ctx, tenantID, id)
	if err != nil {
		return nil, mapPGError(err)
	}
	out := mapAgent(*rec)
	return &out, nil
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
	out := mapPrincipal(*rec)
	return &out, nil
}

func (a *pgProvisioningAdapter) LookupPrincipalsByPrefix(ctx context.Context, prefix string) ([]provisioning.PrincipalAuthCandidate, error) {
	rows, err := a.store.LookupPrincipalsByPrefix(ctx, prefix)
	if err != nil {
		return nil, mapPGError(err)
	}
	return mapAuthCandidates(rows), nil
}

func (a *pgProvisioningAdapter) UpsertTaskPermission(ctx context.Context, grant provisioning.TaskPermissionGrant) error {
	return mapPGError(a.store.UpsertTaskPermission(ctx, postgres.TaskPermissionGrant{
		TenantID: grant.TenantID, ProjectID: grant.ProjectID, TaskID: grant.TaskID,
		PrincipalID: grant.PrincipalID, CanRead: grant.CanRead, CanWrite: grant.CanWrite,
	}))
}

func mapTenant(r postgres.TenantRecord) provisioning.Tenant {
	return provisioning.Tenant{ID: r.ID, Slug: r.Slug, Name: r.Name, IsActive: r.IsActive}
}

func mapProject(r postgres.ProjectRecord) provisioning.Project {
	return provisioning.Project{ID: r.ID, TenantID: r.TenantID, Slug: r.Slug, Name: r.Name, IsActive: r.IsActive}
}

func mapAgent(r postgres.AgentRecord) provisioning.Agent {
	return provisioning.Agent{
		ID: r.ID, TenantID: r.TenantID, ExternalKey: r.ExternalKey,
		Name: r.Name, RoleType: r.RoleType, IsActive: r.IsActive,
	}
}

func mapPrincipal(r postgres.PrincipalRecord) provisioning.Principal {
	return provisioning.Principal{
		ID: r.ID, TenantID: r.TenantID, AgentID: r.AgentID, PrincipalType: r.PrincipalType,
		Name: r.Name, APIKeyPrefix: r.APIKeyPrefix, AllowedProjects: r.AllowedProjects,
		AllowedScopes: r.AllowedScopes, IsActive: r.IsActive, ExpiresAt: r.ExpiresAt,
		CreatedAt: r.CreatedAt, IsAdmin: r.IsAdmin,
	}
}

func mapAuthCandidates(rows []postgres.PrincipalAuthCandidate) []provisioning.PrincipalAuthCandidate {
	out := make([]provisioning.PrincipalAuthCandidate, 0, len(rows))
	for _, r := range rows {
		out = append(out, provisioning.PrincipalAuthCandidate{
			Principal:    mapPrincipal(r.Principal),
			APIKeySHA256: r.APIKeySHA256,
			TenantActive: r.TenantActive,
			AgentActive:  r.AgentActive,
		})
	}
	return out
}

func servePostgresServer(ctx context.Context, dsn, listenAddr string) error {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return fmt.Errorf("connect postgres: %w", err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping postgres: %w", err)
	}

	store, err := postgres.NewStore(pool)
	if err != nil {
		return err
	}

	repo := newPostgresAdapter(pgStoreWrapper{ProvisioningStore: store.Provisioning()})
	audit := store.Audit()
	auth := provisioning.NewAuthenticator(repo, audit)
	taskPerms := store.TaskPermissions()
	opaqueStore := store.OpaqueRequests(60 * time.Second)

	capSvc, err := capture.NewService(store, audit, pgCaptureAccess{r: taskPerms}, pgCaptureOpaque{o: opaqueStore}, nil)
	if err != nil {
		return err
	}
	retSvc, err := retrieval.NewService(store, audit, pgRetrievalAccess{r: taskPerms}, pgOpaqueStatus{o: opaqueStore}, nil)
	if err != nil {
		return err
	}
	fbSvc, err := feedback.NewService(store, audit, pgFeedbackAccess{r: taskPerms}, pgFeedbackOpaque{o: opaqueStore}, capSvc, nil)
	if err != nil {
		return err
	}

	worker, err := requests.NewWorker(
		pgWorkerQueue{o: opaqueStore},
		pgPrincipalResolver{repo: repo},
		&requests.ServiceExecutor{Capture: capSvc, Feedback: fbSvc},
		requests.Config{},
	)
	if err != nil {
		return err
	}

	handler, _, err := rest.NewRouter(rest.RouterDeps{
		Auth:      auth,
		Capture:   capSvc,
		Retrieval: retSvc,
		Feedback:  fbSvc,
		DB:        pool,
		Version:   cli.Version,
	})
	if err != nil {
		return err
	}

	workerCtx, cancelWorker := context.WithCancel(ctx)
	defer cancelWorker()
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		_ = worker.Run(workerCtx)
	}()

	srv := &http.Server{
		Addr:              listenAddr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		if lErr := srv.ListenAndServe(); lErr != nil && !errors.Is(lErr, http.ErrServerClosed) {
			errCh <- lErr
		}
		close(errCh)
	}()

	select {
	case sErr := <-errCh:
		cancelWorker()
		<-workerDone
		return sErr
	case <-ctx.Done():
		shutCtx, cancelShut := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancelShut()
		_ = srv.Shutdown(shutCtx)
		cancelWorker()
		<-workerDone
		return nil
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
