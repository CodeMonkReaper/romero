---
type: plan
title: Plan — Phase 02 Hybrid Retrieval (Recuperación Híbrida, Grafo Causal y Empaquetado de Contexto)
description: The structure-level implementation plan for phase-02-hybrid-retrieval — contracts, shapes, flows, testing strategy, sequencing and risks.
tags: [sdd, plan, romero, phase-02, retrieval, graph, submodular, zero-trust]
timestamp: 2026-11-15T08:56:30-03:00
topic: sdd
slug: phase-02-hybrid-retrieval
status: approved
---

# Plan — Phase 02 Hybrid Retrieval (Recuperación Híbrida, Grafo Causal y Empaquetado de Contexto)

> Spec: [../specs/phase-02-hybrid-retrieval.md](../specs/phase-02-hybrid-retrieval.md) · Constitution: [../constitution.md](../constitution.md) · Status: approved
> Last updated: 2026-11-15

## 0. Global Constraints

- **Lenguaje y compilación (`constitution §1 #1`):** Go `1.23+` estricto con `CGO_ENABLED=0` obligatorio en compilación del binario (`CGO_ENABLED=0 go build -trimpath ./cmd/romero`). Cero enlaces a librerías `.dll`, `.so` o `.dylib`.
- **Stack canónico (`constitution §1 #2, #3, #4`):** HTTP REST con `net/http` + `Huma v2` (OpenAPI 3.1) bajo `/api/v1`; CLI con `Cobra`; migraciones con `Goose`; acceso a datos con `pgx/v5` (`pgxpool`) + consultas compiladas estáticamente por `sqlc` sobre `PostgreSQL 17` (`pgcrypto`, `vector` 0.8.0+ con `halfvec(1024)`, `pg_trgm`, `btree_gist`). Clientes de inferencia local vía HTTP persistente (`keep-alive`, `DisableCompression: true`) detrás de puertos de dominio en `internal/domain/memory/ports.go`. Prohibido usar ORMs con reflexión o concatenar SQL fuera de `.sql` compilados por `sqlc`.
- **Fronteras de arquitectura limpia (`constitution §3 #8`):** `internal/domain` (`identity`, `memory`, `graph`, `provenance`, `security`) contiene exclusivamente Go puro de la librería estándar (y `github.com/google/uuid`), sin importar `infrastructure`, `interfaces`, drivers SQL ni clientes HTTP. Dirección de dependencias invariante: `interfaces → application → domain ← infrastructure`.
- **Calidad y verificación en dos niveles (`constitution §2 #5, #6, #7`):**
  - **Nivel 1 (obligatorio en cada tarea):** Cero errores y cero advertencias en `gofmt -s -w .`, `go vet ./...`, `golangci-lint run` y `sqlc generate` sin diffs; `go test -race ./...` siguiendo TDD (`red → green → refactor`) con cobertura de líneas $\ge 80\%$ en `internal/domain` e `internal/application`.
  - **Nivel 2 (`romero eval`, obligatorio en rutas cognitivas y cierre):** Ejecución reproducible sobre `tests/evaluation/golden_dataset.json` verificando $\text{NDCG}@5 \ge 0.85$, $\text{Recall}@K$ multi-salto ($2\text{–}3$ saltos) $\ge 0.85$, supresión de obsolescencia ($0$ recuerdos `superseded` infiltrados por defecto) y tasa de fuga entre ámbitos exactamente $0.00\%$.
- **Aislamiento Zero-Trust, poda pre-propagación y escaneo iterativo HNSW (`constitution §5 #14`, `spec §Acceptance AC-03, AC-10, AC-11`):**
  - Toda consulta SQL de lectura/búsqueda y todo recorrido en la Caché L1 filtra obligatoriamente por `tenant_id`, `project_id`, `scope_type` (`agent_private`, `task_shared`, `project_shared`, `org_shared`, `global_public`) y permisos vigentes del `AuthContext` (incluyendo evaluación en vivo de `task_permissions` para `task_shared`).
  - **Poda estricta pre-propagación:** Ningún nodo ni arista fuera de los ámbitos legibles por el solicitante puede recibir ni transmitir energía durante *Spreading Activation*, ni sumarse a `relations_count`, ni aparecer en `GetSubgraph`.
  - **Parámetros obligatorios en transacciones de búsqueda vectorial HNSW (`constitution §5 #14`):** Toda transacción `pgx/v5` que ejecute búsqueda vectorial sobre `memory_embeddings.dense_vec` (`halfvec(1024)`) debe configurar `SET LOCAL hnsw.iterative_scan = relaxed_order` y `SET LOCAL hnsw.max_scan_tuples = 20000`.
  - **Opacidad anti-enumeración (`404 Not Found`):** En `GetSubgraph` sobre una semilla inexistente o ajena, y en `CaptureMemory` síncrono cuando un `to_memory_id` en `relations` no existe o no es legible para el solicitante, devolver `404 Not Found` idéntico y registrar `audit_events(outcome = 'denied')` fuera del rollback de negocio.
- **Constantes matemáticas canónicas de Fase 02 (`spec §Behaviour`, `AC-01`, `AC-02`, `AC-04`, `AC-05`, `AC-07`, `Master Spec §12`):**
  - **Fusión Híbrida Tri-Modal ($\text{RRF}$):** $k_{\text{rrf}} = 60$; pesos normales $\omega_{\text{trgm}} = 0.25$, $\omega_{\text{fts}} = 0.30$, $\omega_{\text{vec}} = 0.45$. En modo degradado sin vector ($\text{degraded\_mode} = \text{true}$): $\omega_{\text{trgm}} = 0.45$, $\omega_{\text{fts}} = 0.55$, $\omega_{\text{vec}} = 0.00$.
  - **Activación Propagada (*Spreading Activation*):** Semillas iniciales Top-$50$ de $S_{\text{rrf}}$ normalizadas a $[0, 1]$; saltos $H \in \{0, 1, 2, 3\}$ (default $2$ en `SearchMemories`/`BuildContext`, rango $1..3$ con default $2$ en `GetSubgraph`); amortiguamiento $\gamma = 0.35$; conductancias por tipo de arista: $\psi(\text{depends\_on}) = 1.00$, $\psi(\text{caused\_by}) = 0.95$, $\psi(\text{supports}) = 0.85$, $\psi(\text{derived\_from}) = 0.80$, $\psi(\text{contradicts}) = 0.60$ (activa `has_conflict_alert = true`), $\psi(\text{related\_to}) = 0.50$, $\psi(\text{supersedes}) = 0.00$ (no propaga hacia obsoletos salvo `include_superseded = true`, donde $\psi(\text{supersedes}) = 0.40$). Recorrido bidireccional sobre aristas activas vigentes cuyos dos extremos sean visibles para el solicitante, normalizado por el grado activo visible $\text{deg}_{\text{vis}}(u) \ge 1$.
  - **Ponderación Epistemológica y Bayesiana ($M_{\text{epist}}$):** $\hat{\theta}_m = \frac{\alpha_m}{\alpha_m + \beta_m}$; multiplicadores $W_{\text{ep}}(\text{verified}) = 1.20$, $W_{\text{ep}}(\text{observed}) = 1.00$, $W_{\text{ep}}(\text{reported}) = 0.90$, $W_{\text{ep}}(\text{inferred}) = 0.75$, $W_{\text{ep}}(\text{hypothesis}) = 0.50$; $M_{\text{epist}}(m) = W_{\text{ep}}(\text{epistemic\_status}(m)) \cdot (0.45 + 0.55 \cdot \hat{\theta}_m)$.
  - **Decaimiento Temporal por Tipo Cognitivo ($M_{\text{time}}$):** $\Delta t_{\text{days}} = \max(0, \frac{t_{\text{now}} - \text{observed\_at}(m)}{86400})$; $\lambda_{\text{episodic}} = 0.015$, $\lambda_{\text{semantic}} = 0.002$, $\lambda_{\text{procedural}} = 0.001$, $\lambda_{\text{relational}} = 0.002$; $M_{\text{time}}(m) = 0.70 + 0.30 \cdot \exp(-\lambda_{\text{type}(m)} \cdot \Delta t_{\text{days}})$.
  - **Factor de Estado ($P_{\text{state}}$) y Puntuación Gobernada ($\text{FinalScore}$):** $P_{\text{state}}(\text{active}) = 1.00$, $P_{\text{state}}(\text{conflicted}) = 0.55$, $P_{\text{state}}(\text{consolidated}) = 0.65$ (solo si `include_consolidated = true`), $P_{\text{state}}(\text{superseded}) = 0.35$ (solo si `include_superseded = true`); `archived`, `rejected` y `candidate` tienen $P_{\text{state}} = 0.00$ (nunca elegibles). $\text{FinalScore}(m, Q) = S_{\text{sem}}(m, Q) \cdot M_{\text{epist}}(m) \cdot M_{\text{time}}(m) \cdot (0.85 + 0.30 \cdot \text{importance}(m)) \cdot (1.0 + 0.20 \cdot \text{utility\_score}(m)) \cdot P_{\text{state}}(m)$, donde en Fase 02 (sin Cross-Encoder) $S_{\text{sem}}(m, Q) = \hat{a}_m^{(H)}$. Desempate determinista: `FinalScore DESC`, `ObservedAt DESC`, `ID ASC`.
  - **Empaquetado Submodular (`BuildContext`):** Función objetivo $F(S) = \sum_{m_i \in S} \text{FinalScore}(m_i, Q) + \eta \sum_{m_j \in U} \max_{m_i \in S} (\text{FinalScore}(m_i, Q) \cdot \text{sim}(m_i, m_j))$ con $\eta = 0.35$ y selección voraz por ganancia marginal $\frac{F(S \cup \{m\}) - F(S)}{(c(m))^{\rho}}$ con $\rho = 0.65$. $\text{sim}(m_i, m_j) = \max(0, \vec{v}_i \cdot \vec{v}_j)$ cuando ambos tienen vector normalizado; en `degraded_mode = true` (o si falta vector en algún candidato), $\text{sim}(m_i, m_j)$ usa similitud de Jaccard sobre el conjunto normalizado de `tags` + `entities` + tokens de `summary`. Si $c_{\text{full}}(m)$ excede el presupuesto restante pero $c_{\text{summary}}(m)$ cabe, degrada el ítem a modo compacto (`summary` + `id`).
