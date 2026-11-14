---
type: analysis
title: Análisis — autorización por tarea y solicitudes opacas
description: Análisis de consistencia de la enmienda de Phase 01.
timestamp: 2026-11-14T00:00:00-03:00
topic: sdd
slug: phase-01-task-authorization
---

# Análisis — autorización por tarea y solicitudes opacas

**GATE: PASS** para el diseño de la enmienda. Segunda lectura del 2026-11-14. Cero hallazgos críticos o altos abiertos en el alcance revisado. El primer análisis había identificado dos altos y tres medios.

| Hallazgo inicial | Criterios | Responsables y prueba prevista |
| --- | --- | --- |
| F1. Unicidad de tema entre proyectos | AC-02 | T004 comprueba datos heredados y sustituye el índice en una migración atómica. T006 usa la misma frontera y T009 enfrenta dos capturas concurrentes desde proyectos distintos. |
| F2. Requisitos de la cola sin trazabilidad | AC-T7–AC-T12 | T004 persiste. T006/T007 aceptan y consultan. T011 procesa y recupera. T008 expone el contrato. T009 verifica concurrencia, idempotencia y retención. |
| F3. Momento de revocación | AC-T1–AC-T4, AC-T9 | T002 define el puerto. T004 vincula la consulta vigente a la transacción. T009 controla el instante de revocación con dos conexiones. |
| F4. Respuestas contradictorias por ID | AC-01, AC-T1, AC-T7, AC-T9, AC-T11 | AC-01 distingue la aceptación durable de la operación síncrona. T008/T009 comparan ID ajeno y ausente al aceptar, al consultar el estado y al fallar la auditoría. |
| F5. Inventario de rutas e interfaces incompleto | AC-T5–AC-T12 | T005 cubre administración local. T008 incluye la ruta de estado en OpenAPI. T011 recibe reserva, renovación, ejecución y limpieza mediante contratos definidos. Cada tarea tiene prueba y previsión de riesgo. |

La revisión cruzó la [constitución](../constitution.md), la [especificación principal](../specs/phase-01-foundation.md), la [enmienda](../specs/phase-01-task-authorization-draft.md), el [plan principal](../plans/phase-01-foundation.md) y la [tabla de tareas](../plans/phase-01-task-authorization-draft.md). La nueva ruta está aprobada, el dominio conserva su independencia de PostgreSQL y la administración permanece en CLI/bootstrap. La tabla asigna cada criterio a una entrega y conserva los IDs de fase. La descripción del índice de T001 documenta el estado inicial que T004 debe migrar.

**Entrega pendiente.** El código existente de T002 requiere la política de permisos explícitos y sus pruebas de regresión. La migración, la reautorización real contra PostgreSQL y la ejecución durable se validarán en sus respectivas tareas. La excepción para diferir sqlc a T004 sigue registrada. Esta revisión documental permite retomar T002 y no acredita integración ni cierre del issue.
