package migrations_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/romero-project/romero/migrations"
)

func TestCoreSchemaMigrationContract(t *testing.T) {
	data, err := fs.ReadFile(migrations.FS, "00001_romero_core_schema.sql")
	if err != nil {
		t.Fatalf("expected embedded 00001_romero_core_schema.sql, got error: %v", err)
	}
	sqlText := string(data)

	requiredDirectives := []string{
		"-- +goose Up",
		"-- +goose Down",
		"CREATE EXTENSION IF NOT EXISTS \"pgcrypto\"",
		"CREATE EXTENSION IF NOT EXISTS \"vector\"",
		"CREATE EXTENSION IF NOT EXISTS \"pg_trgm\"",
		"CREATE EXTENSION IF NOT EXISTS \"btree_gist\"",
	}
	for _, directive := range requiredDirectives {
		if !strings.Contains(sqlText, directive) {
			t.Errorf("migration missing required directive or extension: %s", directive)
		}
	}

	requiredTables := []string{
		"CREATE TABLE tenants (",
		"CREATE TABLE projects (",
		"CREATE TABLE agents (",
		"CREATE TABLE principals (",
		"CREATE TABLE memory_sources (",
		"CREATE TABLE memories (",
		"CREATE TABLE memory_versions (",
		"CREATE TABLE memory_embeddings (",
		"CREATE TABLE memory_relations (",
		"CREATE TABLE memory_feedback (",
		"CREATE TABLE memory_jobs (",
		"CREATE TABLE audit_events (",
	}
	for _, tbl := range requiredTables {
		if !strings.Contains(sqlText, tbl) {
			t.Errorf("migration missing required table definition: %s", tbl)
		}
	}

	// Verify memory_versions is self-contained for Point-in-Time queries (AC-07, D6, Technical 1 & 2)
	versionBlock := extractBlock(t, sqlText, "CREATE TABLE memory_versions (", ");")
	requiredVersionColDefs := []string{
		"memory_id           UUID NOT NULL REFERENCES memories(id) ON DELETE CASCADE",
		"version             INTEGER NOT NULL",
		"summary             VARCHAR(600) NOT NULL",
		"content             TEXT NOT NULL",
		"tags                TEXT[] NOT NULL DEFAULT '{}'",
		"entities            TEXT[] NOT NULL DEFAULT '{}'",
		"metadata            JSONB NOT NULL DEFAULT '{}'::jsonb",
		"epistemic_status    epistemic_status_enum NOT NULL",
		"alpha_score         REAL NOT NULL CHECK (alpha_score > 0.0)",
		"beta_score          REAL NOT NULL CHECK (beta_score > 0.0)",
		"importance          REAL NOT NULL DEFAULT 0.50 CHECK (importance >= 0.0 AND importance <= 1.0)",
		"utility_score       REAL NOT NULL DEFAULT 0.00 CHECK (utility_score >= -1.0 AND utility_score <= 1.0)",
		"status              memory_status_enum NOT NULL",
		"valid_from          TIMESTAMPTZ NOT NULL DEFAULT NOW()",
		"valid_until         TIMESTAMPTZ",
		"changed_by_source   UUID REFERENCES memory_sources(id) ON DELETE SET NULL",
		"change_reason       TEXT",
		"created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()",
	}
	for _, colDef := range requiredVersionColDefs {
		if !strings.Contains(versionBlock, colDef) {
			t.Errorf("memory_versions missing required self-contained column definition %q", colDef)
		}
	}

	// Verify idx_memories_idempotency block (AC-03, D3)
	idempotencyIdxBlock := extractBlock(t, sqlText, "CREATE UNIQUE INDEX idx_memories_idempotency", ";")
	for _, frag := range []string{
		"ON memories(tenant_id, idempotency_key)",
		"WHERE idempotency_key IS NOT NULL",
	} {
		if !strings.Contains(idempotencyIdxBlock, frag) {
			t.Errorf("idx_memories_idempotency block missing %q", frag)
		}
	}

	// Verify partitioned idx_memories_active_topic_key block with parenthesized CASE expressions (AC-02, D5)
	topicIdxBlock := extractBlock(t, sqlText, "CREATE UNIQUE INDEX idx_memories_active_topic_key", ";")
	requiredTopicIdxParts := []string{
		"tenant_id,",
		"COALESCE(project_id, '00000000-0000-0000-0000-000000000000'::uuid),",
		"scope_type,",
		"(CASE WHEN scope_type = 'agent_private' THEN COALESCE(owner_agent_id, '00000000-0000-0000-0000-000000000000'::uuid) ELSE '00000000-0000-0000-0000-000000000000'::uuid END),",
		"(CASE WHEN scope_type = 'task_shared' THEN COALESCE(task_id, '') ELSE '' END),",
		"topic_key",
		"WHERE status = 'active' AND topic_key IS NOT NULL",
	}
	for _, part := range requiredTopicIdxParts {
		if !strings.Contains(topicIdxBlock, part) {
			t.Errorf("idx_memories_active_topic_key block missing required expression %q", part)
		}
	}

	// Verify no unparenthesized CASE WHEN appears inside any CREATE [UNIQUE] INDEX statement
	unparenthesizedCaseInIndex := regexp.MustCompile(`(?m)^\s*CASE\s+WHEN\b`)
	if unparenthesizedCaseInIndex.MatchString(topicIdxBlock) {
		t.Errorf("idx_memories_active_topic_key contains unparenthesized CASE WHEN expression invalid in PostgreSQL index_elem grammar")
	}

	// Verify audit_events allows NULL tenant_id for unauthenticated 401 audit entries (AC-01, Spec §Behaviour)
	auditBlock := extractBlock(t, sqlText, "CREATE TABLE audit_events (", ");")
	if !strings.Contains(auditBlock, "tenant_id           UUID REFERENCES tenants(id) ON DELETE CASCADE") {
		t.Errorf("audit_events.tenant_id must be nullable UUID REFERENCES tenants(id) ON DELETE CASCADE to record unauthenticated 401 attempts")
	}
}

