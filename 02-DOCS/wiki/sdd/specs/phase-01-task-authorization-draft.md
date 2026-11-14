---
type: spec
title: Borrador de enmienda — autorización por tarea
description: Contrato propuesto para permisos explícitos de task_shared en Phase 01.
status: approved
topic: sdd
slug: phase-01-task-authorization
---

# Borrador de enmienda — autorización por tarea

Esta enmienda complementa la especificación Phase 01. El usuario aprobó su contrato y su integración el 2026-11-13. La aprobación no equivale a una verificación de implementación; se requiere análisis de consistencia antes de implementar.

## Problema y resultado

Conocer un identificador de tarea no demuestra pertenencia. Un principal solo podrá acceder a recuerdos `task_shared` si tiene simultáneamente permisos vigentes sobre el proyecto, el ámbito y la tarea. Los permisos de lectura y escritura son independientes y se comprueban al comenzar cada operación contra PostgreSQL. No hay excepción implícita para administradores u orquestadores.

## Asignaciones

- El servidor conserva cada asignación por inquilino, proyecto, tarea y principal, con permisos de lectura y escritura separados. Dura hasta su revocación explícita.
- Un administrador o un orquestador autorizado para administrar el proyecto puede conceder y revocar permisos. Administrar asignaciones es distinto de acceder al contenido: el concedente no necesita poseer el permiso que concede. No puede actuar fuera de su autoridad de proyecto e inquilino.
- Bootstrap y CLI local administran las asignaciones inicialmente; no se añaden endpoints REST administrativos.
- Concesión o revocación y su auditoría, con actor, tarea, principal y cambio, se confirman juntas. Si falla la auditoría, no se aplica el cambio.
- Una revocación afecta las operaciones cuya comprobación de autorización comienza después de que se confirme. Una operación ya autorizada puede terminar. Los recuerdos permanecen conservados tras revocar todos los permisos.

## Comportamiento y fallos

- Un `task_id` recibido identifica la tarea; no prueba autorización. Si no se puede consultar el estado vigente de permisos, se deniega la operación y se devuelve `503` sin datos del recuerdo; no se utiliza una concesión potencialmente revocada en caché.
- La creación sin escritura devuelve `403` y audita `denied`. Para operaciones por ID se valida el formato, se comprueban los permisos y se devuelve el mismo `404` para ausencia o falta de acceso, sin revelar existencia. La denegación de acceso a un recurso existente se audita. Si falla la auditoría de una denegación, el acceso sigue denegado y se devuelve `503` sin confirmar la existencia ni exponer contenido.
- La escritura comprende captura, actualización, archivado, supersesión y feedback, sujetas a sus demás reglas. Sin lectura, toda solicitud válida y autorizada se acepta de forma indistinguible para procesamiento asíncrono; no se promete que el cambio se haya aplicado.

## Criterios de aceptación

- **AC-T1.** Un principal con acceso al proyecto y ámbito, pero sin asignación a la tarea, no puede acceder a sus recuerdos aunque conozca `task_id` y UUID; lectura/mutación por ID produce `404` auditado y creación produce `403` auditado. La escritura autorizada sin lectura sigue la excepción opaca `202` descrita abajo.
- **AC-T2.** Para cada combinación de lectura/escritura, se permite únicamente la operación concedida; ni el tipo de principal ni una asignación sustituye los permisos de proyecto y ámbito. La respuesta de escritura sin lectura no revela contenido ni metadatos protegidos.
- **AC-T3.** Una operación iniciada tras la confirmación de una revocación observa la revocación; una ya autorizada puede terminar. Una falla al consultar permisos produce `503` y no permite la operación.
- **AC-T4.** Una concesión o revocación no se confirma sin su evento de auditoría. Una denegación cuyo registro falle nunca concede acceso ni expone contenido.
- **AC-T5.** Un orquestador puede gestionar asignaciones únicamente en proyectos que administra y dentro de su inquilino; la administración no concede acceso implícito al contenido. Bootstrap y CLI permiten gestionar las asignaciones sin nuevos endpoints administrativos.
- **AC-T6.** Las pruebas de integración usan PostgreSQL real y cubren dos inquilinos, proyectos, tareas y principales; incluyen concesión, revocación, permisos independientes, fallos de consulta/auditoría y ausencia de fuga por identificador.

