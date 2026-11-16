package main

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/romero-project/romero/internal/application/consolidation"
	"github.com/romero-project/romero/internal/domain/governor"
	"github.com/romero-project/romero/internal/domain/identity"
	"github.com/romero-project/romero/internal/infrastructure/persistence/postgres"
)

func TestDefaultDependenciesIncludesConsolidate(t *testing.T) {
	deps := defaultDependencies()
	if deps.Consolidate == nil {
		t.Fatal("expected Consolidate dependency to be wired")
	}
}

func TestBuildNeuralAndGovernorProfiles(t *testing.T) {
	t.Setenv("ROMERO_PROFILE", "ultra")
	t.Setenv("ROMERO_RERANKER_URL", "http://127.0.0.1:18001")
	t.Setenv("ROMERO_NLI_URL", "http://127.0.0.1:18002")
	t.Setenv("ROMERO_SLM_URL", "http://127.0.0.1:18003")

	_, _, _, gov, prof := buildNeuralAndGovernor()
	if prof != governor.ProfileUltra {
		t.Fatalf("profile = %s, want ultra", prof)
	}
	snap := gov.Status()
	if !snap.RerankerAvailable || !snap.NLIAvailable || !snap.SLMAvailable {
		t.Fatalf("expected all neural capabilities available in ultra profile: %+v", snap)
	}

	t.Setenv("ROMERO_PROFILE", "lite")
	_, _, _, govLite, profLite := buildNeuralAndGovernor()
	if profLite != governor.ProfileLite {
		t.Fatalf("profile = %s, want lite", profLite)
	}
	if govLite.Status().RerankerAvailable {
		t.Fatal("expected reranker disabled in lite profile")
	}
}

func TestBuildNeuralAndGovernorFromConfigFile(t *testing.T) {
	for _, k := range []string{"ROMERO_PROFILE", "ROMERO_BGE_M3_URL", "ROMERO_RERANKER_URL", "ROMERO_NLI_URL", "ROMERO_SLM_URL", "ROMERO_SLM_MODEL"} {
		t.Setenv(k, "")
	}
	_, _, _, gov, prof := buildNeuralAndGovernorFromConfig("../../configs/romero.toml")
	if prof != governor.ProfileUltra {
		t.Fatalf("profile = %s, want ultra", prof)
	}
	snap := gov.Status()
	if !snap.RerankerAvailable || !snap.NLIAvailable || !snap.SLMAvailable {
		t.Fatalf("expected ultra endpoints from configs/romero.toml to enable capabilities: %+v", snap)
	}
}

type fakeREMLister struct {
	targets []postgres.REMTarget
	err     error
	calls   int
}

func (f *fakeREMLister) ListActiveREMTargets(context.Context) ([]postgres.REMTarget, error) {
	f.calls++
	return f.targets, f.err
}

type fakeREMConsolidator struct {
	calls []identity.AuthContext
	cmds  []consolidation.ConsolidateCommand
}

func (f *fakeREMConsolidator) Consolidate(_ context.Context, auth identity.AuthContext, cmd consolidation.ConsolidateCommand) (*consolidation.ConsolidateResult, error) {
	f.calls = append(f.calls, auth)
	f.cmds = append(f.cmds, cmd)
	return &consolidation.ConsolidateResult{}, nil
}

func TestRunIdleREMTick_RespectsIdleWindowAndTenants(t *testing.T) {
	ctx := context.Background()
	burst := 50 * time.Millisecond
	gov := governor.NewPowerGovernor(governor.Config{
		Profile:      governor.ProfileBalanced,
		BurstWindow:  burst,
		SLMAvailable: true,
		NLIAvailable: true,
	})
	base := gov.Status().LastActivityAt
	t1 := postgres.REMTarget{TenantID: uuid.New(), PrincipalID: uuid.New()}
	t2 := postgres.REMTarget{TenantID: uuid.New(), PrincipalID: uuid.New()}
	lister := &fakeREMLister{targets: []postgres.REMTarget{t1, t2}}
	cons := &fakeREMConsolidator{}

	// Before burstWindow expires -> no DB list or consolidation triggered.
	runIdleREMTick(ctx, gov, lister, cons, func() time.Time { return base.Add(burst / 2) })
	if lister.calls != 0 || len(cons.calls) != 0 {
		t.Fatalf("expected tick before idle window to skip, got lister=%d cons=%d", lister.calls, len(cons.calls))
	}

	// After burstWindow expires -> consolidates both active tenants.
	runIdleREMTick(ctx, gov, lister, cons, func() time.Time { return base.Add(burst + time.Millisecond) })
	if lister.calls != 1 || len(cons.calls) != 2 {
		t.Fatalf("expected tick after idle window to consolidate 2 tenants, got lister=%d cons=%d", lister.calls, len(cons.calls))
	}
	if cons.calls[0].TenantID != t1.TenantID || cons.calls[0].PrincipalID != t1.PrincipalID || !cons.calls[0].IsAdmin || cons.cmds[0].CorrelationID != "rem-scheduler" {
		t.Fatalf("unexpected scheduler auth/cmd: auth=%+v cmd=%+v", cons.calls[0], cons.cmds[0])
	}
}
