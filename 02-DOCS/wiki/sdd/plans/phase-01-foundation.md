---
type: plan
title: Plan — Phase 01 Foundation (Gobernanza Zero-Trust y Persistencia Tri-Temporal)
description: The structure-level implementation plan for phase-01-foundation — contracts, shapes, flows, testing strategy, sequencing and risks.
tags: [sdd, plan, romero, phase-01, zero-trust, bitemporal, bayesian]
timestamp: 2026-11-11T21:15:00-03:00
topic: sdd
slug: phase-01-foundation
status: approved
---

# Plan — Phase 01 Foundation (Gobernanza Zero-Trust y Persistencia Tri-Temporal)

> Spec: [../specs/phase-01-foundation.md](../specs/phase-01-foundation.md) · Constitution: [../constitution.md](../constitution.md) · Status: approved
> Last updated: 2026-11-11

## 0. Global Constraints

- **Lenguaje y compilación (`constitution §1 #1`):** Go `1.23+` estricto con `CGO_ENABLED=0` obligatorio en compilación y pruebas (`CGO_ENABLED=0 go build ./cmd/romero`). Cero enlaces a librerías `.dll`, `.so` o `.dylib`.
- **Stack canónico (`constitution §1 #2, #3`):** HTTP REST con `net/http` + `Huma v2` (OpenAPI 3.1) bajo el prefijo `/api/v1`; CLI con `Cobra`; migraciones SQL con `Goose`; acceso a datos con `pgx/v5` (`pgxpool`) + código compilado estáticamente por `sqlc` sobre `PostgreSQL 17` (`pgcrypto`, `vector` 0.8.0+, `pg_trgm`, `btree_gist`). Prohibido usar ORMs con reflexión o concatenar SQL fuera de `.sql` compilados por `sqlc`.
- **Fronteras de arquitectura limpia (`constitution §3 #8`):** `internal/domain` contiene exclusivamente Go puro de la librería estándar (y `uuid`), sin importar `infrastructure`, `interfaces`, drivers SQL ni clientes HTTP. Dirección de dependencias invariante: `interfaces → application → domain ← infrastructure`.
- **Calidad y verificación Nivel 1 (`constitution §2 #5, #6, #7`):** Cero errores y cero advertencias en `gofmt -s -w .`, `go vet ./...`, `golangci-lint run` y `sqlc generate`. Desarrollo TDD (`red → green → refactor`) ejecutando `go test -race ./...` con cobertura de líneas $\ge 80\%$ en `internal/domain` e `internal/application`.
- **Cinco niveles de ámbito (`scope_type`) y frontera de visibilidad/unicidad (`spec §What it must respect`, `AC-01`, `AC-02`):**
  - `agent_private`: visible y supercedible exclusivamente por `(tenant_id, project_id, owner_agent_id)`.
  - `task_shared`: requiere `task_id` no vacío (`422 Unprocessable Entity` si falta); visible y supercedible exclusivamente por `(tenant_id, project_id, task_id)`.
  - `project_shared`: visible y supercedible por principales autorizados en `(tenant_id, project_id)`.
  - `org_shared`: visible y supercedible en todo `(tenant_id)`.
  - `global_public`: lectura permitida en `(tenant_id)`; escritura/modificación restringida a principales administrativos (`user` / `orchestrator` con permiso admin).
- **Opacidad anti-enumeración Zero-Trust (`spec §Behaviour`, `AC-01`):**
  - Credencial ausente, expirada, inactiva o inválida $\rightarrow$ `401 Unauthorized` + registro en `audit_events`.
  - Intento de crear en un proyecto/ámbito sin permiso de escritura o solicitar `epistemic_status = verified` siendo `principal_type = agent` $\rightarrow$ `403 Forbidden` + `audit_events(outcome = 'denied')`.
  - Intento de leer o mutar por UUID (`GetMemory`, `UpdateMemory`, `SupersedeMemory`, `ArchiveMemory`, `RecordFeedback`) un recuerdo que pertenece a otro `tenant_id`, otro `project_id`, otro `owner_agent_id` (`agent_private`) u otro `task_id` (`task_shared`) $\rightarrow$ `404 Not Found` al cliente + `audit_events(outcome = 'denied')`.
- **Jerarquía epistemológica y priors bayesianos $\text{Beta}(\alpha_0, \beta_0)$ (`constitution §5 #15`, `spec §Acceptance AC-04`):**
  - `verified` ($\text{Rank} = 5$): $\alpha_0 = 19.0, \beta_0 = 1.0 \Rightarrow \hat{\theta}_0 = 0.95$. Solo asignable por `principal_type IN ('user', 'orchestrator', 'service')`.
  - `observed` ($\text{Rank} = 4$): $\alpha_0 = 8.5, \beta_0 = 1.5 \Rightarrow \hat{\theta}_0 = 0.85$. Tope máximo para `principal_type = 'agent'`.
  - `reported` ($\text{Rank} = 3$): $\alpha_0 = 5.25, \beta_0 = 2.25 \Rightarrow \hat{\theta}_0 = 0.70$.
  - `inferred` ($\text{Rank} = 2$): $\alpha_0 = 3.0, \beta_0 = 2.0 \Rightarrow \hat{\theta}_0 = 0.60$.
  - `hypothesis` ($\text{Rank} = 1$): $\alpha_0 = 1.4, \beta_0 = 2.6 \Rightarrow \hat{\theta}_0 = 0.35$.
- **Reglas de supersesión e inmutabilidad (`spec §Acceptance AC-05, AC-06`):**
  - En `CaptureMemory` con `topic_key` activa en la misma frontera de ámbito: si $\text{Rank}(M_{\text{new}}) \ge \text{Rank}(M_{\text{old}})$, cierra `valid_until` de $M_{\text{old}}$, pasa $M_{\text{old}}$ a `superseded`, crea nueva versión inmutable, inserta $M_{\text{new}}$ como único `active` y crea relación `supersedes`. Si $\text{Rank}(M_{\text{new}}) < \text{Rank}(M_{\text{old}})$, mantiene $M_{\text{old}}$ como único `active`, inserta $M_{\text{new}}$ como `conflicted` y crea relación `contradicts`.
  - En `SupersedeMemory` explícito sobre un recuerdo `verified` ($\text{Rank} = 5$) con reemplazo `observed` ($\text{Rank} = 4$): permitido únicamente si `principal_type IN ('user', 'orchestrator')` y `change_reason` no está vacío.
  - Prohibido ejecutar `UpdateMemory` o `SupersedeMemory` sobre recuerdos en estado cerrado (`superseded`, `archived`, `rejected`) $\rightarrow$ `409 Conflict`.
