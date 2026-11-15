---
type: spec
title: Spec — Phase 04 Servidor MCP de Memoria Cognitiva y Librería Cliente Exportable para Gandalf Wizard
description: WHAT and WHY for phase-04-mcp-server — exposición del servidor del protocolo de herramientas para agentes con estrictamente las 5 herramientas canónicas de memoria, divulgación progresiva en dos fases, transporte por tubería estándar y flujo de eventos por red, librería cliente pública exportable para el proyecto externo Gandalf Wizard, aislamiento Zero-Trust y preempción inmediata de Sueño REM.
tags: [sdd, spec, romero, phase-04, mcp, sdk, progressive-disclosure, zero-trust, power-governor]
timestamp: 2026-11-15T16:24:30-03:00
topic: sdd
slug: phase-04-mcp-server
branch: feat/phase04-mcp-server
status: in-progress
---

# Spec — Phase 04 Servidor MCP de Memoria Cognitiva y Librería Cliente Exportable para Gandalf Wizard

> Slug: `phase-04-mcp-server` · Status: clarified · Created: 2026-11-15
> Inherits: [constitution](../constitution.md) · Builds on: [phase-01-foundation](phase-01-foundation.md), [phase-02-hybrid-retrieval](phase-02-hybrid-retrieval.md), [phase-03-nli-governor-rem](phase-03-nli-governor-rem.md)

## Problem & why

Con las Fases 01, 02 y 03 completadas, el motor de memoria cognitiva de ROMERO ofrece gobernanza bitemporal Zero-Trust, recuperación híbrida tri-modal con propagación causal en tres saltos, empaquetado de contexto ajustado a presupuesto de tokens, reordenamiento cruzado de precisión, detección automática de contradicciones lógicas y consolidación autónoma en Sueño REM. Sin embargo, toda esta capacidad solo es accesible mediante llamadas directas a la interfaz administrativa y de integración por red.

Los agentes autónomos de código e investigación y los entornos de trabajo compatibles con el protocolo estándar de herramientas para modelos de lenguaje no pueden conectarse de forma nativa a ROMERO ni por proceso local (tubería de entrada/salida estándar) ni por sesión remota de eventos:
1. **Incompatibilidad de protocolo con agentes estándar:** Los agentes que descubren e invocan herramientas mediante el protocolo estándar de contexto de modelos no pueden consumir directamente las trece rutas de red sin un adaptador dedicado.
2. **Riesgo de contaminación de ventana de contexto:** Exponer las trece operaciones administrativas y de ciclo de vida directamente como herramientas al agente saturaría su ventana de instrucciones y aumentaría la tasa de elección errónea de herramienta, o bien volcaría cuerpos completos de decenas de recuerdos en la primera búsqueda.
3. **Brecha de identidad y gobierno de potencia en sesiones de herramientas:** Sin una frontera de autenticación vinculada al transporte de herramientas y conectada al gobernador de potencia, un agente conectado por protocolo de herramientas podría eludir las restricciones de ámbito Zero-Trust o sufrir latencias elevadas si un ciclo de Sueño REM permanece activo en segundo plano durante la invocación de una herramienta.

## Cost of not building it

Si no se construye esta fase:
- Ningún agente compatible con el protocolo estándar de herramientas (ni los agentes coordinados por el orquestador externo) puede leer, buscar, empaquetar contexto, capturar hallazgos o reportar retroalimentación empírica de forma nativa sin código propietario de adaptación en cada cliente.
- Si se intentara suplir con un puente genérico que mapee cada operación de red uno a uno sin divulgación progresiva, cada búsqueda inyectaría el texto completo de los candidatos en el contexto del agente, multiplicando el consumo de tokens y degradando la capacidad de razonamiento del modelo.
- Quedaría incompleto el Principio 9 de la Constitución del proyecto, que exige exponer exactamente las cinco herramientas canónicas con divulgación progresiva en dos fases.

## The cheapest alternative

La alternativa más barata sería proporcionar a los agentes un guion externo o instrucción para invocar manualmente las rutas de red mediante comandos de consola o peticiones manuales. Esa alternativa no basta porque obliga al agente a construir cargas útiles manuales propensas a errores de formato, expone las credenciales en los argumentos de consola del agente, carece de esquemas tipados de descubrimiento automático de herramientas y no ofrece transporte persistente por tubería estándar ni por flujo de eventos autenticado.