- **Límites de entrada y validación (`422 Unprocessable Entity`, `spec §Points to clarify`, `AC-11`):**
  - `SearchMemories`: `query` no vacía tras `strings.TrimSpace`; `limit` en $1..100$ (default $20$); `hops` en $0..3$ (default $2$).
  - `BuildContext`: `query` no vacía; `max_tokens` en $64..32768$ (default $4000$); `hops` en $0..3$ (default $2$).
  - `GetSubgraph`: `hops` en $1..3$ (default $2$).
  - `CaptureMemory.relations`: tipos permitidos exclusivamente en `{'depends_on', 'caused_by', 'supports', 'derived_from', 'related_to'}` (rechazar `supersedes` y `contradicts` con `422`); `weight` en $(0.0, 1.0]$ (default $1.0$ si se omite o es $0$); prohibidos auto-bucles y pares `(to_memory_id, relation_type)` duplicados en la misma solicitud (`422`).
- **Lecturas puras y divulgación progresiva (`constitution §3 #9`, `spec §Acceptance AC-01, AC-12`):** `SearchMemories` nunca devuelve `content`; `SearchMemories`, `BuildContext` y `GetSubgraph` no modifican `access_count`, `last_accessed_at` ni `version` en `memories` (solo insertan `audit_events`).
- **Git y autoría (`constitution §3 #10, §4 #11, #12`):** Trabajo en ramas dedicadas con Pull Request, mensajes de commit con `gitmoji` + Conventional Commits y autoría exclusivamente humana (prohibido `Co-Authored-By`).

## 1. Context & constraints

- **Criterios de aceptación que gobiernan el diseño:**
  - Fusión híbrida tri-modal ($\text{RRF}$), divulgación progresiva sin `content` y ordenamiento gobernado con desempate determinista: `spec §Acceptance AC-01, AC-04`.
  - Propagación de activación multi-salto ($1..3$ saltos) bidireccional, rescate causal y exploración de vecindario en `GetSubgraph` con opacidad `404`: `spec §Acceptance AC-02, AC-10`.
  - Poda Zero-Trust estricta pre-propagación ($0.00\%$ de fuga directa e indirecta y `relations_count` filtrado por visibilidad): `spec §Acceptance AC-03`.
  - Supresión por defecto de `superseded`/`consolidated`, exclusión total de `archived`/`rejected`, alerta de contradicción activa y coherencia inmediata del grafo ante mutaciones: `spec §Acceptance AC-05, AC-06`.
  - Empaquetado submodular voraz bajo restricción estricta de `max_tokens`, divulgación progresiva (`content` $\rightarrow$ `summary`) y contenedor XML `untrusted_content="true"`: `spec §Acceptance AC-07`.
  - Worker transaccional priorizado de embeddings `BGE-M3` (`halfvec(1024)`), reintentos con retroceso hasta `dead_letter` y degradación elegante (`degraded_mode = true`): `spec §Acceptance AC-08, AC-09`.
  - Declaración explícita de relaciones causales en `CaptureMemory` con validación Zero-Trust de lectura sobre el destino (`404` opaco) y lecturas cognitivas libres de mutación sobre `memories`: `spec §Acceptance AC-11, AC-12`.
- **Barras no funcionales:** Presupuesto de latencia en tiempo real con paralelismo `errgroup` entre vectorización de consulta y pre-resolución en Caché L1 (`constitution §6 #17`); $0.00\%$ de fuga entre scopes con `SET LOCAL hnsw.iterative_scan = relaxed_order` (`constitution §5 #14`); tope de memoria RAM en Caché L1 (`l1_max_memory_mb = 128` en perfil `balanced`).
- **Fuera de alcance (`spec §Non-goals`):** Cross-Encoder Reranker (`bge-reranker-v2-m3`), detector NLI de contradicciones (`mDeBERTa-v3`, `ListConflicts`, `ReviewConflict`), consolidación en Sueño REM (`SLM 3B`), servidor MCP y mutación síncrona de `access_count` en lecturas.

## 2. Architecture

```text
+---------------------------------------------------------------------------------------------------+
|                             ROMERO Single Static Binary (Go 1.23+, CGO_ENABLED=0)                 |
|                                                                                                   |
|  +---------------------------+                       +-----------------------------------------+  |
|  | CLI Adapter (INTERNAL)    |                       | REST API Adapter (INTERNAL - Huma v2)   |  |
|  | migrate / bootstrap /     |                       | POST /api/v1/memories (+relations)      |  |
|  | principal / serve / eval  |                       | POST /api/v1/memories/search            |  |
|  +-------------+-------------+                       | POST /api/v1/memories/context           |  |
|                |                                     | GET  /api/v1/memories/{id}/subgraph     |  |
|                |                                     | GET  /api/v1/system/status              |  |
|                |                                     +--------------------+--------------------+  |
|                |                                                          |                       |
|                v                                                          v                       |
|  +---------------------------------------------------------------------------------------------+  |
|  | Application Layer (INTERNAL)                                                                |  |
|  | - CaptureService        (explicit relations validation + write-through L1 Graph sync)       |  |
|  | - RetrievalService      (SearchMemories parallel errgroup + GetSubgraph + GetMemory)        |  |
|  | - ContextBuilderService (BuildContext submodular facility location + progressive XML wrap)  |  |
|  | - EmbeddingJobWorker    (priority dequeue embed_and_nli -> BGE-M3 -> upsert + L1 sync)      |  |
|  +-------------------+---------------------------------+---------------------------------------+  |
|                      |                                 |                                          |
|                      v                                 v                                          |
|  +---------------------------------------+   +-------------------------------------------------+  |
|  | Domain Core (INTERNAL - Pure Go)      |   | L1 Cognitive Graph Cache (INTERNAL - In-RAM)    |  |
|  | - memory/ranking:    RRF, Epistemic,  |   | - Indexed node/edge arena per (tenant, project) |  |
|  |                      Temporal, Final  |   | - Write-through mutation hooks + lazy load      |  |
|  | - memory/submodular: Greedy optimizer |   | - Zero-Trust pre-pruned subgraph projection     |  |
|  | - graph:             SpreadingActiv.  |   +-------------------------------------------------+  |
|  +-------------------+-------------------+                                                        |
|                      ^                                                                            |
|                      | implements ports                                                           |
|  +-------------------+---------------------------------+---------------------------------------+  |
|  | Infrastructure Adapters (INTERNAL)                  |                                       |  |
|  | - Persistence Adapter (pgx/v5 + sqlc + HNSW scan)   | - Neural Adapter (BGE-M3 HTTP Pool)   |  |
|  +-----------------------------------------------------+---------------------------------------+  |
+--------------------------------------------------------|------------------------------------------+
                           | TCP (pgxpool)               | HTTP/1.1 & HTTP/2 Keep-Alive
                           v                             v
      +----------------------------------------+   +---------------------------------------------+
      | PostgreSQL 17 + pgvector 0.8.0+        |   | Local BGE-M3 Embedding Daemon               |
      | (EXTERNAL: FTS, pg_trgm, halfvec HNSW, |   | (EXTERNAL / OPTIONAL: 1024-dim FP16 vectors;|
      |  memory_relations, SKIP LOCKED queue)  |   |  graceful fallback to degraded_mode=true)   |
      +----------------------------------------+   +---------------------------------------------+
```

