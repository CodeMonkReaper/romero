package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/romero-project/romero/internal/application/consolidation"
	"github.com/romero-project/romero/internal/interfaces/cli"
)

func TestConsolidateCmd(t *testing.T) {
	t.Run("executes consolidation with flags and prints JSON", func(t *testing.T) {
		wantProj := uuid.New()
		var gotDSN, gotKey string
		var gotProj *uuid.UUID
		var gotDry bool

		cmd := cli.NewRootCmd(cli.Dependencies{
			Consolidate: func(_ context.Context, dsn, rawAPIKey string, projectID *uuid.UUID, dryRun bool) (*consolidation.ConsolidateResult, error) {
				gotDSN = dsn
				gotKey = rawAPIKey
				gotProj = projectID
				gotDry = dryRun
				return &consolidation.ConsolidateResult{
					ClustersFound:        1,
					DistilledCount:       1,
					ConsolidatedEpisodes: 3,
				}, nil
			},
		})
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetArgs([]string{
			"consolidate",
			"--dsn", "postgres://example/db",
			"--api-key", "rmr_admin_test",
			"--project-id", wantProj.String(),
			"--dry-run",
		})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("consolidate execute: %v", err)
		}
		if gotDSN != "postgres://example/db" || gotKey != "rmr_admin_test" || !gotDry || gotProj == nil || *gotProj != wantProj {
			t.Fatalf("unexpected args passed: dsn=%q key=%q proj=%v dry=%v", gotDSN, gotKey, gotProj, gotDry)
		}
		var res consolidation.ConsolidateResult
		if err := json.Unmarshal(out.Bytes(), &res); err != nil {
			t.Fatalf("unmarshal output: %v", err)
		}
		if res.ClustersFound != 1 || res.DistilledCount != 1 || res.ConsolidatedEpisodes != 3 {
			t.Fatalf("unexpected result: %+v", res)
		}
	})

	t.Run("requires api key and valid project-id", func(t *testing.T) {
		t.Setenv("ROMERO_API_KEY", "")
		cmd := cli.NewRootCmd(cli.Dependencies{
			Consolidate: func(context.Context, string, string, *uuid.UUID, bool) (*consolidation.ConsolidateResult, error) {
				return &consolidation.ConsolidateResult{}, nil
			},
		})
		cmd.SetArgs([]string{"consolidate", "--dsn", "postgres://example/db"})
		err := cmd.Execute()
		if err == nil || !strings.Contains(err.Error(), "api key") {
			t.Fatalf("expected missing api key error, got %v", err)
		}

		cmdBad := cli.NewRootCmd(cli.Dependencies{
			Consolidate: func(context.Context, string, string, *uuid.UUID, bool) (*consolidation.ConsolidateResult, error) {
				return &consolidation.ConsolidateResult{}, nil
			},
		})
		cmdBad.SetArgs([]string{"consolidate", "--dsn", "postgres://example/db", "--api-key", "k", "--project-id", "bad"})
		errBad := cmdBad.Execute()
		if errBad == nil || !strings.Contains(errBad.Error(), "invalid --project-id") {
			t.Fatalf("expected invalid project-id error, got %v", errBad)
		}
	})
}
