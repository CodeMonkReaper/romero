package provisioning_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/romero-project/romero/internal/application/provisioning"
	"github.com/romero-project/romero/internal/domain/identity"
	"github.com/romero-project/romero/internal/domain/memory"
)

type fakeStore struct {
	mu              sync.Mutex
	tenants         map[uuid.UUID]provisioning.Tenant
	projects        map[uuid.UUID]provisioning.Project
	agents          map[uuid.UUID]provisioning.Agent
	principals      map[uuid.UUID]provisioning.Principal
	hashesByID      map[uuid.UUID]string
	taskPerms       map[string]provisioning.TaskPermissionGrant
	allowedAudits   []memory.AuditEvent
	deniedAudits    []memory.AuditEvent
	failAllowedAud  error
	failDeniedAud   error
	failLookup      error
	inactiveAgents  map[uuid.UUID]bool
	inactiveTenants map[uuid.UUID]bool
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		tenants:         make(map[uuid.UUID]provisioning.Tenant),
		projects:        make(map[uuid.UUID]provisioning.Project),
		agents:          make(map[uuid.UUID]provisioning.Agent),
		principals:      make(map[uuid.UUID]provisioning.Principal),
		hashesByID:      make(map[uuid.UUID]string),
		taskPerms:       make(map[string]provisioning.TaskPermissionGrant),
		inactiveAgents:  make(map[uuid.UUID]bool),
		inactiveTenants: make(map[uuid.UUID]bool),
	}
}

func (f *fakeStore) clone() *fakeStore {
	c := newFakeStore()
	for k, v := range f.tenants {
		c.tenants[k] = v
	}
	for k, v := range f.projects {
		c.projects[k] = v
	}
	for k, v := range f.agents {
		c.agents[k] = v
	}
	for k, v := range f.principals {
		c.principals[k] = v
	}
	for k, v := range f.hashesByID {
		c.hashesByID[k] = v
	}
	for k, v := range f.taskPerms {
		c.taskPerms[k] = v
	}
	c.allowedAudits = append([]memory.AuditEvent(nil), f.allowedAudits...)
	c.deniedAudits = append([]memory.AuditEvent(nil), f.deniedAudits...)
	c.failAllowedAud = f.failAllowedAud
	c.failDeniedAud = f.failDeniedAud
	c.failLookup = f.failLookup
	for k, v := range f.inactiveAgents {
		c.inactiveAgents[k] = v
	}
	for k, v := range f.inactiveTenants {
		c.inactiveTenants[k] = v
	}
	return c
}

func (f *fakeStore) ExecTx(ctx context.Context, fn func(provisioning.Repository, memory.AuditLogger) error) error {
	f.mu.Lock()
	snap := f.clone()
	f.mu.Unlock()

	txAudit := &fakeTxAudit{tx: snap, parent: f}
	if err := fn(snap, txAudit); err != nil {
		// Copy denied audits recorded out-of-rollback even when tx rolls back.
		f.mu.Lock()
		f.deniedAudits = append([]memory.AuditEvent(nil), snap.deniedAudits...)
		f.mu.Unlock()
		return err
	}

	f.mu.Lock()
	f.tenants = snap.tenants
	f.projects = snap.projects
	f.agents = snap.agents
	f.principals = snap.principals
	f.hashesByID = snap.hashesByID
	f.taskPerms = snap.taskPerms
	f.allowedAudits = snap.allowedAudits
	f.deniedAudits = snap.deniedAudits
	f.mu.Unlock()
	return nil
}

type fakeTxAudit struct {
	tx     *fakeStore
	parent *fakeStore
}

func (a *fakeTxAudit) RecordAllowed(ctx context.Context, ev memory.AuditEvent) error {
	return a.tx.RecordAllowed(ctx, ev)
}

func (a *fakeTxAudit) RecordDenied(ctx context.Context, ev memory.AuditEvent) error {
	return a.parent.RecordDenied(ctx, ev)
}

func (f *fakeStore) RecordAllowed(_ context.Context, ev memory.AuditEvent) error {
	if f.failAllowedAud != nil {
		return f.failAllowedAud
	}
	if err := ev.Validate(); err != nil {
		return err
	}
	f.allowedAudits = append(f.allowedAudits, ev)
	return nil
}

func (f *fakeStore) RecordDenied(_ context.Context, ev memory.AuditEvent) error {
	if f.failDeniedAud != nil {
		return f.failDeniedAud
	}
	if err := ev.Validate(); err != nil {
		return err
	}
	f.deniedAudits = append(f.deniedAudits, ev)
	return nil
}