## Goals

- Exponer un servidor del protocolo de herramientas para modelos que publique única y exclusivamente las **cinco herramientas canónicas** (`romero_memory_save`, `romero_memory_search`, `romero_memory_get`, `romero_memory_context` y `romero_memory_feedback`), sin superar jamás ese tope.
- Aplicar **divulgación progresiva en dos fases**: `romero_memory_search` devuelve únicamente vistas compactas (resumen, estado epistemológico, confianza, conteo de relaciones visibles y alerta de contradicción) sin incluir nunca el cuerpo completo del recuerdo, mientras que `romero_memory_get` permite descargar en una segunda fase el contenido completo envuelto en el contenedor seguro anti-inyección, el historial de versiones y las relaciones causales de uno o varios recuerdos seleccionados.
- Soportar dos modos de transporte desde el mismo ejecutable:
  1. **Transporte local por tubería estándar (`stdio`):** autenticado una sola vez al iniciar el proceso mediante la credencial configurada, asociando todas las llamadas de esa sesión a la identidad verificada.
  2. **Transporte por red de flujo de eventos (`HTTP/SSE`):** capaz de atender múltiples sesiones de agentes concurrentes exigiendo credencial válida en cada conexión/solicitud y resolviendo el contexto de autorización de forma independiente por petición.
- Garantizar que toda invocación de cualquiera de las cinco herramientas respete exactamente los mismos invariantes Zero-Trust, opacidad anti-enumeración, recibos opacos en escrituras sin lectura, calibración bayesiana, protección de rango epistemológico y encapsulamiento XML `<romero_memory ... untrusted_content="true">` que el resto de la plataforma.
- Notificar al gobernador de potencia (`PowerGovernor`) al inicio y fin de cada invocación de herramienta para que cualquier ciclo de consolidación en Sueño REM sea interrumpido en menos de diez milisegundos.
- Proveer en este repositorio una **librería cliente pública y exportable** (`pkg/romeroclient`) libre de dependencias internas privadas, para que el proyecto externo **Gandalf Wizard** pueda importarla directamente y conectar su constructor de contexto, su evaluador bayesiano y sus flujos de arbitraje y consolidación contra ROMERO sin redeclarar contratos ni construir peticiones manuales.

## Non-goals / out of scope

- Implementación del motor de orquestación, planificador de agentes o bucle de ejecución propio de **Gandalf Wizard** (que vive en su propio repositorio independiente y consume a ROMERO como dependencia externa).
- Exposición de una sexta o séptima herramienta en el servidor de herramientas para operaciones administrativas como listar conflictos (`ListConflicts`), arbitrar conflictos (`ReviewConflict`), disparar consolidación (`TriggerREM`), archivar recuerdos o administrar permisos de tareas (esas operaciones se consumen mediante la librería cliente exportable y la interfaz de red/consola, nunca como herramientas adicionales en el catálogo de los agentes).
- Paso de credenciales o llaves secretas dentro de los argumentos de negocio de cada herramienta (la identidad se resuelve exclusivamente en la capa de transporte/sesión y nunca es manipulable por el modelo en los parámetros de la herramienta).
- Modificación destructiva de las reglas de dominio, fórmulas de puntuación o esquema de persistencia establecidos en las Fases 01, 02 y 03.

## Users & context

1. **Agente especializado conectado por tubería local (`agent` vía `stdio`):** Ejecutado como subproceso por un entorno de trabajo o arquestador local con su propia llave de acceso asignada en el arranque; utiliza las cinco herramientas para consultar el contexto de su tarea, explorar hallazgos previos en dos fases, registrar nuevos descubrimientos y reportar qué recuerdos le resultaron útiles o erróneos.
2. **Agente remoto conectado por red (`agent` vía `HTTP/SSE`):** Se conecta concurrentemente al puerto del servidor de herramientas enviando su credencial en las cabeceras de transporte; invoca `romero_memory_context` antes de abordar una subtarea, `romero_memory_search` y `romero_memory_get` durante la investigación, `romero_memory_save` al registrar hallazgos y `romero_memory_feedback` al evaluar el resultado.
3. **Orquestador externo (`orchestrator` — proyecto independiente GANDALF WIZARD vía librería cliente `pkg/romeroclient` o `HTTP/SSE`):** Importa la librería cliente exportada por ROMERO para ensamblar paquetes de contexto antes de despachar agentes, registrar retroalimentación empírica al evaluar el éxito o fallo de las tareas, consultar el estado de solicitudes opacas encoladas, inspeccionar y resolver contradicciones activas, disparar ciclos de consolidación REM y verificar el estado del sistema.
4. **Operador o administrador del sistema (`user` / `service`):** Inicia el servidor de herramientas desde la línea de comandos en modo local (`stdio`) o en modo servidor de red (`HTTP/SSE`).

