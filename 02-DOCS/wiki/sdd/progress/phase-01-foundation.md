# Progress Ledger — `phase-01-foundation`

> Spec: [../specs/phase-01-foundation.md](../specs/phase-01-foundation.md) · Plan: [../plans/phase-01-foundation.md](../plans/phase-01-foundation.md)

## T001 — 2026-11-13
- status: complete
- red: `CGO_ENABLED=0 go test -v ./migrations/...` failed on `TestCoreSchemaMigrationContract` (`open 00001_romero_core_schema.sql: file does not exist`) and `TestBaseScaffoldingFilesExist`
- green: `CGO_ENABLED=0 go build ./cmd/romero` and `CGO_ENABLED=0 go test -v ./migrations/...` passed after implementing `migrations/00001_romero_core_schema.sql` and base scaffolding
- triangulation: `TestCoreSchemaTriangulationInvariants` (reverse FK drop order in `-- +goose Down`, enum drops, temporal/Bayesian/audit constraints, block-scoped `idx_memories_active_topic_key` parenthesized `CASE` verification) and `TestRun` (table-driven CLI branches, 80.0% coverage in `cmd/romero`)
- files: `.gitignore`, `02-DOCS/wiki/sdd/config.yaml`, `Makefile`, `cmd/romero/main.go`, `cmd/romero/main_test.go`, `configs/romero.toml`, `docker-compose.yml`, `go.mod`, `migrations/00001_romero_core_schema.sql`, `migrations/embed.go`, `migrations/schema_test.go`, `sqlc.yaml`
- risk: tier 2 (`governor-infra`, `migrations`) — 3 refuters (`correctness`, `security`, `tests`), `humanApproval: true`
- review: Round 1: 8 reported → 7 unique → 6 confirmed (`L1`–`L7` fixed in `078645f`); Round 2: 0 reported → 0 unique → `VERDICT: pass` across all 3 lenses (`panel of 3 recorded on 078645f`)
- controls: tests ✅ (`go test -v -cover ./...`) · build ✅ (`CGO_ENABLED=0 go build ./cmd/romero` & `go vet ./...`) · security ✅ (`refuter-security` pass, loopback bindings, nullable `tenant_id` for 401 audit) · traceability ✅ (`AC-01`, `AC-02`, `AC-03`, `AC-07`, `AC-10`) · live-postgres-container **not run** (`com.docker.service` stopped on host; DDL verified structurally and ready for `docker compose up -d romero-postgres`)
- skill_resolution: `used: [implement, go, postgresdb, review, verify]`, `missing: []`, `fallback: []`
- decision: logged in `02-DOCS/wiki/sdd/decisions.md` (`T001`: parenthesized `CASE` in `idx_memories_active_topic_key` and nullable `audit_events.tenant_id`); Tier 2 checkpoint presented to user

## Decision queue

- **DQ-01 (`review-after`, task: `T001`, tier: `2`):**
  - **Question:** Ejecutar la validación en vivo del contenedor `romero-postgres` (`docker compose up -d romero-postgres`) cuando el servicio `Docker Desktop Service` (`com.docker.service`) esté iniciado en el host Windows.
  - **Options:** (1) Iniciar Docker Desktop antes de `T004`/`T009` (mientras `T002` y `T003` son paquetes de dominio puro en Go sin dependencia de red/DB); (2) Iniciarlo ahora mismo antes de pasar a `T002`.
  - **Recommendation:** Opción 1 o 2 según prefiera el usuario (`T002` y `T003` son 100% Go puro en `internal/domain`; `T004` y `T009` ejercitarán `romero-postgres` en `127.0.0.1:54329`).
  - **Evidence:** `migrations/00001_romero_core_schema.sql`, `migrations/schema_test.go` (`078645f`).