func (f *fakeStore) CreateTenant(_ context.Context, p provisioning.CreateTenantParams) (provisioning.Tenant, error) {
	for _, existing := range f.tenants {
		if existing.Slug == p.Slug {
			return provisioning.Tenant{}, provisioning.ErrAlreadyExists
		}
	}
	id := p.ID
	if id == uuid.Nil {
		id = uuid.New()
	}
	t := provisioning.Tenant{ID: id, Slug: p.Slug, Name: p.Name, IsActive: p.IsActive}
	f.tenants[id] = t
	return t, nil
}

func (f *fakeStore) GetTenantByID(_ context.Context, id uuid.UUID) (*provisioning.Tenant, error) {
	t, ok := f.tenants[id]
	if !ok {
		return nil, provisioning.ErrNotFound
	}
	cp := t
	return &cp, nil
}

func (f *fakeStore) CreateProject(_ context.Context, p provisioning.CreateProjectParams) (provisioning.Project, error) {
	for _, existing := range f.projects {
		if existing.TenantID == p.TenantID && existing.Slug == p.Slug {
			return provisioning.Project{}, provisioning.ErrAlreadyExists
		}
	}
	id := p.ID
	if id == uuid.Nil {
		id = uuid.New()
	}
	proj := provisioning.Project{ID: id, TenantID: p.TenantID, Slug: p.Slug, Name: p.Name, IsActive: p.IsActive}
	f.projects[id] = proj
	return proj, nil
}

func (f *fakeStore) GetProjectByID(_ context.Context, tenantID, id uuid.UUID) (*provisioning.Project, error) {
	proj, ok := f.projects[id]
	if !ok || proj.TenantID != tenantID {
		return nil, provisioning.ErrNotFound
	}
	cp := proj
	return &cp, nil
}

func (f *fakeStore) CountTenantProjects(_ context.Context, tenantID uuid.UUID, ids []uuid.UUID) (int64, error) {
	var cnt int64
	for _, id := range ids {
		if p, ok := f.projects[id]; ok && p.TenantID == tenantID {
			cnt++
		}
	}
	return cnt, nil
}

func (f *fakeStore) CreateAgent(_ context.Context, p provisioning.CreateAgentParams) (provisioning.Agent, error) {
	for _, existing := range f.agents {
		if existing.TenantID == p.TenantID && existing.ExternalKey == p.ExternalKey {
			return provisioning.Agent{}, provisioning.ErrAlreadyExists
		}
	}
	id := p.ID
	if id == uuid.Nil {
		id = uuid.New()
	}
	ag := provisioning.Agent{ID: id, TenantID: p.TenantID, ExternalKey: p.ExternalKey, Name: p.Name, RoleType: p.RoleType, IsActive: p.IsActive}
	f.agents[id] = ag
	return ag, nil
}

func (f *fakeStore) GetAgentByID(_ context.Context, tenantID, id uuid.UUID) (*provisioning.Agent, error) {
	ag, ok := f.agents[id]
	if !ok || ag.TenantID != tenantID {
		return nil, provisioning.ErrNotFound
	}
	cp := ag
	return &cp, nil
}

func (f *fakeStore) CreatePrincipal(_ context.Context, p provisioning.CreatePrincipalParams) (provisioning.Principal, error) {
	for _, h := range f.hashesByID {
		if h == p.APIKeySHA256 {
			return provisioning.Principal{}, provisioning.ErrAlreadyExists
		}
	}
	id := p.ID
	if id == uuid.Nil {
		id = uuid.New()
	}
	pr := provisioning.Principal{
		ID:              id,
		TenantID:        p.TenantID,
		AgentID:         p.AgentID,
		PrincipalType:   p.PrincipalType,
		Name:            p.Name,
		APIKeyPrefix:    p.APIKeyPrefix,
		AllowedProjects: append([]uuid.UUID(nil), p.AllowedProjects...),
		AllowedScopes:   append([]identity.ScopeType(nil), p.AllowedScopes...),
		IsActive:        p.IsActive,
		ExpiresAt:       p.ExpiresAt,
		IsAdmin:         p.IsAdmin,
	}
	f.principals[id] = pr
	f.hashesByID[id] = p.APIKeySHA256
	return pr, nil
}

func (f *fakeStore) GetPrincipalByID(_ context.Context, tenantID, id uuid.UUID) (*provisioning.Principal, error) {
	pr, ok := f.principals[id]
	if !ok || pr.TenantID != tenantID {
		return nil, provisioning.ErrNotFound
	}
	cp := pr
	return &cp, nil
}

