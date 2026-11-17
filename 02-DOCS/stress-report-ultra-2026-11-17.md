# Informe de Pruebas de Estrés, Recursos y Sueño REM — Romero Perfil `ultra`

**Fecha:** 2026-11-17  
**Entorno:** Windows (23.71 GB RAM) + NVIDIA GeForce RTX 4050 Laptop GPU (6141 MiB VRAM) + Docker Desktop  
**Configuración:** `configs/romero.toml` (`active_profile = "ultra"`, `rest_port = 8420`, `mcp_sse_port = 8421`)

---

## 1. Resumen Ejecutivo

- **Carga HTTP:** **100 % de éxito** en todas las ráfagas ejecutadas (`190/190` moderada + `580/580` agresiva antes de REM + `580/580` agresiva post-REM = **1,350/1,350 peticiones OK, 0 errores**).
- **Evaluación cognitiva (`romero eval --hops 3`):** `pass: true` (`ndcg_at_5 = 1`, `multi_hop_recall = 1`, `obsolete_suppression_rate = 1`, `contradiction_catch_rate = 1`, `zero_trust_leakage_rate = 0`).
- **Hallazgo crítico de VRAM en Sueño REM:** con `romero-embeddings` + `romero-reranker` activos (**3.8–4.0 GiB** VRAM ocupados), `romero-slm` (`qwen2.5:3b-instruct-q4_K_M`) agotó el timeout de **125.09 s** y bloqueó `dockerDesktopLinuxEngine`.
- **Validación de la Opción 2 (pausar `romero-reranker` durante REM):** al detener `romero-reranker` (**3816 → 2564/2726 MiB** VRAM), `qwen2.5:3b` ejecutó el Sueño REM en GPU en **7.09 s** (pico **67 °C**, **4814 MiB**, **66.04 W**) y `romero-reranker` recargó en **6.90–8.17 s**.
- **Hallazgo funcional en REM (`SQLSTATE 23505`):** la destilación falló al persistir por colisión en el índice único `idx_memories_active_topic_key` cuando el SLM eligió un `topic_key` ya activo.

---

## 2. Resultados de Rendimiento HTTP

### 2.1 Ráfaga Moderada (190 peticiones)
| Operación | Concurrencia | Total | OK | Throughput | p50 | p95 | Máx |
|---|---|---|---|---|---|---|---|
| `POST /api/v1/memories` | 10 | 50 | 50 | 227.78 req/s | 42 ms | 56 ms | 59 ms |
| `POST /api/v1/memories/search` (`hops=3`) | 16 | 100 | 100 | 35.20 req/s | 419 ms | 568 ms | 747 ms |
| `POST /api/v1/memories/context` (`hops=3`) | 8 | 40 | 40 | 26.64 req/s | 306 ms | 358 ms | 364 ms |

### 2.2 Ráfaga Agresiva Pre-REM vs. Post-REM (580 peticiones cada una, 469 nodos L1 al cierre)
| Operación | Concurrencia | Total | OK | Throughput (Pre → Post) | p50 (Pre → Post) | p95 (Pre → Post) | Máx (Pre → Post) |
|---|---|---|---|---|---|---|---|
| `POST /api/v1/memories` | 32 | 200 | 200 | 181.81 → **201.48 req/s** | 166 → **136 ms** | 211 → **210 ms** | 230 → **248 ms** |
| `POST /api/v1/memories/search` (`hops=3, limit=15`) | 32 | 300 | 300 | 27.53 → **26.21 req/s** | 1.258 → **1.13 s** | 1.677 → **1.478 s** | 1.689 → **1.772 s** |
| `POST /api/v1/memories/context` (`hops=3, max_tokens=4096`) | 16 | 80 | 80 | 25.90 → **24.89 req/s** | 679 → **645 ms** | 780 → **722 ms** | 902 → **779 ms** |

---

## 3. Telemetría de GPU, Temperaturas y Contenedores

### 3.1 GPU (NVIDIA GeForce RTX 4050 Laptop GPU — 6141 MiB VRAM)
| Momento | Temp | Uso GPU | VRAM Usada | Potencia |
|---|---|---|---|---|
| Carga inicial / reposo `ultra` | 55 °C | 17 % | 4013 MiB | 16.02 W |
| Pre-REM sin `romero-reranker` | 57 °C | 8 % | 2726 MiB | 15.66 W |
| Pico durante Sueño REM (`qwen2.5:3b`) | 67 °C | 77 % | **4814 MiB** | 66.04 W |
| Post-estrés agresivo (con los 5 servicios) | **75 °C** | **85 %** | 3904 MiB | **72.65 W** |

### 3.2 Contenedores (`docker stats --no-stream` post-estrés)
| Contenedor | CPU % | RAM Usada | Límite |
|---|---|---|---|
| `romero-postgres` | 1.09 % | 126 MiB | 2.5 GiB |
| `romero-embeddings` | 4.51 % | 1.026 GiB | 7.755 GiB |
| `romero-reranker` | 1.20 % | 3.038 GiB | 7.755 GiB |
| `romero-nli` | 531.59 % *(pico 563.09 %)* | 1.154 GiB | 7.755 GiB |
| `romero-slm` | 0.00 % | 949.7 MiB | 7.755 GiB |

- **Estado final de Caché L1 (`/api/v1/system/status`):** `469` nodos, `583` aristas, `302` embeddings en RAM; `PowerGovernor` en `guard`, los 3 motores (`reranker`, `nli`, `slm`) disponibles.

---

## 4. Recomendaciones Accionables

1. **Integrar la Opción 2 en el `PowerGovernor`:** descargar/pausar `romero-reranker` al entrar en Sueño REM y recargarlo al volver a `Guard`/`Burst` (coste medido: ~6.9–8.2 s de recarga frente a evitar el bloqueo de 6 GB VRAM).
2. **Corregir colisión `idx_memories_active_topic_key` en consolidación REM:** asegurar que `DistillCluster` / `commitClusterTx` genere un `topic_key` único o haga upsert/supersede cuando ya exista una memoria activa con ese `topic_key`.
3. **Añadir `deploy.resources.limits.memory` en `docker-compose.yml`** para `romero-nli`, `romero-embeddings`, `romero-reranker` y `romero-slm`, y limitar `OMP_NUM_THREADS=4` en `romero-nli` para evitar picos de >500 % CPU.