func TestCoreSchemaTriangulationInvariants(t *testing.T) {
	data, err := fs.ReadFile(migrations.FS, "00001_romero_core_schema.sql")
	if err != nil {
		t.Fatalf("expected embedded 00001_romero_core_schema.sql, got error: %v", err)
	}
	sqlText := string(data)

	parts := strings.Split(sqlText, "-- +goose Down")
	if len(parts) != 2 {
		t.Fatalf("expected exactly one '-- +goose Down' section, got %d parts", len(parts))
	}
	upSQL, downSQL := parts[0], parts[1]

	// 1. Every table in Up must be dropped in Down in reverse FK order
	expectedReverseDropOrder := []string{
		"DROP TABLE IF EXISTS audit_events;",
		"DROP TABLE IF EXISTS memory_jobs;",
		"DROP TABLE IF EXISTS memory_feedback;",
		"DROP TABLE IF EXISTS memory_relations;",
		"DROP TABLE IF EXISTS memory_embeddings;",
		"DROP TABLE IF EXISTS memory_versions;",
		"DROP TABLE IF EXISTS memories;",
		"DROP TABLE IF EXISTS memory_sources;",
		"DROP TABLE IF EXISTS principals;",
		"DROP TABLE IF EXISTS agents;",
		"DROP TABLE IF EXISTS projects;",
		"DROP TABLE IF EXISTS tenants;",
	}
	lastIdx := -1
	for _, stmt := range expectedReverseDropOrder {
		idx := strings.Index(downSQL, stmt)
		if idx == -1 {
			t.Errorf("down migration missing %q", stmt)
			continue
		}
		if idx < lastIdx {
			t.Errorf("down migration statement %q is out of reverse FK dependency order", stmt)
		}
		lastIdx = idx
	}

	// 2. Every enum in Up must be dropped in Down
	expectedEnumDrops := []string{
		"DROP TYPE IF EXISTS job_status_enum;",
		"DROP TYPE IF EXISTS feedback_signal_enum;",
		"DROP TYPE IF EXISTS relation_type_enum;",
		"DROP TYPE IF EXISTS source_type_enum;",
		"DROP TYPE IF EXISTS scope_type_enum;",
		"DROP TYPE IF EXISTS epistemic_status_enum;",
		"DROP TYPE IF EXISTS memory_status_enum;",
		"DROP TYPE IF EXISTS memory_type_enum;",
	}
	for _, stmt := range expectedEnumDrops {
		if !strings.Contains(downSQL, stmt) {
			t.Errorf("down migration missing %q", stmt)
		}
	}

	// 3. Domain invariants enforced at the SQL constraint level (AC-01, AC-04, AC-07)
	requiredConstraints := []string{
		"principal_type IN ('user', 'service', 'orchestrator', 'agent')",
		"GENERATED ALWAYS AS (alpha_score / (alpha_score + beta_score)) STORED",
		"CONSTRAINT chk_valid_temporal_window CHECK (valid_until IS NULL OR valid_until >= valid_from)",
		"CONSTRAINT chk_version_temporal_window CHECK (valid_until IS NULL OR valid_until >= valid_from)",
		"CONSTRAINT chk_relation_temporal_window CHECK (valid_until IS NULL OR valid_until >= valid_from)",
		"CONSTRAINT chk_no_self_loop CHECK (from_memory_id <> to_memory_id)",
		"outcome IN ('allowed', 'denied')",
	}
	for _, c := range requiredConstraints {
		if !strings.Contains(upSQL, c) {
			t.Errorf("up migration missing domain constraint %q", c)
		}
	}
}