func (f *fakeStore) LookupPrincipalsByPrefix(_ context.Context, prefix string) ([]provisioning.PrincipalAuthCandidate, error) {
	if f.failLookup != nil {
		return nil, f.failLookup
	}
	var out []provisioning.PrincipalAuthCandidate
	for id, pr := range f.principals {
		if pr.APIKeyPrefix != prefix {
			continue
		}
		tenantActive := f.tenants[pr.TenantID].IsActive && !f.inactiveTenants[pr.TenantID]
		agentActive := true
		if pr.AgentID != nil {
			ag, ok := f.agents[*pr.AgentID]
			agentActive = ok && ag.IsActive && !f.inactiveAgents[*pr.AgentID]
		}
		out = append(out, provisioning.PrincipalAuthCandidate{
			Principal:    pr,
			APIKeySHA256: f.hashesByID[id],
			TenantActive: tenantActive,
			AgentActive:  agentActive,
		})
	}
	return out, nil
}

func (f *fakeStore) UpsertTaskPermission(_ context.Context, g provisioning.TaskPermissionGrant) error {
	key := g.TenantID.String() + "|" + g.ProjectID.String() + "|" + g.TaskID + "|" + g.PrincipalID.String()
	f.taskPerms[key] = g
	return nil
}

func TestBootstrapCreatePrincipalAndAuthenticate(t *testing.T) {
	store := newFakeStore()
	fixedNow := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	svc := provisioning.NewService(store, store, store, provisioning.WithClock(func() time.Time { return fixedNow }))
	auth := provisioning.NewAuthenticator(store, store, provisioning.WithAuthClock(func() time.Time { return fixedNow }))
	ctx := context.Background()

	out, err := svc.Bootstrap(ctx, provisioning.BootstrapSpec{
		TenantSlug:       "gandalf",
		TenantName:       "Gandalf Org",
		ProjectSlug:      "romero",
		ProjectName:      "Romero Core",
		AgentExternalKey: "orchestrator-1",
		AgentName:        "Orchestrator One",
		PrincipalName:    "root-orch",
		PrincipalType:    identity.PrincipalOrchestrator,
		AllowedScopes:    []identity.ScopeType{identity.ScopeProjectShared, identity.ScopeTaskShared, identity.ScopeOrgShared},
		IsAdmin:          true,
		InitialTaskGrants: []provisioning.BootstrapTaskGrant{
			{TaskID: "task-001", CanRead: true, CanWrite: true},
		},
	})
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if !strings.HasPrefix(out.RawAPIKey, "rmr_orchestrator_") || len(out.Principal.APIKeyPrefix) != 20 {
		t.Fatalf("unexpected API key/prefix: key=%q prefix=%q", out.RawAPIKey, out.Principal.APIKeyPrefix)
	}
	if out.Principal.APIKeyPrefix != out.RawAPIKey[:20] {
		t.Fatalf("prefix mismatch: %q vs %q", out.Principal.APIKeyPrefix, out.RawAPIKey[:20])
	}
	sum := sha256.Sum256([]byte(out.RawAPIKey))
	if store.hashesByID[out.Principal.ID] != hex.EncodeToString(sum[:]) {
		t.Fatal("stored SHA-256 digest does not match raw API key")
	}
	if len(store.allowedAudits) == 0 || store.allowedAudits[0].Operation != "bootstrap" {
		t.Fatalf("expected bootstrap audit event, got %+v", store.allowedAudits)
	}
	if len(store.taskPerms) != 1 {
		t.Fatalf("expected 1 initial task grant, got %d", len(store.taskPerms))
	}

	// Authenticate with generated raw key and verify AuthContext fields including IsAdmin.
	actx, err := auth.Authenticate(ctx, out.RawAPIKey)
	if err != nil {
		t.Fatalf("Authenticate valid key: %v", err)
	}
	if actx.TenantID != out.Tenant.ID || actx.PrincipalID != out.Principal.ID || !actx.IsAdmin {
		t.Fatalf("unexpected AuthContext: %+v", actx)
	}
	if len(actx.AllowedProjects) != 1 || actx.AllowedProjects[0] != out.Project.ID {
		t.Fatalf("unexpected AllowedProjects: %+v", actx.AllowedProjects)
	}

	// Create secondary agent principal attached to project.
	agentKey, agentPrinc, err := svc.CreatePrincipal(ctx, provisioning.CreatePrincipalInput{
		TenantID:        out.Tenant.ID,
		AgentID:         &out.Agent.ID,
		PrincipalType:   identity.PrincipalAgent,
		Name:            "worker-agent",
		AllowedProjects: []uuid.UUID{out.Project.ID},
		AllowedScopes:   []identity.ScopeType{identity.ScopeAgentPrivate, identity.ScopeTaskShared},
	})
	if err != nil {
		t.Fatalf("CreatePrincipal: %v", err)
	}
	if agentPrinc.ID == uuid.Nil || agentPrinc.PrincipalType != identity.PrincipalAgent {
		t.Fatalf("unexpected agentPrinc: %+v", agentPrinc)
	}
	if !strings.HasPrefix(agentKey, "rmr_agent_") {
		t.Fatalf("unexpected agent key format: %q", agentKey)
	}
	agentAuth, err := auth.Authenticate(ctx, agentKey)
	if err != nil {
		t.Fatalf("Authenticate agent key: %v", err)
	}
	if agentAuth.IsAdmin || agentAuth.AgentID == nil || *agentAuth.AgentID != out.Agent.ID {
		t.Fatalf("unexpected agent AuthContext: %+v", agentAuth)
	}

	// Agent principals cannot be marked IsAdmin.
	if _, _, err := svc.CreatePrincipal(ctx, provisioning.CreatePrincipalInput{
		TenantID:      out.Tenant.ID,
		AgentID:       &out.Agent.ID,
		PrincipalType: identity.PrincipalAgent,
		Name:          "bad-admin-agent",
		AllowedScopes: []identity.ScopeType{identity.ScopeAgentPrivate},
		IsAdmin:       true,
	}); !errors.Is(err, provisioning.ErrForbidden) {
		t.Fatalf("expected ErrForbidden creating admin agent, got %v", err)
	}
}

