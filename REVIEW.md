# REVIEW.md — Cómo se evalúa el código en ROMERO

> Ratificado junto con `02-DOCS/wiki/sdd/constitution.md` (v1.0.0).
> Todo revisor (`review`, `finding-verifier` y panel `refuter-*`) lee este archivo antes de inspeccionar un diff.

## 1. Formato del veredicto

Toda revisión comienza en su primera línea con el conteo exacto de hallazgos:
`N findings: X blockers, Y nits` (ej. `2 findings: 1 blocker, 1 nit` o `0 findings: 0 blockers, 0 nits`).

- **Tope de nits (`nit cap`):** Máximo **3 nits** por revisión. El resto se descarta.
- Desde la ronda 2 de revisión, evalúa únicamente el nuevo diff y la resolución de los hallazgos previos.

## 2. Qué constituye un fallo crítico (Blocker) en ROMERO

1. **Fuga de aislamiento Multi-Tenant / Multi-Scope o ausencia de escaneo iterativo HNSW (Principio 14):** Cualquier consulta SQL en `internal/infrastructure/persistence/queries/` o recorrido en `internal/infrastructure/cachel1/` que omita el filtro por `tenant_id`, `project_id`, `scope_type` o `owner_agent_id` derivado de `AuthContext`, o cualquier transacción de búsqueda vectorial HNSW que omita `SET LOCAL hnsw.iterative_scan = relaxed_order` y `SET LOCAL hnsw.max_scan_tuples = 20000`.
2. **Violación de inmutabilidad tri-temporal o jerarquía epistemológica (Principio 15):** Un `UPDATE` o `DELETE` destructivo que borre historial sin crear versión en `memory_versions`, que omita `primary_source_id`, que no respete `[valid_from, valid_until)` o que permita que un recuerdo de menor rango epistemológico (`hypothesis`, `inferred`) reemplace a uno `verified`/`observed` en la misma `topic_key`.
3. **Ruptura de `CGO_ENABLED=0` o fronteras de `internal/domain` (Principios 1, 8):** Introducir dependencias C/C++ en el binario Go o importar paquetes de infraestructura/HTTP/SQL dentro de `internal/domain`.
4. **Exposición de contenido sin escudo Anti-Injection o exceso de herramientas MCP (Principios 9, 16):** Devolver recuerdos en `BuildContext` o MCP sin el envoltorio `<romero_memory ... untrusted_content="true">`, devolver `content` completo en `romero_memory_search`, o exponer más de las 5 herramientas MCP canónicas.
5. **Serialización innecesaria en búsqueda o bloqueo síncrono de VRAM en Sueño REM (Principios 17, 18):** Crear clientes HTTP sin pool `keep-alive` persistente hacia los motores locales, no paralelizar (`errgroup`) la consulta L1 con la vectorización `BGE-M3`, invocar el SLM generativo `3B` en el camino crítico síncrono, o esperar síncronamente al `keep_alive = 0` en lugar de cancelar el contexto (`< 10 ms`) y liberar VRAM en segundo plano.
6. **Migración SQL irreversible:** Todo archivo en `migrations/*.sql` debe incluir bloques `-- +goose Up` y `-- +goose Down` simétricos y seguros.

## 3. Rutas que se omiten en la revisión (`Paths to skip`)

- `internal/infrastructure/persistence/sqlc/**` — código Go autogenerado por `sqlc` (se revisan los `.sql` fuente en `queries/` y `migrations/`).
- `.rsc/**`, `.claude/**`, `.codex/**`, `.gemini/**`, `.opencode/**`, `.antigravity/**` — estado y configuración gestionados por el arnés.
- Binarios compilados (`bin/`, `dist/`) y datos de volúmenes locales.

## 4. Rutas que exigen siempre aprobación humana (`Always-human paths` — Tier 2)

Los cambios en cualquiera de estas rutas requieren el panel de 3 refutadores (`correctness`, `security`, `tests`) y **aprobación humana explícita** antes de hacer merge (reflejadas en `.rsc/sello-config.json` bajo `risk.raise`):

- `migrations/**` — esquema físico de PostgreSQL 17, índices `pgvector`/`pg_trgm` y restricciones bitemporales.
- `internal/domain/identity/**` y `internal/interfaces/**/auth*` — autenticación, resolución de `AuthContext`, hashes de API Keys y políticas de scopes Zero-Trust.
- `internal/domain/governor/**` — presupuestos de hardware y máquina de estados `PowerGovernor` (`Guardia`, `Ráfaga`, `Sueño REM`).
- `configs/romero.toml` y `docker-compose.yml` — límites de RAM/VRAM, puertos e infraestructura de contenedores.

