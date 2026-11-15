---
type: plan
title: Plan — Phase 03 NLI, Arbitraje de Conflictos, Gobernador de Potencia y Consolidación en Sueño REM
description: The structure-level implementation plan for phase-03-nli-governor-rem — contracts, shapes, flows, testing strategy, sequencing and risks.
tags: [sdd, plan, romero, phase-03, nli, conflicts, reranker, power-governor, rem-consolidation, zero-trust]
timestamp: 2026-11-15T14:32:00-03:00
topic: sdd
slug: phase-03-nli-governor-rem
status: approved
---

# Plan — Phase 03 NLI, Arbitraje de Conflictos, Gobernador de Potencia y Consolidación en Sueño REM

> Spec: [../specs/phase-03-nli-governor-rem.md](../specs/phase-03-nli-governor-rem.md) · Constitution: [../constitution.md](../constitution.md) · Status: approved
> Last updated: 2026-11-15

## 0. Global Constraints

- **Lenguaje y compilación (`constitution §1 #1`):** Go `1.23+` estricto con `CGO_ENABLED=0` obligatorio en compilación del binario (`CGO_ENABLED=0 go build -trimpath ./cmd/romero`). Cero enlaces a librerías `.dll`, `.so` o `.dylib`.
- **Stack canónico (`constitution §1 #2, #3, #4`):** HTTP REST con `net/http` + `Huma v2` (OpenAPI 3.1) bajo `/api/v1`; CLI con `Cobra`; migraciones con `Goose`; acceso a datos con `pgx/v5` (`pgxpool`) + consultas compiladas estáticamente por `sqlc` sobre `PostgreSQL 17` (`pgcrypto`, `vector` 0.8.0+ con `halfvec(1024)`, `pg_trgm`, `btree_gist`). Clientes de inferencia local (`BGE-M3`, `bge-reranker-v2-m3`, `mDeBERTa-v3`, `Qwen2.5-3B-Instruct`) vía HTTP persistente (`keep-alive`, `DisableCompression: true`) detrás de puertos de dominio en `internal/domain/memory/ports.go`. Prohibido usar ORMs con reflexión o concatenar SQL fuera de `.sql` compilados por `sqlc`.
- **Fronteras de arquitectura limpia (`constitution §3 #8`):** `internal/domain` (`identity`, `memory`, `graph`, `governor`, `provenance`, `security`) contiene exclusivamente Go puro de la librería estándar (y `github.com/google/uuid`), sin importar `infrastructure`, `interfaces`, drivers SQL ni clientes HTTP. Dirección de dependencias invariante: `interfaces → application → domain ← infrastructure`.
- **Calidad y verificación en dos niveles (`constitution §2 #5, #6, #7`):**
  - **Nivel 1 (obligatorio en cada tarea):** Cero errores y cero advertencias en `gofmt -s -w .`, `go vet ./...`, `golangci-lint run` y `sqlc generate` sin diffs; `go test -race ./...` siguiendo TDD (`red → green → refactor`) con cobertura de líneas $\ge 80\%$ en `internal/domain` e `internal/application`.
  - **Nivel 2 (`romero eval`, obligatorio en rutas cognitivas y cierre):** Ejecución reproducible en CPU sobre `tests/evaluation/golden_dataset.json` verificando $\text{NDCG}@5 \ge 0.85$, $\text{Recall}@K$ multi-salto $\ge 0.85$, tasa de detección de contradicciones NLI (`contradiction_catch_rate`) $> 0.90$, ratio de destilación REM (`rem_distillation_ratio`) $> 0.0$, supresión de obsolescencia $1.0$ y tasa de fuga entre ámbitos exactamente $0.00\%$.
- **Aislamiento Zero-Trust por partición de ámbito y opacidad (`constitution §5 #14`, `spec §Acceptance AC-04, AC-05, AC-07`):**
  - Una **partición de ámbito exacta** queda definida por la tupla `(tenant_id, project_id, scope_type, owner_agent_id, task_id)` (misma relación de equivalencia `MemoryScopeTarget.SameTopicPartition` de Fase 01).
  - **En NLI asíncrono (`AC-02`, `AC-03`, `AC-04`):** La búsqueda de los Top-5 vecinos vigentes ($\text{similitud coseno} \ge 0.82$) para evaluación NLI filtra obligatoriamente dentro de la **misma partición de ámbito exacta** usando `SET LOCAL hnsw.iterative_scan = relaxed_order` y `SET LOCAL hnsw.max_scan_tuples = 20000`. Jamás se comparan ni enlazan recuerdos de distintos agentes privados (`agent_private`) ni de distintas tareas (`task_shared`).
  - **En `ListConflicts` y `ReviewConflict` (`AC-04`, `AC-05`):** Un conflicto (`relation_type = 'contradicts'`, `is_active = TRUE`) solo es visible en `ListConflicts` si el solicitante posee permiso de lectura vigente sobre **ambos** recuerdos extremos (`from_memory_id` y `to_memory_id`). En `ReviewConflict` (`POST /api/v1/conflicts/{id}/resolve`), si el conflicto no existe, ya está inactivo o el solicitante carece de permiso de lectura sobre cualquiera de los dos extremos, devolver `404 Not Found` opaco idéntico y registrar `audit_events(outcome = 'denied')` fuera del rollback; si tiene lectura sobre ambos pero carece de permiso de escritura en el ámbito o intenta dejar como perdedor a un recuerdo `verified` sin `is_admin = true`, devolver `403 Forbidden` y auditar `denied`. `ReviewConflict` es siempre síncrono (prohibido encolarlo en `opaque_requests`).
  - **En Consolidación de Sueño REM (`AC-07`):** Los episodios (`memory_type = 'episodic'`, `status = 'active'`, `confidence >= 0.70`) se agrupan estrictamente dentro de la **misma partición de ámbito exacta**, y el recuerdo destilado (`procedural` o `semantic`) hereda esa misma partición de ámbito. `POST /api/v1/admin/consolidate` (`TriggerREM`) exige `auth.IsAdmin == true` o (`auth.PrincipalType == "orchestrator"` con permiso de escritura sobre el `project_id` solicitado); cualquier otro principal recibe `403 Forbidden` + auditoría `denied`.
