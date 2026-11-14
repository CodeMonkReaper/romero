# Progress Ledger — `phase-01-foundation`

## T001 — validación real complementaria — 2026-11-13
- status: pending-controls (no reimplementación de T001)
- issue: https://github.com/CodeMonkReaper/romero/issues/1
- branch: codex/t001-validation; base: 026b599a1c871cd2ace666729296919c207ce1ad
- live-postgres: PASS; contenedor aislado romero-t001-validation sin puertos publicados ni volúmenes del proyecto; PostgreSQL 17.12, vector 0.8.7, pgcrypto 1.3, pg_trgm 1.6, btree_gist 1.7. Imagen sha256:b513c48566e4777cd3370451400000c78eda6a08131d68f4c31d4a76f27bd8fd.
- migration: Up/Down/Up mediante psql ON_ERROR_STOP y transacción única; 12 tablas tras Up y 0 tras Down; ambos índices únicos inspeccionados. No se probó el runner Goose.
- behavioral-check: PASS migrations/t001-live-check.sql; rechaza duplicados de tema activo y clave idempotente, admite tema igual entre agentes y tareas distintos, admite auditoría denied sin tenant. Fixtures revertidas por ROLLBACK. No valida todos los ámbitos ni concurrencia (T009).
- controls: CGO_ENABLED=0 build PASS; go vet PASS; go test -v -cover ./... PASS (CLI 80%). race FAIL por ausencia de gcc; sqlc generate y golangci-lint NOT RUN por herramientas ausentes. git diff --check PASS antes de esta anotación.
- risk: computed tier 2 (migrations), reviewers 3, humanApproval true. Nueva revisión de este archivo NOT RUN; no commit ni integración.
- skill_resolution: used implement, worktrees, postgresdb; models.enabled false; regla compacta: preservar evidencia previa, validar sobre base aislada, controles omitidos no equivalen a PASS.
- next: habilitar herramientas de verificación (o ejecutarlas en un entorno aislado reproducible), completar panel de tres revisores, registrar aprobación requerida e integrar evidencia antes de cerrar Issue.

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

## T001 — continuación: controles y revisión — 2026-11-13
- status: verified-awaiting-integration
- issue: https://github.com/CodeMonkReaper/romero/issues/1
- red: TestRunVersionWriteFailure falló con io.ErrClosedPipe porque la CLI devolvía 0.
- green: error de Fprintln ahora devuelve 1; suite race completa PASS (CLI 83.3%). Build CGO_ENABLED=0, vet, formato, golangci-lint 0 issues, go mod verify y govulncheck sin vulnerabilidades PASS.
- live-postgres: Goose v3.30.1 Up/Down/Up PASS en PostgreSQL 17.12/vector 0.8.7, versión final 1. SQL PASS y dos mutantes (quitar cada índice) fallan correctamente. La prueba está ahora en tests/sql/t001-live-check.sql, fuera del directorio reservado para migraciones Goose.
- sqlc: ejecutado con v1.31.1; FAIL por consultas inexistentes. Diferido a T004 por respuesta humana explícita; seguimiento en Issue #4 y decisions.md. Configuración intacta.
- risk: diff complementario calculado tier 1; T001 original tier 2. Panel completo, ronda 1: 0 reportados → 0 únicos → 0 por verificar; correctness/security/tests pass. Límites de reproducción independiente en reviews/phase-01-foundation-T001-validation.md.
- skill_resolution: implement, go, postgresdb, verify, review, ship; developer delegado no disponible por modelo incompatible con la cuenta; ejecución local como fallback, revisores independientes disponibles.
- evidence: verifications/phase-01-foundation-T001-2026-11-13-r2.md y reviews/phase-01-foundation-T001-validation.md. El veredicto FAIL previo permanece histórico, sustituido para T001 por la excepción autorizada.
- next: publicar el commit complementario y el PR hacia la rama de Phase 01; no cerrar Issue #1 antes de la integración y aprobación requeridas. No se declara Phase 01 completa ni se inicia T002.

## T001 — entrega preparada — 2026-11-13
- status: verified-awaiting-integration
- commit: 30ad02d; autor humano configurado en Git; árbol limpio después del commit.
- PR: https://github.com/CodeMonkReaper/romero/pull/11 (borrador, base feat/phase-01-foundation, head codex/t001-validation).
- tracker: https://github.com/CodeMonkReaper/romero/issues/1#issuecomment-6867770727; control sqlc transferido con autorización humana a https://github.com/CodeMonkReaper/romero/issues/4#issuecomment-6867601911.
- review-ledger: panel correctness/security/tests registrado sobre 30ad02d. Una ejecución de cada lente; el CLI registra round 2 tras asociar el panel al commit final (el registro inicial estaba sobre la base 026b599).
- next: aprobación humana e integración del PR; Issue #1 permanece abierto hasta la integración requerida. T001 validada no implica Phase 01 completada.

## T001 — aprobación e integración en rama de fase — 2026-11-13
- status: integrated-in-phase-branch; entrega a main pendiente.
- human-approval: el usuario respondió «apruebo» a integrar el PR #11 en feat/phase-01-foundation.
- merged: https://github.com/CodeMonkReaper/romero/pull/11; commit aec3b398ef011f1ece783c4c66de17b4ea9e44fa. GitHub confirmó merged=true sobre head esperado 9352cf6554eed9f564665f687d79ed149fcb6dd3; base limpia y mergeable, sin checks CI configurados.
- controls: mismo código verificado y revisado; merge sin conflictos sobre la base original 026b599. No se modificó main ni la carpeta de la sesión concurrente.
- decision-queue: la validación PostgreSQL de DQ-01 ya tiene evidencia runtime y aprobación de integración humana; no queda bloqueada por Docker. sqlc permanece diferido explícitamente a T004.
- next: entrega del trabajo base de T001 desde la rama de fase a main con sus controles de cierre; mantener Issue #1 abierto hasta esa entrega. No se declara Phase 01 completada.

## T001 — entrega a main — 2026-11-13
- status: complete (implementación y verificación); entrega mediante PR #12.
- human-approval: el usuario respondió «si» a proceder con la entrega de T001 a main.
- PR: https://github.com/CodeMonkReaper/romero/pull/12; Closes #1 al fusionarse en main. El estado de merge lo confirma GitHub, no esta anotación preparada antes de integrarse.
- source: código completo 7118a66 contra main 6dd9935, sin commits de main pendientes de integrar; cambios posteriores solo de documentación.
- controls: build CGO_ENABLED=0, vet, race (sin caché, CLI 83.3%), formato, lint, vulnerabilidades y módulos PASS. Goose Down/Up y SQL con rollback PASS. sqlc diferido expresamente a T004; sin otros controles omitidos del gate ejecutado.
- review: tier 2; panel de tres lentes sobre todo el diff contra main, ronda 1; 0 reportados → 0 únicos → 0 por verificar, verdict pass. Límites de reproducción independiente documentados.
- evidence: verifications/phase-01-foundation-T001-main-2026-11-13.md y reviews/phase-01-foundation-T001-main.md.
- next: confirmar merge/cierre de Issue #1; luego T002. Phase 01 sigue en ejecución, T002–T010 pendientes.