## What the user can do

- **Listar y descubrir las herramientas disponibles del servidor:** Un cliente conectado recibe exactamente la definición y esquema de entrada de las cinco herramientas canónicas: `romero_memory_save`, `romero_memory_search`, `romero_memory_get`, `romero_memory_context` y `romero_memory_feedback`.
- **Guardar un recuerdo (`romero_memory_save`):** El agente registra un hallazgo, decisión, error resuelto o procedimiento indicando tipo cognitivo, ámbito (`agent_private`, `task_shared`, `project_shared`, `org_shared`, `global_public`), resumen, contenido completo, estado epistemológico permitido para su identidad, y opcionalmente proyecto, identificador de tarea, llave canónica de tema (`topic_key`), etiquetas, entidades, importancia, clave de idempotencia y relaciones causales explícitas (`depends_on`, `caused_by`, `supports`, `derived_from`, `related_to`).
- **Buscar recuerdos en vista compacta (`romero_memory_search` — Fase 1 de Divulgación Progresiva):** El agente ejecuta una consulta híbrida indicando el texto de búsqueda y opcionalmente proyecto, ámbitos, tarea, tipos cognitivos, confianza mínima, número de saltos de propagación ($0\text{–}3$), límite de resultados e indicadores para incluir recuerdos supercedidos o consolidados, recibiendo únicamente vistas compactas sin el cuerpo `content`.
- **Inspeccionar el detalle completo de uno o varios recuerdos (`romero_memory_get` — Fase 2 de Divulgación Progresiva):** A partir de los identificadores obtenidos en la búsqueda, el agente solicita un identificador individual (`memory_id`) o un lote acotado de identificadores (`memory_ids`, de $1$ a $20$), con opción de consulta histórica en un punto del tiempo (`as_of`, o el par `valid_at` y `known_at`), para obtener su contenido completo encapsulado, sus metadatos bayesianos, su historial de versiones y sus relaciones causales visibles.
- **Construir un paquete de contexto óptimo (`romero_memory_context`):** El agente o el orquestador solicita en una sola llamada un paquete de contexto curado para su consulta dentro de un presupuesto máximo de tokens (`max_tokens`), con filtros opcionales de proyecto, ámbitos, tarea y saltos de propagación.
- **Reportar retroalimentación empírica (`romero_memory_feedback`):** El agente informa el resultado empírico tras utilizar un recuerdo (`helpful`, `unhelpful`, `contradicted`, `outdated`, `confirmed`, `corrected_by_user`, `retrieved`, `used_in_context`), incluyendo opcionalmente razón, identificador de tarea y recuerdo de reemplazo cuando se trate de una corrección autorizada.
- **Operar por transporte `stdio` o `HTTP/SSE` (integrado en `romero serve` o autónomo en `romero mcp`):** El operador puede lanzar `romero serve` para exponer simultáneamente el servidor REST (`:8420`) y el servidor MCP `HTTP/SSE` (`:8421` según `mcp_sse_port`) compartiendo en la misma memoria RAM la Caché Cognitiva L1, el `PowerGovernor` y los trabajadores en segundo plano, o bien ejecutar `romero mcp` para sesiones locales por tubería estándar (`stdio`, por defecto) o `HTTP/SSE` autónomo.
- **Consumir ROMERO desde el proyecto externo Gandalf Wizard mediante la librería cliente exportable (`pkg/romeroclient`):** Un proyecto externo en Go puede importar `pkg/romeroclient`, configurarlo con la dirección del servidor de ROMERO y su credencial, e invocar operaciones tipadas de ciclo de vida, recuperación híbrida, empaquetado de contexto, retroalimentación bayesiana, consulta de solicitudes opacas, listado/resolución de conflictos, disparo de consolidación REM y estado del sistema.

