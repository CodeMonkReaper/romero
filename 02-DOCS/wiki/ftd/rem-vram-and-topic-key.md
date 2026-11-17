---
branch: fix/rem-vram-and-topic-key
issue: https://github.com/CodeMonkReaper/romero/issues/82
status: ready-for-pr
---
# Fix REM VRAM (Opción 2) y colisiones en `commitClusterTx`

## Intent
Resolver los dos bloqueos detectados durante las pruebas de estrés y consolidación REM en modo `ultra` sobre GPU de 6 GB VRAM:
1. Timeout/bloqueo de `romero-slm` (`qwen2.5:3b`) al convivir en VRAM con `romero-reranker`.
2. Colisiones `SQLSTATE 23505` en `idx_memories_active_topic_key` y `memory_versions_memory_id_version_key` durante `commitClusterTx`.

## Scope
- [service.go](file:///C:/Users/luis_/Documentos/Romero/internal/application/consolidation/service.go): hooks `SetREMLifecycleHooks`, desambiguación de `topic_key` (`-rem-<uuid8>`) cuando existe memoria activa previa, y recarga de cada episodio en transacción ACID antes de incrementar `ep.Version`.
- [service_test.go](file:///C:/Users/luis_/Documentos/Romero/internal/application/consolidation/service_test.go): prueba unitaria `TestConsolidate_TopicKeyCollisionAndLifecycleHooks`.
- [config.go](file:///C:/Users/luis_/Documentos/Romero/internal/interfaces/cli/config.go) y [romero.toml](file:///C:/Users/luis_/Documentos/Romero/configs/romero.toml): opción `rem_pause_reranker_container = "romero-reranker"` bajo `[profiles.ultra]`.
- [wiring.go](file:///C:/Users/luis_/Documentos/Romero/cmd/romero/wiring.go): `attachREMLifecycleHooks` para pausar el contenedor reranker antes de REM y recargarlo esperando `/health -> 200` al terminar.

## Checklist
- [x] Crear Issue #82 en GitHub y rama `fix/rem-vram-and-topic-key`.
- [x] Implementar desambiguación `topic_key` y refresh de `ep.Version` en `commitClusterTx`.
- [x] Implementar hooks de ciclo de vida REM (Opción 2) configurables en `romero.toml`.
- [x] Ejecutar tests unitarios y prueba end-to-end en vivo con `romero consolidate`.

## Evidence
- Unit tests (`CGO_ENABLED=0 go test ./internal/application/consolidation/... ./internal/interfaces/cli/... ./cmd/romero/...`): **PASS** en los 3 paquetes.
- Compilación (`go build -trimpath -o bin/romero.exe ./cmd/romero`): **OK**.
- Prueba end-to-end en vivo (`.\bin\romero.exe consolidate`): `ClustersFound: 1`, `Preempted: false`, completada sin errores en **14.34 s** con pausa/recarga automática de `romero-reranker` (`http://127.0.0.1:8082/health -> 200`).

## Next
Revisar el diff en la rama `fix/rem-vram-and-topic-key` y abrir PR contra `main` cerrando el Issue #82.
