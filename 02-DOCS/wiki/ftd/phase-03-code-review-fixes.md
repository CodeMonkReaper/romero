---
author: Luis Haroldo Salamanca Fernandéz
branch: fix/phase03-code-review
status: done
issue: https://github.com/CodeMonkReaper/romero/issues/61
---
# Phase 03 `/code-review` Fixes

## Intent
Resolver los 5 hallazgos funcionales de Spec y los 3 smells de duplicación de Standards identificados en el `/code-review` de la Fase 03 (`b7f2392...HEAD`).

## Scope
- **Spec #1:** Soportar `summary` y `content` consolidados opcionales en `ReviewConflictCommand`, servicio `conflicts` y endpoint REST `POST /api/v1/conflicts/{id}/resolve`.
- **Spec #2:** Usar `burstWindow` en `PowerGovernor.CanAutoEnterREM` e iniciar el scheduler automático de `rem_sleep` en `servePostgresServer` (`cmd/romero/wiring.go`) usando `ListActiveTenantIDs`.
- **Spec #3:** Propagar `DegradedMode = true` desde `applyCrossEncoderRerank` en `internal/application/retrieval/hybrid.go` cuando falle el reranker en perfil `ultra`.
- **Spec #4:** Devolver `ErrEngineUnavailable` (`503`) en `consolidation.Service.Consolidate` (no `dry_run`) cuando falte SLM/NLI o devuelva `memory.ErrProviderUnavailable`, sin modificar memorias.
- **Spec #5:** Alinear la dirección NLI `Premise: draft, Hypothesis: episode` en `consolidation` y `eval`, e incluir `verifiedScores` en `eval`.
- **Standards:** Extraer `memory.NewVersionFromMemory`, `toConflictMemoryFields` y `newNeuralHTTPClient`.
- **Fuera de alcance:** Cambios de esquema SQL (no se requieren nuevas migraciones) o cambios en Phase 01/02 ajenos a estos puntos.

## Checklist
- [x] Extraer helpers compartidos (`memory.NewVersionFromMemory`, `memory.ErrProviderUnavailable`, `newNeuralHTTPClient`, `mapActiveConflictRow`, `memory.FilterVerifiedForScope`)
- [x] Soportar `Summary`/`Content` opcional en `ReviewConflict` + REST + test
- [x] Propagar `degraded_mode` en falla de Cross-Encoder Reranker (`hybrid.go`) + test
- [x] Devolver `ErrEngineUnavailable` (`503`) en `Consolidate` + alinear dirección NLI en `consolidation` y `eval` + tests
- [x] Implementar `CanAutoEnterREM` (`burstWindow`) + `ListActiveREMTargets` + scheduler idle REM en `wiring.go` + test
- [x] Ejecutar verificación completa (`go vet ./...`, `CGO_ENABLED=0 go build`, `go test -p 1 -count=1 -cover ./...` contra PG17, `romero eval`) y revisión Tier 1

## Evidence
- `sqlc generate`, `go vet ./...`, `CGO_ENABLED=0 go build ./cmd/romero`: exit 0.
- `go test -p 1 -count=1 -cover ./...` con `ROMERO_TEST_DSN` sobre PostgreSQL 17: todos los paquetes (26 paquetes con pruebas, incluidos `tests/integration` y `tests/security`) pasaron (`cmd/romero` 30.7%, `conflicts` 83.0%, `consolidation` 85.4%, `retrieval` 87.4%, `governor` 97.2%, `neural` 82.3%, `rest` 83.0%, `postgres` 64.7%).
- `romero.exe eval --dataset tests/evaluation/golden_dataset.json`: `pass: true` (`ndcg_at_5: 1`, `multi_hop_recall: 1`, `obsolete_suppression_rate: 1`, `zero_trust_leakage_rate: 0`, `contradiction_catch_rate: 1`, `rem_distillation_ratio: 1`).
- Revisión Tier 1 (`research`): 0 bloqueantes, 2 observaciones menores atendidas y re-verificadas.

## Next
Fusionar PR a `main` cerrando Issue #61 y verificar rama `main` sincronizada.