- **CLI Adapter (`internal/interfaces/cli`, INTERNAL):** Añade el comando `romero eval` para ejecutar la evaluación cognitiva Nivel 2 sobre `golden_dataset.json` y conecta el worker de embeddings y la Caché L1 en `romero serve`.
- **REST API Adapter (`internal/interfaces/rest`, INTERNAL):** Expone `POST /api/v1/memories/search`, `POST /api/v1/memories/context`, `GET /api/v1/memories/{id}/subgraph`, amplía `POST /api/v1/memories` con `relations` y enriquece `GET /api/v1/system/status`.
- **CaptureService (`internal/application/capture`, INTERNAL):** Valida y persiste atómicamente relaciones explícitas en captura (`AC-11`) y notifica sincrónicamente a la Caché Cognitiva L1 cada captura, actualización, supersesión o archivado (`AC-06`).
- **RetrievalService (`internal/application/retrieval`, INTERNAL):** Orquesta en paralelo (`errgroup`) la vectorización de la consulta y la resolución de candidatos/grafo, aplica poda Zero-Trust pre-propagación, ejecuta *Spreading Activation* y puntúa candidatos en `SearchMemories` y `GetSubgraph`.
- **ContextBuilderService (`internal/application/context`, INTERNAL):** Selecciona el subconjunto óptimo de candidatos mediante optimización submodular voraz bajo `max_tokens`, aplica divulgación progresiva (`content` vs. `summary`) y envuelve cada ítem en XML seguro (`AC-07`).
- **EmbeddingJobWorker (`internal/application/jobs` / `internal/infrastructure/queue`, INTERNAL):** Reclama trabajos pendientes `embed_and_nli` por prioridad (`FOR UPDATE SKIP LOCKED`), invoca al cliente `BGE-M3`, guarda `halfvec(1024)` en `memory_embeddings`, sincroniza el vector normalizado en la Caché L1 y gestiona reintentos con retroceso hasta `dead_letter` (`AC-08`).
- **Domain Core (`internal/domain/{memory,graph}`, INTERNAL):** Implementa como funciones puras deterministas la fusión $\text{RRF}$, el motor de *Spreading Activation* amortiguado, la puntuación gobernada $\text{FinalScore}$ y el optimizador submodular *Facility Location*.
- **L1 Cognitive Graph Cache (`internal/infrastructure/cachel1`, INTERNAL):** Mantiene en memoria RAM los nodos vigentes y aristas activas con `sync.RWMutex`, sincronización *write-through* desde los servicios de mutación (`CaptureService`, `FeedbackService`) y respaldo transaccional en PostgreSQL.
- **Neural Adapter (`internal/infrastructure/neural`, INTERNAL):** Cliente HTTP persistente precalentado hacia el endpoint `BGE-M3` que convierte textos en vectores de $1024$ dimensiones y clasifica caídas o *timeouts* para activar `degraded_mode = true` sin abortar lecturas (`AC-09`).

**Top architectural decision:** **Combinar una Caché Cognitiva L1 en RAM sincronizada *write-through* (con respaldo SQL recursivo exacto) que proyecta un subgrafo podado por permisos Zero-Trust *antes* de ejecutar la propagación matemática en el dominio puro, corriendo en paralelo (`errgroup`) con la vectorización `BGE-M3` y degradando limpiamente a búsqueda léxica/exacta + Jaccard cuando faltan vectores.**
- *Alternativa descartada:* Ejecutar *Spreading Activation* sobre el grafo completo en memoria y filtrar los nodos no autorizados después de propagar, o exigir que todo recuerdo tenga fila en `memory_embeddings` mediante `INNER JOIN` para ser candidato en `SearchMemories`.
- *Por qué se elige este diseño:* Filtrar después de propagar viola `AC-03` y el Principio 14 porque permite que un nodo privado (`agent_private` o `task_shared` ajeno) transmita energía como puente entre dos nodos públicos e infle `relations_count`. A su vez, un `INNER JOIN` estricto con `memory_embeddings` ocultaría cualquier recuerdo recién capturado mientras su trabajo asíncrono `embed_and_nli` está en cola y derribaría el servicio si el contenedor `BGE-M3` no está activo. Podar el subgrafo por `AuthContext` + `task_permissions` antes de propagar y desacoplar la rama vectorial con `degraded_mode = true` garantiza simultáneamente $0.00\%$ de fuga indirecta (`AC-03`), latencia sub-$18\text{ ms}$ (Principio 17) y disponibilidad inmediata tras captura (`AC-09`).

## 3. Interfaces & contracts

```text
memory.EmbeddingProvider.Embed(ctx: Context, text: string) -> EmbeddingOutput | ProviderUnavailableError
memory.EmbeddingProvider.EmbedBatch(ctx: Context, texts: []string) -> []EmbeddingOutput | ProviderUnavailableError
  - invariant: produce vectores densos L2-normalizados de exactamente 1024 dimensiones (float32 -> halfvec(1024)) usando pool HTTP keep-alive con DisableCompression=true.
  - postcondition: si el endpoint está deshabilitado, inaccesible o vence el timeout de consulta, devuelve ProviderUnavailableError para que el llamador active degraded_mode=true sin fallar la lectura.

graph.Engine.PropagateActivation(seeds: map[UUID]float32, adj: AuthorizedSubgraph, hops: int, includeSuperseded: bool) -> map[UUID]ActivationNodeResult
  - invariant: función pura en internal/domain/graph; opera exclusivamente sobre adj (que ya contiene solo nodos y aristas vigentes y autorizadas para el AuthContext del llamador); sin efectos laterales ni acceso a red/DB.
  - precondition: 0 <= hops <= 3; seeds contiene puntajes S_rrf normalizados en [0, 1] para hasta 50 nodos semilla.
  - postcondition: aplica H iteraciones con amortiguamiento gamma=0.35 y pesos psi(relation_type)*w_uv / deg_vis(u) de forma bidireccional sin ciclos de auto-refuerzo inmediato; marca HasConflictAlert=true en cualquier nodo alcanzado por una arista 'contradicts' o con status=='conflicted'; devuelve activación normalizada a_m^(H) en [0, 1] y distancia mínima MinHopDepth.

memory.Scorer.ComputeFinalScores(candidates: []CandidateInput, now: Time, degradedMode: bool) -> []ScoredCandidate
  - invariant: función pura en internal/domain/memory; calcula RRF (redistribuyendo pesos si degradedMode==true), M_epist(alpha, beta, epistemic_status), M_time(observed_at, memory_type), P_state(status) y FinalScore.
  - postcondition: excluye cualquier candidato con P_state == 0.0; ordena deterministamente por (FinalScore DESC, ObservedAt DESC, ID ASC).

memory.SubmodularOptimizer.SelectContext(candidates: []ScoredCandidate, maxTokens: int, degradedMode: bool) -> ContextSelection
  - invariant: función pura en internal/domain/memory; maximiza F(S) con eta=0.35 y ganancia marginal dividida por c(m)^0.65 sujeto a sum(tokens) <= maxTokens.
  - precondition: 64 <= maxTokens <= 32768.
  - postcondition: usa similitud coseno vectorial cuando ambos candidatos tienen DenseVec y degradedMode==false, o similitud de Jaccard sobre (tags U entities U summary_tokens) en caso contrario; si el contenido completo c_full(m) de un candidato excede el presupuesto restante pero c_summary(m) cabe, lo selecciona con IsCompact=true.

cachel1.GraphCache.ProjectAuthorizedSubgraph(ctx: Context, auth: AuthContext, filter: ScopeFilter, allowedTasks: map[string]bool, now: Time) -> AuthorizedSubgraph
cachel1.GraphCache.ApplyMutation(mutation: GraphMutationEvent) -> void
  - invariant: thread-safe (sync.RWMutex); ApplyMutation actualiza o retira inmediatamente en RAM cualquier recuerdo capturado, actualizado, supercedido, puesto en cuarentena (<0.30 -> conflicted) o archivado; ProjectAuthorizedSubgraph excluye todo nodo o arista fuera de la frontera de lectura de auth y allowedTasks antes de devolver la estructura al dominio.

retrieval.Service.SearchMemories(ctx: Context, auth: AuthContext, query: SearchMemoriesQuery) -> SearchMemoriesResult | UnauthorizedError | ForbiddenError | ValidationError
  - invariant: lectura pura sobre recuerdos (no modifica access_count, last_accessed_at ni version); nunca incluye el campo Content en los ítems devueltos (AC-01, AC-12).
  - precondition: auth válido (401 si falta); query.Query no vacío, Limit en 1..100, Hops en 0..3 (422 si fuera de rango); proyectos y scopes solicitados permitidos en auth y task_id con permiso de lectura vigente si consulta task_shared (403 + audita 'denied' si no autorizado).
  - postcondition: ejecuta en paralelo (errgroup) la vectorización de query.Query y la consulta tri-modal + proyección L1; si el proveedor vectorial falla o algún candidato carece de vector, opera con ramas disponibles y marca DegradedMode=true; registra AuditEvent(outcome='allowed', operation='search_memories') y devuelve Items (compactos) + DegradedMode.

retrieval.Service.GetSubgraph(ctx: Context, auth: AuthContext, query: GetSubgraphQuery) -> SubgraphResult | UnauthorizedError | NotFoundError | ValidationError
  - invariant: lectura pura; aplica opacidad anti-enumeración idéntica a GetMemory sobre la semilla (AC-10, AC-12).
  - precondition: query.SeedMemoryID != Nil y query.Hops en 1..3 (422 si inválido); si la semilla no existe o no es legible por auth -> registra AuditEvent(outcome='denied', operation='get_subgraph') y devuelve NotFoundError (404).
  - postcondition: devuelve el nodo semilla y todos los nodos (en vista compacta) y aristas vigentes hasta query.Hops cuyos dos extremos sean visibles para auth; registra AuditEvent(outcome='allowed').

context.Service.BuildContext(ctx: Context, auth: AuthContext, req: BuildContextRequest) -> BuildContextResult | UnauthorizedError | ForbiddenError | ValidationError
  - invariant: lectura pura sobre recuerdos; el total de tokens del paquete devuelto cumple siempre UsedTokens <= req.MaxTokens; cada ítem se envuelve individualmente en <romero_memory ... untrusted_content="true"> (AC-07, AC-12).
  - precondition: req.Query no vacío, req.MaxTokens en 64..32768, req.Hops en 0..3 (422 si inválido); permisos de proyecto/scope/task_id verificados igual que en SearchMemories (403 + audita 'denied' si falla).
  - postcondition: obtiene candidatos puntuados, ejecuta SubmodularOptimizer.SelectContext, envuelve Content (si IsCompact==false) o Summary (si IsCompact==true) en XML seguro, registra AuditEvent(outcome='allowed', operation='build_context') y devuelve Items, CombinedXML, UsedTokens, MaxTokens, DegradedMode y HasConflictAlert.

jobs.EmbeddingWorker.ProcessNextBatch(ctx: Context, limit: int) -> int | OperationalError
  - invariant: reclama trabajos 'pending' en memory_jobs ordenados por (priority DESC, scheduled_at ASC) mediante FOR UPDATE SKIP LOCKED.
  - postcondition: genera vector halfvec(1024) en BGE-M3, hace upsert en memory_embeddings, actualiza L1 GraphCache y marca el job 'completed'; ante error del proveedor, incrementa attempts y reprograma scheduled_at con backoff exponencial (o pasa a 'dead_letter' cuando attempts >= max_attempts) sin alterar el recuerdo en memories (AC-08).
```

