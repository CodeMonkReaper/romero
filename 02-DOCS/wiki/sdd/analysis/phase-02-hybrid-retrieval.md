---
type: analysis
title: Analysis — Phase 02 Hybrid Retrieval (Recuperación Híbrida, Grafo Causal y Empaquetado de Contexto)
description: Adversarial pre-implementation consistency gate cross-reading constitution, spec, plan and tasks for phase-02-hybrid-retrieval.
tags: [sdd, analysis, romero, phase-02, retrieval, graph, submodular, zero-trust]
timestamp: 2026-11-15T09:05:10-03:00
topic: sdd
slug: phase-02-hybrid-retrieval
status: pass
---

# Analysis — Phase 02 Hybrid Retrieval (Recuperación Híbrida, Grafo Causal y Empaquetado de Contexto)

> Constitution: [../constitution.md](../constitution.md) · Spec: [../specs/phase-02-hybrid-retrieval.md](../specs/phase-02-hybrid-retrieval.md) · Plan & Tasks: [../plans/phase-02-hybrid-retrieval.md](../plans/phase-02-hybrid-retrieval.md)

## 1. Verdict

**`GATE: PASS`** — `0 CRITICAL` · `0 HIGH` · `0 MEDIUM` · `3 LOW`

La lectura cruzada adversarial de los cuatro artefactos ([02-DOCS/wiki/sdd/constitution.md](file:///c:/Users/luis_/OneDrive/Documentos/Romero/02-DOCS/wiki/sdd/constitution.md), [02-DOCS/wiki/sdd/specs/phase-02-hybrid-retrieval.md](file:///c:/Users/luis_/OneDrive/Documentos/Romero/02-DOCS/wiki/sdd/specs/phase-02-hybrid-retrieval.md), [02-DOCS/wiki/sdd/plans/phase-02-hybrid-retrieval.md](file:///c:/Users/luis_/OneDrive/Documentos/Romero/02-DOCS/wiki/sdd/plans/phase-02-hybrid-retrieval.md) y su tabla `## Tasks` `T001`–`T008`) confirma cobertura completa ($12/12$ criterios de aceptación `AC-01`..`AC-12`), cero violaciones constitucionales, cero brechas (`0 GAP`) y cero deriva de alcance (`0 DRIFT`). Se registran 3 observaciones de severidad `LOW` (no bloqueantes) con sus ubicaciones exactas.

---

## 2. Requirement Coverage Map (`Spec → Plan → Tasks`)

| REQ-ID | Spec requirement (short) | Plan section | Task(s) | Status |
| :--- | :--- | :--- | :--- | :--- |
| `AC-01` | Fusión híbrida tri-modal ($\text{RRF}$, $k=60$) y divulgación progresiva en `SearchMemories` sin exponer jamás `content` | `§0` (`L32-38, L45`), `§3` (`L131-133, L148-153`), `§4` (`L174-190`), `§5` (`L200`) | `T001`, `T002`, `T005`, `T006`, `T007`, `T008` | `covered` |
| `AC-02` | Rescate causal multi-salto ($1..3$ saltos) mediante propagación de activación amortiguada ($\gamma = 0.35$) sobre relaciones vigentes | `§0` (`L33`), `§2` (`L103-118`), `§3` (`L125-129`), `§5` (`L201`) | `T001`, `T002`, `T003`, `T005`, `T006`, `T007`, `T008` | `covered` |
| `AC-03` | Poda Zero-Trust estricta pre-propagación ($0.00\%$ de fuga directa e indirecta y `relations_count` filtrado por visibilidad) | `§0` (`L27-30`), `§2` (`L115-118`), `§3` (`L142-146`), `§5` (`L202`), `§7` (`L235`) | `T001`, `T002`, `T003`, `T005`, `T006`, `T007`, `T008` | `covered` |
| `AC-04` | Gobernanza epistemológica ($W_{\text{ep}}$), esperanza bayesiana ($\hat{\theta}$) y decaimiento temporal por tipo cognitivo ($\lambda_{\text{type}}$) con desempate determinista | `§0` (`L34-36`), `§3` (`L131-134`), `§5` (`L203`) | `T001`, `T005`, `T007`, `T008` | `covered` |
| `AC-05` | Supresión por defecto de `superseded`/`consolidated`, exclusión total de `archived`/`rejected` y bandera `has_conflict_alert` | `§0` (`L33, L36`), `§3` (`L128, L133`), `§5` (`L204`) | `T001`, `T002`, `T004`, `T005`, `T006`, `T007`, `T008` | `covered` |
| `AC-06` | Coherencia inmediata *write-through* de la Caché Cognitiva L1 ante capturas, actualizaciones, supersesiones, cuarentenas ($<0.30$) y archivados | `§2` (`L97, L111, L115`), `§3` (`L143-146`), `§4` (`L193-195`), `§5` (`L205`), `§7` (`L236`) | `T003`, `T004`, `T005`, `T007`, `T008` | `covered` |
| `AC-07` | Empaquetado submodular voraz (*Facility Location*, $\eta=0.35, \rho=0.65$) bajo `max_tokens`, divulgación progresiva (`content` $\rightarrow$ `summary`) y XML `untrusted_content="true"` | `§0` (`L37`), `§3` (`L136-140, L160-164`), `§4` (`L191`), `§5` (`L206`) | `T001`, `T005`, `T006`, `T007`, `T008` | `covered` |
| `AC-08` | Worker transaccional priorizado (`priority = 100`, `FOR UPDATE SKIP LOCKED`), persistencia `halfvec(1024)` y reintentos con backoff hasta `dead_letter` | `§2` (`L100`), `§3` (`L166-169`), `§4` (`L196`), `§5` (`L207`) | `T002`, `T003`, `T007`, `T008` | `covered` |
| `AC-09` | Degradación elegante (`degraded_mode = true`) cuando el proveedor `BGE-M3` no responde o un recuerdo recién capturado tiene vector pendiente | `§0` (`L32, L37`), `§2` (`L112-118`), `§3` (`L120-123, L152, L164`), `§4` (`L184-187`), `§5` (`L208`), `§7` (`L237`) | `T001`, `T002`, `T003`, `T005`, `T006`, `T007`, `T008` | `covered` |
| `AC-10` | Exploración de vecindario causal en `GetSubgraph` ($1..3$ saltos) con opacidad `404 Not Found` y auditoría `denied` sobre semilla ajena/inexistente | `§0` (`L30, L41`), `§3` (`L155-158`), `§5` (`L209`) | `T001`, `T002`, `T003`, `T005`, `T006`, `T007`, `T008` | `covered` |
| `AC-11` | Declaración opcional de `relations` explícitas en `CaptureMemory` con verificación Zero-Trust de lectura sobre el destino (`404` opaco) y validación `422` | `§0` (`L30, L42-44`), `§2` (`L97`), `§4` (`L174, L193`), `§5` (`L210`) | `T001`, `T002`, `T004`, `T006`, `T007`, `T008` | `covered` |
| `AC-12` | Lectura pura sin mutar `access_count`, `last_accessed_at` ni `version` en `SearchMemories`, `BuildContext` y `GetSubgraph` | `§0` (`L45`), `§3` (`L149, L156, L161`), `§5` (`L211`) | `T002`, `T005`, `T006`, `T007`, `T008` | `covered` |

---

## 3. Six-Point Adversarial Check

1. **Constitution compliance (`constitution.md` vs. `spec` / `plan` / `tasks`):**
   - **Cumple al $100\%$.** Los Principios `1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17 y 19` de [02-DOCS/wiki/sdd/constitution.md](file:///c:/Users/luis_/OneDrive/Documentos/Romero/02-DOCS/wiki/sdd/constitution.md) están reflejados explícitamente en `plans/phase-02-hybrid-retrieval.md §0 Global Constraints` (`L19-46`), incluyendo `SET LOCAL hnsw.iterative_scan = relaxed_order` y `SET LOCAL hnsw.max_scan_tuples = 20000` (Principio 14), paralelismo `errgroup` y pool HTTP `keep-alive` con `DisableCompression: true` (Principio 17), divulgación progresiva sin `content` en búsqueda (Principio 9) y verificación en dos niveles con `romero eval` (Principio 7). El Principio `18` (preempción de VRAM en `Sueño REM`) y las piezas de perfil `Ultra` (`Cross-Encoder` y detector `mDeBERTa-v3` NLI) quedan explícitamente acotados para la Fase 03 en `specs/phase-02-hybrid-retrieval.md §Non-goals` (`L50-53`) y `§Points to clarify` (`L173-174`).
2. **Requirement coverage (`spec → plan → tasks`):**
   - **Cero brechas (`0 GAP`).** Los 12 criterios `AC-01`..`AC-12` cuentan con diseño en `§2`–`§4`, fila dedicada en `§5 Testing strategy` (`L200-211`) y tareas verificables en `## Tasks` (`T001`–`T008`).
3. **Scope drift (`tasks/plan → spec`):**
   - **Cero deriva (`0 DRIFT`).** Cada tarea `T001`–`T008` traza directamente a criterios aprobados en `specs/phase-02-hybrid-retrieval.md`.
4. **Contradiction (`spec ↔ plan ↔ tasks`):**
   - **Cero contradicciones.** Las reglas de opacidad (`404` vs. `403` vs. `422`), los rangos de consulta (`limit`, `hops`, `max_tokens`), el comportamiento en `degraded_mode = true` y la inmutabilidad en lecturas coinciden entre especificación, plan y tareas.
5. **Duplication:**
   - **Cero duplicaciones conflictivas.** Las tareas paralelas `[P]` (`T001` con `T002`; `T004` con `T005`) operan sobre paquetes disjuntos sin solapamiento de archivos.
6. **Ambiguity & Carrier completeness (chequeo de implementador aislado):**
   - **Completo.** `§0 Global Constraints` (`L19-46`) fija literalmente todas las constantes y fórmulas matemáticas, y `### Per-Task Interfaces` (`L257-310`) especifica firmas exactas `Consumes`/`Produces` para `T001`–`T006`.

---

## 4. Findings Table

| # | Severity | Type | Artifact A (loc) | Artifact B (loc) | Conflict / Observation | Resolve in (phase) |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| `F-01` | `LOW` | `Phased metric scope` | [02-DOCS/wiki/sdd/constitution.md#L36](file:///c:/Users/luis_/OneDrive/Documentos/Romero/02-DOCS/wiki/sdd/constitution.md#L36) (Principio 7 Nivel 2: menciona `detección NLI > 90%`) | [02-DOCS/wiki/sdd/specs/phase-02-hybrid-retrieval.md#L51](file:///c:/Users/luis_/OneDrive/Documentos/Romero/02-DOCS/wiki/sdd/specs/phase-02-hybrid-retrieval.md#L51) y [02-DOCS/wiki/sdd/plans/phase-02-hybrid-retrieval.md#L25-L26](file:///c:/Users/luis_/OneDrive/Documentos/Romero/02-DOCS/wiki/sdd/plans/phase-02-hybrid-retrieval.md#L25-L26) (`T006` `romero eval`) | El detector NLI (`mDeBERTa-v3`) está diferido por acuerdo explícito (Q2 de `specify` y `Points to clarify`) a la Fase 03; por tanto, en Fase 02 `romero eval` evalúa las 4 métricas de recuperación y seguridad (`NDCG@5`, `MultiHopRecall@K`, `StaleMemorySuppression`, `CrossScopeLeakage`), quedando la métrica NLI para activarse en Fase 03. | Ninguna (diferimiento explícito aprobado en `spec`) |
| `F-02` | `LOW` | `Interface naming refinement` | [02-DOCS/wiki/sdd/plans/phase-02-hybrid-retrieval.md#L125-L169](file:///c:/Users/luis_/OneDrive/Documentos/Romero/02-DOCS/wiki/sdd/plans/phase-02-hybrid-retrieval.md#L125-L169) (`§3 Interfaces & contracts`: nombres conceptuales de arquitectura) | [02-DOCS/wiki/sdd/plans/phase-02-hybrid-retrieval.md#L259-L309](file:///c:/Users/luis_/OneDrive/Documentos/Romero/02-DOCS/wiki/sdd/plans/phase-02-hybrid-retrieval.md#L259-L309) (`### Per-Task Interfaces`: firmas Go concretas por paquete) | `§3` describe los contratos a nivel agnóstico de estructura (`memory.Scorer.ComputeFinalScores`, `cachel1.GraphCache.ApplyMutation`), mientras que `Per-Task Interfaces` (`T001`–`T006`) aterriza los nombres exactos de funciones y tipos en Go (`memory.RankCandidates`, `cachel1.CognitiveGraph` implementando `memory.GraphMutationNotifier`). No hay contradicción funcional; los implementadores deben seguir las firmas literales de `Per-Task Interfaces`. | `implement` (usar las firmas literales de `### Per-Task Interfaces`) |
| `F-03` | `LOW` | `Granularity mapping` | [02-DOCS/wiki/sdd/plans/phase-02-hybrid-retrieval.md#L218-L223](file:///c:/Users/luis_/OneDrive/Documentos/Romero/02-DOCS/wiki/sdd/plans/phase-02-hybrid-retrieval.md#L218-L223) (`§6 Sequencing`: 6 pasos macro) | [02-DOCS/wiki/sdd/plans/phase-02-hybrid-retrieval.md#L246-L255](file:///c:/Users/luis_/OneDrive/Documentos/Romero/02-DOCS/wiki/sdd/plans/phase-02-hybrid-retrieval.md#L246-L255) (`## Tasks`: 8 tareas `T001`–`T008`) | `## Tasks` divide el paso macro `#4` (capa de aplicación) en dos tareas paralelas `[P]` sin archivos compartidos (`T004` para `capture`/`feedback`/`requests` y `T005` para `retrieval`/`context`) y añade la tarea de cierre `T008`. Es el desglose esperado de la fase `tasks`. | Ninguna (informativo) |

---

## 5. Recommended Routing

- **Veredicto:** **`GATE: PASS`** (`0 CRITICAL`, `0 HIGH`, `0 MEDIUM`, `3 LOW`).
- **Acción recomendada:** Aceptar las 3 observaciones `LOW`, registrar los tickets de ejecución `T001`–`T008` en GitHub Issues conforme a [github-issues-policy.md](file:///c:/Users/luis_/OneDrive/Documentos/Romero/02-DOCS/wiki/ftd/github-issues-policy.md) y avanzar a la fase **`implement`** (Etapa 4 de ASDD) comenzando en rama aislada.