- **Fórmulas de recalibración bayesiana y cuarentena síncrona (`spec §Behaviour`, `AC-08`):**
  - `retrieved` / `used_in_context`: `access_count += 1`, actualiza `last_accessed_at` ($\Delta\alpha = 0, \Delta\beta = 0$).
  - `helpful_success`: $\alpha \leftarrow \alpha + 1.0$, $u \leftarrow \min(1.0, 0.85u + 0.15)$.
  - `confirmed_verified`: restringido a `user`/`orchestrator`/`service`; $\alpha \leftarrow \alpha + 3.0$, promueve `epistemic_status = 'verified'` y registra nueva versión inmutable.
  - `ignored`: $\beta \leftarrow \beta + 0.15$.
  - `misleading_failure`: $\beta \leftarrow \beta + 2.5$, $u \leftarrow \max(-1.0, 0.75u - 0.25)$.
  - `corrected_by_user`: $\beta \leftarrow \beta + 4.0$ (o supersesión atómica si aporta recuerdo de reemplazo).
  - **Umbral de cuarentena:** Si tras actualizar $(\alpha, \beta)$ un recuerdo `active` queda con $\hat{\theta} = \frac{\alpha}{\alpha + \beta} < 0.30$, pasa síncronamente a `status = 'conflicted'` en la misma transacción, libera el bloqueo de `topic_key` activa y registra una nueva versión en `memory_versions`.
- **Sanitización determinista y contenedor XML (`constitution §5 #16`, `spec §Acceptance AC-09`):**
  - Límites de entrada: `summary` $\le 600$ caracteres, `content` $\le 65536$ caracteres (`422 Unprocessable Entity` si exceden o quedan vacíos tras sanitizar).
  - En escritura (antes de calcular `content_sha256`): eliminar caracteres de control ASCII (`0x00-0x08`, `0x0B`, `0x0C`, `0x0E-0x1F`, `0x7F`) y caracteres invisibles Unicode Bidi (`U+202A..U+202E`, `U+2066..U+2069`, `U+200B..U+200F`, `U+FEFF`), y escapar cualquier aparición de `<romero_memory` o `</romero_memory>`, anotando `metadata.sanitized = true` cuando ocurra.
  - En lectura: envolver en `<romero_memory id="<uuid>" type="<memory_type>" epistemic="<epistemic_status>" confidence="<0.00>" source="<source_ref>" untrusted_content="true">...</romero_memory>`.
- **Git y autoría (`constitution §3 #10, §4 #11, #12`):** Trabajo en rama dedicada (`feat/phase-01-foundation`), mensajes de commit con `gitmoji` + Conventional Commits y autoría exclusivamente humana (prohibido `Co-Authored-By`).

## 1. Context & constraints

- **Criterios de aceptación rectores:**
  - Aislamiento Zero-Trust, opacidad `404` anti-enumeración y particionamiento de `topic_key` por propietario de ámbito: `spec §Acceptance AC-01, AC-02`.
  - Idempotencia estricta verificada por `content_sha256` y `scope_type`: `spec §Acceptance AC-03`.
  - Priors bayesianos $\text{Beta}(\alpha_0, \beta_0)$, restricción de autoridad `verified`, supersesión automática/explícita e inmutabilidad de estados cerrados: `spec §Acceptance AC-04, AC-05, AC-06`.
  - Trazabilidad tri-temporal (`observed_at`, `created_at`, `[valid_from, valid_until)`) y consulta bitemporal *Point-in-Time* (`as_of` / `valid_at` + `known_at`): `spec §Acceptance AC-07`.
  - Recalibración bayesiana por feedback y cuarentena automática síncrona ($\hat{\theta} < 0.30$): `spec §Acceptance AC-08`.
  - Sanitización determinista anti-inyección en escritura, contenedor XML en lectura y aprovisionamiento por CLI/bootstrap: `spec §Acceptance AC-09, AC-10`.
- **Barras no funcionales:** Atomicidad ACID bajo concurrencia sobre la misma `topic_key` e `idempotency_key`; $0.00\%$ de fuga entre ámbitos (`constitution §5 #14`); compilación estática multiplataforma `CGO_ENABLED=0` (`constitution §1 #1`).
- **Fuera de alcance en este plan (`spec §Non-goals`):** Búsqueda híbrida (`SearchMemories`), empaquetado submodular (`BuildContext`), Caché Cognitiva L1 en RAM (`GetSubgraph`), ejecución de modelos neuronales (`BGE-M3`, `Reranker`, `DeBERTa-v3`, `SLM REM` — en esta fase los trabajos `embed_and_nli` solo se encolan en `memory_jobs`) y servidor MCP.

## 2. Architecture

```text
+-----------------------------------------------------------------------------------+
|                        ROMERO Single Static Binary (Go 1.23+)                     |
|                                                                                   |
|  +---------------------------+             +-----------------------------------+  |
|  | CLI Adapter (INTERNAL)    |             | REST API Adapter (INTERNAL)       |  |
|  | migrate / bootstrap /     |             | /api/v1/memories/* + /status      |  |
|  | principal create / serve  |             +-----------------+-----------------+  |
|  +-------------+-------------+                               |                    |
|                |                                             v                    |
|                |                           +-----------------------------------+  |
|                |                           | Zero-Trust AuthMiddleware         |  |
|                |                           | (INTERNAL: SHA-256 Key -> Context)|  |
|                |                           +-----------------+-----------------+  |
|                |                                             |                    |
|                v                                             v                    |
|  +-----------------------------------------------------------------------------+  |
|  | Application Layer (INTERNAL)                                                |  |
|  | - ProvisioningService (bootstrap tenants, projects, agents, principals)     |  |
|  | - CaptureService      (idempotency, sanitization, topic_key supersede)      |  |
|  | - RetrievalService    (GetMemory current & Point-in-Time valid_at/known_at) |  |
|  | - FeedbackService     (Beta(a,b) recalibration & <0.30 quarantine)          |  |
|  +-------------------------------------+---------------------------------------+  |
|                                        |                                          |
|                                        v                                          |
|  +-----------------------------------------------------------------------------+  |
|  | Domain Core (INTERNAL - Pure Go, Zero External Imports)                     |  |
|  | - identity:   AuthContext, ScopePolicy, PrincipalAuthority                  |  |
|  | - memory:     Memory, Version, Clocks, EpistemicRank, BayesianBeta, Ports   |  |
|  | - provenance: Source invariants                                             |  |
|  | - security:   DeterministicSanitizer & UntrustedXMLEnvelope                 |  |
|  +-------------------------------------+---------------------------------------+  |
|                                        ^                                          |
|                                        | implements ports                         |
|  +-------------------------------------+---------------------------------------+  |
|  | Persistence & Audit Adapter (INTERNAL - pgx/v5 + sqlc + Goose)              |  |
|  | - Transactional UnitOfWork (ACID locking on active topic_key / idempotency) |  |
|  | - Compiled SQL Repositories (memories, versions, relations, jobs, audit)    |  |
|  +-------------------------------------+---------------------------------------+  |
+----------------------------------------|------------------------------------------+
                                         | Binary Wire Protocol (TCP)
                                         v
                    +-----------------------------------------+
                    | PostgreSQL 17 + pgvector 0.8.0+         |
                    | (EXTERNAL Store: ACID, Bitemporal,      |
                    |  Partitioned Partial Unique Indexes)    |
                    +-----------------------------------------+
```

