---
type: plan
title: Plan — Phase 04 Servidor MCP de Memoria Cognitiva y Librería Cliente Exportable para Gandalf Wizard
description: The structure-level implementation plan for phase-04-mcp-server — contracts, shapes, flows, testing strategy, sequencing and risks.
tags: [sdd, plan, romero, phase-04, mcp, sdk, progressive-disclosure, zero-trust, power-governor]
timestamp: 2026-11-15T17:34:00-03:00
topic: sdd
slug: phase-04-mcp-server
status: approved
---

# Plan — Phase 04 Servidor MCP de Memoria Cognitiva y Librería Cliente Exportable para Gandalf Wizard

> Spec: [../specs/phase-04-mcp-server.md](../specs/phase-04-mcp-server.md) · Constitution: [../constitution.md](../constitution.md) · Status: approved
> Last updated: 2026-11-15

## 0. Global Constraints

- **Lenguaje y compilación (`constitution §1 #1`):** Go `1.23+` estricto con `CGO_ENABLED=0` obligatorio en compilación del binario (`CGO_ENABLED=0 go build -trimpath ./cmd/romero`). Cero enlaces a librerías `.dll`, `.so` o `.dylib`.
- **Stack canónico (`constitution §1 #2, #3, #4`):** HTTP REST con `net/http` + `Huma v2` (OpenAPI 3.1) bajo `/api/v1`; Servidor MCP con JSON-RPC 2.0 sobre `stdio` y `HTTP/SSE` (`GET /sse`, `POST /message` y `POST /mcp`) con `net/http` de la librería estándar; CLI con `Cobra`; migraciones con `Goose`; acceso a datos con `pgx/v5` (`pgxpool`) + consultas compiladas estáticamente por `sqlc` sobre `PostgreSQL 17`. Prohibido usar ORMs con reflexión o concatenar SQL fuera de `.sql` compilados por `sqlc`.
- **Fronteras de arquitectura limpia y visibilidad de paquetes (`constitution §3 #8`, `spec §Acceptance AC-07`, `spec §Clarifications`):**
  - `internal/domain` (`identity`, `memory`, `graph`, `governor`, `provenance`, `security`) contiene exclusivamente Go puro de la librería estándar (y `github.com/google/uuid`), sin importar `infrastructure`, `interfaces`, drivers SQL ni clientes HTTP.
  - Dirección de dependencias interna invariante: `interfaces → application → domain ← infrastructure`.
  - El paquete público exportable `pkg/romeroclient` **no importa ningún paquete bajo `internal/`** (solo librería estándar de Go y `github.com/google/uuid`), permitiendo que cualquier repositorio externo —en particular el proyecto independiente **Gandalf Wizard**— lo importe limpiamente mediante `go get github.com/romero-project/romero/pkg/romeroclient`.
- **Calidad y verificación en dos niveles (`constitution §2 #5, #6, #7`):**
  - **Nivel 1 (obligatorio en cada tarea):** Cero errores y cero advertencias en `gofmt -s -w .`, `go vet ./...`, `golangci-lint run` y `sqlc generate` sin diffs; `go test -race ./...` siguiendo TDD (`red → green → refactor`) con cobertura de líneas $\ge 80\%$ en `internal/domain` e `internal/application`.
  - **Nivel 2 (`romero eval`, obligatorio en cierre):** Ejecución reproducible en CPU sobre `tests/evaluation/golden_dataset.json` verificando $\text{NDCG}@5 \ge 0.85$, $\text{Recall}@K$ multi-salto $\ge 0.85$, tasa de detección de contradicciones NLI $> 0.90$, ratio de destilación REM $> 0.0$, supresión de obsolescencia $1.0$ y tasa de fuga entre ámbitos exactamente $0.00\%$.
- **Tope estricto de 5 herramientas MCP y divulgación progresiva en dos fases (`constitution §3 #9`, `spec §Acceptance AC-01, AC-02`):**
  - El servidor MCP publica única y exclusivamente las **5 herramientas canónicas** en `tools/list`:
    1. `romero_memory_save`
    2. `romero_memory_search`
    3. `romero_memory_get`
    4. `romero_memory_context`
    5. `romero_memory_feedback`
  - Está terminantemente prohibido registrar una sexta herramienta en el servidor MCP.
  - **Fase 1 (`romero_memory_search`):** Devuelve exclusivamente vistas compactas (`id`, `summary`, `type`, `scope_type`, `status`, `epistemic_status`, `confidence`, `final_score`, `relations_count`, `min_hop_depth`, `has_conflict_alert`, `observed_at`) junto con `degraded_mode` y `total_found`, omitiendo siempre el cuerpo `content`. Soporta filtros opcionales de `project_id`, `scopes`, `task_id`, `memory_types`, `min_confidence`, `hops` ($0..3$), `limit` ($1..100$), `include_superseded` e `include_consolidated`.
  - **Fase 2 (`romero_memory_get`):** Acepta `memory_id` (UUID individual) y/o `memory_ids` (arreglo de $1..20$ UUIDs; si se envían ambos se unifican sin duplicados manteniendo el orden de aparición y exigiendo entre $1$ y $20$ identificadores únicos en total), además de parámetros opcionales *Point-in-Time* (`as_of`, o el par `valid_at` + `known_at`). Para cada UUID legible devuelve el recuerdo con su cuerpo envuelto en `<romero_memory id="..." type="..." epistemic="..." confidence="..." source="..." untrusted_content="true">...</romero_memory>`, sus parámetros bayesianos $(\alpha, \beta)$, sus versiones inmutables y únicamente las relaciones causales activas cuyos **dos extremos** (`from_memory_id` y `to_memory_id`) sean legibles para el solicitante; para cada UUID inexistente o fuera del ámbito de lectura del solicitante devuelve un elemento individual `{ "memory_id": "<uuid>", "status": "not_found" }` opaco y registra `audit_events(outcome = 'denied')` sin abortar el resto del lote.
