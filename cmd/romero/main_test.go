package main

import (
	"bytes"
	"testing"
)

func TestRun(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantCode int
		wantOut  string
	}{
		{
			name:     "version subcommand prints version string",
			args:     []string{"version"},
			wantCode: 0,
			wantOut:  "romero v0.1.0-phase01\n",
		},
		{
			name:     "no arguments exits cleanly with empty output",
			args:     nil,
			wantCode: 0,
			wantOut:  "",
		},
		{
			name:     "unknown subcommand stub exits cleanly with empty output",
			args:     []string{"noop"},
			wantCode: 0,
			wantOut:  "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			code := run(tc.args, &out)
			if code != tc.wantCode {
				t.Fatalf("expected exit code %d, got %d", tc.wantCode, code)
			}
			if got := out.String(); got != tc.wantOut {
				t.Fatalf("expected output %q, got %q", tc.wantOut, got)
			}
		})
	}
}
