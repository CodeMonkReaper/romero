---
author: Luis Haroldo Salamanca Fernandéz
branch: fix/80-slm-timeout-unload
status: in-progress
issue: https://github.com/CodeMonkReaper/romero/issues/80
---
# Fix #80 — Timeout en frío SLM 120s, descarga VRAM tras REM normal y síntesis multi-episodio

## Intent
Corregir los tres fallos observados en la validación en vivo del Sueño REM contra Ollama (`qwen2.5:3b-instruct-q4_K_M`) y `romero-nli`:
1. Elevar `defaultSLMTimeout` a `120 * time.Second` (y hacerlo configurable vía `ROMERO_SLM_TIMEOUT`) para tolerar la carga en frío (`47.58 s` medidos en RTX 4050 WDDM) manteniendo la cancelación inmediata (`< 10 ms`) vía `remCtx`.
2. Invocar `s.slm.UnloadVRAM` al finalizar normalmente `consolidation.Service.runConsolidate` cuando `!cmd.DryRun && len(clusters) > 0` (además de en preempción), cumpliendo `rem_unload_slm_immediately = true` y P18.
3. Mejorar el prompt de `distillViaOllama` para sintetizar los hechos clave de todos los episodios del clúster sin omitir términos exigidos por el gate NLI (`>= 0.80`).

## Scope
- En alcance: `internal/infrastructure/neural/qwen_slm.go`, `internal/infrastructure/neural/phase03_clients_test.go`, `internal/application/consolidation/service.go`, `internal/application/consolidation/service_test.go`, `internal/interfaces/cli/config.go`, `internal/interfaces/cli/config_test.go`, `cmd/romero/wiring.go`.
- Fuera de alcance: relajar los umbrales NLI (`REMMinSourceEntailment = 0.80`).

## Checklist
- [x] Pruebas TDD para `defaultSLMTimeout = 120s` / `ROMERO_SLM_TIMEOUT` y llamada a `UnloadVRAM` al finalizar `Consolidate` normal (RED -> GREEN).
- [x] Implementación de `UnloadVRAM` en `runConsolidate` (solo cuando `!cmd.DryRun && len(clusters) > 0`), timeout `120s` configurable y prompt multi-episodio en `distillViaOllama`.
- [x] `go vet ./...` y `CGO_ENABLED=0 go test -p 1 -count=1 ./...` pasando al 100%.
- [x] Validación en vivo contra los 5 contenedores (`ultra`) comprobando `DistilledCount >= 1` en `romero consolidate` y `ollama ps` vacío inmediatamente después.
- [x] `npx @ericrisco/rsc risk`, revisión independiente de contexto fresco y documentación completa en Issue #80 + PR.

## Evidence
- **TDD RED:** `go test ./internal/application/consolidation/... ./internal/infrastructure/neural/... ./internal/interfaces/cli/...` falló por `UnloadVRAM called once ... got 0` y símbolos `Timeout` / `SLMTimeout` pendientes.
- **TDD GREEN + Suite completa:** `go vet ./...` y `CGO_ENABLED=0 ROMERO_TEST_DSN=... go test -p 1 -count=1 ./...` pasaron todos los paquetes (`exit 0`).
- **Validación en vivo contra los 5 contenedores (`ultra`):** `romero --config configs/romero.toml consolidate` completó en `7506 ms` (`EXIT=0`) con `ClustersFound: 1`, `DistilledCount: 1`, `ConsolidatedEpisodes: 3`, creó el recuerdo `semantic` `8b264e6a-89a1-4013-a664-ba535fb75162` aprobado por `mDeBERTa-v3` (marcando los 3 episodios fuente como `consolidated`), dejó `docker exec romero-slm ollama ps` vacío inmediatamente tras finalizar y redujo la VRAM usada de `3892 MiB` a `3261 MiB` (`2660 MiB` libres).
- **Riesgo y revisión (`rsc risk`):** `tier 1` (`reviewers: 1`, `human approval: not required`); revisión independiente reportó 0 blockers y 1 nit (`ROMERO_SLM_TIMEOUT` aislado e inválidos en `config_test.go`), resuelto y re-verificado.

## Next
Abrir PR con `Closes #80`, comentar resumen de evidencia en Issue #80 e integrar en `main`.
