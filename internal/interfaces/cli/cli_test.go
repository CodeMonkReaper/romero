package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/romero-project/romero/internal/application/provisioning"
	"github.com/romero-project/romero/internal/domain/identity"
	"github.com/romero-project/romero/internal/infrastructure/persistence/postgres"
	"github.com/romero-project/romero/internal/interfaces/cli"
)

type fakeService struct {
	bootstrapCalls []provisioning.BootstrapSpec
	bootstrapOut   *provisioning.BootstrapOutput
	bootstrapErr   error

	createCalls []provisioning.CreatePrincipalInput
	createKey   string
	createView  provisioning.PrincipalView
	createErr   error

	taskCalls []struct {
		actor identity.AuthContext
		cmd   provisioning.ManageTaskPermissionCommand
	}
	taskErr error
}

func (f *fakeService) Bootstrap(_ context.Context, spec provisioning.BootstrapSpec) (*provisioning.BootstrapOutput, error) {
	f.bootstrapCalls = append(f.bootstrapCalls, spec)
	if f.bootstrapErr != nil {
		return nil, f.bootstrapErr
	}
	return f.bootstrapOut, nil
}

func (f *fakeService) CreatePrincipal(_ context.Context, req provisioning.CreatePrincipalInput) (string, provisioning.PrincipalView, error) {
	f.createCalls = append(f.createCalls, req)
	if f.createErr != nil {
		return "", provisioning.PrincipalView{}, f.createErr
	}
	return f.createKey, f.createView, nil
}

func (f *fakeService) ManageTaskPermission(_ context.Context, actor identity.AuthContext, cmd provisioning.ManageTaskPermissionCommand) error {
	f.taskCalls = append(f.taskCalls, struct {
		actor identity.AuthContext
		cmd   provisioning.ManageTaskPermissionCommand
	}{actor: actor, cmd: cmd})
	return f.taskErr
}

type fakeAuth struct {
	keys    map[string]identity.AuthContext
	failErr error
}

func (f *fakeAuth) Authenticate(_ context.Context, rawKey string) (identity.AuthContext, error) {
	if f.failErr != nil {
		return identity.AuthContext{}, f.failErr
	}
	ctx, ok := f.keys[rawKey]
	if !ok {
		return identity.AuthContext{}, provisioning.ErrUnauthenticated
	}
	return ctx, nil
}

type fakeMigrator struct {
	directions []string
	dsns       []string
	version    int64
	err        error
}

func (f *fakeMigrator) Run(_ context.Context, dsn, direction string) (int64, error) {
	f.dsns = append(f.dsns, dsn)
	f.directions = append(f.directions, direction)
	return f.version, f.err
}

func testDeps(svc *fakeService, auth *fakeAuth, mig *fakeMigrator) cli.Dependencies {
	return cli.Dependencies{
		Migrator: mig,
		OpenProvisioner: func(_ context.Context, _ string) (cli.ProvisioningClient, func(), error) {
			return cli.ProvisioningClient{
				Service: svc,
				Auth:    auth,
			}, func() {}, nil
		},
	}
}

func TestVersionAndRootHelp(t *testing.T) {
	var out, errBuf bytes.Buffer
	cmd := cli.NewRootCmd(cli.Dependencies{})
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"version"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("version Execute: %v", err)
	}
	if got := out.String(); got != "romero v0.1.0-phase01\n" {
		t.Fatalf("unexpected version output %q", got)
	}

	out.Reset()
	cmd = cli.NewRootCmd(cli.Dependencies{})
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"noop"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error for unknown subcommand")
	}
}