- **Aislamiento Zero-Trust, recibos opacos y manejo de errores en MCP (`constitution §5 #14, #15, #16`, `spec §Acceptance AC-03, AC-04, AC-05`):**
  - **Prohibición de credenciales en argumentos de herramienta:** Ninguna de las 5 herramientas acepta llaves de API, `tenant_id`, `principal_id` ni `agent_id` del llamador en sus argumentos JSON; el `identity.AuthContext` proviene exclusivamente de la sesión autenticada (`stdio`) o de la cabecera HTTP de la solicitud (`HTTP/SSE`).
  - **Transporte `stdio` (`romero mcp --transport stdio`, por defecto):** Exige credencial al arrancar (`--api-key` o `ROMERO_API_KEY`), autentica una sola vez contra `Authenticator.Authenticate(ctx, rawKey)` antes de procesar el bucle JSON-RPC, y si falla registra `denied` en auditoría y termina con código de salida distinto de cero.
  - **Transporte `HTTP/SSE` (`romero serve` en `mcp_sse_port = 8421` y `romero mcp --transport sse`):** Exige cabecera `Authorization: Bearer <key>` o `X-API-Key` en cada solicitud HTTP (`GET /sse`, `POST /message`, `POST /mcp`). Si falta o es inválida/expirada, registra `denied` en auditoría y responde HTTP `401 Unauthorized`. Cuando un cliente abre `GET /sse`, la sesión creada queda vinculada criptográficamente a `(TenantID, PrincipalID)` de esa credencial; cualquier `POST /message?sessionId=<id>` posterior verifica que el `AuthContext` de la petición coincida con el propietario de la sesión (`403 Forbidden` + auditoría `denied` en caso de discrepancia) y ejecuta la llamada con el `AuthContext` recién autenticado de la petición.
  - **Escrituras opacas (`task_shared` write-only en `romero_memory_save` y `romero_memory_feedback`):** Cuando el servicio subyacente devuelve `Result.Accepted == true`, la herramienta MCP devuelve un resultado exitoso con `{"status": "accepted", "request_id": "<uuid>"}` sin filtrar existencia previa ni estado de la tarea.
  - **Errores de negocio en herramientas MCP (`spec §Points to clarify #3`):** Los fallos de dominio (`422` validación, `403` permiso denegado o violación de rango epistemológico `verified`/`corrected_by_user`, `409` conflicto de idempotencia/versión, `404` recurso no encontrado, `503` dependencia no disponible) se devuelven dentro de `tools/call` como `CallToolResult{ IsError: true }` con carga JSON estructurada `{"error": {"code": "validation_error|forbidden|conflict|not_found|service_unavailable", "message": "..."}}` sin cerrar ni derribar la sesión de transporte `stdio` o `HTTP/SSE`.
- **Preempción de Sueño REM en $< 10\text{ ms}$ y compartición en RAM (`constitution §6 #17, #18`, `spec §Acceptance AC-06`, `spec §Clarifications Q1`):**
  - Toda invocación `tools/call` en el servidor MCP ejecuta `PowerGovernor.BeginAgentRequest()` al entrar y `defer PowerGovernor.EndAgentRequest()` al salir, cancelando síncronamente en $< 10\text{ ms}$ cualquier contexto activo de `rem_sleep` y despachando en segundo plano la liberación de VRAM del SLM.
  - `romero serve` levanta en el mismo proceso tanto el servidor REST (`:8420` por defecto, `--listen` / `ROMERO_LISTEN_ADDR`) como el servidor MCP `HTTP/SSE` (`:8421` por defecto según `mcp_sse_port`, configurable por `--mcp-listen` / `ROMERO_MCP_LISTEN_ADDR`, o deshabilitable si se fija vacío/`off`), compartiendo la misma instancia en RAM de `cachel1.CognitiveGraph`, `governor.PowerGovernor` y los trabajadores asíncronos (`requests.Worker`, `jobs.EmbeddingWorker`, `runIdleREMScheduler`).
- **Git y autoría (`constitution §3 #10, §4 #11, #12`):** Trabajo en ramas dedicadas con Pull Request, mensajes de commit con `gitmoji` + Conventional Commits y autoría exclusivamente humana (prohibido `Co-Authored-By`).

## 1. Context & constraints

- **Criterios de aceptación que gobiernan el diseño:**
  - Catálogo estricto e invariante de las 5 herramientas canónicas en `tools/list`: `spec §Acceptance AC-01`, `constitution §3 #9`.
  - Divulgación progresiva en dos fases (`romero_memory_search` compacto sin `content`, y `romero_memory_get` individual o por lote $1..20$ con sobre XML `untrusted_content="true"`, versiones, relaciones visibles con ambos extremos autorizados y opacidad `not_found` por ítem con auditoría `denied`): `spec §Acceptance AC-02`.
  - Captura (`romero_memory_save`) y retroalimentación bayesiana (`romero_memory_feedback`) con soporte de recibos opacos `status = "accepted"` + `request_id` para permisos de solo-escritura en `task_shared`: `spec §Acceptance AC-03`.
  - Empaquetado submodular de contexto XML seguro en `romero_memory_context` respetando `max_tokens` y $0.00\%$ de fuga: `spec §Acceptance AC-04`.
  - Autenticación Zero-Trust diferenciada en `stdio` (al arranque) y `HTTP/SSE` (por petición y vinculada a sesión), integrada en `romero serve` (`:8421`) y autónoma en `romero mcp`: `spec §Acceptance AC-05`, `spec §Clarifications Q1`.
  - Preempción de `rem_sleep` en $< 10\text{ ms}` ante cualquier llamada MCP y coherencia de Caché L1 en RAM entre REST y MCP dentro de `romero serve`: `spec §Acceptance AC-06`.
  - Librería cliente pública exportable `pkg/romeroclient` (cero imports de `internal/`) cubriendo las 14 operaciones de ROMERO y distinción de recibos `202 Accepted` para consumo directo desde el repositorio externo de Gandalf Wizard: `spec §Acceptance AC-07`.
- **Barras no funcionales:** Preempción de Sueño REM $< 10\text{ ms}$ (`constitution §6 #18`); búsqueda híbrida $p_{95} < 18\text{ ms}$ (`constitution §6 #17`); $0.00\%$ de fuga entre ámbitos (`constitution §5 #14`); binario único con `CGO_ENABLED=0` (`constitution §1 #1`).
- **Fuera de alcance (`spec §Non-goals`):** Motor de orquestación propio de Gandalf Wizard (vive en su repositorio externo); exposición de una sexta herramienta MCP para administración/conflictos/REM; envío de credenciales dentro de los argumentos de negocio de las herramientas.

## 2. Architecture

