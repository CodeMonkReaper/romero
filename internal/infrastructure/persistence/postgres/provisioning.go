package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/romero-project/romero/internal/domain/identity"
	"github.com/romero-project/romero/internal/domain/memory"
	"github.com/romero-project/romero/internal/infrastructure/persistence/sqlcgen"
)

// TenantRecord represents a provisioned organization tenant.
type TenantRecord struct {
	ID        uuid.UUID
	Slug      string
	Name      string
	IsActive  bool
	Settings  map[string]any
	CreatedAt time.Time
	UpdatedAt time.Time
}

// CreateTenantParams carries inputs for inserting a tenant.
type CreateTenantParams struct {
	ID       uuid.UUID
	Slug     string
	Name     string
	IsActive bool
	Settings map[string]any
}

// ProjectRecord represents a provisioned project inside a tenant.
type ProjectRecord struct {
	ID            uuid.UUID
	TenantID      uuid.UUID
	Slug          string
	Name          string
	RepositoryURL string
	Description   string
	IsActive      bool
	Metadata      map[string]any
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// CreateProjectParams carries inputs for inserting a project.
type CreateProjectParams struct {
	ID            uuid.UUID
	TenantID      uuid.UUID
	Slug          string
	Name          string
	RepositoryURL string
	Description   string
	IsActive      bool
	Metadata      map[string]any
}

// AgentRecord represents a provisioned agent registered under a tenant.
type AgentRecord struct {
	ID           uuid.UUID
	TenantID     uuid.UUID
	ExternalKey  string
	Name         string
	RoleType     string
	Capabilities []string
	IsActive     bool
	Metadata     map[string]any
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// CreateAgentParams carries inputs for inserting an agent.
type CreateAgentParams struct {
	ID           uuid.UUID
	TenantID     uuid.UUID
	ExternalKey  string
	Name         string
	RoleType     string
	Capabilities []string
	IsActive     bool
	Metadata     map[string]any
}

// PrincipalRecord represents a provisioned security principal without exposing its SHA-256 hash.
type PrincipalRecord struct {
	ID              uuid.UUID
	TenantID        uuid.UUID
	AgentID         *uuid.UUID
	PrincipalType   identity.PrincipalType
	Name            string
	APIKeyPrefix    string
	AllowedProjects []uuid.UUID
	AllowedScopes   []identity.ScopeType
	IsActive        bool
	ExpiresAt       *time.Time
	CreatedAt       time.Time
	IsAdmin         bool
}

// CreatePrincipalParams carries inputs for inserting a principal credential record.
type CreatePrincipalParams struct {
	ID              uuid.UUID
	TenantID        uuid.UUID
	AgentID         *uuid.UUID
	PrincipalType   identity.PrincipalType
	Name            string
	APIKeyPrefix    string
	APIKeySHA256    string
	AllowedProjects []uuid.UUID
	AllowedScopes   []identity.ScopeType
	IsActive        bool
	ExpiresAt       *time.Time
	IsAdmin         bool
}

// PrincipalAuthCandidate holds principal metadata and key digest for constant-time authentication.
type PrincipalAuthCandidate struct {
	Principal    PrincipalRecord
	APIKeySHA256 string
	TenantActive bool
	AgentActive  bool
}

// ProvisioningStore implements PostgreSQL identity, provisioning, and credential lookups.
type ProvisioningStore struct {
	pool    *pgxpool.Pool
	queries *sqlcgen.Queries
}

// ExecTx runs fn inside an ACID READ COMMITTED transaction bound to a transactional
// ProvisioningStore and an AuditLogger whose RecordDenied runs outside the transaction rollback.
func (p *ProvisioningStore) ExecTx(ctx context.Context, fn func(txStore *ProvisioningStore, audit memory.AuditLogger) error) error {
	if p == nil || p.pool == nil || p.queries == nil {
		return fmt.Errorf("%w: provisioning store pool is required", ErrInvalidInput)
	}
	if fn == nil {
		return fmt.Errorf("%w: nil transaction callback", ErrInvalidInput)
	}
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	committed := false
	defer rollbackTx(&committed, tx)

	txQ := p.queries.WithTx(tx)
	txStore := &ProvisioningStore{queries: txQ}
	audit := &auditRepo{
		allowedQueries: txQ,
		deniedQueries:  p.queries,
	}
	if err := fn(txStore, audit); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	committed = true
	return nil
}

// CreateTenant inserts a new tenant row.
func (p *ProvisioningStore) CreateTenant(ctx context.Context, arg CreateTenantParams) (TenantRecord, error) {
	if p == nil || p.queries == nil {
		return TenantRecord{}, fmt.Errorf("%w: provisioning store uninitialized", ErrInvalidInput)
	}
	if strings.TrimSpace(arg.Slug) == "" || strings.TrimSpace(arg.Name) == "" {
		return TenantRecord{}, fmt.Errorf("%w: tenant slug and name are required", ErrInvalidInput)
	}
	id := arg.ID
	if id == uuid.Nil {
		id = uuid.New()
	}
	settings, err := marshalJSONMap(arg.Settings)
	if err != nil {
		return TenantRecord{}, err
	}
	row, err := p.queries.InsertTenant(ctx, sqlcgen.InsertTenantParams{
		ID:       id,
		Slug:     strings.TrimSpace(arg.Slug),
		Name:     strings.TrimSpace(arg.Name),
		IsActive: arg.IsActive,
		Settings: settings,
	})
	if err != nil {
		return TenantRecord{}, mapProvisioningPGError(err)
	}
	return toTenantRecord(row)
}

// GetTenantByID looks up a tenant by ID.
func (p *ProvisioningStore) GetTenantByID(ctx context.Context, id uuid.UUID) (*TenantRecord, error) {
	if p == nil || p.queries == nil {
		return nil, fmt.Errorf("%w: provisioning store uninitialized", ErrInvalidInput)
	}
	if id == uuid.Nil {
		return nil, memory.ErrInvalidIdentifier
	}
	row, err := p.queries.GetTenantByID(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, memory.ErrNotFound
		}
		return nil, err
	}
	rec, err := toTenantRecord(row)
	if err != nil {
		return nil, err
	}
	return &rec, nil
}

// GetTenantBySlug looks up a tenant by unique slug.
func (p *ProvisioningStore) GetTenantBySlug(ctx context.Context, slug string) (*TenantRecord, error) {
	if p == nil || p.queries == nil {
		return nil, fmt.Errorf("%w: provisioning store uninitialized", ErrInvalidInput)
	}
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return nil, fmt.Errorf("%w: tenant slug is required", ErrInvalidInput)
	}
	row, err := p.queries.GetTenantBySlug(ctx, slug)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, memory.ErrNotFound
		}
		return nil, err
	}
	rec, err := toTenantRecord(row)
	if err != nil {
		return nil, err
	}
	return &rec, nil
}