func TestAuthenticatorRejectionsAndFailures(t *testing.T) {
	store := newFakeStore()
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	svc := provisioning.NewService(store, store, store, provisioning.WithClock(func() time.Time { return now }))
	auth := provisioning.NewAuthenticator(store, store, provisioning.WithAuthClock(func() time.Time { return now }))
	ctx := context.Background()

	out, err := svc.Bootstrap(ctx, provisioning.BootstrapSpec{
		TenantSlug:    "auth-sec",
		TenantName:    "Auth Sec",
		ProjectSlug:   "p1",
		ProjectName:   "P1",
		PrincipalName: "u1",
		PrincipalType: identity.PrincipalUser,
		AllowedScopes: []identity.ScopeType{identity.ScopeProjectShared},
		IsAdmin:       true,
	})
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}

	// 1. Malformed key -> ErrUnauthenticated + denied audit.
	if _, err := auth.Authenticate(ctx, "not-a-valid-key"); !errors.Is(err, provisioning.ErrUnauthenticated) {
		t.Fatalf("expected ErrUnauthenticated for malformed key, got %v", err)
	}
	// 2. Wrong secret keeping identical 20-char prefix -> ErrUnauthenticated + denied audit.
	tampered := out.RawAPIKey[:len(out.RawAPIKey)-4] + "ffff"
	if tampered == out.RawAPIKey {
		tampered = out.RawAPIKey[:len(out.RawAPIKey)-4] + "0000"
	}
	if _, err := auth.Authenticate(ctx, tampered); !errors.Is(err, provisioning.ErrUnauthenticated) {
		t.Fatalf("expected ErrUnauthenticated for tampered secret, got %v", err)
	}

	// 3. Expired key -> ErrUnauthenticated + denied audit tagged to tenant.
	exp := now.Add(time.Minute)
	expKey, expPrinc, err := svc.CreatePrincipal(ctx, provisioning.CreatePrincipalInput{
		TenantID:        out.Tenant.ID,
		PrincipalType:   identity.PrincipalUser,
		Name:            "short-lived",
		AllowedProjects: []uuid.UUID{out.Project.ID},
		AllowedScopes:   []identity.ScopeType{identity.ScopeProjectShared},
		ExpiresAt:       &exp,
	})
	if err != nil {
		t.Fatalf("CreatePrincipal expired test: %v", err)
	}
	now = now.Add(time.Minute) // equal to ExpiresAt => expired!
	if _, err := auth.Authenticate(ctx, expKey); !errors.Is(err, provisioning.ErrUnauthenticated) {
		t.Fatalf("expected ErrUnauthenticated for expired key, got %v", err)
	}
	lastDenied := store.deniedAudits[len(store.deniedAudits)-1]
	if lastDenied.PrincipalID == nil || *lastDenied.PrincipalID != expPrinc.ID {
		t.Fatalf("expected denied audit attributed to principal %s, got %+v", expPrinc.ID, lastDenied)
	}

	// 4. Denied audit failure must never authenticate (fail closed with ErrAuditUnavailable).
	store.failDeniedAud = errors.New("audit disk full")
	if _, err := auth.Authenticate(ctx, expKey); !errors.Is(err, provisioning.ErrAuditUnavailable) {
		t.Fatalf("expected ErrAuditUnavailable when denied audit fails, got %v", err)
	}
	store.failDeniedAud = nil

	// 5. Prefix lookup failure surfaces error cleanly.
	store.failLookup = errors.New("db unavailable")
	if _, err := auth.Authenticate(ctx, out.RawAPIKey); err == nil {
		t.Fatal("expected lookup error")
	}
}

