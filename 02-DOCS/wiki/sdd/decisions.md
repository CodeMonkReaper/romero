# SDD Decisions Log (`02-DOCS/wiki/sdd/decisions.md`)

## 2026-11-13 — Representaciones de dominio T002

Contexto — El plan fija las operaciones públicas, pero deja abiertas representaciones auxiliares.
Opciones — Grants como slices o mapas; identidad de agente opcional o UUID cero; estimación de tokens local o dependencia de tokenizer.
Decisión — Grants como slices, AgentID opcional mediante puntero, estimación de tokens como techo de caracteres Unicode sanitizados dividido por cuatro. MemoryScopeTarget.SameTopicPartition expresa la frontera de unicidad de §0 sin acceso a persistencia.
Motivo — Mantiene dominio Go puro y contratos explícitos. TokenCount es una estimación, no una tokenización de modelo. La dependencia google/uuid v1.6.0 prevista por T002 se incorpora aquí porque T001 no la declaró.


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


---

## 2026-11-11 — Especificación funcional `phase-01-foundation` (Gobernanza Zero-Trust y Persistencia Tri-Temporal)

- **Tarea / Fase:** `specify` (`phase-01-foundation`)
- **Contexto:** Definición del contrato funcional (QUÉ y POR QUÉ) para la Fase 01 de ROMERO cubriendo el aislamiento multiagente Zero-Trust ($0.00\%$ de fuga), los tres relojes temporales, la captura idempotente, la supersesión por `topic_key` y la jerarquía bayesiana $\text{Beta}(\alpha,\beta)$, resolviendo 10 ramas de decisión mediante entrevista `/grill-me`.
- **Opciones consideradas:**
  1. Limitar la Fase 01 a captura y lectura simple sin consultas *Point-in-Time* ni recalibración bayesiana por feedback.
  2. Cubrir el ciclo completo de gobernanza y vida del recuerdo (`CaptureMemory`, `GetMemory` actual y *Point-in-Time*, `UpdateMemory`, `SupersedeMemory`, `ArchiveMemory` y `RecordFeedback`) fijando los invariantes de seguridad, temporalidad y epistemología.
- **Decisión:** Opción 2, incorporando las siguientes reglas contractuales aprobadas en el diálogo de diseño:
  1. **Alcance de operaciones:** Ciclo completo de gobernanza y vida del recuerdo en `02-DOCS/wiki/sdd/specs/phase-01-foundation.md`.
  2. **Resolución Principio 15 vs. §11.2 en recuerdos `verified`:** En captura normal por `topic_key`, ningún rango $< 5$ puede superceder un recuerdo `verified` (se persiste como `conflicted` con relación `contradicts`); el reemplazo de un `verified` obsoleto por un hecho `observed` ($\text{Rank}=4$) exige la operación explícita `SupersedeMemory` ejecutada por un principal `user` u `orchestrator` con motivo obligatorio.
  3. **Idempotencia estricta por huella:** Reutilizar `idempotency_key` dentro del mismo inquilino con idéntico `content_sha256` y `scope_type` devuelve `200 OK`; si difieren, devuelve `409 Conflict`.
  4. **Opacidad anti-enumeración Zero-Trust ($0.00\%$ fuga):** El acceso o mutación por UUID sobre un recuerdo fuera del ámbito visible responde `404 Not Found` y registra `denied` en auditoría; la escritura en un ámbito/proyecto sin permiso responde `403 Forbidden`.
  5. **Particionamiento de `topic_key` activa por propietario real del ámbito:** La unicidad de `topic_key` activa se particiona por `(tenant_id, project_id, owner_agent_id, topic_key)` en `agent_private` y por `(tenant_id, project_id, task_id, topic_key)` en `task_shared`, evitando colisiones o fugas entre agentes o tareas distintas.
  6. **Consultas *Point-in-Time* bitemporales:** Soporte de `as_of` o del par separado `valid_at` (vigencia `[valid_from, valid_until)`) y `known_at` (`created_at <= known_at`) para auditoría histórica sin contaminación retroactiva.
  7. **Autoridad para estado `verified`:** Restringido a principales `user`, `orchestrator` o `service`; los agentes estándar tienen tope en `observed` ($\text{Rank}=4$).
  8. **Cuarentena bayesiana automática:** Si tras señales negativas la esperanza $\hat{\theta} = \frac{\alpha}{\alpha+\beta}$ cae por debajo de $0.30$, el recuerdo pasa automáticamente de `active` a `conflicted` y registra nueva versión inmutable.
  9. **Sanitización determinista anti-inyección:** Eliminación de caracteres de control/Unicode Bidi invisibles y escape de etiquetas `<romero_memory>` / `</romero_memory>` antes de calcular `content_sha256`, con encapsulamiento XML `untrusted_content="true"` en lectura.