func TestMigrateCommandResolvesDSNFromConfigAndFlags(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "romero.toml")
	cfgBody := "[database]\ndsn = \"postgres://romero:secret@127.0.0.1:54329/romero?sslmode=disable\"\n"
	if err := os.WriteFile(cfgPath, []byte(cfgBody), 0o600); err != nil {
		t.Fatalf("write temp config: %v", err)
	}

	mig := &fakeMigrator{version: 3}
	var out, errBuf bytes.Buffer
	cmd := cli.NewRootCmd(testDeps(&fakeService{}, &fakeAuth{}, mig))
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"migrate", "--config", cfgPath})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("migrate Execute: %v", err)
	}
	if len(mig.dsns) != 1 || !strings.Contains(mig.dsns[0], "127.0.0.1:54329") || mig.directions[0] != "up" {
		t.Fatalf("unexpected migrator calls: dsns=%v dirs=%v", mig.dsns, mig.directions)
	}
	var res map[string]any
	if err := json.Unmarshal(out.Bytes(), &res); err != nil {
		t.Fatalf("unmarshal migrate output: %v (raw=%q)", err, out.String())
	}
	if res["version"] != float64(3) || res["direction"] != "up" {
		t.Fatalf("unexpected migrate json: %+v", res)
	}

	// Missing DSN errors clearly when config doesn't exist and no --dsn/env supplied.
	t.Setenv("ROMERO_DSN", "")
	cmd = cli.NewRootCmd(testDeps(&fakeService{}, &fakeAuth{}, mig))
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"migrate", "--config", filepath.Join(dir, "missing.toml")})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error when no DSN configured")
	}
}

func TestBootstrapAndPrincipalCreateCommands(t *testing.T) {
	tid := uuid.New()
	pid := uuid.New()
	aid := uuid.New()
	princID := uuid.New()
	svc := &fakeService{
		bootstrapOut: &provisioning.BootstrapOutput{
			Tenant:  provisioning.Tenant{ID: tid, Slug: "acme", Name: "Acme", IsActive: true},
			Project: provisioning.Project{ID: pid, TenantID: tid, Slug: "core", Name: "Core", IsActive: true},
			Agent:   &provisioning.Agent{ID: aid, TenantID: tid, ExternalKey: "orch", Name: "Orch", IsActive: true},
			Principal: provisioning.PrincipalView{
				ID:            princID,
				TenantID:      tid,
				PrincipalType: identity.PrincipalOrchestrator,
				Name:          "root",
				APIKeyPrefix:  "rmr_orchestrator_123",
				IsAdmin:       true,
			},
			RawAPIKey: "rmr_orchestrator_12345678_0123456789abcdef0123456789abcdef",
		},
		createKey: "rmr_agent_aaaaaaaa_0123456789abcdef0123456789abcdef",
		createView: provisioning.PrincipalView{
			ID:              uuid.New(),
			TenantID:        tid,
			AgentID:         &aid,
			PrincipalType:   identity.PrincipalAgent,
			Name:            "worker-1",
			APIKeyPrefix:    "rmr_agent_aaaaaaaa_0",
			AllowedProjects: []uuid.UUID{pid},
			AllowedScopes:   []identity.ScopeType{identity.ScopeAgentPrivate, identity.ScopeTaskShared},
		},
	}

	var out, errBuf bytes.Buffer
	cmd := cli.NewRootCmd(testDeps(svc, &fakeAuth{}, &fakeMigrator{}))
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{
		"bootstrap",
		"--dsn", "postgres://user:pass@localhost:5432/romero",
		"--tenant-slug", "acme",
		"--tenant-name", "Acme",
		"--project-slug", "core",
		"--project-name", "Core",
		"--agent-key", "orch",
		"--agent-name", "Orch",
		"--principal-name", "root",
		"--principal-type", "orchestrator",
		"--scopes", "project_shared,task_shared",
		"--admin",
		"--task-grant", "task-1:read,write",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("bootstrap Execute: %v", err)
	}
	if len(svc.bootstrapCalls) != 1 || !svc.bootstrapCalls[0].IsAdmin || len(svc.bootstrapCalls[0].InitialTaskGrants) != 1 {
		t.Fatalf("unexpected bootstrap call: %+v", svc.bootstrapCalls)
	}
	if !strings.Contains(out.String(), "rmr_orchestrator_12345678_") {
		t.Fatalf("expected raw api key in bootstrap output, got %q", out.String())
	}

	// Invalid task-grant syntax rejected cleanly.
	cmd = cli.NewRootCmd(testDeps(svc, &fakeAuth{}, &fakeMigrator{}))
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{
		"bootstrap",
		"--dsn", "postgres://u:p@localhost:5432/db",
		"--tenant-slug", "a", "--tenant-name", "A",
		"--project-slug", "p", "--project-name", "P",
		"--principal-name", "root",
		"--task-grant", "badformat",
	})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error for invalid --task-grant format")
	}

	// principal create parses UUIDs, scopes, and expires-in.
	out.Reset()
	cmd = cli.NewRootCmd(testDeps(svc, &fakeAuth{}, &fakeMigrator{}))
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{
		"principal", "create",
		"--dsn", "postgres://u:p@localhost:5432/db",
		"--tenant-id", tid.String(),
		"--agent-id", aid.String(),
		"--type", "agent",
		"--name", "worker-1",
		"--projects", pid.String(),
		"--scopes", "agent_private,task_shared",
		"--expires-in", "24h",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("principal create Execute: %v", err)
	}
	if len(svc.createCalls) != 1 || svc.createCalls[0].ExpiresAt == nil {
		t.Fatalf("unexpected createCalls: %+v", svc.createCalls)
	}
	if !strings.Contains(out.String(), svc.createKey) {
		t.Fatalf("expected generated key in principal create output, got %q", out.String())
	}
}

