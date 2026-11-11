---
type: constitution
title: Romero — Constitution
description: Los principios no negociables que toda fase de rsc-sdd obedece en ROMERO.
tags: [sdd, constitution, romero, go, postgres, pgvector, mcp]
timestamp: 2026-11-11T19:22:30-03:00
topic: sdd
version: v1.0.0
---

# Romero — Constitution

> Version: v1.0.0 · Ratified: 2026-11-11 · Last amended: 2026-11-11
> Los principios no negociables que toda fase de `rsc-sdd` obedece. La especificación maestra reside en `romero_gandalf_wizard_especificaci_n_maestra_de_arquitectura_producto_e_ingenier_a.md`.

## Known facts (from onboarding)

- Goal: Build ROMERO, a high-performance neuro-symbolic, bitemporal, multi-agent cognitive memory platform in Go + PostgreSQL 17/pgvector with REST API and MCP server integration for GANDALF WIZARD
- Project kind: software · software scope: complex
- How code changes land: branches closed with a pull request; the default branch is closed to direct work (`--workflow branches`).
- Stack evidence: Go 1.23+ (`CGO_ENABLED=0`), PostgreSQL 17 (`pgvector` 0.8.0+ `halfvec`/`sparsevec` con `hnsw.iterative_scan`, `pg_trgm`, `btree_gist`), Huma v2 (OpenAPI 3.1), `mcp-go`, `pgx/v5` + `sqlc`, Goose migrations, motores neuronales locales vía HTTP2/gRPC (`BGE-M3`, `bge-reranker-v2-m3`, `mDeBERTa-v3`, `Qwen2.5-3B-Instruct`).

## 1. Stack canon

1. **Binario único en Go puro (`CGO_ENABLED=0`).** El núcleo de ROMERO se implementa en **Go 1.23+** (fijado en `go.mod` y `go.sum`) y compila con `CGO_ENABLED=0` para Linux, macOS y Windows (`amd64`/`arm64`) sin enlazar librerías `.dll`, `.so` ni `.dylib`. Verificado por `CGO_ENABLED=0 go build ./cmd/romero`.
2. **Frameworks y librerías canónicas fijadas.** La interfaz HTTP usa `net/http` + **Huma v2** (OpenAPI 3.1), el servidor MCP usa **mcp-go** (`stdio` y `Streamable HTTP/SSE`), la CLI usa **Cobra**, las migraciones usan **Goose** y el acceso a datos usa **pgx/v5 (`pgxpool`) + `sqlc`**. Queda prohibido introducir ORMs con reflexión en tiempo de ejecución; cambiar una pieza canónica requiere una enmienda MAJOR.
3. **PostgreSQL 17 + `pgvector 0.8.0+` como única fuente de verdad persistente.** Transacciones relacionales, vectores (`halfvec(1024)` y `sparsevec`), índices `pg_trgm`/`GIN`/`HNSW` con soporte de escaneo iterativo, grafo bitemporal (`memory_relations`) y cola de trabajos (`memory_jobs` con `FOR UPDATE SKIP LOCKED`) residen en **PostgreSQL 17**. Queda prohibido añadir Neo4j, Redis o brokers externos.
4. **Motores neuronales desacoplados por red local y gobernados por perfil.** Todo modelo neuronal (`BGE-M3`, `bge-reranker-v2-m3`, `mDeBERTa-v3`, SLM `3B` REM) se consume a través de las interfaces de `internal/domain/memory/ports.go` vía HTTP2/gRPC local, adaptándose a los perfiles `lite`, `balanced` y `ultra` sin cambiar contratos ni esquema SQL.

## 2. Quality bar

