package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/romero-project/romero/internal/application/provisioning"
	"github.com/romero-project/romero/internal/domain/identity"
	"github.com/romero-project/romero/internal/domain/memory"
	"github.com/romero-project/romero/internal/infrastructure/persistence/postgres"
)

type stubPGBackend struct {
	err error
}

func (s *stubPGBackend) ExecTx(ctx context.Context, fn func(pgBackend, memory.AuditLogger) error) error {
	if s.err != nil {
		return s.err
	}
	return fn(s, nil)
}

func (s *stubPGBackend) CreateTenant(_ context.Context, a postgres.CreateTenantParams) (postgres.TenantRecord, error) {
	if s.err != nil {
		return postgres.TenantRecord{}, s.err
	}
	return postgres.TenantRecord{ID: a.ID, Slug: a.Slug, Name: a.Name, IsActive: a.IsActive}, nil
}

func (s *stubPGBackend) GetTenantByID(_ context.Context, id uuid.UUID) (*postgres.TenantRecord, error) {
	if s.err != nil {
		return nil, s.err
	}
	return &postgres.TenantRecord{ID: id, Slug: "s", Name: "n", IsActive: true}, nil
}

func (s *stubPGBackend) CreateProject(_ context.Context, a postgres.CreateProjectParams) (postgres.ProjectRecord, error) {
	if s.err != nil {
		return postgres.ProjectRecord{}, s.err
	}
	return postgres.ProjectRecord{ID: a.ID, TenantID: a.TenantID, Slug: a.Slug, Name: a.Name, IsActive: a.IsActive}, nil
}

func (s *stubPGBackend) GetProjectByID(_ context.Context, tid, id uuid.UUID) (*postgres.ProjectRecord, error) {
	if s.err != nil {
		return nil, s.err
	}
	return &postgres.ProjectRecord{ID: id, TenantID: tid, Slug: "p", Name: "P", IsActive: true}, nil
}

func (s *stubPGBackend) CountTenantProjects(_ context.Context, _ uuid.UUID, ids []uuid.UUID) (int64, error) {
	return int64(len(ids)), s.err
}

func (s *stubPGBackend) CreateAgent(_ context.Context, a postgres.CreateAgentParams) (postgres.AgentRecord, error) {
	if s.err != nil {
		return postgres.AgentRecord{}, s.err
	}
	return postgres.AgentRecord{ID: a.ID, TenantID: a.TenantID, ExternalKey: a.ExternalKey, Name: a.Name, RoleType: a.RoleType, IsActive: a.IsActive}, nil
}

func (s *stubPGBackend) GetAgentByID(_ context.Context, tid, id uuid.UUID) (*postgres.AgentRecord, error) {
	if s.err != nil {
		return nil, s.err
	}
	return &postgres.AgentRecord{ID: id, TenantID: tid, ExternalKey: "k", Name: "A", IsActive: true}, nil
}

func (s *stubPGBackend) CreatePrincipal(_ context.Context, a postgres.CreatePrincipalParams) (postgres.PrincipalRecord, error) {
	if s.err != nil {
		return postgres.PrincipalRecord{}, s.err
	}
	return postgres.PrincipalRecord{
		ID: a.ID, TenantID: a.TenantID, AgentID: a.AgentID, PrincipalType: a.PrincipalType,
		Name: a.Name, APIKeyPrefix: a.APIKeyPrefix, AllowedProjects: a.AllowedProjects,
		AllowedScopes: a.AllowedScopes, IsActive: a.IsActive, ExpiresAt: a.ExpiresAt, IsAdmin: a.IsAdmin,
	}, nil
}

func (s *stubPGBackend) GetPrincipalByID(_ context.Context, tid, id uuid.UUID) (*postgres.PrincipalRecord, error) {
	if s.err != nil {
		return nil, s.err
	}
	return &postgres.PrincipalRecord{ID: id, TenantID: tid, PrincipalType: identity.PrincipalUser, Name: "u"}, nil
}

func (s *stubPGBackend) LookupPrincipalsByPrefix(_ context.Context, prefix string) ([]postgres.PrincipalAuthCandidate, error) {
	if s.err != nil {
		return nil, s.err
	}
	return []postgres.PrincipalAuthCandidate{{
		Principal:    postgres.PrincipalRecord{ID: uuid.New(), TenantID: uuid.New(), APIKeyPrefix: prefix},
		APIKeySHA256: "h", TenantActive: true, AgentActive: true,
	}}, nil
}

func (s *stubPGBackend) UpsertTaskPermission(_ context.Context, _ postgres.TaskPermissionGrant) error {
	return s.err
}

