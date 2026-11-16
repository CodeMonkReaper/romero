---
author: Luis Haroldo Salamanca Fernandéz
branch: fix/75-toml-neural-config
status: in-progress
issue: https://github.com/CodeMonkReaper/romero/issues/75
---
# Fix #75 — Leer perfil activo y endpoints neurales desde configs/romero.toml

## Intent
Hacer que `romero serve`, `romero mcp` y `romero consolidate` resuelvan `[runtime].active_profile` y los endpoints del perfil activo desde `configs/romero.toml` (o `--config`), manteniendo precedencia `env > toml > defaults` y normalizando sufijos de ruta (`/embed`, `/rerank`, `/nli`, `/consolidate`, `/api/generate`) para evitar URLs duplicadas en los clientes HTTP.

## Scope
- En alcance: `internal/interfaces/cli/config.go` (`RuntimeNeuralConfig`, `ResolveRuntimeNeuralConfig`, normalización de endpoints), `internal/interfaces/cli/root.go` (propagación de `--config` cuando aplique), `cmd/romero/wiring.go` (`openPostgresRuntime`, `buildNeuralAndGovernor`), y pruebas TDD en `internal/interfaces/cli/` y `cmd/romero/`.
- Fuera de alcance: añadir contenedores o código servidor para NLI/SLM (Issue #76).

## Checklist
- [x] Pruebas TDD que verifiquen lectura de `[server].active_profile` / `[runtime].active_profile` y `[profiles.ultra]` desde TOML, normalización de sufijos (`/embed`, `/api/embeddings`, `/rerank`, `/nli`, `/consolidate`, `/api/generate`) y precedencia de variables de entorno sobre TOML (RED -> GREEN).
- [x] `ResolveRuntimeNeuralConfig` integrado en `cmd/romero/wiring.go` para `BGEM3Client`, `RerankerClient`, `DeBERTaNLIClient`, `QwenSLMClient` y `PowerGovernor`.
- [x] `go vet ./...` y `CGO_ENABLED=0 go test -p 1 -count=1 ./...` en verde.
- [x] Validación en vivo de `romero serve` con `configs/romero.toml` sin exportar `ROMERO_PROFILE`, verificando `active_profile: "ultra"` y `reranker_available: true` en `GET /api/v1/system/status`.
- [x] `npx @ericrisco/rsc risk` y revisión independiente por subagente con hallazgos resueltos.

## Evidence
- **TDD RED:** `go test ./internal/interfaces/cli/... ./cmd/romero/...` falló con `undefined: ResolveRuntimeNeuralConfig` y `undefined: buildNeuralAndGovernorFromConfig`.
- **TDD GREEN + Hallazgos:** `TestResolveRuntimeNeuralConfig`, `TestBuildNeuralAndGovernorFromConfigFile`, `go vet ./...` y `CGO_ENABLED=0 ROMERO_TEST_DSN=... go test -p 1 -count=1 ./...` pasaron todos los paquetes (`exit 0`).
- **Validación en vivo (`--config configs/romero.toml` sin env vars):** `GET /api/v1/system/status` devolvió `ACTIVE_PROFILE=ultra RERANKER=True NLI=True SLM=True`.
- **Riesgo y revisión (`rsc risk`):** `tier 1` (`reviewers: 1`, `human approval: not required`); revisión de contexto fresco reportó 0 blockers y 3 nits (normalización `/api/embeddings`, aviso stderr en parseo TOML fallido y cobertura de casos borde), los 3 resueltos antes de la corrida final de tests.

## Next
Abrir PR con `Closes #75`, integrar en `main` y continuar con Issue #76 (ASDD).
