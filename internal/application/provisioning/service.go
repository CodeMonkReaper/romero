package provisioning

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/romero-project/romero/internal/domain/identity"
	"github.com/romero-project/romero/internal/domain/memory"
)

// ServiceOption configures optional dependencies on Service.
type ServiceOption func(*Service)

// WithClock injects a deterministic UTC clock for expiration and audit timestamps.
func WithClock(nowFn func() time.Time) ServiceOption {
	return func(s *Service) {
		if nowFn != nil {
			s.nowFn = nowFn
		}
	}
}

// BootstrapTaskGrant configures an initial task_shared grant applied during Bootstrap.
type BootstrapTaskGrant struct {
	TaskID   string
	CanRead  bool
	CanWrite bool
}

// BootstrapSpec defines inputs for atomic initialization of a tenant, project, optional agent, and root principal.
type BootstrapSpec struct {
	TenantSlug        string
	TenantName        string
	ProjectSlug       string
	ProjectName       string
	ProjectRepoURL    string
	ProjectDesc       string
	AgentExternalKey  string
	AgentName         string
	AgentRoleType     string
	PrincipalName     string
	PrincipalType     identity.PrincipalType
	AllowedScopes     []identity.ScopeType
	IsAdmin           bool
	ExpiresAt         *time.Time
	InitialTaskGrants []BootstrapTaskGrant
}

// BootstrapOutput returns the provisioned resources and the one-time raw API key.
type BootstrapOutput struct {
	Tenant    Tenant
	Project   Project
	Agent     *Agent
	Principal PrincipalView
	RawAPIKey string
}

// CreatePrincipalInput defines inputs for creating a principal inside an existing tenant.
type CreatePrincipalInput struct {
	TenantID        uuid.UUID
	AgentID         *uuid.UUID
	PrincipalType   identity.PrincipalType
	Name            string
	AllowedProjects []uuid.UUID
	AllowedScopes   []identity.ScopeType
	IsAdmin         bool
	ExpiresAt       *time.Time
}

// ManageTaskPermissionCommand carries inputs to grant or revoke task_shared permissions.
type ManageTaskPermissionCommand struct {
	TenantID    uuid.UUID
	ProjectID   uuid.UUID
	TaskID      string
	PrincipalID uuid.UUID
	CanRead     bool
	CanWrite    bool
}

// Service implements Zero-Trust tenant bootstrap, principal creation, and task ACL administration.
type Service struct {
	repo  Repository
	uow   UnitOfWork
	audit memory.AuditLogger
	nowFn func() time.Time
}

// NewService constructs a ProvisioningService backed by repository, UnitOfWork, and audit logger.
func NewService(repo Repository, uow UnitOfWork, audit memory.AuditLogger, opts ...ServiceOption) *Service {
	s := &Service{
		repo:  repo,
		uow:   uow,
		audit: audit,
		nowFn: func() time.Time { return time.Now().UTC() },
	}
	for _, opt := range opts {
		if opt != nil {
			opt(s)
		}
	}
	return s
}