```text
+--------------------------------------------------------------------------------------------------------+
|                              EXTERNAL CONSUMERS (Outside ROMERO Process)                               |
|                                                                                                        |
|  +----------------------------------------+          +----------------------------------------------+  |
|  | Local / Remote Agents (EXTERNAL)       |          | Gandalf Wizard Repo (EXTERNAL Go Module)     |  |
|  | - Local Agent via MCP stdio            |          | Imports:                                     |  |
|  | - Remote Agent via MCP HTTP/SSE (:8421)|          | github.com/romero-project/romero/            |  |
|  +-------------------+--------------------+          |   pkg/romeroclient (INTERNAL Public Library) |  |
+----------------------|-------------------------------+-----------------------|----------------------+--+
                       | JSON-RPC 2.0 (stdio / SSE / POST)                     | HTTP/1.1 REST (/api/v1/*)
                       v                                                       v
+--------------------------------------------------------------------------------------------------------+
|                              ROMERO Single Static Binary (Go 1.23+, CGO_ENABLED=0)                     |
|                                                                                                        |
|  +------------------------------------------------+  +----------------------------------------------+  |
|  | MCP Server Adapter (INTERNAL)                  |  | REST API Adapter (INTERNAL - Huma v2 :8420)  |  |
|  | internal/interfaces/mcp                        |  | internal/interfaces/rest                     |  |
|  | - Stdio Runner (startup AuthContext)           |  | - 13 OpenAPI 3.1 endpoints + /healthz        |  |
|  | - HTTP/SSE & Streamable HTTP Handler (:8421)   |  | - Per-request AuthMiddleware                 |  |
|  | - Strictly 5 Canonical Tools                   |  +-----------------------+----------------------+  |
|  +------------------------+-----------------------+                          |                         |
|                           |                                                  |                         |
|                           +------------------------+-------------------------+                         |
|                                                    |                                                   |
|                                                    v                                                   |
|                           +--------------------------------------------------+                         |
|                           | Shared PowerGovernor Preemption Gate (INTERNAL)  |                         |
|                           | BeginAgentRequest() / EndAgentRequest() (<10ms)  |                         |
|                           +------------------------+-------------------------+                         |
|                                                    |                                                   |
|                                                    v                                                   |
|  +--------------------------------------------------------------------------------------------------+  |
|  | Application Layer & Shared In-RAM State (INTERNAL)                                               |  |
|  | - CaptureService        (CaptureMemory + L1 write-through + opaque write enqueue)                |  |
|  | - RetrievalService      (SearchMemories w/ Type+MinConf filters, GetMemory w/ visible relations) |  |
|  | - ContextBuilderService (BuildContext submodular greedy XML packaging)                           |  |
|  | - FeedbackService       (RecordFeedback Bayesian calibration + quarantine + opaque enqueue)      |  |
|  | - ConflictService & ConsolidationService + Shared L1 CognitiveGraph Cache                        |  |
|  +-------------------------------------------------+------------------------------------------------+  |
|                                                    |                                                   |
|                                                    v                                                   |
|  +--------------------------------------------------------------------------------------------------+  |
|  | Infrastructure Layer (INTERNAL): PostgreSQL 17 (pgxpool + sqlc) & Local Neural Clients           |  |
|  +--------------------------------------------------------------------------------------------------+  |
+--------------------------------------------------------------------------------------------------------+
```

- **MCP Server Adapter (`internal/interfaces/mcp`)** (INTERNAL) — Implementa el protocolo MCP (JSON-RPC 2.0 `initialize`, `notifications/initialized`, `ping`, `tools/list`, `tools/call`) sobre dos transportes (`stdio` y `HTTP/SSE` + `POST /mcp`), exponiendo únicamente las 5 herramientas canónicas, resolviendo `identity.AuthContext` en la frontera de transporte e invocando `PowerGovernor.BeginAgentRequest()` en cada llamada.
- **CLI MCP & Dual-Listener Serve (`internal/interfaces/cli` + `cmd/romero`)** (INTERNAL) — Añade el subcomando `romero mcp` (`--transport stdio|sse`, `--listen`, `--api-key`) y extiende `romero serve` (`--mcp-listen`) para levantar el listener MCP `HTTP/SSE` en `:8421` compartiendo en memoria `cachel1.CognitiveGraph`, `PowerGovernor` y los workers con el servidor REST (`:8420`).
- **Retrieval Visible-Relations & Filter Enhancement (`internal/application/retrieval` + `internal/interfaces/rest`)** (INTERNAL) — Asegura que `GetMemory` (tanto en modo actual como *Point-in-Time*) devuelva `Versions` y filtre `Relations` verificando `authorizeRead` sobre ambos extremos de cada arista, y que `SearchQuery` soporte filtros opcionales `MemoryTypes` y `MinConfidence` en MCP y REST.
- **Exportable Go Client Library (`pkg/romeroclient`)** (INTERNAL — paquete público exportable) — Provee un cliente HTTP tipado, seguro para concurrencia y libre de dependencias a `internal/` que encapsula las 14 operaciones del servidor ROMERO, el manejo de recibos opacos `202 Accepted` y la decodificación de errores RFC 9457 para que el proyecto externo **Gandalf Wizard** lo consuma directamente.

**Top architectural decision:** Construir el adaptador MCP (`internal/interfaces/mcp`) directamente sobre la librería estándar (`net/http`, `encoding/json`, `bufio`) e integrarlo en el mismo proceso de `romero serve` compartiendo la instancia en RAM de `cachel1.CognitiveGraph` y `governor.PowerGovernor` con REST (`:8420`), además de ofrecer `romero mcp` para ejecución `stdio`/`sse` dedicada, manteniendo `pkg/romeroclient` como paquete público desacoplado de `internal/`.
- *Alternativa descartada:* Ejecutar MCP `HTTP/SSE` exclusivamente como un proceso separado de `romero serve` o acoplar `pkg/romeroclient` a tipos de `internal/domain`.
- *Justificación (`spec §Clarifications Q1`, `spec §Acceptance AC-06, AC-07`, `constitution §6 #17, #18`):* Si MCP `HTTP/SSE` corriera en otro proceso separado de `romero serve`, una escritura por REST (`:8420`) no actualizaría *write-through* la Caché L1 del proceso MCP (`:8421`) y una llamada MCP no podría cancelar en $< 10\text{ ms}$ el `context.CancelFunc` en memoria de un ciclo de Sueño REM iniciado por el planificador de `romero serve`. Al compartir el proceso en `romero serve`, la coherencia L1 y la preempción REM son instantáneas en RAM; y al mantener `pkg/romeroclient` en `pkg/` sin imports de `internal/`, el módulo externo de Gandalf Wizard puede importarlo sin violar las reglas de visibilidad del compilador Go.

## 3. Interfaces & contracts

### 3.1 Mejoras en `RetrievalService` (`internal/application/retrieval`)

