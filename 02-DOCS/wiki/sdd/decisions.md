# SDD Decisions Log (`02-DOCS/wiki/sdd/decisions.md`)

## 2026-11-15 — Enmienda Constitucional `v2.0.0` (Principio 2: Servidor MCP sobre Librería Estándar Go de Alto Rendimiento)

- **Tarea / Fase:** `constitution` + resolución de `analyze` (`phase-04-mcp-server`)
- **Contexto:** La compuerta `analyze` detectó una contradicción entre el Principio 2 original de `02-DOCS/wiki/sdd/constitution.md` (`v1.0.0`, que citaba `mcp-go`) y el plan técnico de Fase 04 (`02-DOCS/wiki/sdd/plans/phase-04-mcp-server.md`, que implementaba el servidor MCP sobre la librería estándar de Go).
- **Opciones consideradas:**
  1. **Ruta A (`mcp-go`):** Añadir `github.com/mark3labs/mcp-go` a `go.mod` y envolver su `SSEServer`, asumiendo doble pasada de deserialización JSON (`bytes -> map[string]any -> struct`, $\approx 0.12\text{–}0.35\text{ ms}$ y $25\text{–}60\text{ allocs/op}$).
  2. **Ruta B (Librería estándar Go + Enmienda MAJOR `v2.0.0`):** Enmendar el Principio 2 de la Constitución a `v2.0.0` para implementar el servidor MCP JSON-RPC 2.0 (`stdio`, `HTTP/SSE` y `Streamable HTTP`) directamente sobre la librería estándar de Go (`net/http`, `encoding/json`, `bufio`).
- **Decisión:** Ruta B (ratificada explícitamente por el usuario).
- **Por qué:** Reduce la latencia de la capa MCP a $\approx 15\text{–}40\text{ }\mu\text{s}$ con una sola pasada de deserialización JSON directo al struct tipado (~8x más rápido y ~10x menos presión sobre el recolector de basura), permite invocar `PowerGovernor.BeginAgentRequest()` en la primera línea antes de decodificar argumentos ($\le 5\text{ }\mu\text{s}$), da control directo sobre la tabla de sesiones SSE Zero-Trust `(sessionID -> TenantID, PrincipalID)` y mantiene `go.mod` con cero dependencias externas adicionales.

## 2026-11-15 — Plan técnico `phase-04-mcp-server` (`02-DOCS/wiki/sdd/plans/phase-04-mcp-server.md`)

- **Tarea / Fase:** `plan` (`phase-04-mcp-server`)
- **Contexto:** Diseño de la arquitectura, contratos entre componentes, flujo de datos, estrategia de pruebas por criterio (`AC-01`..`AC-07`), secuenciación y mitigación de riesgos para el Servidor MCP de 5 herramientas canónicas (`internal/interfaces/mcp`) y la librería cliente exportable (`pkg/romeroclient`).
- **Opciones consideradas:**
  1. Implementar MCP `HTTP/SSE` como un proceso separado de `romero serve` y compartir tipos internos de `internal/domain` en el cliente Go.
  2. Implementar el adaptador MCP (`internal/interfaces/mcp`) con JSON-RPC 2.0 sobre la librería estándar (`stdio` + `HTTP/SSE` `GET /sse`, `POST /message` vinculado a `(TenantID, PrincipalID)` de la sesión, y `POST /mcp`), integrándolo dentro de `romero serve` (`:8421` según `mcp_sse_port`) para compartir en RAM la misma instancia de `cachel1.CognitiveGraph` y `governor.PowerGovernor` con REST (`:8420`) —además del subcomando autónomo `romero mcp`—, reforzar `RetrievalService.GetMemory` para filtrar `Relations` verificando `authorizeRead` sobre ambos extremos de cada arista, y construir `pkg/romeroclient` con cero importaciones de `internal/` verificado por prueba estática de AST.