func TestPGProvisioningAdapterRoundtripAndErrors(t *testing.T) {
	ctx := context.Background()
	tid, pid, aid, prid := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	now := time.Now().UTC()

	okAdapter := NewPostgresAdapter(&stubPGBackend{})
	if err := okAdapter.ExecTx(ctx, func(repo provisioning.Repository, _ memory.AuditLogger) error {
		tRec, err := repo.CreateTenant(ctx, provisioning.CreateTenantParams{ID: tid, Slug: "s", Name: "n", IsActive: true})
		if err != nil || tRec.ID != tid {
			return errors.New("CreateTenant failed")
		}
		if got, err := repo.GetTenantByID(ctx, tid); err != nil || got.ID != tid {
			return errors.New("GetTenantByID failed")
		}
		pRec, err := repo.CreateProject(ctx, provisioning.CreateProjectParams{ID: pid, TenantID: tid, Slug: "p", Name: "P", IsActive: true})
		if err != nil || pRec.ID != pid {
			return errors.New("CreateProject failed")
		}
		if got, err := repo.GetProjectByID(ctx, tid, pid); err != nil || got.ID != pid {
			return errors.New("GetProjectByID failed")
		}
		if cnt, err := repo.CountTenantProjects(ctx, tid, []uuid.UUID{pid}); err != nil || cnt != 1 {
			return errors.New("CountTenantProjects failed")
		}
		ag, err := repo.CreateAgent(ctx, provisioning.CreateAgentParams{ID: aid, TenantID: tid, ExternalKey: "k", Name: "A", IsActive: true})
		if err != nil || ag.ID != aid {
			return errors.New("CreateAgent failed")
		}
		if got, err := repo.GetAgentByID(ctx, tid, aid); err != nil || got.ID != aid {
			return errors.New("GetAgentByID failed")
		}
		pr, err := repo.CreatePrincipal(ctx, provisioning.CreatePrincipalParams{
			ID: prid, TenantID: tid, AgentID: &aid, PrincipalType: identity.PrincipalAgent,
			Name: "pr", APIKeyPrefix: "rmr_agent_1234567890", APIKeySHA256: "h",
			AllowedProjects: []uuid.UUID{pid}, AllowedScopes: []identity.ScopeType{identity.ScopeAgentPrivate},
			IsActive: true, ExpiresAt: &now,
		})
		if err != nil || pr.ID != prid {
			return errors.New("CreatePrincipal failed")
		}
		if got, err := repo.GetPrincipalByID(ctx, tid, prid); err != nil || got.ID != prid {
			return errors.New("GetPrincipalByID failed")
		}
		if cands, err := repo.LookupPrincipalsByPrefix(ctx, "rmr_agent_1234567890"); err != nil || len(cands) != 1 {
			return errors.New("LookupPrincipalsByPrefix failed")
		}
		return repo.UpsertTaskPermission(ctx, provisioning.TaskPermissionGrant{
			TenantID: tid, ProjectID: pid, TaskID: "t1", PrincipalID: prid, CanRead: true,
		})
	}); err != nil {
		t.Fatalf("okAdapter.ExecTx: %v", err)
	}

	failAdapter := NewPostgresAdapter(&stubPGBackend{err: errors.New("boom")})
	if _, err := failAdapter.CreateTenant(ctx, provisioning.CreateTenantParams{}); err == nil {
		t.Fatal("expected error from CreateTenant")
	}
	if _, err := failAdapter.GetTenantByID(ctx, tid); err == nil {
		t.Fatal("expected error from GetTenantByID")
	}
	if _, err := failAdapter.CreateProject(ctx, provisioning.CreateProjectParams{}); err == nil {
		t.Fatal("expected error from CreateProject")
	}
	if _, err := failAdapter.GetProjectByID(ctx, tid, pid); err == nil {
		t.Fatal("expected error from GetProjectByID")
	}
	if _, err := failAdapter.CreateAgent(ctx, provisioning.CreateAgentParams{}); err == nil {
		t.Fatal("expected error from CreateAgent")
	}
	if _, err := failAdapter.GetAgentByID(ctx, tid, aid); err == nil {
		t.Fatal("expected error from GetAgentByID")
	}
	if _, err := failAdapter.CreatePrincipal(ctx, provisioning.CreatePrincipalParams{}); err == nil {
		t.Fatal("expected error from CreatePrincipal")
	}
	if _, err := failAdapter.GetPrincipalByID(ctx, tid, prid); err == nil {
		t.Fatal("expected error from GetPrincipalByID")
	}
	if _, err := failAdapter.LookupPrincipalsByPrefix(ctx, "x"); err == nil {
		t.Fatal("expected error from LookupPrincipalsByPrefix")
	}
}

func TestConfigAndParserEdgeCases(t *testing.T) {
	t.Setenv("ROMERO_DSN", "postgres://env/db")
	if got, err := resolveDSN("", ""); err != nil || got != "postgres://env/db" {
		t.Fatalf("resolveDSN env: got=%q err=%v", got, err)
	}
	t.Setenv("ROMERO_DSN", "")

	dir := t.TempDir()
	emptyTOML := filepath.Join(dir, "empty.toml")
	_ = os.WriteFile(emptyTOML, []byte("[database]\ndsn = \"\"\n"), 0o600)
	if _, err := resolveDSN("", emptyTOML); err == nil {
		t.Fatal("expected error for empty [database].dsn")
	}
	badTOML := filepath.Join(dir, "bad.toml")
	_ = os.WriteFile(badTOML, []byte("not = [valid"), 0o600)
	if _, err := resolveDSN("", badTOML); err == nil {
		t.Fatal("expected error for malformed toml")
	}

	if _, err := parseScopes([]string{"unknown_scope"}); err == nil {
		t.Fatal("expected parseScopes error")
	}
	if _, err := parseUUIDList([]string{"not-a-uuid"}); err == nil {
		t.Fatal("expected parseUUIDList error")
	}
	if _, err := parseTaskGrants([]string{"t1:bogus"}); err == nil {
		t.Fatal("expected parseTaskGrants error")
	}
	if defs := DefaultDependencies(); defs.Migrator == nil || defs.OpenProvisioner == nil {
		t.Fatal("expected DefaultDependencies to populate Migrator and OpenProvisioner")
	}
}