- **Constantes matemáticas y umbrales canónicos de Fase 03 (`spec §Behaviour`, `AC-01`..`AC-08`, `constitution §5 #15, §6 #17, #18`):**
  - **Fusión de Reordenamiento Cruzado (`AC-01`, `Master Spec §12.3`):** Sobre hasta los Top-$30$ candidatos tras *Spreading Activation*, el logit $z_{\text{ce}}(Q, m)$ del Cross-Encoder se transforma con $\sigma(z) = \frac{1}{1 + e^{-z}}$ y se combina con la activación normalizada $\hat{a}_m^{(H)} \in [0, 1]$:
    $$S_{\text{sem}}(m, Q) = 0.70 \cdot \sigma\left(z_{\text{ce}}(Q, m)\right) + 0.30 \cdot \hat{a}_m^{(H)}$$
    Cuando el perfil no es `ultra` o el proveedor de reranking no está disponible / falla, $S_{\text{sem}}(m, Q) = \hat{a}_m^{(H)}$ sin abortar la lectura.
  - **Umbrales de Inferencia Lógica NLI (`AC-02`, `AC-03`, `Master Spec §7.4, §11.1`):**
    - Similitud coseno mínima para seleccionar hasta Top-$5$ vecinos vigentes (`active` o `conflicted`, excluyendo la misma `topic_key` ya supercedida): $\text{cosine\_sim} \ge 0.82$ (distancia coseno $\le 0.18$).
    - **Implicación / Soporte:** Si $P(\text{Entailment}) \ge 0.85$, crear relación activa `supports` (`from_memory_id = newID`, `to_memory_id = neighborID`, `weight = P(Entailment)`), incrementar $\alpha_{\text{neighbor}} \leftarrow \alpha_{\text{neighbor}} + 0.50$ y actualizar versión/estado en `memories`.
    - **Contradicción:** Si $P(\text{Contradiction}) \ge 0.80$, crear relación activa `contradicts` (`from_memory_id = newID`, `to_memory_id = neighborID`, `weight = P(Contradiction)`, `nli_entailment`, `nli_contradiction`). Determinación del recuerdo que pasa a `status = 'conflicted'`:
      1. Comparar rango epistemológico $\text{Rank}(\text{verified})=5 > \text{Rank}(\text{observed})=4 > \text{Rank}(\text{reported})=3 > \text{Rank}(\text{inferred})=2 > \text{Rank}(\text{hypothesis})=1$: el de menor rango pasa a `conflicted`.
      2. En caso de empate de rango (y siempre que no sea `verified`), el de menor confianza bayesiana esperada $\hat{\theta} = \frac{\alpha}{\alpha + \beta}$ pasa a `conflicted` (y si empatan en $\hat{\theta}$, el recuerdo recién evaluado `newID` pasa a `conflicted`).
      3. **Protección absoluta de `verified`:** Un recuerdo con `epistemic_status = 'verified'` jamás pasa automáticamente a `conflicted`; si ambos son `verified`, ambos permanecen en `active` unidos por la arista activa `contradicts` para arbitraje administrativo en `ReviewConflict`.
  - **Arbitraje de Conflictos en `ReviewConflict` (`AC-05`):**
    - El parámetro `{id}` de `POST /api/v1/conflicts/{id}/resolve` es el UUID de la relación activa `contradicts`.
    - `winner_memory_id` debe coincidir con `from_memory_id` o `to_memory_id` del conflicto (`422 Unprocessable Entity` en caso contrario).
    - El recuerdo ganador pasa/permanece en `status = 'active'`, recibe $+\Delta\alpha = 1.0$ (y si el llamador envía `resolved_summary`/`resolved_content` o es administrador marcando verificación, crea una nueva versión inmutable cerrando la versión previa); el perdedor pasa a `status = 'superseded'` cerrando su `valid_until = now`; la arista `contradicts` pasa a `is_active = false` con `valid_until = now`; se inserta una arista activa `supersedes` (`from = winner`, `to = loser`); y se sincroniza *write-through* la Caché L1.
  - **Gobernador de Potencia y Preempción en $< 10\text{ ms}$ (`AC-06`, `constitution §6 #18`):**
    - Perfiles soportados: `lite`, `balanced`, `ultra`. Estados operativos: `guard` (reposo normal), `burst` (tráfico activo concurrente), `rem_sleep` (consolidación en segundo plano).
    - Ante cualquier operación entrante de lectura o escritura de agentes mientras el estado es `rem_sleep`, `PowerGovernor.NotifyAgentTraffic()` cancela síncronamente el `context.CancelFunc` activo del ciclo REM en $< 10\text{ ms}$, transiciona inmediatamente a `guard`/`burst`, y despacha en una goroutine asíncrona no bloqueante la liberación de VRAM del SLM (`keep_alive = 0`).
  - **Agrupamiento y Validación Cruzada en Sueño REM (`AC-07`, `Master Spec §13.1`):**
    - Elegibilidad de episodios: `memory_type = 'episodic'`, `status = 'active'`, `confidence >= 0.70` y vector denso presente, particionados por ámbito idéntico.
    - Clúster válido: tamaño $m \ge 3$ episodios con similitud coseno media por pares $\ge 0.80$.
    - Validación lógica cruzada antes de persistir el recuerdo destilado $M_{\text{proc}}$: $P(\text{Entailment}) \ge 0.80$ frente a los episodios fuente del clúster y $P(\text{Contradiction}) < 0.80$ frente a todos los recuerdos `verified` activos de la partición/proyecto.
    - Estado epistemológico de $M_{\text{proc}}$: `'observed'` si todos los episodios fuente tenían `epistemic_status IN ('verified', 'observed')`, o `'inferred'` en caso contrario; `source_type = 'rem_consolidation'`; aristas activas `derived_from` desde $M_{\text{proc}}$ hacia cada episodio $e_i$; transición de cada $e_i$ a `status = 'consolidated'`.
- **Git y autoría (`constitution §3 #10, §4 #11, #12`):** Trabajo en ramas dedicadas con Pull Request, mensajes de commit con `gitmoji` + Conventional Commits y autoría exclusivamente humana (prohibido `Co-Authored-By`).

## 1. Context & constraints

- **Criterios de aceptación que gobiernan el diseño:**
  - Reordenamiento cruzado Top-$30$ ($0.70 \cdot \sigma(z) + 0.30 \cdot \hat{a}$) y caída limpia a activación del grafo: `spec §Acceptance AC-01`.
  - Detección automática NLI de soporte ($\ge 0.85 \rightarrow \text{supports}$, $+\Delta\alpha = 0.5$) y contradicción ($\ge 0.80 \rightarrow \text{contradicts}$, `conflicted` en menor autoridad protegiendo `verified`) dentro de la misma partición de ámbito: `spec §Acceptance AC-02, AC-03, AC-04`.
  - Listado Zero-Trust de contradicciones (`ListConflicts`), resolución atómica (`ReviewConflict` con opacidad `404`) y limpieza automática de aristas `contradicts` al archivar/superceder un extremo fuera de `ReviewConflict`: `spec §Acceptance AC-04, AC-05`.
  - `PowerGovernor` (`guard` / `burst` / `rem_sleep`) con preempción síncrona en $< 10\text{ ms}$ y descarga asíncrona de VRAM: `spec §Acceptance AC-06`, `constitution §6 #18`.
  - Consolidación en Sueño REM (`TriggerREM` para `is_admin` u `orchestrator`, y `romero consolidate`) con agrupamiento $\ge 3$ episodios ($\text{confianza} \ge 0.70$, similitud media $\ge 0.80$), validación NLI cruzada ($\ge 0.80$), aristas `derived_from` y paso a `consolidated`: `spec §Acceptance AC-07`.
  - Evaluación cognitiva reproducible de Nivel 2 en CPU (`romero eval`) verificando las 6 métricas canónicas: `spec §Acceptance AC-08`, `constitution §2 #7`.
- **Barras no funcionales:** Latencia de búsqueda en tiempo real $p_{95} < 18\text{ ms}$ (`constitution §6 #17`); preempción de contexto REM en $< 10\text{ ms}$ y descarga asíncrona de VRAM (`constitution §6 #18`); $0.00\%$ de fuga entre ámbitos (`constitution §5 #14`).
- **Fuera de alcance (`spec §Non-goals`):** Servidor MCP de 5 herramientas (`stdio` y `HTTP/SSE`) y cliente SDK para Gandalf Wizard (Fase 04); encolamiento opaco en `ReviewConflict`; comparación NLI cruzando agentes privados o tareas distintas.

## 2. Architecture

```text
+-------------------------------------------------------------------------------------------------------+
|                               ROMERO Single Static Binary (Go 1.23+, CGO_ENABLED=0)                   |
|                                                                                                       |
|  +-----------------------------+                     +---------------------------------------------+  |
|  | CLI Adapter (INTERNAL)      |                     | REST API Adapter (INTERNAL - Huma v2)       |  |
|  | migrate / bootstrap /       |                     | GET  /api/v1/conflicts        (List)        |  |
|  | principal / serve /         |                     | POST /api/v1/conflicts/{id}/resolve         |  |
|  | consolidate / eval          |                     | POST /api/v1/admin/consolidate (TriggerREM) |  |
|  +--------------+--------------+                     | GET  /api/v1/system/status    (+Governor)   |  |
|                 |                                    +----------------------+----------------------+  |
|                 |                                                           |                         |
|                 |                                    +----------------------v----------------------+  |
|                 |                                    | PowerGovernor Preemption Hook (INTERNAL)    |  |
|                 |                                    | Cancels active REM ctx in <10ms on traffic; |  |
|                 |                                    | dispatches async SLM VRAM unload            |  |
|                 |                                    +----------------------+----------------------+  |
|                 v                                                           v                         |
|  +-------------------------------------------------------------------------------------------------+  |
|  | Application Layer (INTERNAL)                                                                    |  |
|  | - RetrievalService      (+Top-30 Cross-Encoder Rerank in Ultra profile with graceful fallback)  |  |
|  | - EmbeddingAndNLIWorker (BGE-M3 embed -> Top-5 same-partition HNSW -> NLI -> supports/conflict) |  |
|  | - ConflictService       (ListConflicts Zero-Trust + atomic ReviewConflict + L1 sync)            |  |
|  | - CaptureService        (+auto-close active contradicts edges on Supersede / Archive)           |  |
|  | - REMConsolidationSvc   (Episodic clustering -> SLM 3B distill -> NLI cross-check -> persist)   |  |
|  +-------------------+-----------------------------------+-----------------------------------------+  |
|                      |                                   |                                            |
|                      v                                   v                                            |
|  +-----------------------------------------+   +---------------------------------------------------+  |
|  | Domain Core (INTERNAL - Pure Go)        |   | L1 Cognitive Graph Cache (INTERNAL - In-RAM)      |  |
|  | - memory/ranking:       CrossEncoder    |   | - Write-through node/edge upsert & deactivation   |  |
|  | - memory/nli:           Authority rules |   | - Zero-Trust pre-pruned subgraph projection       |  |
|  | - memory/consolidation: Clustering/NLI  |   +---------------------------------------------------+  |
|  | - governor:             State machine   |                                                          |
|  +-------------------+---------------------+                                                          |
|                      ^                                                                                |
|                      | implements ports                                                               |
|  +-------------------+-----------------------------------+-----------------------------------------+  |
|  | Infrastructure Adapters (INTERNAL)                    |                                         |  |
|  | - Persistence Adapter (pgx/v5 + sqlc + HNSW scan)     | - Neural HTTP Clients (Keep-Alive Pool) |  |
|  +-------------------------------------------------------+-----------------------------------------+  |
+----------------------------------------------------------|--------------------------------------------+
                             | TCP (pgxpool)               | HTTP/1.1 & HTTP/2 Keep-Alive
                             v                             v
        +----------------------------------------+   +-----------------------------------------------+
        | PostgreSQL 17 + pgvector 0.8.0+        |   | Local Neural Inference Daemons (OPTIONAL/EXT) |
        | (EXTERNAL: memories, memory_relations, |   | - BGE-M3 Embeddings (halfvec 1024)            |
        |  memory_embeddings, memory_jobs)       |   | - bge-reranker-v2-m3 (Cross-Encoder Top-30)   |
        +----------------------------------------+   | - mDeBERTa-v3 (NLI Entailment/Contradiction)  |
                                                     | - Qwen2.5-3B-Instruct (REM SLM + keep_alive)  |
                                                     +-----------------------------------------------+
```