- **Decisión:** Opción 2.
- **Por qué:** Garantiza coherencia instantánea ($0\text{ ms}$) de la Caché Cognitiva L1 *write-through* y preempción en $< 10\text{ ms}` del `context.CancelFunc` de Sueño REM entre llamadas REST y MCP (`AC-06`), cierra cualquier vector de fuga de identificadores por relaciones o secuestro de sesión SSE (`AC-02`, `AC-05`), y asegura que el repositorio externo de **Gandalf Wizard** pueda importar `github.com/romero-project/romero/pkg/romeroclient` sin restricciones del compilador Go (`AC-07`).

## 2026-11-15 — Clarificación (`clarify`) de `phase-04-mcp-server`

- **Tarea / Fase:** `clarify` (`phase-04-mcp-server`)
- **Contexto:** Cierre de brechas de la especificación `02-DOCS/wiki/sdd/specs/phase-04-mcp-server.md` tras recorrer la lista de verificación de `clarify` sobre las cuatro preguntas ASDD y los límites fuera de alcance.
- **Opciones consideradas y decisiones adoptadas:**
  1. **Coordinación de Caché L1 en RAM y `PowerGovernor` entre `romero serve` y MCP `HTTP/SSE` (Q1):** `romero serve` levanta tanto el servidor REST (`:8420`) como el listener MCP `HTTP/SSE` (`mcp_sse_port`, `:8421` por defecto) en el mismo proceso compartiendo la misma instancia en RAM de la Caché Cognitiva L1 (`cachel1.GraphCache`), del `PowerGovernor` y de los trabajadores en segundo plano, además de mantener el subcomando `romero mcp` para sesiones `stdio` locales (por defecto) y `sse` autónomo.
  2. **Alcance de `romero_memory_save`:** Mapea directamente a `CaptureMemory` (incluyendo supersesión automática por `topic_key` cuando el rango epistemológico lo permite y aristas `relations`), manteniendo las mutaciones explícitas por identificador (`UpdateMemory`, `SupersedeMemory`, `ArchiveMemory`) en REST y en `pkg/romeroclient` para respetar el tope estricto de 5 herramientas MCP (Principio 9).
  3. **Validación de las 4 suposiciones tomadas (`validated (held)`):**
     - `romero mcp` soporta `--transport stdio|sse` (`stdio` por defecto con `--api-key` / `ROMERO_API_KEY`) y `romero serve` expone MCP `HTTP/SSE` en `mcp_sse_port` (`:8421`).
     - `romero_memory_get` admite hasta `20` identificadores en `memory_ids[]` (error de validación si está vacío sin `memory_id` o si supera `20`).
     - Errores de negocio (`422`, `403`, `409`, `404`, `503`) en las 5 herramientas MCP se devuelven como errores estructurados de herramienta (`validation_error`, `forbidden`, `conflict`, `not_found`, `service_unavailable`) sin cerrar la sesión de transporte.
     - `pkg/romeroclient` no importa ningún paquete bajo `internal/` para permitir su importación limpia desde el repositorio externo de Gandalf Wizard.

## 2026-11-15 — Especificación funcional `phase-04-mcp-server` (Servidor MCP de 5 Herramientas Canónicas, Divulgación Progresiva y Transporte Dual)

- **Tarea / Fase:** `specify` (`phase-04-mcp-server`)
- **Contexto:** Definición del contrato funcional (QUÉ y POR QUÉ) para la Fase 04 (Sprint 5) de ROMERO, resolviendo 5 decisiones de frontera con el usuario antes de redactar `02-DOCS/wiki/sdd/specs/phase-04-mcp-server.md`.
- **Opciones consideradas y decisiones adoptadas:**
  1. **Modo de ejecución ASDD:** Flujo con aprobación humana por fase (*gated*: `specify` -> `clarify` -> `plan` -> `tasks` -> `analyze` -> `implement`).
  2. **Alcance funcional de Fase 04 (actualizado a Opción A):** Exponer tanto el Servidor MCP (`stdio` y `HTTP/SSE`) con las 5 herramientas canónicas (`romero_memory_save`, `romero_memory_search`, `romero_memory_get`, `romero_memory_context`, `romero_memory_feedback`) como la librería cliente pública exportable (`pkg/romeroclient`, libre de dependencias a `internal/`) en este repositorio para que el proyecto independiente Gandalf Wizard pueda importarla directamente con `go get`.
  3. **Autenticación Zero-Trust y preempción en transportes MCP:** En modo `stdio`, autenticar una sola vez al iniciar mediante `--api-key` / `ROMERO_API_KEY` asociando el `AuthContext` a la sesión del proceso; en modo `HTTP/SSE`, exigir cabecera `Authorization: Bearer <key>` o `X-API-Key` en cada solicitud resolviendo el `AuthContext` por petición; en ambos transportes, toda invocación de herramienta activa `BeginAgentRequest()` en el `PowerGovernor` para interrumpir cualquier ciclo de `rem_sleep` en $< 10\text{ ms}$.
  4. **Contrato de `romero_memory_get` (Fase 2 de Divulgación Progresiva):** Aceptar tanto un `memory_id` individual como un lote acotado `memory_ids[]` ($1..20$) con soporte opcional *Point-in-Time* (`as_of` o `valid_at` + `known_at`), devolviendo el `content` envuelto en `<romero_memory ... untrusted_content="true">`, el historial de versiones y las aristas causales visibles, y reportando como `not_found` opaco (con auditoría `denied`) cualquier ID inexistente o fuera del ámbito legible sin abortar el resto del lote.
  5. **Recibos opacos en escrituras sin lectura (`romero_memory_save` y `romero_memory_feedback`):** Cuando un agente con permiso de solo-escritura en `task_shared` invoca `romero_memory_save` o `romero_memory_feedback`, devolver una respuesta estructurada con `status = "accepted"` y el `request_id` opaco, preservando la opacidad Zero-Trust establecida en la Fase 01.
- **Por qué:** Cumple el Principio 9 de la Constitución (tope estricto de 5 herramientas MCP con divulgación progresiva) manteniendo el servidor MCP como un adaptador delgado sobre los casos de uso existentes bajo aislamiento Zero-Trust ($0.00\%$ de fuga) y preempción inmediata del Sueño REM.

## 2026-11-15 — Plan técnico `phase-03-nli-governor-rem` (`02-DOCS/wiki/sdd/plans/phase-03-nli-governor-rem.md`)

- **Tarea / Fase:** `plan` (`phase-03-nli-governor-rem`)
- **Contexto:** Diseño de la arquitectura, contratos entre componentes, flujo de datos, estrategia de pruebas por criterio (`AC-01`..`AC-08`), secuenciación y mitigación de riesgos para la Fase 03 (`NLI`, `ConflictService`, `PowerGovernor` y `REMConsolidationService`).
- **Opciones consideradas:**
  1. Ejecutar el ciclo completo de consolidación de Sueño REM de un proyecto dentro de una sola transacción SQL y descargar sincrónicamente la VRAM del modelo generativo antes de atender peticiones de agentes.
  2. Ejecutar la inferencia SLM y la validación NLI cruzada fuera de cualquier transacción SQL bajo un `context.Context` gobernado por `PowerGovernor` (cancelable en $< 10\text{ ms}` vía `BeginAgentRequest()` con descarga asíncrona de VRAM en una goroutine separada), abrir una transacción ACID corta únicamente por clúster validado tras verificar `remCtx.Err() == nil`, filtrar el vecindario NLI y los episodios REM estrictamente por la partición exacta `(tenant_id, project_id, scope_type, owner_agent_id, task_id)`, y cerrar automáticamente las aristas `contradicts` huérfanas al archivar o superceder un recuerdo.