- **CLI Adapter (`internal/interfaces/cli`, INTERNAL):** Expone los comandos operativos (`migrate`, `bootstrap`, `principal create`, `serve`) para inicializar el esquema y aprovisionar identidades Zero-Trust sin abrir endpoints HTTP de administración (`AC-10`).
- **REST API Adapter (`internal/interfaces/rest`, INTERNAL):** Valida esquemas de entrada/salida OpenAPI 3.1 y expone las operaciones del ciclo de vida de recuerdos (`/api/v1/memories`, `/api/v1/memories/{id}`, `/api/v1/memories/{id}/supersede`, `/api/v1/memories/{id}/archive`, `/api/v1/memories/feedback`, `/api/v1/system/status`).
- **Zero-Trust AuthMiddleware (`internal/interfaces/rest/middleware`, INTERNAL):** Extrae la credencial de la petición, calcula su huella `SHA-256`, resuelve el `AuthContext` inmutable desde la base de datos y rechaza credenciales inválidas o expiradas con `401 Unauthorized` auditado.
- **Application Layer (`internal/application/{provisioning,capture,retrieval,feedback}`, INTERNAL):** Orquesta transacciones atómicas combinando validación de políticas de ámbito, sanitización, idempotencia, supersesión jerárquica, instantáneas de versión, encolado de trabajos y emisión de eventos de auditoría.
- **Domain Core (`internal/domain/{identity,memory,provenance,security}`, INTERNAL):** Implementa en Go puro las reglas matemáticas e invariantes (jerarquía epistemológica $5..1$, distribución $\text{Beta}(\alpha,\beta)$, cuarentena $\hat{\theta} < 0.30$, validación de los 3 relojes, matriz de visibilidad de los 5 ámbitos y sanitización/encapsulamiento XML).
- **Persistence & Audit Adapter (`internal/infrastructure/persistence`, INTERNAL):** Ejecuta las consultas compiladas por `sqlc` dentro de transacciones `pgx/v5` y garantiza que todo intento denegado (`denied`) se persista en `audit_events` incluso cuando la operación principal se aborte.
- **PostgreSQL 17 Store (EXTERNAL):** Única fuente de verdad transaccional que impone integridad referencial, unicidad particionada de `topic_key` activa e idempotencia por inquilino.

**Top architectural decision:** **Imponer la unicidad de `topic_key` activa, la resolución de supersesión/conflicto y el aislamiento de ámbito dentro de una única transacción ACID en PostgreSQL apoyada en bloqueo de fila (`SELECT ... FOR UPDATE`) y un índice único parcial particionado por propietario de ámbito, registrando los eventos `audit_events(outcome = 'denied')` fuera del rollback de negocio.**
- *Alternativa descartada:* Resolver colisiones de `topic_key` con mutexes en memoria del proceso Go o filtrar permisos en Go después de leer por UUID sin predicados de ámbito.
- *Por qué se elige el diseño transaccional en base de datos:* Un mutex en memoria falla si existen múltiples conexiones o instancias concurrentes de CLI/servidor, y el índice simple `(tenant_id, project_id, scope_type, topic_key)` de la Especificación Maestra colisionaría cuando dos agentes distintos usen la misma `topic_key` en `agent_private` o dos tareas distintas en `task_shared`. Particionar el índice único parcial por la clave real del propietario del ámbito (`owner_agent_id` en `agent_private`, `task_id` en `task_shared`) junto con bloqueo transaccional `FOR UPDATE` garantiza el cumplimiento simultáneo de `AC-01`, `AC-02`, `AC-05` y `AC-06` con cero condiciones de carrera.

## 3. Interfaces & contracts

```text
identity.Authenticator.Authenticate(rawCredential: string) -> AuthContext | UnauthorizedError
  - invariant: deriva tenant_id, principal_id, principal_type, agent_id, allowed_projects y allowed_scopes exclusivamente del hash SHA-256 de la credencial activa y no expirada.
  - precondition: rawCredential no vacío.
  - postcondition: si falla, registra AuditEvent(outcome = "denied", operation = "authenticate") y devuelve 401.

security.Sanitizer.SanitizeInput(summary: string, content: string) -> SanitizedPayload | ValidationError
  - invariant: función pura y determinista; elimina caracteres de control ASCII e invisibles Unicode Bidi y escapa etiquetas <romero_memory> / </romero_memory> antes de calcular content_sha256.
  - precondition: len(summary) <= 600 y len(content) <= 65536 caracteres.
  - postcondition: devuelve summary limpio, content limpio, content_sha256 (64 hex chars), token_count estimado y bandera sanitized: bool; si el resultado queda vacío, devuelve ValidationError (422).

security.Sanitizer.WrapUntrustedXML(m: MemoryView, sourceRef: string) -> string
  - invariant: produce siempre un contenedor <romero_memory id="..." type="..." epistemic="..." confidence="..." source="..." untrusted_content="true"> bien formado cuyo cuerpo interno no puede cerrar la etiqueta raíz.

capture.Service.CaptureMemory(ctx: AuthContext, cmd: CaptureCommand) -> CaptureResult | ForbiddenError | ConflictError | ValidationError
  - invariant: idempotente por (ctx.TenantID, cmd.IdempotencyKey) cuando coinciden content_sha256 y scope_type; transaccional en la creación de fuente, recuerdo, versión v1, supersesión/conflicto de topic_key y trabajo 'embed_and_nli' (prioridad 100).
  - precondition: ctx tiene permiso 'write' en cmd.ProjectID y cmd.ScopeType; si cmd.ScopeType == 'task_shared', cmd.TaskID es obligatorio; si ctx.PrincipalType == 'agent', cmd.EpistemicStatus != 'verified'.
  - postcondition:
      1) Si IdempotencyKey existe con igual content_sha256 y scope_type -> devuelve el recuerdo existente sin mutar estado.
      2) Si IdempotencyKey existe con distinto content_sha256 o scope_type -> devuelve ConflictError (409).
      3) Si TopicKey existe activa en la misma partición de ámbito:
         - Rank(new) >= Rank(active) -> cierra valid_until del anterior, lo pasa a 'superseded' (nueva versión), inserta el nuevo como 'active' y crea relación 'supersedes'.
         - Rank(new) < Rank(active)  -> mantiene el anterior como único 'active', inserta el nuevo como 'conflicted' y crea relación 'contradicts'.
      4) En todos los casos no idempotentes, registra versión inicial en memory_versions, encola job 'embed_and_nli' y audita 'allowed'.

capture.Service.UpdateMemory(ctx: AuthContext, memoryID: UUID, cmd: UpdateCommand) -> MemoryView | NotFoundError | ForbiddenError | ConflictError | ValidationError
  - invariant: nunca sobrescribe destructivamente la historia; incrementa version en +1 e inserta instantánea en memory_versions.
  - precondition: el recuerdo existe y es visible en la frontera de ámbito de ctx (si existe fuera de su frontera -> audita 'denied' y devuelve NotFoundError 404); status actual debe ser 'active' o 'conflicted' (si es 'superseded', 'archived' o 'rejected' -> devuelve ConflictError 409).
  - postcondition: actualiza summary/content/tags/metadata sanitizados, recalcula content_sha256, incrementa version, inserta fila en memory_versions y encola job 'embed_and_nli'.

capture.Service.SupersedeMemory(ctx: AuthContext, oldMemoryID: UUID, cmd: ExplicitSupersedeCommand) -> SupersedeResult | NotFoundError | ForbiddenError | ConflictError | ValidationError
  - invariant: transición atómica entre el recuerdo saliente y el recuerdo entrante con enlace 'supersedes'.
  - precondition: oldMemory existe y es visible para ctx (si no -> 404 + audita 'denied'); oldMemory.Status IN ('active', 'conflicted') (si está cerrado -> 409); Rank(new) >= Rank(old) O (old == 'verified' y new == 'observed' con ctx.PrincipalType IN ('user', 'orchestrator') y cmd.ChangeReason != "").
  - postcondition: oldMemory pasa a 'superseded' con valid_until cerrado y nueva versión; newMemory queda 'active' con versión v1; se crea relación activa 'supersedes'.

capture.Service.ArchiveMemory(ctx: AuthContext, memoryID: UUID, reason: string) -> MemoryView | NotFoundError | ForbiddenError
  - invariant: retira el recuerdo de la vigencia activa conservando intacta su trazabilidad histórica.
  - precondition: el recuerdo existe y es visible para ctx (si no -> 404 + audita 'denied').
  - postcondition: status pasa a 'archived', valid_until se cierra al instante actual, se libera su topic_key activa y se registra nueva versión en memory_versions.

retrieval.Service.GetMemory(ctx: AuthContext, query: GetMemoryQuery) -> MemoryDetail | NotFoundError
  - invariant: aplica aislamiento Zero-Trust antes de proyectar datos y serializa siempre el bloque XML con untrusted_content="true".
  - precondition: si el UUID existe fuera del ámbito visible de ctx -> registra AuditEvent(outcome = "denied") y devuelve NotFoundError (404).
  - postcondition:
      - Sin filtro temporal: devuelve estado actual, versiones y relaciones activas.
      - Con as_of (valid_at = as_of, known_at = as_of) o par (valid_at, known_at): devuelve la instantánea de la última versión con created_at <= known_at si y solo si el recuerdo existía en known_at y su ventana de validez cubría valid_at; en caso contrario devuelve NotFoundError (404).

feedback.Service.RecordFeedback(ctx: AuthContext, cmd: FeedbackCommand) -> FeedbackResult | NotFoundError | ForbiddenError | ValidationError
  - invariant: actualización matemática exacta de Beta(alpha, beta) y evaluación síncrona de cuarentena en la misma transacción.
  - precondition: el recuerdo existe y es visible para ctx (si no -> 404 + audita 'denied'); si cmd.Signal == 'confirmed_verified', ctx.PrincipalType IN ('user', 'orchestrator', 'service') (si es 'agent' -> 403 + audita 'denied').
  - postcondition: inserta fila en memory_feedback, actualiza alpha_score, beta_score, confidence y utility_score; si status == 'active' y nuevo confidence < 0.30, cambia status a 'conflicted' y crea nueva versión en memory_versions documentando la cuarentena.
```