// CreateProject inserts a project scoped to arg.TenantID.
func (p *ProvisioningStore) CreateProject(ctx context.Context, arg CreateProjectParams) (ProjectRecord, error) {
	if p == nil || p.queries == nil {
		return ProjectRecord{}, fmt.Errorf("%w: provisioning store uninitialized", ErrInvalidInput)
	}
	if arg.TenantID == uuid.Nil || strings.TrimSpace(arg.Slug) == "" || strings.TrimSpace(arg.Name) == "" {
		return ProjectRecord{}, fmt.Errorf("%w: project tenant_id, slug, and name are required", ErrInvalidInput)
	}
	id := arg.ID
	if id == uuid.Nil {
		id = uuid.New()
	}
	meta, err := marshalJSONMap(arg.Metadata)
	if err != nil {
		return ProjectRecord{}, err
	}
	row, err := p.queries.InsertProject(ctx, sqlcgen.InsertProjectParams{
		ID:            id,
		TenantID:      arg.TenantID,
		Slug:          strings.TrimSpace(arg.Slug),
		Name:          strings.TrimSpace(arg.Name),
		RepositoryUrl: strings.TrimSpace(arg.RepositoryURL),
		Description:   strings.TrimSpace(arg.Description),
		IsActive:      arg.IsActive,
		Metadata:      meta,
	})
	if err != nil {
		return ProjectRecord{}, mapProvisioningPGError(err)
	}
	return toProjectRecord(row)
}

// GetProjectByID looks up a project scoped strictly to tenantID.
func (p *ProvisioningStore) GetProjectByID(ctx context.Context, tenantID, id uuid.UUID) (*ProjectRecord, error) {
	if p == nil || p.queries == nil {
		return nil, fmt.Errorf("%w: provisioning store uninitialized", ErrInvalidInput)
	}
	if tenantID == uuid.Nil || id == uuid.Nil {
		return nil, memory.ErrInvalidIdentifier
	}
	row, err := p.queries.GetProjectByID(ctx, sqlcgen.GetProjectByIDParams{TenantID: tenantID, ID: id})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, memory.ErrNotFound
		}
		return nil, err
	}
	rec, err := toProjectRecord(row)
	if err != nil {
		return nil, err
	}
	return &rec, nil
}