## 4. Data model & flow

**Entities**

- **MemoryEmbedding (`memory_embeddings`):** Almacena por `memory_id` (`PRIMARY KEY`) su `tenant_id`, `project_id`, `model_name` (`BAAI/bge-m3`), `dimensions` (`1024`), `dense_vec` (`halfvec(1024)` indexado con HNSW coseno `m = 16, ef_construction = 64`) y marcas temporales.
- **ExplicitRelationSpec (entrada en `CaptureCommand.Relations`):** Tupla `(ToMemoryID: UUID, RelationType: RelationType, Weight: float32, Notes: string)` validada en `CaptureMemory` y persistida en `memory_relations` con `from_memory_id = newMemory.ID`, `valid_from = newMemory.ValidFrom`, `is_active = true`.
- **L1GraphNode / L1GraphEdge (en memoria RAM `internal/infrastructure/cachel1`):** Proyección compacta en RAM de recuerdos vigentes (`ID`, `TenantID`, `ProjectID`, `ScopeType`, `OwnerAgentID`, `TaskID`, `Type`, `Status`, `EpistemicStatus`, `AlphaScore`, `BetaScore`, `Confidence`, `Importance`, `UtilityScore`, `TokenCount`, `Summary`, `Tags`, `Entities`, `ObservedAt`, `ValidFrom`, `ValidUntil`, `DenseVec`) y aristas activas (`FromID`, `ToID`, `RelationType`, `Weight`, `ValidFrom`, `ValidUntil`, `IsActive`).
- **SearchCandidate / ContextItem (proyecciones de salida):**
  - `SearchCandidate`: vista compacta sin `Content`, con `ID`, `Summary`, `Type`, `ScopeType`, `Status`, `EpistemicStatus`, `Confidence`, `FinalScore`, `RelationsCount` (solo aristas visibles), `MinHopDepth` ($0$ si es coincidencia directa, $1..3$ si fue rescatado/alcanzado por grafo), `HasConflictAlert` y `ObservedAt`.
  - `ContextItem`: `MemoryID`, `IsCompact` (`false` = `Content` completo, `true` = `Summary` por restricción de presupuesto), `TokenCost`, `FinalScore`, `HasConflictAlert` y `XMLEnvelope`.

**Primary flow** (`SearchMemories` y `BuildContext` con paralelismo `errgroup`, poda Zero-Trust y degradación elegante)

1. `REST Adapter` recibe `POST /api/v1/memories/search` o `POST /api/v1/memories/context`; `AuthMiddleware` inyecta `AuthContext`.
2. `RetrievalService` valida rangos (`limit`, `hops`, `max_tokens`, `query` no vacía $\rightarrow$ `422` si falla) y autoriza los proyectos/scopes solicitados en `AuthContext` (más `TaskAccessChecker.EvaluateTaskAccess` con `TaskRead` si incluye `task_shared` $\rightarrow$ `403` + `audit_events(outcome = 'denied')` si carece de permiso).
3. `RetrievalService` lanza dos ramas concurrentes mediante `errgroup`:
   - **Rama A (Vectorización de consulta):** Invoca `EmbeddingProvider.Embed(ctx, query)` con timeout acotado. Si el proveedor no está disponible o falla, registra `degraded_mode = true` y devuelve vector nulo sin abortar el grupo.
   - **Rama B (Recuperación léxica/exacta y proyección L1):** Consulta en PostgreSQL los candidatos autorizados por trigramas (`pg_trgm` sobre `topic_key` y `summary`) y Full-Text (`fts_doc` con `websearch_to_tsquery` / `plainto_tsquery` en `'spanish'` y `'simple'`), y proyecta en la Caché L1 (hidratando desde PostgreSQL si el proyecto aún no está caliente) el subgrafo vigente podado estrictamente por los ámbitos y tareas autorizados del solicitante.
4. Si la Rama A produjo vector de consulta, ejecuta dentro de una transacción de lectura con `SET LOCAL hnsw.iterative_scan = relaxed_order` y `SET LOCAL hnsw.max_scan_tuples = 20000` la búsqueda vectorial HNSW (`halfvec_cosine_ops`) sobre `memory_embeddings` filtrada por la misma frontera Zero-Trust. Si algún candidato léxico/exacto o del subgrafo aún no tiene vector persistido, activa `degraded_mode = true`.
5. `memory.Scorer` fusiona los rankings ordinales ($\text{RRF}$) de las ramas disponibles y entrega las semillas Top-$50$ a `graph.Engine.PropagateActivation`, que propaga energía durante $H$ saltos únicamente sobre el `AuthorizedSubgraph` pre-podado, rescatando nodos causales conectados (`depends_on`, `caused_by`, `supports`, `derived_from`, `related_to`, `contradicts`) y calculando `relations_count` visible y `has_conflict_alert`.
6. `memory.Scorer.ComputeFinalScores` aplica la ponderación epistemológica/bayesiana ($M_{\text{epist}}$), el decaimiento temporal por tipo cognitivo ($M_{\text{time}}$), importancia, utilidad y penalización de estado ($P_{\text{state}}$), ordenando deterministamente.
7. Si la operación es `SearchMemories`: trunca a `limit`, proyecta a vistas compactas (sin `Content`), registra `audit_events(outcome = 'allowed', operation = 'search_memories')` y devuelve el resultado.
8. Si la operación es `BuildContext`: pasa los candidatos puntuados a `memory.SubmodularOptimizer.SelectContext` con `max_tokens`, carga el `Content` de los candidatos seleccionados con `IsCompact = false`, envuelve cada ítem en `<romero_memory ... untrusted_content="true">`, registra `audit_events(outcome = 'allowed', operation = 'build_context')` y devuelve el paquete de contexto.

- **Consistency boundaries:**
  - En `CaptureMemory` con `relations` explícitas (`AC-11`), la validación de visibilidad de lectura de todos los `to_memory_id`, la inserción del recuerdo, su versión `v1`, las filas en `memory_relations`, el trabajo `embed_and_nli` y la auditoría `allowed` ocurren dentro de **una única transacción ACID**, seguida inmediatamente por la actualización *write-through* de la Caché L1 antes de responder al cliente.
  - En `UpdateMemory`, `SupersedeMemory`, `ArchiveMemory` y `RecordFeedback`, el cambio en PostgreSQL es **transaccional (ACID)** y actualiza sincrónicamente (*write-through*) la Caché L1 al confirmar la transacción (`AC-06`).
  - La generación de vectores `halfvec(1024)` en `memory_embeddings` es **eventualmente consistente** vía `memory_jobs` (`priority = 100`), cubierta durante la ventana intermedia por `degraded_mode = true` (`AC-08`, `AC-09`).