5. **Formato y linter limpios en cada commit.** Todo el código Go debe pasar `gofmt -s -w .`, `go vet ./...` y `golangci-lint run` con cero errores y cero advertencias antes de hacer merge.
6. **Tipado estático estricto y SQL compilado.** Ninguna consulta SQL se concatena manualmente fuera de los archivos `.sql` validados por `sqlc generate`, y los contratos de entrada/salida se validan con esquemas tipados en el borde (Huma v2 / MCP).
7. **Verificación en dos niveles (Rápido en CPU vs. Cognitivo en `romero eval`).**
   - **Nivel 1 — Rápido (obligatorio en cada commit/tarea en perfil `lite` CPU):** Ejecuta `gofmt`, `go vet`, `golangci-lint`, verificación de `sqlc generate` sin diffs y `go test -race ./...` siguiendo TDD (`red → green → refactor`) con cobertura de líneas $\ge 80\%$ en el código modificado de `internal/domain` e `internal/application`.
   - **Nivel 2 — Cognitivo (`romero eval`, obligatorio en cambios del pipeline cognitivo y en `verify` de cierre):** Se ejecuta siempre que un cambio toque `internal/domain/memory/{ranking,bayesian,submodular}.go`, `internal/domain/graph/**`, `internal/application/{retrieval,context,consolidation}/**`, `internal/infrastructure/persistence/queries/**` o `internal/infrastructure/neural/**`, exigiendo cumplir los umbrales de $\text{NDCG}@5$, $\text{Recall}@K$ multi-salto y detección NLI $> 90\%$ sobre `golden_dataset.json`.

## 3. Conventions

8. **Arquitectura limpia con fronteras de paquetes estrictas.** `internal/domain` contiene exclusivamente Go puro sin importar paquetes de `infrastructure`, `interfaces`, base de datos ni clientes HTTP; el flujo de dependencias es siempre `interfaces → application → domain ← infrastructure`.
9. **Contratos API-First, Divulgación Progresiva y tope de 5 herramientas MCP.** La API REST se sirve bajo `/api/v1` y el servidor MCP expone únicamente las **5 herramientas canónicas** (`romero_memory_save`, `romero_memory_search`, `romero_memory_get`, `romero_memory_context`, `romero_memory_feedback`). Las búsquedas devuelven solo vistas compactas (`summary`, estado epistemológico, confianza y conteo de relaciones) y nunca el `content` completo salvo en `get` o `context`.
10. **Formato de commits.** Todo mensaje de commit sigue `gitmoji` + Conventional Commits (ej. `✨ feat(capture): add topic_key epistemic guard`), verificado por `.rsc/gitmoji-guard.mjs`.

## 4. Branching & shipping

11. **Trabajo en ramas y entrega por PR.** La rama principal está cerrada a commits directos (`--workflow branches`); todo cambio de código se desarrolla en una rama dedicada y se integra mediante Pull Request tras superar `verify` y `review`.
12. **La autoría en Git es siempre humana.** Los commits y PRs llevan exclusivamente la autoría de la persona responsable configurada en Git; queda prohibido incluir `Co-Authored-By` de IA o pies de página tipo "generated with". Enforced en la fase `ship`.

## 5. Security & privacy floor

13. **Cero secretos en el repositorio.** Ninguna clave, token o contraseña real se versiona en Git; los secretos se cargan por variables de entorno o desde `01-TOOLS/<provider>/.env` (ignorado en Git). Verificado por `security-audit`.
14. **Aislamiento Zero-Trust en SQL con escaneo iterativo HNSW acotado.** Toda consulta de lectura, búsqueda híbrida o recorrido en Caché L1 filtra obligatoriamente por `tenant_id`, `project_id`, `scope_type` (`agent_private`, `task_shared`, `project_shared`, `org_shared`, `global_public`) e identidad criptográfica derivada de `AuthContext` (`api_key_sha256`). Para evitar caída de recall en scopes pequeños sobre índices `halfvec` HNSW (`pgvector 0.8.0+`), toda transacción de búsqueda vectorial en `pgx/v5` debe configurar `SET LOCAL hnsw.iterative_scan = relaxed_order` y `SET LOCAL hnsw.max_scan_tuples = 20000`. La tasa de fuga entre scopes en `romero eval` debe ser invariablemente $0.00\%$.
15. **Inmutabilidad tri-temporal, procedencia obligatoria y protección epistemológica.** Todo recuerdo exige una fuente en `memory_sources`, registra los 3 relojes (`observed_at`, `created_at`, `[valid_from, valid_until)`) y respeta la jerarquía `verified > observed > reported > inferred > hypothesis` con calibración bayesiana $\text{Beta}(\alpha, \beta)$. Ninguna operación sobrescribe destructivamente un recuerdo previo ni permite que un recuerdo de menor rango epistemológico reemplace a uno activo de mayor rango en la misma `topic_key`.
16. **Defensa contra Prompt Injection.** En escritura se sanitizan secuencias de control/escape y en lectura (`BuildContext` y MCP) todo recuerdo se encapsula en `<romero_memory ... untrusted_content="true">` con sus atributos de procedencia y confianza.

## 6. Performance budgets