// GetProjectBySlug looks up a project by slug scoped strictly to tenantID.
func (p *ProvisioningStore) GetProjectBySlug(ctx context.Context, tenantID uuid.UUID, slug string) (*ProjectRecord, error) {
	if p == nil || p.queries == nil {
		return nil, fmt.Errorf("%w: provisioning store uninitialized", ErrInvalidInput)
	}
	slug = strings.TrimSpace(slug)
	if tenantID == uuid.Nil || slug == "" {
		return nil, fmt.Errorf("%w: tenant_id and project slug are required", ErrInvalidInput)
	}
	row, err := p.queries.GetProjectBySlug(ctx, sqlcgen.GetProjectBySlugParams{TenantID: tenantID, Slug: slug})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, memory.ErrNotFound
		}
		return nil, err
	}
	rec, err := toProjectRecord(row)
	if err != nil {
		return nil, err
	}
	return &rec, nil
}

// CountTenantProjects counts how many of ids belong to tenantID.
func (p *ProvisioningStore) CountTenantProjects(ctx context.Context, tenantID uuid.UUID, ids []uuid.UUID) (int64, error) {
	if p == nil || p.queries == nil {
		return 0, fmt.Errorf("%w: provisioning store uninitialized", ErrInvalidInput)
	}
	if tenantID == uuid.Nil {
		return 0, memory.ErrInvalidIdentifier
	}
	if len(ids) == 0 {
		return 0, nil
	}
	return p.queries.CountTenantProjectsByIDs(ctx, sqlcgen.CountTenantProjectsByIDsParams{
		TenantID: tenantID,
		Ids:      ids,
	})
}

// CreateAgent inserts an agent scoped to arg.TenantID.
func (p *ProvisioningStore) CreateAgent(ctx context.Context, arg CreateAgentParams) (AgentRecord, error) {
	if p == nil || p.queries == nil {
		return AgentRecord{}, fmt.Errorf("%w: provisioning store uninitialized", ErrInvalidInput)
	}
	if arg.TenantID == uuid.Nil || strings.TrimSpace(arg.ExternalKey) == "" || strings.TrimSpace(arg.Name) == "" {
		return AgentRecord{}, fmt.Errorf("%w: agent tenant_id, external_key, and name are required", ErrInvalidInput)
	}
	id := arg.ID
	if id == uuid.Nil {
		id = uuid.New()
	}
	role := strings.TrimSpace(arg.RoleType)
	if role == "" {
		role = "general"
	}
	meta, err := marshalJSONMap(arg.Metadata)
	if err != nil {
		return AgentRecord{}, err
	}
	row, err := p.queries.InsertAgent(ctx, sqlcgen.InsertAgentParams{
		ID:           id,
		TenantID:     arg.TenantID,
		ExternalKey:  strings.TrimSpace(arg.ExternalKey),
		Name:         strings.TrimSpace(arg.Name),
		RoleType:     role,
		Capabilities: nonNilSlice(arg.Capabilities),
		IsActive:     arg.IsActive,
		Metadata:     meta,
	})
	if err != nil {
		return AgentRecord{}, mapProvisioningPGError(err)
	}
	return toAgentRecord(row)
}

// GetAgentByID looks up an agent scoped strictly to tenantID.
func (p *ProvisioningStore) GetAgentByID(ctx context.Context, tenantID, id uuid.UUID) (*AgentRecord, error) {
	if p == nil || p.queries == nil {
		return nil, fmt.Errorf("%w: provisioning store uninitialized", ErrInvalidInput)
	}
	if tenantID == uuid.Nil || id == uuid.Nil {
		return nil, memory.ErrInvalidIdentifier
	}
	row, err := p.queries.GetAgentByID(ctx, sqlcgen.GetAgentByIDParams{TenantID: tenantID, ID: id})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, memory.ErrNotFound
		}
		return nil, err
	}
	rec, err := toAgentRecord(row)
	if err != nil {
		return nil, err
	}
	return &rec, nil
}

// GetAgentByExternalKey looks up an agent by external_key scoped strictly to tenantID.
func (p *ProvisioningStore) GetAgentByExternalKey(ctx context.Context, tenantID uuid.UUID, externalKey string) (*AgentRecord, error) {
	if p == nil || p.queries == nil {
		return nil, fmt.Errorf("%w: provisioning store uninitialized", ErrInvalidInput)
	}
	externalKey = strings.TrimSpace(externalKey)
	if tenantID == uuid.Nil || externalKey == "" {
		return nil, fmt.Errorf("%w: tenant_id and external_key are required", ErrInvalidInput)
	}
	row, err := p.queries.GetAgentByExternalKey(ctx, sqlcgen.GetAgentByExternalKeyParams{
		TenantID:    tenantID,
		ExternalKey: externalKey,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, memory.ErrNotFound
		}
		return nil, err
	}
	rec, err := toAgentRecord(row)
	if err != nil {
		return nil, err
	}
	return &rec, nil
}

