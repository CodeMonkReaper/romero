---
type: verification
title: T001 — PostgreSQL real y controles
topic: sdd
source_state: 026b599 + working tree codex/t001-validation (dirty)
---
# T001 — verificación complementaria

## Alcance
Control pendiente de T001, no cierre de Phase 01. El esquema de 078645f permanece sin cambios. Se añade una prueba SQL aislada y se corrige un error de escritura de la CLI encontrado al ejecutar lint.

## Resultados observados
- PostgreSQL 17.12 / pgvector 0.8.7: psql Up/Down/Up PASS; 12 tablas tras Up, cero tras Down, índices únicos inspeccionados.
- Goose v3.30.1: Up/Down/Up PASS en base nueva romero_t001_goose; status/version confirman versión 1. La base pertenece al contenedor aislado romero-t001-validation sin puertos publicados ni volúmenes del proyecto.
- tests/sql/t001-live-check.sql PASS en esa base. Prueba partición por agente/tarea, duplicado de tema activo e idempotencia y auditoría denied sin tenant; ROLLBACK elimina fixtures.
- Controles negativos: quitar transaccionalmente idx_memories_active_topic_key provoca 'duplicate active topic accepted'; quitar idx_memories_idempotency provoca 'duplicate idempotency key accepted'. Ambos fallan con salida no cero y la versión íntegra pasa. No prueba todos los ámbitos ni concurrencia (T009).
- CLI: TestRunVersionWriteFailure RED (salida 0 ante io.ErrClosedPipe), luego GREEN tras devolver 1 ante error de Fprintln. Rama feliz conserva su salida.
- Go 1.27.4 Linux / GCC 14.2: CGO_ENABLED=0 go build, go vet y go test -race -coverprofile=/tmp/coverage.out ./... PASS; CLI 83.3%, migraciones sin sentencias ejecutables. No existen aún domain/application para aplicar su umbral.
- golangci-lint v2.15.0 run: 0 issues. gofmt aplicado; go mod verify: all modules verified.
- govulncheck v1.9.0: No vulnerabilities found.
- sqlc v1.31.1 generate: FAIL, 'stat /workspace/internal/infrastructure/persistence/queries: no such file or directory'. La configuración existe desde T001; las consultas y generación están asignadas a T004. No se crean consultas ficticias ni se desactiva el control para obtener verde.

## Reproducción en el entorno de prueba
Ejecutar en romero-t001-tools, que comparte red con el PostgreSQL aislado y monta este worktree en /workspace:
`CGO_ENABLED=0 go build -o /tmp/romero ./cmd/romero`; `go vet ./...`; `go test -race -coverprofile=/tmp/coverage.out ./...`; `golangci-lint run`; `govulncheck ./...`; `sqlc generate`.

Para Goose usar driver postgres, conexión host=127.0.0.1 user=postgres dbname=romero_t001_goose sslmode=disable y directorio migrations. Up/Down se ejecutan exclusivamente en esa base de prueba. Ejecutar la prueba SQL con psql -v ON_ERROR_STOP=1.

## Trazabilidad y límites
El done-check de T001 (build y migración real con índices) pasa. Evidencia parcial de AC-01/02/03/07/10 a nivel esquema; la implementación completa de esos AC corresponde a T002–T009. No declara la fase completa ni validación de aislamiento de aplicación.

## Rollback
Revertir el futuro commit de esta validación. No se alteró la migración del producto. Detener solo los contenedores romero-t001-tools y romero-t001-validation; conservar los datos de cualquier otro servicio.

## Veredicto
FAIL del gate global por sqlc; done-check específico de T001 PASS. Se requiere resolver explícitamente si sqlc es una dependencia diferida a T004 antes de declarar T001 cerrada. Revisión y aprobación humana/integración pendientes.