## What result they get

- **En descubrimiento de herramientas (`tools/list`):** Recibe invariablemente un catálogo de longitud exacta igual a $5$ con los nombres `romero_memory_save`, `romero_memory_search`, `romero_memory_get`, `romero_memory_context` y `romero_memory_feedback` y sus esquemas de validación.
- **En `romero_memory_save`:**
  - Si el solicitante posee permiso de lectura y escritura en el ámbito destino, recibe el identificador del recuerdo creado (o recuperado por idempotencia), su versión, estado (`active` o `conflicted`), estado epistemológico, confianza inicial y si su contenido fue sanitizado.
  - Si el solicitante posee permiso de escritura pero no de lectura sobre una tarea (`task_shared` write-only), recibe una respuesta estructurada con `status = "accepted"` y el identificador opaco `request_id`, sin revelar si existía un recuerdo previo con la misma llave de tema.
- **En `romero_memory_search`:** Recibe la lista ordenada de candidatos autorizados en vista estrictamente compacta (`id`, `summary`, `type`, `scope_type`, `status`, `epistemic_status`, `confidence`, `final_score`, `relations_count`, `min_hop_depth`, `has_conflict_alert`, `observed_at`) junto con el indicador `degraded_mode`. Ningún elemento incluye el campo `content`.
- **En `romero_memory_get`:** Para cada identificador solicitado (hasta $20$ por llamada):
  - Si el recuerdo existe y es legible por el solicitante (en modo actual o en el instante histórico solicitado), recibe el recuerdo con su cuerpo envuelto en `<romero_memory id="..." type="..." epistemic="..." confidence="..." source="..." untrusted_content="true">...</romero_memory>`, sus parámetros bayesianos $(\alpha, \beta)$, su lista de versiones inmutables y sus relaciones causales activas cuyos dos extremos sean legibles para el solicitante.
  - Si un identificador no existe o pertenece a un ámbito fuera de los permisos de lectura del solicitante, ese elemento se reporta como `not_found` opaco y se registra `denied` en auditoría sin abortar la entrega de los demás identificadores legibles del lote.
- **En `romero_memory_context`:** Recibe el documento XML de contexto ajustado a `max_tokens` donde cada recuerdo seleccionado está envuelto en `<romero_memory ... untrusted_content="true">` (aplicando contenido completo para los recuerdos principales y vista compacta para los secundarios según el presupuesto), junto con el total de tokens utilizados, el conteo de recuerdos incluidos, el indicador `degraded_mode` y la lista de identificadores seleccionados.
- **En `romero_memory_feedback`:**
  - Si el solicitante posee permiso de lectura y escritura sobre el recuerdo, recibe los nuevos valores calibrados $(\alpha, \beta)$, la confianza actualizada, el puntaje de utilidad y el estado resultante (`active` o `conflicted` si cayó bajo el umbral de cuarentena bayesiana $< 0.30$).
  - Si el solicitante solo posee permiso de escritura sobre la tarea, recibe `status = "accepted"` y el `request_id` opaco para procesamiento asíncrono.
- **En la librería cliente exportable (`pkg/romeroclient`):**
  - El proyecto consumidor externo recibe respuestas tipadas para cada operación de ROMERO, incluyendo distinción explícita entre resultado inmediato (`200`/`201`) y recibo opaco encolado (`202 Accepted` con `request_id` cuando la operación se ejecuta en modo solo-escritura sobre una tarea), así como errores tipados que permiten inspeccionar el código de estado y problema (`401`, `403`, `404`, `409`, `422`, `503`).

## What it must respect

- **Principio 9 (Tope estricto de 5 herramientas MCP y divulgación progresiva):**
  - El servidor de herramientas jamás expone más ni menos de las cinco herramientas canónicas.
  - `romero_memory_search` jamás devuelve el campo `content` completo.