// Bootstrap atomically creates a tenant, initial project, optional agent, initial principal, and optional task grants.
func (s *Service) Bootstrap(ctx context.Context, spec BootstrapSpec) (*BootstrapOutput, error) {
	if s == nil || s.uow == nil {
		return nil, fmt.Errorf("%w: service uninitialized", ErrInvalidInput)
	}
	if err := s.validateBootstrapSpec(spec); err != nil {
		return nil, err
	}

	scopes, err := normalizeScopes(spec.AllowedScopes)
	if err != nil {
		return nil, err
	}

	var out BootstrapOutput
	if err := s.uow.ExecTx(ctx, func(txRepo Repository, txAudit memory.AuditLogger) error {
		tenant, err := txRepo.CreateTenant(ctx, CreateTenantParams{
			ID:       uuid.New(),
			Slug:     strings.TrimSpace(spec.TenantSlug),
			Name:     strings.TrimSpace(spec.TenantName),
			IsActive: true,
		})
		if err != nil {
			return err
		}

		project, err := txRepo.CreateProject(ctx, CreateProjectParams{
			ID:            uuid.New(),
			TenantID:      tenant.ID,
			Slug:          strings.TrimSpace(spec.ProjectSlug),
			Name:          strings.TrimSpace(spec.ProjectName),
			RepositoryURL: strings.TrimSpace(spec.ProjectRepoURL),
			Description:   strings.TrimSpace(spec.ProjectDesc),
			IsActive:      true,
		})
		if err != nil {
			return err
		}

		var agentPtr *Agent
		var agentIDPtr *uuid.UUID
		if extKey := strings.TrimSpace(spec.AgentExternalKey); extKey != "" {
			agName := strings.TrimSpace(spec.AgentName)
			if agName == "" {
				agName = extKey
			}
			ag, err := txRepo.CreateAgent(ctx, CreateAgentParams{
				ID:          uuid.New(),
				TenantID:    tenant.ID,
				ExternalKey: extKey,
				Name:        agName,
				RoleType:    strings.TrimSpace(spec.AgentRoleType),
				IsActive:    true,
			})
			if err != nil {
				return err
			}
			agentPtr = &ag
			aid := ag.ID
			agentIDPtr = &aid
		}

		principalID := uuid.New()
		rawKey, prefix, shaHex, err := GenerateAPIKey(spec.PrincipalType, principalID)
		if err != nil {
			return err
		}

		principal, err := txRepo.CreatePrincipal(ctx, CreatePrincipalParams{
			ID:              principalID,
			TenantID:        tenant.ID,
			AgentID:         agentIDPtr,
			PrincipalType:   spec.PrincipalType,
			Name:            strings.TrimSpace(spec.PrincipalName),
			APIKeyPrefix:    prefix,
			APIKeySHA256:    shaHex,
			AllowedProjects: []uuid.UUID{project.ID},
			AllowedScopes:   scopes,
			IsActive:        true,
			ExpiresAt:       spec.ExpiresAt,
			IsAdmin:         spec.IsAdmin,
		})
		if err != nil {
			return err
		}

		for _, grant := range spec.InitialTaskGrants {
			if err := txRepo.UpsertTaskPermission(ctx, TaskPermissionGrant{
				TenantID:    tenant.ID,
				ProjectID:   project.ID,
				TaskID:      strings.TrimSpace(grant.TaskID),
				PrincipalID: principal.ID,
				CanRead:     grant.CanRead,
				CanWrite:    grant.CanWrite,
			}); err != nil {
				return err
			}
		}

		tid := tenant.ID
		pid := principal.ID
		if err := txAudit.RecordAllowed(ctx, memory.AuditEvent{
			ID:           uuid.New(),
			TenantID:     &tid,
			PrincipalID:  &pid,
			AgentID:      agentIDPtr,
			Operation:    "bootstrap",
			ResourceType: "tenant",
			ResourceID:   &tid,
			Outcome:      memory.AuditAllowed,
			Details: map[string]any{
				"tenant_slug":  tenant.Slug,
				"project_slug": project.Slug,
			},
			CreatedAt: s.nowFn(),
		}); err != nil {
			return err
		}

		out = BootstrapOutput{
			Tenant:    tenant,
			Project:   project,
			Agent:     agentPtr,
			Principal: principal,
			RawAPIKey: rawKey,
		}
		return nil
	}); err != nil {
		return nil, err
	}

	return &out, nil
}

