package main

import (
	"testing"

	"github.com/romero-project/romero/internal/domain/governor"
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