- **Por qué:** Cierra todas las ambigüedades entre la Constitución `v1.0.0` y la Especificación Maestra antes de pasar a `clarify` y `plan`.


---

## 2026-11-11 — Clarificación (`clarify`) de `phase-01-foundation`

- **Tarea / Fase:** `clarify` (`phase-01-foundation`)
- **Contexto:** Cierre de brechas de la especificación `02-DOCS/wiki/sdd/specs/phase-01-foundation.md` antes de pasar a `plan`.
- **Opciones consideradas:**
  1. Exponer endpoints HTTP REST administrativos para crear tenants, proyectos, agentes y API keys (`principals`).
  2. Mantener intacta la superficie de 13 endpoints REST de la Especificación Maestra (§14.1) y aprovisionar identidades mediante bootstrap declarativo y subcomando CLI administrativo local.
- **Decisión:** Opción 2 (`AC-10`), más validación explícita de las tres reglas de borde: obligatoriedad de `task_id` en `task_shared` (`422`), cuarentena bayesiana síncrona ($\hat{\theta} < 0.30 \rightarrow \text{conflicted}$) en `RecordFeedback`, y rechazo con `409 Conflict` al intentar mutar o volver a superceder recuerdos ya cerrados (`superseded`, `archived`, `rejected`).
- **Por qué:** Elimina brechas de aprovisionamiento e inmutabilidad sin inflar la superficie HTTP y deja la especificación en estado `clarified` lista para `plan`.


---

## 2026-11-11 — Plan técnico `phase-01-foundation` (`02-DOCS/wiki/sdd/plans/phase-01-foundation.md`)

- **Tarea / Fase:** `plan` (`phase-01-foundation`)
- **Contexto:** Diseño de la arquitectura, contratos, modelo de datos, estrategia de pruebas y secuenciación para los 10 criterios de aceptación de `phase-01-foundation`.
- **Opciones consideradas:**
  1. Coordinar la unicidad de `topic_key` y el filtrado de permisos en la capa Go mediante mutexes en memoria y filtrado post-consulta.
  2. Imponer la unicidad de `topic_key` activa mediante un índice único parcial en PostgreSQL 17 particionado por propietario de ámbito (`owner_agent_id` en `agent_private`, `task_id` en `task_shared`, `project_id` en `project_shared`, `tenant_id` en `org_shared`/`global_public`) combinado con bloqueo transaccional `SELECT ... FOR UPDATE`, reconstrucción *Point-in-Time* desde `memory_versions` (`valid_from`/`valid_until` + `created_at`) y escritura independiente de `audit_events(outcome = 'denied')` fuera del rollback de negocio.
- **Decisión:** Opción 2.
- **Por qué:** Garantiza atomicidad ACID ante escrituras concurrentes, preserva $0.00\%$ de fuga entre ámbitos sin colisiones de `topic_key` privadas/de tarea, asegura que las auditorías `denied` nunca se pierdan en un rollback y permite reconstruir con exactitud las consultas históricas `as_of` / `(valid_at, known_at)`.


---

## 2026-11-13 — Refinamiento técnico de `plan` y `tasks` (`phase-01-foundation`) vía `/grill-me`