## 4. Data model & flow

**Entities**

- **Tenant / Project / Agent / Principal:** Identidades organizacionales y criptográficas. `Principal` almacena `principal_type` (`user`, `service`, `orchestrator`, `agent`), `api_key_prefix`, `api_key_sha256` (único), `allowed_projects`, `allowed_scopes` y `expires_at`.
- **MemorySource:** Procedencia obligatoria e inmutable (`tenant_id`, `project_id`, `source_type`, `external_source_id`, `actor_principal_id`, `actor_agent_id`, `uri`, `raw_excerpt_hash`).
- **Memory:** Registro central con clasificación (`memory_type`, `scope_type`, `status`, `topic_key`), propietarios de ámbito (`tenant_id`, `project_id`, `owner_agent_id`, `task_id`), contenido (`summary`, `content`, `content_sha256`, `token_count`, `tags`, `entities`), calibración bayesiana (`epistemic_status`, `alpha_score`, `beta_score`, `confidence` generada como $\frac{\alpha}{\alpha+\beta}$, `importance`, `utility_score`), procedencia (`primary_source_id`, `idempotency_key`) y los tres relojes (`observed_at`, `created_at`, `valid_from`, `valid_until`).
  - *Frontera de unicidad de `topic_key` activa:* Índice único parcial sobre recuerdos con `status = 'active' AND topic_key IS NOT NULL` particionado por `(tenant_id, COALESCE(project_id, nil_uuid), scope_type, COALESCE(owner_agent_id, nil_uuid) cuando agent_private, COALESCE(task_id, '') cuando task_shared, topic_key)`.
- **MemoryVersion:** Historial inmutable de instantáneas autocontenidas por `(memory_id, version)` registrando `summary`, `content`, `tags`, `entities`, `metadata`, `epistemic_status`, `alpha_score`, `beta_score`, `importance`, `utility_score`, `status`, `valid_from`, `valid_until`, `changed_by_source`, `change_reason` y `created_at` (reloj de transacción de esa versión para consultas *Point-in-Time*). En `UpdateMemory` ($v \rightarrow v+1$ en $t_u$), la versión $v$ cierra su `valid_until = t_u` en `memory_versions` y la versión $v+1$ se abre con `valid_from = t_u` y `valid_until = NULL`, encadenando intervalos sin solapamiento.
- **MemoryRelation:** Aristas dirigidas bitemporales (`from_memory_id`, `to_memory_id`, `relation_type` en `{'supports', 'contradicts', 'supersedes', 'caused_by', 'depends_on', 'derived_from', 'related_to'}`, `weight`, `valid_from`, `valid_until`, `is_active`).
- **MemoryFeedback:** Registro inmutable de señales empíricas (`memory_id`, `agent_id`, `task_id`, `signal`, `alpha_delta`, `beta_delta`, `utility_delta`, `comment`, `created_at`).
- **MemoryJob:** Cola transaccional priorizada (`tenant_id`, `project_id`, `memory_id`, `job_type`, `priority`, `status`, `payload`, `scheduled_at`).
- **AuditEvent:** Bitácora de seguridad (`tenant_id`, `principal_id`, `agent_id`, `operation`, `resource_type`, `resource_id`, `scope_used`, `outcome` en `{'allowed', 'denied'}`, `details`, `created_at`).

**Primary flow** (`CaptureMemory` con supersesión/conflicto por `topic_key` y aislamiento Zero-Trust)

