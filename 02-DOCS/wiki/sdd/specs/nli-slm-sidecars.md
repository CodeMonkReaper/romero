---
type: spec
title: Spec — Sidecars NLI (:8083) y REM SLM (Qwen 2.5 3B / Ollama) y Alineación de QwenSLMClient
description: WHAT and WHY for nli-slm-sidecars — provisión de los servicios NLI (mDeBERTa-v3 en :8083) y SLM REM (Ollama Qwen 2.5 3B en :11434) en docker-compose.yml y soporte nativo de Ollama /api/generate + keep_alive:0 en QwenSLMClient.
tags: [sdd, spec, romero, nli, slm, ollama, docker, power-governor]
timestamp: 2026-11-15T22:43:30-03:00
topic: sdd
slug: nli-slm-sidecars
branch: feat/76-nli-slm-sidecars
status: clarified
---

# Spec — Sidecars NLI (:8083) y REM SLM (Qwen 2.5 3B / Ollama)

> Slug: `nli-slm-sidecars` · Status: clarified · Created: 2026-11-15
> Inherits: [constitution](../constitution.md) · Issue: https://github.com/CodeMonkReaper/romero/issues/76

## Problem & why

Con `#74` y `#75` integrados, `romero serve --config configs/romero.toml` resuelve el perfil `ultra`, genera embeddings `BGE-M3` en GPU (`:8081`) y reordena resultados con `bge-reranker-v2-m3` (`:8082`). Sin embargo, `nli_endpoint` (`:8083/nli`) y `slm_rem_endpoint` (`:11434/api/generate`) carecen de servicios ejecutables en `docker-compose.yml` y `QwenSLMClient` envía peticiones a `/consolidate` y `/unload` en lugar de `/api/generate` de Ollama con `keep_alive: 0`.

## Cost of not building it

Sin estos servicios y la alineación de protocolo:
- La detección automática de contradicciones lógicas (`mDeBERTa-v3`) y la destilación autónoma en Sueño REM (`qwen2.5:3b-instruct-q4_K_M`) no pueden ejecutarse contra servicios reales del stack `ultra`.
- `PowerGovernor.UnloadVRAM` no puede liberar la VRAM de Ollama mediante `"keep_alive": 0` tras consolidar o al recibir preempción `< 10 ms`.

## The cheapest alternative

Ejecutar scripts manuales fuera del repositorio o mockear ambos motores en memoria; se descarta porque impide levantar el perfil `ultra` completo mediante `docker compose --profile ultra up -d` y deja roto el contrato entre `configs/romero.toml` (`:11434/api/generate`) y `QwenSLMClient`.

## Goals

- Proveer el servicio NLI (`MoritzLaurer/mDeBERTa-v3-base-mnli-xnli`) en `:8083/nli` dentro de `docker-compose.yml` (`romero-nli`).
- Proveer el servicio SLM REM (`ollama/ollama:latest`, modelo `qwen2.5:3b-instruct-q4_K_M`) en `:11434` dentro de `docker-compose.yml` (`romero-slm`).
- Alinear `QwenSLMClient` para operar contra Ollama `/api/generate` (`format: "json"`, `keep_alive: 0` en `UnloadVRAM`) conservando compatibilidad con `/consolidate` y `/unload`.
- Preservar el presupuesto VRAM del Principio 18 (`NLI_DEVICE=cpu` por defecto, descarga inmediata en Ollama).

## Non-goals / out of scope

- Entrenamiento o fine-tuning de modelos propios de NLI o SLM.
- Sustituir `text-embeddings-inference` (`:8081` / `:8082`) ni modificar el algoritmo matemático de consolidación en `internal/application/consolidation`.

## Users & context

1. **Operador de ROMERO:** Levanta `docker compose --profile ultra up -d` y ejecuta `romero serve --config configs/romero.toml` o `romero consolidate`.
2. **Agentes y Orquestadores (`Gandalf Wizard`):** Consumen `GET /api/v1/system/status`, `GET /api/v1/conflicts` y `POST /api/v1/admin/consolidate` con los 4 motores disponibles en `ultra`.

## What the user can do

- Levantar el stack completo (`postgres`, `embeddings`, `reranker`, `nli`, `slm`) con `docker compose --profile ultra up -d`, ejecutar `romero serve --config configs/romero.toml` y disparar evaluación NLI o consolidación REM (`POST /api/v1/admin/consolidate` / `romero consolidate`).

## What result they get

- `GET /api/v1/system/status` reporta `reranker_available: true`, `nli_available: true` y `slm_available: true`.
- `POST http://127.0.0.1:8083/nli` devuelve `{"scores":[{"entailment":...,"neutral":...,"contradiction":...}]}`.
- `QwenSLMClient` destila clusters vía `/api/generate` (`format: "json"`) y libera VRAM vía `"keep_alive": 0`.
- *Referencia verificable:* Contratos HTTP de `DeBERTaNLIClient` (`internal/infrastructure/neural/deberta_nli.go`) y Ollama `/api/generate` (`configs/romero.toml`), validados mediante pruebas unitarias Go/Python y `docker compose --profile ultra config`.

