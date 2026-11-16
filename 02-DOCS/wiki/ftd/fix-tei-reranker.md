---
author: Luis Haroldo Salamanca Fernandéz
branch: fix/74-tei-89-reranker-array
status: in-progress
issue: https://github.com/CodeMonkReaper/romero/issues/74
---
# Fix #74 — TEI 89-1.9 en Docker Compose y decodificación de array en RerankerClient

## Intent
Actualizar las imágenes TEI de `docker-compose.yml` a `ghcr.io/huggingface/text-embeddings-inference:89-1.9` (Ada Lovelace `sm_89`, compatible con redirects relativos de HuggingFace Hub) y soportar la respuesta JSON array raíz (`[{"index":...,"score":...}]`) de TEI `/rerank` en `RerankerClient` para eliminar `degraded_mode: true` en búsqueda híbrida y `BuildContext` bajo perfil `ultra`.

## Scope
- En alcance: `docker-compose.yml` (`romero-embeddings` y `romero-reranker` a tag `89-1.9`), `internal/infrastructure/neural/reranker.go` (decodificación de respuesta array raíz además de `{"results":...}` y `{"scores":...}`), y pruebas unitarias en `internal/infrastructure/neural/phase03_clients_test.go`.
- Fuera de alcance: lectura de `[profiles.*]` desde `configs/romero.toml` (Issue #75) y sidecars NLI/SLM REM (Issue #76).

## Checklist
- [x] Prueba TDD en `phase03_clients_test.go` que falle con respuesta JSON array raíz de TEI `/rerank` y pase tras actualizar `RerankerClient`.
- [x] `docker-compose.yml` actualizado a `ghcr.io/huggingface/text-embeddings-inference:89-1.9` para `romero-embeddings` y `romero-reranker`.
- [x] `go vet ./...` y `CGO_ENABLED=0 go test -p 1 -count=1 ./...` pasando sin regresiones.
- [x] Validación en vivo con `docker compose --profile ultra up -d` y `romero serve` (`ROMERO_PROFILE=ultra`) comprobando `degraded_mode: false` en `/api/v1/memories/search` y `/api/v1/memories/context`.
- [x] `npx @ericrisco/rsc risk` y revisión independiente por subagente con hallazgos resueltos.

## Evidence
- **TDD RED:** `go test -v -run TestRerankerClientTEIArrayResponse ./internal/infrastructure/neural/...` falló con `decode rerank response: json: cannot unmarshal array into Go value of type struct`.
- **TDD GREEN:** `go test -v ./internal/infrastructure/neural/...` pasó (`4.097s`).
- **Suite completa y análisis estático:** `go vet ./...` (`exit 0`) y `CGO_ENABLED=0 ROMERO_TEST_DSN=... go test -p 1 -count=1 ./...` pasaron todos los paquetes (`exit 0`).
- **Validación GPU real (`89-1.9`):** `docker compose --profile ultra up -d` levantó `romero-embeddings` (`:8081`) y `romero-reranker` (`:8082`) sanos en GPU; con `romero serve` en `ROMERO_PROFILE=ultra`, `POST /api/v1/memories/search` devolvió `SEARCH_DEGRADED=False TOTAL=2 TOP_SCORE=0.8318343` y `POST /api/v1/memories/context` devolvió `CTX_DEGRADED=False TOKENS=26`.
- **Riesgo y revisión (`rsc risk`):** `tier 1` (`reviewers: 1`, `human approval: not required`); revisión de contexto fresco sobre `git diff main` reportó `NO_FINDINGS` (0 blockers, 0 nits).

## Next
Abrir PR con `Closes #74`, integrar en `main` y continuar con Issue #75.