- **Principio 14 (Aislamiento Zero-Trust e identidad criptográfica en ambos transportes):**
  - El contexto de identidad (`AuthContext`: inquilino, principal, agente, proyectos permitidos, ámbitos permitidos y carácter administrativo) se deriva exclusivamente de la credencial verificada en el arranque (`stdio`) o en la cabecera de la petición (`HTTP/SSE`), jamás de parámetros enviados dentro de los argumentos de la herramienta.
  - Toda lectura, búsqueda, subgrafo, contexto, captura o retroalimentación invocada por una herramienta aplica las mismas reglas de visibilidad, permisos explícitos de tarea (`task_permissions`), opacidad `not_found` y recibos opacos `accepted` que la interfaz de red, manteniendo $0.00\%$ de fuga entre ámbitos.
- **Principio 15 (Inmutabilidad tri-temporal y protección de rango epistemológico):**
  - Un agente estándar (`principal_type = 'agent'`) que invoque `romero_memory_save` con estado `verified` o que intente aplicar `corrected_by_user` en `romero_memory_feedback` es rechazado con error de autorización y registro `denied` en auditoría.
- **Principio 16 (Defensa contra inyección de instrucciones):**
  - Todo contenido escrito mediante `romero_memory_save` o `romero_memory_feedback` pasa por la sanitización determinista de caracteres de control y escape de etiquetas antes de persistirse, y todo contenido leído mediante `romero_memory_get` o `romero_memory_context` se entrega encapsulado en `<romero_memory ... untrusted_content="true">`.
- **Principios 17 y 18 (Presupuesto de latencia y preempción de Sueño REM en $< 10\text{ ms}$):**
  - Cada invocación a cualquiera de las cinco herramientas registra actividad de agente en tiempo real en el gobernador de potencia (`PowerGovernor`), interrumpiendo en menos de diez milisegundos cualquier ciclo de consolidación en Sueño REM en curso.

## How it proves it

- **Pruebas unitarias y de contrato del servidor de herramientas (Nivel 1 CPU):**
  - Verifican que el catálogo registrado expone exactamente las 5 herramientas canónicas con sus nombres y esquemas esperados.
  - Verifican la divulgación progresiva: `romero_memory_search` omite siempre `content`, y `romero_memory_get` devuelve el contenido encapsulado en `<romero_memory ... untrusted_content="true">`, versiones y relaciones visibles tanto para un `memory_id` individual como para un lote `memory_ids[]` (incluyendo modo *Point-in-Time*).
  - Verifican el manejo de escrituras opacas (`status = "accepted"` + `request_id`) en `romero_memory_save` y `romero_memory_feedback` para agentes con permiso de solo-escritura en `task_shared`.
  - Verifican que toda llamada a herramienta activa `BeginAgentRequest()` en el `PowerGovernor` y previene o interrumpe `rem_sleep` en $< 10\text{ ms}$.
- **Pruebas de transporte (`stdio` y `HTTP/SSE`) y seguridad Zero-Trust contra motor transaccional real:**
  - Verifican que el modo `stdio` rechaza arrancar si falta la credencial o es inválida, y que una sesión `stdio` autenticada ejecuta el ciclo completo de las 5 herramientas contra PostgreSQL 17 real.
  - Verifican que el modo `HTTP/SSE` rechaza con `401 Unauthorized` (y registra `denied` en auditoría) conexiones o llamadas sin cabecera válida, y aisla sesiones concurrentes de distintos agentes/inquilinos con $0.00\%$ de fuga cruzada en búsqueda, contexto, lectura por lote y retroalimentación.
  - Verifican que en un lote `memory_ids[]` de `romero_memory_get` que mezcle recuerdos propios y recuerdos de otro agente/proyecto/inquilino, el solicitante recibe únicamente los recuerdos autorizados y un resultado `not_found` opaco para los ajenos (con auditoría `denied` por cada acceso no autorizado).
- **Evaluación cognitiva reproducible (Nivel 2 — `romero eval`):**
  - Continúa ejecutándose en verde con $0.00\%$ de tasa de fuga Zero-Trust y cumplimiento de todos los indicadores cognitivos establecidos.

## Behaviour

