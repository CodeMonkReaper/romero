// Package provisioning implements Zero-Trust tenant/project/principal provisioning,
// SHA-256 API key authentication, and project-scoped task_permission administration.
package provisioning

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/romero-project/romero/internal/domain/identity"
	"github.com/romero-project/romero/internal/domain/memory"
)

var (
	// ErrUnauthenticated is returned when an API key is missing, malformed, unknown, inactive, or expired.
	ErrUnauthenticated = errors.New("unauthenticated")
	// ErrForbidden mirrors identity.ErrForbidden for policy/authority denials.
	ErrForbidden = identity.ErrForbidden
	// ErrNotFound mirrors memory.ErrNotFound when a target tenant, project, agent, or principal does not exist.
	ErrNotFound = memory.ErrNotFound
	// ErrInvalidInput indicates invalid provisioning arguments.
	ErrInvalidInput = errors.New("invalid provisioning input")
	// ErrAlreadyExists indicates a unique constraint conflict (e.g. duplicate tenant or project slug).
	ErrAlreadyExists = errors.New("resource already exists")
	// ErrAuditUnavailable indicates denied-audit logging failed; callers fail closed without granting access.
	ErrAuditUnavailable = errors.New("audit logger unavailable")
)

// Tenant represents an isolated organization boundary.
type Tenant struct {
	ID       uuid.UUID
	Slug     string
	Name     string
	IsActive bool
}

// CreateTenantParams carries inputs for inserting a tenant.
type CreateTenantParams struct {
	ID       uuid.UUID
	Slug     string
	Name     string
	IsActive bool
	Settings map[string]any
}

// Project represents a workspace inside a tenant.
type Project struct {
	ID       uuid.UUID
	TenantID uuid.UUID
	Slug     string
	Name     string
	IsActive bool
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

// Agent represents a registered cognitive agent identity inside a tenant.
type Agent struct {
	ID          uuid.UUID
	TenantID    uuid.UUID
	ExternalKey string
	Name        string
	RoleType    string
	IsActive    bool
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

// Principal represents a security credential holder (without exposing its secret digest).
type Principal struct {
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

// PrincipalView aliases Principal for public readouts returned by provisioning operations.
type PrincipalView = Principal

// CreatePrincipalParams carries hashed credentials and grants to persist.
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

// PrincipalAuthCandidate holds persisted key digest and status flags for authentication.
type PrincipalAuthCandidate struct {
	Principal    Principal
	APIKeySHA256 string
	TenantActive bool
	AgentActive  bool
}

// TaskPermissionGrant captures an explicit task_shared ACL mutation.
type TaskPermissionGrant struct {
	TenantID    uuid.UUID
	ProjectID   uuid.UUID
	TaskID      string
	PrincipalID uuid.UUID
	CanRead     bool
	CanWrite    bool
}

// PrefixLookup resolves candidate principals indexed by their API key prefix.
type PrefixLookup interface {
	LookupPrincipalsByPrefix(ctx context.Context, prefix string) ([]PrincipalAuthCandidate, error)
}

// Repository defines persistence operations needed by ProvisioningService.
type Repository interface {
	PrefixLookup
	CreateTenant(ctx context.Context, arg CreateTenantParams) (Tenant, error)
	GetTenantByID(ctx context.Context, id uuid.UUID) (*Tenant, error)
	CreateProject(ctx context.Context, arg CreateProjectParams) (Project, error)
	GetProjectByID(ctx context.Context, tenantID, id uuid.UUID) (*Project, error)
	CountTenantProjects(ctx context.Context, tenantID uuid.UUID, ids []uuid.UUID) (int64, error)
	CreateAgent(ctx context.Context, arg CreateAgentParams) (Agent, error)
	GetAgentByID(ctx context.Context, tenantID, id uuid.UUID) (*Agent, error)
	CreatePrincipal(ctx context.Context, arg CreatePrincipalParams) (Principal, error)
	GetPrincipalByID(ctx context.Context, tenantID, id uuid.UUID) (*Principal, error)
	UpsertTaskPermission(ctx context.Context, grant TaskPermissionGrant) error
}

// UnitOfWork runs provisioning writes and their allowed audit event atomically in one transaction.
type UnitOfWork interface {
	ExecTx(ctx context.Context, fn func(repo Repository, audit memory.AuditLogger) error) error
}
