package eval_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/romero-project/romero/internal/application/eval"
)

func TestRunDataset_GoldenDatasetPassesThresholds(t *testing.T) {
	t.Parallel()

	path := filepath.Join("..", "..", "..", "tests", "evaluation", "golden_dataset.json")
	rep, err := eval.RunDataset(context.Background(), path, 2048, 2)
	if err != nil {
		t.Fatalf("RunDataset failed: %v", err)
	}
	if !rep.Pass {
		t.Fatalf("expected golden dataset to pass, got %+v", rep)
	}
	if rep.NDCGAt5 < 0.80 || rep.MultiHopRecall != 1.0 || rep.ObsoleteSuppressionRate != 1.0 || rep.ZeroTrustLeakageRate != 0.0 {
		t.Fatalf("unexpected metrics: %+v", rep)
	}
}