- **Main path — Arranque y autenticación del transporte:**
  1. Cuando el operador ejecuta `romero mcp` en modo `stdio` (por defecto), el sistema exige una credencial válida (`--api-key` o `ROMERO_API_KEY`), la verifica contra el almacén de identidades antes de atender mensajes y fija ese `AuthContext` para toda la vida del proceso `stdio`.
  2. Cuando el operador ejecuta `romero serve` (que levanta tanto REST en `:8420` como MCP `HTTP/SSE` en `mcp_sse_port` `:8421` compartiendo en el mismo proceso la Caché Cognitiva L1 en RAM, el `PowerGovernor` y los trabajadores asíncronos) o `romero mcp --transport sse`, cada solicitud entrante por `HTTP/SSE` debe presentar su credencial en la cabecera (`Authorization: Bearer <key>` o `X-API-Key`), resolviendo el `AuthContext` asociado a esa petición.
- **Main path — Invocación de las 5 herramientas con gobierno de potencia:**
  1. Al recibir una llamada a cualquiera de las cinco herramientas (`romero_memory_save`, `romero_memory_search`, `romero_memory_get`, `romero_memory_context`, `romero_memory_feedback`), el servidor notifica el inicio de petición de agente al `PowerGovernor` (interrumpiendo de inmediato cualquier ciclo de Sueño REM activo) y libera el contador al finalizar.
  2. En `romero_memory_save`, delega en el servicio de captura (`CaptureMemory`) con el `AuthContext` de la sesión (incluyendo supersesión automática por `topic_key` cuando el rango epistemológico lo permite y aristas `relations`), devolviendo la vista del recuerdo creado o el recibo opaco `accepted` si se encoló para verificación asíncrona de tarea.
  3. En `romero_memory_search`, delega en el servicio de recuperación híbrida y transforma los candidatos a vistas estrictamente compactas (sin `content`).
  4. En `romero_memory_get`, acepta `memory_id` o `memory_ids` ($1..20$ UUIDs; si se proporcionan ambos, los unifica sin duplicados respetando el tope de $20$). Para cada UUID, consulta el recuerdo (en modo actual o *Point-in-Time*), obtiene sus versiones y filtra sus relaciones causales activas para incluir solo aquellas cuyos dos extremos sean legibles por el solicitante, envolviendo el `content` en `<romero_memory ... untrusted_content="true">`.
  5. En `romero_memory_context`, delega en el empaquetador submodular de contexto y devuelve el paquete XML seguro junto con sus métricas de presupuesto de tokens y modo degradado.
  6. En `romero_memory_feedback`, delega en el servicio de retroalimentación bayesiana y devuelve la nueva calibración del recuerdo o el recibo opaco `accepted`.
