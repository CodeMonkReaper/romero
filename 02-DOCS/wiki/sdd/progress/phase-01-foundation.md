# Progress Ledger — `phase-01-foundation`

## T002 — 2026-11-13 — implementation checkpoint
- status: implemented; verification pending
- base: 026b599a1c871cd2ace666729296919c207ce1ad; branch: codex/t002-identity-security
- red/green: compiling stubs failed behavioral assertions, then passed; topic partition had a separate red/green cycle. Evidence: [t002-tdd-evidence.md](t002-tdd-evidence.md).
- controls: Docker golang:1.27: domain `go test -race -cover` PASS (identity 97.6%, security 100%); full `go test -race -coverprofile=/tmp/coverage.out ./...` PASS; `go vet ./...` PASS; static `CGO_ENABLED=0 go build` PASS; `go mod verify` PASS. Final domain race/vet/build rerun after review test additions PASS. govulncheck v1.9.0: no vulnerabilities found.
- sqlc: FAIL, pre-existing missing queries directory assigned to T004; no persistence queries belong to T002. No placeholders added to bypass the gate.
- review: computed tier 2/auth, round 1: 1 reported -> 1 unique -> 1 confirmed tests-as-evidence finding. Independent mutant removing IsAdmin survived original tests; new nonadmin user/orchestrator cases kill it. L1 fixed. Round 2 three lenses PASS on test-only delta. Reports under ../reviews/t002-*.md.
- skill_resolution: used implement/go/testing-go/review/verify/security-audit; developer agent unavailable due unsupported account model; worker fallback used with same contracts.
- compact_rules: strict assertion red before green; domain only; computed risk; three independent review lenses; no missing control called passed.
- traceability: AC-01 scope authorization; AC-02 task requirement and topic partitions; AC-04 principal epistemic authority; AC-09 sanitizer/post-clean hash/XML envelope. Database audit/HTTP mappings remain downstream tasks.
- remaining: linter result pending; sqlc pre-existing gate gap and human tier-2 approval must be explicit before merge. Issue stays open until delivery integrated.
- control correction: golangci-lint v2.15.0 executed, FAIL: pre-existing unchecked fmt.Fprintln in cmd/romero/main.go:15; T002 test QF1001 fixed by equivalent De Morgan expression. Global verification remains FAIL due inherited CLI lint and missing T004 queries. No claim of merge readiness.

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

- **DQ-02 (`blocking`, task: `T002`, tier: `2`):**
  - **Question:** Aprobar el cambio de autorización tras revisar evidencia y aceptar que `sqlc generate` no puede completarse hasta T004 (directorio de consultas ausente en la base).
  - **Options:** Aceptar la brecha de generación SQL no relacionada con dominio; o esperar T004 antes de cerrar la verificación global.
  - **Recommendation:** Revisar T002 con sus pruebas de dominio y mantener el control global SQL pendiente hasta T004; no ampliar T002 con consultas ficticias.
  - **Evidence:** `t002-tdd-evidence.md`, `../reviews/t002-correctness.md`, `../reviews/t002-security.md`, `../reviews/t002-tests.md`; Docker `sqlc v1.31.1 generate` -> missing `internal/infrastructure/persistence/queries`.

- **DQ-01 (`review-after`, task: `T001`, tier: `2`):**
  - **Question:** Ejecutar la validación en vivo del contenedor `romero-postgres` (`docker compose up -d romero-postgres`) cuando el servicio `Docker Desktop Service` (`com.docker.service`) esté iniciado en el host Windows.
  - **Options:** (1) Iniciar Docker Desktop antes de `T004`/`T009` (mientras `T002` y `T003` son paquetes de dominio puro en Go sin dependencia de red/DB); (2) Iniciarlo ahora mismo antes de pasar a `T002`.
  - **Recommendation:** Opción 1 o 2 según prefiera el usuario (`T002` y `T003` son 100% Go puro en `internal/domain`; `T004` y `T009` ejercitarán `romero-postgres` en `127.0.0.1:54329`).
  - **Evidence:** `migrations/00001_romero_core_schema.sql`, `migrations/schema_test.go` (`078645f`).
