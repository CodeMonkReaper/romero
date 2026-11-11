# SDD Decisions Log (`02-DOCS/wiki/sdd/decisions.md`)

Registro append-only de las decisiones arquitectónicas y de especificación tomadas en **ROMERO**.

---

## 2026-11-11 — Constitución del proyecto ROMERO v1.0.0 (en ratificación) y adopción de ADRs 001–012

- **Tarea / Fase:** `constitution` (Fase Base de ASDD)
- **Contexto:** Inicialización formal de los principios no negociables de ROMERO a partir de la Especificación Maestra v2.0.0 (`romero_gandalf_wizard_especificaci_n_maestra_de_arquitectura_producto_e_ingenier_a.md`).
- **Opciones consideradas:**
  1. Mantener principios genéricos de Go/PostgreSQL sin ligar los invariantes cognitivos, bitemporales y presupuestos de hardware.
  2. Incorporar los 12 ADRs de la Especificación Maestra como 19 principios numerados, verificables y falsables en `02-DOCS/wiki/sdd/constitution.md` junto con las reglas de revisión en `REVIEW.md` y rutas Tier 2 en `.rsc/sello-config.json`.
- **Decisión:** Opción 2. Se consolidan los 19 principios cubriendo Stack Canon (`Go 1.23+ CGO_ENABLED=0`, `Huma v2`, `mcp-go`, `pgx/v5 + sqlc`, `PostgreSQL 17 + pgvector halfvec + pg_trgm`), Quality Bar (TDD con cobertura $\ge 80\%$ en `domain`/`application`, cero warnings de linter), Convenciones (arquitectura limpia, tope de 5 herramientas MCP con divulgación progresiva, commits con `gitmoji`), Seguridad Zero-Trust ($0.00\%$ cross-scope leakage, jerarquía epistemológica $\text{Beta}(\alpha,\beta)$, inmutabilidad tri-temporal, escudo XML anti-injection) y Presupuestos de Rendimiento ($p_{95} < 18\text{ ms}$, límite de VRAM y preempción de Sueño REM en $< 50\text{ ms}$).
- **Por qué:** Garantiza que `analyze`, `verify` y `review` puedan auditar objetivamente cada tarea frente a los invariantes cognitivos y de rendimiento de ROMERO.


---

## 2026-11-11 — Refinamiento operativo de los Bloques 2, 5 y 6 y ratificación de la Constitución v1.0.0 (Principios 3, 7, 14, 17 y 18)

- **Tarea / Fase:** `constitution` (Fase Base de ASDD — ratificación v1.0.0)
- **Contexto:** Evaluación y aprobación de cuatro salvaguardas operativas en el hardware objetivo (Core i5-12400H, 24 GB RAM, RTX 4050 6 GB VRAM): (1) lentitud de CI si `romero eval` corre en cambios no cognitivos, (2) colapso de recall en HNSW al aplicar filtros Zero-Trust sobre scopes pequeños en tablas de $>250\text{k}$ vectores, (3) margen estrecho ($0.6\text{ ms}$) frente al SLA $p_{95} < 18\text{ ms}$ si las etapas corren en serie, y (4) bloqueo superior a $50\text{ ms}$ si la descarga de VRAM (`keep_alive = 0`) del SLM en Sueño REM se ejecuta de forma síncrona en el camino crítico.
- **Opciones consideradas:**
  1. Mantener la redacción original de los principios 7, 14, 17 y 18 tratando estos puntos solo como detalles de implementación.
  2. Incorporar las cuatro salvaguardas directamente en los principios constitucionales 3, 7, 14, 17 y 18 como reglas verificables en código y revisión, y ratificar `v1.0.0`.
- **Decisión:** Opción 2.
  - **Principio 7:** Se divide la verificación en Nivel 1 Rápido (CPU, cada commit/tarea) y Nivel 2 Cognitivo (`romero eval`, obligatorio al tocar rutas de ranking, grafo, SQL de búsqueda, empaquetado submodular o adaptadores neuronales, y en `verify` de cierre).
  - **Principios 3 y 14:** Se fija `pgvector 0.8.0+` y se exige `SET LOCAL hnsw.iterative_scan = relaxed_order` con `SET LOCAL hnsw.max_scan_tuples = 20000` en las transacciones de búsqueda `pgx/v5` para preservar el recall con $0.00\%$ de fuga entre scopes.
  - **Principio 17:** Se exige pool HTTP/gRPC `keep-alive` precalentado hacia los motores locales y paralelización con `errgroup` entre la vectorización `BGE-M3` y la consulta inicial a la Caché L1 en RAM.
  - **Principio 18:** Se exige preempción en dos tiempos en `Sueño REM`: cancelación síncrona del contexto HTTP del SLM (`< 10 ms`) para liberar inmediatamente los Tensor Cores (dado que `BGE-M3`, `Reranker` y `DeBERTa` ya residen en los $2.3\text{ GB}$ base) y descarga asíncrona de los $2.2\text{ GB}$ de VRAM (`keep_alive = 0`) fuera del camino crítico.
- **Por qué:** Convierte cuatro modos de fallo reales de rendimiento y recall en restricciones de diseño auditables por `analyze`, `verify` y `review` sin alterar los invariantes del dominio, dejando ratificada la Constitución `v1.0.0`.