- **Tarea / Fase:** `plan` + `tasks` (`phase-01-foundation`)
- **Contexto:** Resolución de 6 decisiones técnicas de detalle sobre el esquema de `memory_versions`, encadenamiento de ventanas de validez en `UpdateMemory`, formato de credenciales, contrato de `corrected_by_user`, entorno de pruebas con PostgreSQL 17 y estrategia de ejecución de tareas.
- **Opciones consideradas y decisiones adoptadas:**
  1. **Esquema autocontenido en `memory_versions` (`T001`, `T004`):** Añadir `valid_from`, `valid_until`, `tags`, `entities`, `metadata`, `importance` y `utility_score` a `memory_versions` para reconstruir instantáneas *Point-in-Time* completas (`AC-07`).
  2. **Ventanas de validez encadenadas por versión en `UpdateMemory` (`T003`, `T006`, `T007`):** Al actualizar $v \rightarrow v+1$ en $t_u$, la versión $v$ cierra su `valid_until = t_u` en `memory_versions` y la versión $v+1$ se abre con `valid_from = t_u, valid_until = NULL`.
  3. **Formato de API Key y cabeceras HTTP (`T005`, `T008`):** Generar llaves `rmr_<tipo>_<id8>_<secreto32>` (prefijo de 20 caracteres en `api_key_prefix` + comparación en tiempo constante `crypto/subtle` de `api_key_sha256`), aceptando tanto `Authorization: Bearer <key>` como `X-API-Key: <key>`.
  4. **Soporte dual de `corrected_by_user` en `RecordFeedback` (`T007`, `T008`):** Permitir un objeto opcional `replacement_memory` en `POST /api/v1/memories/feedback` para ejecutar en una sola transacción atómica la penalización $\beta + 4.0$ y la supersesión del recuerdo corregido.
  5. **Entorno de pruebas de integración (`T001`, `T004`, `T009`):** Ejecutar contra el contenedor `romero-postgres` (`pgvector/pgvector:pg17` en `127.0.0.1:54329`) de `docker-compose.yml` con limpieza `TRUNCATE ... CASCADE` por test.
  6. **Ejecución secuencial (`T001` $\rightarrow$ `T010`):** Ejecutar las tareas secuencialmente en la rama `feat/phase-01-foundation` con un commit verificado por tarea.
- **Por qué:** Elimina cualquier ambigüedad en los contratos entre tareas antes de ejecutar la compuerta `analyze` y comenzar `implement`.

---

## 2026-11-13 — Expresiones `CASE` parentizadas en `idx_memories_active_topic_key` y `audit_events.tenant_id` anulable para `401` (feature: `phase-01-foundation`, task: `T001`)

- **Contexto:** Durante el panel Tier 2 de `T001` (`refuter-correctness`, `refuter-security`, `refuter-tests`), se detectó que (1) la regla gramatical `index_elem` de PostgreSQL 17 exige parentizar las expresiones `CASE WHEN ... END` dentro de `CREATE UNIQUE INDEX`, y (2) `audit_events.tenant_id` declarado `NOT NULL` impediría persistir los eventos `audit_events(outcome = 'denied', operation = 'authenticate')` exigidos por el contrato cuando una petición `401 Unauthorized` llega sin credencial o con una API key desconocida que no resuelve a ningún `tenant_id`.
- **Opciones consideradas:**
  1. Crear un tenant centinela ficticio para los intentos `401` sin credencial válida y usar funciones SQL auxiliares para el índice único.
  2. Permitir `tenant_id UUID REFERENCES tenants(id) ON DELETE CASCADE` (anulable, igual que `principal_id` y `agent_id`) en `audit_events` y envolver cada expresión `CASE WHEN ... END` entre paréntesis `(CASE WHEN ... END)` en `idx_memories_active_topic_key`.
- **Decisión:** Opción 2 (aplicada en commit `078645f`, verificada en Ronda 2 con 0 hallazgos).
- **Por qué:** Satisface la gramática nativa de PostgreSQL 17 y `sqlc` sin funciones auxiliares y permite auditar cualquier intento de intrusión `401` sin contaminar la tabla `tenants` con registros centinela.