func TestManageTaskPermissionsAuthorityAndAuditAtomicity(t *testing.T) {
	store := newFakeStore()
	svc := provisioning.NewService(store, store, store)
	ctx := context.Background()

	b1, err := svc.Bootstrap(ctx, provisioning.BootstrapSpec{
		TenantSlug:    "t1",
		TenantName:    "T1",
		ProjectSlug:   "p1",
		ProjectName:   "P1",
		PrincipalName: "admin1",
		PrincipalType: identity.PrincipalUser,
		AllowedScopes: []identity.ScopeType{identity.ScopeTaskShared},
		IsAdmin:       true,
	})
	if err != nil {
		t.Fatalf("Bootstrap t1: %v", err)
	}
	// Create a second project and a non-admin orchestrator authorized ONLY on Project1.
	proj2 := provisioning.Project{ID: uuid.New(), TenantID: b1.Tenant.ID, Slug: "p2", Name: "P2", IsActive: true}
	store.projects[proj2.ID] = proj2

	_, orch, err := svc.CreatePrincipal(ctx, provisioning.CreatePrincipalInput{
		TenantID:        b1.Tenant.ID,
		PrincipalType:   identity.PrincipalOrchestrator,
		Name:            "orch-p1",
		AllowedProjects: []uuid.UUID{b1.Project.ID},
		AllowedScopes:   []identity.ScopeType{identity.ScopeTaskShared},
	})
	if err != nil {
		t.Fatalf("CreatePrincipal orch: %v", err)
	}
	_, worker, err := svc.CreatePrincipal(ctx, provisioning.CreatePrincipalInput{
		TenantID:        b1.Tenant.ID,
		PrincipalType:   identity.PrincipalUser,
		Name:            "regular-user",
		AllowedProjects: []uuid.UUID{b1.Project.ID},
		AllowedScopes:   []identity.ScopeType{identity.ScopeTaskShared},
	})
	if err != nil {
		t.Fatalf("CreatePrincipal worker: %v", err)
	}

	b2, err := svc.Bootstrap(ctx, provisioning.BootstrapSpec{
		TenantSlug:    "t2",
		TenantName:    "T2",
		ProjectSlug:   "p-other",
		ProjectName:   "P Other",
		PrincipalName: "admin2",
		PrincipalType: identity.PrincipalUser,
		AllowedScopes: []identity.ScopeType{identity.ScopeTaskShared},
		IsAdmin:       true,
	})
	if err != nil {
		t.Fatalf("Bootstrap t2: %v", err)
	}

	orchActor := identity.AuthContext{
		TenantID:        b1.Tenant.ID,
		PrincipalID:     orch.ID,
		PrincipalType:   identity.PrincipalOrchestrator,
		AllowedProjects: orch.AllowedProjects,
		AllowedScopes:   orch.AllowedScopes,
		IsAdmin:         false,
	}

	// Orchestrator grants & revokes on its authorized project inside its tenant (AC-T5).
	if err := svc.ManageTaskPermission(ctx, orchActor, provisioning.ManageTaskPermissionCommand{
		TenantID:    b1.Tenant.ID,
		ProjectID:   b1.Project.ID,
		TaskID:      "task-42",
		PrincipalID: worker.ID,
		CanRead:     true,
		CanWrite:    true,
	}); err != nil {
		t.Fatalf("orchestrator grant on own project: %v", err)
	}

	// Orchestrator CANNOT grant on unmanaged Project 2 (AC-T5).
	if err := svc.ManageTaskPermission(ctx, orchActor, provisioning.ManageTaskPermissionCommand{
		TenantID:    b1.Tenant.ID,
		ProjectID:   proj2.ID,
		TaskID:      "task-42",
		PrincipalID: worker.ID,
		CanRead:     true,
	}); !errors.Is(err, identity.ErrForbidden) {
		t.Fatalf("expected ErrForbidden granting on unmanaged project, got %v", err)
	}

	// Cross-tenant grant attempt by Tenant 2 admin against Tenant 1 project is denied and audited.
	t2AdminActor := identity.AuthContext{
		TenantID:        b2.Tenant.ID,
		PrincipalID:     b2.Principal.ID,
		PrincipalType:   identity.PrincipalUser,
		AllowedProjects: []uuid.UUID{b2.Project.ID},
		AllowedScopes:   []identity.ScopeType{identity.ScopeTaskShared},
		IsAdmin:         true,
	}
	deniedBefore := len(store.deniedAudits)
	if err := svc.ManageTaskPermission(ctx, t2AdminActor, provisioning.ManageTaskPermissionCommand{
		TenantID:    b1.Tenant.ID,
		ProjectID:   b1.Project.ID,
		TaskID:      "task-42",
		PrincipalID: worker.ID,
		CanRead:     true,
	}); !errors.Is(err, identity.ErrForbidden) {
		t.Fatalf("expected ErrForbidden on cross-tenant grant, got %v", err)
	}
	if len(store.deniedAudits) != deniedBefore+1 {
		t.Fatalf("expected denied audit recorded for cross-tenant grant")
	}

	// Audit failure inside grant transaction rolls back permission change (AC-T4).
	store.failAllowedAud = errors.New("audit write failure")
	if err := svc.ManageTaskPermission(ctx, orchActor, provisioning.ManageTaskPermissionCommand{
		TenantID:    b1.Tenant.ID,
		ProjectID:   b1.Project.ID,
		TaskID:      "task-rollback",
		PrincipalID: worker.ID,
		CanRead:     true,
		CanWrite:    false,
	}); err == nil {
		t.Fatal("expected error when allowed audit fails")
	}
	key := b1.Tenant.ID.String() + "|" + b1.Project.ID.String() + "|task-rollback|" + worker.ID.String()
	if _, exists := store.taskPerms[key]; exists {
		t.Fatal("permission mutation must roll back if audit fails")
	}

	// Revocation works cleanly and records task_permission.revoke audit event.
	store.failAllowedAud = nil
	if err := svc.ManageTaskPermission(ctx, orchActor, provisioning.ManageTaskPermissionCommand{
		TenantID:    b1.Tenant.ID,
		ProjectID:   b1.Project.ID,
		TaskID:      "task-42",
		PrincipalID: worker.ID,
		CanRead:     false,
		CanWrite:    false,
	}); err != nil {
		t.Fatalf("ManageTaskPermission revoke: %v", err)
	}
	if got := store.allowedAudits[len(store.allowedAudits)-1]; got.Operation != "task_permission.revoke" {
		t.Fatalf("expected task_permission.revoke audit operation, got %q", got.Operation)
	}

	// Agent actor (even if IsAdmin were spoofed) is always denied from managing task permissions.
	agentActor := identity.AuthContext{
		TenantID:        b1.Tenant.ID,
		PrincipalID:     uuid.New(),
		PrincipalType:   identity.PrincipalAgent,
		AllowedProjects: []uuid.UUID{b1.Project.ID},
		IsAdmin:         true,
	}
	if err := svc.ManageTaskPermission(ctx, agentActor, provisioning.ManageTaskPermissionCommand{
		TenantID:    b1.Tenant.ID,
		ProjectID:   b1.Project.ID,
		TaskID:      "task-42",
		PrincipalID: worker.ID,
		CanRead:     true,
	}); !errors.Is(err, identity.ErrForbidden) {
		t.Fatalf("expected ErrForbidden for agent managing task permission, got %v", err)
	}

	// If RecordDenied fails during denied task permission check, surface ErrAuditUnavailable.
	store.failDeniedAud = errors.New("audit down")
	if err := svc.ManageTaskPermission(ctx, agentActor, provisioning.ManageTaskPermissionCommand{
		TenantID:    b1.Tenant.ID,
		ProjectID:   b1.Project.ID,
		TaskID:      "task-42",
		PrincipalID: worker.ID,
		CanRead:     true,
	}); !errors.Is(err, provisioning.ErrAuditUnavailable) {
		t.Fatalf("expected ErrAuditUnavailable when denied audit fails, got %v", err)
	}
	store.failDeniedAud = nil
}