func TestTaskPermissionGrantAndRevokeCommands(t *testing.T) {
	tid := uuid.New()
	pid := uuid.New()
	targetPrinc := uuid.New()
	actorID := uuid.New()
	apiKey := "rmr_orchestrator_12345678_0123456789abcdef0123456789abcdef"

	svc := &fakeService{}
	auth := &fakeAuth{
		keys: map[string]identity.AuthContext{
			apiKey: {
				TenantID:        tid,
				PrincipalID:     actorID,
				PrincipalType:   identity.PrincipalOrchestrator,
				AllowedProjects: []uuid.UUID{pid},
				AllowedScopes:   []identity.ScopeType{identity.ScopeTaskShared},
			},
		},
	}

	var out, errBuf bytes.Buffer
	cmd := cli.NewRootCmd(testDeps(svc, auth, &fakeMigrator{}))
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{
		"task-permission", "grant",
		"--dsn", "postgres://u:p@localhost:5432/db",
		"--api-key", apiKey,
		"--tenant-id", tid.String(),
		"--project-id", pid.String(),
		"--task-id", "task-99",
		"--principal-id", targetPrinc.String(),
		"--read", "--write",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("task-permission grant Execute: %v", err)
	}
	if !strings.Contains(out.String(), `"status": "granted"`) {
		t.Fatalf("expected status=granted in output, got %q", out.String())
	}
	if len(svc.taskCalls) != 1 || !svc.taskCalls[0].cmd.CanRead || !svc.taskCalls[0].cmd.CanWrite {
		t.Fatalf("unexpected grant call: %+v", svc.taskCalls)
	}

	// Revoke uses ROMERO_API_KEY fallback and sets both false.
	t.Setenv("ROMERO_API_KEY", apiKey)
	out.Reset()
	cmd = cli.NewRootCmd(testDeps(svc, auth, &fakeMigrator{}))
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{
		"task-permission", "revoke",
		"--dsn", "postgres://u:p@localhost:5432/db",
		"--tenant-id", tid.String(),
		"--project-id", pid.String(),
		"--task-id", "task-99",
		"--principal-id", targetPrinc.String(),
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("task-permission revoke Execute: %v", err)
	}
	if !strings.Contains(out.String(), `"status": "revoked"`) {
		t.Fatalf("expected status=revoked in output, got %q", out.String())
	}
	if len(svc.taskCalls) != 2 || svc.taskCalls[1].cmd.CanRead || svc.taskCalls[1].cmd.CanWrite {
		t.Fatalf("unexpected revoke call: %+v", svc.taskCalls)
	}

	// Missing API key fails closed before touching service.
	t.Setenv("ROMERO_API_KEY", "")
	cmd = cli.NewRootCmd(testDeps(svc, auth, &fakeMigrator{}))
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{
		"task-permission", "grant",
		"--dsn", "postgres://u:p@localhost:5432/db",
		"--tenant-id", tid.String(),
		"--project-id", pid.String(),
		"--task-id", "task-99",
		"--principal-id", targetPrinc.String(),
		"--read",
	})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error when API key missing")
	}

	// Cross-tenant / forbidden error propagates cleanly.
	svc.taskErr = identity.ErrForbidden
	cmd = cli.NewRootCmd(testDeps(svc, auth, &fakeMigrator{}))
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{
		"task-permission", "grant",
		"--dsn", "postgres://u:p@localhost:5432/db",
		"--api-key", apiKey,
		"--tenant-id", tid.String(),
		"--project-id", pid.String(),
		"--task-id", "task-99",
		"--principal-id", targetPrinc.String(),
		"--read",
	})
	if err := cmd.Execute(); !errors.Is(err, identity.ErrForbidden) {
		t.Fatalf("expected ErrForbidden, got %v", err)
	}

	// Grant without --read or --write, and invalid UUIDs on principal create / task-permission fail cleanly.
	invalidCmds := [][]string{
		{"task-permission", "grant", "--api-key", apiKey, "--tenant-id", tid.String(), "--project-id", pid.String(), "--task-id", "t", "--principal-id", targetPrinc.String()},
		{"task-permission", "grant", "--api-key", apiKey, "--tenant-id", "bad", "--project-id", pid.String(), "--task-id", "t", "--principal-id", targetPrinc.String(), "--read"},
		{"task-permission", "grant", "--api-key", apiKey, "--tenant-id", tid.String(), "--project-id", "bad", "--task-id", "t", "--principal-id", targetPrinc.String(), "--read"},
		{"task-permission", "grant", "--api-key", apiKey, "--tenant-id", tid.String(), "--project-id", pid.String(), "--task-id", "t", "--principal-id", "bad", "--read"},
		{"principal", "create", "--tenant-id", "bad", "--type", "user", "--name", "n"},
		{"principal", "create", "--tenant-id", tid.String(), "--agent-id", "bad", "--type", "agent", "--name", "n"},
		{"principal", "create", "--tenant-id", tid.String(), "--type", "user", "--name", "n", "--projects", "bad"},
		{"principal", "create", "--tenant-id", tid.String(), "--type", "user", "--name", "n", "--scopes", "bad"},
		{"bootstrap", "--tenant-slug", "s", "--tenant-name", "n", "--project-slug", "p", "--project-name", "pn", "--principal-name", "u", "--scopes", "bad"},
	}
	for i, args := range invalidCmds {
		c := cli.NewRootCmd(testDeps(svc, auth, &fakeMigrator{}))
		c.SetOut(&out)
		c.SetErr(&errBuf)
		c.SetArgs(args)
		if err := c.Execute(); err == nil {
			t.Fatalf("invalidCmds[%d] (%v) expected error", i, args)
		}
	}
}

