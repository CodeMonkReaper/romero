# ROMERO + GANDALF WIZARD

## Especificación Maestra de Producto, Negocio, Arquitectura e Ingeniería Cognitiva

**Versión:** 2.0.0 — Edición Definitiva Multi-Perfil (`Lite` / `Balanced` / `Ultra`)

**Fecha:** 10 de noviembre de 2026

**Estado:** Aprobado para implementación inmediata (Baseline de Ingeniería)

**Idioma:** Español

**Foco Primario:** Construcción de **ROMERO** como plataforma de memoria cognitiva neuro-simbólica, bitemporal, multiagente y evolutiva de ultra alto rendimiento, diseñada para integrarse mediante contratos estrictos con el Meta Harness **GANDALF WIZARD**.

**Sistemas Operativos Soportados:** Linux (`amd64` / `arm64`), macOS (`Intel` / `Apple Silicon M-Series`) y Windows (`amd64` / `arm64`).

**Hardware de Referencia (Target Baseline):** Intel Core i5-12400H ($8\text{ núcleos} / 12\text{ hilos}$), NVIDIA GeForce RTX 4050 ($6\text{ GB}$ GDDR6 VRAM) y $24\text{ GB}$ de RAM.

---

# Índice General

1. [Resumen Ejecutivo y Propuesta de Valor](#1-resumen-ejecutivo-y-propuesta-de-valor)
2. [Contexto de Negocio y Radiografía Competitiva (ROMERO vs. Engram vs. Soluciones Cloud)](#2-contexto-de-negocio-y-radiografía-competitiva-romero-vs-engram-vs-soluciones-cloud)
3. [Visión del Sistema y Delimitación de Dominios](#3-visión-del-sistema-y-delimitación-de-dominios)
4. [Principios Arquitectónicos No Negociables](#4-principios-arquitectónicos-no-negociables)
5. [Perfiles de Potencia, Estados Dinámicos y Presupuesto de Hardware](#5-perfiles-de-potencia-estados-dinámicos-y-presupuesto-de-hardware)
6. [Stack Tecnológico Definitivo y Estrategia Multiplataforma](#6-stack-tecnológico-definitivo-y-estrategia-multiplataforma)
7. [Arquitectura Lógica y Los Cuatro Motores Cognitivos de Alta Potencia](#7-arquitectura-lógica-y-los-cuatro-motores-cognitivos-de-alta-potencia)
8. [Modelo Conceptual, Epistemológico y Bayesiano de Memoria (ROMERO Core)](#8-modelo-conceptual-epistemológico-y-bayesiano-de-memoria-romero-core)
9. [Scopes, Identidad, Seguridad Multiagente y Gobernanza Zero-Trust](#9-scopes-identidad-seguridad-multiagente-y-gobernanza-zero-trust)
10. [Modelo Físico de Datos y Esquema SQL Completo (PostgreSQL 17 + pgvector)](#10-modelo-físico-de-datos-y-esquema-sql-completo-postgresql-17--pgvector)
11. [Captura, Inferencia Lógica (NLI), Idempotencia, Topic Keys y Supersesión](#11-captura-inferencia-lógica-nli-idempotencia-topic-keys-y-supersesión)
12. [Modelo Matemático Completo: Ranking, Activación Propagada, Reranking y Empaquetado Submodular](#12-modelo-matemático-completo-ranking-activación-propagada-reranking-y-empaquetado-submodular)
13. [Motor de Consolidación "Sueño REM" (Sleep-Time Compute) y Aprendizaje Verificable](#13-motor-de-consolidación-sueño-rem-sleep-time-compute-y-aprendizaje-verificable)
14. [Contratos de Integración Completos (REST API OpenAPI 3.1 y Servidor MCP)](#14-contratos-de-integración-completos-rest-api-openapi-31-y-servidor-mcp)
15. [Diseño del Código Base en Go (Estructura, Interfaces, Caché L1 y Consultas SQL)](#15-diseño-del-código-base-en-go-estructura-interfaces-caché-l1-y-consultas-sql)
16. [Infraestructura Local, Configuración de Perfiles y Compilación Cruzada](#16-infraestructura-local-configuración-de-perfiles-y-compilación-cruzada)
17. [Observabilidad, Auditoría y Framework de Evaluación Reproducible](#17-observabilidad-auditoría-y-framework-de-evaluación-reproducible)
18. [Registro Definitivo de Decisiones Arquitectónicas (ADRs)](#18-registro-definitivo-de-decisiones-arquitectónicas-adrs)
19. [Matriz de Riesgos, Amenazas y Mitigaciones](#19-matriz-de-riesgos-amenazas-y-mitigaciones)
20. [Qué NO Construir Todavía (Fronteras Anti-Sobreingeniería)](#20-qué-no-construir-todavía-fronteras-anti-sobreingeniería)
21. [Roadmap y Plan de Ejecución por Sprints](#21-roadmap-y-plan-de-ejecución-por-sprints)
22. [Glosario Técnico, Cognitivo y Epistemológico](#22-glosario-técnico-cognitivo-y-epistemológico)
23. [Definición de Éxito del Proyecto](#23-definición-de-éxito-del-proyecto)

---

# 1. Resumen Ejecutivo y Propuesta de Valor

Este documento constituye la especificación maestra y definitiva para diseñar, construir, evaluar y operar dos sistemas complementarios pero estrictamente desacoplados:

* **ROMERO:** Plataforma de memoria persistente neuro-simbólica, contextual, bitemporal, epistemológica, multiagente y evolutiva. Su responsabilidad es capturar experiencias, validarlas lógicamente, convertirlas en recuerdos trazables, almacenarlos con control de vigencia, relacionarlos en un grafo causal, recuperarlos con precisión quirúrgica y consolidarlos autónomamente bajo reglas explícitas de calidad, seguridad y permisos.
* **GANDALF WIZARD:** *Meta Harness* que coordina agentes, herramientas, tareas, flujos de ejecución concurrentes y evaluación. Su responsabilidad es decidir qué agente actúa, con qué paquete de contexto, qué herramientas puede invocar, cómo se supervisa su ejecución y cómo se evalúan los resultados operacionales.

### El Principio Rector

**Construir primero una memoria fiable, verificable, ultrarrápida y agnóstica (ROMERO); después conectar y ampliar el harness de agentes (GANDALF WIZARD).**

La relación entre ambos sistemas se establece exclusivamente mediante contratos estables y públicos (**REST API OpenAPI 3.1** y **Model Context Protocol - MCP**), nunca mediante dependencias internas ni lecturas directas de base de datos. Gandalf Wizard consume Romero como un servicio externo autenticado; Romero no depende de Gandalf Wizard para funcionar y puede servir simultáneamente a cualquier agente de código, CLI, IDE o sistema multiagente.

### Potencia Cognitiva Adaptativa sin Límites Artificiales

A diferencia de soluciones básicas que recortan capacidades por temor al consumo de recursos, **ROMERO** está diseñado con una **arquitectura elástica de tres perfiles de potencia (`Lite`, `Balanced`, `Ultra`)** y **tres estados dinámicos de ejecución (`Guardia`, `Ráfaga`, `Sueño REM`)**. En su perfil **Ultra**, aprovecha los núcleos del procesador, la memoria RAM del sistema y los Tensor Cores de la tarjeta gráfica para activar cuatro motores cognitivos avanzados:

1. **Recuperación Cuádruple con Cross-Encoder Reranker en GPU:** Combina búsqueda exacta de código por trigramas (`pg_trgm`), búsqueda léxica Full-Text/BM25, vectores densos/dispersos (`BGE-M3` en media precisión `halfvec`) y reordenamiento neuronal profundo (`bge-reranker-v2-m3`).
2. **Grafo Bitemporal con Caché L1 en RAM y Activación Propagada (*Spreading Activation*):** Mantiene el subgrafo activo en la memoria del proceso Go (latencia sub-milisegundo) y propaga puntajes de relevancia a través de cadenas causales multi-salto sin requerir Neo4j.
3. **Motor NLI de Contradicciones y Calibración Bayesiana:** Evalúa cada nuevo recuerdo mediante un clasificador de Inferencia de Lenguaje Natural (`DeBERTa-v3 NLI`) para detectar contradicciones lógicas en tiempo real y actualiza la confianza mediante distribuciones Beta-Bernoulli basadas en evidencia empírica.
4. **Consolidación "Sueño REM" (*Sleep-Time Compute*) y Empaquetado Submodular:** Durante ventanas de inactividad de la GPU, un modelo generativo local ligero ($3\text{B}$ cuantizado) destila agrupaciones de recuerdos episódicos repetidos en memoria procedimental y semántica verificable, mientras el `Context Builder` aplica optimización submodular para maximizar la densidad informativa por token.

---

# 2. Contexto de Negocio y Radiografía Competitiva (ROMERO vs. Engram vs. Soluciones Cloud)

## 2.1 El Problema Estructural de la Memoria en Sistemas Multiagente

Los agentes de inteligencia artificial actuales enfrentan tres fallos críticos cuando operan en proyectos reales de ingeniería de software u operaciones complejas:

1. **Amnesia de Sesión y Redescubrimiento Costoso:** Cada nueva sesión obliga al agente a explorar nuevamente el repositorio o el entorno, gastando miles de tokens y repitiendo errores que ya fueron diagnosticados en días anteriores.
2. **Envenenamiento Epistemológico (*Memory Poisoning*):** Los gestores de memoria actuales guardan texto plano sin distinguir entre una suposición temporal de un agente explorador (`hypothesis`) y un hecho comprobado en producción (`verified`). Cuando otro agente recupera esa suposición, la asume como verdad absoluta y rompe el sistema.
3. **Ceguera Temporal y Relacional:** Cuando una configuración o decisión arquitectónica cambia, las bases vectoriales convencionales devuelven mezclados el estado antiguo y el nuevo, o bien sobrescriben destructivamente el registro previo impidiendo auditar por qué falló una ejecución pasada.

## 2.2 Matriz Competitiva Exhaustiva: Engram vs. Frameworks Cloud/Graph vs. ROMERO

| Dimensión Técnica y de Negocio | Engram (`Gentleman-Programming/engram`) | Frameworks Cloud / Grafos Pesados (Zep, Mem0, GraphRAG estándar) | ROMERO (Arquitectura Cognitiva Multi-Perfil) | Ventaja Estratégica para Gandalf Wizard |
| :--- | :--- | :--- | :--- | :--- |
| **Foco de Diseño** | Memoria individual para un único asistente de código local. | Servicios SaaS o stacks pesados orientados a chatbots conversacionales. | **Motor de memoria neuro-simbólico para Meta Harnesses multiagente e ingeniería.** | Escala desde $1$ agente local hasta decenas de agentes concurrentes con rigor transaccional. |
| **Motor de Base de Datos** | SQLite local (`~/.engram/engram.db`). | Neo4j + Base Vectorial externa + Redis + Postgres. | **PostgreSQL 17 + `pgvector` (`halfvec`) + `pg_trgm` + Caché L1 en Go.** | Cero bloqueos `SQLITE_BUSY` en concurrencia y cero sobrecarga de mantener Neo4j o Redis externos. |
| **Motor de Búsqueda** | Solo léxica (**SQLite FTS5**). Falla si cambian las palabras o sinónimos. | Vectorial básica o extracción lenta de entidades por LLM en cada turno. | **Cuádruple: Trigramas (`pg_trgm`) + FTS Lingüístico/Código + Vectores `BGE-M3` + Cross-Encoder Reranker.** | Precisión quirúrgica tanto en símbolos exactos (`ERR_CONN_RESET`) como en conceptos abstractos. |
| **Razonamiento Relacional** | Inexistente (registros planos aislados). | Consultas Cypher lentas dependientes de LLMs externos en el camino crítico. | **Grafo Bitemporal con Activación Propagada (*Spreading Activation*) sobre Caché L1 y CTEs Recursivos.** | Descubre cadenas completas de causa, dependencia e impacto en $< 5\text{ ms}$. |
| **Gobernanza de la Verdad (Epistemología)** | Plana. Un rumor tiene el mismo valor que una regla verificada. | Plana. Todo hecho extraído por el LLM recibe el mismo trato. | **5 Estados Epistemológicos + Calibración Bayesiana ($\alpha, \beta$) + Detector NLI de Contradicciones.** | Impide que alucinaciones o hipótesis reemplacen hechos verificados. |
| **Manejo del Tiempo e Historial** | Sobrescritura destructiva por `topic_key`. | Bitemporalidad parcial sólo en grafos conversacionales. | **Modelo Tri-Temporal (3 Relojes) + Supersesión Inmutable + Consultas *Point-in-Time*.** | Permite viajar en el tiempo para auditar exactamente qué sabía un agente en un instante $t_x$. |
| **Consolidación y Aprendizaje** | Manual (depende de que el agente decida resumir). | Síncrona y costosa en cada mensaje. | **Asíncrona "Sueño REM" (*Sleep-Time Compute*): destila episodios en procedimientos cuando la GPU está libre.** | Los agentes aprenden *runbooks* reutilizables a partir de errores repetidos sin penalizar la latencia diurna. |
| **Seguridad y Aislamiento** | Delegado al *prompt* del agente (vulnerable a fugas entre proyectos). | Basado en `user_id` simple de aplicación web. | **Zero-Trust en Servidor: 5 niveles de Scope (`private`, `task`, `project`, `org`, `global`) + Defensa Anti-Injection.** | Aislamiento duro garantizado para que agentes de distintos proyectos o tareas no filtren secretos. |
| **Superficie MCP (Consumo de Contexto)** | $\sim 26$ herramientas expuestas (alta contaminación de tokens). | Sin estándar MCP optimizado o APIs propietarias fragmentadas. | **5 herramientas MCP nucleares con Divulgación Progresiva y Empaquetado Submodular.** | Mínimo gasto de ventana de contexto y máxima densidad de información relevante por token. |
| **Portabilidad y Distribución** | Binario en Go. | Múltiples microservicios en Python/Node difíciles de portar. | **Binario estático único en Go (`CGO_ENABLED=0`) para Linux, macOS y Windows.** | Instalación limpia y velocidad nativa en los tres sistemas operativos principales. |

## 2.3 Lo que ROMERO Adopta y Eleva de Engram

ROMERO conserva los tres aciertos ergonómicos que hicieron popular a Engram, pero los rediseña bajo estándares de ingeniería de sistemas:

1. **Llaves de Tema Canónicas (`topic_key`) con Supersesión Automática e Inmutable:** Los agentes pueden guardar conocimiento bajo rutas semánticas legibles (ej. `auth/jwt-validation-policy`, `build/windows-cross-compile`). Si la llave ya existe activa en ese proyecto, Romero no destruye el registro anterior como hace Engram: verifica la jerarquía epistemológica, cierra el `valid_until` del recuerdo previo, lo pasa a estado `superseded`, inserta la nueva versión activa y crea el arco `supersedes` en el grafo relacional.
2. **Divulgación Progresiva (*Progressive Disclosure*) en Dos Fases:** `romero_memory_search` devuelve únicamente resúmenes compactos (`summary`), estado epistemológico, confianza bayesiana y procedencia ($\sim 45\text{ tokens}$ por ítem). El cuerpo extenso (`content`) solo se transfiere cuando el agente invoca `romero_memory_get` o mediante el optimizador submodular de `romero_memory_context`.
3. **Distribución en Binario Único en Go:** API REST, servidor MCP, CLI administrativa, Caché L1 de grafo y orquestador de workers viven dentro de un solo ejecutable compilado estáticamente.

---

# 3. Visión del Sistema y Delimitación de Dominios

## 3.1 ROMERO (Plataforma de Memoria Cognitiva)

**Definición:** Sistema de memoria externa neuro-simbólica para agentes de inteligencia artificial y aplicaciones. Permite capturar experiencias y conocimiento, validar su consistencia lógica, almacenarlos con trazabilidad tri-temporal, relacionarlos en un grafo causal, recuperarlos mediante búsqueda híbrida y activación propagada, y consolidarlos autónomamente bajo políticas estrictas de seguridad y permisos.

### Objetivos Nucleares de ROMERO

* Mantener conocimiento útil, estructurado y verificable entre sesiones, reinicios y distintos agentes.
* Diferenciar rigurosamente entre hechos verificados (`verified`), observaciones empíricas (`observed`), reportes (`reported`), inferencias (`inferred`) e hipótesis (`hypothesis`), y entre memoria episódica, semántica, procedimental y relacional.
* Recuperar recuerdos combinando coincidencia exacta de código, búsqueda léxica, similitud vectorial densa/dispersa, propagación por grafo causal y reordenamiento con Cross-Encoder.
* Conservar **procedencia obligatoria e inmutable**: quién aportó cada recuerdo, cuándo ocurrió el evento, cuándo se registró, bajo qué ejecución (`run_id`) y con qué evidencia.
* Representar cambios en el tiempo mediante tres relojes independientes sin borrar silenciosamente el historial.
* Permitir memoria privada del agente, compartida por tarea, compartida por proyecto, organizacional y global con aislamiento en el servidor.
* Detectar contradicciones lógicas en tiempo real mediante modelos NLI y destilar aprendizajes durante periodos de baja carga (*Sueño REM*).
* Ser $100\%$ independiente del proveedor de LLMs, framework de agentes, lenguaje consumidor y sistema operativo.

### No Objetivos Iniciales de ROMERO

* Modificar los pesos neuronales (*fine-tuning*) de los modelos fundacionales que consumen la memoria.
* Reemplazar la base de datos transaccional del dominio de negocio de las aplicaciones clientes.
* Ejecutar agentes autónomos de cara al usuario o planificar tareas operativas (responsabilidad exclusiva de Gandalf Wizard).
* Aceptar que cualquier salida textual de un agente se convierta automáticamente en verdad verificada sin pasar por validación epistemológica y NLI.

## 3.2 GANDALF WIZARD (Meta Harness de Orquestación)

**Definición:** Meta Harness encargado de coordinar, gobernar y supervisar la ejecución de sistemas multiagente. Proporciona un runtime determinista para descomponer objetivos en tareas, seleccionar agentes según capacidades, inyectar el paquete de contexto óptimo, gobernar el uso de herramientas, supervisar ejecuciones concurrentes y evaluar resultados.

### Responsabilidades Principales de Gandalf Wizard

* Recibir, autenticar y normalizar solicitudes complejas.
* Descomponer objetivos en grafos acíclicos dirigidos (DAGs) de tareas y subtareas.
* Seleccionar agentes del `Agent Registry` según capacidades declaradas, disponibilidad y políticas.
* Gestionar ciclos de ejecución, delegación, límites de presupuesto, timeouts, reintentos y cancelaciones.
* Invocar a **ROMERO** para construir el contexto exacto necesario para cada agente sin exponer información de otros proyectos o tareas.
* Autorizar herramientas y auditar cada invocación (`Tool Governance`).
* Evaluar el resultado final de las tareas y enviar a **ROMERO** tanto candidatos a recuerdos como señales empíricas de utilidad (`memory_feedback`).

## 3.3 Matriz de Responsabilidades: ROMERO vs. GANDALF WIZARD

| Capacidad del Sistema | ROMERO (Memoria Cognitiva) | GANDALF WIZARD (Meta Harness) |
| :--- | :--- | :--- |
| **Persistencia duradera y versionada de recuerdos** | **Responsable único (Fuente de verdad)** | Consumidor vía REST API / MCP |
| **Búsqueda híbrida, grafo causal y reranking** | **Responsable único** | Solicita contexto y lo inyecta al agente |
| **Validación NLI de contradicciones y supersesión** | **Responsable único** | Recibe alertas de conflicto si aplica |
| **Aislamiento y permisos de memoria por Scope** | **Responsable de aplicar en servidor** | Emite tokens/credenciales de identidad de ejecución |
| **Planificación de tareas y orquestación de agentes** | No participa | **Responsable único** |
| **Gobierno y ejecución de herramientas externas** | Expone las $5$ herramientas MCP de memoria | **Responsable de autorizar y supervisar herramientas** |
| **Estado operacional de ejecuciones (`tasks`/`runs`)** | Almacena `external_source_id` como procedencia | **Responsable del registro operacional en vivo** |
| **Evaluación de éxito de tareas y feedback** | Recibe señales y recalcula utilidad bayesiana | **Responsable de evaluar al agente y emitir la señal** |
| **Consolidación "Sueño REM" (Episódica $\rightarrow$ Procedimental)** | **Responsable único** | Puede consultar procedimientos consolidados |

---

# 4. Principios Arquitectónicos No Negociables

1. **Separación Estricta de Dominios:** Memoria (Romero) y Orquestación (Gandalf Wizard) son productos independientes comunicados exclusivamente por contratos de red documentados.
2. **API-First y Tipado Fuerte:** Toda operación de Romero está regida por contratos OpenAPI 3.1 y JSON Schema validados en el borde antes de tocar la lógica de aplicación.
3. **MCP como Adaptador Delgado:** El servidor MCP expone herramientas para agentes invocando exactamente los mismos servicios de aplicación que la API REST.
4. **PostgreSQL como Única Fuente de Verdad Persistente:** Transacciones relacionales, vectores densos/dispersos, índices de texto, grafo de relaciones y cola de trabajos residen en PostgreSQL 17 con garantías ACID.
5. **Procedencia Obligatoria (*Mandatory Provenance*):** Ningún recuerdo puede existir sin un registro de origen verificable (`memory_sources`).
6. **Aislamiento Zero-Trust por Defecto:** Todo filtro de permisos (`tenant_id`, `project_id`, `scope_type`, `agent_id`) se aplica dentro del motor SQL antes de evaluar similitud vectorial o textual.
7. **Trazabilidad Tri-Temporal:** Todo recuerdo registra el tiempo de observación (`observed_at`), el tiempo de registro (`created_at`) y la ventana de validez (`valid_from`, `valid_until`).
8. **Calibración Epistemológica y Bayesiana:** Confianza, importancia y relevancia son dimensiones matemáticamente separadas. Las hipótesis jamás desplazan a los hechos verificados.
9. **Inmutabilidad Histórica y Supersesión:** El conocimiento nuevo no borra el pasado; cierra la vigencia del recuerdo anterior y enlaza la cadena causal.
10. **Potencia Elástica por Perfiles:** El sistema adapta su huella de hardware mediante tres perfiles (`Lite`, `Balanced`, `Ultra`) sin alterar una sola línea de los contratos públicos ni del esquema de datos.
11. **Portabilidad Nativa Multiplataforma:** El núcleo de Romero compila con `CGO_ENABLED=0` en un único binario nativo para **Linux, macOS y Windows**.
12. **Defensa Activa contra Prompt Injection y Envenenamiento:** Los recuerdos recuperados son tratados siempre como evidencia contextual etiquetada y nunca como instrucciones de control del sistema.

---

# 5. Perfiles de Potencia, Estados Dinámicos y Presupuesto de Hardware

## 5.1 Hardware de Referencia (Target Baseline)

El diseño de **ROMERO** toma como línea base tu máquina de referencia para exprimir su potencial sin ahogar al sistema operativo ni a **Gandalf Wizard**:

* **Procesador (CPU):** Intel Core i5-12400H ($8\text{ núcleos}: 4\text{ P-Cores} + 4\text{ E-Cores}$, $12\text{ hilos}$, instrucciones AVX2 / VNNI).
* **Tarjeta Gráfica (GPU):** NVIDIA GeForce RTX 4050 ($6\text{ GB}$ GDDR6 VRAM, arquitectura Ada Lovelace, Tensor Cores FP16/INT8).
* **Memoria Principal (RAM):** $24\text{ GB}$ DDR4/DDR5.
* **Almacenamiento:** Unidad de estado sólido NVMe M.2.
* **Sistemas Operativos:** Linux, macOS y Windows.

## 5.2 Los Tres Perfiles de Potencia Configurables (`ROMERO_PROFILE`)

Al iniciar Romero (`romero serve --profile=ultra`), el `PowerGovernor` interno activa los motores correspondientes manteniendo idéntica la API pública:

| Característica / Motor | Perfil `lite` (Portátiles sin GPU / CI) | Perfil `balanced` (Uso Diario Equilibrado) | Perfil `ultra` (Máxima Potencia Cognitiva) |
| :--- | :--- | :--- | :--- |
| **Búsqueda Full-Text + Trigramas (`pg_trgm`)** | Activo (PostgreSQL) | Activo (PostgreSQL) | Activo (PostgreSQL) |
| **Vectores Densos + Dispersos (`halfvec` HNSW)** | Activo (CPU ONNX / API) | Activo (GPU FP16 `BGE-M3`) | Activo (GPU FP16 `BGE-M3` $1024\text{-dim}$) |
| **Caché Cognitiva L1 de Grafo en RAM (Go)** | Desactivado (Lee de Postgres) | Activo (Proyectos calientes, $128\text{ MB}$) | Activo (Grafo completo + Vectores calientes, $350\text{ MB}$) |
| **Activación Propagada (*Spreading Activation*)** | $1\text{ salto}$ SQL directo | $2\text{ saltos}$ CTE Recursivo | $3\text{ saltos}$ en Caché L1 + PageRank Personalizado |
| **Cross-Encoder Reranker (`bge-reranker-v2-m3`)** | Desactivado (Usa RRF puro) | Bajo demanda en consultas complejas | **Siempre activo en GPU sobre el Top-$30$** |
| **Detector NLI de Contradicciones (`DeBERTa-v3`)** | Heurístico + Similitud Coseno | Activo asíncrono en Worker GPU | **Activo en tiempo real (Captura + Worker GPU)** |
| **Consolidador "Sueño REM" (SLM $3\text{B}$ Local)** | Desactivado | Programado manual (`romero consolidate`) | **Automático en ventanas de inactividad GPU** |
| **Consumo Total de RAM del Sistema** | $\approx 800\text{ MB}$ | $\approx 2.2\text{ GB}$ | $\approx 4.2\text{ GB a }5.0\text{ GB}$ (de $24\text{ GB}$) |
| **Consumo de VRAM (RTX 4050 $6\text{ GB}$)** | $0\text{ MB}$ | $\approx 1.2\text{ GB}$ | $2.3\text{ GB}$ (Guardia/Ráfaga) $\rightarrow 4.5\text{ GB}$ (Sueño REM) |
| **Latencia de Búsqueda Híbrida ($p_{95}$)** | $\approx 14\text{ ms}$ | $\approx 9\text{ ms}$ | **$\approx 4\text{ ms}$ (L1 Hit) / $\approx 16\text{ ms}$ (Con Reranker GPU)** |

## 5.3 Los Tres Estados Operativos Dinámicos en Tiempo de Ejecución (Perfil `Ultra`)

Dentro del perfil `Ultra`, el `PowerGovernor` de Romero gestiona inteligentemente los $6\text{ GB}$ de VRAM de tu RTX 4050 alternando entre tres estados automáticos:

```mermaid
stateDiagram-v2
    [*] --> Guardia : Inicio en Perfil Ultra
    Guardia --> Rafaga : Petición entrante de Gandalf Wizard / Agente
    Rafaga --> Guardia : Cola en tiempo real vacía por > 15s
    Guardia --> SuenoREM : Sin tráfico de agentes por > 120s y GPU libre (< 30% uso externo)
    SuenoREM --> Rafaga : Petición entrante de Agente (Preempción inmediata < 50ms)
    SuenoREM --> Guardia : Lote de consolidación completado
```

1. **Estado de Guardia (*Standby Sentinel*):**
   * Mantiene cargados en la VRAM de la RTX 4050 únicamente los tres modelos ligeros de respuesta inmediata: `BGE-M3` ($1.1\text{ GB}$), `bge-reranker-v2-m3` ($650\text{ MB}$) y `DeBERTa-v3-NLI` ($550\text{ MB}$).
   * **Total VRAM ocupada:** $2.3\text{ GB}$ de $6.0\text{ GB}$ (deja $3.7\text{ GB}$ libres permanentemente).
2. **Estado de Ráfaga (*High-Throughput Burst*):**
   * Cuando **Gandalf Wizard** lanza múltiples agentes concurrentes, Romero prioriza el ancho de banda de PCIe y Tensor Cores para resolver búsquedas híbridas, propagación en Caché L1, reranking en lotes (*dynamic batching*) y validación NLI de escrituras con latencia $p_{95} < 18\text{ ms}$.
3. **Estado de Sueño REM (*Sleep-Time Compute Consolidation*):**
   * Cuando no hay solicitudes de agentes durante $120\text{ segundos}$ y el monitor de recursos detecta que la GPU no está saturada por otro proceso, el worker carga temporalmente el modelo generativo local cuantizado (`Qwen2.5-3B-Instruct-Q4_K_M` o `Llama-3.2-3B-Instruct-Q4_K_M`, $\sim 2.2\text{ GB}$ de VRAM).
   * Agrupa recuerdos episódicos fragmentados, sintetiza reglas semánticas y procedimientos (*runbooks*), poda duplicados y, al terminar (o si entra una petición urgente de un agente en cualquier milisegundo), **descarga el SLM de la VRAM inmediatamente**, volviendo a $2.3\text{ GB}$.

## 5.4 Desglose Minucioso de Recursos por Componente en Modo `Ultra`

| Componente del Sistema ROMERO | Tecnología / Modelo | RAM Sistema | VRAM (RTX 4050) | Uso CPU (i5-12400H) | Disco Fijo | Latencia Operativa |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| **Binario ROMERO + Caché L1 de Grafo** | Go 1.23+ (`CGO_ENABLED=0`) | $350\text{ MB}$ | $0\text{ MB}$ | $1\% - 3\%$ | $16\text{ MB}$ | $0.2\text{ ms} - 0.6\text{ ms}$ |
| **Motor Transaccional e Índices** | PostgreSQL 17 + `pgvector` + `pg_trgm` | $2500\text{ MB}$ | $0\text{ MB}$ | $5\% - 12\%$ | $1.2\text{ GB} / 100\text{k mems}$ | $2\text{ ms} - 5\text{ ms}$ |
| **Motor Vectorial Denso + Disperso** | `BAAI/bge-m3` (FP16) | $600\text{ MB}$ | $1100\text{ MB}$ | $2\% - 4\%$ | $1.1\text{ GB}$ | $4\text{ ms} - 6\text{ ms}$ |
| **Motor Reranker Cross-Encoder** | `BAAI/bge-reranker-v2-m3` (FP16) | $400\text{ MB}$ | $650\text{ MB}$ | $2\% - 5\%$ | $600\text{ MB}$ | $6\text{ ms} - 9\text{ ms}$ (Top-$30$) |
| **Motor Detector de Contradicciones NLI** | `mDeBERTa-v3-base-mnli-xnli` (FP16) | $350\text{ MB}$ | $550\text{ MB}$ | $2\% - 4\%$ | $550\text{ MB}$ | $4\text{ ms} - 6\text{ ms}$ |
| **Motor Consolidador "Sueño REM"** | `Qwen2.5-3B-Instruct` (`Q4_K_M`, bajo demanda) | $800\text{ MB}$ | $2200\text{ MB}$ *(Solo en REM)* | $10\% - 18\%$ | $1.9\text{ GB}$ | $\sim 48\text{ tokens/s}$ |
| **TOTAL EN GUARDIA / RÁFAGA DIURNA** | **Operación en Tiempo Real** | **$4.2\text{ GB}$ ($17.5\%$)** | **$2.3\text{ GB}$ ($38.3\%$)** | **$< 15\%$** | **$\approx 3.5\text{ GB}$** | **$p_{95} \approx 16\text{ ms}$** |
| **TOTAL EN PICO DE SUEÑO REM** | **Consolidación en Inactividad** | **$5.0\text{ GB}$ ($20.8\%$)** | **$4.5\text{ GB}$ ($75.0\%$)** | **$< 25\%$** | **$\approx 3.5\text{ GB}$** | **Trabajo en background** |

---

# 6. Stack Tecnológico Definitivo y Estrategia Multiplataforma

## 6.1 Mapeo Tecnológico por Capas

| Capa Arquitectónica | Componente / Tecnología | Justificación de Ingeniería |
| :--- | :--- | :--- |
| **Lenguaje del Núcleo** | **Go (Golang) 1.23+** con `CGO_ENABLED=0` | Compilación estática multiplataforma instantánea, concurrencia masiva con goroutines, recolector de basura sub-milisegundo y seguridad de tipos. |
| **Interfaz REST API** | `net/http` nativo + **Huma v2** | Validación estricta de esquemas y generación automática de documentación **OpenAPI 3.1** y JSON Schema con cero costo de reflexión en caliente. |
| **Interfaz MCP Server** | **mcp-go** (`stdio` y `Streamable HTTP / SSE`) | Permite conectar agentes locales por tubería estándar (`stdio`) y a **Gandalf Wizard** por red con baja latencia. |
| **Interfaz CLI** | **Cobra** | Integra en el mismo ejecutable `romero serve`, `romero mcp`, `romero migrate`, `romero consolidate`, `romero eval` y `romero doctor`. |
| **Caché Cognitiva L1** | Estructuras concurrentes en Go (`sync.RWMutex` + Arena Indexada) | Almacena en la RAM del proceso los nodos y aristas activas de los proyectos en curso para ejecutar *Spreading Activation* en microsegundos. |
| **Driver y Acceso SQL** | **pgx v5** (`pgxpool`) + **sqlc** | Protocolo binario nativo de PostgreSQL y generación de código Go estático a partir de SQL puro sin ORMs pesados. |
| **Persistencia Principal** | **PostgreSQL 17 + `pgvector` 0.7+ + `pg_trgm`** | Combina integridad ACID, vectores `halfvec` (Float16), vectores dispersos (`sparsevec`), búsqueda Full-Text y trigramas de código en un solo motor. |
| **Motores Neuronales Locales** | **HuggingFace TEI / ONNX Runtime Server + Ollama** (vía HTTP2/gRPC local) | Ejecuta los modelos `BGE-M3`, `Reranker` y `NLI` con aceleración nativa por hardware sin contaminar el binario Go con librerías C++. |
| **Cola de Trabajos** | **Cola Transaccional PostgreSQL (`FOR UPDATE SKIP LOCKED`)** | Despacha trabajos de vectorización, validación NLI y consolidación REM con garantías transaccionales sin depender de Redis ni RabbitMQ. |
| **Observabilidad** | `log/slog` + **OpenTelemetry Go SDK** | Trazas distribuidas entre Gandalf Wizard y Romero, métricas Prometheus y logs JSON estructurados. |

## 6.2 Portabilidad Absoluta en Linux, macOS y Windows

Para garantizar que el mismo código fuente funcione a máxima velocidad en **Linux, macOS y Windows**:

1. **Binario Principal Libre de CGo (`CGO_ENABLED=0`):** El ejecutable de Romero no enlaza dinámicamente ninguna librería `.dll`, `.so` ni `.dylib` de C/C++. Se compila para los tres sistemas operativos desde cualquier máquina en segundos.
2. **Aceleración de GPU Agnóstica por Interfaz de Red Local (ADR-009):**
   * En **Windows 11 y Linux con tu RTX 4050**: Los contenedores o demonios locales de inferencia usan **NVIDIA CUDA y TensorRT** automáticamente.
   * En **macOS (Apple Silicon M1/M2/M3/M4)**: Usan **Apple Metal y Accelerate** de forma nativa.
   * En **equipos sin GPU dedicada**: Caen automáticamente a **CPU con instrucciones vectoriales AVX2 / VNNI / ARM NEON**.

---

# 7. Arquitectura Lógica y Los Cuatro Motores Cognitivos de Alta Potencia

## 7.1 Diagrama de Arquitectura Integral (ROMERO `Ultra` + GANDALF WIZARD)

```mermaid
flowchart TB
    subgraph Harness["GANDALF WIZARD (Meta Harness)"]
        GW_RT[Task Runtime & Planner]
        GW_CB[Context Builder Client]
        GW_SUP[Execution Supervisor & Evaluator]
        AG[Pool de Agentes Especializados]
        GW_RT --> GW_CB --> AG --> GW_SUP
    end

    subgraph Romero["ROMERO (Binario Único Multiplataforma en Go)"]
        direction TB
        IFACE[Interfaces: REST API OpenAPI 3.1 / MCP Server / CLI]
        SEC[Zero-Trust Guard: AuthN, AuthZ, Scope Policy & Anti-Injection]
        GOV[PowerGovernor: Perfiles Lite/Balanced/Ultra & Estados Guardia/Ráfaga/REM]
        
        subgraph Core["Application & Domain Core"]
            CAP[Capture & Supersede Service]
            RET[Quad-Stage Retrieval & Submodular Context Service]
            FBK[Bayesian Feedback & Utility Calibration]
            REM[REM Consolidation & Conflict Resolution Service]
        end

        subgraph L1["Caché Cognitiva L1 en RAM (Go In-Memory)"]
            HOT_GRAPH[Grafo Bitemporal Activo + Spreading Activation Engine]
        end

        WRK[Worker Pool Transaccional: Prioridad Real-Time vs. Idle REM]

        IFACE --> SEC --> Core
        GOV -.-> Core
        GOV -.-> WRK
        RET <--> HOT_GRAPH
        CAP --> HOT_GRAPH
        Core --> WRK
    end

    subgraph Storage["Persistencia Unificada (Docker / Local)"]
        PG[(PostgreSQL 17\npgvector halfvec/sparsevec + pg_trgm + SKIP LOCKED Queue)]
    end

    subgraph Neural["Motores Neuronales Locales (GPU CUDA / Metal / CPU AVX2)"]
        M_EMB[Motor 1A: BGE-M3\nDense + Sparse Embeddings]
        M_RERANK[Motor 1B: Cross-Encoder\nbge-reranker-v2-m3]
        M_NLI[Motor 3: Detector NLI\nmDeBERTa-v3 Contradicciones]
        M_SLM[Motor 4: Consolidador REM\nSLM 3B Cuantizado Q4_K_M]
    end

    GW_CB -->|REST / MCP| IFACE
    GW_SUP -->|Feedback & Episodic Save| IFACE
    AG -->|MCP Tools Autorizadas| IFACE

    Core -->|pgxpool Binario| PG
    WRK -->|SKIP LOCKED| PG

    RET -->|1. Query Embed| M_EMB
    RET -->|2. Top-30 Rerank| M_RERANK
    CAP -->|Verificación NLI| M_NLI
    WRK -->|Batch Embed / NLI| M_EMB
    WRK -->|Batch NLI| M_NLI
    REM -->|Destilación en Sueño REM| M_SLM
```

## 7.2 Motor Cognitivo 1: Pipeline de Recuperación Cuádruple + Cross-Encoder Reranker

Cuando un agente o **Gandalf Wizard** solicita buscar recuerdos o armar contexto, Romero ejecuta un embudo de alta precisión en cuatro etapas:

1. **Etapa 1 — Filtrado Duro de Seguridad y Vigencia Bitemporal:** Descarta inmediatamente cualquier recuerdo fuera del `tenant_id`, `project_id` o `scope_type` autorizado, o cuya ventana `[valid_from, valid_until)` no esté vigente.
2. **Etapa 2 — Recuperación Candidata Tri-Modal en Paralelo (Top-$100$):**
   * *Rama Exacta de Código (`pg_trgm`):* Captura nombres exactos de funciones, variables, rutas de archivos y códigos de error.
   * *Rama Léxica y Dispersa (`FTS` + `SPLADE`):* Captura términos técnicos ponderados por especificidad documental.
   * *Rama Semántica Densa (`halfvec(1024)` HNSW con `BGE-M3`):* Captura similitud conceptual profunda aunque no compartan ninguna palabra.
3. **Etapa 3 — Activación Propagada por el Grafo Causal (Top-$100 \rightarrow$ Top-$30$ enriquecido):** Los nodos semilla de la Etapa 2 inyectan energía en la **Caché L1 de Grafo**, rescatando recuerdos fuertemente conectados por relaciones `depends_on`, `caused_by` o `supports` que una búsqueda vectorial simple habría pasado por alto.
4. **Etapa 4 — Reordenamiento Neuronal con Cross-Encoder (`bge-reranker-v2-m3`, Top-$30 \rightarrow$ Top-$K$):** En lugar de comparar vectores comprimidos por separado (Bi-Encoder), el Cross-Encoder procesa conjuntamente el par $(\text{Consulta}, \text{Contenido del Recuerdo})$ con atención cruzada completa en los Tensor Cores de la RTX 4050 en $\sim 7\text{ ms}$, eliminando falsos positivos semánticos.

## 7.3 Motor Cognitivo 2: Grafo Bitemporal en Caché L1 y Activación Propagada (*HippoRAG Local*)

Inspirado en la indexación hipocampal neurobiológica (**HippoRAG**), Romero evita el costo y la lentitud de Neo4j manteniendo los nodos activos y las aristas de `memory_relations` de los proyectos en curso replicados en una estructura de grafo en memoria dentro del proceso Go (`L1 Cognitive Graph Cache`):

* **Sincronización Write-Through + LISTEN/NOTIFY:** Cada escritura, supersesión o nueva relación en PostgreSQL actualiza síncronamente la Caché L1 en $< 50\text{ microsegundos}$.
* **Recorrido Multi-Salto Respetando los 3 Relojes:** Al propagar activación desde un recuerdo $u$ hacia un recuerdo $v$ a través de una arista $e = (u, v)$, el motor verifica en memoria que tanto el nodo destino como la arista estén activos en el instante de consulta $t_{\text{query}}$.
* **Descubrimiento de Contexto Oculto:** Si un agente pregunta *"¿Por qué falla el despliegue de pagos en staging?"*, la búsqueda léxica/vectorial encuentra el recuerdo episódico del fallo de pagos, y la **Activación Propagada** trae automáticamente el recuerdo semántico sobre el certificado TLS interno del cual depende el servicio de pagos (`depends_on`), entregando la causa raíz completa.

## 7.4 Motor Cognitivo 3: Detector NLI de Contradicciones y Calibración Bayesiana

En lugar de usar reglas ingenuas o esperar a que un humano note que hay recuerdos contradictorios:

* Al capturar un recuerdo nuevo $M_{\text{new}}$, Romero recupera de la Caché L1 / `pgvector` los Top-$5$ recuerdos activos del mismo proyecto con similitud coseno $\ge 0.82$.
* Pasa los pares $(M_{\text{existing}}, M_{\text{new}})$ por el clasificador local **`mDeBERTa-v3-base-mnli-xnli`**, que produce en $\sim 5\text{ ms}$ la distribución de probabilidad sobre tres clases lógicas:
  $$P(\text{Entailment}), \quad P(\text{Neutral}), \quad P(\text{Contradiction})$$
* **Acción Automática según Inferencia Lógica:**
  * Si $P(\text{Entailment}) \ge 0.85$: Crea automáticamente una arista `supports` en `memory_relations` e incrementa el parámetro bayesiano de evidencia $\alpha$ del recuerdo existente.
  * Si $P(\text{Contradiction}) \ge 0.80$ y no es una supersesión autorizada por `topic_key` con igual o mayor rango epistemológico: Registra automáticamente una arista `contradicts`, marca el recuerdo de menor autoridad como `conflicted` y genera una alerta auditable para **Gandalf Wizard**.

## 7.5 Motor Cognitivo 4: Consolidación "Sueño REM" y Empaquetado Submodular

Durante el estado **Sueño REM**, Romero ejecuta procesos de mantenimiento cognitivo profundo:

1. **Clustering Episódico Incremental:** Detecta grupos de $\ge 3$ recuerdos `episodic` dentro de un mismo proyecto con alta densidad semántica (ej. varios incidentes resueltos sobre migraciones fallidas o configuración de pruebas).
2. **Síntesis de Memoria Procedimental / Semántica:** El modelo local `Qwen2.5-3B-Instruct` destila esos episodios en un nuevo recuerdo `procedural` (un *runbook* paso a paso) o `semantic` (una regla arquitectónica consolidada), enlazando cada episodio original mediante aristas `derived_from` para mantener **procedencia auditable al $100\%$**.
3. **Empaquetado Submodular de Contexto:** Cuando Gandalf Wizard llama a `BuildContext`, Romero selecciona el subconjunto de recuerdos que maximiza una función objetivo submodular sujeta al presupuesto exacto de tokens, garantizando máxima cobertura informativa y cero redundancia.

---

# 8. Modelo Conceptual, Epistemológico y Bayesiano de Memoria (ROMERO Core)

## 8.1 Taxonomía Funcional (`memory_type`)

1. **Episódica (`episodic`):** Experiencias concretas acotadas en el tiempo (ejecuciones de tareas, errores encontrados y cómo se destrabaron, experimentos realizados). Responde: **¿Qué ocurrió, en qué contexto y con qué resultado?**
2. **Semántica (`semantic`):** Conocimiento declarativo estable sobre el proyecto, arquitectura, reglas de negocio, variables de entorno y restricciones verificadas. Responde: **¿Qué sabemos que es cierto y qué evidencia lo respalda?**
3. **Procedimental (`procedural`):** Secuencias de acción reproducibles, flujos de trabajo, *runbooks* y estrategias probadas empíricamente. Responde: **¿Cómo se ejecuta exitosamente esta tarea y bajo qué precondiciones?**
4. **Relacional (`relational`):** Reglas explícitas de dependencia, causalidad o impacto entre componentes y recuerdos del sistema. Responde: **¿Cómo afecta este elemento al resto del sistema?**

## 8.2 Máquina de Estados del Ciclo de Vida (`status`)

* `candidate`: Recuerdo propuesto aún no validado o retenido por sospecha de baja calidad/conflicto.
* `active`: Recuerdo vigente y elegible para recuperación ordinaria.
* `superseded`: Recuerdo reemplazado por una versión más reciente o precisa; su `valid_until` está cerrado pero permanece intacto para auditoría y consultas históricas (*Point-in-Time*).
* `conflicted`: Recuerdo activo que presenta una contradicción lógica detectada por el motor NLI ($P(\text{Contradiction}) \ge 0.80$) pendiente de resolución.
* `consolidated`: Recuerdo episódico cuya enseñanza ya fue destilada hacia un recuerdo semántico o procedimental durante el Sueño REM (permanece accesible por trazabilidad `derived_from`, pero reduce su peso en búsquedas generales para no saturar con episodios crudos).
* `archived`: Retirado de la recuperación activa.
* `rejected`: Descartado definitivamente.

## 8.3 Epistemología y Calibración Bayesiana ($\alpha, \beta$)

Todo recuerdo distingue su **naturaleza epistemológica** (`epistemic_status`) de su **confianza matemática** (`confidence`). En lugar de un número fijo arbitrario, Romero modela la confianza y utilidad de cada recuerdo mediante una **Distribución Beta** parametrizada por evidencias positivas ($\alpha > 0$) y evidencias negativas/fallos ($\beta > 0$):

| Estado Epistemológico (`epistemic_status`) | Descripción Rigurosa | Priors Bayesianos Iniciales $(\alpha_0, \beta_0)$ | Confianza Esperada Inicial $E[\theta] = \frac{\alpha_0}{\alpha_0 + \beta_0}$ | Multiplicador Epistémico ($W_{\text{ep}}$) |
| :--- | :--- | :--- | :--- | :--- |
| `verified` | Hecho comprobado por test automatizado, compilación exitosa verificada o confirmación humana directa. | $(\alpha_0 = 19.0, \beta_0 = 1.0)$ | $0.95$ | $1.20$ |
| `observed` | Observación empírica directa extraída de la salida de una herramienta, log o ejecución real. | $(\alpha_0 = 8.5, \beta_0 = 1.5)$ | $0.85$ | $1.00$ |
| `reported` | Información declarada por un usuario, documentación externa o agente sin verificación directa en ejecución. | $(\alpha_0 = 5.25, \beta_0 = 2.25)$ | $0.70$ | $0.90$ |
| `inferred` | Deducción lógica realizada por un agente o por el consolidador a partir de otros recuerdos. | $(\alpha_0 = 3.0, \beta_0 = 2.0)$ | $0.60$ | $0.75$ |
| `hypothesis` | Hipótesis exploratoria o teoría de trabajo pendiente de validación experimental. | $(\alpha_0 = 1.4, \beta_0 = 2.6)$ | $0.35$ | $0.50$ |

## 8.4 Los Tres Relojes Temporales (Modelo Tri-Temporal)

1. **Tiempo de Evento (`observed_at`):** Instante real en que ocurrió el hecho observado.
2. **Tiempo de Transacción (`created_at`):** Instante en que Romero registró el hecho en PostgreSQL.
3. **Ventana de Validez (`valid_from`, `valid_until`):** Intervalo $[t_{\text{from}}, t_{\text{until}})$ durante el cual el hecho es verdadero en el dominio.

---

# 9. Scopes, Identidad, Seguridad Multiagente y Gobernanza Zero-Trust

## 9.1 Cinco Niveles de Scope (`scope_type`)

1. `agent_private`: Exclusivo del agente propietario (`owner_agent_id`).
2. `task_shared`: Compartido únicamente entre los agentes asignados a la misma ejecución o tarea (`task_id` / `run_id`) coordinada por Gandalf Wizard.
3. `project_shared`: Compartido entre todos los agentes y principales autorizados de un `project_id`.
4. `org_shared`: Compartido transversalmente en todos los proyectos del mismo `tenant_id`.
5. `global_public`: Conocimiento universal de solo lectura para agentes estándar (escritura restringida a administradores).

## 9.2 Autenticación, Autorización y Defensa contra Prompt Injection

* **Identidad Criptográfica (`AuthContext`):** Ningún agente puede falsificar su `agent_id`, `project_id` ni `tenant_id`. Romero los deriva de la API Key (`SHA-256`) o del token firmado por **Gandalf Wizard**.
* **Escudo Anti-Inyección en Captura y Lectura:**
  * En escritura, se escanean secuencias de escape de prompt, caracteres invisibles Unicode Bidi y directivas imperativas de secuestro de sistema.
  * En lectura (`BuildContext` y MCP), todo recuerdo se serializa dentro de un contenedor XML estricto con atributos de procedencia y nivel epistemológico:
    ```xml
    <romero_memory id="01931658-a9b2-7c00-8123-456789abcdef" type="semantic" epistemic="verified" confidence="0.96" source="run:01931a" untrusted_content="true">
    El servicio de autenticación requiere AUTH_ISSUER=https://auth.staging.internal en el entorno de staging.
    </romero_memory>
    ```

---

# 10. Modelo Físico de Datos y Esquema SQL Completo (PostgreSQL 17 + pgvector)

El archivo `migrations/00001_romero_core_schema.sql` implementa el modelo completo de producción para **ROMERO**, soportando los tres perfiles de potencia (`Lite`, `Balanced`, `Ultra`), parámetros bayesianos ($\alpha, \beta$), vectores densos en media precisión (`halfvec(1024)`), vectores dispersos (`sparsevec`), auditoría NLI, grafo bitemporal y cola asíncrona con prioridades:

```sql
-- +goose Up
-- +goose StatementBegin

-- 1. EXTENSIONES REQUERIDAS
CREATE EXTENSION IF NOT EXISTS "pgcrypto";
CREATE EXTENSION IF NOT EXISTS "vector";      -- pgvector 0.7+ (soporte halfvec, sparsevec y HNSW)
CREATE EXTENSION IF NOT EXISTS "pg_trgm";     -- Índices de trigramas para código exacto y rutas
CREATE EXTENSION IF NOT EXISTS "btree_gist";  -- Restricciones temporales e índices compuestos

-- 2. TIPOS ENUMERADOS DEL DOMINIO COGNITIVO
CREATE TYPE memory_type_enum AS ENUM (
    'episodic',
    'semantic',
    'procedural',
    'relational'
);

CREATE TYPE memory_status_enum AS ENUM (
    'candidate',
    'active',
    'superseded',
    'conflicted',
    'consolidated',
    'archived',
    'rejected'
);

CREATE TYPE epistemic_status_enum AS ENUM (
    'observed',
    'reported',
    'inferred',
    'hypothesis',
    'verified'
);

CREATE TYPE scope_type_enum AS ENUM (
    'agent_private',
    'task_shared',
    'project_shared',
    'org_shared',
    'global_public'
);

CREATE TYPE source_type_enum AS ENUM (
    'agent_execution',
    'user_input',
    'tool_output',
    'document_ingest',
    'rem_consolidation',
    'human_review'
);

CREATE TYPE relation_type_enum AS ENUM (
    'supports',
    'contradicts',
    'supersedes',
    'caused_by',
    'depends_on',
    'derived_from',
    'related_to'
);

CREATE TYPE feedback_signal_enum AS ENUM (
    'retrieved',
    'used_in_context',
    'helpful_success',
    'ignored',
    'misleading_failure',
    'corrected_by_user',
    'confirmed_verified'
);

CREATE TYPE job_status_enum AS ENUM (
    'pending',
    'processing',
    'completed',
    'failed',
    'dead_letter'
);

-- 3. IDENTIDAD, TENANTS, PROYECTOS Y AGENTES
CREATE TABLE tenants (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    slug            VARCHAR(64) NOT NULL UNIQUE,
    name            VARCHAR(255) NOT NULL,
    is_active       BOOLEAN NOT NULL DEFAULT TRUE,
    settings        JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE projects (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    slug            VARCHAR(100) NOT NULL,
    name            VARCHAR(255) NOT NULL,
    repository_url  TEXT,
    description     TEXT,
    is_active       BOOLEAN NOT NULL DEFAULT TRUE,
    metadata        JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, slug)
);

CREATE INDEX idx_projects_tenant ON projects(tenant_id);

CREATE TABLE agents (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    external_key    VARCHAR(128) NOT NULL,
    name            VARCHAR(255) NOT NULL,
    role_type       VARCHAR(64) NOT NULL DEFAULT 'general',
    capabilities    TEXT[] NOT NULL DEFAULT '{}',
    is_active       BOOLEAN NOT NULL DEFAULT TRUE,
    metadata        JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, external_key)
);

CREATE INDEX idx_agents_tenant ON agents(tenant_id);

CREATE TABLE principals (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    agent_id            UUID REFERENCES agents(id) ON DELETE SET NULL,
    principal_type      VARCHAR(32) NOT NULL CHECK (principal_type IN ('user', 'service', 'orchestrator', 'agent')),
    name                VARCHAR(255) NOT NULL,
    api_key_prefix      VARCHAR(24) NOT NULL,
    api_key_sha256      CHAR(64) NOT NULL UNIQUE,
    allowed_projects    UUID[] DEFAULT NULL,
    allowed_scopes      TEXT[] NOT NULL DEFAULT ARRAY['read', 'write'],
    is_active           BOOLEAN NOT NULL DEFAULT TRUE,
    expires_at          TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_principals_lookup ON principals(api_key_prefix, is_active);

-- 4. PROCEDENCIA INMUTABLE (MEMORY_SOURCES)
CREATE TABLE memory_sources (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    project_id          UUID REFERENCES projects(id) ON DELETE CASCADE,
    source_type         source_type_enum NOT NULL,
    external_source_id  VARCHAR(255) NOT NULL,
    actor_principal_id  UUID REFERENCES principals(id) ON DELETE SET NULL,
    actor_agent_id      UUID REFERENCES agents(id) ON DELETE SET NULL,
    uri                 TEXT,
    raw_excerpt_hash    CHAR(64),
    metadata            JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_memory_sources_lookup ON memory_sources(tenant_id, source_type, external_source_id);

-- 5. TABLA CENTRAL DE RECUERDOS (MEMORIES)
CREATE TABLE memories (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    project_id          UUID REFERENCES projects(id) ON DELETE CASCADE,
    owner_agent_id      UUID REFERENCES agents(id) ON DELETE SET NULL,
    task_id             VARCHAR(128),

    -- Clasificación Cognitiva y Visibilidad
    memory_type         memory_type_enum NOT NULL,
    scope_type          scope_type_enum NOT NULL DEFAULT 'project_shared',
    status              memory_status_enum NOT NULL DEFAULT 'active',
    topic_key           VARCHAR(255),

    -- Contenido (Progressive Disclosure: summary + content)
    summary             VARCHAR(600) NOT NULL,
    content             TEXT NOT NULL,
    content_sha256      CHAR(64) NOT NULL,
    token_count         INTEGER NOT NULL DEFAULT 50,
    tags                TEXT[] NOT NULL DEFAULT '{}',
    entities            TEXT[] NOT NULL DEFAULT '{}', -- Símbolos de código, servicios o componentes extraídos

    -- Epistemología y Calibración Bayesiana Beta(alpha, beta)
    epistemic_status    epistemic_status_enum NOT NULL DEFAULT 'observed',
    alpha_score         REAL NOT NULL DEFAULT 8.50 CHECK (alpha_score > 0.0),
    beta_score          REAL NOT NULL DEFAULT 1.50 CHECK (beta_score > 0.0),
    confidence          REAL GENERATED ALWAYS AS (alpha_score / (alpha_score + beta_score)) STORED,
    importance          REAL NOT NULL DEFAULT 0.50 CHECK (importance >= 0.0 AND importance <= 1.0),
    utility_score       REAL NOT NULL DEFAULT 0.00 CHECK (utility_score >= -1.0 AND utility_score <= 1.0),
    access_count        INTEGER NOT NULL DEFAULT 0,
    version             INTEGER NOT NULL DEFAULT 1,
    nli_checked         BOOLEAN NOT NULL DEFAULT FALSE,

    -- Procedencia e Idempotencia
    primary_source_id   UUID NOT NULL REFERENCES memory_sources(id) ON DELETE RESTRICT,
    idempotency_key     VARCHAR(128),

    -- Los Tres Relojes Temporales (Modelo Tri-Temporal)
    observed_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    valid_from          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    valid_until         TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_accessed_at    TIMESTAMPTZ,

    -- Metadatos y Documento Full-Text Ponderado (Español + Código Simple)
    metadata            JSONB NOT NULL DEFAULT '{}'::jsonb,
    fts_doc             TSVECTOR GENERATED ALWAYS AS (
        setweight(to_tsvector('simple', coalesce(topic_key, '')), 'A') ||
        setweight(to_tsvector('spanish', coalesce(summary, '')), 'A') ||
        setweight(to_tsvector('simple', coalesce(summary, '')), 'A') ||
        setweight(to_tsvector('spanish', coalesce(content, '')), 'B')
    ) STORED,

    CONSTRAINT chk_valid_temporal_window CHECK (valid_until IS NULL OR valid_until >= valid_from)
);

CREATE UNIQUE INDEX idx_memories_idempotency 
    ON memories(tenant_id, idempotency_key) 
    WHERE idempotency_key IS NOT NULL;

CREATE UNIQUE INDEX idx_memories_active_topic_key 
    ON memories(tenant_id, COALESCE(project_id, '00000000-0000-0000-0000-000000000000'::uuid), scope_type, topic_key) 
    WHERE status = 'active' AND topic_key IS NOT NULL;

CREATE INDEX idx_memories_scope_lookup 
    ON memories(tenant_id, project_id, scope_type, status, memory_type);

CREATE INDEX idx_memories_bitemporal 
    ON memories(tenant_id, status, valid_from, valid_until, observed_at DESC);

CREATE INDEX idx_memories_rem_candidates 
    ON memories(tenant_id, project_id, memory_type, status, created_at) 
    WHERE memory_type = 'episodic' AND status = 'active';

CREATE INDEX idx_memories_fts ON memories USING GIN (fts_doc);
CREATE INDEX idx_memories_trgm_summary ON memories USING GIN (summary gin_trgm_ops);
CREATE INDEX idx_memories_trgm_topic ON memories USING GIN (topic_key gin_trgm_ops) WHERE topic_key IS NOT NULL;
CREATE INDEX idx_memories_tags ON memories USING GIN (tags);
CREATE INDEX idx_memories_entities ON memories USING GIN (entities);

-- 6. HISTORIAL DE VERSIONES INMUTABLE
CREATE TABLE memory_versions (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    memory_id           UUID NOT NULL REFERENCES memories(id) ON DELETE CASCADE,
    version             INTEGER NOT NULL,
    summary             VARCHAR(600) NOT NULL,
    content             TEXT NOT NULL,
    epistemic_status    epistemic_status_enum NOT NULL,
    alpha_score         REAL NOT NULL,
    beta_score          REAL NOT NULL,
    status              memory_status_enum NOT NULL,
    changed_by_source   UUID REFERENCES memory_sources(id) ON DELETE SET NULL,
    change_reason       TEXT,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (memory_id, version)
);

CREATE INDEX idx_memory_versions_lookup ON memory_versions(memory_id, version DESC);

-- 7. EMBEDDINGS MULTIMODALES (DENSOS EN HALFVEC FP16 + DISPERSOS SPLADE OPCIONALES)
CREATE TABLE memory_embeddings (
    memory_id           UUID PRIMARY KEY REFERENCES memories(id) ON DELETE CASCADE,
    tenant_id           UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    project_id          UUID REFERENCES projects(id) ON DELETE CASCADE,
    model_name          VARCHAR(128) NOT NULL DEFAULT 'BAAI/bge-m3',
    model_version       VARCHAR(64) NOT NULL DEFAULT '1.0',
    dimensions          INTEGER NOT NULL DEFAULT 1024,
    dense_vec           halfvec(1024) NOT NULL,
    sparse_vec          sparsevec(250002), -- Pesos léxicos aprendidos de BGE-M3 (opcional según perfil)
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_memory_embeddings_hnsw_dense 
    ON memory_embeddings 
    USING hnsw (dense_vec halfvec_cosine_ops) 
    WITH (m = 16, ef_construction = 64);

CREATE INDEX idx_memory_embeddings_tenant_proj ON memory_embeddings(tenant_id, project_id);

-- 8. GRAFO BITEMPORAL DE RELACIONES CAUSALES Y LÓGICAS (MEMORY_RELATIONS)
CREATE TABLE memory_relations (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    project_id          UUID REFERENCES projects(id) ON DELETE CASCADE,
    from_memory_id      UUID NOT NULL REFERENCES memories(id) ON DELETE CASCADE,
    to_memory_id        UUID NOT NULL REFERENCES memories(id) ON DELETE CASCADE,
    relation_type       relation_type_enum NOT NULL,
    weight              REAL NOT NULL DEFAULT 1.0 CHECK (weight >= 0.0 AND weight <= 1.0),
    nli_entailment      REAL,
    nli_contradiction   REAL,
    source_id           UUID REFERENCES memory_sources(id) ON DELETE SET NULL,
    valid_from          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    valid_until         TIMESTAMPTZ,
    is_active           BOOLEAN NOT NULL DEFAULT TRUE,
    notes               TEXT,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_no_self_loop CHECK (from_memory_id <> to_memory_id),
    UNIQUE (tenant_id, from_memory_id, to_memory_id, relation_type)
);

CREATE INDEX idx_memory_relations_forward 
    ON memory_relations(tenant_id, project_id, from_memory_id, is_active) 
    INCLUDE (to_memory_id, relation_type, weight);

CREATE INDEX idx_memory_relations_backward 
    ON memory_relations(tenant_id, project_id, to_memory_id, is_active) 
    INCLUDE (from_memory_id, relation_type, weight);

-- 9. SEÑALES DE FEEDBACK Y RECALIBRACIÓN BAYESIANA
CREATE TABLE memory_feedback (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    memory_id           UUID NOT NULL REFERENCES memories(id) ON DELETE CASCADE,
    agent_id            UUID REFERENCES agents(id) ON DELETE SET NULL,
    task_id             VARCHAR(128),
    signal              feedback_signal_enum NOT NULL,
    alpha_delta         REAL NOT NULL DEFAULT 0.0,
    beta_delta          REAL NOT NULL DEFAULT 0.0,
    utility_delta       REAL NOT NULL DEFAULT 0.0,
    comment             TEXT,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_memory_feedback_lookup ON memory_feedback(memory_id, created_at DESC);

-- 10. COLA ASÍNCRONA CON PRIORIDADES (REAL-TIME vs. SUEÑO REM)
CREATE TABLE memory_jobs (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    project_id          UUID REFERENCES projects(id) ON DELETE CASCADE,
    memory_id           UUID REFERENCES memories(id) ON DELETE CASCADE,
    job_type            VARCHAR(64) NOT NULL, -- 'embed_and_nli', 'rem_consolidate_cluster', 'recalibrate_graph'
    priority            SMALLINT NOT NULL DEFAULT 10, -- 100 = Real-Time, 50 = Normal, 10 = Sueño REM
    status              job_status_enum NOT NULL DEFAULT 'pending',
    payload             JSONB NOT NULL DEFAULT '{}'::jsonb,
    attempts            INTEGER NOT NULL DEFAULT 0,
    max_attempts        INTEGER NOT NULL DEFAULT 5,
    last_error          TEXT,
    scheduled_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    locked_at           TIMESTAMPTZ,
    completed_at        TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_memory_jobs_dequeue 
    ON memory_jobs(status, priority DESC, scheduled_at ASC) 
    WHERE status = 'pending';

-- 11. REGISTRO INMUTABLE DE AUDITORÍA Y EJECUCIONES DE CONSOLIDACIÓN
CREATE TABLE audit_events (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    principal_id        UUID REFERENCES principals(id) ON DELETE SET NULL,
    agent_id            UUID REFERENCES agents(id) ON DELETE SET NULL,
    operation           VARCHAR(64) NOT NULL,
    resource_type       VARCHAR(64) NOT NULL,
    resource_id         UUID,
    scope_used          VARCHAR(64),
    correlation_id      VARCHAR(128),
    outcome             VARCHAR(32) NOT NULL,
    details             JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_audit_events_tenant_time ON audit_events(tenant_id, created_at DESC);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS audit_events;
DROP TABLE IF EXISTS memory_jobs;
DROP TABLE IF EXISTS memory_feedback;
DROP TABLE IF EXISTS memory_relations;
DROP TABLE IF EXISTS memory_embeddings;
DROP TABLE IF EXISTS memory_versions;
DROP TABLE IF EXISTS memories;
DROP TABLE IF EXISTS memory_sources;
DROP TABLE IF EXISTS principals;
DROP TABLE IF EXISTS agents;
DROP TABLE IF EXISTS projects;
DROP TABLE IF EXISTS tenants;
DROP TYPE IF EXISTS job_status_enum;
DROP TYPE IF EXISTS feedback_signal_enum;
DROP TYPE IF EXISTS relation_type_enum;
DROP TYPE IF EXISTS source_type_enum;
DROP TYPE IF EXISTS scope_type_enum;
DROP TYPE IF EXISTS epistemic_status_enum;
DROP TYPE IF EXISTS memory_status_enum;
DROP TYPE IF EXISTS memory_type_enum;
-- +goose StatementEnd
```

---

# 11. Captura, Inferencia Lógica (NLI), Idempotencia, Topic Keys y Supersesión

## 11.1 Flujo Cognitivo de Captura (`CaptureMemory`)

```mermaid
flowchart TD
    A[Solicitud de Escritura + Idempotency Key] --> B[Autenticar Principal y Resolver AuthContext]
    B --> C{¿Autorizado para el Scope y Proyecto?}
    C -->|No| D[Auditar 'denied' y devolver 403 Forbidden]
    C -->|Sí| E[Sanitizar Prompt Injection y Extraer Símbolos de Código]
    E --> F{¿Existe idempotency_key en el Tenant?}
    F -->|Sí| G[Devolver Recuerdo Existente 200 OK]
    F -->|No| H[Asignar Priors Bayesianos alpha_0, beta_0 según epistemic_status]
    H --> I{¿Tiene topic_key existente activa en este Scope?}
    I -->|Sí| J{¿Rango Epistémico Nuevo >= Anterior?}
    J -->|No| K[Proteger Recuerdo Anterior + Guardar Nuevo como 'conflicted']
    J -->|Sí| L[Ejecutar Supersesión Atómica + Cerrar valid_until Anterior]
    I -->|No| M[Insertar en memories + memory_versions v1 + Actualizar Caché L1]
    K --> N[Encolar Job Prioridad 100 'embed_and_nli']
    L --> N
    M --> N
    N --> O[Worker Genera Embedding BGE-M3 + Evalúa NLI DeBERTa contra Vecinos Top-5]
    O --> P{¿Resultado NLI?}
    P -->|Entailment >= 0.85| Q[Crear relación 'supports' y sumar +0.5 a alpha_score]
    P -->|Contradiction >= 0.80| R[Crear relación 'contradicts' y marcar estado 'conflicted']
    P -->|Neutral| S[Marcar nli_checked = true y finalizar]
```

## 11.2 Protección Jerárquica en Supersesión por `topic_key`

Sea $\text{Rank}(\text{epistemic\_status})$ la función de orden jerárquico:
$$\text{Rank}(\text{verified}) = 5 > \text{Rank}(\text{observed}) = 4 > \text{Rank}(\text{reported}) = 3 > \text{Rank}(\text{inferred}) = 2 > \text{Rank}(\text{hypothesis}) = 1$$

Cuando entra un recuerdo $M_{\text{new}}$ con la misma `topic_key` que un recuerdo activo $M_{\text{old}}$:
* Si $\text{Rank}(M_{\text{new}}) \ge \text{Rank}(M_{\text{old}})$ (o si $\text{Rank}(M_{\text{new}}) = 4$ y el actor tiene permiso de supersesión explícito): $M_{\text{new}}$ reemplaza a $M_{\text{old}}$ cerrando su `valid_until` y creando la arista `supersedes`.
* Si $\text{Rank}(M_{\text{new}}) < \text{Rank}(M_{\text{old}})$: **Romero impide que una conjetura degrade conocimiento sólido.** $M_{\text{old}}$ continúa siendo el único recuerdo `active` para esa `topic_key`, mientras que $M_{\text{new}}$ se persiste con estado `conflicted` y una arista `contradicts` para revisión.

---

# 12. Modelo Matemático Completo: Ranking, Activación Propagada, Reranking y Empaquetado Submodular

## 12.1 Etapa 1: Fusión Híbrida Tri-Modal ($\text{RRF}$)

Para una consulta $Q$ en el instante $t_{\text{now}}$, cada recuerdo autorizado $m$ recibe tres posiciones ordinales iniciales: $\text{rank}_{\text{trgm}}(m)$ (símbolos exactos de código), $\text{rank}_{\text{fts}}(m)$ (léxico/disperso) y $\text{rank}_{\text{vec}}(m)$ (coseno sobre `halfvec(1024)`). Con constante $k_{\text{rrf}} = 60$ y pesos $\omega_{\text{trgm}} = 0.25$, $\omega_{\text{fts}} = 0.30$, $\omega_{\text{vec}} = 0.45$:

$$S_{\text{rrf}}(m, Q) = \frac{\omega_{\text{trgm}}}{k_{\text{rrf}} + \text{rank}_{\text{trgm}}(m)} + \frac{\omega_{\text{fts}}}{k_{\text{rrf}} + \text{rank}_{\text{fts}}(m)} + \frac{\omega_{\text{vec}}}{k_{\text{rrf}} + \text{rank}_{\text{vec}}(m)}$$

## 12.2 Etapa 2: Activación Propagada sobre el Grafo Bitemporal (*Spreading Activation*)

Sea $G_{t_{\text{now}}} = (V, E)$ el subgrafo activo en la **Caché Cognitiva L1** para el proyecto y scope autorizados. Definimos el vector de activación inicial $\vec{a}^{(0)}$ normalizando $S_{\text{rrf}}(m, Q)$ sobre los nodos semilla Top-$50$. Durante $H \in \{1, 2, 3\}$ saltos (con factor de amortiguamiento $\gamma = 0.35$), la energía se propaga a través de las aristas activas $e = (u, v) \in E$:

$$a_v^{(h+1)} = (1 - \gamma) \cdot a_v^{(0)} + \gamma \sum_{u \in \mathcal{N}_{\text{in}}(v)} a_u^{(h)} \cdot \frac{\psi(\text{type}(e)) \cdot w_{uv}}{\text{deg}_{\text{out}}(u)}$$

Donde $\psi(\text{type}(e))$ es el peso de conductancia semántica según el tipo de arista:
* $\psi(\text{depends\_on}) = 1.00$
* $\psi(\text{caused\_by}) = 0.95$
* $\psi(\text{supports}) = 0.85$
* $\psi(\text{derived\_from}) = 0.80$
* $\psi(\text{related\_to}) = 0.50$
* $\psi(\text{contradicts}) = 0.60$ *(se propaga con bandera de alerta de conflicto para que el agente vea ambas caras de una contradicción activa)*.

## 12.3 Etapa 3: Reordenamiento Neuronal con Cross-Encoder (Modo `Ultra`)

Los Top-$30$ nodos con mayor activación $a_m^{(H)}$ se evalúan en lote mediante el modelo Cross-Encoder `bge-reranker-v2-m3`, obteniendo el logit de relevancia $z_{\text{ce}}(Q, m)$, que se transforma mediante la función sigmoide $\sigma(z) = \frac{1}{1 + e^{-z}}$ y se combina con la activación del grafo normalizada $\hat{a}_m^{(H)} \in [0, 1]$:

$$S_{\text{sem}}(m, Q) = 0.70 \cdot \sigma\left(z_{\text{ce}}(Q, m)\right) + 0.30 \cdot \hat{a}_m^{(H)}$$

*(En perfil `Lite` donde el Cross-Encoder está desactivado, $S_{\text{sem}}(m, Q) = \hat{a}_m^{(H)}$).*

## 12.4 Etapa 4: Ponderación Epistemológica, Bayesiana y Temporal

### Confianza Bayesiana Esperada con Penalización de Incertidumbre

Dado el par $(\alpha_m, \beta_m)$ del recuerdo $m$, su esperanza bayesiana y su límite inferior de credibilidad conservador son:

$$\hat{\theta}_m = \frac{\alpha_m}{\alpha_m + \beta_m}, \qquad M_{\text{epist}}(m) = W_{\text{ep}}(\text{epistemic\_status}(m)) \times \left(0.45 + 0.55 \cdot \hat{\theta}_m\right)$$

### Decaimiento Temporal Diferenciado por Tipo Cognitivo ($M_{\text{time}}$)

Con antigüedad $\Delta t_{\text{days}} = \frac{t_{\text{now}} - \text{observed\_at}(m)}{86400}$ y constantes de decaimiento $\lambda_{\text{episodic}} = 0.015$, $\lambda_{\text{semantic}} = 0.002$, $\lambda_{\text{procedural}} = 0.001$, $\lambda_{\text{relational}} = 0.002$:

$$M_{\text{time}}(m) = 0.70 + 0.30 \cdot \exp\left(-\lambda_{\text{type}(m)} \cdot \Delta t_{\text{days}}\right)$$

### Puntuación Final de Relevancia Gobernada ($\text{FinalScore}$)

$$\text{FinalScore}(m, Q) = S_{\text{sem}}(m, Q) \times M_{\text{epist}}(m) \times M_{\text{time}}(m) \times \left(0.85 + 0.30 \cdot \text{importance}(m)\right) \times \left(1.0 + 0.20 \cdot \text{utility\_score}(m)\right) \times P_{\text{state}}(m)$$

## 12.5 Etapa 5: Empaquetado Submodular de Contexto con Restricción de Presupuesto (`BuildContext`)

Dado un presupuesto máximo de tokens $B_{\text{max}}$ solicitado por **Gandalf Wizard** y un conjunto de recuerdos candidatos $U = \{m_1, m_2, \dots, m_n\}$ con costos en tokens $c(m_i)$ y vectores normalizados $\vec{v}_i$, queremos seleccionar el subconjunto $S \subseteq U$ con $\sum_{m \in S} c(m) \le B_{\text{max}}$ que maximice la función submodular monótona $F(S)$ que equilibra relevancia total y cobertura diversa (*Facility Location*):

$$F(S) = \sum_{m_i \in S} \text{FinalScore}(m_i, Q) + \eta \sum_{m_j \in U} \max_{m_i \in S} \left(\text{FinalScore}(m_i, Q) \cdot \max\left(0, \vec{v}_i \cdot \vec{v}_j\right)\right)$$

El `Context Builder` resuelve este problema en $< 1\text{ ms}$ mediante el algoritmo voraz de costo-beneficio marginal seleccionando en cada iteración el elemento $m^* \in U \setminus S$ que maximiza la ganancia marginal por unidad de token:

$$m^* = \arg\max_{m \in U \setminus S, \, c(S \cup \{m\}) \le B_{\text{max}}} \frac{F(S \cup \{m\}) - F(S)}{\left(c(m)\right)^{\rho}}$$

con exponente de eficiencia de longitud $\rho = 0.65$. Si para un recuerdo secundario el contenido completo $c_{\text{full}}(m)$ excede el presupuesto restante pero su resumen $c_{\text{summary}}(m)$ cabe, el algoritmo inserta su versión compacta (`summary` + `id`) aplicando **Divulgación Progresiva**.

## 12.6 Regla de Recalibración Bayesiana ante Feedback (`RecordFeedback`)

Cuando Gandalf Wizard reporta el resultado de una tarea donde se utilizó el recuerdo $m$:
* **`helpful_success`:** $\alpha_m \leftarrow \alpha_m + 1.0$, $\quad u_m \leftarrow \min(1.0, \, 0.85 u_m + 0.15)$.
* **`confirmed_verified`:** $\alpha_m \leftarrow \alpha_m + 3.0$, $\quad \text{epistemic\_status} \leftarrow \text{verified}$.
* **`ignored`:** $\beta_m \leftarrow \beta_m + 0.15$.
* **`misleading_failure`:** $\beta_m \leftarrow \beta_m + 2.5$, $\quad u_m \leftarrow \max(-1.0, \, 0.75 u_m - 0.25)$.
* **`corrected_by_user`:** $\beta_m \leftarrow \beta_m + 4.0$ (o disparo inmediato de supersesión con la corrección aportada).

---

# 13. Motor de Consolidación "Sueño REM" (Sleep-Time Compute) y Aprendizaje Verificable

## 13.1 Algoritmo del Ciclo de Sueño REM

El servicio `REMConsolidationService` se ejecuta exclusivamente cuando el `PowerGovernor` entra en estado **Sueño REM** (o cuando se invoca manualmente `romero consolidate`):

1. **Selección de Episodios No Consolidados:** Consulta en PostgreSQL los recuerdos con `memory_type = 'episodic'`, `status = 'active'` y `confidence >= 0.70` de un mismo `project_id`.
2. **Agrupamiento por Densidad Coseno (HDBSCAN / Greedy Graph Clustering):** Forma clústeres $C_k = \{e_1, e_2, \dots, e_m\}$ tales que $m \ge 3$ y la similitud coseno media por pares sobre `dense_vec` sea $\ge 0.80$.
3. **Destilación Neuro-Simbólica con SLM Local ($3\text{B}$):**
   * Carga `Qwen2.5-3B-Instruct-Q4_K_M` en la RTX 4050 con gramática JSON restringida (*Guided Decoding*).
   * Envía los episodios $C_k$ solicitando extraer:
     * Un `summary` canónico ($\le 600\text{ chars}$).
     * Un `content` estructurado en formato **Procedimiento Operacional (Precondiciones, Síntoma, Causa Raíz, Pasos de Solución y Verificación)**.
     * Una propuesta de `topic_key` y lista de entidades técnicas (`entities`).
4. **Validación NLI Cruzada del Recuerdo Destilado:**
   * Antes de persistir el nuevo recuerdo `procedural` $M_{\text{proc}}$, el motor NLI `DeBERTa-v3` verifica que $M_{\text{proc}}$ tenga relación de implicación lógica ($P(\text{Entailment}) \ge 0.80$) con los episodios fuente $e_i \in C_k$ y que no contradiga ningún recuerdo `verified` existente del proyecto.
5. **Persistencia con Trazabilidad Completa:**
   * Se crea $M_{\text{proc}}$ con `epistemic_status = 'inferred'` (o `'observed'` si todos los episodios eran `'verified'`/`'observed'`), fuente `source_type = 'rem_consolidation'` y relaciones `derived_from` apuntando a cada episodio $e_i \in C_k$.
   * Los episodios individuales $e_i$ pasan a `status = 'consolidated'`, conservando su historial intacto pero cediendo el protagonismo en búsquedas futuras al procedimiento destilado.

---

# 14. Contratos de Integración Completos (REST API OpenAPI 3.1 y Servidor MCP)

## 14.1 Tabla Completa de Endpoints REST API (`/api/v1`)

| Método | Ruta | Caso de Uso | Descripción |
| :--- | :--- | :--- | :--- |
| `POST` | `/api/v1/memories` | `CaptureMemory` | Crea un recuerdo, aplica idempotencia, supersesión automática por `topic_key` y validación NLI. |
| `GET` | `/api/v1/memories/{id}` | `GetMemory` | Obtiene el `content` completo, parámetros bayesianos $(\alpha, \beta)$, versiones y relaciones. |
| `POST` | `/api/v1/memories/search` | `SearchMemories` | Ejecuta el pipeline de recuperación cuádruple (Trigramas + FTS + Vectores + Grafo L1 + Reranker) devolviendo `summary`. |
| `POST` | `/api/v1/memories/context` | `BuildContext` | Ejecuta el empaquetador submodular para devolver el contexto óptimo dentro de `max_tokens`. |
| `PATCH` | `/api/v1/memories/{id}` | `UpdateMemory` | Actualiza un recuerdo generando una nueva versión inmutable en `memory_versions`. |
| `POST` | `/api/v1/memories/{id}/supersede` | `SupersedeMemory` | Reemplaza explícitamente la vigencia de un recuerdo por uno nuevo. |
| `POST` | `/api/v1/memories/{id}/archive` | `ArchiveMemory` | Archiva un recuerdo y lo retira de la Caché Cognitiva L1. |
| `POST` | `/api/v1/memories/feedback` | `RecordFeedback` | Aplica la actualización bayesiana Beta-Bernoulli y ajusta `utility_score`. |
| `GET` | `/api/v1/memories/{id}/subgraph` | `GetSubgraph` | Devuelve el vecindario causal a $1-3\text{ saltos}$ alrededor de un recuerdo desde la Caché L1. |
| `GET` | `/api/v1/conflicts` | `ListConflicts` | Lista contradicciones activas detectadas por el motor NLI (`DeBERTa-v3`). |
| `POST` | `/api/v1/conflicts/{id}/resolve` | `ReviewConflict` | Resuelve una contradicción declarando el recuerdo ganador y supercediendo el perdedor. |
| `POST` | `/api/v1/admin/consolidate` | `TriggerREM` | Dispara bajo demanda un ciclo de consolidación "Sueño REM" para un proyecto. |
| `GET` | `/api/v1/system/status` | `GetSystemStatus` | Devuelve el perfil activo (`lite`/`balanced`/`ultra`), estado (`guard`/`burst`/`rem_sleep`), métricas de Caché L1 y uso de RAM/VRAM. |

## 14.2 Las 5 Herramientas MCP Canónicas de ROMERO

Romero mantiene estrictamente **5 herramientas MCP** para no contaminar la ventana de contexto de los agentes:

1. `romero_memory_save`: Registra hallazgos, decisiones, errores resueltos o procedimientos (con soporte de `topic_key`, `epistemic_status` y `entities`).
2. `romero_memory_search`: Ejecuta la búsqueda cuádruple + activación propagada + Cross-Encoder devolviendo vistas compactas (`summary`, `confidence`, `epistemic_status`, `relations_count`).
3. `romero_memory_get`: Descarga el `content` completo, historial de versiones y aristas causales (`depends_on`, `caused_by`, `supersedes`, `contradicts`) de los IDs seleccionados.
4. `romero_memory_context`: Invoca el optimizador submodular y el grafo L1 para obtener en una sola llamada el paquete de contexto XML seguro ajustado a `max_tokens`.
5. `romero_memory_feedback`: Reporta el éxito o fallo empírico de un recuerdo para recalibrar su distribución bayesiana $(\alpha, \beta)$.

---

# 15. Diseño del Código Base en Go (Estructura, Interfaces, Caché L1 y Consultas SQL)

## 15.1 Árbol Completo del Proyecto (`romero`)

```text
romero/
├── cmd/
│   └── romero/
│       └── main.go                        # Entrypoint único: CLI, REST, MCP, Workers y PowerGovernor
├── internal/
│   ├── domain/                            # Núcleo puro en Go sin dependencias externas
│   │   ├── memory/
│   │   │   ├── entity.go                  # Memory, Version, 3 relojes, estados y validaciones
│   │   │   ├── bayesian.go                # Distribución Beta(alpha, beta) y actualización de utilidad
│   │   │   ├── ranking.go                 # RRF, fusión Cross-Encoder y decaimiento temporal
│   │   │   ├── submodular.go              # Optimizador voraz Facility Location para BuildContext
│   │   │   └── ports.go                   # Interfaces de repositorios y motores neuronales
│   │   ├── graph/
│   │   │   ├── spreading_activation.go    # Algoritmo de propagación multi-salto bitemporal
│   │   │   └── relation.go                # Tipos de aristas y pesos de conductancia
│   │   ├── provenance/
│   │   │   └── source.go                  # Entidad Source e invariantes de trazabilidad
│   │   ├── identity/
│   │   │   └── auth_context.go            # Principal, Tenant, Project, Agent y Scopes
│   │   └── governor/
│   │       └── power_state.go             # Máquina de estados Lite/Balanced/Ultra y Guardia/Ráfaga/REM
│   ├── application/                       # Casos de uso orquestados
│   │   ├── capture/
│   │   │   └── service.go                 # CaptureMemory, Supersede por topic_key y chequeo NLI
│   │   ├── retrieval/
│   │   │   └── service.go                 # Pipeline cuádruple: SQL + L1 Graph + Reranker
│   │   ├── context/
│   │   │   └── builder.go                 # BuildContext submodular + serializador XML anti-injection
│   │   ├── feedback/
│   │   │   └── service.go                 # RecordFeedback y ajuste bayesiano
│   │   └── consolidation/
│   │       └── rem_service.go             # Clustering HDBSCAN/Graph + Destilación SLM 3B + NLI check
│   ├── infrastructure/                    # Adaptadores concretos
│   │   ├── cachel1/
│   │   │   └── cognitive_graph.go         # Grafo caliente en memoria RAM con RWMutex y arenas
│   │   ├── persistence/
│   │   │   ├── postgres/                  # pgxpool, transacciones ACID y migraciones Goose
│   │   │   ├── sqlc/                      # Código generado estáticamente por sqlc
│   │   │   └── queries/                   # Archivos .sql optimizados
│   │   ├── neural/
│   │   │   ├── bge_m3_client.go           # Cliente HTTP2/gRPC para embeddings densos y dispersos
│   │   │   ├── reranker_client.go         # Cliente para Cross-Encoder bge-reranker-v2-m3
│   │   │   ├── nli_deberta_client.go      # Cliente para clasificador lógico mDeBERTa-v3
│   │   │   └── slm_rem_client.go          # Cliente con carga/descarga dinámica de VRAM para SLM 3B
│   │   ├── queue/
│   │   │   └── priority_worker.go         # Worker pool con prioridades y preempción de Sueño REM
│   │   └── observability/
│   │       └── telemetry.go               # slog JSON + OpenTelemetry + Prometheus
│   └── interfaces/                        # Puertos de entrada
│       ├── rest/                          # Servidor Huma v2 + OpenAPI 3.1
│       ├── mcp/                           # Servidor MCP (stdio y Streamable HTTP/SSE)
│       └── cli/                           # Comandos Cobra
├── migrations/
│   ├── embed.go
│   └── 00001_romero_core_schema.sql
├── configs/
│   └── romero.toml                        # Definición de perfiles lite, balanced y ultra
├── tests/
│   ├── integration/
│   ├── security/
│   └── evaluation/
│       └── golden_dataset.json
├── docker-compose.yml
├── sqlc.yaml
├── Makefile
└── go.mod
```

## 15.2 Interfaces de los Motores Cognitivos en Go (`internal/domain/memory/ports.go`)

```go
package memory

import (
	"context"
	"github.com/google/uuid"
)

// EmbeddingOutput contiene el vector denso FP16 y opcionalmente los pesos dispersos aprendidos.
type EmbeddingOutput struct {
	Dense      []float32
	SparseIdx  []int32
	SparseVals []float32
}

// EmbeddingProvider abstrae BGE-M3 u otros modelos locales/remotos (ADR-009).
type EmbeddingProvider interface {
	ModelName() string
	Dimensions() int
	Embed(ctx context.Context, text string) (EmbeddingOutput, error)
	EmbedBatch(ctx context.Context, texts []string) ([]EmbeddingOutput, error)
}

// RerankCandidate representa un candidato para el Cross-Encoder.
type RerankCandidate struct {
	MemoryID uuid.UUID
	Text     string
}

// RerankerProvider ejecuta atención cruzada sobre el Top-N candidatos en GPU.
type RerankerProvider interface {
	IsEnabled() bool
	Rerank(ctx context.Context, query string, candidates []RerankCandidate) (map[uuid.UUID]float32, error)
}

// NLIRelationResult contiene las probabilidades de inferencia lógica entre dos recuerdos.
type NLIRelationResult struct {
	Entailment    float32
	Neutral       float32
	Contradiction float32
}

// NLIProvider evalúa si un recuerdo nuevo respalda o contradice a otro existente.
type NLIProvider interface {
	IsEnabled() bool
	EvaluatePair(ctx context.Context, premise string, hypothesis string) (NLIRelationResult, error)
}

// ConsolidationSLMProvider destila un clúster de episodios en un recuerdo procedimental o semántico.
type ConsolidationSLMProvider interface {
	AcquireVRAM(ctx context.Context) error
	ReleaseVRAM(ctx context.Context) error
	DistillCluster(ctx context.Context, episodes []Memory) (*DistilledMemoryProposal, error)
}
```

## 15.3 Consulta SQL de Activación Propagada Multi-Salto (`spreading_activation.sql`)

Además de la Caché L1 en RAM, PostgreSQL cuenta con esta consulta recursiva compilada con `sqlc` como respaldo transaccional exacto:

```sql
-- name: TraverseCausalSubgraph :many
WITH RECURSIVE causal_walk AS (
    -- Paso Base: Nodos semilla obtenidos de la búsqueda híbrida inicial
    SELECT 
        m.id AS memory_id,
        0 AS hop_depth,
        1.0::real AS path_weight,
        ARRAY[m.id] AS visited_path
    FROM memories m
    WHERE m.tenant_id = $1
      AND m.id = ANY($2::uuid[])
      AND m.status IN ('active', 'conflicted')

    UNION ALL

    -- Paso Recursivo: Propagación por aristas bitemporalmente vigentes hasta H saltos
    SELECT 
         CASE 
            WHEN r.from_memory_id = cw.memory_id THEN r.to_memory_id 
            ELSE r.from_memory_id 
        END AS memory_id,
        cw.hop_depth + 1 AS hop_depth,
        (cw.path_weight * r.weight * 0.35)::real AS path_weight,
        cw.visited_path || CASE 
            WHEN r.from_memory_id = cw.memory_id THEN r.to_memory_id 
            ELSE r.from_memory_id 
        END
    FROM causal_walk cw
    JOIN memory_relations r 
      ON (r.from_memory_id = cw.memory_id OR r.to_memory_id = cw.memory_id)
    JOIN memories target_m 
      ON target_m.id = CASE 
            WHEN r.from_memory_id = cw.memory_id THEN r.to_memory_id 
            ELSE r.from_memory_id 
        END
    WHERE cw.hop_depth < $3::integer
      AND r.tenant_id = $1
      AND r.is_active = TRUE
      AND r.valid_from <= NOW()
      AND (r.valid_until IS NULL OR r.valid_until > NOW())
      AND target_m.status IN ('active', 'conflicted')
      AND target_m.valid_from <= NOW()
      AND (target_m.valid_until IS NULL OR target_m.valid_until > NOW())
      AND NOT (
          CASE 
              WHEN r.from_memory_id = cw.memory_id THEN r.to_memory_id 
              ELSE r.from_memory_id 
          END = ANY(cw.visited_path)
      )
)
SELECT 
    memory_id,
    MIN(hop_depth)::integer AS min_hop_depth,
    SUM(path_weight)::real AS propagated_activation
FROM causal_walk
GROUP BY memory_id
ORDER BY propagated_activation DESC
LIMIT 50;
```

---

# 16. Infraestructura Local, Configuración de Perfiles y Compilación Cruzada

## 16.1 Archivo de Configuración de Perfiles (`configs/romero.toml`)

```toml
[server]
host = "127.0.0.1"
rest_port = 8420
mcp_sse_port = 8421
active_profile = "ultra" # Opciones: "lite" | "balanced" | "ultra"

[database]
dsn = "postgres://romero:romero_local_secret@127.0.0.1:54329/romero?sslmode=disable"
max_conns = 32
min_conns = 4

[profiles.lite]
enable_l1_graph_cache = false
spreading_activation_hops = 1
enable_cross_encoder = false
enable_nli_detector = false
enable_rem_consolidation = false
embedding_endpoint = "http://127.0.0.1:11434/api/embeddings"

[profiles.balanced]
enable_l1_graph_cache = true
l1_max_memory_mb = 128
spreading_activation_hops = 2
enable_cross_encoder = false
enable_nli_detector = true
nli_mode = "async_worker"
enable_rem_consolidation = false
embedding_endpoint = "http://127.0.0.1:8081/embed"
nli_endpoint = "http://127.0.0.1:8083/nli"

[profiles.ultra]
enable_l1_graph_cache = true
l1_max_memory_mb = 350
spreading_activation_hops = 3
enable_cross_encoder = true
cross_encoder_top_k = 30
enable_nli_detector = true
nli_mode = "realtime_and_worker"
enable_rem_consolidation = true
rem_idle_trigger_seconds = 120
rem_Unload_slm_immediately = true
embedding_endpoint = "http://127.0.0.1:8081/embed"
reranker_endpoint = "http://127.0.0.1:8082/rerank"
nli_endpoint = "http://127.0.0.1:8083/nli"
slm_rem_endpoint = "http://127.0.0.1:11434/api/generate"
slm_rem_model = "qwen2.5:3b-instruct-q4_K_M"
```

## 16.2 `docker-compose.yml` Multi-Perfil para tu Hardware ($24\text{ GB}$ RAM + RTX 4050 $6\text{ GB}$)

```yaml
services:
  romero-postgres:
    image: pgvector/pgvector:pg17
    container_name: romero-postgres
    restart: unless-stopped
    environment:
      POSTGRES_USER: ${ROMERO_DB_USER:-romero}
      POSTGRES_PASSWORD: ${ROMERO_DB_PASSWORD:-romero_local_secret}
      POSTGRES_DB: ${ROMERO_DB_NAME:-romero}
      TZ: UTC
    ports:
      - "127.0.0.1:54329:5432"
    volumes:
      - romero_pgdata:/var/lib/postgresql/data
    command:
      - "postgres"
      - "-c"
      - "shared_buffers=768MB"
      - "-c"
      - "effective_cache_size=1792MB"
      - "-c"
      - "work_mem=32MB"
      - "-c"
      - "maintenance_work_mem=256MB"
      - "-c"
      - "max_connections=100"
      - "-c"
      - "random_page_cost=1.1"
      - "-c"
      - "effective_io_concurrency=200"
      - "-c"
      - "wal_buffers=16MB"
    deploy:
      resources:
        limits:
          memory: 2560M
          cpus: "4.0"
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U ${ROMERO_DB_USER:-romero} -d ${ROMERO_DB_NAME:-romero}"]
      interval: 5s
      timeout: 3s
      retries: 5

  # Motor 1A: Embeddings BGE-M3 en GPU (Perfiles Balanced y Ultra)
  romero-embeddings:
    image: ghcr.io/huggingface/text-embeddings-inference:86-1.5
    container_name: romero-embeddings
    profiles: ["balanced", "ultra"]
    restart: unless-stopped
    ports:
      - "127.0.0.1:8081:80"
    volumes:
      - romero_hf_cache:/data
    command: --model-id BAAI/bge-m3 --dtype float16 --max-batch-tokens 16384
    deploy:
      resources:
        reservations:
          devices:
            - driver: nvidia
              count: 1
              capabilities: [gpu]

  # Motor 1B: Cross-Encoder Reranker en GPU (Perfil Ultra)
  romero-reranker:
    image: ghcr.io/huggingface/text-embeddings-inference:86-1.5
    container_name: romero-reranker
    profiles: ["ultra"]
    restart: unless-stopped
    ports:
      - "127.0.0.1:8082:80"
    volumes:
      - romero_hf_cache:/data
    command: --model-id BAAI/bge-reranker-v2-m3 --dtype float16 --max-batch-tokens 8192
    deploy:
      resources:
        reservations:
          devices:
            - driver: nvidia
              count: 1
              capabilities: [gpu]

volumes:
  romero_pgdata:
  romero_hf_cache:
```

*(Nota: Para el SLM de Sueño REM con descarga instantánea de VRAM se utiliza Ollama nativo o en contenedor pasando el parámetro `"keep_alive": 0` al finalizar el lote de consolidación, garantizando que los $2.2\text{ GB}$ de VRAM se liberen en el acto).*

---

# 17. Observabilidad, Auditoría y Framework de Evaluación Reproducible

## 17.1 Métricas de Calidad Cognitiva (`romero eval`)

El comando integrado `romero eval --profile=ultra` ejecuta el dataset versionado (`golden_dataset.json`) y calcula seis métricas objetivas:

1. **$\text{NDCG}@5$ y $\text{NDCG}@10$:** Evalúa si el Cross-Encoder y la ponderación epistemológica colocan los recuerdos `verified` y vigentes en las primeras posiciones.
2. **$\text{Recall}@K$ Multi-Salto (*Multi-Hop Causal Recall*):** Mide qué porcentaje de dependencias ocultas a $2\text{ y }3\text{ saltos}$ son rescatadas por la Activación Propagada del Grafo L1 frente a una búsqueda vectorial plana.
3. **Tasa de Detección NLI de Contradicciones (*Contradiction Catch Rate*):** Porcentaje de pares contradictorios inyectados en el benchmark que son marcados exitosamente como `conflicted` por `DeBERTa-v3`.
4. **Eficiencia de Compresión REM (*REM Distillation Ratio*):** Reducción en tokens consumidos y mejora en tasa de éxito cuando un agente recibe un procedimiento consolidado en Sueño REM frente a recibir $10$ episodios sueltos.
5. **Tasa de Fuga entre Scopes (*Cross-Scope Leakage*):** Debe ser invariablemente $0.00\%$.
6. **Tasa de Obsolescencia Filtrada (*Stale Memory Suppression*):** Debe asegurar que $0$ recuerdos `superseded` se presenten como vigentes sin haber sido solicitados con `include_superseded = true`.

---

# 18. Registro Definitivo de Decisiones Arquitectónicas (ADRs)

| ID | Decisión Arquitectónica | Estado |
| :--- | :--- | :--- |
| **ADR-001** | Desacoplamiento total entre **ROMERO** (Memoria) y **GANDALF WIZARD** (Meta Harness) vía REST API y MCP. | **Aceptada** |
| **ADR-002** | **PostgreSQL 17** como única fuente de verdad persistente (relacional, vectorial, grafo y cola). | **Aceptada** |
| **ADR-003** | Vectores densos en media precisión `halfvec(1024)` + dispersos con `BGE-M3` sobre `pgvector`. | **Aceptada** |
| **ADR-004** | Binario único en **Go 1.23+ (`CGO_ENABLED=0`)** para portabilidad nativa en Linux, macOS y Windows. | **Aceptada** |
| **ADR-005** | Consultas SQL compiladas estáticamente con **`sqlc` + `pgx/v5`** (cero ORMs con reflexión). | **Aceptada** |
| **ADR-006** | Arquitectura elástica de **3 Perfiles (`Lite`, `Balanced`, `Ultra`)** y **3 Estados (`Guardia`, `Ráfaga`, `Sueño REM`)**. | **Aceptada** |
| **ADR-007** | **Caché Cognitiva L1 en RAM** dentro del proceso Go con **Activación Propagada (*Spreading Activation*)** tipo HippoRAG. | **Aceptada** |
| **ADR-008** | Reordenamiento neuronal con **Cross-Encoder (`bge-reranker-v2-m3`)** en GPU sobre el Top-$30$ de candidatos. | **Aceptada** |
| **ADR-009** | Detección lógica de contradicciones mediante clasificador **NLI (`mDeBERTa-v3`)** y **Calibración Bayesiana $\text{Beta}(\alpha, \beta)$**. | **Aceptada** |
| **ADR-010** | Consolidación asíncrona **"Sueño REM" (*Sleep-Time Compute*)** con SLM $3\text{B}$ local y liberación inmediata de VRAM. | **Aceptada** |
| **ADR-011** | **Empaquetado Submodular de Contexto** con Divulgación Progresiva (*Progressive Disclosure*) para maximizar información por token. | **Aceptada** |
| **ADR-012** | Llaves canónicas **`topic_key`** con supersesión atómica protegida por jerarquía epistemológica. | **Aceptada** |

---

# 19. Matriz de Riesgos, Amenazas y Mitigaciones

| Riesgo | Consecuencia | Mitigación en ROMERO |
| :--- | :--- | :--- |
| **Envenenamiento de memoria por alucinación de un agente** | Un agente guarda una suposición falsa y corrompe ejecuciones futuras. | Jerarquía epistemológica estricta, priors bayesianos $\text{Beta}(\alpha, \beta)$ y prohibición de que una `hypothesis` sobrescriba un recuerdo `verified`. |
| **Contradicciones silenciosas entre agentes concurrentes** | Dos agentes guardan reglas opuestas sobre el mismo módulo. | Clasificador NLI `DeBERTa-v3` que evalúa $P(\text{Contradiction})$ contra vecinos semánticos y etiqueta el conflicto automáticamente. |
| **Contención de VRAM en la RTX 4050 ($6\text{ GB}$) durante consolidación** | Caída de rendimiento si un agente solicita memoria mientras corre el SLM de Sueño REM. | Preempción inmediata en el `PowerGovernor`: si entra tráfico de agentes, se aborta/pausa el lote REM y se libera la VRAM del SLM (`keep_alive = 0`) en $< 50\text{ ms}$. |
| **Pérdida de causas raíz indirectas en consultas complejas** | La búsqueda vectorial simple solo devuelve el síntoma pero no la dependencia rota. | Activación Propagada (*Spreading Activation*) a $3\text{ saltos}$ sobre el Grafo Bitemporal en Caché L1. |
| **Prompt Injection persistente en recuerdos** | Un texto externo malicioso guardado en memoria intenta tomar control de un agente futuro. | Sanitización en captura y encapsulamiento XML `<romero_memory untrusted_content="true">` con procedencia explícita. |

---

# 20. Qué NO Construir Todavía (Fronteras Anti-Sobreingeniería)

Incluso en el perfil `Ultra`, se mantienen fronteras claras para evitar complejidad parásita:

1. **NO desplegar Neo4j ni bases de grafos externas:** La combinación de la tabla indexada `memory_relations` en PostgreSQL y la **Caché Cognitiva L1 en Go** es $10\times$ más rápida en local y no rompe la atomicidad transaccional ACID.
2. **NO invocar LLMs generativos pesados en el camino crítico de escritura o lectura diurna:** En tiempo real solo intervienen modelos discriminativos ultrarrápidos (`BGE-M3`, `Cross-Encoder` y `DeBERTa NLI`, todos $< 8\text{ ms}$). El modelo generativo SLM $3\text{B}$ trabaja exclusivamente en segundo plano durante el **Sueño REM**.
3. **NO usar Redis ni brokers externos de mensajería:** La cola priorizada sobre PostgreSQL (`memory_jobs` con `SKIP LOCKED`) junto con canales en memoria de Go cubre todas las necesidades de concurrencia.
4. **NO superar las 5 herramientas en el servidor MCP:** Toda la sofisticación cognitiva ocurre detrás de las $5$ herramientas simples para no degradar el razonamiento de los agentes.

---

# 21. Roadmap y Plan de Ejecución por Sprints

## Sprint 1 — ROMERO Core, Persistencia Bitemporal, Bayes y Supersesión (`Perfil Lite Base`)
1. Inicializar repositorio Go (`CGO_ENABLED=0`), `docker-compose.yml`, `Makefile` multiplataforma y migración `00001_romero_core_schema.sql`.
2. Implementar autenticación Zero-Trust (`AuthContext`), scopes, los 3 relojes temporales, priors bayesianos $(\alpha, \beta)$ y supersesión por `topic_key`.
3. Exponer API REST v1 (`CaptureMemory`, `GetMemory`, `UpdateMemory`, `SupersedeMemory`, `RecordFeedback`) y pruebas de integración de aislamiento.

## Sprint 2 — Recuperación Híbrida, Caché Cognitiva L1 y Activación Propagada (`Perfil Balanced`)
1. Implementar el worker transaccional por prioridades y el cliente de embeddings `BGE-M3` (`halfvec(1024)`).
2. Construir la **Caché Cognitiva L1 de Grafo en RAM** y el motor de **Activación Propagada (*Spreading Activation*)** a $3\text{ saltos}$.
3. Implementar `SearchMemories` (Trigramas + FTS + Vectores + Grafo L1) y el optimizador submodular en `BuildContext`.

## Sprint 3 — Motores Neuronales `Ultra`: Cross-Encoder Reranker, Detector NLI y Benchmark
1. Integrar el cliente `RerankerProvider` (`bge-reranker-v2-m3`) en el pipeline de búsqueda.
2. Integrar el cliente `NLIProvider` (`mDeBERTa-v3`) para detección automática de relaciones `supports` y `contradicts`.
3. Construir `romero eval` y el `golden_dataset.json` para medir $\text{NDCG}@K$, $\text{Recall}@K$ multi-salto y captura de contradicciones.

## Sprint 4 — Motor de Consolidación "Sueño REM" y `PowerGovernor`
1. Implementar el `PowerGovernor` con transiciones automáticas entre estados **Guardia**, **Ráfaga** y **Sueño REM**.
2. Implementar el agrupamiento de episodios y la destilación a memoria procedimental/semántica con `Qwen2.5-3B-Instruct` y validación NLI cruzada.

## Sprint 5 — Servidor MCP e Integración End-to-End con GANDALF WIZARD
1. Exponer el servidor MCP (`stdio` y `HTTP/SSE`) con las $5$ herramientas canónicas.
2. Construir el SDK cliente en **Gandalf Wizard**, conectando su `Context Builder` y su `Evaluator` al ciclo de feedback bayesiano de Romero.

---

# 22. Glosario Técnico, Cognitivo y Epistemológico

* **Activación Propagada (*Spreading Activation*):** Algoritmo que propaga puntajes de relevancia desde los recuerdos encontrados inicialmente hacia sus vecinos en el grafo causal (`depends_on`, `caused_by`, `supports`).
* **Calibración Bayesiana $\text{Beta}(\alpha, \beta)$:** Modelo probabilístico que actualiza dinámicamente la confianza de un recuerdo sumando evidencias de éxito ($\alpha$) y de fallo ($\beta$).
* **Caché Cognitiva L1:** Réplica en memoria RAM dentro del binario Go que mantiene el subgrafo activo de los proyectos en curso para recorridos relacionales en microsegundos.
* **Cross-Encoder Reranker:** Modelo neuronal que evalúa conjuntamente la consulta y cada recuerdo candidato con atención cruzada completa, logrando una precisión de ordenamiento superior a la similitud coseno simple.
* **Empaquetado Submodular (*Submodular Context Packing*):** Algoritmo de optimización matemática que selecciona el conjunto de recuerdos que maximiza la información relevante y minimiza la redundancia dentro de un límite de tokens.
* **NLI (*Natural Language Inference*):** Modelo clasificador que determina si un enunciado implica lógicamente (`entailment`), es neutro (`neutral`) o contradice (`contradiction`) a otro.
* **Sueño REM (*Sleep-Time Compute*):** Estado operativo en el que Romero aprovecha los periodos de inactividad para agrupar recuerdos episódicos repetidos y destilarlos en procedimientos y conocimiento semántico consolidado.

---

# 23. Definición de Éxito del Proyecto

**ROMERO + GANDALF WIZARD** habrán alcanzado su objetivo definitivo cuando:

1. **Superioridad Cognitiva Demostrada:** En el benchmark `romero eval`, el perfil `Ultra` supere en más de un $35\%$ el $\text{NDCG}@5$ y el $\text{Recall}@5$ en consultas multi-salto frente a sistemas léxicos (Engram) y vectoriales planos, detectando $> 90\%$ de las contradicciones lógicas mediante el motor NLI.
2. **Velocidad Sub-20ms en Tiempo Real:** El pipeline completo de búsqueda híbrida + propagación en Caché L1 + Cross-Encoder Reranker responda en $< 18\text{ ms}$ ($p_{95}$) en tu máquina con Core i5-12400H, $24\text{ GB}$ de RAM y RTX 4050.
3. **Aprendizaje Autónomo en Sueño REM:** Tras múltiples ejecuciones con errores resueltos en un proyecto, el ciclo de Sueño REM genere automáticamente procedimientos verificables (`procedural`) con trazabilidad `derived_from` que eviten que los agentes de Gandalf Wizard vuelvan a cometer el mismo error.
4. **Portabilidad y Aislamiento Inquebrantables:** El binario compile y opere de forma nativa en **Linux, macOS y Windows** con $0.00\%$ de fugas de información entre scopes, tareas o proyectos.