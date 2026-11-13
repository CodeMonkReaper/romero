package main

import (
	"bytes"
	"testing"
)

func TestRunVersion(t *testing.T) {
	var out bytes.Buffer
	code := run([]string{"version"}, &out)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}
	if got := out.String(); got != "romero v0.1.0-phase01\n" {
		t.Fatalf("unexpected version output: %q", got)
	}
}