1. `REST Adapter` recibe `POST /api/v1/memories`; `AuthMiddleware` valida el token Bearer/API Key por `SHA-256` y construye `AuthContext`.
2. `CaptureService` verifica autorización de escritura sobre `(project_id, scope_type)`, obligatoriedad de `task_id` en `task_shared` y autoridad epistemológica del `principal_type`; si falla, registra `AuditEvent(outcome = 'denied')` y devuelve `403` o `422`.
3. `Sanitizer` limpia caracteres de control/Bidi y escapa `<romero_memory>` / `</romero_memory>`, calculando `content_sha256`.
4. Dentro de una transacción ACID en PostgreSQL:
   - Si viene `idempotency_key`, busca en `memories` por `(tenant_id, idempotency_key)`: si existe y coinciden `content_sha256` y `scope_type`, hace commit de lectura y devuelve `200 OK`; si difieren, aborta y devuelve `409 Conflict`.
   - Inserta `memory_sources`.
   - Si viene `topic_key`, bloquea con `SELECT ... FOR UPDATE` el recuerdo `active` existente en la misma partición de ámbito (`tenant_id`, `project_id`, `scope_type`, `owner_agent_id`/`task_id`, `topic_key`):
     - Si existe y $\text{Rank}(M_{\text{new}}) \ge \text{Rank}(M_{\text{old}})$: actualiza $M_{\text{old}}$ (`status = 'superseded'`, `valid_until = now`, `version += 1`), inserta instantánea en `memory_versions`, inserta $M_{\text{new}}$ (`status = 'active'`, `version = 1`), inserta su `v1` en `memory_versions` e inserta arista `supersedes` en `memory_relations`.
     - Si existe y $\text{Rank}(M_{\text{new}}) < \text{Rank}(M_{\text{old}})$: deja $M_{\text{old}}$ intacto, inserta $M_{\text{new}}$ (`status = 'conflicted'`, `version = 1`), inserta su `v1` en `memory_versions` e inserta arista `contradicts` en `memory_relations`.
     - Si no existe: inserta $M_{\text{new}}$ (`status = 'active'`, `version = 1`) y su `v1` en `memory_versions`.
   - Encola trabajo `embed_and_nli` (`priority = 100`, `status = 'pending'`) en `memory_jobs` e inserta `AuditEvent(outcome = 'allowed')`.
5. Confirma la transacción y devuelve el resultado tipado al cliente.

- **Consistency boundaries:** Creación de fuente, recuerdo, versión inmutable, cambio de estado/relación por `topic_key`, recalibración bayesiana/cuarentena, encolado del job `embed_and_nli` y evento de auditoría `allowed` son **estrictamente transaccionales (ACID)** en la misma transacción SQL. Los eventos de auditoría `denied` se persisten en una operación independiente para que no se pierdan al abortar la operación rechazada. El consumo de `memory_jobs` por motores neuronales es **eventualmente consistente** (diferido a Fases 02 y 03).
- **Migration impact:** Nueva migración fundacional `migrations/00001_romero_core_schema.sql` (no destructiva; inicializa tipos enumerados, las 11 tablas del núcleo, restricciones de ventana temporal y el índice único parcial particionado de `topic_key` activa).

## 5. Testing strategy

| Acceptance criterion | Level | Asserts | Fakes / mocks |
| --- | --- | --- | --- |
| `spec §Acceptance AC-01` (Aislamiento Zero-Trust y opacidad `404` anti-enumeración) | unit + integration | Matriz de acceso cruzado entre 2 tenants, 2 proyectos, 2 `owner_agent_id` (`agent_private`) y 2 `task_id` (`task_shared`) devuelve `404 Not Found` en acceso por ID y `403` en creación no autorizada, dejando registro `denied` en `audit_events` con $0.00\%$ de fuga. | En unit: repositorio en memoria; en integration: PostgreSQL 17 real sin mocks. |
| `spec §Acceptance AC-02` (Particionamiento de `topic_key` por propietario de ámbito y `task_id` obligatorio) | unit + integration | Dos agentes en `agent_private` y dos tareas en `task_shared` mantienen simultáneamente recuerdos `active` con la misma `topic_key` en el mismo proyecto sin colisionar; `task_shared` sin `task_id` devuelve `422`. | En integration: PostgreSQL 17 real (verifica índice único parcial). |
| `spec §Acceptance AC-03` (Idempotencia estricta con verificación de huella) | integration + contract | Mismo `(tenant_id, idempotency_key)` + mismo `content_sha256` y `scope_type` devuelve `200 OK` con `version = 1` y un solo registro; distinto `content_sha256` o `scope_type` devuelve `409 Conflict`. | PostgreSQL 17 real + cliente HTTP `httptest` contra router Huma v2. |
| `spec §Acceptance AC-04` (Priors bayesianos canónicos y bloqueo de auto-verificación por agentes) | unit + contract | Cada `epistemic_status` inicializa exactamente sus $(\alpha_0, \beta_0, \hat{\theta}_0)$ canónicos; un `principal_type = agent` que envía `verified` en captura o `confirmed_verified` en feedback recibe `403 Forbidden`. | Sin mocks en unit (dominio puro); `httptest` en contract. |
| `spec §Acceptance AC-05` (Protección jerárquica en supersesión automática por `topic_key`) | unit + integration | Captura con rango $< 5$ sobre `topic_key` con recuerdo `verified` activo conserva el `verified` como único `active`, guarda el nuevo como `conflicted` y crea arista `contradicts`. | PostgreSQL 17 real en integration. |
| `spec §Acceptance AC-06` (Supersesión válida, explícita auditada e inmutabilidad de estados cerrados) | unit + integration | Supersesión automática ($\text{Rank} \ge \text{anterior}$) y explícita (`verified` $\rightarrow$ `observed` por `user`/`orchestrator` con motivo) cierran `valid_until`, pasan a `superseded` y enlazan `supersedes` (incluso bajo goroutines concurrentes); `UpdateMemory` o `SupersedeMemory` sobre recuerdos `superseded`/`archived`/`rejected` devuelve `409 Conflict`. | PostgreSQL 17 real con prueba de concurrencia (`errgroup`). |
| `spec §Acceptance AC-07` (Trazabilidad tri-temporal y consulta histórica *Point-in-Time*) | integration | Consultas `GetMemory` con `as_of` y con `(valid_at, known_at)` entre $t_1 < t_2 < t_3$ devuelven exactamente la instantánea vigente en $t_2$ y registrada hasta $t_2$, y `404` para `known_at < t_1`. | PostgreSQL 17 real con reloj inyectable en pruebas. |
| `spec §Acceptance AC-08` (Recalibración bayesiana y cuarentena automática $\hat{\theta} < 0.30$) | unit + integration | Fórmulas exactas para las 7 señales de `RecordFeedback`; cuando `misleading_failure` reduce $\hat{\theta}$ de `0.35` a $\approx 0.215 < 0.30$, el recuerdo pasa a `conflicted` en la misma transacción, registra nueva versión y libera su `topic_key`. | Dominio puro en unit; PostgreSQL 17 real en integration. |
| `spec §Acceptance AC-09` (Sanitización determinista anti-inyección y contenedor XML seguro) | unit + contract | Caracteres invisibles Unicode Bidi/control se eliminan y etiquetas `<romero_memory>` / `</romero_memory>` se escapan antes de `content_sha256`, marcando `metadata.sanitized = true` y devolviendo XML bien formado con `untrusted_content="true"`. | Sin mocks (función pura + `httptest`). |
| `spec §Acceptance AC-10` (Aprovisionamiento de identidades por bootstrap/CLI sin rutas REST extra) | integration + contract | El comando CLI/bootstrap crea tenant, proyecto, agente y principal; la llave generada autentica en `/api/v1/memories` y la especificación OpenAPI 3.1 no expone endpoints HTTP de alta de tenants/principals. | PostgreSQL 17 real + ejecución de comando CLI e inspección de rutas HTTP. |

