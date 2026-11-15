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