- **Migration impact:** **Ninguna migración destructiva ni alteración de tablas requerida (`none`).** Las tablas `memory_embeddings` (con índice HNSW `idx_memory_embeddings_hnsw_dense`), `memory_relations` (con índices `idx_memory_relations_forward` e `idx_memory_relations_backward`), `memory_jobs` (con `idx_memory_jobs_dequeue`) y los índices GIN `idx_memories_fts`, `idx_memories_trgm_summary` e `idx_memories_trgm_topic` ya fueron creados en `migrations/00001_romero_core_schema.sql`. Toda la persistencia nueva se resuelve añadiendo consultas compiladas en `internal/infrastructure/persistence/queries/*.sql` y regenerando `sqlc`.

## 5. Testing strategy

| Acceptance criterion | Level | Asserts | Fakes / mocks |
| --- | --- | --- | --- |
| `spec §Acceptance AC-01` (Fusión híbrida tri-modal y divulgación progresiva sin `content`) | unit + integration + contract | Coincidencias por símbolo exacto (`pg_trgm`), término léxico (`fts_doc`) y vector (`halfvec(1024)`) se fusionan con $\text{RRF}$ ($k=60$); el JSON de `POST /api/v1/memories/search` expone `summary`, `confidence`, `relations_count`, `min_hop_depth`, `has_conflict_alert` y `degraded_mode`, y omite siempre `content`. | En unit: candidatos sintéticos; en integration/contract: PostgreSQL 17 real + proveedor de embeddings determinista + `httptest`. |
| `spec §Acceptance AC-02` (Rescate causal multi-salto a $1..3$ saltos) | unit + integration + e2e (`romero eval`) | Semilla $M_1$ rescata causa raíz $M_2$ (1 salto `caused_by`) y dependencia $M_3$ (2 saltos `depends_on`) sin solapamiento léxico con la consulta cuando `hops >= 2`, reportando `min_hop_depth = 1` y `2`. | En unit: grafo en memoria puro; en integration: PostgreSQL 17 real + Caché L1. |
| `spec §Acceptance AC-03` (Poda Zero-Trust pre-propagación y $0.00\%$ fuga indirecta) | unit + integration | En cadena $M_1 (\text{project\_shared}) \rightarrow M_{\text{priv}} (\text{agent\_private ajeno o task\_shared sin permiso}) \rightarrow M_3 (\text{project\_shared})$, el Agente B no ve $M_{\text{priv}}$, `relations_count` de $M_1$ excluye la arista hacia $M_{\text{priv}}$ y $M_3$ recibe $0.0$ de activación propagada desde $M_1$. | En integration: PostgreSQL 17 real con matriz multi-tenant, multi-agente y permisos `task_permissions` revocados (`tests/security`). |
| `spec §Acceptance AC-04` (Gobernanza epistemológica, bayesiana y decaimiento temporal diferenciado) | unit | A igual similitud semántica, `verified` ($\hat{\theta}=0.95$) supera estrictamente a `observed`, `reported`, `inferred` e `hypothesis` ($\hat{\theta}=0.35$); un recuerdo `episodic` de 60 días decae más rápido ($\lambda=0.015$) que uno `procedural` ($\lambda=0.001$) o `semantic` ($\lambda=0.002$). | Sin mocks (funciones matemáticas puras en `internal/domain/memory`). |
| `spec §Acceptance AC-05` (Supresión de obsolescencia y señalización de contradicciones) | unit + integration | Por defecto, $0$ recuerdos `superseded`, `consolidated`, `archived` o `rejected` aparecen en `SearchMemories` o `BuildContext`; par con arista `contradicts` o nodo `conflicted` activa `has_conflict_alert = true` manteniendo al nodo `active` por encima del `conflicted`; con `include_superseded = true`, `superseded` aparece en búsqueda pero `archived` sigue excluido. | Dominio puro en unit + PostgreSQL 17 real en integration. |
| `spec §Acceptance AC-06` (Coherencia inmediata del grafo ante mutaciones de ciclo de vida) | integration | Tras `SupersedeMemory`, captura con colisión de `topic_key`, cuarentena bayesiana ($\hat{\theta} < 0.30 \rightarrow \text{conflicted}$) o `ArchiveMemory`, una llamada inmediata a `SearchMemories`, `BuildContext` o `GetSubgraph` refleja el cambio en la Caché L1 sin servir nodos archivados ni obsoletos. | PostgreSQL 17 real + instancia viva de `cachel1.GraphCache`. |
| `spec §Acceptance AC-07` (Empaquetado submodular bajo `max_tokens` y divulgación progresiva) | unit + integration + contract | Con 3 recuerdos redundantes y 1 recuerdo complementario, `BuildContext` selecciona el complementario antes que el 2.º/3.º redundante, cumple `UsedTokens <= MaxTokens`, degrada a `summary` (`is_compact = true`) cuando el `content` completo no cabe en el margen restante, y envuelve cada ítem en `<romero_memory ... untrusted_content="true">`. | Sin mocks en optimizador puro; PostgreSQL 17 + `httptest` en integration/contract. |
| `spec §Acceptance AC-08` (Enriquecimiento vectorial asíncrono priorizado y resiliente) | unit + integration | El worker reclama primero `priority = 100` antes que `priority = 50/10` vía `FOR UPDATE SKIP LOCKED`, persiste `halfvec(1024)` en `memory_embeddings`, y ante fallos del proveedor incrementa `attempts` con backoff hasta marcar `dead_letter` en `max_attempts` sin alterar el recuerdo en `memories`. | PostgreSQL 17 real + servidor HTTP `httptest` simulando `BGE-M3` sano y con fallos transitorios/permanentes. |
| `spec §Acceptance AC-09` (Degradación elegante sin proveedor vectorial o con vector pendiente) | unit + integration + contract | Si el servidor `BGE-M3` está apagado o un recuerdo recién capturado aún tiene su job en `pending`, `SearchMemories` y `BuildContext` devuelven `200 OK` con `degraded_mode = true`, encuentran el recuerdo por trigramas/FTS/grafo y aplican diversidad Jaccard en `BuildContext`. | PostgreSQL 17 real + proveedor de embeddings desconectado / con error de red. |
| `spec §Acceptance AC-10` (Exploración de vecindario causal en `GetSubgraph` con opacidad `404`) | integration + contract | `GET /api/v1/memories/{id}/subgraph?hops=2` devuelve la semilla y aristas/nodos visibles hasta 2 saltos sin ciclos; si `id` no existe o pertenece a un ámbito/tarea sin permiso de lectura, devuelve `404 Not Found` idéntico y persiste `audit_events(outcome = 'denied')`. | PostgreSQL 17 real + `httptest`. |
| `spec §Acceptance AC-11` (Declaración explícita de relaciones causales en `CaptureMemory` con opacidad Zero-Trust) | unit + integration + contract | `POST /api/v1/memories` con `relations` válidas hacia un recuerdo legible persiste atómicamente el recuerdo y las aristas en `memory_relations`; si `to_memory_id` no existe o no es legible por el llamador, aborta con `404 Not Found` opaco y audita `denied`; si envía `supersedes`/`contradicts` o auto-bucle, devuelve `422`. | PostgreSQL 17 real + `httptest`. |
| `spec §Acceptance AC-12` (Lectura pura sin mutación de recuerdos en operaciones cognitivas) | integration | Ejecutar `SearchMemories`, `BuildContext` y `GetSubgraph` registra eventos `allowed` en `audit_events` pero deja `access_count`, `last_accessed_at` y `version` de los recuerdos leídos exactamente iguales antes y después de la llamada. | PostgreSQL 17 real. |

- **Dónde se sitúa la línea E2E y por qué:** El límite superior en Fase 02 incluye (1) pruebas de integración y seguridad HTTP REST contra **PostgreSQL 17 + `pgvector` real** y un servidor HTTP local de embeddings (`httptest` / `BGE-M3`), y (2) el comando CLI reproducible **`romero eval`** (Nivel 2 Cognitivo, Principio 7) ejecutado sobre `tests/evaluation/golden_dataset.json` para medir $\text{NDCG}@5$, $\text{Recall}@K$ multi-salto, supresión de obsoletos y $0.00\%$ de fuga entre ámbitos.
- **Qué DEBE ser una dependencia real (sin mocks):** **PostgreSQL 17 con las extensiones `vector` (`halfvec` HNSW), `pg_trgm` y búsqueda `TSVECTOR`** en todas las pruebas de integración de búsqueda tri-modal, `SET LOCAL hnsw.iterative_scan = relaxed_order`, bloqueo `FOR UPDATE SKIP LOCKED` de `memory_jobs` y aislamiento Zero-Trust (`AC-01`, `AC-03`, `AC-08`, `AC-11`, `AC-12`), porque las garantías dependen de la ejecución real de los índices GIN/HNSW y transacciones de PostgreSQL.

## 6. Sequencing & dependencies