- **Dónde se sitúa la línea E2E y por qué:** El límite superior en la Fase 01 son las pruebas de contrato e integración HTTP + CLI contra una instancia real de **PostgreSQL 17 + `pgvector`** levantada por Docker Compose, ejercitando desde el comando CLI de bootstrap hasta las llamadas REST autenticadas.
- **Qué DEBE ser una dependencia real (sin mocks):** **PostgreSQL 17** en todas las pruebas de aislamiento Zero-Trust (`AC-01`), unicidad particionada de `topic_key` (`AC-02`), idempotencia (`AC-03`), concurrencia de supersesión (`AC-06`) y consultas *Point-in-Time* (`AC-07`), porque las garantías residen en las restricciones, índices únicos parciales y bloqueos transaccionales del motor SQL.

## 6. Sequencing & dependencies

1. **Andamiaje del módulo Go (`CGO_ENABLED=0`), configuración base (`configs/romero.toml`, `docker-compose.yml`, `sqlc.yaml`, `Makefile`) y migración fundacional `migrations/00001_romero_core_schema.sql`** — depends on: none — `[serial]`
2. **Núcleo de dominio puro (`internal/domain/{identity,provenance,memory,security}`): relojes tri-temporales, jerarquía epistemológica ($1..5$), motor bayesiano $\text{Beta}(\alpha,\beta)$ con cuarentena $<0.30$, políticas de los 5 ámbitos y sanitizador/envoltorio XML determinista con su suite unitaria TDD** — depends on: #1 — `[parallelizable con #3]`
3. **Consultas SQL compiladas (`internal/infrastructure/persistence/queries/*.sql` + `sqlc generate`) y adaptador transaccional PostgreSQL (`pgxpool`, `UnitOfWork`, repositorios de identidades, fuentes, recuerdos, versiones, relaciones, feedback, jobs y auditoría)** — depends on: #1 — `[parallelizable con #2]`
4. **Servicio de aprovisionamiento Zero-Trust (`ProvisioningService`), autenticador `SHA-256` (`Authenticator`) y comandos CLI (`romero migrate`, `romero bootstrap`, `romero principal create`) (`AC-10`)** — depends on: #2, #3 — `[serial]`
5. **Servicios de aplicación de ciclo de vida y gobernanza (`CaptureService`, `RetrievalService` con soporte *Point-in-Time* `valid_at`/`known_at`, y `FeedbackService` con cuarentena síncrona) (`AC-01` a `AC-09`)** — depends on: #2, #3, #4 — `[serial]`
6. **Adaptador HTTP REST con `Huma v2` (OpenAPI 3.1), middleware Zero-Trust y comando `romero serve` (`/api/v1/memories/*`, `/api/v1/system/status`)** — depends on: #4, #5 — `[serial]`
7. **Suite completa de pruebas de integración, concurrencia y seguridad Zero-Trust ($0.00\%$ fuga, colisiones de `idempotency_key` y `topic_key`, *Point-in-Time* y sanitización XML) contra PostgreSQL 17 real** — depends on: #6 — `[serial]`

- **Candidatos paralelos:** Los pasos **#2** (dominio puro en Go y pruebas unitarias) y **#3** (consultas SQL `sqlc` y adaptadores de persistencia) operan sobre directorios disjuntos (`internal/domain/**` vs. `internal/infrastructure/persistence/**`) sin estado compartido.
- **Restricciones duras de orden:** El paso **#1** debe ir primero porque inicializa `go.mod` y el esquema SQL que `sqlc` necesita compilar en **#3**; **#5** requiere tanto las reglas de dominio (**#2**) como los repositorios compilados (**#3**); **#7** cierra la verificación empírica sobre el binario completo (**#6**).

## 7. Risks & open decisions

**Risks**

| Risk | Trigger | Impact | Mitigation / spike to retire it |
| --- | --- | --- | --- |
| **Condición de carrera en supersesión concurrente sobre la misma `topic_key`** | Dos agentes envían simultáneamente `CaptureMemory` con la misma `topic_key` en el mismo ámbito. | Violación de unicidad o dos recuerdos activos para la misma llave. | Bloqueo `SELECT ... FOR UPDATE` sobre el registro activo dentro de la transacción antes de decidir supersesión/conflicto, respaldado por el índice único parcial particionado y reintento controlado ante `unique_violation` (`23505`). |
| **Contaminación retroactiva en consultas *Point-in-Time*** | Una actualización en $t_3$ modifica filas en `memories` y una consulta histórica para $t_2 < t_3$ lee la tabla `memories` en lugar de reconstruir desde `memory_versions`. | La auditoría histórica devuelve datos que el agente no conocía en $t_2$ (falla `AC-07`). | Almacenar `valid_from` y `valid_until` en cada instantánea de `memory_versions` y resolver las consultas con `as_of` / `(valid_at, known_at)` sobre la última versión con `created_at <= known_at` cuya ventana de validez cubra `valid_at`. |
| **Pérdida del evento de auditoría `denied` al hacer rollback de una transacción rechazada** | El servicio detecta falta de permisos y hace `tx.Rollback()`, deshaciendo también el `INSERT INTO audit_events`. | Los intentos de intrusión o acceso cruzado no quedan registrados en `audit_events` (falla `AC-01`). | Ejecutar el registro de `audit_events(outcome = 'denied')` en una conexión/transacción independiente del pool (`pgxpool`) fuera del `Rollback` de la operación de negocio. |
| **Ruptura del contenedor XML por secuencias CDATA o etiquetas parciales** | Un recuerdo contiene `<romero_memory` sin cerrar o secuencias `]]>` que alteran el parser del consumidor. | Posible escape del contenedor `untrusted_content="true"`. | Escapar de forma determinista `<` y `>` en secuencias `<romero_memory` / `</romero_memory>` y validar en pruebas unitarias de propiedad que el bloque devuelto sea siempre un único elemento raíz XML válido. |

**Open decisions**

- **Ninguna (`none`):** Las 10 ramas de diseño de `specify` y las 2 preguntas de `clarify` están cerradas y registradas en `02-DOCS/wiki/sdd/decisions.md`.



## Tasks
<!-- generated by tasks on 2026-11-11; IDs are stable, do not renumber -->