func TestPostgresAdapterMappings(t *testing.T) {
	now := time.Now().UTC()
	tid := uuid.New()
	pid := uuid.New()
	aid := uuid.New()
	prID := uuid.New()

	pgTenant := postgres.TenantRecord{ID: tid, Slug: "s", Name: "n", IsActive: true}
	if got := cli.MapTenant(pgTenant); got.ID != tid || got.Slug != "s" || !got.IsActive {
		t.Fatalf("MapTenant mismatch: %+v", got)
	}
	pgProj := postgres.ProjectRecord{ID: pid, TenantID: tid, Slug: "p", Name: "P", IsActive: true}
	if got := cli.MapProject(pgProj); got.ID != pid || got.TenantID != tid {
		t.Fatalf("MapProject mismatch: %+v", got)
	}
	pgAg := postgres.AgentRecord{ID: aid, TenantID: tid, ExternalKey: "k", Name: "A", RoleType: "r", IsActive: true}
	if got := cli.MapAgent(pgAg); got.ID != aid || got.ExternalKey != "k" {
		t.Fatalf("MapAgent mismatch: %+v", got)
	}
	pgCand := postgres.PrincipalAuthCandidate{
		Principal: postgres.PrincipalRecord{
			ID: prID, TenantID: tid, AgentID: &aid, PrincipalType: identity.PrincipalAgent,
			Name: "p", APIKeyPrefix: "rmr_agent_1234567890", AllowedProjects: []uuid.UUID{pid},
			AllowedScopes: []identity.ScopeType{identity.ScopeAgentPrivate}, IsActive: true,
			ExpiresAt: &now, CreatedAt: now, IsAdmin: false,
		},
		APIKeySHA256: "abc",
		TenantActive: true,
		AgentActive:  true,
	}
	gotCands := cli.MapAuthCandidates([]postgres.PrincipalAuthCandidate{pgCand})
	if len(gotCands) != 1 || gotCands[0].Principal.ID != prID || !gotCands[0].TenantActive || !gotCands[0].AgentActive {
		t.Fatalf("MapAuthCandidates mismatch: %+v", gotCands)
	}
}
