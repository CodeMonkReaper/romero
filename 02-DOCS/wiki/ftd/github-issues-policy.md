---
branch: codex/github-issues-policy
status: in-progress
---
# rsc con GitHub Issues

## Intent
Aplicar al trabajo nuevo de ROMERO el acuerdo confirmado por el usuario: rsc es el orquestador principal y GitHub Issues registra la ejecución.

## Scope
Instrucciones del proyecto. Publicar tickets del backlog y construir un sincronizador quedan para trabajo posterior. Esta política entra en vigor al integrarse en la rama de trabajo.

## Checklist
- [x] Registrar la política y su punto de entrada; comprobar enlaces y diff.

## Policy
Antes de implementar una tarea nueva aprobada por rsc, buscar en GitHub Issues de CodeMonkReaper/romero si ya existe un ticket abierto o cerrado para ella. Crear el Issue si falta, conservando los identificadores rsc y enlazando especificación, plan, criterios de aceptación, condición de cierre y dependencias. rsc conserva el control del desglose y las aprobaciones; las skills de Matt ayudan a preparar y gestionar los tickets.

Registrar la URL del Issue en el artefacto de trabajo de rsc. Actualizar el Issue al iniciar, bloquearse, terminar verificaciones y entregar. Incluir rama, PR y un resumen de evidencia con enlaces al registro local. Comprobar bloqueos y puertas rsc antes de ejecutar. Un estado o etiqueta de GitHub no sustituye pruebas ni revisiones.

Cerrar el Issue solo con sus criterios cumplidos, controles requeridos aprobados y cambio integrado en la rama por defecto. Si GitHub no está disponible, registrar el bloqueo y continuar únicamente tareas independientes de lectura o preparación; la implementación espera su ticket. No crear tickets para preguntas informativas.

Las ideas se definen en rsc antes de convertirse en tareas ejecutables. El trabajo histórico conserva su evidencia y controles pendientes; su migración al tracker requiere revisar el estado real y evitar duplicados.

## Evidence
`git diff --check` terminó sin errores y `Test-Path` confirmó el destino del enlace en AGENTS.md. No se ejecutan pruebas de aplicación: cambio de instrucciones.

## Next
Integrar esta política en la rama de trabajo junto con la configuración del tracker ya preparada en la carpeta principal.