func TestBaseScaffoldingFilesExist(t *testing.T) {
	root := filepath.Join("..")
	requiredFiles := map[string][]string{
		"configs/romero.toml": {
			"[server]",
			"rest_port = 8420",
			"[database]",
			"54329/romero",
			"[profiles.lite]",
			"[profiles.balanced]",
			"[profiles.ultra]",
		},
		"docker-compose.yml": {
			"romero-postgres:",
			"pgvector/pgvector:pg17",
			"127.0.0.1:54329:5432",
		},
		"sqlc.yaml": {
			"version: \"2\"",
			"engine: \"postgresql\"",
			"sql_package: \"pgx/v5\"",
			"import: \"time\"",
			"type: \"Time\"",
			"pointer: true",
		},
		"Makefile": {
			"CGO_ENABLED=0",
			"./cmd/romero",
			"test-race:",
		},
		"cmd/romero/main.go": {
			"package main",
			"func main()",
		},
	}

	for relPath, expectedSubstrings := range requiredFiles {
		fullPath := filepath.Join(root, filepath.FromSlash(relPath))
		content, err := os.ReadFile(fullPath)
		if err != nil {
			t.Errorf("expected scaffolding file %s to exist, got error: %v", relPath, err)
			continue
		}
		text := string(content)
		for _, sub := range expectedSubstrings {
			if !strings.Contains(text, sub) {
				t.Errorf("expected file %s to contain %q", relPath, sub)
			}
		}
	}
}

func extractBlock(t *testing.T, sqlText, startMarker, endMarker string) string {
	t.Helper()
	start := strings.Index(sqlText, startMarker)
	if start == -1 {
		t.Fatalf("marker %q not found in SQL", startMarker)
	}
	end := strings.Index(sqlText[start:], endMarker)
	if end == -1 {
		t.Fatalf("end marker %q after %q not found in SQL", endMarker, startMarker)
	}
	return sqlText[start : start+end+len(endMarker)]
}
