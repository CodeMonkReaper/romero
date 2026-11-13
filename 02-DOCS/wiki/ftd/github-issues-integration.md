---
author: Luis Haroldo Salamanca Fernandéz
branch: feat/phase-01-foundation
status: in-progress
---
# GitHub Issues + rsc + Matt

## Intent
Usar GitHub Issues como tracker operativo manteniendo las aprobaciones y evidencia de rsc.

## Scope
Configurar documentos compartidos, instrucciones de agentes y etiquetas. La publicación de tareas de Phase 01 se hará tras revisar su desglose; no se migra el historial ni se modifica su estado aquí.

## Checklist
- [x] Registrar tracker, etiquetas y contrato de convivencia; comprobar enlaces.
- [x] Crear solo etiquetas faltantes; comprobarlas en GitHub.
- [x] Verificar Issues habilitado y acceso de lectura.

## Evidence
- GitHub CLI 2.102.0 autenticado como CodeMonkReaper; acceso al remoto verificado en esta sesión.
- Las etiquetas consultadas incluyen wontfix; faltan las otras cuatro de triage.
- Verificación posterior: `hasIssuesEnabled: true`; las cinco etiquetas están presentes; `gh issue list` terminó correctamente con `[]` (sin issues abiertos).
- Comprobación local: archivos y enlaces relativos correctos; `git diff --check` sin errores en las instrucciones y el índice. Cambio documental: pruebas de aplicación no ejecutadas.

## Next
Revisar el desglose vigente de Phase 01 y proponer el mapeo de tareas a Issues antes de publicarlo.