// CreatePrincipal provisions a new API credential scoped strictly within an existing tenant.
func (s *Service) CreatePrincipal(ctx context.Context, req CreatePrincipalInput) (string, PrincipalView, error) {
	if s == nil || s.uow == nil {
		return "", PrincipalView{}, fmt.Errorf("%w: service uninitialized", ErrInvalidInput)
	}
	if err := s.validateCreatePrincipalInput(req); err != nil {
		return "", PrincipalView{}, err
	}

	scopes, err := normalizeScopes(req.AllowedScopes)
	if err != nil {
		return "", PrincipalView{}, err
	}
	projects, err := dedupeProjectIDs(req.AllowedProjects)
	if err != nil {
		return "", PrincipalView{}, err
	}

	var rawKey string
	var created PrincipalView
	if err := s.uow.ExecTx(ctx, func(txRepo Repository, txAudit memory.AuditLogger) error {
		tenant, err := txRepo.GetTenantByID(ctx, req.TenantID)
		if err != nil {
			return err
		}
		if !tenant.IsActive {
			return fmt.Errorf("%w: tenant is inactive", ErrInvalidInput)
		}

		if req.AgentID != nil {
			ag, err := txRepo.GetAgentByID(ctx, req.TenantID, *req.AgentID)
			if err != nil {
				return err
			}
			if !ag.IsActive {
				return fmt.Errorf("%w: agent is inactive", ErrInvalidInput)
			}
		}

		if len(projects) > 0 {
			cnt, err := txRepo.CountTenantProjects(ctx, req.TenantID, projects)
			if err != nil {
				return err
			}
			if cnt != int64(len(projects)) {
				return fmt.Errorf("%w: one or more allowed_projects do not belong to tenant", ErrInvalidInput)
			}
		}

		principalID := uuid.New()
		var prefix, shaHex string
		rawKey, prefix, shaHex, err = GenerateAPIKey(req.PrincipalType, principalID)
		if err != nil {
			return err
		}

		created, err = txRepo.CreatePrincipal(ctx, CreatePrincipalParams{
			ID:              principalID,
			TenantID:        req.TenantID,
			AgentID:         copyUUIDPtr(req.AgentID),
			PrincipalType:   req.PrincipalType,
			Name:            strings.TrimSpace(req.Name),
			APIKeyPrefix:    prefix,
			APIKeySHA256:    shaHex,
			AllowedProjects: projects,
			AllowedScopes:   scopes,
			IsActive:        true,
			ExpiresAt:       req.ExpiresAt,
			IsAdmin:         req.IsAdmin,
		})
		if err != nil {
			return err
		}

		tid := req.TenantID
		pid := created.ID
		return txAudit.RecordAllowed(ctx, memory.AuditEvent{
			ID:           uuid.New(),
			TenantID:     &tid,
			PrincipalID:  &pid,
			AgentID:      copyUUIDPtr(req.AgentID),
			Operation:    "principal.create",
			ResourceType: "principal",
			ResourceID:   &pid,
			Outcome:      memory.AuditAllowed,
			Details: map[string]any{
				"principal_type": string(req.PrincipalType),
				"is_admin":       req.IsAdmin,
			},
			CreatedAt: s.nowFn(),
		})
	}); err != nil {
		return "", PrincipalView{}, err
	}

	return rawKey, created, nil
}