// CreatePrincipal inserts a principal credential row.
func (p *ProvisioningStore) CreatePrincipal(ctx context.Context, arg CreatePrincipalParams) (PrincipalRecord, error) {
	if p == nil || p.queries == nil {
		return PrincipalRecord{}, fmt.Errorf("%w: provisioning store uninitialized", ErrInvalidInput)
	}
	if arg.TenantID == uuid.Nil || strings.TrimSpace(arg.Name) == "" ||
		strings.TrimSpace(arg.APIKeyPrefix) == "" || len(arg.APIKeySHA256) != 64 {
		return PrincipalRecord{}, fmt.Errorf("%w: invalid principal credentials or identifiers", ErrInvalidInput)
	}
	id := arg.ID
	if id == uuid.Nil {
		id = uuid.New()
	}
	projects := arg.AllowedProjects
	if projects == nil {
		projects = []uuid.UUID{}
	}
	scopes := make([]string, 0, len(arg.AllowedScopes))
	for _, s := range arg.AllowedScopes {
		scopes = append(scopes, string(s))
	}
	row, err := p.queries.InsertPrincipal(ctx, sqlcgen.InsertPrincipalParams{
		ID:              id,
		TenantID:        arg.TenantID,
		AgentID:         toNullUUID(arg.AgentID),
		PrincipalType:   string(arg.PrincipalType),
		Name:            strings.TrimSpace(arg.Name),
		ApiKeyPrefix:    arg.APIKeyPrefix,
		ApiKeySha256:    arg.APIKeySHA256,
		AllowedProjects: projects,
		AllowedScopes:   scopes,
		IsActive:        arg.IsActive,
		ExpiresAt:       arg.ExpiresAt,
		IsAdmin:         arg.IsAdmin,
	})
	if err != nil {
		return PrincipalRecord{}, mapProvisioningPGError(err)
	}
	return PrincipalRecord{
		ID:              row.ID,
		TenantID:        row.TenantID,
		AgentID:         fromNullUUID(row.AgentID),
		PrincipalType:   identity.PrincipalType(row.PrincipalType),
		Name:            row.Name,
		APIKeyPrefix:    row.ApiKeyPrefix,
		AllowedProjects: append([]uuid.UUID(nil), row.AllowedProjects...),
		AllowedScopes:   toScopeTypes(row.AllowedScopes),
		IsActive:        row.IsActive,
		ExpiresAt:       row.ExpiresAt,
		CreatedAt:       row.CreatedAt,
		IsAdmin:         row.IsAdmin,
	}, nil
}

// GetPrincipalByID looks up a principal scoped strictly to tenantID.
func (p *ProvisioningStore) GetPrincipalByID(ctx context.Context, tenantID, id uuid.UUID) (*PrincipalRecord, error) {
	if p == nil || p.queries == nil {
		return nil, fmt.Errorf("%w: provisioning store uninitialized", ErrInvalidInput)
	}
	if tenantID == uuid.Nil || id == uuid.Nil {
		return nil, memory.ErrInvalidIdentifier
	}
	row, err := p.queries.GetPrincipalByID(ctx, sqlcgen.GetPrincipalByIDParams{TenantID: tenantID, ID: id})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, memory.ErrNotFound
		}
		return nil, err
	}
	rec := PrincipalRecord{
		ID:              row.ID,
		TenantID:        row.TenantID,
		AgentID:         fromNullUUID(row.AgentID),
		PrincipalType:   identity.PrincipalType(row.PrincipalType),
		Name:            row.Name,
		APIKeyPrefix:    row.ApiKeyPrefix,
		AllowedProjects: append([]uuid.UUID(nil), row.AllowedProjects...),
		AllowedScopes:   toScopeTypes(row.AllowedScopes),
		IsActive:        row.IsActive,
		ExpiresAt:       row.ExpiresAt,
		CreatedAt:       row.CreatedAt,
		IsAdmin:         row.IsAdmin,
	}
	return &rec, nil
}

