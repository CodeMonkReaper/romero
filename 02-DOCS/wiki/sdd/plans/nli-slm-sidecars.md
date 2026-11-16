---
type: plan
title: Plan & Tasks — Sidecars NLI (:8083) y REM SLM (Qwen 2.5 3B / Ollama)
slug: nli-slm-sidecars
branch: feat/76-nli-slm-sidecars
status: approved
spec: ../specs/nli-slm-sidecars.md
---

# Plan & Tasks — Sidecars NLI (:8083) y REM SLM (Qwen 2.5 3B / Ollama)

## Architecture & contracts

1. **Servicio NLI (`services/nli/`):**
   - `server.py`: servidor HTTP sobre librería estándar (`ThreadingHTTPServer`) exponiendo `GET /health` y `POST /nli` con el contrato exacto de `DeBERTaNLIClient` (`{"model", "pairs":[{"premise","hypothesis"}]}` -> `{"scores":[{"entailment","neutral","contradiction"}]}`).
   - Carga perezosa / configurable de `MoritzLaurer/mDeBERTa-v3-base-mnli-xnli` (`transformers`) con `NLI_DEVICE=cpu` por defecto (0 MiB VRAM en Guard/Burst, `P18`) o `cuda` bajo demanda.
   - `test_server.py`: prueba unitaria HTTP (`unittest`) con predictor inyectado que verifica `/health`, `/nli`, mapeo de probabilidades y errores `400`.
2. **Protocolo Ollama en `QwenSLMClient` (`internal/infrastructure/neural/qwen_slm.go`):**
   - `SLMConfig.Protocol`: `""`/`"custom"` (usa `/consolidate` y `/unload`) o `"ollama"` (usa `POST /api/generate` con `"format": "json"`, `"stream": false`, y `UnloadVRAM` con `"keep_alive": 0`).
   - Helper compartido `normalizeDistilledDraft(rawDraft)` para ambos protocolos (sin duplicar validación de `memory_type` ni truncado de `summary` a 600 chars).
3. **Detección en configuración (`internal/interfaces/cli/config.go` + `cmd/romero/wiring.go`):**
   - `RuntimeNeuralConfig.SLMProtocol`: `"ollama"` cuando `ROMERO_SLM_PROTOCOL=ollama` o el endpoint crudo contiene `/api/generate` (como en `configs/romero.toml`).
4. **Orquestación en `docker-compose.yml`:**
   - `romero-nli`: `build: ./services/nli`, `profiles: ["balanced", "ultra"]`, `127.0.0.1:8083:8083`, `NLI_DEVICE=${ROMERO_NLI_DEVICE:-cpu}`, volumen `romero_hf_cache:/data`.
   - `romero-slm`: `image: ollama/ollama:latest`, `profiles: ["ultra"]`, `127.0.0.1:11434:11434`, reserva GPU NVIDIA, volumen `romero_ollama_cache:/root/.ollama`.

## Task breakdown

| ID | Task | Criteria | Risk | Done check | Deps | Parallel |
| --- | --- | --- | --- | --- | --- | --- |
| T001 | Implementar soporte `Protocol: "ollama"` (`/api/generate` + `keep_alive: 0`) en `QwenSLMClient` con pruebas TDD (`qwen_slm.go`, `phase03_clients_test.go`) | AC-03 | Tier 1 | `go test -v ./internal/infrastructure/neural/...` exits 0 | - | yes |
| T002 | Detectar y propagar `SLMProtocol` en `ResolveRuntimeNeuralConfig` y `cmd/romero/wiring.go` con pruebas TDD | AC-04 | Tier 1 | `go test -v ./internal/interfaces/cli/... ./cmd/romero/...` exits 0 | T001 | no |
| T003 | Crear microservicio NLI (`services/nli/server.py`, `test_server.py`, `requirements.txt`, `Dockerfile`) | AC-01 | Tier 1 | `python -m unittest discover -s services/nli` exits 0 | - | yes |
| T004 | Registrar `romero-nli` y `romero-slm` en `docker-compose.yml` y validar Compose + suite completa Go | AC-01, AC-02, AC-05 | Tier 1 | `docker compose --profile ultra config` + `go vet ./...` + `go test -p 1 -count=1 ./...` exits 0 | T001, T002, T003 | no |