// ManageTaskPermission grants or revokes task_shared permissions after verifying tenant and project authority.
func (s *Service) ManageTaskPermission(ctx context.Context, actor identity.AuthContext, cmd ManageTaskPermissionCommand) error {
	if s == nil || s.uow == nil {
		return fmt.Errorf("%w: service uninitialized", ErrInvalidInput)
	}
	taskID := strings.TrimSpace(cmd.TaskID)
	if cmd.TenantID == uuid.Nil || cmd.ProjectID == uuid.Nil || cmd.PrincipalID == uuid.Nil || taskID == "" {
		return fmt.Errorf("%w: tenant_id, project_id, principal_id, and task_id are required", ErrInvalidInput)
	}

	op := "task_permission.grant"
	if !cmd.CanRead && !cmd.CanWrite {
		op = "task_permission.revoke"
	}

	if !canAdministerTaskPermissions(actor, cmd.TenantID, cmd.ProjectID) {
		if err := s.recordDeniedTaskAdmin(ctx, actor, cmd, taskID, op); err != nil {
			return err
		}
		return ErrForbidden
	}

	return s.uow.ExecTx(ctx, func(txRepo Repository, txAudit memory.AuditLogger) error {
		if _, err := txRepo.GetProjectByID(ctx, cmd.TenantID, cmd.ProjectID); err != nil {
			return err
		}
		if _, err := txRepo.GetPrincipalByID(ctx, cmd.TenantID, cmd.PrincipalID); err != nil {
			return err
		}
		if err := txRepo.UpsertTaskPermission(ctx, TaskPermissionGrant{
			TenantID:    cmd.TenantID,
			ProjectID:   cmd.ProjectID,
			TaskID:      taskID,
			PrincipalID: cmd.PrincipalID,
			CanRead:     cmd.CanRead,
			CanWrite:    cmd.CanWrite,
		}); err != nil {
			return err
		}

		tid := actor.TenantID
		actorID := actor.PrincipalID
		targetID := cmd.PrincipalID
		return txAudit.RecordAllowed(ctx, memory.AuditEvent{
			ID:           uuid.New(),
			TenantID:     &tid,
			PrincipalID:  &actorID,
			AgentID:      copyUUIDPtr(actor.AgentID),
			Operation:    op,
			ResourceType: "task_permission",
			ResourceID:   &targetID,
			ScopeUsed:    string(identity.ScopeTaskShared),
			Outcome:      memory.AuditAllowed,
			Details: map[string]any{
				"project_id":          cmd.ProjectID.String(),
				"task_id":             taskID,
				"target_principal_id": cmd.PrincipalID.String(),
				"can_read":            cmd.CanRead,
				"can_write":           cmd.CanWrite,
			},
			CreatedAt: s.nowFn(),
		})
	})
}

func (s *Service) validateBootstrapSpec(spec BootstrapSpec) error {
	if strings.TrimSpace(spec.TenantSlug) == "" || strings.TrimSpace(spec.TenantName) == "" {
		return fmt.Errorf("%w: tenant_slug and tenant_name are required", ErrInvalidInput)
	}
	if strings.TrimSpace(spec.ProjectSlug) == "" || strings.TrimSpace(spec.ProjectName) == "" {
		return fmt.Errorf("%w: project_slug and project_name are required", ErrInvalidInput)
	}
	if strings.TrimSpace(spec.PrincipalName) == "" || !validPrincipalType(spec.PrincipalType) {
		return fmt.Errorf("%w: valid principal_name and principal_type are required", ErrInvalidInput)
	}
	if spec.PrincipalType == identity.PrincipalAgent && spec.IsAdmin {
		return fmt.Errorf("%w: agent principals cannot be administrators", ErrForbidden)
	}
	if spec.PrincipalType == identity.PrincipalAgent && strings.TrimSpace(spec.AgentExternalKey) == "" {
		return fmt.Errorf("%w: agent principal requires agent_external_key", ErrInvalidInput)
	}
	if spec.ExpiresAt != nil && !spec.ExpiresAt.After(s.nowFn()) {
		return fmt.Errorf("%w: expires_at must be in the future", ErrInvalidInput)
	}
	seenTasks := make(map[string]struct{}, len(spec.InitialTaskGrants))
	for _, g := range spec.InitialTaskGrants {
		taskID := strings.TrimSpace(g.TaskID)
		if taskID == "" {
			return fmt.Errorf("%w: bootstrap task grant requires task_id", ErrInvalidInput)
		}
		if _, dup := seenTasks[taskID]; dup {
			return fmt.Errorf("%w: duplicate task_id %q in initial task grants", ErrInvalidInput, taskID)
		}
		seenTasks[taskID] = struct{}{}
	}
	return nil
}

