# Knowledge Map (`02-DOCS/wiki/index.md`)

Mapa central del conocimiento del proyecto **ROMERO**.

## Especificación Maestra

| Documento | Ruta |
| :--- | :--- |
| Especificación Maestra de Arquitectura, Producto e Ingeniería (ROMERO + GANDALF WIZARD v2.0.0) | `romero_gandalf_wizard_especificaci_n_maestra_de_arquitectura_producto_e_ingenier_a.md` |

## Harness (`harness/`)

Integración de GitHub Issues y skills Matt/rsc: [contrato de flujo](../../../docs/agents/rsc-matt-workflow.md), [tracker](../../../docs/agents/issue-tracker.md), [registro de configuración](ftd/github-issues-integration.md).

| Documento | Ruta |
| :--- | :--- |
| Perfil de usuario y objetivo del proyecto | `02-DOCS/wiki/harness/user-profile.md` |
| Plan de instalación aceptado del arnés | `02-DOCS/wiki/harness/installation-plan.md` |
| Decisiones del arnés | `02-DOCS/wiki/harness/decisions.md` |

## Spec-Driven Development (`sdd/`)

| Documento | Ruta |
| :--- | :--- |
| Configuración runtime de SDD | `02-DOCS/wiki/sdd/config.yaml` |
| Project constitution (SDD non-negotiables) | `02-DOCS/wiki/sdd/constitution.md` |
| Registro de decisiones arquitectónicas y de SDD | `02-DOCS/wiki/sdd/decisions.md` |
| Criterios de revisión del repositorio | `REVIEW.md` |

## Especificaciones Funcionales (`sdd/specs/`)

| Documento | Ruta |
| :--- | :--- |
| Spec — Phase 01 Foundation (Gobernanza Zero-Trust y Persistencia Tri-Temporal) | `02-DOCS/wiki/sdd/specs/phase-01-foundation.md` |
| Enmienda — autorización explícita por tarea | [Contrato](sdd/specs/phase-01-task-authorization-draft.md) · [plan](sdd/plans/phase-01-task-authorization-draft.md) |
| Spec — Phase 02 Hybrid Retrieval (Recuperación Híbrida, Grafo Causal y Empaquetado de Contexto) | `02-DOCS/wiki/sdd/specs/phase-02-hybrid-retrieval.md` |
| Spec — Phase 03 NLI, Arbitraje de Conflictos, Gobernador de Potencia y Consolidación en Sueño REM | `02-DOCS/wiki/sdd/specs/phase-03-nli-governor-rem.md` |
| Spec — Phase 04 Servidor MCP de Memoria Cognitiva y Librería Cliente Exportable para Gandalf Wizard | `02-DOCS/wiki/sdd/specs/phase-04-mcp-server.md` |
| Spec — Sidecars NLI (:8083) y REM SLM (Qwen 2.5 3B / Ollama) | `02-DOCS/wiki/sdd/specs/nli-slm-sidecars.md` |

## Planes Técnicos (`sdd/plans/`)

| Documento | Ruta |
| :--- | :--- |
| Plan — Phase 01 Foundation (Gobernanza Zero-Trust y Persistencia Tri-Temporal) | `02-DOCS/wiki/sdd/plans/phase-01-foundation.md` |
| Plan — Phase 02 Hybrid Retrieval (Recuperación Híbrida, Grafo Causal y Empaquetado de Contexto) | `02-DOCS/wiki/sdd/plans/phase-02-hybrid-retrieval.md` |
| Plan — Phase 03 NLI, Arbitraje de Conflictos, Gobernador de Potencia y Consolidación en Sueño REM | `02-DOCS/wiki/sdd/plans/phase-03-nli-governor-rem.md` |
| Plan — Phase 04 Servidor MCP de Memoria Cognitiva y Librería Cliente Exportable para Gandalf Wizard | `02-DOCS/wiki/sdd/plans/phase-04-mcp-server.md` |

## Análisis de Consistencia (`sdd/analysis/`)

| Documento | Ruta |
| :--- | :--- |
| Analysis — Phase 01 Foundation (Gobernanza Zero-Trust y Persistencia Tri-Temporal) | `02-DOCS/wiki/sdd/analysis/phase-01-foundation.md` |
| Analysis — autorización por tarea y solicitudes opacas | [Informe 2026-11-14](sdd/analysis/phase-01-task-authorization-2026-11-14.md) |
| Analysis — Phase 02 Hybrid Retrieval (Recuperación Híbrida, Grafo Causal y Empaquetado de Contexto) | `02-DOCS/wiki/sdd/analysis/phase-02-hybrid-retrieval.md` |
| Analysis — Phase 03 NLI, Arbitraje de Conflictos, Gobernador de Potencia y Consolidación en Sueño REM | `02-DOCS/wiki/sdd/analysis/phase-03-nli-governor-rem.md` |
| Analysis — Phase 04 Servidor MCP de Memoria Cognitiva y Librería Cliente Exportable para Gandalf Wizard | `02-DOCS/wiki/sdd/analysis/phase-04-mcp-server.md` |

## Progreso de Implementación (`sdd/progress/`)

| Documento | Ruta |
| :--- | :--- |
| Progress Ledger — Phase 01 Foundation | `02-DOCS/wiki/sdd/progress/phase-01-foundation.md` |
| Progress Ledger — Phase 02 Hybrid Retrieval | `02-DOCS/wiki/sdd/progress/phase-02-hybrid-retrieval.md` |
| Progress Ledger — Phase 03 NLI, Arbitraje de Conflictos, Gobernador de Potencia y Consolidación en Sueño REM | `02-DOCS/wiki/sdd/progress/phase-03-nli-governor-rem.md` |
| Progress Ledger — Phase 04 Servidor MCP y Librería Cliente Exportable | `02-DOCS/wiki/sdd/progress/phase-04-mcp-server.md` |

## Verificación de T001

- [Correcciones T001/T002 y seguimiento #16](ftd/t001-t002-corrections.md)
- [Revisión del delta final T002, tres lentes](sdd/reviews/t002-final-delta-2026-11-14.md)
- [Verificación de correcciones T001/T002](sdd/verifications/t001-t002-corrections-2026-11-14.md)

- [PostgreSQL real y controles de T001](sdd/verifications/phase-01-foundation-T001-2026-11-13.md)
- [Verificación T001 con sqlc diferido por aprobación humana](sdd/verifications/phase-01-foundation-T001-2026-11-13-r2.md)
- [Revisión complementaria de T001](sdd/reviews/phase-01-foundation-T001-validation.md)
- [Verificación de entrega T001 a main](sdd/verifications/phase-01-foundation-T001-main-2026-11-13.md)
- [Revisión completa T001 contra main](sdd/reviews/phase-01-foundation-T001-main.md)






- [Delegación automática Antigravity/Vibe](ftd/antigravity-vibe-delegation.md) · [Protocolo](../../../docs/agents/vibe-delegation.md)
- [Fix REM VRAM (Opción 2) y colisiones en commitClusterTx (#82)](ftd/rem-vram-and-topic-key.md)