func TestProvisioningValidationAndBoundaryTriangulation(t *testing.T) {
	ctx := context.Background()
	fixedNow := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)

	// Nil receiver / uninitialized guards.
	var nilSvc *provisioning.Service
	if _, err := nilSvc.Bootstrap(ctx, provisioning.BootstrapSpec{}); !errors.Is(err, provisioning.ErrInvalidInput) {
		t.Fatalf("nilSvc.Bootstrap got %v", err)
	}
	if _, _, err := nilSvc.CreatePrincipal(ctx, provisioning.CreatePrincipalInput{}); !errors.Is(err, provisioning.ErrInvalidInput) {
		t.Fatalf("nilSvc.CreatePrincipal got %v", err)
	}
	if err := nilSvc.ManageTaskPermission(ctx, identity.AuthContext{}, provisioning.ManageTaskPermissionCommand{}); !errors.Is(err, provisioning.ErrInvalidInput) {
		t.Fatalf("nilSvc.ManageTaskPermission got %v", err)
	}
	var nilAuth *provisioning.Authenticator
	if _, err := nilAuth.Authenticate(ctx, "x"); !errors.Is(err, provisioning.ErrInvalidInput) {
		t.Fatalf("nilAuth.Authenticate got %v", err)
	}

	// GenerateAPIKey + ParseAPIKey validation cases.
	if _, _, _, err := provisioning.GenerateAPIKey(identity.PrincipalType("invalid"), uuid.New()); !errors.Is(err, provisioning.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for invalid principal type, got %v", err)
	}
	if _, _, _, err := provisioning.GenerateAPIKey(identity.PrincipalUser, uuid.Nil); !errors.Is(err, provisioning.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for nil principal ID, got %v", err)
	}
	for _, bad := range []string{
		"",
		" rmr_user_01234567_0123456789abcdef0123456789abcdef",
		"rmr_unknown_01234567_0123456789abcdef0123456789abcdef",
		"rmr_user_ZZZZZZZZ_0123456789abcdef0123456789abcdef",
		"rmr_user_01234567_short",
	} {
		if _, _, ok := provisioning.ParseAPIKey(bad); ok {
			t.Fatalf("expected ParseAPIKey(%q) to return false", bad)
		}
	}

	store := newFakeStore()
	svc := provisioning.NewService(store, store, store, provisioning.WithClock(func() time.Time { return fixedNow }))
	auth := provisioning.NewAuthenticator(store, store, provisioning.WithAuthClock(func() time.Time { return fixedNow }))

	// Bootstrap validation error paths.
	past := fixedNow.Add(-time.Minute)
	badSpecs := []provisioning.BootstrapSpec{
		{TenantSlug: "", TenantName: "n", ProjectSlug: "p", ProjectName: "pn", PrincipalName: "u", PrincipalType: identity.PrincipalUser},
		{TenantSlug: "t", TenantName: "n", ProjectSlug: "", ProjectName: "pn", PrincipalName: "u", PrincipalType: identity.PrincipalUser},
		{TenantSlug: "t", TenantName: "n", ProjectSlug: "p", ProjectName: "pn", PrincipalName: "", PrincipalType: identity.PrincipalUser},
		{TenantSlug: "t", TenantName: "n", ProjectSlug: "p", ProjectName: "pn", PrincipalName: "u", PrincipalType: identity.PrincipalAgent, IsAdmin: true},
		{TenantSlug: "t", TenantName: "n", ProjectSlug: "p", ProjectName: "pn", PrincipalName: "u", PrincipalType: identity.PrincipalAgent},
		{TenantSlug: "t", TenantName: "n", ProjectSlug: "p", ProjectName: "pn", PrincipalName: "u", PrincipalType: identity.PrincipalUser, ExpiresAt: &past},
		{TenantSlug: "t", TenantName: "n", ProjectSlug: "p", ProjectName: "pn", PrincipalName: "u", PrincipalType: identity.PrincipalUser, InitialTaskGrants: []provisioning.BootstrapTaskGrant{{TaskID: ""}}},
		{TenantSlug: "t", TenantName: "n", ProjectSlug: "p", ProjectName: "pn", PrincipalName: "u", PrincipalType: identity.PrincipalUser, AllowedScopes: []identity.ScopeType{"bad_scope"}},
	}
	for i, sp := range badSpecs {
		if _, err := svc.Bootstrap(ctx, sp); err == nil {
			t.Fatalf("badSpecs[%d] should have failed validation", i)
		}
	}

	b1, err := svc.Bootstrap(ctx, provisioning.BootstrapSpec{
		TenantSlug:       "b1",
		TenantName:       "B1",
		ProjectSlug:      "p1",
		ProjectName:      "P1",
		AgentExternalKey: "ag-1",
		PrincipalName:    "root",
		PrincipalType:    identity.PrincipalUser,
		AllowedScopes:    []identity.ScopeType{identity.ScopeProjectShared, identity.ScopeProjectShared},
		IsAdmin:          true,
	})
	if err != nil {
		t.Fatalf("Bootstrap b1: %v", err)
	}
	b2, err := svc.Bootstrap(ctx, provisioning.BootstrapSpec{
		TenantSlug:    "b2",
		TenantName:    "B2",
		ProjectSlug:   "p2",
		ProjectName:   "P2",
		PrincipalName: "root2",
		PrincipalType: identity.PrincipalUser,
		IsAdmin:       true,
	})
	if err != nil {
		t.Fatalf("Bootstrap b2: %v", err)
	}

	// Cross-tenant project and cross-tenant agent checks in CreatePrincipal.
	if _, _, err := svc.CreatePrincipal(ctx, provisioning.CreatePrincipalInput{
		TenantID:        b1.Tenant.ID,
		PrincipalType:   identity.PrincipalUser,
		Name:            "xproject",
		AllowedProjects: []uuid.UUID{b2.Project.ID},
		AllowedScopes:   []identity.ScopeType{identity.ScopeProjectShared},
	}); !errors.Is(err, provisioning.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for cross-tenant project, got %v", err)
	}
	if _, _, err := svc.CreatePrincipal(ctx, provisioning.CreatePrincipalInput{
		TenantID:      b2.Tenant.ID,
		AgentID:       &b1.Agent.ID,
		PrincipalType: identity.PrincipalAgent,
		Name:          "xagent",
		AllowedScopes: []identity.ScopeType{identity.ScopeAgentPrivate},
	}); !errors.Is(err, provisioning.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-tenant agent, got %v", err)
	}
	if _, _, err := svc.CreatePrincipal(ctx, provisioning.CreatePrincipalInput{
		TenantID:      b1.Tenant.ID,
		PrincipalType: identity.PrincipalAgent,
		Name:          "missing-agent-id",
	}); !errors.Is(err, provisioning.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for agent without AgentID, got %v", err)
	}
	nilUUID := uuid.Nil
	if _, _, err := svc.CreatePrincipal(ctx, provisioning.CreatePrincipalInput{
		TenantID:      b1.Tenant.ID,
		AgentID:       &nilUUID,
		PrincipalType: identity.PrincipalUser,
		Name:          "nil-agent-ptr",
	}); !errors.Is(err, provisioning.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for nil UUID AgentID, got %v", err)
	}
	if _, _, err := svc.CreatePrincipal(ctx, provisioning.CreatePrincipalInput{
		TenantID:        b1.Tenant.ID,
		PrincipalType:   identity.PrincipalUser,
		Name:            "nil-proj-id",
		AllowedProjects: []uuid.UUID{uuid.Nil},
	}); !errors.Is(err, provisioning.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for nil UUID in AllowedProjects, got %v", err)
	}

	// Inactive agent rejects authentication.
	agKey, _, err := svc.CreatePrincipal(ctx, provisioning.CreatePrincipalInput{
		TenantID:        b1.Tenant.ID,
		AgentID:         &b1.Agent.ID,
		PrincipalType:   identity.PrincipalAgent,
		Name:            "ag-p",
		AllowedProjects: []uuid.UUID{b1.Project.ID},
		AllowedScopes:   []identity.ScopeType{identity.ScopeAgentPrivate},
	})
	if err != nil {
		t.Fatalf("CreatePrincipal agent: %v", err)
	}
	store.inactiveAgents[b1.Agent.ID] = true
	if _, err := auth.Authenticate(ctx, agKey); !errors.Is(err, provisioning.ErrUnauthenticated) {
		t.Fatalf("expected ErrUnauthenticated for inactive agent, got %v", err)
	}

	// Inactive tenant rejects CreatePrincipal.
	store.inactiveTenants[b1.Tenant.ID] = true
	tRec := store.tenants[b1.Tenant.ID]
	tRec.IsActive = false
	store.tenants[b1.Tenant.ID] = tRec
	if _, _, err := svc.CreatePrincipal(ctx, provisioning.CreatePrincipalInput{
		TenantID:      b1.Tenant.ID,
		PrincipalType: identity.PrincipalUser,
		Name:          "u-inactive-t",
	}); !errors.Is(err, provisioning.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for inactive tenant, got %v", err)
	}
}