```text
retrieval.SearchQuery:
  - Query: string (1..2048 runes)
  - ProjectIDs: []UUID (optional)
  - Scopes: []ScopeType (optional)
  - TaskID: string (optional)
  - MemoryTypes: []MemoryType (optional; filters candidates to matching cognitive types)
  - MinConfidence: float32 (optional, in [0.0, 1.0]; filters candidates with Confidence >= MinConfidence)
  - Limit: int (default 10, max 100)
  - Hops: int (default 2, range 0..3)
  - IncludeSuperseded: bool
  - IncludeConsolidated: bool
  - CorrelationID: string

retrieval.Service.GetMemory(ctx, auth: AuthContext, query: GetMemoryQuery) -> MemoryDetail | ErrUnauthorized | ErrValidation | ErrNotFound | ErrOperational
  - invariant: in both current and Point-in-Time modes, loads Versions (filtered to recorded_at <= knownAt when Point-in-Time) and active Relations touching MemoryID where BOTH endpoints (FromMemoryID and ToMemoryID) pass authorizeRead(ctx, auth, peerMemory).
  - invariant: any relation whose peer memory is unreadable by `auth` is silently omitted from `MemoryDetail.Relations` (0.00% cross-scope relation leakage).
```

### 3.2 Adaptador del Servidor MCP (`internal/interfaces/mcp`)

```text
mcp.ServerDeps:
  - Auth: Authenticator (Authenticate(ctx, rawKey) -> AuthContext | error)
  - Capture: CaptureService (CaptureMemory)
  - Retrieval: RetrievalService (SearchMemories, GetMemory)
  - Context: ContextService (BuildContext)
  - Feedback: FeedbackService (RecordFeedback)
  - Governor: GovernorPreemptor (BeginAgentRequest(), EndAgentRequest())
  - ServerName: string (default "romero-mcp")
  - ServerVersion: string

mcp.Server.ListTools() -> [5]ToolDefinition
  - postcondition: returns strictly ["romero_memory_save", "romero_memory_search", "romero_memory_get", "romero_memory_context", "romero_memory_feedback"] with JSON Schema (draft 2020-12 / MCP inputSchema) definitions.

mcp.Server.ServeStdio(ctx, rawAPIKey: string, in: io.Reader, out: io.Writer) -> error
  - precondition: rawAPIKey != "" and Auth.Authenticate(ctx, rawAPIKey) succeeds once at startup; otherwise audit `denied` (inside Authenticator) and return authentication error before reading JSON-RPC messages.
  - invariant: binds the verified AuthContext to all JSON-RPC requests in the stdio stream.
  - invariant: every `tools/call` invokes Governor.BeginAgentRequest() and defers Governor.EndAgentRequest().

mcp.Server.HTTPHandler() -> http.Handler
  - Exposes:
    - `GET /sse`: Authenticates header (`Authorization: Bearer <key>` or `X-API-Key`), creates an SSE session bound to `(sessionID, auth.TenantID, auth.PrincipalID)`, emits `event: endpoint\ndata: /message?sessionId=<id>\n\n`, and streams JSON-RPC responses as `event: message\ndata: <json>\n\n`.
    - `POST /message?sessionId=<id>`: Authenticates header on every request, verifies caller `(TenantID, PrincipalID)` matches the SSE session owner (`403 Forbidden` if mismatched), executes JSON-RPC request under caller's fresh `AuthContext`, pushes the JSON-RPC response over the `GET /sse` stream, and responds HTTP `202 Accepted`.
    - `POST /mcp` (Streamable HTTP JSON-RPC): Authenticates header on every request and returns the JSON-RPC response directly in the HTTP `200 OK` response body (`202 Accepted` for JSON-RPC notifications without `id`).
  - error mode: missing/invalid API key on any HTTP endpoint -> HTTP `401 Unauthorized` (with `denied` recorded in `audit_events` by `Authenticator`).
```

### 3.3 Contratos de las 5 Herramientas Canónicas MCP (`tools/call`)

```text
Tool 1: romero_memory_save(args: SaveToolInput) -> SaveToolOutput | ToolError
  - input: { project_id?: UUID, scope_type: ScopeType, task_id?: string, type: MemoryType, summary: string, content: string, epistemic_status?: EpistemicStatus, topic_key?: string, tags?: []string, entities?: []string, importance?: float32, idempotency_key?: string, relations?: []RelationDraft }
  - output (immediate): { status: "created", memory_id: UUID, version_number: int, memory_status: MemoryStatus, epistemic_status: EpistemicStatus, confidence: float32, sanitized: bool, idempotent_replay: bool }
  - output (write-only task_shared): { status: "accepted", request_id: UUID }

Tool 2: romero_memory_search(args: SearchToolInput) -> SearchToolOutput | ToolError
  - input: { query: string, project_id?: UUID, project_ids?: []UUID, scopes?: []ScopeType, task_id?: string, memory_types?: []MemoryType, min_confidence?: float32, hops?: int (0..3), limit?: int (1..100), include_superseded?: bool, include_consolidated?: bool }
  - output: { candidates: []SearchCandidate (strictly compact, NO content field), degraded_mode: bool, total_found: int }

Tool 3: romero_memory_get(args: GetToolInput) -> GetToolOutput | ToolError
  - input: { memory_id?: UUID, memory_ids?: []UUID, as_of?: RFC3339, valid_at?: RFC3339, known_at?: RFC3339 }
  - precondition: 1 <= len(dedup(memory_id + memory_ids)) <= 20; else ToolError(code="validation_error").
  - output: { items: []GetToolItem }
    where each GetToolItem is either:
    - { memory_id: UUID, status: "found", memory: MemoryView, untrusted_xml_envelope: string, point_in_time_version?: MemoryVersionView, versions: []MemoryVersionView, relations: []MemoryRelationView }
    - { memory_id: UUID, status: "not_found" } (opaque when missing or unauthorized; `denied` recorded in audit).

Tool 4: romero_memory_context(args: ContextToolInput) -> ContextToolOutput | ToolError
  - input: { query: string, max_tokens?: int (128..32768, default 2000), project_id?: UUID, project_ids?: []UUID, scopes?: []ScopeType, task_id?: string, hops?: int (1..3, default 2) }
  - output: { xml_document: string, total_tokens: int, memories_count: int, degraded_mode: bool, selected_ids: []UUID }

Tool 5: romero_memory_feedback(args: FeedbackToolInput) -> FeedbackToolOutput | ToolError
  - input: { memory_id: UUID, signal: FeedbackSignal, task_id?: string, reason?: string, replacement_memory?: ReplacementMemoryInput }
  - output (immediate): { status: "recorded", feedback_id: UUID, memory_id: UUID, new_alpha: float32, new_beta: float32, new_confidence: float32, utility_score: float32, new_status: MemoryStatus, superseded_by_id?: UUID }
  - output (write-only task_shared): { status: "accepted", request_id: UUID }
```