- **Edge cases:**
  - Un agente invoca `romero_memory_get` pasando un lote de $5$ identificadores donde $3$ son legibles y $2$ pertenecen a otro agente privado o no existen: la herramienta no falla toda la llamada; devuelve los $3$ recuerdos legibles completos y marca los $2$ restantes con estado individual `not_found`, registrando los eventos `denied` correspondientes en auditoría.
  - Un ciclo de Sueño REM está ejecutándose en segundo plano cuando entra una llamada a `romero_memory_search` o `romero_memory_context`: el `PowerGovernor` cancela el contexto de Sueño REM en $< 10\text{ ms}` y la herramienta responde sin esperar a la descarga del modelo generativo.
  - Un agente con permiso de solo-escritura en `task_shared` invoca `romero_memory_save` o `romero_memory_feedback`: recibe un resultado exitoso de herramienta con `status = "accepted"` y el `request_id` opaco generado por la cola durable.
- **Error paths:**
  - Arranque en modo `stdio` sin credencial o con una credencial inexistente, inactiva o expirada: registra `denied` en auditoría y termina con error de autenticación sin abrir el bucle de lectura de herramientas.
  - Petición en modo `HTTP/SSE` sin cabecera de autenticación o con credencial inválida/expirada: registra `denied` en auditoría y responde `401 Unauthorized`.
  - Llamada a `romero_memory_get` sin ningún identificador o superando el máximo de $20$ identificadores por lote, o llamada a cualquiera de las herramientas con parámetros fuera de rango: devuelve error estructurado de validación de argumentos sin mutar estado.
  - Llamada a `romero_memory_save` o `romero_memory_feedback` intentando escribir en un proyecto o ámbito no autorizado para el principal, o asignando estado `verified` / `corrected_by_user` desde una identidad de tipo `agent`: registra `denied` en auditoría y devuelve error de autorización prohibida.

## Acceptance criteria

- **AC-01 (Catálogo estricto de 5 herramientas canónicas):** Given un cliente conectado al servidor de herramientas en modo `stdio` o `HTTP/SSE`, When solicita la lista de herramientas disponibles, Then el servidor devuelve exactamente 5 herramientas llamadas `romero_memory_save`, `romero_memory_search`, `romero_memory_get`, `romero_memory_context` y `romero_memory_feedback` con sus esquemas de entrada tipados, sin exponer ninguna herramienta adicional.
- **AC-02 (Divulgación progresiva en dos fases y lote con opacidad en `romero_memory_get`):** Given recuerdos activos visibles y un recuerdo privado ajeno $M_{\text{other}}$, When un agente invoca `romero_memory_search`, Then recibe únicamente vistas compactas (`summary`, `confidence`, `epistemic_status`, `relations_count`, `has_conflict_alert`) sin el campo `content`; y When invoca `romero_memory_get` pasando un lote `memory_ids` que incluye recuerdos propios y $M_{\text{other}}$ (en modo actual o *Point-in-Time*), Then recibe para los recuerdos propios su `content` envuelto en `<romero_memory ... untrusted_content="true">`, sus versiones y sus relaciones causales visibles, mientras que para $M_{\text{other}}$ recibe únicamente estado `not_found` opaco con registro `denied` en auditoría.
- **AC-03 (Captura, retroalimentación bayesiana y recibos opacos en `romero_memory_save` y `romero_memory_feedback`):** Given un agente autenticado, When invoca `romero_memory_save` y posteriormente `romero_memory_feedback` sobre un ámbito donde posee lectura y escritura, Then el recuerdo se persiste sanitizado (con soporte de `topic_key`, `epistemic_status`, `entities` y `relations`) y su distribución bayesiana $(\alpha, \beta)$ se recalibra; y When el agente posee únicamente permiso de escritura en `task_shared`, Then ambas herramientas devuelven `status = "accepted"` con un `request_id` opaco sin filtrar el estado ni la existencia de recuerdos en esa tarea.
- **AC-04 (Empaquetado de contexto XML seguro en `romero_memory_context`):** Given un conjunto de recuerdos interconectados dentro del ámbito autorizado del solicitante, When invoca `romero_memory_context` con una consulta y un presupuesto `max_tokens`, Then recibe el paquete de contexto XML optimizado donde todo recuerdo está encapsulado con `untrusted_content="true"`, el consumo total de tokens no supera `max_tokens` y no se filtra ningún recuerdo ni relación fuera de su frontera de permisos ($0.00\%$ de fuga).
- **AC-05 (Autenticación Zero-Trust diferenciada en transportes `stdio` y `HTTP/SSE`):** Given el servidor de herramientas configurado (tanto en `romero mcp` como en el listener MCP `HTTP/SSE` integrado en `romero serve`), When se inicia en modo `stdio` sin credencial válida o se realiza una petición `HTTP/SSE` sin cabecera `Authorization: Bearer` / `X-API-Key` válida, Then el acceso es rechazado y se registra `denied` en auditoría; y When dos sesiones de distintos agentes o inquilinos invocan herramientas sobre `HTTP/SSE`, Then cada llamada opera estrictamente bajo su propio `AuthContext` sin contaminación ni fuga cruzada.
- **AC-06 (Preempción en $< 10\text{ ms}` del Sueño REM y coherencia de Caché L1 entre REST y MCP):** Given el sistema ejecutando un ciclo de consolidación en estado `rem_sleep`, When ingresa una invocación a cualquiera de las 5 herramientas por `stdio` o `HTTP/SSE`, Then el gobernador de potencia cancela el contexto de Sueño REM en menos de $10\text{ ms}`, transiciona a estado activo (`guard` o `burst`) y atiende la herramienta sin bloquearse en la liberación del modelo generativo; y cuando MCP `HTTP/SSE` corre integrado en `romero serve`, comparte la misma instancia en RAM de Caché Cognitiva L1 y `PowerGovernor` que el servidor REST.
- **AC-07 (Librería cliente pública exportable `pkg/romeroclient` para Gandalf Wizard):** Given un consumidor externo (como el repositorio independiente de Gandalf Wizard) que importa `pkg/romeroclient` sin acceso a paquetes `internal/` de ROMERO, When invoca sus métodos autenticados contra ROMERO (`CaptureMemory`, `GetMemory`, `UpdateMemory`, `SupersedeMemory`, `ArchiveMemory`, `RecordFeedback`, `GetRequestStatus`, `SearchMemories`, `BuildContext`, `GetSubgraph`, `ListConflicts`, `ReviewConflict`, `TriggerREM` y `GetSystemStatus`), Then ejecuta de extremo a extremo el ciclo de empaquetado de contexto, captura, retroalimentación bayesiana (incluyendo manejo de recibos `202 Accepted` con `request_id`), arbitraje de conflictos y consolidación REM, devolviendo errores tipados ante respuestas `401`/`403`/`404`/`409`/`422`/`503`.

## Points to clarify

- **suposición tomada** — El comando `romero mcp` soporta seleccionar el modo de transporte (`stdio` por defecto para integración directa con clientes locales, o `sse`/`http` escuchando en el puerto `mcp_sse_port` configurado en `romero.toml`, por defecto `8421`), y en modo `stdio` lee la credencial desde la bandera `--api-key` o la variable de entorno `ROMERO_API_KEY`; adicionalmente, `romero serve` levanta en el mismo proceso el listener MCP `HTTP/SSE` en `mcp_sse_port` (`:8421`) compartiendo la Caché Cognitiva L1 y el `PowerGovernor` con REST (`:8420`). *Estado:* Validada en `clarify` (held).
- **suposición tomada** — En `romero_memory_get`, el límite máximo de identificadores por llamada en lote (`memory_ids`) es de $20$ recuerdos, devolviendo error de validación si la lista está vacía (cuando tampoco se envía `memory_id`) o si supera los $20$ elementos. *Estado:* Validada en `clarify` (held).
- **suposición tomada** — Cuando una llamada a cualquiera de las 5 herramientas falla por validación de negocio (`422`), permiso denegado (`403`), conflicto de estado/idempotencia (`409`) o recurso inexistente en operación puntual (`404`), la respuesta de la herramienta informa un resultado de error de herramienta con código semántico estructurado (`validation_error`, `forbidden`, `conflict`, `not_found`, `service_unavailable`) sin cerrar ni derribar la sesión de transporte (`stdio` o `HTTP/SSE`). *Estado:* Validada en `clarify` (held).
- **suposición tomada** — El paquete exportable `pkg/romeroclient` no importa ningún paquete bajo `internal/`, de modo que cualquier módulo Go externo (como el repositorio independiente de Gandalf Wizard) pueda importarlo limpiamente con `go get` sin acoplarse a detalles internos del servidor. *Estado:* Validada en `clarify` (held).

## Clarifications

### Session 2026-11-15
- **Q1:** ¿Cómo deben coordinarse `romero serve` (`:8420`) y el transporte MCP `HTTP/SSE` (`:8421`) respecto a la Caché Cognitiva L1 en RAM y el `PowerGovernor`, y confirmas las 4 suposiciones tomadas del spec? → **A1:** `romero serve` levanta también el listener MCP `HTTP/SSE` en `mcp_sse_port` (`:8421`) compartiendo en la misma memoria RAM la Caché Cognitiva L1, el `PowerGovernor` y los trabajadores en segundo plano con REST (`:8420`), además de mantener `romero mcp` para sesiones `stdio` locales (y `sse` autónomo); `romero_memory_save` mapea directamente a `CaptureMemory` (incluyendo supersesión por `topic_key` y `relations`); y se confirman las 4 `suposición tomada` (`validated (held)`).

## Revisions

- **2026-11-15 (`specify`):** Actualizado el alcance de la Fase 04 a petición del usuario (cambio de Opción B a Opción A) para incluir en este repositorio la librería cliente pública exportable (`pkg/romeroclient`, `AC-07`) además del Servidor MCP (`stdio` + `HTTP/SSE`, `AC-01`..`AC-06`), facilitando su importación directa desde el repositorio independiente de Gandalf Wizard.
- **2026-11-15 (`clarify`):** Cerrada la ronda de clarificación (`status: clarified`): `romero serve` expone de forma integrada el servidor MCP `HTTP/SSE` en `:8421` compartiendo en RAM la Caché Cognitiva L1 y el `PowerGovernor` con REST (`:8420`), además del subcomando `romero mcp` (`stdio` / `sse`); validadas las 4 suposiciones tomadas.
