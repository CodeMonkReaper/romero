package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/romero-project/romero/internal/application/capture"
	"github.com/romero-project/romero/internal/application/conflicts"
	"github.com/romero-project/romero/internal/application/consolidation"
	contextbuilder "github.com/romero-project/romero/internal/application/context"
	"github.com/romero-project/romero/internal/application/eval"
	"github.com/romero-project/romero/internal/application/feedback"
	"github.com/romero-project/romero/internal/application/jobs"
	"github.com/romero-project/romero/internal/application/provisioning"
	"github.com/romero-project/romero/internal/application/requests"
	"github.com/romero-project/romero/internal/application/retrieval"
	"github.com/romero-project/romero/internal/domain/governor"
	"github.com/romero-project/romero/internal/domain/identity"
	"github.com/romero-project/romero/internal/domain/memory"
	"github.com/romero-project/romero/internal/infrastructure/cachel1"
	"github.com/romero-project/romero/internal/infrastructure/neural"
	"github.com/romero-project/romero/internal/infrastructure/persistence/postgres"
	"github.com/romero-project/romero/internal/interfaces/cli"
	"github.com/romero-project/romero/internal/interfaces/mcp"
	"github.com/romero-project/romero/internal/interfaces/rest"
	"github.com/romero-project/romero/migrations"
)