### 3.4 Librería Cliente Pública Exportable (`pkg/romeroclient`)

```text
package romeroclient (zero imports from internal/*)

romeroclient.NewClient(cfg: Config{ BaseURL: string, APIKey: string, HTTPClient?: *http.Client, Timeout?: time.Duration }) -> (*Client, error)

Methods on *Client (all send `Authorization: Bearer <APIKey>` and optional `X-Correlation-ID`, and return `*APIError` on 4xx/5xx):
  1. CaptureMemory(ctx, req: CaptureRequest) -> (*CaptureResponse, error)
     - Handles both HTTP 201 Created (`Accepted=false`, `Memory != nil`) and HTTP 202 Accepted (`Accepted=true`, `RequestID != nil`).
  2. GetMemory(ctx, memoryID: UUID, opts?: *GetMemoryOptions{ AsOf, ValidAt, KnownAt }) -> (*MemoryDetailResponse, error)
  3. UpdateMemory(ctx, memoryID: UUID, req: UpdateRequest) -> (*MutationResponse, error)
  4. SupersedeMemory(ctx, memoryID: UUID, req: SupersedeRequest) -> (*SupersedeResponse, error)
  5. ArchiveMemory(ctx, memoryID: UUID, req: ArchiveRequest) -> (*MutationResponse, error)
  6. RecordFeedback(ctx, memoryID: UUID, req: FeedbackRequest) -> (*FeedbackResponse, error)
     - Handles both HTTP 200 OK (`Accepted=false`) and HTTP 202 Accepted (`Accepted=true`, `RequestID != nil`).
  7. GetRequestStatus(ctx, requestID: UUID) -> (*RequestStatusResponse, error)
  8. SearchMemories(ctx, req: SearchRequest) -> (*SearchResponse, error)
  9. BuildContext(ctx, req: ContextRequest) -> (*ContextResponse, error)
  10. GetSubgraph(ctx, memoryID: UUID, hops: int) -> (*SubgraphResponse, error)
  11. ListConflicts(ctx, filter: ConflictFilter) -> (*ConflictListResponse, error)
  12. ReviewConflict(ctx, conflictID: UUID, req: ReviewConflictRequest) -> (*ReviewConflictResponse, error)
  13. TriggerREM(ctx, req: TriggerREMRequest) -> (*TriggerREMResponse, error)
  14. GetSystemStatus(ctx) -> (*SystemStatusResponse, error)
```

## 4. Data model & flow

**Entities**

- **`mcp.ToolDefinition` / `mcp.CallToolRequest` / `mcp.CallToolResult`** — Estructuras de protocolo JSON-RPC 2.0 (`2024-11-05`) para descubrimiento e invocación de las 5 herramientas canónicas sin acoplar el dominio.
- **`mcp.GetToolItem`** — Proyección por elemento devuelta por `romero_memory_get` que discrimina `status: "found"` (con `untrusted_xml_envelope`, `versions` y `relations` visibles) frente a `status: "not_found"` opaco.
- **`romeroclient.Client` y DTOs públicos (`pkg/romeroclient`)** — Contratos públicos de transporte REST para consumidores externos como Gandalf Wizard, incluyendo `APIError` (`StatusCode`, `Title`, `Detail`) y soporte explícito de recibos `202 Accepted`.

**Primary flow 1 — Divulgación Progresiva en 2 Fases sobre MCP (`romero_memory_search` → `romero_memory_get`)**

1. Un agente conectado por `stdio` o `HTTP/SSE` invoca `tools/call` con `name = "romero_memory_search"` y `{ "query": "timeout en pool postgres", "hops": 2, "limit": 5 }`.
2. `mcp.Server` verifica el `AuthContext` de la sesión/petición, invoca `PowerGovernor.BeginAgentRequest()` (cancelando cualquier ciclo `rem_sleep` en $< 10\text{ ms}$) y delega en `RetrievalService.SearchMemories`.
3. `romero_memory_search` devuelve los candidatos autorizados en formato compacto (`summary`, `confidence`, `epistemic_status`, `relations_count`, `has_conflict_alert`) sin `content`.
4. El agente selecciona $2$ UUIDs relevantes y llama a `romero_memory_get` con `{ "memory_ids": [id1, id2] }`.
5. `mcp.Server` deduplica y valida el lote ($1..20$), consulta `RetrievalService.GetMemory` para cada UUID y devuelve `status = "found"` con `<romero_memory ... untrusted_content="true">`, versiones y relaciones cuyos dos extremos son legibles, o `status = "not_found"` opaco (con auditoría `denied`) si alguno no es accesible.

**Primary flow 2 — Integración de Gandalf Wizard vía `pkg/romeroclient` y Coherencia en `romero serve`**

1. `romero serve` arranca el servidor REST en `:8420` y el listener MCP `HTTP/SSE` en `:8421` compartiendo la misma Caché Cognitiva L1 en RAM y el mismo `PowerGovernor`.
2. El orquestador externo (Gandalf Wizard) utiliza `pkg/romeroclient` contra `:8420` para invocar `BuildContext` antes de lanzar un agente y despacha al agente conectado a `:8421` por MCP `HTTP/SSE`.
3. Cuando el agente guarda un hallazgo con `romero_memory_save` en `:8421` (o el orquestador arbitra un conflicto con `ReviewConflict` en `:8420`), la actualización *write-through* en la Caché L1 compartida es visible inmediatamente en ambos puertos en $0\text{ ms}$.

- **Consistency boundaries:** Ninguna herramienta MCP abre transacciones SQL propias; cada herramienta delega en los servicios de aplicación (`CaptureService`, `RetrievalService`, `ContextBuilderService`, `FeedbackService`), que mantienen sus fronteras transaccionales ACID y el registro de eventos `denied` fuera del rollback.
- **Migration impact:** **None** — El esquema SQL fundacional (`00001_foundation.sql` y consultas `sqlc` existentes) ya soporta todas las operaciones requeridas por el servidor MCP y `pkg/romeroclient`.

## 5. Testing strategy

