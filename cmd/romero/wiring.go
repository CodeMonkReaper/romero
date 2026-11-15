package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/romero-project/romero/internal/application/provisioning"
	"github.com/romero-project/romero/internal/domain/memory"
	"github.com/romero-project/romero/internal/infrastructure/persistence/postgres"
	"github.com/romero-project/romero/internal/interfaces/cli"
	"github.com/romero-project/romero/migrations"
)

func defaultDependencies() cli.Dependencies {
	return cli.Dependencies{
		Migrator:        gooseMigrator{},
		OpenProvisioner: openPostgresProvisioner,
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