- **Decisión:** Opción 2.
- **Por qué:** Garantiza el presupuesto de preempción en $< 10\text{ ms}` (Principio 18) y de latencia en tiempo real $p_{95} < 18\text{ ms}` (Principio 17) sin retener bloqueos de fila durante la inferencia, preserva $0.00\%$ de fuga entre ámbitos (`AC-04`, `AC-07`), evita conflictos huérfanos en `ListConflicts` (`AC-05`) y no requiere migraciones destructivas sobre el esquema SQL fundacional.

## 2026-11-15 — Clarificación (`clarify`) de `phase-03-nli-governor-rem`

- **Tarea / Fase:** `clarify` (`phase-03-nli-governor-rem`)
- **Contexto:** Cierre de brechas de la especificación `02-DOCS/wiki/sdd/specs/phase-03-nli-governor-rem.md` tras recorrer la lista de verificación de `clarify` sobre las cuatro preguntas ASDD y los límites fuera de alcance.
- **Opciones consideradas y decisiones adoptadas:**
  1. **Autorización de `TriggerREM` (`POST /api/v1/admin/consolidate` — `AC-07`):** Se autoriza tanto a principales administrativos (`is_admin = true`, sobre cualquier proyecto del inquilino) como a principales de tipo `orchestrator` con permiso de escritura sobre el `project_id` solicitado, rechazando con `403 Forbidden` (`denied` en auditoría) a agentes ordinarios (`agent`) o principales sin acceso de escritura al proyecto.
  2. **Validación de las 4 suposiciones tomadas + cierre automático de conflictos huérfanos y `romero eval` determinista en CPU (`AC-05`, `AC-08`):** Se confirman (a) `conflict_id` como UUID de la relación activa `contradicts`, (b) refuerzo bayesiano $+\Delta\alpha = 1.0$ y versión inmutable opcional del ganador en `ReviewConflict` (exigiendo lectura y escritura síncronas sin cola opaca), (c) persistencia vectorial con `nli_checked = false` cuando NLI no está activo/disponible, (d) parámetros `project_id` y `dry_run` en `TriggerREM`/`romero consolidate`, (e) cierre automático de aristas `contradicts` (y restauración a `active` del par si no tiene otros conflictos ni cuarentena bayesiana) cuando un extremo se archiva o supercede fuera de `ReviewConflict`, y (f) evaluación determinista en CPU dentro de `romero eval`.