1. **Motores matemáticos puros de dominio (`internal/domain/memory/{ranking,submodular}.go`, `internal/domain/graph/{relation,spreading_activation}.go`): fusión $\text{RRF}$ (normal y degradada), propagación amortiguada multi-salto ($1..3$ saltos) con alerta de conflicto, puntuación gobernada $\text{FinalScore}$, optimizador submodular voraz *Facility Location* (coseno y Jaccard), validación de relaciones explícitas y puertos de búsqueda/embeddings en `ports.go` con su suite unitaria TDD** — depends on: none — `[parallelizable con #2]`
2. **Consultas SQL compiladas (`internal/infrastructure/persistence/queries/{relations,embeddings,jobs,search}.sql` + `sqlc generate`) y repositorios PostgreSQL (`internal/infrastructure/persistence/postgres/*`): validación/inserción de relaciones explícitas, reclamo priorizado `SKIP LOCKED` y reintentos/`dead_letter` en `memory_jobs`, upsert de `halfvec(1024)` en `memory_embeddings`, y consultas tri-modales Zero-Trust (`pg_trgm`, `fts_doc`, `halfvec` HNSW con `SET LOCAL hnsw.iterative_scan = relaxed_order` y `hnsw.max_scan_tuples = 20000`) + CTE recursivo de respaldo** — depends on: none — `[parallelizable con #1]`
3. **Cliente HTTP persistente de embeddings `BGE-M3` (`internal/infrastructure/neural/bge_m3_client.go`), Caché Cognitiva L1 en RAM (`internal/infrastructure/cachel1/cognitive_graph.go`) con poda Zero-Trust pre-propagación y sincronización *write-through*, y worker transaccional priorizado de enriquecimiento vectorial (`internal/application/jobs/embedding_worker.go`) con pruebas unitarias y de integración (`AC-03`, `AC-06`, `AC-08`, `AC-09`)** — depends on: #1, #2 — `[serial]`
4. **Ampliación de `CaptureService` (`relations` explícitas con validación Zero-Trust `404` opaca y notificación *write-through* a Caché L1 en mutaciones/feedback), ampliación de `RetrievalService` (`SearchMemories` con paralelismo `errgroup` y `GetSubgraph` con opacidad `404`) y nuevo `ContextBuilderService` (`BuildContext` submodular con divulgación progresiva y contenedor XML seguro) (`AC-01`..`AC-07`, `AC-09`..`AC-12`)** — depends on: #1, #2, #3 — `[serial]`
5. **Adaptador HTTP REST Huma v2 (`POST /api/v1/memories/search`, `POST /api/v1/memories/context`, `GET /api/v1/memories/{id}/subgraph`, `relations` en `POST /api/v1/memories` y métricas L1/perfil en `GET /api/v1/system/status`), integración de worker/L1 en `romero serve` y comando `romero eval` con `tests/evaluation/golden_dataset.json` (Nivel 2 Cognitivo)** — depends on: #4 — `[serial]`
6. **Suite completa de pruebas de integración E2E, seguridad Zero-Trust multi-salto ($0.00\%$ fuga directa e indirecta), coherencia *write-through* de Caché L1, resiliencia `degraded_mode` y verificación de cierre Nivel 1 + Nivel 2 (`romero eval`) contra PostgreSQL 17 real** — depends on: #5 — `[serial]`

- **Candidatos paralelos:** Los pasos **#1** (`internal/domain/{memory,graph}/**`) y **#2** (`internal/infrastructure/persistence/**`) tocan árboles de directorios disjuntos sin estado compartido y pueden desarrollarse y verificarse en paralelo antes de converger en **#3**.
- **Restricciones duras de orden:** **#3** necesita los tipos/puertos matemáticos de **#1** y los repositorios compilados de **#2**; **#4** ensambla los casos de uso sobre la Caché L1, el cliente `BGE-M3` y los repositorios; **#5** expone los contratos HTTP REST y el comando `romero eval`; **#6** ejecuta la batería completa de integración, seguridad multi-salto y evaluación cognitiva sobre el sistema integrado.

## 7. Risks & open decisions

**Risks**

| Risk | Trigger | Impact | Mitigation / spike to retire it |
| --- | --- | --- | --- |
| **Caída de recall en índice HNSW al filtrar por scopes pequeños (`agent_private` / `task_shared`)** | PostgreSQL filtra candidatos post-índice HNSW en un ámbito con pocos recuerdos dentro de una tabla grande y agota la ventana de exploración por defecto (`ef_search = 40`). | La rama vectorial devuelve menos de $K$ resultados aunque existan recuerdos válidos en el ámbito (viola Principio 14). | Ejecutar obligatoriamente `SET LOCAL hnsw.iterative_scan = relaxed_order` y `SET LOCAL hnsw.max_scan_tuples = 20000` dentro de la transacción `pgx/v5` de búsqueda vectorial, verificado con test de integración contra `pgvector 0.8.0+`. |
| **Fuga indirecta de información o inflación de `relations_count` a través de nodos puente no autorizados en la Caché L1** | La Caché L1 mantiene en RAM el grafo del proyecto completo y un recorrido multi-salto atraviesa un nodo `agent_private` ajeno o `task_shared` sin permiso antes de filtrar la salida. | Fuga de inferencia entre agentes/tareas o conteo de aristas privadas visibles en `relations_count` (viola `AC-03` y Principio 14). | `GraphCache.ProjectAuthorizedSubgraph` evalúa la visibilidad de cada nodo (`AuthContext.CanReadMemory` + mapa pre-resuelto de `task_permissions` vigentes) *antes* de construir la lista de adyacencia que recibe `graph.Engine.PropagateActivation`, descartando cualquier arista donde el origen o el destino no sea legible. |
| **Lectura de nodos obsoletos o archivados en la Caché L1 tras mutaciones** | Un recuerdo es supercedido por `topic_key`, archivado o puesto en cuarentena ($\hat{\theta} < 0.30$) en PostgreSQL pero la Caché L1 en RAM conserva su estado anterior. | `SearchMemories`, `BuildContext` o `GetSubgraph` devuelve un recuerdo `superseded` o `archived` como vigente (viola `AC-05` y `AC-06`). | Sincronización *write-through* inmediata bajo `sync.RWMutex` al confirmar cada transacción en `CaptureService` y `FeedbackService`, más verificación de ventana `[valid_from, valid_until)` y estado en tiempo de consulta. |
| **Invisibilidad temporal de recuerdos recién capturados o fallo total si `BGE-M3` no responde** | Un agente captura un recuerdo e invoca `SearchMemories` o `BuildContext` en el turno siguiente antes de que el worker procese `memory_jobs`, o corre en entorno CPU sin el contenedor `BGE-M3`. | El recuerdo nuevo no aparece o la búsqueda aborta con `503`. | Consultas de candidatos por ramas independientes (unión en Go/SQL de trigramas, FTS y vectores con `LEFT JOIN` / consultas desacopladas), redistribución automática de pesos $\text{RRF}$ cuando falta el vector, similitud de Jaccard en `BuildContext` y bandera `degraded_mode = true` (`AC-09`). |

**Open decisions**

- **Ninguna (`none`):** Todas las decisiones de alcance, contratos, aislamiento Zero-Trust en propagación, creación de relaciones explícitas (`AC-11`), lecturas puras (`AC-12`) y modo degradado quedaron cerradas en `specify` y `clarify` y registradas en `02-DOCS/wiki/sdd/decisions.md`.

## Tasks
<!-- generated by tasks on 2026-11-15; IDs are stable, do not renumber -->