| Acceptance criterion | Level | Asserts | Fakes / mocks |
| --- | --- | --- | --- |
| `spec §Acceptance AC-01` (Catálogo estricto de 5 herramientas MCP) | unit + contract | `tools/list` en `stdio` y `HTTP/SSE` devuelve exactamente `["romero_memory_save", "romero_memory_search", "romero_memory_get", "romero_memory_context", "romero_memory_feedback"]` con `inputSchema` válido y ningún campo de credencial en argumentos. | Servicios de aplicación simulados en pruebas unitarias de contrato JSON-RPC. |
| `spec §Acceptance AC-02` (Divulgación progresiva y lote `romero_memory_get` con opacidad) | unit + integration (PostgreSQL 17 real) | `romero_memory_search` nunca serializa `"content"` (además de respetar filtros `memory_types` y `min_confidence`); `romero_memory_get` con lote mixto (propios + `agent_private` ajeno + inexistente, actual y *Point-in-Time*) entrega `<romero_memory ... untrusted_content="true">`, versiones y solo relaciones con ambos extremos legibles para los propios, y `status: "not_found"` opaco + fila `denied` en `audit_events` para los ajenos. | Unidad con fakes; integración contra PostgreSQL 17 real sin mocks de SQL. |
| `spec §Acceptance AC-03` (Captura, feedback bayesiano y recibos opacos en MCP) | unit + integration (PostgreSQL 17 real) | `romero_memory_save` y `romero_memory_feedback` persisten y recalibran $(\alpha, \beta)$ cuando hay permiso de lectura+escritura; cuando el agente es *write-only* en `task_shared`, ambas herramientas devuelven `{"status": "accepted", "request_id": "<uuid>"}` opaco; si un `agent` intenta `verified` o `corrected_by_user`, recibe `IsError: true` con `code: "forbidden"` y auditoría `denied`. | Integración contra PostgreSQL 17 real. |
| `spec §Acceptance AC-04` (Empaquetado XML seguro en `romero_memory_context`) | unit + integration (PostgreSQL 17 real) | `romero_memory_context` devuelve `xml_document` con `untrusted_content="true"`, `total_tokens <= max_tokens` y $0.00\%$ de fuga de recuerdos o relaciones de otros ámbitos. | Integración contra PostgreSQL 17 real. |
| `spec §Acceptance AC-05` (Autenticación Zero-Trust en `stdio` y `HTTP/SSE`) | unit + integration (PostgreSQL 17 real) | `ServeStdio` sin llave o con llave inválida falla al arranque y registra `denied`; `HTTP/SSE` (`GET /sse`, `POST /message`, `POST /mcp`) sin cabecera válida devuelve `401 Unauthorized` y audita `denied`; un Agente B no puede publicar en el `sessionId` SSE del Agente A (`403 Forbidden`); sesiones concurrentes de distintos agentes/inquilinos mantienen $0.00\%$ de fuga cruzada. | Integración contra PostgreSQL 17 real y `httptest.Server`. |
| `spec §Acceptance AC-06` (Preempción REM $< 10\text{ ms}` y coherencia L1 en `romero serve`) | unit + integration | Con `PowerGovernor` en `rem_sleep`, cualquier `tools/call` MCP cancela el contexto REM en $< 10\text{ ms}` y transiciona a `guard`/`burst`; en `romero serve`, el listener REST y el listener MCP comparten la misma instancia de `CognitiveGraph` y `PowerGovernor`. | `PowerGovernor` real con reloj/canal determinista. |
| `spec §Acceptance AC-07` (Librería cliente pública `pkg/romeroclient`) | unit + integration (end-to-end HTTP) | Un test de paquete externo (`package romeroclient_test`) verifica que `pkg/romeroclient` no importa nada de `internal/` y ejecuta contra el servidor ROMERO real las 14 operaciones (`CaptureMemory`, `GetMemory`, `UpdateMemory`, `SupersedeMemory`, `ArchiveMemory`, `RecordFeedback`, `GetRequestStatus`, `SearchMemories`, `BuildContext`, `GetSubgraph`, `ListConflicts`, `ReviewConflict`, `TriggerREM`, `GetSystemStatus`), incluyendo recibos `202 Accepted` y errores tipados `401`/`403`/`404`/`422`. | Servidor REST de ROMERO real sobre PostgreSQL 17. |

- **Where the e2e line is, and why:** El límite E2E de esta fase abarca tanto un cliente MCP (`stdio` y `HTTP/SSE`) como un consumidor externo mediante `pkg/romeroclient` ejecutando el flujo completo (contexto → búsqueda compacta → lectura detallada en lote → captura → retroalimentación → arbitraje de conflicto → estado del sistema) contra PostgreSQL 17 real, además de `romero eval` en verde.
- **What MUST be a real dependency:** PostgreSQL 17 real para todas las pruebas de integración de aislamiento Zero-Trust, opacidad en lotes `romero_memory_get`, auditoría `denied` y flujo E2E de `pkg/romeroclient`.

## 6. Sequencing & dependencies

1. **Filtrado de relaciones visibles en `GetMemory` y filtros `MemoryTypes` / `MinConfidence` en `SearchMemories` (`internal/application/retrieval` + `internal/interfaces/rest`)** — depends on: none — [serial]
   - Asegurar que `RetrievalService.GetMemory` cargue `Versions` y filtre `Relations` verificando `authorizeRead` sobre ambos extremos de cada relación (tanto en modo actual como *Point-in-Time*), y añadir soporte opcional de `MemoryTypes` y `MinConfidence` en `SearchQuery` y en `POST /api/v1/memories/search`.
2. **Servidor MCP Core y las 5 Herramientas Canónicas con Preempción de `PowerGovernor` (`internal/interfaces/mcp`)** — depends on: #1 — [parallelizable con #3]
   - Implementar el motor JSON-RPC 2.0 MCP, el registro estricto de las 5 herramientas canónicas (`romero_memory_save`, `romero_memory_search`, `romero_memory_get`, `romero_memory_context`, `romero_memory_feedback`), el mapeo de errores estructurados de herramienta (`validation_error`, `forbidden`, `conflict`, `not_found`, `service_unavailable`), el enganche `PowerGovernor.BeginAgentRequest()` / `EndAgentRequest()`, el transporte `stdio` autenticado al arranque y el transporte `HTTP/SSE` (`GET /sse`, `POST /message`, `POST /mcp`) autenticado por petición y vinculado por sesión.
3. **Librería Cliente Pública Exportable (`pkg/romeroclient`)** — depends on: #1 — [parallelizable con #2]
   - Implementar `pkg/romeroclient` sin ningún import de `internal/`, cubriendo los 14 métodos tipados contra la API de ROMERO, soporte explícito de recibos opacos `202 Accepted` (`Accepted: true`, `RequestID`) y errores tipados `APIError`.
4. **Integración CLI (`romero mcp` y listener MCP `HTTP/SSE` compartido en `romero serve`) y Suite E2E Zero-Trust (`internal/interfaces/cli`, `cmd/romero`)** — depends on: #2, #3 — [serial]
   - Conectar el subcomando `romero mcp` (`--transport stdio|sse`, `--listen`, `--api-key`), integrar el listener MCP `HTTP/SSE` (`--mcp-listen`, por defecto `:8421` desde `mcp_sse_port`) dentro de `romero serve` compartiendo `cachel1.CognitiveGraph` y `PowerGovernor` con REST (`:8420`), y ejecutar las pruebas de integración contra PostgreSQL 17 real más `romero eval`.

- **Parallel candidates:** Los pasos #2 (`internal/interfaces/mcp`) y #3 (`pkg/romeroclient`) tocan árboles de directorios completamente disjuntos y pueden construirse de forma independiente una vez cerrado #1.
- **Hard ordering constraints:** #1 debe preceder a #2 y #3 porque define los filtros de `SearchQuery` y el filtrado Zero-Trust de `Relations` en `GetMemory`; #4 integra el binario completo y depende de #2 y #3.

## 7. Risks & open decisions

**Risks**

| Risk | Trigger | Impact | Mitigation / spike to retire it |
| --- | --- | --- | --- |
| **R1: Fuga de existencia de recuerdos ajenos a través de `Relations` en `romero_memory_get`** | Un recuerdo legible en `project_shared` tiene una arista hacia un recuerdo en `agent_private` de otro agente o en una tarea no autorizada, y `GetMemory` devuelve la lista cruda de `ListActiveRelationsByMemoryID`. | El agente llamador descubriría el UUID de un recuerdo fuera de su ámbito. | En el Paso #1, `GetMemory` carga el recuerdo par de cada arista y aplica `authorizeRead(ctx, auth, peerMem)`, omitiendo silenciosamente cualquier relación cuyo otro extremo no sea legible para el solicitante. |
| **R2: Secuestro de `sessionId` entre agentes concurrentes en MCP `HTTP/SSE`** | Un Agente B autenticado intenta enviar `POST /message?sessionId=<id_de_Agente_A>` hacia una sesión SSE abierta por el Agente A. | Mezcla de contextos de identidad o inyección de respuestas en el flujo SSE de otro agente. | Vincular cada sesión SSE en memoria a `(TenantID, PrincipalID)` al abrir `GET /sse` y verificar en cada `POST /message` que el `AuthContext` autenticado de la petición coincida con el propietario de la sesión (`403 Forbidden` + auditoría `denied` si no coincide). |
| **R3: Bloqueo de tubería `stdio` por escrituras concurrentes o líneas JSON-RPC grandes** | Múltiples llamadas `tools/call` concurrentes escriben en `stdout` sin sincronización o un lote de 20 recuerdos supera el buffer por defecto de `bufio.Scanner` ($64\text{ KiB}$). | Corrupción de tramas JSON-RPC en `stdio` o fallo de lectura por `bufio.ErrTooLong`. | Proteger el escritor de `stdio` con un `sync.Mutex` dedicado y configurar el buffer del lector `stdio` hasta $4\text{ MiB}$ por mensaje JSON-RPC. |
| **R4: Acoplamiento accidental de `pkg/romeroclient` a paquetes `internal/`** | Alguien importa un tipo de `internal/domain` dentro de `pkg/romeroclient`. | El repositorio externo de Gandalf Wizard fallaría al compilar con `use of internal package not allowed`. | Declarar DTOs e identificadores de dominio propios en `pkg/romeroclient` y añadir un test de verificación estática en `pkg/romeroclient/client_test.go` que parsee las importaciones del paquete con `go/parser` y falle si aparece cualquier ruta que contenga `/internal/`. |

**Open decisions**

- **None** — Todas las decisiones arquitectónicas y de contrato quedaron cerradas en `specify` y `clarify` y registradas en [decisions.md](../decisions.md).

## Tasks
<!-- generated by tasks on 2026-11-15; IDs are stable, do not renumber -->

| ID | [P] | Task | Done-check | Depends-on | Trace | Risk |
| --- | --- | --- | --- | --- | --- | --- |
| T001 |  | Filter visible relations by both endpoints in `GetMemory` and add `MemoryTypes`/`MinConfidence` filters to `SearchMemories` & REST | `go test -race -count=1 ./internal/application/retrieval/... ./internal/interfaces/rest/...` green: `GetMemory` (actual y *Point-in-Time*) omite relaciones con extremo ilegible y carga `Versions`; `SearchMemories` filtra por `MemoryTypes` y `MinConfidence` | — | AC-02 | 1 |
| T002 | [P] | Implement MCP JSON-RPC 2.0 core, strict 5-tool catalog, structured errors, and `PowerGovernor` preemption hook | `go test -race -count=1 ./internal/interfaces/mcp/...` green: `tools/list` devuelve exactamente 5 herramientas canónicas; `tools/call` ejecuta `BeginAgentRequest`/`EndAgentRequest`, valida lote $1..20$ en `romero_memory_get`, envuelve XML `untrusted_content="true"`, devuelve `accepted` opaco y mapea errores a `IsError: true` sin cerrar sesión | T001 | AC-01, AC-02, AC-03, AC-04, AC-06 | 1 |
| T003 |  | Implement `stdio` startup-auth loop and `HTTP/SSE` (`GET /sse`, `POST /message`, `POST /mcp`) per-request auth & session binding | `go test -race -count=1 ./internal/interfaces/mcp/...` green: `ServeStdio` falla sin credencial válida y opera autenticado; `HTTP/SSE` devuelve `401` sin credencial, `403` si otro principal intenta usar un `sessionId` ajeno, y aísla sesiones concurrentes | T002 | AC-05, AC-06 | 1 |
| T004 | [P] | Implement exportable public Go client library `pkg/romeroclient` with zero `internal/` imports | `go test -race -count=1 ./pkg/romeroclient/...` green: test AST con `go/parser` confirma cero imports de `/internal/`; los 14 métodos tipados manejan `200`/`201`, recibos `202 Accepted` (`Accepted: true`, `RequestID`) y `*APIError` (`401`/`403`/`404`/`409`/`422`/`503`) | T001 | AC-07 | 1 |
| T005 |  | Wire `romero mcp` CLI subcommand and integrate shared MCP `HTTP/SSE` listener (`:8421`) inside `romero serve` | `go test -race -count=1 ./internal/interfaces/cli/... ./cmd/romero/...` green: `romero mcp --transport stdio|sse` valida flags/credencial y `romero serve` levanta REST (`:8420`) + MCP `HTTP/SSE` (`:8421`) compartiendo `CognitiveGraph` y `PowerGovernor` en RAM | T003 | AC-05, AC-06 | 1 |
| T006 |  | Run PostgreSQL 17 Zero-Trust integration tests for MCP transports and E2E `pkg/romeroclient` against live server | `go test -race -count=1 ./internal/interfaces/mcp/... ./pkg/romeroclient/...` contra PostgreSQL 17 real green: `stdio` y `HTTP/SSE` logran $0.00\%$ fuga, lote mixto `romero_memory_get` devuelve `not_found` opaco + auditoría `denied`, `task_shared` write-only devuelve `accepted` y `pkg/romeroclient` completa el ciclo E2E | T004, T005 | AC-02, AC-03, AC-04, AC-05, AC-07 | 1 |
| T007 |  | Verify full test suite, linters, static `CGO_ENABLED=0` build, and Level 2 `romero eval` cognitive gate | `gofmt -s -l internal cmd pkg` vacío, `go vet ./...` 0 errores, `golangci-lint run` 0 incidencias, `go test -race -p 1 -count=1 ./...` green, `CGO_ENABLED=0 go build -trimpath ./cmd/romero` ok y `romero.exe eval --dataset tests/evaluation/golden_dataset.json` devuelve `pass: true` con `zero_trust_leakage_rate: 0` | T006 | AC-01..AC-07 | — |

**T001 — Interfaces**
- Consumes: `retrieval.Service.authorizeRead(ctx, auth, mem)` y `reg.Memories().GetMemoryByID`, `ListVersionsByMemoryID`, `ListActiveRelationsByMemoryID` en `internal/application/retrieval`.
- Produces: `retrieval.SearchQuery{ MemoryTypes []memory.MemoryType, MinConfidence *float32 }` (y campos opcionales `memory_types`, `min_confidence` en `POST /api/v1/memories/search`); `retrieval.Service.GetMemory` devolviendo `Versions` (filtradas a `RecordedAt <= knownAt` en *Point-in-Time*) y `Relations` filtradas para incluir solo aristas cuyos dos extremos pasen `authorizeRead`.

**T002 — Interfaces**
- Consumes: `CaptureService.CaptureMemory`, `RetrievalService.SearchMemories`, `RetrievalService.GetMemory`, `ContextService.BuildContext`, `FeedbackService.RecordFeedback`, `GovernorPreemptor` (`BeginAgentRequest()`, `EndAgentRequest()`), `Authenticator.Authenticate(ctx, rawKey)`.
- Produces: `internal/interfaces/mcp.Server` con `ListTools()` (estrictamente 5 herramientas: `romero_memory_save`, `romero_memory_search`, `romero_memory_get`, `romero_memory_context`, `romero_memory_feedback`), `HandleJSONRPC(ctx, auth, rawReq)` y mapeo de errores de dominio a `CallToolResult{ IsError: true, Content: ... }` (`validation_error`, `forbidden`, `conflict`, `not_found`, `service_unavailable`).

**T003 — Interfaces**
- Consumes: `mcp.Server.HandleJSONRPC(ctx, auth, rawReq)` y `Authenticator.Authenticate(ctx, rawKey)` de T002.
- Produces: `(*mcp.Server).ServeStdio(ctx, rawAPIKey, in, out) error` (con buffer hasta `4 MiB` y `sync.Mutex` en `out`) y `(*mcp.Server).HTTPHandler() http.Handler` con `GET /sse`, `POST /message?sessionId=<id>` (verificando coincidencia de `(TenantID, PrincipalID)`) y `POST /mcp`.

**T004 — Interfaces**
- Consumes: Contrato HTTP/REST `/api/v1/*` existente + filtros `memory_types`/`min_confidence` de T001.
- Produces: Paquete exportable `pkg/romeroclient` (cero imports de `internal/`) con `NewClient(Config) (*Client, error)`, `*APIError`, `CaptureResponse{ Accepted bool, RequestID *uuid.UUID, ... }`, `FeedbackResponse{ Accepted bool, RequestID *uuid.UUID, ... }` y los 14 métodos tipados (`CaptureMemory`, `GetMemory`, `UpdateMemory`, `SupersedeMemory`, `ArchiveMemory`, `RecordFeedback`, `GetRequestStatus`, `SearchMemories`, `BuildContext`, `GetSubgraph`, `ListConflicts`, `ReviewConflict`, `TriggerREM`, `GetSystemStatus`).

**T005 — Interfaces**
- Consumes: `mcp.NewServer(mcp.ServerDeps{...})`, `ServeStdio`, `HTTPHandler()` de T003 y `configs/romero.toml` (`mcp_sse_port = 8421`).
- Produces: Subcomando Cobra `romero mcp` (`--transport stdio|sse`, `--listen`, `--api-key`), extensión de `romero serve` (`--mcp-listen`, por defecto `:8421`) compartiendo `l1Graph` y `gov` con REST (`:8420`), y wiring completo en `cmd/romero/wiring.go`.

**T006 — Interfaces**
- Consumes: `mcp.Server` (`ServeStdio` + `HTTPHandler`), `rest.NewRouter`, `pkg/romeroclient.NewClient` y pool real `PostgreSQL 17` (`ROMERO_TEST_DSN` / `ROMERO_DSN`).
- Produces: Suite de integración real en `internal/interfaces/mcp/integration_pg_test.go` y `pkg/romeroclient/integration_pg_test.go` demostrando `AC-02`, `AC-03`, `AC-04`, `AC-05` y `AC-07`.

## Review Workload Forecast

| Dimension | Forecast | Why |
| --- | --- | --- |
| Estimated changed lines | ~1,600–2,300 líneas (código + tests) | Adaptador MCP completo (`protocol`, `tools`, `stdio`, `http_sse`), paquete `pkg/romeroclient` (14 métodos + DTOs), mejoras en `retrieval`/`rest`, CLI `romero mcp`/`serve` y tests de integración PostgreSQL 17. |
| Files / areas | ~16–20 archivos en 5 paquetes | `internal/application/retrieval`, `internal/interfaces/rest`, `internal/interfaces/mcp`, `pkg/romeroclient`, `internal/interfaces/cli`, `cmd/romero`. |
| Risk | Max tier `1`; `0` tareas tier-2 | Todas las rutas evaluadas por `rsc risk` arrojaron `tier 1` (`1` revisor por tarea, sin aprobación humana bloqueante por riesgo). |
| Suggested delivery | `ask-on-risk` (PR único de fase o subdivisión por paquete si supera cómodamente el presupuesto de revisión) | Supera el `line_budget: 400` de `config.yaml`, por lo que conviene revisión estricta por tarea durante `implement` y cierre en PR dedicado `feat/phase04-mcp-server`. |