- **CLI Adapter (`internal/interfaces/cli`, INTERNAL):** Añade el subcomando `romero consolidate` (`--project-id`, `--dry-run`) y amplía `romero eval` con las métricas `contradiction_catch_rate` y `rem_distillation_ratio`.
- **REST API Adapter (`internal/interfaces/rest`, INTERNAL):** Expone `GET /api/v1/conflicts`, `POST /api/v1/conflicts/{id}/resolve`, `POST /api/v1/admin/consolidate`, amplía `GET /api/v1/system/status` con el estado del `PowerGovernor` e intercepta tráfico de agentes para disparar la preempción de Sueño REM en $< 10\text{ ms}`.
- **PowerGovernor (`internal/domain/governor`, INTERNAL):** Máquina de estados concurrente (`guard`, `burst`, `rem_sleep`) que registra el pulso de solicitudes entrantes, coordina el contexto cancelable del ciclo REM activo y dispara en segundo plano la descarga de VRAM (`UnloadVRAM`) al abortar un ciclo REM.
- **EmbeddingAndNLIWorker (`internal/application/jobs`, INTERNAL):** Extiende `EmbeddingWorker` para que, tras persistir el vector `halfvec(1024)`, busque los Top-$5$ vecinos vigentes ($\text{similitud} \ge 0.82$) dentro de la misma partición de ámbito, evalúe pares en `NLIProvider`, inserte relaciones `supports` o `contradicts` (aplicando la regla de autoridad sobre `conflicted`), marque `nli_checked = true` y sincronice la Caché L1.
- **ConflictService (`internal/application/conflicts`, INTERNAL):** Orquesta el listado filtrado por visibilidad de ambos extremos (`ListConflicts`) y la transacción atómica de arbitraje (`ReviewConflict`) con sincronización *write-through* a la Caché L1.
- **REMConsolidationService (`internal/application/consolidation`, INTERNAL):** Agrupa por densidad coseno ($\ge 3$ episodios activos, $\text{confianza} \ge 0.70$, similitud media $\ge 0.80$) por partición de ámbito, invoca al sintetizador `SLMConsolidationProvider`, ejecuta la validación NLI cruzada contra episodios fuente y recuerdos `verified`, y persiste atómicamente cada recuerdo destilado (`procedural`/`semantic`) con sus aristas `derived_from` y transición de episodios a `consolidated` (abortando sin mutar el grupo en curso si el `PowerGovernor` cancela el contexto).
- **Domain Core (`internal/domain/{memory,governor}`, INTERNAL):** Contiene las funciones puras de fusión Cross-Encoder, decisión de autoridad NLI, clustering voraz por densidad coseno, validación de destilación y transiciones del `PowerGovernor`.
- **Neural Adapters (`internal/infrastructure/neural`, INTERNAL):** Clientes HTTP persistentes precalentados para `RerankerProvider`, `NLIProvider` y `SLMConsolidationProvider`, más implementaciones deterministas en memoria para pruebas y `romero eval` en CPU.

**Top architectural decision:** **Ejecutar la consolidación de Sueño REM con transacciones ACID acotadas *por clúster* bajo un `context.Context` gobernado por `PowerGovernor`, verificando el estado del contexto antes de abrir la transacción de escritura y despachando `keep_alive = 0` en una goroutine desacoplada cuando entra tráfico de agentes.**
- *Alternativa descartada:* Ejecutar todo el ciclo de consolidación de un proyecto dentro de una única transacción SQL larga y descargar sincrónicamente la VRAM del modelo generativo antes de atender la petición HTTP del agente.
- *Por qué se elige este diseño:* Mantener una transacción abierta mientras el SLM genera texto retendría bloqueos sobre filas de `memories` durante cientos de milisegundos, y esperar sincrónicamente la descarga de $2.2\text{ GB}$ de VRAM violaría los Principios 17 y 18 ($< 10\text{ ms}$ de preempción y $p_{95} < 18\text{ ms}$). Al separar la fase de inferencia (cancelable instantáneamente vía `ctx.Cancel()`) de la transacción corta de persistencia por clúster y liberar VRAM en segundo plano, cualquier petición de agente preempa el cómputo en microsegundos y jamás observa un clúster a medio consolidar (`AC-06`, `AC-07`).

## 3. Interfaces & contracts

```text
memory.RerankerProvider.Rerank(ctx: Context, query: string, candidates: []RerankCandidate) -> map[UUID]float32 | ProviderUnavailableError
  - invariant: evalúa hasta los Top-30 candidatos devolviendo el logit crudo z_ce(Q, m) por cada MemoryID usando conexiones HTTP keep-alive.
  - postcondition: si el proveedor está deshabilitado o falla, devuelve ProviderUnavailableError y el llamador conserva S_sem = a_hat sin fallar la búsqueda (AC-01).

memory.NLIProvider.EvaluatePairs(ctx: Context, pairs: []NLIPairInput) -> []NLIScore | ProviderUnavailableError
  - invariant: para cada par (Premise, Hypothesis), devuelve probabilidades normalizadas (Entailment, Neutral, Contradiction) en [0, 1].
  - postcondition: si el proveedor no está disponible durante embed_and_nli, el worker conserva el embedding persistido con nli_checked=false (AC-02, AC-03).

memory.SLMConsolidationProvider.DistillCluster(ctx: Context, episodes: []Memory) -> DistilledMemoryDraft | ProviderUnavailableError | ContextCanceledError
memory.SLMConsolidationProvider.UnloadVRAM(ctx: Context) -> error
  - invariant: DistillCluster respeta cancelación inmediata de ctx (<10ms); produce Summary (<=600 chars), Content estructurado (Precondiciones, Síntoma, Causa Raíz, Pasos, Verificación), MemoryType ('procedural' o 'semantic'), TopicKey sugerida, Tags y Entities.
  - postcondition: UnloadVRAM solicita descarga de VRAM (keep_alive=0) fuera del camino crítico de lectura/escritura (AC-06, AC-07).

governor.PowerGovernor.BeginAgentRequest() -> func()
governor.PowerGovernor.RunInREMSleep(ctx: Context, fn: func(remCtx: Context) error) -> error
governor.PowerGovernor.Status() -> GovernorSnapshot
  - invariant: thread-safe; BeginAgentRequest() incrementa el contador de tráfico en vuelo, actualiza last_activity_at, y si el estado actual es 'rem_sleep', invoca inmediatamente remCancel() (<10ms), cambia el estado a 'guard'/'burst' y dispara UnloadVRAM en una goroutine asíncrona (AC-06).

memory.EvaluateNLIDecision(newMem: Memory, existingMem: Memory, score: NLIScore) -> NLIDecision
  - invariant: función pura en internal/domain/memory; si score.Entailment >= 0.85 devuelve Action='supports' (AlphaDelta=+0.5 sobre existingMem); si score.Contradiction >= 0.80 devuelve Action='contradicts' y calcula ConflictedMemoryID según jerarquía epistemológica (verified > observed > reported > inferred > hypothesis), desempatando por menor confianza bayesiana y luego newMem.ID, con ConflictedMemoryID=Nil cuando ambos son 'verified' (AC-02, AC-03).