| ID | [P] | Task | Done-check | Depends-on | Trace | Risk |
| --- | --- | --- | --- | --- | --- | --- |
| T001 | [P] | Implement and unit-test pure domain mathematical engines and ports: tri-modal `RRF` (normal & degraded), governed `FinalScore` (epistemic, Bayesian, cognitive temporal decay, state penalty, deterministic tie-breaker), 1–3 hop bidirectional `SpreadingActivation` with conflict alert, submodular `FacilityLocation` context selector (cosine & Jaccard fallback + progressive disclosure), explicit relation validation, and search/embedding/graph ports (`internal/domain/memory`, `internal/domain/graph`) | `go test -race -cover ./internal/domain/memory/... ./internal/domain/graph/...` exits 0 with statement coverage >= 85% | — | AC-01, AC-02, AC-03, AC-04, AC-05, AC-07, AC-09, AC-10, AC-11 | 1 |
| T002 | [P] | Write compiled SQL queries (`internal/infrastructure/persistence/queries/{relations,embeddings,jobs,search}.sql`), run `sqlc generate`, and implement PostgreSQL repositories for explicit relation visibility/insertion, priority job claim/retry/dead-letter (`FOR UPDATE SKIP LOCKED`), `halfvec(1024)` embedding upsert, and Zero-Trust tri-modal search (`pg_trgm`, `fts_doc`, `halfvec` HNSW with `SET LOCAL hnsw.iterative_scan = relaxed_order` and `hnsw.max_scan_tuples = 20000`) + recursive subgraph query (`internal/infrastructure/persistence`) | `sqlc generate` produces zero diffs AND `go vet ./internal/infrastructure/persistence/...` AND `go test -race -cover ./internal/infrastructure/persistence/...` exit 0 | — | AC-01, AC-02, AC-03, AC-05, AC-08, AC-09, AC-10, AC-11, AC-12 | 1 |
| T003 |  | Implement and unit-test persistent HTTP `BGE-M3` embedding client (`internal/infrastructure/neural`), in-RAM `L1 Cognitive Graph Cache` (`internal/infrastructure/cachel1`) with Zero-Trust pre-propagation pruning and write-through sync, and priority transactional `EmbeddingWorker` (`internal/application/jobs`) | `go test -race -cover ./internal/infrastructure/neural/... ./internal/infrastructure/cachel1/... ./internal/application/jobs/...` exits 0 with coverage >= 85% | T001, T002 | AC-02, AC-03, AC-06, AC-08, AC-09, AC-10 | 1 |
| T004 | [P] | Extend `CaptureService` (`CaptureMemory` sync and opaque worker execution with optional explicit `Relations` validation, `404` opaque anti-enumeration, and write-through L1 Graph sync on capture/update/supersede/archive) and `FeedbackService` (write-through L1 Graph sync on Bayesian update and `< 0.30` quarantine) (`internal/application/capture`, `internal/application/feedback`, `internal/application/requests`) | `go test -race -cover ./internal/application/capture/... ./internal/application/feedback/... ./internal/application/requests/...` exits 0 with coverage >= 85% | T001, T002, T003 | AC-05, AC-06, AC-11 | 1 |
| T005 | [P] | Extend `RetrievalService` (`SearchMemories` with `errgroup` parallel query embedding + L1/tri-modal resolution, governed scoring, `degraded_mode` fallback, and `GetSubgraph` with `404` opaque seed check) and implement `ContextBuilderService` (`BuildContext` with submodular optimizer, progressive disclosure `content` -> `summary`, and `<romero_memory ... untrusted_content="true">` wrapping) (`internal/application/retrieval`, `internal/application/context`) | `go test -race -cover ./internal/application/retrieval/... ./internal/application/context/...` exits 0 with coverage >= 85% | T001, T002, T003 | AC-01, AC-02, AC-03, AC-04, AC-05, AC-06, AC-07, AC-09, AC-10, AC-12 | 1 |
| T006 |  | Wire Huma v2 OpenAPI 3.1 REST routes (`POST /api/v1/memories/search`, `POST /api/v1/memories/context`, `GET /api/v1/memories/{id}/subgraph`, `relations` in `POST /api/v1/memories`, and L1/profile status in `GET /api/v1/system/status`), connect `EmbeddingWorker` and `GraphCache` in `romero serve`, and implement `romero eval` CLI command with `tests/evaluation/golden_dataset.json` (`internal/interfaces/rest`, `internal/interfaces/cli`, `tests/evaluation`) | `go test -race -cover ./internal/interfaces/rest/... ./internal/interfaces/cli/... ./cmd/romero/...` exits 0 with coverage >= 80% in `rest`/`cli` AND `go run ./cmd/romero eval --dataset tests/evaluation/golden_dataset.json` exits 0 | T004, T005 | AC-01, AC-02, AC-03, AC-05, AC-07, AC-09, AC-10, AC-11, AC-12 | 1 |
| T007 |  | Implement and run full Phase 02 integration, multi-hop Zero-Trust security (`0.00%` direct & indirect leakage), L1 write-through coherence, pure read-only invariance, and `degraded_mode` resilience test suites against real PostgreSQL 17 (`tests/integration`, `tests/security`) | `go test -p 1 -count=1 -race -v ./tests/integration/... ./tests/security/...` exits 0 against live PostgreSQL 17 covering AC-01 through AC-12 with 0.00% cross-scope leakage | T006 | AC-01, AC-02, AC-03, AC-04, AC-05, AC-06, AC-07, AC-08, AC-09, AC-10, AC-11, AC-12 | 1 |
| T008 |  | All done-checks pass + full Level 1 & Level 2 (`romero eval`) verification green | Every row `T001`–`T007` checked AND `sqlc generate`, `gofmt -s -w .`, `CGO_ENABLED=0 go build -trimpath ./cmd/romero`, `go vet ./...`, `golangci-lint run`, `go test -p 1 -count=1 -race -coverprofile=coverage.out ./...`, and `romero eval` exit 0 with >= 80% coverage in `domain`/`application` | T001, T002, T003, T004, T005, T006, T007 | all ACs | — |

### GitHub Issues (`CodeMonkReaper/romero`)

