# Plan de Pruebas — Uso Cotidiano de Alto Rendimiento (ROMERO `ultra`)

**Objetivo:** Validar que ROMERO en perfil `ultra` sostiene trabajo diario intensivo multi-agente (lectura, escritura, NLI asíncrono y Sueño REM) sin degradación de latencia, bloqueos de VRAM ni errores de persistencia.  
**Hardware de referencia:** NVIDIA GeForce RTX 4050 Laptop GPU (6141 MiB VRAM) · 23.71 GB RAM · Docker Desktop.

---

## 1. Criterios de Éxito (SLOs)

| Métrica | Umbral Objetivo |
|---|---|
| Tasa de error HTTP (`2xx`) | **0 %** de errores `5xx` o timeouts |
| Escritura (`POST /api/v1/memories`) | **p95 ≤ 220 ms** |
| Búsqueda 3-hop (`POST /api/v1/memories/search`, uso normal 1–4 agentes) | **p95 ≤ 500 ms** |
| Búsqueda 3-hop en ráfaga (`c=32`) | **p95 ≤ 1.6 s** |
| Construcción de contexto (`POST /api/v1/memories/context`, `max_tokens=2048`) | **p95 ≤ 750 ms** |
| Ciclo completo de Sueño REM + recarga de `romero-reranker` | **≤ 25 s**, sin `SQLSTATE 23505` ni cuelgue de Docker |
| Temperatura GPU (`nvidia-smi`) | **≤ 80 °C** sostenida |
| Evaluación cognitiva (`romero eval --hops 3`) | `pass: true` (`ndcg_at_5 = 1`, `zero_trust_leakage_rate = 0`) |

---

## 2. Precondiciones

1. Contenedores `ultra` arriba y sanos: `romero-postgres`, `romero-embeddings`, `romero-reranker`, `romero-nli`, `romero-slm`.
2. Servidor activo: `.\bin\romero.exe serve` escuchando en `127.0.0.1:8420` (REST) y `127.0.0.1:8421` (MCP SSE).
3. `configs/romero.toml` con `active_profile = "ultra"` y `rem_pause_reranker_container = "romero-reranker"`.
4. Tenant/proyecto aislado para pruebas (ej. `qa-daily-perf`) para no contaminar memorias productivas.

---

## 3. Escenarios de Prueba

### E1 — Arranque y Salud en Estado `guard`
- **Qué prueba:** disponibilidad de base de datos, caché L1 y los 3 motores neuronales.
- **Acción:**
  1. Consultar `GET /api/v1/system/status`.
  2. Ejecutar `.\bin\romero.exe eval --hops 3`.
- **Criterio:** `status = "ok"`, `governor.state = "guard"`, `reranker_available = true`, `nli_available = true`, `slm_available = true`, y `eval` con `pass = true`.

### E2 — Jornada Sostenida Multi-Agente (10 minutos, 2–4 agentes)
- **Qué prueba:** estabilidad de caché L1, Postgres y GPU durante trabajo continuo real (mezcla 70 % lecturas / 30 % escrituras).
- **Acción:** simular 4 clientes concurrentes enviando cada 500 ms:
  - `POST /api/v1/memories/search` (`hops=3`, `limit=10`)
  - `POST /api/v1/memories/context` (`hops=3`, `max_tokens=2048`)
  - `POST /api/v1/memories` (alternando `episodic` y `semantic`)
- **Criterio:** 0 % errores, `p95` de lectura ≤ 500 ms, temperatura GPU ≤ 80 °C, crecimiento estable de `l1_nodes_count` y `l1_embeddings_count`.

### E3 — Ráfaga de Arranque de Subagentes (`burst`, `c=16–32`)
- **Qué prueba:** comportamiento cuando varios subagentes piden contexto al mismo tiempo.
- **Acción:** lanzar 200 escrituras (`c=32`), 300 búsquedas (`c=32, hops=3`) y 80 contextos (`c=16, hops=3`).
- **Criterio:** 580/580 HTTP `2xx`, escritura ≥ 150 req/s, búsqueda ≥ 25 req/s, contexto ≥ 22 req/s.

### E4 — Auditoría NLI y Detección de Contradicciones
- **Qué prueba:** que el worker NLI en CPU detecte contradicciones sin bloquear el camino crítico de lectura.
- **Acción:**
  1. Capturar una memoria `verified` con una afirmación concreta (ej. *"El límite de timeout es 30 s"*).
  2. Capturar otra memoria contradictoria sobre el mismo dominio (ej. *"El límite de timeout es 5 s"*).
  3. Esperar 3–5 s y consultar memorias/conflictos mientras se siguen haciendo búsquedas.
- **Criterio:** lecturas sin bloqueo durante el pico de CPU de `romero-nli`; contradicción auditada en segundo plano.

### E5 — Ciclo Completo de Sueño REM (Opción 2 VRAM)
- **Qué prueba:** pausa automática de `romero-reranker`, destilación con `qwen2.5:3b`, validación NLI, commit ACID sin `23505` y recarga del reranker.
- **Acción:**
  1. Capturar 4 memorias `episodic` con `epistemic_status = "verified"`, `topic_key` distinto y contenido del mismo procedimiento.
  2. Esperar 3 s a que BGE-M3 genere sus embeddings.
  3. Ejecutar `.\bin\romero.exe consolidate --api-key <KEY> --project-id <PROJ_ID>` (o esperar `rem_idle_trigger_seconds = 120` en reposo).
- **Criterio:** `ClustersFound >= 1`, `Preempted = false`, 0 errores `SQLSTATE 23505`, VRAM pico ≤ 5.2 GiB durante REM y `http://127.0.0.1:8082/health` de vuelta en `200` al finalizar.

### E6 — Preempción en Vivo (`rem_sleep` → `burst`)
- **Qué prueba:** prioridad inmediata al agente si llega una petición durante el Sueño REM.
- **Acción:** iniciar `romero consolidate` sobre un lote de episodios e inmediatamente enviar un `POST /api/v1/memories/search` a `127.0.0.1:8420`.
- **Criterio:** la búsqueda responde sin esperar a que termine todo el lote REM; `governor.preemptions_count` se incrementa en `/api/v1/system/status`.

---

## 4. Telemetría y Limpieza Post-Prueba

Durante y al cierre de E2, E3 y E5 registrar:
- `nvidia-smi --query-gpu=temperature.gpu,utilization.gpu,memory.used,memory.total,power.draw --format=csv,noheader`
- `docker stats --no-stream romero-postgres romero-embeddings romero-reranker romero-nli romero-slm`
- `GET /api/v1/system/status`

Al finalizar, eliminar el tenant de pruebas para dejar limpia la memoria productiva:
```powershell
docker exec romero-postgres psql -U romero -d romero -c "DELETE FROM tenants WHERE slug = 'qa-daily-perf';"
```