- **Por qué:** Elimina la contradicción interna entre `Users & context` y `Error paths` sobre quién invoca `TriggerREM`, impide que contradicciones obsoletas permanezcan activas en `ListConflicts` y deja `phase-03-nli-governor-rem.md` en estado `clarified` lista para `plan`.

## 2026-11-15 — Especificación funcional `phase-03-nli-governor-rem` (NLI, Arbitraje de Conflictos, Gobernador de Potencia y Consolidación en Sueño REM)

- **Tarea / Fase:** `specify` (`phase-03-nli-governor-rem`)
- **Contexto:** Definición del contrato funcional (QUÉ y POR QUÉ) para la Fase 03 de ROMERO, resolviendo 6 decisiones de frontera con el usuario en dos rondas antes de redactar `02-DOCS/wiki/sdd/specs/phase-03-nli-governor-rem.md`.
- **Opciones consideradas y decisiones adoptadas:**
  1. **Modo de ejecución ASDD:** Flujo con aprobación humana por fase (*gated*: revisión en `spec` -> `clarify` -> `plan` -> `tasks` -> `analyze` -> `implement`).
  2. **Alcance funcional de Fase 03:** Unificar los Sprints 3 y 4 de la Especificación Maestra en la Fase 03 — Reordenamiento Cross-Encoder (`bge-reranker-v2-m3`), Detector NLI de soporte y contradicciones (`mDeBERTa-v3`), listado y arbitraje de conflictos (`ListConflicts`, `ReviewConflict`), `PowerGovernor` (`guard` / `burst` / `rem_sleep` con preempción en $< 10\text{ ms}`), Consolidación en Sueño REM (`TriggerREM`, `romero consolidate`) y métricas NLI/REM en `romero eval` — completando los 13 endpoints REST canónicos y dejando el servidor MCP (Sprint 5) para la Fase 04.
  3. **Frontera Zero-Trust en inferencia lógica automática (NLI):** La comparación y creación automática de aristas `supports` y `contradicts` opera exclusivamente dentro de la misma partición de ámbito efectiva (nunca cruza `agent_private` de distintos agentes ni `task_shared` de distintas tareas), garantizando $0.00\%$ de fuga en `ListConflicts` y en alertas de contradicción.
  4. **Regla de autoridad ante contradicción NLI entre distintas `topic_key`:** Cuando se detecta contradicción ($P(\text{Contradiction}) \ge 0.80$), pasa a estado `conflicted` únicamente el recuerdo de menor rango epistemológico (en empate, el de menor confianza bayesiana $\hat{\theta}$, y si persiste el empate, el recién evaluado), sin degradar nunca automáticamente un recuerdo `verified`.
  5. **Autorización y efecto de `ReviewConflict`:** Cualquier principal con lectura y escritura vigente en el ámbito de ambos recuerdos puede resolver el conflicto (exigiendo principal administrativo si el recuerdo que resultaría perdedor es `verified`); en una sola transacción el ganador queda `active`, el perdedor pasa a `superseded` con `valid_until` cerrado, se desactiva la arista `contradicts` y se crea una arista activa `supersedes`.
  6. **Aislamiento de ámbito y preempción en Consolidación de Sueño REM:** Los episodios se agrupan estrictamente dentro de la misma partición de ámbito (heredando ese ámbito en el recuerdo destilado), se validan con NLI cruzado ($\ge 0.80$ de implicación respecto a las fuentes y cero contradicciones con recuerdos `verified`), y si el ciclo es interrumpido por preempción ($< 10\text{ ms}$) o los motores no están disponibles, los episodios fuente permanecen intactos en estado `active`.
- **Por qué:** Cierra el ciclo completo de los 4 motores cognitivos y los 13 endpoints REST canónicos de ROMERO bajo aislamiento Zero-Trust estricto y respeto a los presupuestos de latencia y VRAM de los Principios 17 y 18.

## 2026-11-15 — Plan técnico `phase-02-hybrid-retrieval` (`02-DOCS/wiki/sdd/plans/phase-02-hybrid-retrieval.md`)

- **Tarea / Fase:** `plan` (`phase-02-hybrid-retrieval`)
- **Contexto:** Diseño de la arquitectura, contratos entre componentes, flujo de datos, estrategia de pruebas por criterio (`AC-01`..`AC-12`), secuenciación y mitigación de riesgos para la Fase 02 (Perfil `Balanced`).
- **Opciones consideradas:**
  1. Ejecutar *Spreading Activation* sobre el grafo completo en RAM y filtrar permisos al construir la respuesta final, exigiendo `INNER JOIN` con `memory_embeddings` en las consultas de búsqueda.
  2. Proyectar en la Caché Cognitiva L1 un subgrafo podado por permisos Zero-Trust (`AuthContext` + `task_permissions` vigentes) *antes* de entregar la lista de adyacencia al motor matemático puro (`internal/domain/graph`), ejecutar en paralelo (`errgroup`) la vectorización `BGE-M3` y la consulta tri-modal (`pg_trgm`, `fts_doc`, `halfvec` HNSW con `SET LOCAL hnsw.iterative_scan = relaxed_order`), sincronizar la Caché L1 *write-through* tras cada mutación ACID y degradar limpiamente a $\text{RRF}$ léxico/exacto + diversidad Jaccard cuando falten vectores (`degraded_mode = true`).
- **Decisión:** Opción 2.
- **Por qué:** Garantiza $0.00\%$ de fuga indirecta entre scopes a través de caminos multi-salto (`AC-03`), mantiene coherencia inmediata tras supersesiones/archivados (`AC-06`), preserva el presupuesto de latencia $p_{95} < 18\text{ ms}$ (Principio 17) y asegura disponibilidad inmediata tras captura incluso antes de que finalice el trabajo asíncrono `embed_and_nli` (`AC-08`, `AC-09`), sin requerir migraciones destructivas sobre el esquema SQL de Fase 01.

## 2026-11-15 — Clarificación (`clarify`) de `phase-02-hybrid-retrieval`

- **Tarea / Fase:** `clarify` (`phase-02-hybrid-retrieval`)
- **Contexto:** Cierre de brechas de la especificación `02-DOCS/wiki/sdd/specs/phase-02-hybrid-retrieval.md` tras recorrer la lista de verificación de `clarify` sobre las cuatro preguntas ASDD y los límites fuera de alcance.
- **Opciones consideradas y decisiones adoptadas:**
  1. **Declaración de aristas causales explícitas en `CaptureMemory` (`AC-11`):** En lugar de añadir un 14.º endpoint REST fuera del catálogo canónico (§14.1), `CaptureMemory` (`POST /api/v1/memories`) acepta un campo opcional `relations` (`depends_on`, `caused_by`, `supports`, `derived_from`, `related_to`) validando visibilidad de lectura Zero-Trust sobre cada `to_memory_id` (`404 Not Found` opaco auditado si no existe o es ajeno; `422` si usa tipos reservados `supersedes`/`contradicts`).
  2. **Lecturas puras sin bloqueo sobre `memories` (`AC-12`):** `SearchMemories`, `BuildContext` y `GetSubgraph` no mutan `access_count` ni `last_accessed_at` (escriben únicamente en `audit_events`); el incremento de uso permanece en `RecordFeedback` (`retrieved` / `used_in_context`).
  3. **Validación de las 4 suposiciones tomadas:** Se confirman (a) los rangos y defaults de `limit` ($1..100$, def $20$), `hops` ($0..3$ / $1..3$, def $2$) y `max_tokens` ($64..32768$, def $4000$), (b) la expansión automática de `scopes` autorizados cuando se omiten (+ `task_shared` si incluye `task_id` legible), (c) la similitud de Jaccard sobre etiquetas/entidades/resumen cuando `degraded_mode = true`, y (d) el recorrido bidireccional sobre aristas vigentes visibles con desempate determinista (`observed_at DESC, id ASC`).
- **Por qué:** Elimina brechas de creación de relaciones causales y contención de bloqueos en lecturas concurrentes sin inflar la superficie REST, dejando `phase-02-hybrid-retrieval.md` en estado `clarified` lista para `plan`.

## 2026-11-15 — Especificación funcional `phase-02-hybrid-retrieval` (Recuperación Híbrida, Grafo Causal y Empaquetado de Contexto)

- **Tarea / Fase:** `specify` (`phase-02-hybrid-retrieval`)
- **Contexto:** Definición del contrato funcional (QUÉ y POR QUÉ) para la Fase 02 de ROMERO (Perfil `Balanced`), resolviendo 5 decisiones de frontera con el usuario antes de redactar `02-DOCS/wiki/sdd/specs/phase-02-hybrid-retrieval.md`.
- **Opciones consideradas y decisiones adoptadas:**
  1. **Modo de ejecución ASDD:** Flujo con aprobación por fase (*gated*: revisión humana en `spec` -> `clarify` -> `plan` -> `tasks` -> `implement`).
  2. **Alcance funcional de Fase 02:** Incluir `SearchMemories`, `BuildContext` y `GetSubgraph` ($1\text{–}3$ saltos) junto con el procesamiento asíncrono priorizado de representaciones vectoriales; diferir el detector NLI de contradicciones (`mDeBERTa-v3`, `ListConflicts`, `ReviewConflict`) y el Cross-Encoder (`bge-reranker-v2-m3`) a la Fase 03.
  3. **Degradación elegante sin vector disponible (`degraded_mode`):** Cuando un recuerdo recién capturado aún tiene su vectorización pendiente en cola o el proveedor de embeddings no está disponible (ej. perfil `Lite` o caída local), `SearchMemories` y `BuildContext` responden sin caer usando coincidencia exacta, léxica y grafo causal, calculan diversidad sobre metadatos/términos e informan `degraded_mode = true`.
  4. **Aislamiento Zero-Trust durante propagación multi-salto:** Poda estricta pre-propagación: ningún nodo ni arista fuera del `AuthContext` y `task_permissions` vigentes del solicitante recibe ni transmite energía, ni se contabiliza en `relations_count` o `GetSubgraph` ($0.00\%$ de fuga directa e indirecta).
  5. **Elegibilidad de estados del ciclo de vida:** Por defecto solo participan recuerdos vigentes en estado `active` y `conflicted` (con penalización de estado en `conflicted` y bandera de alerta de contradicción); `superseded` y `consolidated` solo aparecen con flag explícito en `SearchMemories`; `archived` y `rejected` quedan siempre excluidos.
- **Por qué:** Cierra el alcance del Perfil `Balanced` manteniendo disponibilidad continua ante la naturaleza asíncrona de la cola de vectorización y blindando el aislamiento Zero-Trust a través de caminos multi-salto.

## 2026-11-14 — Correcciones verificadas de T001/T002

El usuario autorizó corregir directamente en main el desbordamiento tras sanitizar, la representación persistida de permisos y el cierre de trazabilidad. Seguimiento: Issue #16. El límite se comprueba antes y después de clean; se rechaza el exceso, sin truncar. La migración nueva `00002_principal_authorization.sql` añade `is_admin=false`, ámbitos vacíos por defecto y una restricción de nombres de ámbito válidos sin elementos NULL. No se modifica la migración T001 entregada ni se transforma read/write en permisos distintos. Datos previos incompatibles abortan la migración transaccional para revisión del operador. T004 usará el siguiente número libre para task_permissions; T005 deberá mapear is_admin desde persistencia.

Reversión: Down de 00002 elimina is_admin y su información; restaura el default histórico read/write y retira la restricción, sin reescribir los scopes explícitos existentes. No ejecutar esa reversión sobre una base con uso real sin conservar antes las concesiones administrativas. Ninguna migración se aplicó a una base del usuario durante esta corrección; las comprobaciones usan contenedores desechables.

## 2026-11-14 — Aprobación de contratos técnicos de tarea

El usuario aprobó el puerto de permisos, la cola durable, el uso de reservas con token, la transacción de mutación y resultado, la ruta de consulta propuesta y la regla de que solicitudes opacas sin clave de idempotencia son independientes. Fuente: [plan de autorización](plans/phase-01-task-authorization-draft.md). La aprobación no reemplaza la puerta de análisis ni la revisión de implementación.

## 2026-11-14 — Escrituras opacas asíncronas (Phase 01)

Contexto — Una escritura sin lectura no debe revelar si un ID o conflicto existe ni afirmar falsamente que se aplicó. Decisión humana: aceptación durable `202` con identificador opaco para toda escritura autorizada sin lectura; reautorización en el trabajador, estado legible solo con permiso vigente, tres reintentos y retención configurable. El usuario aprobó incorporar esta ampliación y la ruta REST de estado. La idempotencia opaca se separa de la captura normal y se limita a tenant y principal. La [enmienda de especificación](specs/phase-01-task-authorization-draft.md) y su [plan](plans/phase-01-task-authorization-draft.md) recogen los límites; firmas y análisis quedan pendientes.

## 2026-11-13 — Autorización explícita para task_shared (Phase 01)

Contexto — La revisión de T002 detectó que conocer un task_id no prueba autorización. Opciones consideradas: acceso por proyecto, credencial por tarea o asignaciones del servidor. Decisión humana: asignaciones verificadas en PostgreSQL, lectura y escritura independientes, comprobadas por el servicio usando la identidad autenticada y el recurso; administración mediante bootstrap/CLI con auditoría transaccional. Enmiendas: [spec](specs/phase-01-task-authorization-draft.md) y [plan](plans/phase-01-task-authorization-draft.md). El usuario aprobó las reglas y el diseño para integrarlos. La implementación y el análisis siguen pendientes.

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

## 2026-11-13 — Diferir generación sqlc hasta T004 (feature: phase-01-foundation, tasks: T001/T004)

- Contexto: sqlc v1.31.1 ejecutado realmente falla porque el directorio de consultas aún no existe. El plan asigna las consultas y su generación a T004; T001 crea la configuración base.
- Opciones: mantener T001 pendiente hasta T004; o aceptar explícitamente el control diferido a T004 sin modificar la configuración ni crear consultas ficticias.
- Decisión humana: el usuario respondió «Sí, diferir sqlc a T004» a la solicitud de autorización de este control para cerrar T001 cuando terminen revisión y entrega.
- Por qué: respeta el alcance del plan y mantiene la generación SQL como condición de cierre obligatoria de T004. El comando fallido sigue registrado, no se informa como aprobado.
- Entorno: se usa PostgreSQL desechable separado del servicio Compose para validar la misma migración sin afectar la sesión concurrente. Las pruebas race usan GCC en contenedor; el binario de producto sigue compilándose con CGO_ENABLED=0.

## 2026-11-14 — Reconciliación del contrato de T002 con T004 y T011

- T004 corrige la frontera de unicidad por inquilino en una migración posterior. Ante duplicados históricos, la migración revierte y conserva los registros para que el operador los resuelva.
- La consulta vigente de permisos define el momento de autorización. El trabajador vuelve a comprobar antes de mutar y confirma los efectos solo si conserva una reserva válida.
- AC-T7 a AC-T12 reúnen las decisiones aprobadas sobre aceptación, claves, ejecución, estado y retención. T002 entrega política y puerto de dominio. El adaptador PostgreSQL, HTTP y el trabajador tienen tareas y pruebas propias. [Plan de la enmienda](plans/phase-01-task-authorization-draft.md).

## 2026-11-14 — Cierre de DQ-02 y decisiones de T004 (`sqlc`, `pgx v5.9.2`, `clock_timestamp()`, `goose`)

- **`DQ-02` cerrado (`sqlc generate`):** Las consultas tipadas en `internal/infrastructure/persistence/queries/*.sql` y el código generado en `internal/infrastructure/persistence/sqlcgen/*` compilan limpiamente sin diffs residuales tras `a9d35ac`.
- **Actualización de `github.com/jackc/pgx/v5` a `v5.9.2`:** Impuesta por `govulncheck` para resolver `GO-2026-5004` detectado en `v5.7.4`; `govulncheck ./...` reporta `0` vulnerabilidades afectando al código.
- **Uso de `clock_timestamp()` en `opaque_requests.sql`:** En transacciones multi-sentencia `READ COMMITTED` (`OpaqueRequestStore.Execute`), `NOW()` permanece congelado en el inicio de la transacción (`transaction_timestamp()`); usar `clock_timestamp()` garantiza cercado efectivo ante expiración de lease ocurrida durante el callback `apply`.
- **Migración `00003` reversible con pre-check de duplicados:** Adquiere `SHARE ROW EXCLUSIVE`, aborta antes de reconstruir `idx_memories_active_topic_key` si existen duplicados activos en `org_shared`/`global_public` para permitir resolución manual por el operador, y revierte limpiamente en `-- +goose Down`.