func (s *Service) validateCreatePrincipalInput(req CreatePrincipalInput) error {
	if req.TenantID == uuid.Nil || strings.TrimSpace(req.Name) == "" || !validPrincipalType(req.PrincipalType) {
		return fmt.Errorf("%w: tenant_id, name, and valid principal_type are required", ErrInvalidInput)
	}
	if req.PrincipalType == identity.PrincipalAgent && req.IsAdmin {
		return fmt.Errorf("%w: agent principals cannot be administrators", ErrForbidden)
	}
	if req.PrincipalType == identity.PrincipalAgent && (req.AgentID == nil || *req.AgentID == uuid.Nil) {
		return fmt.Errorf("%w: agent principal requires agent_id", ErrInvalidInput)
	}
	if req.AgentID != nil && *req.AgentID == uuid.Nil {
		return fmt.Errorf("%w: agent_id must not be nil UUID", ErrInvalidInput)
	}
	if req.ExpiresAt != nil && !req.ExpiresAt.After(s.nowFn()) {
		return fmt.Errorf("%w: expires_at must be in the future", ErrInvalidInput)
	}
	return nil
}

func canAdministerTaskPermissions(actor identity.AuthContext, targetTenantID, targetProjectID uuid.UUID) bool {
	if actor.TenantID == uuid.Nil || actor.PrincipalID == uuid.Nil || actor.TenantID != targetTenantID {
		return false
	}
	if actor.PrincipalType == identity.PrincipalAgent {
		return false
	}
	if actor.IsAdmin && (actor.PrincipalType == identity.PrincipalUser ||
		actor.PrincipalType == identity.PrincipalOrchestrator ||
		actor.PrincipalType == identity.PrincipalService) {
		return true
	}
	if actor.PrincipalType == identity.PrincipalOrchestrator && slices.Contains(actor.AllowedProjects, targetProjectID) {
		return true
	}
	return false
}

func (s *Service) recordDeniedTaskAdmin(ctx context.Context, actor identity.AuthContext, cmd ManageTaskPermissionCommand, taskID, op string) error {
	if s.audit == nil {
		return fmt.Errorf("%w: audit logger is nil", ErrAuditUnavailable)
	}
	var tidPtr, pidPtr *uuid.UUID
	if actor.TenantID != uuid.Nil {
		tid := actor.TenantID
		tidPtr = &tid
	}
	if actor.PrincipalID != uuid.Nil {
		pid := actor.PrincipalID
		pidPtr = &pid
	}
	targetID := cmd.PrincipalID
	ev := memory.AuditEvent{
		ID:           uuid.New(),
		TenantID:     tidPtr,
		PrincipalID:  pidPtr,
		AgentID:      copyUUIDPtr(actor.AgentID),
		Operation:    op,
		ResourceType: "task_permission",
		ResourceID:   &targetID,
		ScopeUsed:    string(identity.ScopeTaskShared),
		Outcome:      memory.AuditDenied,
		Details: map[string]any{
			"target_tenant_id":    cmd.TenantID.String(),
			"project_id":          cmd.ProjectID.String(),
			"task_id":             taskID,
			"target_principal_id": cmd.PrincipalID.String(),
		},
		CreatedAt: s.nowFn(),
	}
	if err := s.audit.RecordDenied(ctx, ev); err != nil {
		return fmt.Errorf("%w: %v", ErrAuditUnavailable, err)
	}
	return nil
}

func normalizeScopes(raw []identity.ScopeType) ([]identity.ScopeType, error) {
	out := make([]identity.ScopeType, 0, len(raw))
	for _, s := range raw {
		if !validScopeType(s) {
			return nil, fmt.Errorf("%w: invalid scope %q", ErrInvalidInput, s)
		}
		if !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	return out, nil
}

func dedupeProjectIDs(raw []uuid.UUID) ([]uuid.UUID, error) {
	out := make([]uuid.UUID, 0, len(raw))
	for _, id := range raw {
		if id == uuid.Nil {
			return nil, fmt.Errorf("%w: allowed_projects cannot contain nil UUID", ErrInvalidInput)
		}
		if !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out, nil
}
