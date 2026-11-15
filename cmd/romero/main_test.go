package main

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestRun(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		wantCode    int
		wantOut     string
		wantErrPart string
	}{
		{
			name:     "version subcommand prints version string",
			args:     []string{"version"},
			wantCode: 0,
			wantOut:  "romero v0.1.0-phase01\n",
		},
		{
			name:     "no arguments prints help and exits cleanly",
			args:     nil,
			wantCode: 0,
		},
		{
			name:        "unknown subcommand returns non-zero exit code",
			args:        []string{"noop"},
			wantCode:    1,
			wantErrPart: "unknown command",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var out, errBuf bytes.Buffer
			code := run(tc.args, &out, &errBuf)
			if code != tc.wantCode {
				t.Fatalf("expected exit code %d, got %d (stderr=%q)", tc.wantCode, code, errBuf.String())
			}
			if tc.wantOut != "" && out.String() != tc.wantOut {
				t.Fatalf("expected output %q, got %q", tc.wantOut, out.String())
			}
			if tc.wantErrPart != "" && !strings.Contains(errBuf.String(), tc.wantErrPart) {
				t.Fatalf("expected stderr containing %q, got %q", tc.wantErrPart, errBuf.String())
			}
		})
	}
}

type failingWriter struct{}

func (failingWriter) Write(_ []byte) (int, error) {
	return 0, io.ErrClosedPipe
}

func TestRunVersionWriteFailure(t *testing.T) {
	var errBuf bytes.Buffer
	if code := run([]string{"version"}, failingWriter{}, &errBuf); code == 0 {
		t.Fatal("expected nonzero exit code when version output fails")
	}
}