// LookupPrincipalsByPrefix retrieves candidate principals sharing api_key_prefix along with tenant/agent status.
func (p *ProvisioningStore) LookupPrincipalsByPrefix(ctx context.Context, prefix string) ([]PrincipalAuthCandidate, error) {
	if p == nil || p.queries == nil {
		return nil, fmt.Errorf("%w: provisioning store uninitialized", ErrInvalidInput)
	}
	if strings.TrimSpace(prefix) == "" {
		return nil, fmt.Errorf("%w: api_key_prefix is required", ErrInvalidInput)
	}
	rows, err := p.queries.LookupPrincipalsByPrefix(ctx, prefix)
	if err != nil {
		return nil, err
	}
	out := make([]PrincipalAuthCandidate, 0, len(rows))
	for _, r := range rows {
		out = append(out, PrincipalAuthCandidate{
			Principal: PrincipalRecord{
				ID:              r.ID,
				TenantID:        r.TenantID,
				AgentID:         fromNullUUID(r.AgentID),
				PrincipalType:   identity.PrincipalType(r.PrincipalType),
				Name:            r.Name,
				APIKeyPrefix:    r.ApiKeyPrefix,
				AllowedProjects: append([]uuid.UUID(nil), r.AllowedProjects...),
				AllowedScopes:   toScopeTypes(r.AllowedScopes),
				IsActive:        r.IsActive,
				ExpiresAt:       r.ExpiresAt,
				CreatedAt:       r.CreatedAt,
				IsAdmin:         r.IsAdmin,
			},
			APIKeySHA256: r.ApiKeySha256,
			TenantActive: r.TenantActive,
			AgentActive:  r.AgentActive,
		})
	}
	return out, nil
}

// UpsertTaskPermission creates or updates a principal's task_shared permission row on this store/transaction.
func (p *ProvisioningStore) UpsertTaskPermission(ctx context.Context, grant TaskPermissionGrant) error {
	if p == nil || p.queries == nil {
		return fmt.Errorf("%w: provisioning store uninitialized", ErrInvalidInput)
	}
	if grant.TenantID == uuid.Nil || grant.ProjectID == uuid.Nil || grant.PrincipalID == uuid.Nil || strings.TrimSpace(grant.TaskID) == "" {
		return fmt.Errorf("%w: tenant_id, project_id, principal_id, and task_id are required", ErrInvalidInput)
	}
	_, err := p.queries.UpsertTaskPermission(ctx, sqlcgen.UpsertTaskPermissionParams{
		TenantID:    grant.TenantID,
		ProjectID:   grant.ProjectID,
		TaskID:      strings.TrimSpace(grant.TaskID),
		PrincipalID: grant.PrincipalID,
		CanRead:     grant.CanRead,
		CanWrite:    grant.CanWrite,
	})
	return mapProvisioningPGError(err)
}

func toTenantRecord(row sqlcgen.Tenant) (TenantRecord, error) {
	settings, err := unmarshalJSONMap(row.Settings)
	if err != nil {
		return TenantRecord{}, err
	}
	return TenantRecord{
		ID:        row.ID,
		Slug:      row.Slug,
		Name:      row.Name,
		IsActive:  row.IsActive,
		Settings:  settings,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
	}, nil
}

func toProjectRecord(row sqlcgen.Project) (ProjectRecord, error) {
	meta, err := unmarshalJSONMap(row.Metadata)
	if err != nil {
		return ProjectRecord{}, err
	}
	return ProjectRecord{
		ID:            row.ID,
		TenantID:      row.TenantID,
		Slug:          row.Slug,
		Name:          row.Name,
		RepositoryURL: fromPgText(row.RepositoryUrl),
		Description:   fromPgText(row.Description),
		IsActive:      row.IsActive,
		Metadata:      meta,
		CreatedAt:     row.CreatedAt,
		UpdatedAt:     row.UpdatedAt,
	}, nil
}

func toAgentRecord(row sqlcgen.Agent) (AgentRecord, error) {
	meta, err := unmarshalJSONMap(row.Metadata)
	if err != nil {
		return AgentRecord{}, err
	}
	return AgentRecord{
		ID:           row.ID,
		TenantID:     row.TenantID,
		ExternalKey:  row.ExternalKey,
		Name:         row.Name,
		RoleType:     row.RoleType,
		Capabilities: append([]string(nil), row.Capabilities...),
		IsActive:     row.IsActive,
		Metadata:     meta,
		CreatedAt:    row.CreatedAt,
		UpdatedAt:    row.UpdatedAt,
	}, nil
}

func toScopeTypes(raw []string) []identity.ScopeType {
	out := make([]identity.ScopeType, 0, len(raw))
	for _, s := range raw {
		out = append(out, identity.ScopeType(s))
	}
	return out
}

func mapProvisioningPGError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			return fmt.Errorf("%w: %s", ErrAlreadyExists, pgErr.ConstraintName)
		case "23503", "23514":
			return fmt.Errorf("%w: %s", ErrInvalidInput, pgErr.ConstraintName)
		}
	}
	return err
}
