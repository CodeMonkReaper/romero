---
type: progress
title: Progress — Sidecars NLI (:8083) y REM SLM (Qwen 2.5 3B / Ollama)
slug: nli-slm-sidecars
branch: feat/76-nli-slm-sidecars
status: in-progress
---

# Progress — `nli-slm-sidecars`

## Tasks
- [x] T001 — `QwenSLMClient` protocolo Ollama (`/api/generate` + `keep_alive: 0`)
- [x] T002 — Detección y propagación `SLMProtocol` en `cli/config.go` y `cmd/romero/wiring.go`
- [x] T003 — Microservicio `services/nli` (`server.py`, `test_server.py`, `Dockerfile`)
- [x] T004 — `docker-compose.yml` (`romero-nli`, `romero-slm`) y validación completa

## Decision queue
- [review-after] Q1 — `romero-nli` usa `NLI_DEVICE=cpu` por defecto para cumplir P18 (`<= 2.3 GB` VRAM en Guard/Burst) en RTX 4050 6 GB, registrado en `specs/nli-slm-sidecars.md`, T003/T004.
- [review-after] Q2 — `QwenSLMClient` selecciona protocolo Ollama automáticamente si el endpoint termina en `/api/generate` o `ROMERO_SLM_PROTOCOL=ollama`, registrado en `specs/nli-slm-sidecars.md`, T001/T002.
