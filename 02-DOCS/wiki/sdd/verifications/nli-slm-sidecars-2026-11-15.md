---
type: verification
title: Verification — Sidecars NLI (:8083) y REM SLM (Qwen 2.5 3B / Ollama)
slug: nli-slm-sidecars
date: 2026-11-15
verdict: PASS
---

# Verification — `nli-slm-sidecars`

## Checks executed
- `python -B -m unittest discover -s services/nli -v` -> 2/2 PASS (`test_health_and_nli_contract`, `test_rejects_invalid_payload`).
- `docker compose --profile ultra config --services` -> `romero-postgres`, `romero-reranker`, `romero-slm`, `romero-embeddings`, `romero-nli` (exit 0).
- `go vet ./...` -> exit 0.
- `CGO_ENABLED=0 ROMERO_TEST_DSN=postgres://romero:romero_local_secret@127.0.0.1:54329/romero?sslmode=disable go test -p 1 -count=1 ./...` -> todos los paquetes PASS.
- `go run ./cmd/romero eval --dataset tests/evaluation/golden_dataset.json` -> `pass: true` (`ndcg_at_5: 1`, `multi_hop_recall: 1`, `obsolete_suppression_rate: 1`, `zero_trust_leakage_rate: 0`, `contradiction_catch_rate: 1`, `rem_distillation_ratio: 1`).

## Rollback plan
Revertir el commit/PR de `feat/76-nli-slm-sidecars` (`git revert <sha>`), restaurando `docker-compose.yml` y `QwenSLMClient` al estado previo sin impacto en esquema SQL.