| Task | Issue | Title | Blocked by |
| --- | --- | --- | --- |
| `T001` | [#27](https://github.com/CodeMonkReaper/romero/issues/27) | `[Phase 02][T001] Motores matemáticos puros de dominio y puertos (RRF, Spreading Activation, Submodular)` | — |
| `T002` | [#28](https://github.com/CodeMonkReaper/romero/issues/28) | `[Phase 02][T002] Consultas sqlc y repositorios PostgreSQL de búsqueda tri-modal, HNSW y cola priorizada` | — |
| `T003` | [#29](https://github.com/CodeMonkReaper/romero/issues/29) | `[Phase 02][T003] Cliente HTTP BGE-M3, Caché Cognitiva L1 en RAM y EmbeddingWorker priorizado` | `#27`, `#28` |
| `T004` | [#30](https://github.com/CodeMonkReaper/romero/issues/30) | `[Phase 02][T004] Relaciones causales explícitas en CaptureService y sincronización write-through L1` | `#27`, `#28`, `#29` |
| `T005` | [#31](https://github.com/CodeMonkReaper/romero/issues/31) | `[Phase 02][T005] SearchMemories híbrido, GetSubgraph y ContextBuilderService submodular` | `#27`, `#28`, `#29` |
| `T006` | [#32](https://github.com/CodeMonkReaper/romero/issues/32) | `[Phase 02][T006] Rutas REST de búsqueda/contexto/subgrafo y comando CLI romero eval (Nivel 2)` | `#30`, `#31` |
| `T007` | [#33](https://github.com/CodeMonkReaper/romero/issues/33) | `[Phase 02][T007] Suite de integración E2E, seguridad Zero-Trust multi-salto y resiliencia degraded_mode` | `#32` |
| `T008` | [#34](https://github.com/CodeMonkReaper/romero/issues/34) | `[Phase 02][T008] Verificación final Nivel 1 y Nivel 2 (romero eval) de Phase 02` | `#27`–`#33` |

### Per-Task Interfaces

**T001 — Interfaces**
- Consumes: Go standard library + `github.com/google/uuid` + existing `internal/domain/{identity,memory,provenance,security}` types from Phase 01.
- Produces:
  - `memory.ExplicitRelationInput{ToMemoryID uuid.UUID, RelationType RelationType, Weight float32, Notes string}` and `memory.ValidateExplicitRelations(rels []ExplicitRelationInput) ([]ExplicitRelationInput, error)` (allows only `depends_on`, `caused_by`, `supports`, `derived_from`, `related_to`; defaults `Weight == 0` to `1.0`; rejects `Weight < 0 || Weight > 1`, duplicates on `(ToMemoryID, RelationType)`, and reserved types `supersedes`/`contradicts` with `ErrValidation`).
  - `memory.EmbeddingOutput{Dense []float32, SparseIdx []int32, SparseVals []float32}` and `memory.EmbeddingProvider` interface (`ModelName() string`, `Dimensions() int`, `Embed(ctx context.Context, text string) (EmbeddingOutput, error)`, `EmbedBatch(ctx context.Context, texts []string) ([]EmbeddingOutput, error)`).
  - `memory.ComputeRRFScores(trgmOrder, ftsOrder, vecOrder []uuid.UUID, degradedMode bool) map[uuid.UUID]float32` using $k_{\text{rrf}}=60$, normal weights $(0.25, 0.30, 0.45)$, degraded weights $(0.45, 0.55, 0.00)$.
  - `graph.Conductance(rel memory.RelationType, includeSuperseded bool) float32`, `graph.AuthorizedNode{ID uuid.UUID, Status memory.MemoryStatus}`, `graph.AuthorizedEdge{FromID, ToID uuid.UUID, RelationType memory.RelationType, Weight float32}`, `graph.AuthorizedSubgraph{Nodes map[uuid.UUID]AuthorizedNode, Edges []AuthorizedEdge}`, `graph.ActivationResult{NormalizedScore float32, MinHopDepth int, VisibleRelationsCount int, HasConflictAlert bool}`, and `graph.PropagateActivation(seeds map[uuid.UUID]float32, sub AuthorizedSubgraph, hops int, includeSuperseded bool) map[uuid.UUID]ActivationResult`.
  - `memory.CandidateRankingInput` & `memory.ScoredCandidate` and `memory.RankCandidates(inputs []CandidateRankingInput, now time.Time, includeSuperseded, includeConsolidated bool) []ScoredCandidate` implementing $M_{\text{epist}}$, $M_{\text{time}}$, $P_{\text{state}}$, $\text{FinalScore}$, and deterministic sort (`FinalScore DESC`, `ObservedAt DESC`, `ID ASC`).
  - `memory.SubmodularCandidate` & `memory.SelectedContextCandidate{MemoryID uuid.UUID, IsCompact bool, TokenCost int, FinalScore float32, HasConflictAlert bool}` and `memory.SelectSubmodularContext(candidates []SubmodularCandidate, maxTokens int, degradedMode bool) []SelectedContextCandidate` implementing Facility Location ($\eta=0.35$, $\rho=0.65$, cosine similarity when vectors exist and `!degradedMode`, Jaccard similarity over normalized `tags + entities + summary` tokens otherwise, and progressive disclosure `content` $\rightarrow$ `summary`).
  - Extended repository ports in `internal/domain/memory/ports.go`: `EmbeddingRepository`, `JobQueueRepository`, `SearchRepository`, and `GraphMutationNotifier`.

**T002 — Interfaces**
- Consumes: `migrations/00001_romero_core_schema.sql`..`00003_task_authorization_and_opaque_requests.sql` and domain ports from `T001`.
- Produces:
  - Compiled `sqlc` queries and `postgres` repository methods:
    - `UpsertMemoryEmbedding(ctx context.Context, emb memory.MemoryEmbedding) error` and `GetMemoryEmbeddingsByIDs(ctx context.Context, tenantID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID][]float32, error)`.
    - `ClaimPendingMemoryJobs(ctx context.Context, jobType string, limit int, now time.Time) ([]memory.MemoryJob, error)` (`FOR UPDATE SKIP LOCKED` ordered by `priority DESC, scheduled_at ASC`), `CompleteMemoryJob(ctx context.Context, jobID uuid.UUID, completedAt time.Time) error`, and `FailMemoryJob(ctx context.Context, jobID uuid.UUID, lastError string, nextScheduledAt time.Time, deadLetter bool) error`.
    - `SearchByTrigram(ctx context.Context, f memory.SearchFilter) ([]uuid.UUID, error)`, `SearchByFTS(ctx context.Context, f memory.SearchFilter) ([]uuid.UUID, error)`, and `SearchByVector(ctx context.Context, f memory.SearchFilter, queryVec []float32) ([]uuid.UUID, error)` (executing `SET LOCAL hnsw.iterative_scan = relaxed_order` and `SET LOCAL hnsw.max_scan_tuples = 20000` inside the read transaction).
    - `LoadProjectGraphSnapshot(ctx context.Context, tenantID uuid.UUID, projectID *uuid.UUID, now time.Time) ([]memory.Memory, []memory.MemoryRelation, map[uuid.UUID][]float32, error)` and `TraverseCausalSubgraph(ctx context.Context, tenantID uuid.UUID, seeds []uuid.UUID, hops int, now time.Time) ([]memory.CausalWalkRow, error)`.

**T003 — Interfaces**
- Consumes: Domain engines/ports (`T001`) and PostgreSQL repositories (`T002`).
- Produces:
  - `neural.BGEM3Client` implementing `memory.EmbeddingProvider` over persistent HTTP (`keep-alive`, `DisableCompression: true`) with L2 normalization and typed `ErrProviderUnavailable`.
  - `cachel1.CognitiveGraph` implementing `memory.GraphMutationNotifier` (`ApplyMemoryUpsert`, `ApplyMemoryStatusChange`, `ApplyRelationsAdded`, `ApplyEmbeddingUpsert`) and `ProjectAuthorizedSubgraph(ctx context.Context, auth identity.AuthContext, filter memory.SearchFilter, allowedTasks map[string]bool, now time.Time) (graph.AuthorizedSubgraph, map[uuid.UUID]memory.Memory, map[uuid.UUID][]float32, bool, error)` plus `Stats() cachel1.GraphStats`.
  - `jobs.EmbeddingWorker` (`Run(ctx context.Context)` and `ProcessOnce(ctx context.Context) (int, error)`) processing `embed_and_nli` (`priority = 100`) jobs, persisting `halfvec(1024)` embeddings, updating `cachel1.CognitiveGraph`, and applying exponential backoff up to `max_attempts` before `dead_letter`.

**T004 — Interfaces**
- Consumes: `T001` explicit relation validators/ports, `T002` repositories, `T003` `GraphMutationNotifier`.
- Produces:
  - Extended `capture.CaptureCommand` with `Relations []memory.ExplicitRelationInput`.
  - Synchronous `CaptureMemory` validates read visibility on every `rel.ToMemoryID` inside the capture transaction (`ErrNotFound` -> `404` opaque + out-of-rollback `denied` audit if missing or unreadable; `ErrValidation` -> `422` if self-loop or invalid relation type), persists `memory_relations`, and notifies `GraphMutationNotifier` upon commit across `CaptureMemory`, `UpdateMemory`, `SupersedeMemory`, and `ArchiveMemory`.
  - Extended `requests.ServiceExecutor` for opaque `task_shared` captures with `Relations` (re-validating target read/scope membership in worker execution) and `feedback.Service.RecordFeedback` notifying `GraphMutationNotifier` on Bayesian updates and `< 0.30` quarantine transitions.

**T005 — Interfaces**
- Consumes: `T001` ranking/graph/submodular engines, `T002` search repositories, `T003` `BGEM3Client` & `cachel1.CognitiveGraph`.
- Produces:
  - `retrieval.Service.SearchMemories(ctx context.Context, auth identity.AuthContext, q SearchMemoriesQuery) (*SearchMemoriesResult, error)` (`errgroup` parallel query embedding + candidate/L1 graph resolution, Zero-Trust pre-pruned `PropagateActivation`, `RankCandidates`, compact item projection without `Content`, `DegradedMode` flag, and `RecordAllowed` audit without mutating `memories`).
  - `retrieval.Service.GetSubgraph(ctx context.Context, auth identity.AuthContext, q GetSubgraphQuery) (*SubgraphResult, error)` (`404` opaque + `denied` audit if seed missing or unreadable; 1–3 hop bidirectional traversal over authorized nodes/edges).
  - `context.Service.BuildContext(ctx context.Context, auth identity.AuthContext, req BuildContextRequest) (*BuildContextResult, error)` (`SelectSubmodularContext` under `MaxTokens`, progressive disclosure `Content` vs `Summary`, `<romero_memory ... untrusted_content="true">` wrapping, and `RecordAllowed` audit without mutating `memories`).

**T006 — Interfaces**
- Consumes: `T003` (`BGEM3Client`, `CognitiveGraph`, `EmbeddingWorker`), `T004` (`CaptureService`, `FeedbackService`), `T005` (`RetrievalService`, `ContextBuilderService`).
- Produces:
  - Huma v2 routes in `internal/interfaces/rest`:
    - `POST /api/v1/memories` (extended request body with optional `relations`)
    - `POST /api/v1/memories/search`
    - `POST /api/v1/memories/context`
    - `GET /api/v1/memories/{id}/subgraph`
    - `GET /api/v1/system/status` (extended response with `active_profile`, `l1_graph_cache_enabled`, `l1_nodes_count`, `l1_edges_count`)
  - CLI subcommand `romero eval` in `internal/interfaces/cli` + canonical dataset `tests/evaluation/golden_dataset.json` computing and asserting `NDCG@5 >= 0.85`, `MultiHopRecall@K >= 0.85`, `StaleMemorySuppression == 1.00`, and `CrossScopeLeakage == 0.00`.

## Review Workload Forecast

| Dimension | Forecast | Why |
| --- | --- | --- |
| Estimated changed lines | ~1,600 – 2,200 LOC | Motores matemáticos puros (`ranking`, `submodular`, `spreading_activation`), consultas `sqlc` de búsqueda híbrida/embeddings/cola, Caché L1 en RAM, cliente `BGE-M3`, worker priorizado, ampliación de `capture`/`retrieval`/`context`, 3 rutas REST nuevas, `romero eval` + `golden_dataset.json` y suites de integración/seguridad. |
| Files / areas | ~22 archivos en 6 áreas | `internal/domain/{memory,graph}/`, `internal/infrastructure/{persistence,neural,cachel1}/`, `internal/application/{jobs,capture,feedback,retrieval,context}/`, `internal/interfaces/{rest,cli}/`, `tests/{evaluation,integration,security}/`. |
| Risk | Max tier `1` (`0` tareas tier-2; `7` tareas tier-1 verificadas con `npx @ericrisco/rsc risk`) | Ninguna tarea modifica `migrations/`, `internal/domain/identity/`, `configs/romero.toml` ni `docker-compose.yml`; cada tarea pasa por 1 revisor de contexto fresco (`Tier 1`) más controles obligatorios Nivel 1 y Nivel 2 (`romero eval`). |
| Suggested delivery | `ask-on-risk` (por tarea con rama/PR vinculada a su GitHub Issue) | Supera `line_budget: 400` de `02-DOCS/wiki/sdd/config.yaml`; se ejecuta tarea por tarea (`T001` → `T008`) siguiendo la política de GitHub Issues (`02-DOCS/wiki/ftd/github-issues-policy.md`). |

