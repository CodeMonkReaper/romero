---
author: Luis Haroldo Salamanca Fernandéz
branch: main
status: in-progress
---
# Correcciones T001/T002

## Intent
Corregir límites tras sanitizar y alinear permisos persistidos. Completar la evidencia del delta final T002.

## Scope
Autorizado por el usuario: trabajar directamente en main. Migración nueva; no reescribir T001. No implementar T004/T005 ni modificar datos de una base de usuario.
El cambio de migración requiere controles ASDD de nivel 2 sobre el contrato de Phase 01 ya aprobado, más el panel de tres lentes.

## Checklist
- [x] Prueba roja/verde de límites de summary/content después de sanitizar.
- [x] Migración reversible con defaults seguros y rechazo de ámbitos inválidos; PostgreSQL real.
- [x] Revisar el delta final T002 y registrar sus commits, PR e Issue.
- [x] Controles y evidencia actualizados, incluidos los pendientes.

## Evidence
Ticket de seguimiento: https://github.com/CodeMonkReaper/romero/issues/16.
Base local: fed8746. PR #15 fusionado y Issue #2 cerrado, comprobados mediante GitHub. Los informes originales no cubren el delta task_permission final.

Correcciones locales verificadas: regresión roja/verde; SQL real con rechazo legacy atómico, preservación de principal válido y Up/Down/Up; race, vet, build, módulos, lint y vulnerabilidades PASS. Panel de tres revisores PASS; tres mutantes detectados. [Verificación](../sdd/verifications/t001-t002-corrections-2026-11-14.md) y [revisión](../sdd/reviews/t002-final-delta-2026-11-14.md). sqlc diferido a T004, Goose no ejecutado. No se tocó una base del usuario.

## Next
Corrección local lista para revisar en main. No hay commit ni publicación remota; Issue #16 queda abierto hasta entrega. El usuario decide cuándo publicar. Phase 01 sigue pendiente.