| ID | [P] | Task | Done-check | Depends-on | Trace | Risk |
| --- | --- | --- | --- | --- | --- | --- |
| T001 |  | Initialize Go 1.23+ module (`CGO_ENABLED=0`), base configs (`configs/romero.toml`, `docker-compose.yml`, `sqlc.yaml`, `Makefile`), entrypoint stub and Goose migration `migrations/00001_romero_core_schema.sql` | `CGO_ENABLED=0 go build ./cmd/romero` exits 0 AND `migrations/00001_romero_core_schema.sql` applies cleanly on PostgreSQL 17 with partitioned `idx_memories_active_topic_key` and `idx_memories_idempotency` | — | AC-01, AC-02, AC-03, AC-07, AC-10 | 2 |
| T002 | [P] | Implement and unit-test pure domain identity, 5-scope visibility/uniqueness policy, principal authority guard and deterministic anti-injection XML sanitizer (`internal/domain/identity`, `internal/domain/security`) | `go test -race -cover ./internal/domain/identity/... ./internal/domain/security/...` exits 0 with coverage >= 85% | T001 | AC-01, AC-02, AC-04, AC-09 | 2 |
| T003 | [P] | Implement and unit-test pure domain memory entities, tri-temporal clocks, epistemic rank hierarchy (`5..1`), Bayesian `Beta(alpha, beta)` engine with `< 0.30` quarantine, provenance invariants and repository ports (`internal/domain/memory`, `internal/domain/provenance`) | `go test -race -cover ./internal/domain/memory/... ./internal/domain/provenance/...` exits 0 with coverage >= 85% | T001 | AC-04, AC-05, AC-06, AC-07, AC-08 | 1 |
| T004 | [P] | Write compiled SQL queries (`internal/infrastructure/persistence/queries/*.sql`), generate `sqlc` code and implement PostgreSQL transactional `UnitOfWork`, repositories and out-of-rollback `denied` audit logger (`internal/infrastructure/persistence`) | `sqlc generate` produces zero diffs AND `go vet ./internal/infrastructure/persistence/...` AND `go test -race ./internal/infrastructure/persistence/...` exit 0 | T001 | AC-01, AC-02, AC-03, AC-05, AC-06, AC-07, AC-08, AC-10 | 1 |
| T005 |  | Implement Zero-Trust `ProvisioningService`, `SHA-256` `Authenticator` and Cobra CLI commands (`romero migrate`, `romero bootstrap`, `romero principal create`) with unit tests (`internal/application/provisioning`, `internal/interfaces/cli`) | `go test -race -cover ./internal/application/provisioning/... ./internal/interfaces/cli/...` exits 0 with coverage >= 80% | T002, T003, T004 | AC-01, AC-10 | 1 |
| T006 |  | Implement and unit-test `CaptureService` (`CaptureMemory`, `UpdateMemory`, `SupersedeMemory`, `ArchiveMemory`) enforcing idempotency hash check, partitioned `topic_key` supersession/conflict, closed-state immutability (`409`) and `embed_and_nli` job enqueueing (`internal/application/capture`) | `go test -race -cover ./internal/application/capture/...` exits 0 with coverage >= 85% | T002, T003, T004 | AC-01, AC-02, AC-03, AC-04, AC-05, AC-06, AC-09 | 1 |
| T007 | [P] | Implement and unit-test `RetrievalService` (`GetMemory` current and Point-in-Time `as_of` / `valid_at` + `known_at` with XML wrapping) and `FeedbackService` (`RecordFeedback` with synchronous `< 0.30` quarantine) (`internal/application/retrieval`, `internal/application/feedback`) | `go test -race -cover ./internal/application/retrieval/... ./internal/application/feedback/...` exits 0 with coverage >= 85% | T002, T003, T004 | AC-01, AC-04, AC-07, AC-08, AC-09 | 1 |
| T008 |  | Implement Huma v2 OpenAPI 3.1 REST server, Zero-Trust `AuthMiddleware`, lifecycle endpoints (`/api/v1/memories*`, `/api/v1/system/status`) and `romero serve` command with HTTP contract tests (`internal/interfaces/rest`) | `go test -race -cover ./internal/interfaces/rest/...` exits 0 AND OpenAPI 3.1 spec exposes only the canonical `/api/v1` memory/status routes | T005, T006, T007 | AC-01, AC-03, AC-04, AC-09, AC-10 | 2 |
| T009 |  | Implement and run full integration, concurrency and Zero-Trust cross-scope security test suite against real PostgreSQL 17 (`tests/integration`, `tests/security`) | `go test -race -v ./tests/integration/... ./tests/security/...` exits 0 against PostgreSQL 17 with 0.00% cross-scope leakage across all 10 ACs | T008 | AC-01, AC-02, AC-03, AC-04, AC-05, AC-06, AC-07, AC-08, AC-09, AC-10 | 1 |
| T010 |  | All done-checks pass + full Level 1 & integration verification green | Every row `T001`–`T009` checked AND `sqlc generate`, `gofmt -s -w .`, `CGO_ENABLED=0 go build ./cmd/romero`, `go vet ./...`, `golangci-lint run`, and `go test -race -coverprofile=coverage.out ./...` exit 0 with >= 80% coverage in `domain`/`application` | T001, T002, T003, T004, T005, T006, T007, T008, T009 | all ACs | — |

### Per-Task Interfaces

**T001 — Interfaces**
- Consumes: `constitution.md` stack floors (`Go 1.23+`, `PostgreSQL 17`, `pgvector 0.8.0+`, `pg_trgm`, `btree_gist`, `pgcrypto`).
- Produces:
  - `migrations/00001_romero_core_schema.sql` with tables `tenants`, `projects`, `agents`, `principals`, `memory_sources`, `memories`, `memory_versions` (autocontenida con `summary`, `content`, `tags`, `entities`, `metadata`, `epistemic_status`, `alpha_score`, `beta_score`, `importance`, `utility_score`, `status`, `valid_from TIMESTAMPTZ NOT NULL`, `valid_until TIMESTAMPTZ`, `changed_by_source`, `change_reason`, `created_at`), `memory_embeddings`, `memory_relations`, `memory_feedback`, `memory_jobs`, `audit_events`.
  - Partitioned active `topic_key` unique index:
    `CREATE UNIQUE INDEX idx_memories_active_topic_key ON memories(tenant_id, COALESCE(project_id, '00000000-0000-0000-0000-000000000000'::uuid), scope_type, CASE WHEN scope_type = 'agent_private' THEN COALESCE(owner_agent_id, '00000000-0000-0000-0000-000000000000'::uuid) ELSE '00000000-0000-0000-0000-000000000000'::uuid END, CASE WHEN scope_type = 'task_shared' THEN COALESCE(task_id, '') ELSE '' END, topic_key) WHERE status = 'active' AND topic_key IS NOT NULL;`

**T002 — Interfaces**
- Consumes: Go standard library + `github.com/google/uuid` (from `T001`).
- Produces:
  - `identity.PrincipalType` (`user`, `service`, `orchestrator`, `agent`), `identity.ScopeType` (`agent_private`, `task_shared`, `project_shared`, `org_shared`, `global_public`), `identity.AuthContext{TenantID, PrincipalID, PrincipalType, AgentID, AllowedProjects, AllowedScopes, IsAdmin}`.
  - `identity.AuthContext.CanWriteScope(projectID *uuid.UUID, scope ScopeType, taskID string) error` (`ErrForbidden` -> 403, `ErrMissingTaskID` -> 422).
  - `identity.AuthContext.CanReadMemory(m MemoryScopeTarget, callerTaskID string) bool` (if false on existing UUID -> caller audits `denied` and returns `ErrNotFound` 404).
  - `identity.AuthContext.CanAssignEpistemic(status string) error` (`agent` requesting `verified` -> `ErrForbidden` 403).
  - `security.SanitizeInput(summary, content string) (SanitizedPayload{Summary, Content, ContentSHA256 string, TokenCount int, Sanitized bool}, error)` and `security.WrapUntrustedXML(id uuid.UUID, memType, epistemic string, confidence float32, sourceRef, content string) string`.