memory.ClusterEpisodicMemories(episodes: []Memory, vectors: map[UUID][]float32) -> []EpisodicCluster
  - invariant: función pura en internal/domain/memory; filtra episodios con Type=='episodic', Status=='active', Confidence>=0.70, los particiona por MemoryScopeTarget.SameTopicPartition y forma clústeres deterministas de tamaño >= 3 con similitud coseno media por pares >= 0.80 (AC-07).

conflicts.Service.ListConflicts(ctx: Context, auth: AuthContext, filter: ConflictFilter) -> []ConflictView | UnauthorizedError | ForbiddenError
  - invariant: lectura pura; devuelve únicamente relaciones 'contradicts' con is_active==true donde auth posee permiso de lectura vigente sobre AMBOS recuerdos (from_memory_id y to_memory_id) (AC-04).

conflicts.Service.ReviewConflict(ctx: Context, auth: AuthContext, cmd: ReviewConflictCommand) -> ReviewConflictResult | UnauthorizedError | ForbiddenError | NotFoundError | ValidationError
  - invariant: operación transaccional ACID síncrona + actualización write-through de Caché L1; nunca usa cola opaca.
  - precondition: si el conflicto cmd.ConflictID no existe, no está activo o auth no puede leer ambos extremos -> audita 'denied' y devuelve NotFoundError (404 opaco); si auth no puede escribir en el ámbito o el perdedor es 'verified' y !auth.IsAdmin -> audita 'denied' y devuelve ForbiddenError (403); si cmd.WinnerMemoryID no es ni from_memory_id ni to_memory_id -> devuelve ValidationError (422) (AC-05).
  - postcondition: ganador queda en status='active' con alpha_score += 1.0 (y nueva versión si cmd incluye contenido/resumen fusionado); perdedor pasa a status='superseded' con valid_until=now; arista 'contradicts' pasa a is_active=false, valid_until=now; se inserta arista activa 'supersedes' (winner -> loser); se actualiza Caché L1 y se registra AuditEvent(outcome='allowed', operation='review_conflict').

consolidation.Service.Consolidate(ctx: Context, auth: AuthContext, cmd: ConsolidateCommand) -> ConsolidationReport | UnauthorizedError | ForbiddenError | ValidationError
  - invariant: cada clúster validado se persiste en su propia transacción corta tras verificar remCtx.Err()==nil; si el ciclo es preempado o falla la validación NLI cruzada, los episodios del clúster no confirmado permanecen intactos en status='active' (AC-06, AC-07).
  - precondition: auth.IsAdmin == true OR (auth.PrincipalType == 'orchestrator' AND cmd.ProjectID != nil AND auth.CanAccessProject(*cmd.ProjectID) AND auth.HasScopePermission('write')) -> de lo contrario audita 'denied' y devuelve ForbiddenError (403).
  - postcondition: para cada clúster válido cuyo borrador supere entailment >= 0.80 contra sus episodios fuente y contradiction < 0.80 contra recuerdos 'verified' de la partición, crea el recuerdo destilado en la misma partición de ámbito con source_type='rem_consolidation', inserta aristas 'derived_from' hacia cada episodio fuente, actualiza cada episodio fuente a status='consolidated' y sincroniza la Caché L1.
```

## 4. Data model & flow

**Entities**

- **ConflictRecord (proyección sobre `memory_relations` + `memories`):** Identificado por `ConflictID = memory_relations.id` (`relation_type = 'contradicts'`, `is_active = true`), contiene `TenantID`, `ProjectID`, `FromMemory` (vista compacta), `ToMemory` (vista compacta), `Weight`, `NLIEntailment`, `NLIContradiction`, `ValidFrom`, `Notes` y `CreatedAt`.
- **EpisodicCluster (en memoria de aplicación/dominio):** Partición de ámbito (`ScopeTarget`) + lista ordenada de $\ge 3$ recuerdos `episodic` activos con `MeanPairwiseSimilarity >= 0.80`.
- **DistilledMemoryDraft (salida de `SLMConsolidationProvider`):** `MemoryType` (`procedural` | `semantic`), `TopicKey`, `Summary` ($\le 600$ chars), `Content`, `Tags`, `Entities`.
- **GovernorSnapshot (estado en `PowerGovernor` y `GET /api/v1/system/status`):** `Profile` (`lite` | `balanced` | `ultra`), `State` (`guard` | `burst` | `rem_sleep`), `InFlightRequests`, `LastActivityAt`, `PreemptionsCount`, `RerankerAvailable`, `NLIAvailable`, `SLMAvailable`.

**Primary flow 1** (Enriquecimiento asíncrono `embed_and_nli` y arbitraje en `ReviewConflict`)

1. `EmbeddingAndNLIWorker` reclama un trabajo `embed_and_nli` (`priority = 100`), genera el vector `halfvec(1024)` en `BGE-M3`, hace upsert en `memory_embeddings` y actualiza el vector en la Caché L1.
2. Si `NLIProvider` está configurado, consulta en PostgreSQL (`FindSameScopeSemanticNeighbors`) hasta los $5$ recuerdos vigentes (`status IN ('active', 'conflicted')`, distinto `id` y distinta `topic_key`) de la **misma partición exacta de ámbito** cuya similitud coseno con el nuevo vector sea $\ge 0.82$.
3. Envía los pares `(vecino, nuevo)` a `NLIProvider.EvaluatePairs`:
   - Si `EvaluateNLIDecision` dictamina `supports` ($P(\text{Entailment}) \ge 0.85$): dentro de una transacción ACID inserta/actualiza la relación activa `supports`, incrementa `alpha_score += 0.5` del recuerdo respaldado y sincroniza la Caché L1.
   - Si `EvaluateNLIDecision` dictamina `contradicts` ($P(\text{Contradiction}) \ge 0.80$): dentro de una transacción ACID inserta la relación activa `contradicts` con `nli_entailment` y `nli_contradiction`, cambia a `status = 'conflicted'` el recuerdo de menor autoridad (salvo que sea `verified`) generando su versión inmutable, y sincroniza la Caché L1.
4. Marca `nli_checked = true` en el recuerdo procesado y completa el trabajo en `memory_jobs`.
5. Cuando un principal autorizado consulta `GET /api/v1/conflicts`, `ConflictService.ListConflicts` filtra las relaciones `contradicts` activas cuyos dos extremos sean legibles para su `AuthContext` y `task_permissions` vigentes.
6. Cuando el principal invoca `POST /api/v1/conflicts/{id}/resolve`, `ConflictService.ReviewConflict` bloquea la fila de la relación y ambos recuerdos (`FOR UPDATE`), verifica permisos de lectura (`404` opaco si falta) y escritura/admin (`403` si falta), aplica la transición atómica (`winner -> active` con $+\Delta\alpha = 1.0$, `loser -> superseded`, `contradicts -> is_active = false`, nueva arista `supersedes`), sincroniza la Caché L1 y devuelve el conflicto resuelto.

**Primary flow 2** (Consolidación en Sueño REM con preempción en $< 10\text{ ms}$)

1. Un administrador u orquestador invoca `POST /api/v1/admin/consolidate` (`TriggerREM`) o `romero consolidate` (o el temporizador de inactividad del `PowerGovernor` detecta reposo prolongado).
2. `PowerGovernor.RunInREMSleep` cambia el estado a `rem_sleep` y crea un `remCtx` cancelable. Si durante la ejecución entra cualquier petición de agente en `REST Adapter`, `PowerGovernor.BeginAgentRequest()` cancela `remCtx` en $< 10\text{ ms}` y lanza en segundo plano `SLMConsolidationProvider.UnloadVRAM`.
3. `REMConsolidationService` carga los candidatos `episodic` activos ($\text{confianza} \ge 0.70$) del proyecto y sus vectores, los particiona por ámbito idéntico y forma clústeres con $\ge 3$ episodios y similitud media $\ge 0.80$.
4. Por cada clúster (si `!cmd.DryRun`):
   - Invoca `SLMConsolidationProvider.DistillCluster(remCtx, cluster.Episodes)`.
   - Invoca `NLIProvider.EvaluatePairs(remCtx, ...)` para verificar que el borrador destilado tenga $P(\text{Entailment}) \ge 0.80$ frente a los episodios fuente y $P(\text{Contradiction}) < 0.80$ frente a los recuerdos `verified` vigentes de la misma partición/proyecto.
   - Si `remCtx.Err() == nil` y la validación NLI es exitosa, abre una transacción ACID corta que inserta la fuente `rem_consolidation`, el nuevo recuerdo `procedural`/`semantic` (en la misma partición de ámbito), su versión `v1`, las aristas `derived_from` hacia cada episodio fuente, cambia los episodios fuente a `status = 'consolidated'` (con su versión inmutable) y encola la vectorización del nuevo recuerdo; al confirmar la transacción, sincroniza *write-through* la Caché L1.