func defaultDependencies() cli.Dependencies {
	return cli.Dependencies{
		Migrator:        gooseMigrator{},
		OpenProvisioner: openPostgresProvisioner,
		Serve:           servePostgresServer,
		ServeWithMCP:    servePostgresServerWithMCP,
		MCP:             runPostgresMCP,
		Eval:            eval.RunDataset,
		Consolidate:     runPostgresConsolidation,
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

type postgresRuntime struct {
	pool        *pgxpool.Pool
	store       *postgres.Store
	repo        *pgProvisioningAdapter
	auth        *provisioning.Authenticator
	opaqueStore *postgres.OpaqueRequestStore
	bgeClient   *neural.BGEM3Client
	nliClient   *neural.DeBERTaNLIClient
	gov         *governor.PowerGovernor
	l1Graph     *cachel1.CognitiveGraph
	capSvc      *capture.Service
	retSvc      *retrieval.Service
	ctxSvc      *contextbuilder.Service
	fbSvc       *feedback.Service
	conflictSvc *conflicts.Service
	consSvc     *consolidation.Service
	mcpSrv      *mcp.Server
}

func openPostgresRuntime(ctx context.Context, dsn string) (*postgresRuntime, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("connect postgres: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	store, err := postgres.NewStore(pool)
	if err != nil {
		pool.Close()
		return nil, err
	}

	repo := newPostgresAdapter(pgStoreWrapper{ProvisioningStore: store.Provisioning()})
	audit := store.Audit()
	auth := provisioning.NewAuthenticator(repo, audit)
	taskPerms := store.TaskPermissions()
	opaqueStore := store.OpaqueRequests(60 * time.Second)

	bgeURL := strings.TrimSpace(os.Getenv("ROMERO_BGE_M3_URL"))
	bgeClient := neural.NewBGEM3Client(neural.BGEM3Config{
		BaseURL: bgeURL,
		Enabled: bgeURL != "",
	})
	rerankerClient, nliClient, slmClient, gov, profile := buildNeuralAndGovernor()
	l1Graph := cachel1.NewCognitiveGraph(store.Search())

	capSvc, err := capture.NewService(store, audit, pgCaptureAccess{r: taskPerms}, pgCaptureOpaque{o: opaqueStore}, nil)
	if err != nil {
		pool.Close()
		return nil, err
	}
	capSvc.SetGraphNotifier(l1Graph)

	retSvc, err := retrieval.NewService(store, audit, pgRetrievalAccess{r: taskPerms}, pgOpaqueStatus{o: opaqueStore}, nil)
	if err != nil {
		pool.Close()
		return nil, err
	}
	retSvc.SetHybridDeps(store.Search(), bgeClient, l1Graph)
	retSvc.WithReranker(rerankerClient, profile)

	ctxSvc, err := contextbuilder.NewService(retSvc, audit, nil)
	if err != nil {
		pool.Close()
		return nil, err
	}

	fbSvc, err := feedback.NewService(store, audit, pgFeedbackAccess{r: taskPerms}, pgFeedbackOpaque{o: opaqueStore}, capSvc, nil)
	if err != nil {
		pool.Close()
		return nil, err
	}
	fbSvc.SetGraphNotifier(l1Graph)

	conflictSvc, err := conflicts.NewService(store, store.Conflicts(), audit, pgIdentityTaskReader{r: taskPerms}, l1Graph, nil)
	if err != nil {
		pool.Close()
		return nil, err
	}

	consSvc, err := consolidation.NewService(store, store.Consolidation(), slmClient, nliClient, audit, l1Graph, gov, nil)
	if err != nil {
		pool.Close()
		return nil, err
	}

	mcpSrv, err := mcp.NewServer(mcp.Dependencies{
		Authenticator: auth,
		Capture:       capSvc,
		Retrieval:     retSvc,
		Context:       ctxSvc,
		Feedback:      fbSvc,
		Governor:      gov,
	})
	if err != nil {
		pool.Close()
		return nil, err
	}

	return &postgresRuntime{
		pool:        pool,
		store:       store,
		repo:        repo,
		auth:        auth,
		opaqueStore: opaqueStore,
		bgeClient:   bgeClient,
		nliClient:   nliClient,
		gov:         gov,
		l1Graph:     l1Graph,
		capSvc:      capSvc,
		retSvc:      retSvc,
		ctxSvc:      ctxSvc,
		fbSvc:       fbSvc,
		conflictSvc: conflictSvc,
		consSvc:     consSvc,
		mcpSrv:      mcpSrv,
	}, nil
}

func servePostgresServer(ctx context.Context, dsn, listenAddr string) error {
	return servePostgresServerWithMCP(ctx, dsn, listenAddr, "")
}

func servePostgresServerWithMCP(ctx context.Context, dsn, listenAddr, mcpListenAddr string) error {
	rt, err := openPostgresRuntime(ctx, dsn)
	if err != nil {
		return err
	}
	defer rt.pool.Close()

	worker, err := requests.NewWorker(
		pgWorkerQueue{o: rt.opaqueStore},
		pgPrincipalResolver{repo: rt.repo},
		&requests.ServiceExecutor{Capture: rt.capSvc, Feedback: rt.fbSvc},
		requests.Config{},
	)
	if err != nil {
		return err
	}

	embWorker := jobs.NewEmbeddingWorker(rt.store.JobQueue(), rt.store.Search(), rt.store.Embeddings(), rt.bgeClient, rt.l1Graph, jobs.EmbeddingWorkerConfig{}, nil).
		WithNLI(rt.store, rt.store.SemanticNeighbors(), rt.nliClient)

	handler, _, err := rest.NewRouter(rest.RouterDeps{
		Auth:          rt.auth,
		Capture:       rt.capSvc,
		Retrieval:     rt.retSvc,
		Context:       rt.ctxSvc,
		Feedback:      rt.fbSvc,
		Conflicts:     rt.conflictSvc,
		Consolidation: rt.consSvc,
		Governor:      rt.gov,
		DB:            rt.pool,
		L1Stats:       rt.l1Graph,
		Version:       cli.Version,
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
	embDone := make(chan struct{})
	go func() {
		defer close(embDone)
		embWorker.Run(workerCtx)
	}()
	remDone := make(chan struct{})
	go func() {
		defer close(remDone)
		runIdleREMScheduler(workerCtx, rt.gov, rt.store, rt.consSvc, defaultIdleREMTickInterval)
	}()

	srv := &http.Server{
		Addr:              listenAddr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	var mcpHTTP *http.Server
	if mcpAddr := strings.TrimSpace(mcpListenAddr); mcpAddr != "" && !strings.EqualFold(mcpAddr, "disabled") {
		mcpHTTP = &http.Server{
			Addr:              mcpAddr,
			Handler:           rt.mcpSrv.HTTPHandler(),
			ReadHeaderTimeout: 10 * time.Second,
		}
	}

	errCh := make(chan error, 2)
	go func() {
		if lErr := srv.ListenAndServe(); lErr != nil && !errors.Is(lErr, http.ErrServerClosed) {
			errCh <- lErr
		}
	}()
	if mcpHTTP != nil {
		go func() {
			if lErr := mcpHTTP.ListenAndServe(); lErr != nil && !errors.Is(lErr, http.ErrServerClosed) {
				errCh <- lErr
			}
		}()
	}

	shutdownAll := func() {
		shutCtx, cancelShut := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancelShut()
		_ = srv.Shutdown(shutCtx)
		if mcpHTTP != nil {
			_ = mcpHTTP.Shutdown(shutCtx)
		}
		cancelWorker()
		<-workerDone
		<-embDone
		<-remDone
	}

	select {
	case sErr := <-errCh:
		shutdownAll()
		return sErr
	case <-ctx.Done():
		shutdownAll()
		return nil
	}
}

func runPostgresMCP(ctx context.Context, opts cli.MCPServeOptions) error {
	rt, err := openPostgresRuntime(ctx, opts.DSN)
	if err != nil {
		return err
	}
	defer rt.pool.Close()

	if opts.Transport == "stdio" {
		return rt.mcpSrv.ServeStdio(ctx, opts.Stdin, opts.Stdout, opts.APIKey)
	}

	srv := &http.Server{
		Addr:              opts.ListenAddr,
		Handler:           rt.mcpSrv.HTTPHandler(),
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
		return sErr
	case <-ctx.Done():
		shutCtx, cancelShut := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancelShut()
		_ = srv.Shutdown(shutCtx)
		return nil
	}
}

const defaultIdleREMTickInterval = 5 * time.Second

type remTargetLister interface {
	ListActiveREMTargets(ctx context.Context) ([]postgres.REMTarget, error)
}

type remConsolidator interface {
	Consolidate(ctx context.Context, auth identity.AuthContext, cmd consolidation.ConsolidateCommand) (*consolidation.ConsolidateResult, error)
}

func runIdleREMScheduler(
	ctx context.Context,
	gov *governor.PowerGovernor,
	lister remTargetLister,
	cons remConsolidator,
	interval time.Duration,
) {
	if gov == nil || lister == nil || cons == nil {
		return
	}
	if interval <= 0 {
		interval = defaultIdleREMTickInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	nowUTC := func() time.Time { return time.Now().UTC() }
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runIdleREMTick(ctx, gov, lister, cons, nowUTC)
		}
	}
}

func runIdleREMTick(
	ctx context.Context,
	gov *governor.PowerGovernor,
	lister remTargetLister,
	cons remConsolidator,
	nowFn func() time.Time,
) {
	if ctx.Err() != nil || gov == nil || lister == nil || cons == nil {
		return
	}
	if nowFn == nil {
		nowFn = func() time.Time { return time.Now().UTC() }
	}
	if !gov.CanAutoEnterREM(nowFn()) {
		return
	}
	targets, err := lister.ListActiveREMTargets(ctx)
	if err != nil || len(targets) == 0 {
		return
	}
	for _, target := range targets {
		if ctx.Err() != nil || !gov.CanAutoEnterREM(nowFn()) {
			return
		}
		_, _ = cons.Consolidate(ctx, identity.AuthContext{
			TenantID:      target.TenantID,
			PrincipalID:   target.PrincipalID,
			PrincipalType: identity.PrincipalService,
			IsAdmin:       true,
		}, consolidation.ConsolidateCommand{
			CorrelationID: "rem-scheduler",
		})
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

type pgIdentityTaskReader struct {
	r *postgres.TaskPermissionRepository
}

func (a pgIdentityTaskReader) HasTaskPermission(ctx context.Context, q identity.TaskPermissionQuery) (bool, error) {
	s, err := a.r.EvaluateTaskAccess(ctx, q)
	if err != nil {
		return false, err
	}
	if q.Permission == identity.TaskWrite {
		return s.CanWrite, nil
	}
	return s.CanRead, nil
}

func buildNeuralAndGovernor() (
	*neural.RerankerClient,
	*neural.DeBERTaNLIClient,
	*neural.QwenSLMClient,
	*governor.PowerGovernor,
	governor.Profile,
) {
	profile := governor.Profile(strings.ToLower(strings.TrimSpace(os.Getenv("ROMERO_PROFILE"))))
	switch profile {
	case governor.ProfileLite, governor.ProfileBalanced, governor.ProfileUltra:
	default:
		profile = governor.ProfileBalanced
	}

	rerankerURL := strings.TrimSpace(os.Getenv("ROMERO_RERANKER_URL"))
	rerankerClient := neural.NewRerankerClient(neural.RerankerConfig{
		BaseURL: rerankerURL,
		Enabled: rerankerURL != "" && profile == governor.ProfileUltra,
	})

	nliURL := strings.TrimSpace(os.Getenv("ROMERO_NLI_URL"))
	nliClient := neural.NewDeBERTaNLIClient(neural.NLIConfig{
		BaseURL: nliURL,
		Enabled: nliURL != "",
	})

	slmURL := strings.TrimSpace(os.Getenv("ROMERO_SLM_URL"))
	slmClient := neural.NewQwenSLMClient(neural.SLMConfig{
		BaseURL: slmURL,
		Enabled: slmURL != "",
	})

	gov := governor.NewPowerGovernor(governor.Config{
		Profile:           profile,
		OnUnloadVRAM:      slmClient.UnloadVRAM,
		RerankerAvailable: rerankerURL != "" && profile == governor.ProfileUltra,
		NLIAvailable:      nliURL != "",
		SLMAvailable:      slmURL != "",
	})
	return rerankerClient, nliClient, slmClient, gov, profile
}

func runPostgresConsolidation(ctx context.Context, dsn, rawAPIKey string, projectID *uuid.UUID, dryRun bool) (*consolidation.ConsolidateResult, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("connect postgres: %w", err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	store, err := postgres.NewStore(pool)
	if err != nil {
		return nil, err
	}

	repo := newPostgresAdapter(pgStoreWrapper{ProvisioningStore: store.Provisioning()})
	audit := store.Audit()
	auth := provisioning.NewAuthenticator(repo, audit)
	caller, err := auth.Authenticate(ctx, rawAPIKey)
	if err != nil {
		return nil, err
	}

	_, nliClient, slmClient, gov, _ := buildNeuralAndGovernor()
	l1Graph := cachel1.NewCognitiveGraph(store.Search())
	consSvc, err := consolidation.NewService(store, store.Consolidation(), slmClient, nliClient, audit, l1Graph, gov, nil)
	if err != nil {
		return nil, err
	}

	return consSvc.Consolidate(ctx, caller, consolidation.ConsolidateCommand{
		ProjectID: projectID,
		DryRun:    dryRun,
	})
}
