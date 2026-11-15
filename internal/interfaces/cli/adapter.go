package cli

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/romero-project/romero/internal/application/provisioning"
	"github.com/romero-project/romero/internal/domain/memory"
	"github.com/romero-project/romero/internal/infrastructure/persistence/postgres"
	"github.com/romero-project/romero/migrations"
)

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

func openPostgresProvisioner(ctx context.Context, dsn string) (ProvisioningClient, func(), error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return ProvisioningClient{}, nil, fmt.Errorf("connect postgres: %w", err)
	}
	cleanup := pool.Close
	if err := pool.Ping(ctx); err != nil {
		cleanup()
		return ProvisioningClient{}, nil, fmt.Errorf("ping postgres: %w", err)
	}

	store, err := postgres.NewStore(pool)
	if err != nil {
		cleanup()
		return ProvisioningClient{}, nil, err
	}

	repo := NewPostgresAdapter(pgStoreWrapper{ProvisioningStore: store.Provisioning()})
	audit := store.Audit()
	return ProvisioningClient{
		Service: provisioning.NewService(repo, repo, audit),
		Auth:    provisioning.NewAuthenticator(repo, audit),
	}, cleanup, nil
}

// PGProvisioningAdapter bridges pgBackend to provisioning.Repository and UnitOfWork.
type PGProvisioningAdapter struct {
	store pgBackend
}

// NewPostgresAdapter wraps backend as a provisioning Repository and UnitOfWork.
func NewPostgresAdapter(backend pgBackend) *PGProvisioningAdapter {
	return &PGProvisioningAdapter{store: backend}
}

// ExecTx delegates transactional execution to the backing PostgreSQL store.
func (a *PGProvisioningAdapter) ExecTx(ctx context.Context, fn func(provisioning.Repository, memory.AuditLogger) error) error {
	return a.store.ExecTx(ctx, func(txStore pgBackend, audit memory.AuditLogger) error {
		return fn(&PGProvisioningAdapter{store: txStore}, audit)
	})
}

// CreateTenant inserts a tenant via PostgreSQL persistence.
func (a *PGProvisioningAdapter) CreateTenant(ctx context.Context, arg provisioning.CreateTenantParams) (provisioning.Tenant, error) {
	rec, err := a.store.CreateTenant(ctx, postgres.CreateTenantParams{
		ID: arg.ID, Slug: arg.Slug, Name: arg.Name, IsActive: arg.IsActive, Settings: arg.Settings,
	})
	if err != nil {
		return provisioning.Tenant{}, err
	}
	return MapTenant(rec), nil
}

// GetTenantByID looks up a tenant by ID via PostgreSQL persistence.
func (a *PGProvisioningAdapter) GetTenantByID(ctx context.Context, id uuid.UUID) (*provisioning.Tenant, error) {
	rec, err := a.store.GetTenantByID(ctx, id)
	if err != nil {
		return nil, err
	}
	out := MapTenant(*rec)
	return &out, nil
}

// CreateProject inserts a project via PostgreSQL persistence.
func (a *PGProvisioningAdapter) CreateProject(ctx context.Context, arg provisioning.CreateProjectParams) (provisioning.Project, error) {
	rec, err := a.store.CreateProject(ctx, postgres.CreateProjectParams{
		ID: arg.ID, TenantID: arg.TenantID, Slug: arg.Slug, Name: arg.Name,
		RepositoryURL: arg.RepositoryURL, Description: arg.Description, IsActive: arg.IsActive, Metadata: arg.Metadata,
	})
	if err != nil {
		return provisioning.Project{}, err
	}
	return MapProject(rec), nil
}

