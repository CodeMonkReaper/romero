package cli

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/romero-project/romero/internal/application/provisioning"
)

func TestConfigAndParserEdgeCases(t *testing.T) {
	t.Setenv("ROMERO_DSN", "postgres://env/db")
	if got, err := resolveDSN("", ""); err != nil || got != "postgres://env/db" {
		t.Fatalf("resolveDSN env: got=%q err=%v", got, err)
	}
	if got, err := resolveDSN("postgres://flag/db", ""); err != nil || got != "postgres://flag/db" {
		t.Fatalf("resolveDSN flag precedence: got=%q err=%v", got, err)
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

	if got, err := parseExpiresAt(0); err != nil || got != nil {
		t.Fatalf("parseExpiresAt(0): got=%v err=%v", got, err)
	}
	if got, err := parseExpiresAt(-time.Second); !errors.Is(err, provisioning.ErrInvalidInput) || got != nil {
		t.Fatalf("parseExpiresAt(-1s): got=%v err=%v", got, err)
	}
	if got, err := parseExpiresAt(time.Hour); err != nil || got == nil {
		t.Fatalf("parseExpiresAt(1h): got=%v err=%v", got, err)
	}
}

func TestResolveRuntimeNeuralConfig(t *testing.T) {
	dir := t.TempDir()
	tomlPath := filepath.Join(dir, "romero.toml")
	content := `
[server]
active_profile = "ultra"

[profiles.ultra]
embedding_endpoint = "http://127.0.0.1:8081/embed"
reranker_endpoint = "http://127.0.0.1:8082/rerank"
nli_endpoint = "http://127.0.0.1:8083/nli"
slm_rem_endpoint = "http://127.0.0.1:11434/api/generate"
slm_rem_model = "qwen2.5:3b-instruct-q4_K_M"
`
	if err := os.WriteFile(tomlPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write toml: %v", err)
	}

	for _, k := range []string{"ROMERO_PROFILE", "ROMERO_BGE_M3_URL", "ROMERO_RERANKER_URL", "ROMERO_NLI_URL", "ROMERO_SLM_URL", "ROMERO_SLM_MODEL"} {
		t.Setenv(k, "")
	}

	got := ResolveRuntimeNeuralConfig(tomlPath)
	if got.Profile != "ultra" {
		t.Fatalf("Profile = %q, want ultra", got.Profile)
	}
	if got.EmbeddingURL != "http://127.0.0.1:8081" {
		t.Fatalf("EmbeddingURL = %q, want http://127.0.0.1:8081", got.EmbeddingURL)
	}
	if got.RerankerURL != "http://127.0.0.1:8082" {
		t.Fatalf("RerankerURL = %q, want http://127.0.0.1:8082", got.RerankerURL)
	}
	if got.NLIURL != "http://127.0.0.1:8083" {
		t.Fatalf("NLIURL = %q, want http://127.0.0.1:8083", got.NLIURL)
	}
	if got.SLMURL != "http://127.0.0.1:11434" {
		t.Fatalf("SLMURL = %q, want http://127.0.0.1:11434", got.SLMURL)
	}
	if got.SLMModel != "qwen2.5:3b-instruct-q4_K_M" {
		t.Fatalf("SLMModel = %q, want qwen2.5:3b-instruct-q4_K_M", got.SLMModel)
	}

	// Environment variables must override TOML values.
	t.Setenv("ROMERO_PROFILE", "balanced")
	t.Setenv("ROMERO_BGE_M3_URL", "http://127.0.0.1:19081/embed/")
	overridden := ResolveRuntimeNeuralConfig(tomlPath)
	if overridden.Profile != "balanced" || overridden.EmbeddingURL != "http://127.0.0.1:19081" {
		t.Fatalf("env override mismatch: %+v", overridden)
	}

	// Missing file falls back cleanly to balanced.
	missing := ResolveRuntimeNeuralConfig(filepath.Join(dir, "missing.toml"))
	if missing.Profile != "balanced" {
		t.Fatalf("missing file Profile = %q, want balanced", missing.Profile)
	}

	// Base URLs without suffixes, /api/embeddings, /consolidate, and [runtime] fallback.
	runtimeTOML := filepath.Join(dir, "runtime.toml")
	runtimeContent := `
[runtime]
active_profile = "lite"

[profiles.lite]
embedding_endpoint = "http://127.0.0.1:11434/api/embeddings"
slm_rem_endpoint = "http://127.0.0.1:9000/consolidate"
`
	if err := os.WriteFile(runtimeTOML, []byte(runtimeContent), 0o600); err != nil {
		t.Fatalf("write runtime toml: %v", err)
	}
	t.Setenv("ROMERO_PROFILE", "")
	t.Setenv("ROMERO_BGE_M3_URL", "")
	liteCfg := ResolveRuntimeNeuralConfig(runtimeTOML)
	if liteCfg.Profile != "lite" || liteCfg.EmbeddingURL != "http://127.0.0.1:11434" || liteCfg.SLMURL != "http://127.0.0.1:9000" {
		t.Fatalf("runtime/lite fallback mismatch: %+v", liteCfg)
	}
	t.Setenv("ROMERO_BGE_M3_URL", "http://127.0.0.1:8081/")
	bareCfg := ResolveRuntimeNeuralConfig(runtimeTOML)
	if bareCfg.EmbeddingURL != "http://127.0.0.1:8081" {
		t.Fatalf("bare URL with trailing slash = %q, want http://127.0.0.1:8081", bareCfg.EmbeddingURL)
	}

	// Context propagation helpers.
	if gotPath := ConfigPathFromContext(nil); gotPath != "" { //nolint:staticcheck // explicit nil context guard check
		t.Fatalf("ConfigPathFromContext(nil) = %q, want empty", gotPath)
	}
	ctx := WithConfigPath(t.Context(), "")
	if gotPath := ConfigPathFromContext(ctx); gotPath != "" {
		t.Fatalf("ConfigPathFromContext(empty) = %q, want empty", gotPath)
	}
	ctx = WithConfigPath(t.Context(), "  /tmp/custom.toml  ")
	if gotPath := ConfigPathFromContext(ctx); gotPath != "/tmp/custom.toml" {
		t.Fatalf("ConfigPathFromContext(custom) = %q, want /tmp/custom.toml", gotPath)
	}
}