- **AC-T7.** Para captura, actualización, supersesión, archivado y feedback con escritura de tarea sin lectura, guardar durablemente antes del `202`. El cuerpo contiene solo `request_id`. Probar objetivo propio, ajeno, ausente y en conflicto. Un fallo al persistir devuelve `503`.
- **AC-T8.** Dos envíos simultáneos del mismo principal e inquilino con la misma clave y solicitud canónica generan una sola solicitud. Una entrada diferente con esa clave devuelve `409` genérico. Otros principales y el espacio de claves de capturas normales son independientes. Sin clave, cada aceptación crea una solicitud.
- **AC-T9.** Al ejecutar, verificar permisos vigentes del principal original y restringir la mutación a la tarea autorizada. Una revocación previa, un objetivo ausente y uno ajeno terminan en `failed` sin mutación. El lector autorizado del estado observa la misma respuesta para ausencia y objetivo ajeno, incluso si adquirió lectura después. Probar también con un segundo lector de la tarea.
- **AC-T10.** Ante fallos transitorios, hacer un intento inicial y tres reintentos tras 1, 2 y 4 segundos. Al agotarlos, conservar `failed`. Con dos trabajadores y una reserva renovable de 60 segundos, una caída antes o después del commit y la recuperación con un token nuevo producen como máximo una mutación.
- **AC-T11.** La ruta de estado exige lectura vigente de proyecto, ámbito y tarea. El lector recibe solo ID y uno de `pending`, `running`, `completed`, `failed`. Ausencia y falta de acceso producen el mismo `404`, o el mismo `503` si falla el control de auditoría.
- **AC-T12.** El plazo configurable de retención, por defecto 30 días, comienza al terminar. Probar que se conservan solicitudes activas y claves de solicitudes retenidas, y que reutilizar la clave después de eliminar su registro crea otra solicitud. OpenAPI carece de ruta de cancelación.

## Decisiones aprobadas

- **Decisión tomada:** fallo de auditoría de denegación devuelve `503`; ausencia y falta de acceso por ID comparten `404`, después de validar formato y comprobar permisos.
- **Decisión tomada:** el control de auditoría equivalente debe ejecutarse también para un ID inexistente; si falla, ambos casos devuelven `503`, sin revelar existencia.
- **Ampliación aprobada:** para toda escritura sin lectura, persistir durablemente la solicitud antes de responder `202` con un identificador opaco de solicitud, sin resultado ni contenido. Si no se puede guardar, responder `503`. La ejecución vuelve a comprobar el permiso vigente; una revocación previa impide ejecutarla. Solo un principal con lectura vigente consulta su resultado mediante una ruta REST de estado; ausente y no visible devuelven el mismo `404` y control de auditoría equivalente. Los fallos transitorios se reintentan de forma acotada sin duplicar efectos; tras agotar intentos, la solicitud queda fallida y conservada, sin reintento automático.
- **Ampliación aprobada:** la clave de idempotencia de solicitudes opacas se limita a tenant y principal; un reintento idéntico devuelve el mismo identificador, y uno diferente devuelve `409` genérico sin datos protegidos. Las capturas normales mantienen el contrato existente por tenant. Toda escritura sin lectura, con éxito o conflicto posterior, recibe inicialmente el mismo `202`.
- **Ampliación aprobada:** la consulta de estado usa una nueva ruta REST de solicitud de memoria. Expone `pendiente`, `ejecutando`, `completada` o `fallida`, sin detalles de conflictos de otros ámbitos. Fallos transitorios admiten tres reintentos tras 1, 2 y 4 segundos; después la solicitud queda fallida. Una reserva de trabajo dura 60 segundos por defecto y se renueva durante la ejecución; al interrumpirse, puede recuperarse tras vencer sin duplicar efectos. Los estados finales y claves asociadas se conservan 30 días por defecto, con retención configurable. La cancelación por API queda fuera de alcance en esta fase.
- **Ampliación aprobada:** en escritura por ID sin lectura se autoriza la tarea indicada y se acepta con `202` sin comprobar públicamente la existencia del recuerdo; la existencia y el acceso al recurso se resuelven al ejecutar. El solicitante sin lectura no obtiene el resultado. Esta regla es una excepción explícita a la respuesta inmediata `404` por ID. Los espacios de idempotencia opaca y normal son separados; una clave opaca puede reutilizarse una vez eliminado su registro. La retención comienza al finalizar y nunca elimina solicitudes pendientes o en ejecución.
- **AC-02 de Phase 01.** La frontera de unicidad para `org_shared` y `global_public` es por inquilino y ámbito. La migración correctiva corresponde a T004 y las pruebas concurrentes a T006 y T009.
