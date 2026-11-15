package postgres_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/romero-project/romero/internal/domain/identity"
	"github.com/romero-project/romero/internal/domain/memory"
	"github.com/romero-project/romero/internal/infrastructure/persistence/postgres"
)

func TestProvisioningStoreLifecycleAndIsolation(t *testing.T) {
	pool := openTestPool(t)
	store, err := postgres.NewStore(pool)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	prov := store.Provisioning()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	tenantID := uuid.New()
	tenant, err := prov.CreateTenant(ctx, postgres.CreateTenantParams{
		ID:       tenantID,
		Slug:     "acme-" + tenantID.String()[:8],
		Name:     "Acme Corp",
		IsActive: true,
		Settings: map[string]any{"tier": "enterprise"},
	})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	if tenant.ID != tenantID || !tenant.IsActive {
		t.Fatalf("unexpected tenant: %+v", tenant)
	}

	// Duplicate slug must map to ErrAlreadyExists.
	_, err = prov.CreateTenant(ctx, postgres.CreateTenantParams{
		ID:       uuid.New(),
		Slug:     tenant.Slug,
		Name:     "Acme Duplicate",
		IsActive: true,
	})
	if !errors.Is(err, postgres.ErrAlreadyExists) {
		t.Fatalf("expected ErrAlreadyExists on duplicate tenant slug, got %v", err)
	}

	otherTenantID := uuid.New()
	if _, err := prov.CreateTenant(ctx, postgres.CreateTenantParams{
		ID:       otherTenantID,
		Slug:     "other-" + otherTenantID.String()[:8],
		Name:     "Other Corp",
		IsActive: false,
	}); err != nil {
		t.Fatalf("CreateTenant other: %v", err)
	}

	projID := uuid.New()
	proj, err := prov.CreateProject(ctx, postgres.CreateProjectParams{
		ID:       projID,
		TenantID: tenantID,
		Slug:     "core-mem",
		Name:     "Core Memory",
		IsActive: true,
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if proj.TenantID != tenantID {
		t.Fatalf("unexpected project tenant: %+v", proj)
	}

	// Cross-tenant lookup must return ErrNotFound.
	if _, err := prov.GetProjectByID(ctx, otherTenantID, projID); !errors.Is(err, memory.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-tenant GetProjectByID, got %v", err)
	}

	otherProjID := uuid.New()
	if _, err := prov.CreateProject(ctx, postgres.CreateProjectParams{
		ID:       otherProjID,
		TenantID: otherTenantID,
		Slug:     "other-proj",
		Name:     "Other Project",
		IsActive: true,
	}); err != nil {
		t.Fatalf("CreateProject other: %v", err)
	}

	cnt, err := prov.CountTenantProjects(ctx, tenantID, []uuid.UUID{projID, otherProjID})
	if err != nil {
		t.Fatalf("CountTenantProjects: %v", err)
	}
	if cnt != 1 {
		t.Fatalf("expected CountTenantProjects = 1 (strictly scoped to tenant), got %d", cnt)
	}

	agentID := uuid.New()
	agent, err := prov.CreateAgent(ctx, postgres.CreateAgentParams{
		ID:          agentID,
		TenantID:    tenantID,
		ExternalKey: "agent-alpha-" + agentID.String()[:8],
		Name:        "Alpha Agent",
		RoleType:    "coder",
		IsActive:    true,
	})
	if err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	if _, err := prov.GetAgentByID(ctx, otherTenantID, agent.ID); !errors.Is(err, memory.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-tenant GetAgentByID, got %v", err)
	}

	rawKey := "rmr_agent_" + uuid.NewString()[:8] + "_" + "0123456789abcdef0123456789abcdef"
	prefix := rawKey[:20]
	sum := sha256.Sum256([]byte(rawKey))
	sumHex := hex.EncodeToString(sum[:])

	principalID := uuid.New()
	princ, err := prov.CreatePrincipal(ctx, postgres.CreatePrincipalParams{
		ID:              principalID,
		TenantID:        tenantID,
		AgentID:         &agentID,
		PrincipalType:   identity.PrincipalAgent,
		Name:            "alpha-principal",
		APIKeyPrefix:    prefix,
		APIKeySHA256:    sumHex,
		AllowedProjects: []uuid.UUID{projID},
		AllowedScopes:   []identity.ScopeType{identity.ScopeAgentPrivate, identity.ScopeTaskShared},
		IsActive:        true,
		IsAdmin:         true,
	})
	if err != nil {
		t.Fatalf("CreatePrincipal: %v", err)
	}
	if !princ.IsAdmin || princ.APIKeyPrefix != prefix {
		t.Fatalf("unexpected principal record: %+v", princ)
	}

	candidates, err := prov.LookupPrincipalsByPrefix(ctx, prefix)
	if err != nil {
		t.Fatalf("LookupPrincipalsByPrefix: %v", err)
	}
	if len(candidates) != 1 || !candidates[0].TenantActive || !candidates[0].AgentActive || !candidates[0].Principal.IsAdmin {
		t.Fatalf("unexpected lookup candidates: %+v", candidates)
	}

	// GetPrincipalByID enforces tenant scoping.
	gotPrinc, err := prov.GetPrincipalByID(ctx, tenantID, principalID)
	if err != nil {
		t.Fatalf("GetPrincipalByID same tenant: %v", err)
	}
	if gotPrinc.ID != principalID || gotPrinc.TenantID != tenantID {
		t.Fatalf("unexpected GetPrincipalByID result: %+v", gotPrinc)
	}
	if _, err := prov.GetPrincipalByID(ctx, otherTenantID, principalID); !errors.Is(err, memory.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-tenant GetPrincipalByID, got %v", err)
	}

	// Inactive agent and orphaned PrincipalAgent must both report AgentActive=false.
	inactiveAgentID := uuid.New()
	if _, err := prov.CreateAgent(ctx, postgres.CreateAgentParams{
		ID:          inactiveAgentID,
		TenantID:    tenantID,
		ExternalKey: "inactive-agent-" + inactiveAgentID.String()[:8],
		Name:        "Inactive Agent",
		RoleType:    "coder",
		IsActive:    false,
	}); err != nil {
		t.Fatalf("CreateAgent inactive: %v", err)
	}
	rawInactiveAgKey := "rmr_agent_" + uuid.NewString()[:8] + "_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	inactSum := sha256.Sum256([]byte(rawInactiveAgKey))
	if _, err := prov.CreatePrincipal(ctx, postgres.CreatePrincipalParams{
		ID:            uuid.New(),
		TenantID:      tenantID,
		AgentID:       &inactiveAgentID,
		PrincipalType: identity.PrincipalAgent,
		Name:          "inactive-agent-princ",
		APIKeyPrefix:  rawInactiveAgKey[:20],
		APIKeySHA256:  hex.EncodeToString(inactSum[:]),
		IsActive:      true,
	}); err != nil {
		t.Fatalf("CreatePrincipal inactive agent: %v", err)
	}
	inactAgentCands, err := prov.LookupPrincipalsByPrefix(ctx, rawInactiveAgKey[:20])
	if err != nil {
		t.Fatalf("LookupPrincipalsByPrefix inactive agent: %v", err)
	}
	if len(inactAgentCands) != 1 || inactAgentCands[0].AgentActive {
		t.Fatalf("expected AgentActive=false for inactive agent, got %+v", inactAgentCands)
	}

	rawOrphanKey := "rmr_agent_" + uuid.NewString()[:8] + "_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	orphanSum := sha256.Sum256([]byte(rawOrphanKey))
	if _, err := prov.CreatePrincipal(ctx, postgres.CreatePrincipalParams{
		ID:            uuid.New(),
		TenantID:      tenantID,
		AgentID:       nil,
		PrincipalType: identity.PrincipalAgent,
		Name:          "orphan-agent-princ",
		APIKeyPrefix:  rawOrphanKey[:20],
		APIKeySHA256:  hex.EncodeToString(orphanSum[:]),
		IsActive:      true,
	}); err != nil {
		t.Fatalf("CreatePrincipal orphan agent: %v", err)
	}
	orphanCands, err := prov.LookupPrincipalsByPrefix(ctx, rawOrphanKey[:20])
	if err != nil {
		t.Fatalf("LookupPrincipalsByPrefix orphan agent: %v", err)
	}
	if len(orphanCands) != 1 || orphanCands[0].AgentActive {
		t.Fatalf("expected AgentActive=false for orphaned PrincipalAgent, got %+v", orphanCands)
	}

	// UpsertTaskPermission grant & revoke verified via TaskPermissions().EvaluateTaskAccess.
	if err := prov.UpsertTaskPermission(ctx, postgres.TaskPermissionGrant{
		TenantID: tenantID, ProjectID: projID, TaskID: "pg-task-1", PrincipalID: principalID,
		CanRead: true, CanWrite: true,
	}); err != nil {
		t.Fatalf("UpsertTaskPermission grant: %v", err)
	}
	permRepo := store.TaskPermissions()
	permQuery := identity.TaskPermissionQuery{
		TenantID: tenantID, ProjectID: projID, TaskID: "pg-task-1", PrincipalID: principalID,
	}
	grantedPerm, err := permRepo.EvaluateTaskAccess(ctx, permQuery)
	if err != nil {
		t.Fatalf("EvaluateTaskAccess granted: %v", err)
	}
	if !grantedPerm.CanRead || !grantedPerm.CanWrite {
		t.Fatalf("expected granted read+write permission, got %+v", grantedPerm)
	}
	if err := prov.UpsertTaskPermission(ctx, postgres.TaskPermissionGrant{
		TenantID: tenantID, ProjectID: projID, TaskID: "pg-task-1", PrincipalID: principalID,
		CanRead: false, CanWrite: false,
	}); err != nil {
		t.Fatalf("UpsertTaskPermission revoke: %v", err)
	}
	revokedPerm, err := permRepo.EvaluateTaskAccess(ctx, permQuery)
	if err != nil {
		t.Fatalf("EvaluateTaskAccess revoked: %v", err)
	}
	if revokedPerm.CanRead || revokedPerm.CanWrite {
		t.Fatalf("expected revoked permission (CanRead=false, CanWrite=false), got %+v", revokedPerm)
	}

	// Inactive tenant principal lookup reports TenantActive=false.
	rawOtherKey := "rmr_user_" + uuid.NewString()[:8] + "_fedcba9876543210fedcba9876543210"
	otherSum := sha256.Sum256([]byte(rawOtherKey))
	if _, err := prov.CreatePrincipal(ctx, postgres.CreatePrincipalParams{
		ID:              uuid.New(),
		TenantID:        otherTenantID,
		PrincipalType:   identity.PrincipalUser,
		Name:            "other-user",
		APIKeyPrefix:    rawOtherKey[:20],
		APIKeySHA256:    hex.EncodeToString(otherSum[:]),
		AllowedProjects: []uuid.UUID{otherProjID},
		AllowedScopes:   []identity.ScopeType{identity.ScopeProjectShared},
		IsActive:        true,
		IsAdmin:         false,
	}); err != nil {
		t.Fatalf("CreatePrincipal inactive tenant: %v", err)
	}
	inactiveCandidates, err := prov.LookupPrincipalsByPrefix(ctx, rawOtherKey[:20])
	if err != nil {
		t.Fatalf("LookupPrincipalsByPrefix inactive tenant: %v", err)
	}
	if len(inactiveCandidates) != 1 || inactiveCandidates[0].TenantActive {
		t.Fatalf("expected TenantActive=false for inactive tenant, got %+v", inactiveCandidates)
	}
}

func TestProvisioningStoreExecTxRollback(t *testing.T) {
	pool := openTestPool(t)
	store, err := postgres.NewStore(pool)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	prov := store.Provisioning()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	committedTenant, err := prov.CreateTenant(ctx, postgres.CreateTenantParams{
		ID:       uuid.New(),
		Slug:     "committed-" + uuid.NewString()[:8],
		Name:     "Committed Tenant",
		IsActive: true,
	})
	if err != nil {
		t.Fatalf("CreateTenant committed: %v", err)
	}

	slug := "rollback-tenant-" + uuid.NewString()[:8]
	allowedOp := "tx.allowed." + slug
	deniedOp := "tx.denied." + slug
	sentinel := errors.New("abort bootstrap tx")
	err = prov.ExecTx(ctx, func(txProv *postgres.ProvisioningStore, audit memory.AuditLogger) error {
		tRec, err := txProv.CreateTenant(ctx, postgres.CreateTenantParams{
			ID:       uuid.New(),
			Slug:     slug,
			Name:     "Rollback Tenant",
			IsActive: true,
		})
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		if err := audit.RecordAllowed(ctx, memory.AuditEvent{
			ID:           uuid.New(),
			TenantID:     &tRec.ID,
			Operation:    allowedOp,
			ResourceType: "tenant",
			Outcome:      memory.AuditAllowed,
			CreatedAt:    now,
		}); err != nil {
			return err
		}
		if err := audit.RecordDenied(ctx, memory.AuditEvent{
			ID:           uuid.New(),
			TenantID:     &committedTenant.ID,
			Operation:    deniedOp,
			ResourceType: "tenant",
			Outcome:      memory.AuditDenied,
			CreatedAt:    now,
		}); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error, got %v", err)
	}
	if _, err := prov.GetTenantBySlug(ctx, slug); !errors.Is(err, memory.ErrNotFound) {
		t.Fatalf("expected rolled back tenant to not exist (ErrNotFound), got %v", err)
	}

	var allowedCount, deniedCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE operation = $1`, allowedOp).Scan(&allowedCount); err != nil {
		t.Fatalf("query allowed audit_events: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE operation = $1`, deniedOp).Scan(&deniedCount); err != nil {
		t.Fatalf("query denied audit_events: %v", err)
	}
	if allowedCount != 0 {
		t.Fatalf("expected rolled-back allowed audit count = 0, got %d", allowedCount)
	}
	if deniedCount != 1 {
		t.Fatalf("expected out-of-band denied audit count = 1, got %d", deniedCount)
	}
}