- **Consistency boundaries:**
  - La creación de aristas NLI (`supports`/`contradicts`) + actualización de estado `conflicted` / `alpha_score` es **transaccional (ACID)** por recuerdo evaluado.
  - La resolución en `ReviewConflict` y la limpieza automática de aristas `contradicts` en `SupersedeMemory`/`ArchiveMemory` son **transaccionales (ACID)** con sincronización *write-through* inmediata en la Caché L1.
  - En Sueño REM, cada clúster validado se confirma en una **transacción ACID independiente y corta** una vez terminadas las llamadas al SLM y al clasificador NLI, garantizando que una preempción a mitad de camino no deje episodios en estado inconsistente ni bloquee tablas durante la inferencia.
- **Migration impact:** **Ninguna migración SQL adicional requerida (`none`).** El esquema fundacional `migrations/00001_romero_core_schema.sql` ya incluye `memories.nli_checked`, `idx_memories_rem_candidates`, `memory_relations.{nli_entailment, nli_contradiction, is_active, valid_until}`, `source_type_enum` (`rem_consolidation`, `human_review`) y todos los estados/tipos requeridos. Todo el acceso a datos se implementa mediante nuevas consultas estáticas en `internal/infrastructure/persistence/queries/*.sql` compiladas con `sqlc generate`.

## 5. Testing strategy