**T003 — Interfaces**
- Consumes: `identity` and `security` domain types (or pure UUID/time primitives).
- Produces:
  - `memory.EpistemicStatus` (`verified`=5, `observed`=4, `reported`=3, `inferred`=2, `hypothesis`=1), `memory.InitialPriors(status EpistemicStatus) (alpha, beta, confidence float32)`, `memory.Rank(status EpistemicStatus) int`.
  - `memory.ApplyFeedback(currentAlpha, currentBeta, currentUtility float32, currentEpistemic EpistemicStatus, currentStatus MemoryStatus, signal FeedbackSignal) (FeedbackDeltaResult, error)` implementing the exact deltas and synchronous quarantine transition (`newConfidence < 0.30 && currentStatus == StatusActive -> newStatus = StatusConflicted, Quarantined = true`).
  - `memory.DecideTopicSupersession(oldStatus EpistemicStatus, newStatus EpistemicStatus, explicit bool, callerType identity.PrincipalType, reason string) (SupersessionDecision, error)`.
  - Repository port interfaces in `internal/domain/memory/ports.go`.

**T004 — Interfaces**
- Consumes: `migrations/00001_romero_core_schema.sql` (from `T001`) and domain ports (from `T002`, `T003`).
- Produces:
  - `postgres.Store` implementing transactional `ExecTx(ctx context.Context, fn func(RepositoryRegistry) error) error`.
  - `postgres.AuditLogger.RecordDenied(ctx context.Context, ev AuditEvent) error` executing on an independent pool connection outside business transaction rollbacks.
  - Compiled `sqlc` queries for `LockActiveTopicMemory`, `GetMemoryByIdempotencyKey`, `InsertMemory`, `InsertMemoryVersion`, `CloseMemoryVersionValidity`, `GetMemoryVersionPointInTime`, `UpdateMemoryState`, `InsertMemoryRelation`, `InsertMemoryFeedback`, `EnqueueMemoryJob`, `InsertAuditEvent`.

**T005 — Interfaces**
- Consumes: `identity` domain (`T002`) and `postgres.Store` (`T004`).
- Produces:
  - `provisioning.Service.Bootstrap(ctx context.Context, cfg BootstrapSpec) (*BootstrapOutput, error)` and `provisioning.Service.CreatePrincipal(ctx context.Context, req CreatePrincipalInput) (rawAPIKey string, principal PrincipalView, err error)` generating keys in format `rmr_<type>_<id8>_<secret32>` (storing 20-char `api_key_prefix` + `SHA-256` hex `api_key_sha256`).
  - `provisioning.Authenticator.Authenticate(ctx context.Context, rawAPIKey string) (identity.AuthContext, error)` using `api_key_prefix` index lookup + constant-time `crypto/subtle.ConstantTimeCompare` on the `SHA-256` digest.
  - CLI commands `romero migrate`, `romero bootstrap`, `romero principal create`.

**T006 — Interfaces**
- Consumes: `identity`/`security` (`T002`), `memory`/`provenance` (`T003`), `postgres.Store` + `AuditLogger` (`T004`).
- Produces:
  - `capture.Service.CaptureMemory(ctx context.Context, auth identity.AuthContext, cmd CaptureCommand) (*CaptureResult, error)`
  - `capture.Service.UpdateMemory(ctx context.Context, auth identity.AuthContext, id uuid.UUID, cmd UpdateCommand) (*MemoryView, error)` closing previous version's `valid_until = t_u` in `memory_versions` and opening `v+1` with `valid_from = t_u, valid_until = nil`.
  - `capture.Service.SupersedeMemory(ctx context.Context, auth identity.AuthContext, id uuid.UUID, cmd ExplicitSupersedeCommand) (*SupersedeResult, error)`
  - `capture.Service.ArchiveMemory(ctx context.Context, auth identity.AuthContext, id uuid.UUID, reason string) (*MemoryView, error)`

**T007 — Interfaces**
- Consumes: `identity`/`security` (`T002`), `memory` (`T003`), `postgres.Store` + `AuditLogger` (`T004`), `capture.Service` (`T006` for atomic replacement on `corrected_by_user`).
- Produces:
  - `retrieval.Service.GetMemory(ctx context.Context, auth identity.AuthContext, query GetMemoryQuery) (*MemoryDetail, error)` supporting current view and bitemporal `AsOf` / `(ValidAt, KnownAt)` reconstruction from `memory_versions` plus `UntrustedXMLEnvelope`.
  - `feedback.Service.RecordFeedback(ctx context.Context, auth identity.AuthContext, cmd FeedbackCommand) (*FeedbackResult, error)` applying `Beta(alpha, beta)` recalibration, `< 0.30` synchronous quarantine, and atomic supersession when `signal == "corrected_by_user"` includes optional `ReplacementMemory`.

**T008 — Interfaces**
- Consumes: `provisioning.Authenticator` (`T005`), `capture.Service` (`T006`), `retrieval.Service` & `feedback.Service` (`T007`).
- Produces:
  - `AuthMiddleware` accepting both `Authorization: Bearer <key>` and `X-API-Key: <key>`.
  - Huma v2 HTTP router under `/api/v1`:
    - `POST /api/v1/memories`
    - `GET /api/v1/memories/{id}` (query params: `as_of`, `valid_at`, `known_at`, `task_id`)
    - `PATCH /api/v1/memories/{id}`
    - `POST /api/v1/memories/{id}/supersede`
    - `POST /api/v1/memories/{id}/archive`
    - `POST /api/v1/memories/feedback` (with optional `replacement_memory` for `corrected_by_user`)
    - `GET /api/v1/system/status`
  - Consistent HTTP error mapping (`401`, `403`, `404`, `409`, `422`).

## Review Workload Forecast

| Dimension | Forecast | Why |
| --- | --- | --- |
| Estimated changed lines | ~1,800 – 2,400 LOC | Inicialización completa del módulo Go (`CGO_ENABLED=0`), migración fundacional SQL, 4 paquetes de dominio puro, consultas `sqlc`, 4 servicios de aplicación, CLI/REST y suites unitarias + integración Zero-Trust. |
| Files / areas | ~26 archivos en 6 áreas | `migrations/`, `configs/`, `internal/domain/`, `internal/infrastructure/persistence/`, `internal/application/`, `internal/interfaces/{cli,rest}/`, `tests/{integration,security}/`. |
| Risk | Max tier `2` (3 tareas tier-2: `T001`, `T002`, `T008`; 6 tareas tier-1) | Las rutas `migrations/`, `configs/romero.toml`, `docker-compose.yml`, `internal/domain/identity/` e `internal/interfaces/rest/auth*` activan Tier 2 en `.rsc/sello-config.json` y requieren el panel completo de 3 refuters (`correctness`, `security`, `tests`) antes de cerrar la rama. |
| Suggested delivery | `ask-on-risk` (por tarea en rama aislada `feat/phase-01-foundation`) | Supera el `line_budget: 400` de `02-DOCS/wiki/sdd/config.yaml`; se ejecuta tarea por tarea (`T001` → `T010`) con verificación en cada corte y panel de revisión en cierre. |