// GetProjectByID looks up a project by tenant and ID via PostgreSQL persistence.
func (a *PGProvisioningAdapter) GetProjectByID(ctx context.Context, tenantID, id uuid.UUID) (*provisioning.Project, error) {
	rec, err := a.store.GetProjectByID(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	out := MapProject(*rec)
	return &out, nil
}

// CountTenantProjects counts projects belonging to tenantID.
func (a *PGProvisioningAdapter) CountTenantProjects(ctx context.Context, tenantID uuid.UUID, ids []uuid.UUID) (int64, error) {
	return a.store.CountTenantProjects(ctx, tenantID, ids)
}

// CreateAgent inserts an agent via PostgreSQL persistence.
func (a *PGProvisioningAdapter) CreateAgent(ctx context.Context, arg provisioning.CreateAgentParams) (provisioning.Agent, error) {
	rec, err := a.store.CreateAgent(ctx, postgres.CreateAgentParams{
		ID: arg.ID, TenantID: arg.TenantID, ExternalKey: arg.ExternalKey, Name: arg.Name,
		RoleType: arg.RoleType, Capabilities: arg.Capabilities, IsActive: arg.IsActive, Metadata: arg.Metadata,
	})
	if err != nil {
		return provisioning.Agent{}, err
	}
	return MapAgent(rec), nil
}

// GetAgentByID looks up an agent by tenant and ID via PostgreSQL persistence.
func (a *PGProvisioningAdapter) GetAgentByID(ctx context.Context, tenantID, id uuid.UUID) (*provisioning.Agent, error) {
	rec, err := a.store.GetAgentByID(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	out := MapAgent(*rec)
	return &out, nil
}

// CreatePrincipal inserts a principal via PostgreSQL persistence.
func (a *PGProvisioningAdapter) CreatePrincipal(ctx context.Context, arg provisioning.CreatePrincipalParams) (provisioning.Principal, error) {
	rec, err := a.store.CreatePrincipal(ctx, postgres.CreatePrincipalParams{
		ID: arg.ID, TenantID: arg.TenantID, AgentID: arg.AgentID, PrincipalType: arg.PrincipalType,
		Name: arg.Name, APIKeyPrefix: arg.APIKeyPrefix, APIKeySHA256: arg.APIKeySHA256,
		AllowedProjects: arg.AllowedProjects, AllowedScopes: arg.AllowedScopes,
		IsActive: arg.IsActive, ExpiresAt: arg.ExpiresAt, IsAdmin: arg.IsAdmin,
	})
	if err != nil {
		return provisioning.Principal{}, err
	}
	return MapPrincipal(rec), nil
}

// GetPrincipalByID looks up a principal by tenant and ID via PostgreSQL persistence.
func (a *PGProvisioningAdapter) GetPrincipalByID(ctx context.Context, tenantID, id uuid.UUID) (*provisioning.Principal, error) {
	rec, err := a.store.GetPrincipalByID(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	out := MapPrincipal(*rec)
	return &out, nil
}

// LookupPrincipalsByPrefix retrieves candidates matching prefix via PostgreSQL persistence.
func (a *PGProvisioningAdapter) LookupPrincipalsByPrefix(ctx context.Context, prefix string) ([]provisioning.PrincipalAuthCandidate, error) {
	rows, err := a.store.LookupPrincipalsByPrefix(ctx, prefix)
	if err != nil {
		return nil, err
	}
	return MapAuthCandidates(rows), nil
}

// UpsertTaskPermission creates or updates a task_shared ACL via PostgreSQL persistence.
func (a *PGProvisioningAdapter) UpsertTaskPermission(ctx context.Context, grant provisioning.TaskPermissionGrant) error {
	return a.store.UpsertTaskPermission(ctx, postgres.TaskPermissionGrant{
		TenantID: grant.TenantID, ProjectID: grant.ProjectID, TaskID: grant.TaskID,
		PrincipalID: grant.PrincipalID, CanRead: grant.CanRead, CanWrite: grant.CanWrite,
	})
}

// MapTenant converts a persistence tenant record into a provisioning domain model.
func MapTenant(r postgres.TenantRecord) provisioning.Tenant {
	return provisioning.Tenant{ID: r.ID, Slug: r.Slug, Name: r.Name, IsActive: r.IsActive}
}

// MapProject converts a persistence project record into a provisioning domain model.
func MapProject(r postgres.ProjectRecord) provisioning.Project {
	return provisioning.Project{ID: r.ID, TenantID: r.TenantID, Slug: r.Slug, Name: r.Name, IsActive: r.IsActive}
}

// MapAgent converts a persistence agent record into a provisioning domain model.
func MapAgent(r postgres.AgentRecord) provisioning.Agent {
	return provisioning.Agent{
		ID: r.ID, TenantID: r.TenantID, ExternalKey: r.ExternalKey,
		Name: r.Name, RoleType: r.RoleType, IsActive: r.IsActive,
	}
}

// MapPrincipal converts a persistence principal record into a provisioning domain model.
func MapPrincipal(r postgres.PrincipalRecord) provisioning.Principal {
	return provisioning.Principal{
		ID: r.ID, TenantID: r.TenantID, AgentID: r.AgentID, PrincipalType: r.PrincipalType,
		Name: r.Name, APIKeyPrefix: r.APIKeyPrefix, AllowedProjects: r.AllowedProjects,
		AllowedScopes: r.AllowedScopes, IsActive: r.IsActive, ExpiresAt: r.ExpiresAt,
		CreatedAt: r.CreatedAt, IsAdmin: r.IsAdmin,
	}
}

// MapAuthCandidates converts persistence auth rows into provisioning lookup candidates.
func MapAuthCandidates(rows []postgres.PrincipalAuthCandidate) []provisioning.PrincipalAuthCandidate {
	out := make([]provisioning.PrincipalAuthCandidate, 0, len(rows))
	for _, r := range rows {
		out = append(out, provisioning.PrincipalAuthCandidate{
			Principal:    MapPrincipal(r.Principal),
			APIKeySHA256: r.APIKeySHA256,
			TenantActive: r.TenantActive,
			AgentActive:  r.AgentActive,
		})
	}
	return out
}