| Acceptance criterion | Level | Asserts | Fakes / mocks |
| --- | --- | --- | --- |
| `spec §Acceptance AC-01` (Reordenamiento cruzado Top-30 y caída limpia) | unit + integration + contract | Con Cross-Encoder activo en perfil `ultra`, $S_{\text{sem}} = 0.70 \cdot \sigma(z_{\text{ce}}) + 0.30 \cdot \hat{a}$ invierte el orden a favor del candidato lógicamente relevante sobre el ruido léxico; cuando el proveedor falla o el perfil es `lite`/`balanced`, usa $S_{\text{sem}} = \hat{a}$ sin error. | En unit: funciones puras; en integration/contract: PostgreSQL 17 real + servidor HTTP `httptest` simulando `bge-reranker-v2-m3` sano y caído. |
| `spec §Acceptance AC-02` (Detección automática NLI de soporte en la misma partición) | unit + integration | Al procesar `embed_and_nli` de $M_2$ con vecino $M_1$ ($\text{cos} \ge 0.82$, $P(\text{Entailment}) \ge 0.85$) en la misma partición de ámbito, crea arista activa `supports`, incrementa $\alpha(M_1)$ en $+0.5$, marca `nli_checked = true` y actualiza la Caché L1. | PostgreSQL 17 real + cliente HTTP `httptest` para `mDeBERTa-v3`. |
| `spec §Acceptance AC-03` (Detección automática NLI de contradicción entre distintas `topic_key` y protección de `verified`) | unit + integration | Con $P(\text{Contradiction}) \ge 0.80$ entre $M_1$ (`verified`/`observed`, `topic_key = K1`) y $M_2$ (`reported`/`hypothesis`, `topic_key = K2`), crea arista `contradicts`, pasa $M_2$ a `conflicted`, mantiene $M_1$ en `active`, y verifica que cuando ambos son `verified` ninguno pasa a `conflicted`. | En unit: tabla exhaustiva de rangos epistemológicos y desempates bayesianos; en integration: PostgreSQL 17 real + `httptest` NLI. |
| `spec §Acceptance AC-04` (Aislamiento Zero-Trust en NLI y `ListConflicts` con $0.00\%$ de fuga) | unit + integration | Recuerdos con texto idéntico/contradictorio en `agent_private` de distintos agentes o `task_shared` de distintas tareas jamás generan aristas NLI cruzadas; `GET /api/v1/conflicts` nunca devuelve conflictos donde el llamador carezca de lectura sobre cualquiera de los dos extremos. | PostgreSQL 17 real con matriz multi-tenant, multi-agente y `task_permissions` revocados (`tests/security`). |
| `spec §Acceptance AC-05` (Arbitraje atómico en `ReviewConflict`, opacidad `404` y limpieza al archivar/superceder) | unit + integration + contract | `POST /api/v1/conflicts/{id}/resolve` deja al ganador `active` ($+\Delta\alpha = 1.0$), al perdedor `superseded`, cierra `contradicts`, crea `supersedes` y actualiza L1; devuelve `404` opaco (+ audita `denied`) si el llamador no puede leer ambos extremos, `403` si carece de escritura o intenta derrotar un `verified` sin ser admin, y `422` si `winner_memory_id` es ajeno al par; al archivar o superceder un extremo fuera de `ReviewConflict`, la arista `contradicts` se cierra automáticamente. | PostgreSQL 17 real + `httptest`. |
| `spec §Acceptance AC-06` (`PowerGovernor` y preempción en $< 10\text{ ms}$ durante Sueño REM) | unit + integration | Mientras `Consolidate` ejecuta la síntesis de un clúster en estado `rem_sleep`, una petición entrante cancela `remCtx` en $< 10\text{ ms}`, cambia el estado a `guard`/`burst`, invoca `UnloadVRAM` asíncronamente y garantiza que $0$ episodios del clúster interrumpido pasen a `consolidated`. | Proveedor SLM simulado con latencia controlada + medición de tiempo de cancelación + PostgreSQL 17 real. |
| `spec §Acceptance AC-07` (Consolidación de episodios en Sueño REM con validación NLI cruzada y `derived_from`) | unit + integration + contract | Con $\ge 3$ episodios activos ($\text{confianza} \ge 0.70$, similitud media $\ge 0.80$) en la misma partición, `TriggerREM` (por admin u `orchestrator` autorizado; `403` para `agent`) y `romero consolidate` crean el recuerdo `procedural` en la misma partición con fuente `rem_consolidation`, enlazan `derived_from` y pasan los episodios a `consolidated`; si la validación NLI cruzada falla ($< 0.80$ entailment o contradice un `verified`), descarta el borrador y deja los episodios en `active`. | PostgreSQL 17 real + proveedores SLM/NLI en `httptest` y en memoria. |
| `spec §Acceptance AC-08` (Evaluación cognitiva reproducible Nivel 2 con métricas NLI y REM) | e2e (`romero eval`) | `go run ./cmd/romero eval --dataset tests/evaluation/golden_dataset.json` verifica $\text{NDCG}@5 \ge 0.85$, $\text{Recall}@K$ multi-salto $\ge 0.85$, `contradiction_catch_rate > 0.90`, `rem_distillation_ratio > 0.0`, `obsolete_suppression_rate == 1.0` y `zero_trust_leakage_rate == 0.0` (`pass: true`). | Dataset canónico `tests/evaluation/golden_dataset.json` + motores deterministas en CPU. |

- **Dónde se sitúa la línea E2E y por qué:** El límite superior abarca (1) pruebas de integración y seguridad HTTP REST contra **PostgreSQL 17 + `pgvector` real** ejercitando el flujo completo de captura $\rightarrow$ worker NLI $\rightarrow$ `ListConflicts` $\rightarrow$ `ReviewConflict` y el flujo `TriggerREM` con preempción, y (2) el benchmark reproducible **`romero eval`** (Nivel 2 Cognitivo, Principio 7).
- **Qué DEBE ser una dependencia real (sin mocks):** **PostgreSQL 17 con `pgvector` (`halfvec` HNSW)** en las pruebas de vecindario semántico NLI por partición de ámbito, transacciones de arbitraje de conflictos, limpieza automática al archivar/superceder y persistencia transaccional de Sueño REM (`AC-02`..`AC-07`).

## 6. Sequencing & dependencies

1. **Motores puros de dominio y máquina de estados del gobernador (`internal/domain/memory/{ranking,nli,consolidation,ports}.go`, `internal/domain/governor/power_state.go`): fusión Cross-Encoder ($0.70 \cdot \sigma(z) + 0.30 \cdot \hat{a}$), reglas de decisión NLI y jerarquía de autoridad para `conflicted` (protegiendo `verified`), clustering episódico por densidad coseno ($\ge 3$ episodios, similitud media $\ge 0.80$) y validación lógica cruzada REM, máquina de estados `PowerGovernor` (`guard` / `burst` / `rem_sleep`) con preempción en $< 10\text{ ms}$, y puertos de dominio (`RerankerProvider`, `NLIProvider`, `SLMConsolidationProvider`, `ConflictRepository`, `ConsolidationRepository`) con su suite unitaria TDD** — depends on: none — `[parallelizable con #2]`
2. **Consultas SQL compiladas (`internal/infrastructure/persistence/queries/{conflicts,consolidation,memories}.sql` + `sqlc generate`) y repositorios PostgreSQL (`internal/infrastructure/persistence/postgres/{conflicts_repo,consolidation_repo,repositories}.go`): vecindario semántico Top-5 ($\text{cos} \ge 0.82$) restringido a la misma partición de ámbito con `hnsw.iterative_scan = relaxed_order`, inserción/cierre de aristas `supports`/`contradicts`, listado y bloqueo `FOR UPDATE` de conflictos, limpieza automática de contradicciones al archivar/superceder y consultas de candidatos/actualización de Sueño REM** — depends on: none — `[parallelizable con #1]`
3. **Clientes HTTP persistentes de inferencia neuronal (`internal/infrastructure/neural/{reranker,deberta_nli,qwen_slm}.go`), soporte de desactivación de aristas en la Caché Cognitiva L1 (`internal/infrastructure/cachel1/graph_cache.go`) y ampliación de `EmbeddingWorker` (`internal/application/jobs/embedding_worker.go`) para ejecutar la verificación lógica NLI asíncrona post-embedding (`AC-02`, `AC-03`, `AC-04`)** — depends on: #1, #2 — `[serial]`
4. **Servicio de listado y arbitraje de conflictos (`internal/application/conflicts/service.go`), limpieza automática de aristas `contradicts` en `CaptureService` (`SupersedeMemory`, `topic_key`, `ArchiveMemory`) e integración del reordenamiento Cross-Encoder Top-30 en `RetrievalService` (`SearchMemories` y `BuildContext`) (`AC-01`, `AC-04`, `AC-05`)** — depends on: #1, #2, #3 — `[parallelizable con #5]`
5. **Servicio de consolidación en Sueño REM (`internal/application/consolidation/service.go`) integrado con `PowerGovernor` (`RunInREMSleep`, preempción en $< 10\text{ ms}`, descarga asíncrona de VRAM, validación NLI cruzada, creación de recuerdos `procedural`/`semantic` con aristas `derived_from` y transición a `consolidated`) (`AC-06`, `AC-07`)** — depends on: #1, #2, #3 — `[parallelizable con #4]`
6. **Rutas HTTP REST Huma v2 (`GET /api/v1/conflicts`, `POST /api/v1/conflicts/{id}/resolve`, `POST /api/v1/admin/consolidate`, middleware de preempción del `PowerGovernor` y métricas de gobernador en `GET /api/v1/system/status`), subcomando CLI `romero consolidate` y ampliación de `romero eval` + `tests/evaluation/golden_dataset.json` con métricas NLI y REM (`AC-01`..`AC-08`)** — depends on: #4, #5 — `[serial]`
7. **Suite completa de pruebas de integración E2E, seguridad Zero-Trust en NLI/conflictos/REM ($0.00\%$ fuga), preempción de Sueño REM en $< 10\text{ ms}$ y verificación final Nivel 1 + Nivel 2 (`romero eval`) contra PostgreSQL 17 real (`tests/integration`, `tests/security`)** — depends on: #6 — `[serial]`

- **Candidatos paralelos:**
  - Los pasos **#1** (`internal/domain/{memory,governor}/**`) y **#2** (`internal/infrastructure/persistence/**`) operan sobre paquetes disjuntos sin estado compartido.
  - Los pasos **#4** (`internal/application/{conflicts,capture,retrieval}/**`) y **#5** (`internal/application/consolidation/**`) operan sobre casos de uso independientes una vez construidos los puertos, repositorios y clientes neuronales en **#1–#3**.
- **Restricciones duras de orden:** **#3** requiere los puertos de **#1** y los repositorios de **#2**; **#4** y **#5** dependen de **#1–#3**; **#6** conecta ambos servicios en los adaptadores REST/CLI/`eval`; **#7** ejecuta la verificación integral sobre todo el sistema ensamblado.

## 7. Risks & open decisions

**Risks**

| Risk | Trigger | Impact | Mitigation / spike to retire it |
| --- | --- | --- | --- |
| **Fuga de información entre agentes o tareas por emparejamiento NLI o consolidación REM** | Una consulta de vecinos vectoriales o de episodios REM filtra solo por `project_id` sin igualar `scope_type`, `owner_agent_id` y `task_id`. | Un agente privado o una tarea descubre la existencia de recuerdos de otro agente/tarea a través de `ListConflicts` o de un procedimiento destilado (viola `AC-04`, `AC-07` y Principio 14). | Filtrar estrictamente por la partición exacta `(tenant_id, project_id, scope_type, owner_agent_id, task_id)` tanto en SQL (`FindSameScopeSemanticNeighbors`, `ListREMConsolidationCandidates`) como en las funciones puras de dominio, probado en `tests/security`. |
| **Bloqueo de lecturas/escrituras de agentes durante la síntesis en Sueño REM** | La llamada al modelo generativo SLM o la descarga de VRAM se ejecuta dentro de una transacción de base de datos o bloquea el mutex de peticiones entrantes. | Incumplimiento del presupuesto de preempción ($< 10\text{ ms}$, Principio 18) y de latencia en tiempo real ($p_{95} < 18\text{ ms}$, Principio 17). | Ejecutar la inferencia SLM y la validación NLI fuera de cualquier transacción SQL bajo un `context.Context` que `PowerGovernor.BeginAgentRequest()` cancela atómicamente en $< 10\text{ ms}$, despachando `UnloadVRAM` en una goroutine separada. |
| **Conflictos huérfanos en `ListConflicts` tras archivar o superceder un recuerdo** | Un recuerdo involucrado en una relación activa `contradicts` es reemplazado por `SupersedeMemory` o retirado por `ArchiveMemory`, dejando la arista `contradicts` con `is_active = true`. | `ListConflicts` y las alertas de búsqueda siguen reportando un conflicto contra un recuerdo ya obsoleto o archivado (viola `AC-05`). | Cerrar automáticamente toda arista `contradicts` activa asociada al recuerdo supercedido/archivado dentro de la misma transacción de `CaptureService` y restaurar el recuerdo par a `active` si no tiene otras contradicciones activas ni cuarentena bayesiana, sincronizando la Caché L1. |
| **Alucinación del SLM durante la consolidación en Sueño REM** | El modelo generativo produce un procedimiento que contradice una regla `verified` del proyecto o no se desprende de los episodios fuente. | Envenenamiento de memoria procedimental (Riesgo §19 de la Especificación Maestra). | Validación lógica cruzada obligatoria con `NLIProvider` ($P(\text{Entailment}) \ge 0.80$ frente a episodios fuente y $P(\text{Contradiction}) < 0.80$ frente a recuerdos `verified`) antes de abrir la transacción de persistencia (`AC-07`). |

**Open decisions**

- **Ninguna (`none`):** Todas las decisiones de alcance, fronteras Zero-Trust por partición de ámbito, jerarquía de autoridad en contradicciones NLI, arbitraje síncrono en `ReviewConflict`, autorización en `TriggerREM` y preempción en `< 10 ms` quedaron cerradas en `specify` y `clarify` y registradas en `02-DOCS/wiki/sdd/decisions.md`.

## Tasks
<!-- generated by tasks on 2026-11-15; IDs are stable, do not renumber -->

| ID | [P] | Task | Done-check | Depends-on | Trace | Risk |
| --- | --- | --- | --- | --- | --- | --- |
| T001 | [P] | Implement and unit-test pure domain engines and state machine: Cross-Encoder fusion (`0.70*sigmoid(z) + 0.30*a_hat`), NLI decision & epistemic authority rules (`supports`/`contradicts`, `verified` protection), episodic cosine-density clustering (`>= 3` episodes, mean sim `>= 0.80`) & REM cross-check policy, `PowerGovernor` (`guard`/`burst`/`rem_sleep` with `< 10ms` preemption), and Phase 03 ports (`internal/domain/memory`, `internal/domain/governor`) | `go test -race -cover ./internal/domain/memory/... ./internal/domain/governor/...` exits 0 with statement coverage >= 85% | — | AC-01, AC-02, AC-03, AC-06, AC-07 | 2 |
| T002 | [P] | Write compiled SQL queries (`internal/infrastructure/persistence/queries/{conflicts,consolidation,memories}.sql`), run `sqlc generate`, and implement PostgreSQL repositories for same-partition HNSW semantic neighbors (`cos >= 0.82`, `hnsw.iterative_scan = relaxed_order`), `supports`/`contradicts` edge lifecycle, Zero-Trust `ListConflicts` & `FOR UPDATE` `ReviewConflict`, orphan `contradicts` cleanup on archive/supersede, and REM candidate/persistence transactions (`internal/infrastructure/persistence`) | `sqlc generate` produces zero diffs AND `go vet ./internal/infrastructure/persistence/...` AND `go test -race -cover ./internal/infrastructure/persistence/...` exit 0 | — | AC-02, AC-03, AC-04, AC-05, AC-07 | 1 |
| T003 |  | Implement and unit-test persistent HTTP neural clients (`RerankerProvider`, `NLIProvider`, `SLMConsolidationProvider` with `UnloadVRAM`) in `internal/infrastructure/neural`, edge deactivation / `supports`/`contradicts` sync in `cachel1.CognitiveGraph`, and asynchronous NLI neighbor evaluation inside `EmbeddingWorker` (`internal/application/jobs`) | `go test -race -cover ./internal/infrastructure/neural/... ./internal/infrastructure/cachel1/... ./internal/application/jobs/...` exits 0 with coverage >= 85% | T001, T002 | AC-01, AC-02, AC-03, AC-04, AC-06, AC-07 | 1 |
| T004 | [P] | Implement `ConflictService` (`ListConflicts` Zero-Trust dual-end visibility & atomic `ReviewConflict` with `404` opacity, `403` admin/write guard, `422` validation, and L1 sync), add automatic `contradicts` cleanup on `SupersedeMemory`/`ArchiveMemory` in `CaptureService`, and integrate Top-30 Cross-Encoder reranking with clean fallback in `RetrievalService` (`internal/application/conflicts`, `internal/application/capture`, `internal/application/retrieval`) | `go test -race -cover ./internal/application/conflicts/... ./internal/application/capture/... ./internal/application/retrieval/...` exits 0 with coverage >= 85% | T001, T002, T003 | AC-01, AC-04, AC-05 | 1 |
| T005 | [P] | Implement `REMConsolidationService` (`internal/application/consolidation`) integrated with `PowerGovernor.RunInREMSleep`, per-partition episodic clustering, SLM distillation, mandatory NLI cross-check (`entailment >= 0.80`, `contradiction < 0.80` vs `verified`), per-cluster ACID commit (`rem_consolidation` source, `derived_from` edges, `consolidated` episodes), and clean `< 10ms` preemption rollback | `go test -race -cover ./internal/application/consolidation/...` exits 0 with coverage >= 85% | T001, T002, T003 | AC-06, AC-07 | 1 |
| T006 |  | Wire Huma v2 REST routes (`GET /api/v1/conflicts`, `POST /api/v1/conflicts/{id}/resolve`, `POST /api/v1/admin/consolidate`, `PowerGovernor` preemption hook, and governor metrics in `GET /api/v1/system/status`), add `romero consolidate` CLI command, and extend `romero eval` + `tests/evaluation/golden_dataset.json` with `contradiction_catch_rate` and `rem_distillation_ratio` (`internal/interfaces/rest`, `internal/interfaces/cli`, `internal/application/eval`, `tests/evaluation`) | `go test -race -cover ./internal/interfaces/rest/... ./internal/interfaces/cli/... ./internal/application/eval/... ./cmd/romero/...` exits 0 with coverage >= 80% in `rest`/`cli` AND `go run ./cmd/romero eval --dataset tests/evaluation/golden_dataset.json` exits 0 | T004, T005 | AC-01, AC-02, AC-03, AC-04, AC-05, AC-06, AC-07, AC-08 | 1 |
| T007 |  | Implement and run full Phase 03 integration, Zero-Trust security (`0.00%` leakage in NLI/conflicts/REM), `< 10ms` REM preemption, and `ReviewConflict`/orphan-cleanup suites against live PostgreSQL 17 (`tests/integration`, `tests/security`) | `go test -p 1 -count=1 -race -v ./tests/integration/... ./tests/security/...` exits 0 against live PostgreSQL 17 covering AC-01 through AC-07 with 0.00% cross-scope leakage | T006 | AC-01, AC-02, AC-03, AC-04, AC-05, AC-06, AC-07 | 1 |
| T008 |  | All done-checks pass + full Level 1 & Level 2 (`romero eval`) verification green | Every row `T001`–`T007` checked AND `sqlc generate`, `gofmt -s -w .`, `CGO_ENABLED=0 go build -trimpath ./cmd/romero`, `go vet ./...`, `golangci-lint run`, `go test -p 1 -count=1 -race -coverprofile=coverage.out ./...`, and `romero eval` exit 0 with >= 80% coverage in `domain`/`application` | T001, T002, T003, T004, T005, T006, T007 | all ACs | — |

### GitHub Issues (`CodeMonkReaper/romero`)

| Task | Issue | Title | Blocked by |
| --- | --- | --- | --- |
| `T001` | [#44](https://github.com/CodeMonkReaper/romero/issues/44) | `[Phase 03][T001] Motores puros de dominio (Cross-Encoder, NLI, REM) y PowerGovernor` | — |
| `T002` | [#45](https://github.com/CodeMonkReaper/romero/issues/45) | `[Phase 03][T002] Consultas sqlc y repositorios PG17 de vecindario NLI, conflictos y Sueño REM` | — |
| `T003` | [#46](https://github.com/CodeMonkReaper/romero/issues/46) | `[Phase 03][T003] Clientes neuronales HTTP (Reranker, NLI, SLM), L1 edge sync y EmbeddingAndNLIWorker` | `#44`, `#45` |
| `T004` | [#47](https://github.com/CodeMonkReaper/romero/issues/47) | `[Phase 03][T004] ConflictService (List/ReviewConflict), limpieza de conflictos en Capture y Reranker en Retrieval` | `#44`, `#45`, `#46` |
| `T005` | [#48](https://github.com/CodeMonkReaper/romero/issues/48) | `[Phase 03][T005] REMConsolidationService con PowerGovernor, validación NLI cruzada y derived_from` | `#44`, `#45`, `#46` |
| `T006` | [#49](https://github.com/CodeMonkReaper/romero/issues/49) | `[Phase 03][T006] Rutas REST de conflictos/consolidación, hook de PowerGovernor, CLI consolidate y romero eval NLI/REM` | `#47`, `#48` |
| `T007` | [#50](https://github.com/CodeMonkReaper/romero/issues/50) | `[Phase 03][T007] Suite de integración E2E, seguridad Zero-Trust (NLI/conflictos/REM) y preempción < 10 ms en PG17` | `#49` |
| `T008` | [#51](https://github.com/CodeMonkReaper/romero/issues/51) | `[Phase 03][T008] Verificación final Nivel 1 y Nivel 2 (romero eval) de Phase 03` | `#44`–`#50` |

### Per-Task Interfaces

**T001 — Interfaces**
- Consumes: Go standard library + `github.com/google/uuid` + existing `internal/domain/{identity,memory,provenance}` types.
- Produces:
  - `memory.RerankCandidate{MemoryID uuid.UUID, Text string}`, `memory.RerankerProvider` (`Rerank(ctx context.Context, query string, candidates []RerankCandidate) (map[uuid.UUID]float32, error)`), and `memory.FuseCrossEncoderScore(rerankLogit float32, graphActivation float32, rerankAvailable bool) float32` (`0.70*sigmoid(z) + 0.30*a_hat`, or `a_hat` when unavailable).
  - `memory.NLIPairInput{Premise string, Hypothesis string}`, `memory.NLIScore{Entailment float32, Neutral float32, Contradiction float32}`, `memory.NLIProvider` (`EvaluatePairs(ctx context.Context, pairs []NLIPairInput) ([]NLIScore, error)`), and `memory.EvaluateNLIDecision(newMem, existingMem memory.Memory, score memory.NLIScore) memory.NLIDecision` (`supports` if `Entailment >= 0.85`, `contradicts` if `Contradiction >= 0.80` selecting `ConflictedMemoryID` by epistemic rank -> Bayesian confidence -> `newMem.ID`, never `verified`).
  - `memory.EpisodicCluster{PartitionKey string, ScopeTarget identity.MemoryScopeTarget, Episodes []memory.Memory, MeanSimilarity float32}`, `memory.DistilledMemoryDraft{MemoryType memory.MemoryType, TopicKey string, Summary string, Content string, Tags []string, Entities []string}`, `memory.SLMConsolidationProvider` (`DistillCluster(ctx context.Context, episodes []memory.Memory) (memory.DistilledMemoryDraft, error)`, `UnloadVRAM(ctx context.Context) error`), `memory.ClusterEpisodicMemories(episodes []memory.Memory, vectors map[uuid.UUID][]float32) []memory.EpisodicCluster`, and `memory.ValidateDistilledNLI(sourceScores []memory.NLIScore, verifiedScores []memory.NLIScore) bool`.
  - `governor.Profile` (`lite`, `balanced`, `ultra`), `governor.State` (`guard`, `burst`, `rem_sleep`), `governor.Snapshot`, and `governor.PowerGovernor` (`BeginAgentRequest() func()`, `RunInREMSleep(ctx context.Context, fn func(context.Context) error) error`, `Status() governor.Snapshot`).
  - Domain repository ports in `internal/domain/memory/ports.go`: `ConflictRepository`, `ConsolidationRepository`, `SemanticNeighborRepository`.

**T002 — Interfaces**
- Consumes: `migrations/00001_romero_core_schema.sql`..`00003_task_authorization_and_opaque_requests.sql` and domain ports from `T001`.
- Produces:
  - Compiled `sqlc` queries and `postgres` repository methods:
    - `FindSameScopeSemanticNeighbors(ctx context.Context, mem memory.Memory, queryVec []float32, minSimilarity float32, limit int, now time.Time) ([]memory.Memory, error)` (using `SET LOCAL hnsw.iterative_scan = relaxed_order` and `SET LOCAL hnsw.max_scan_tuples = 20000`, matching exact partition `(tenant_id, project_id, scope_type, owner_agent_id, task_id)`).
    - `ListActiveConflicts(ctx context.Context, tenantID uuid.UUID, projectID *uuid.UUID, limit, offset int, now time.Time) ([]memory.ConflictPairRow, error)` and `GetConflictForUpdate(ctx context.Context, tenantID, conflictID uuid.UUID) (*memory.ConflictPairRow, error)`.
    - `DeactivateConflictEdge(ctx context.Context, tenantID, conflictID uuid.UUID, validUntil time.Time) error` and `DeactivateConflictsByMemoryID(ctx context.Context, tenantID, memoryID uuid.UUID, validUntil time.Time) ([]uuid.UUID, error)` (returns peer memory IDs to evaluate restoration to `active`).
    - `ListREMConsolidationCandidates(ctx context.Context, tenantID uuid.UUID, projectID *uuid.UUID, minConfidence float32) ([]memory.Memory, map[uuid.UUID][]float32, error)` and `ListActiveVerifiedByProject(ctx context.Context, tenantID uuid.UUID, projectID *uuid.UUID, now time.Time) ([]memory.Memory, error)`.

**T003 — Interfaces**
- Consumes: Domain engines/ports (`T001`) and PostgreSQL repositories (`T002`).
- Produces:
  - `neural.RerankerClient` (`memory.RerankerProvider`), `neural.DeBERTaNLIClient` (`memory.NLIProvider`), and `neural.QwenSLMClient` (`memory.SLMConsolidationProvider`) over persistent HTTP keep-alive pools, plus deterministic CPU reference implementations for `romero eval`.
  - `cachel1.CognitiveGraph` extended with `ApplyRelationDeactivated(tenantID uuid.UUID, projectID *uuid.UUID, relationID uuid.UUID)` so `ReviewConflict` and orphan cleanup retire `contradicts` edges immediately in RAM.
  - `jobs.EmbeddingWorker` extended with optional `NLIProvider` and `SemanticNeighborRepository`: after embedding upsert, evaluates Top-5 same-partition neighbors (`cos >= 0.82`), writes `supports`/`contradicts` + state/alpha updates in ACID tx, sets `nli_checked = true`, and syncs L1; if `NLIProvider` is nil/unavailable, completes embedding with `nli_checked = false`.

**T004 — Interfaces**
- Consumes: `T001` domain rules/ports, `T002` repositories, `T003` neural clients and L1 sync.
- Produces:
  - `conflicts.Service` (`ListConflicts(ctx, auth, filter)` and `ReviewConflict(ctx, auth, cmd)` enforcing `404` opacity, `403` write/admin guard, `422` winner check, atomic tx, L1 sync, and audit).
  - `capture.Service` updated on `SupersedeMemory`, `topic_key` supersede, and `ArchiveMemory` to close active `contradicts` edges and restore peer memories if eligible.
  - `retrieval.Service` extended with optional `RerankerProvider` (`Top-30` rerank fusion in `ultra` profile with clean fallback to graph activation).

**T005 — Interfaces**
- Consumes: `T001` clustering/NLI-validation/`PowerGovernor`, `T002` consolidation repository, `T003` `SLMConsolidationProvider` & `NLIProvider`.
- Produces:
  - `consolidation.Service.Consolidate(ctx, auth, cmd)` validating admin/orchestrator authorization, running inside `PowerGovernor.RunInREMSleep`, clustering episodes per exact scope partition, distilling via SLM outside SQL tx, cross-checking NLI (`entailment >= 0.80` vs sources, `contradiction < 0.80` vs `verified`), and committing per-cluster ACID transactions with `rem_consolidation` provenance, `derived_from` edges, `consolidated` source status, and L1 sync.

**T006 — Interfaces**
- Consumes: `T004` (`conflicts.Service`, `retrieval.Service`), `T005` (`consolidation.Service`), `T001` (`governor.PowerGovernor`).
- Produces:
  - Huma v2 routes: `GET /api/v1/conflicts`, `POST /api/v1/conflicts/{id}/resolve`, `POST /api/v1/admin/consolidate`, preemption middleware calling `PowerGovernor.BeginAgentRequest()`, and governor snapshot fields in `GET /api/v1/system/status`.
  - Cobra subcommand `romero consolidate` (`--project-id`, `--dry-run`).
  - Extended `eval.Report` (`contradiction_catch_rate > 0.90`, `rem_distillation_ratio > 0.0`) and `tests/evaluation/golden_dataset.json`.

**T007 — Interfaces**
- Consumes: Integrated REST router, services, `PowerGovernor`, `EmbeddingWorker`, and live PostgreSQL 17 (`ROMERO_TEST_DSN`).
- Produces: End-to-end integration and Zero-Trust security suites proving `AC-01`..`AC-07` (`tests/integration/nli_rem_e2e_test.go`, `tests/security/conflicts_rem_zero_trust_test.go`).

**T008 — Interfaces**
- Consumes: `T001`–`T007`.
- Produces: Verified Level 1 + Level 2 (`romero eval`) closure report and updated `02-DOCS/wiki/sdd/progress/phase-03-nli-governor-rem.md`.

## Review Workload Forecast

| Dimension | Forecast | Why |
| --- | --- | --- |
| Estimated changed lines | ~2,600–3,400 lines across 7 implementation tasks (~350–500 lines/task) | Domain engines + PowerGovernor (`T001`), sqlc/PG repos (`T002`), 3 neural clients + NLI worker (`T003`), ConflictService + Retrieval/Capture hooks (`T004`), REMConsolidationService (`T005`), REST/CLI/eval (`T006`), PG17 integration/security (`T007`) |
| Files / areas | ~30 files across `internal/domain/{memory,governor}`, `internal/infrastructure/{persistence,neural,cachel1}`, `internal/application/{jobs,conflicts,capture,retrieval,consolidation,eval}`, `internal/interfaces/{rest,cli}`, `tests/{integration,security,evaluation}` | Touches all clean-architecture layers without altering SQL migrations |
| Risk | Max **Tier 2** (1 Tier-2 task: `T001` touches `governor-infra`; 6 Tier-1 tasks: `T002`–`T007`) | `T001` requires 3-reviewer panel + human approval at PR (`branch: <name>` in spec frontmatter); `T002`–`T007` require 1 fresh-context reviewer each |
| Suggested delivery | `ask-on-risk` (per-task PRs with `T001`/`T002` and `T004`/`T005` parallelizable) | Keeps each PR near the `400`-line `config.yaml` review budget and isolates the single Tier 2 (`governor-infra`) change in `T001` |