17. **Presupuesto de latencia en tiempo real, conexiones calientes y paralelismo.** En el hardware de referencia (Core i5-12400H, 24 GB RAM, RTX 4050 6 GB VRAM), el pipeline completo de búsqueda híbrida + propagación en Caché L1 + Cross-Encoder Reranker debe responder con latencia $p_{95} < 18\text{ ms}$ en perfil `ultra` ($\le 5\text{ ms}$ en acierto de Caché L1 y $\le 14\text{ ms}$ en perfil `lite`). Para sostener ese margen, los clientes de inferencia local deben usar pools HTTP/gRPC persistentes (`keep-alive`, `DisableCompression: true`) precalentados al arranque, y el servicio de recuperación debe ejecutar en paralelo (`errgroup`) la vectorización de la consulta en `BGE-M3` junto con la pre-resolución de semillas en la Caché L1 en RAM.
18. **Presupuesto de VRAM y preempción asíncrona en Sueño REM.** El consumo de VRAM en perfil `ultra` no debe superar $2.3\text{ GB}$ en estados `Guardia`/`Ráfaga` ni $4.5\text{ GB}$ en `Sueño REM` (manteniendo `BGE-M3`, `Reranker` y `DeBERTa` siempre residentes en los $2.3\text{ GB}$ base). Ante cualquier solicitud entrante de un agente durante `Sueño REM`, el `PowerGovernor` debe cancelar inmediatamente el contexto de generación del SLM (`ctx.Cancel()`) en $< 10\text{ ms}$ para ceder el cómputo de los Tensor Cores a la consulta entrante, y despachar en segundo plano la liberación de los $2.2\text{ GB}$ de VRAM del SLM (`keep_alive = 0`) sin bloquear el camino crítico.

## 7. Knowledge & decisions

19. **Registro append-only de decisiones.** Toda decisión arquitectónica o de diseño significativa se registra en `02-DOCS/wiki/sdd/decisions.md` indicando fecha, opciones evaluadas, decisión tomada y tarea asociada. Esta constitución es el registro de decisiones de mayor jerarquía.

## Definition of Done (the merge bar `verify` runs against)

Un cambio solo se aprueba para merge cuando se cumplen TODOS los puntos:

- [ ] Compila con `CGO_ENABLED=0` y pasa `gofmt`, `go vet` y `golangci-lint` sin advertencias (principios 1, 5).
- [ ] Consultas generadas con `sqlc` y contratos OpenAPI 3.1 / MCP sincronizados y tipados (principios 2, 6, 9).
- [ ] Nivel 1 en verde (`go test -race ./...` con cobertura $\ge 80\%$ en `domain`/`application`) y Nivel 2 (`romero eval`) en verde si el cambio toca rutas del pipeline cognitivo o en `verify` de cierre (principio 7).
- [ ] Respeta la arquitectura limpia (`domain` libre de infraestructura) y el límite de 5 herramientas MCP con divulgación progresiva (principios 8, 9).
- [ ] Desarrollado en rama dedicada, commits con `gitmoji` + Conventional Commits y autoría exclusivamente humana (principios 10, 11, 12).
- [ ] Cero secretos commiteados, filtrado Zero-Trust en SQL con `hnsw.iterative_scan = relaxed_order` ($0.00\%$ leakage), invariantes tri-temporales/epistemológicos intactos y encapsulamiento `<romero_memory untrusted_content="true">` (principios 13, 14, 15, 16).
- [ ] Dentro de los presupuestos de latencia ($p_{95} < 18\text{ ms}$ con conexiones `keep-alive` y paralelismo `errgroup`) y preempción asíncrona de VRAM en `Sueño REM` ($< 10\text{ ms}$ cancelación de contexto) (principios 17, 18).
- [ ] Decisiones significativas registradas en `02-DOCS/wiki/sdd/decisions.md` (principio 19).

## Amendment log (append-only)

| Date | Version | Change | Why |
|------|---------|--------|-----|
| 2026-11-11 | draft | Draft generated by `rsc onboard`. | The SDD floor needs a constitution; ratification is the `constitution` phase's. |
| 2026-11-11 | v1.0.0 | Ratificación inicial de la constitución (19 principios) incluyendo verificación en dos niveles, `hnsw.iterative_scan`, paralelismo L1/BGE-M3 y preempción asíncrona de VRAM en Sueño REM. | Establecer la línea base no negociable para el desarrollo de ROMERO en el hardware de referencia. |