## What it must respect

- Principio 18 de `constitution.md` (`<= 2.3 GB` VRAM en Guard/Burst, `<= 4.5 GB` en REM, descarga asíncrona en preempción `< 10 ms`): `romero-nli` corre en CPU por defecto (`NLI_DEVICE=cpu`, `0 MiB` VRAM extra) para no desplazar `BGE-M3` ni `bge-reranker-v2-m3` en la RTX 4050 de `6 GB`, mientras `romero-slm` (`Ollama`) carga `qwen2.5:3b-instruct-q4_K_M` bajo demanda y lo descarga con `keep_alive: 0`.
- Retrocompatibilidad 100% con los tests existentes de `QwenSLMClient` (`/consolidate` y `/unload`) y aislamiento Zero-Trust sin fugas entre ámbitos.

## How it proves it

- Pruebas TDD unitarias de `QwenSLMClient` en modo Ollama (`/api/generate` + `keep_alive: 0`) y modo custom (`/consolidate` + `/unload`).
- Pruebas unitarias de detección de protocolo en `internal/interfaces/cli` y `cmd/romero`.
- Pruebas unitarias del servidor Python NLI (`services/nli/test_server.py`).
- `go vet ./...`, `CGO_ENABLED=0 go test -p 1 -count=1 ./...` y `docker compose --profile ultra config`.

## Behaviour

- `romero-nli` expone `GET /health` y `POST /nli` (`{"model", "pairs":[{"premise","hypothesis"}]}` -> `{"scores":[{"entailment","neutral","contradiction"}]}`) usando `MoritzLaurer/mDeBERTa-v3-base-mnli-xnli`.
- `romero-slm` expone Ollama en `:11434` con GPU NVIDIA y volumen persistente `romero_ollama_cache`.
- `QwenSLMClient` selecciona el adaptador Ollama (`/api/generate` + `"keep_alive": 0`) cuando `Protocol == "ollama"` o la URL configurada apunta a `/api/generate`, manteniendo `/consolidate` y `/unload` para adaptadores custom.

## Acceptance criteria

- **AC-01:** Existe servicio `services/nli` (con `Dockerfile`, servidor HTTP `POST /nli` + `GET /health` para `MoritzLaurer/mDeBERTa-v3-base-mnli-xnli` y test unitario) y está registrado en `docker-compose.yml` como `romero-nli` en puerto `127.0.0.1:8083:8083` bajo perfiles `["balanced", "ultra"]` con `NLI_DEVICE=cpu` por defecto.
- **AC-02:** `docker-compose.yml` incluye `romero-slm` (`ollama/ollama:latest`, perfil `["ultra"]`, puerto `127.0.0.1:11434:11434`, GPU NVIDIA y volumen `romero_ollama_cache`).
- **AC-03:** `QwenSLMClient` soporta protocolo Ollama (`/api/generate` con `format: "json"`, `stream: false`, y `UnloadVRAM` con `keep_alive: 0`) manteniendo compatibilidad total con el protocolo custom (`/consolidate` + `/unload`), seleccionado automáticamente cuando el endpoint original termina en `/api/generate` o por configuración explícita.
- **AC-04:** `internal/interfaces/cli/config.go` y `cmd/romero/wiring.go` propagan el modo Ollama cuando `slm_rem_endpoint` apunta a `/api/generate` (o `ROMERO_SLM_PROTOCOL=ollama`) sin romper `ROMERO_SLM_URL` existente.
- **AC-05:** `go vet ./...`, `CGO_ENABLED=0 go test -p 1 -count=1 ./...`, `services/nli` unit tests y `docker compose --profile ultra config` pasan al 100%.

## Points to clarify

- **suposición tomada** — `romero-nli` se ejecuta en CPU por defecto (`NLI_DEVICE=cpu`) para cumplir el Principio 18 en la RTX 4050 (`6 GB` VRAM donde `bge-m3` + `bge-reranker` + escritorio consumen `~4.1 GB`). *Estado:* Validada en `clarify` (held, `review-after`).
- **suposición tomada** — `QwenSLMClient` detecta automáticamente el protocolo Ollama (`/api/generate` + `"keep_alive": 0`) cuando el endpoint configurado termina en `/api/generate` o `ROMERO_SLM_PROTOCOL=ollama`, preservando `/consolidate` + `/unload` para URLs custom. *Estado:* Validada en `clarify` (held, `review-after`).

## Clarifications

### Session 2026-11-15 (autopilot `/goal`)
- **Q1 (NLI en CPU vs GPU):** Resuelto en favor de `NLI_DEVICE=cpu` por defecto para cumplir P18 en RTX 4050.
- **Q2 (Selección de protocolo en `QwenSLMClient`):** Detección automática por sufijo `/api/generate` o `ROMERO_SLM_PROTOCOL=ollama`.
