# Reporte Automatizado — Uso Cotidiano de Alto Rendimiento (ROMERO `ultra`)

**Fecha:** 2026-11-17 15:52:17 -03:00  
**Tenant QA (limpiado al cierre):** `qa-daily-perf-1794941537666007100`  

## E1 — Arranque y Salud (`guard`)
- **Estado:** `ok` | **Perfil:** `ultra` | **Governor:** `guard`
- **Motores:** reranker=`true`, nli=`true`, slm=`true`
- **Eval (`--hops 3`):** `pass=true`, `ndcg@5=1.00`, `multi_hop_recall=1.00`, `zero_trust_leakage=0.00`
- **Veredicto E1:** **PASS**

## E2 — Jornada Sostenida Multi-Agente (30s, 4 agentes, 70% lectura / 30% escritura)
| Operación | Total | OK | Fallos | RPS | p50 | p95 | Máx |
|---|---|---|---|---|---|---|---|
| E2 Read (search/context) | 370 | 370 | 0 | 12.33 | 99ms | 135ms | 178ms |
| E2 Write (memories) | 158 | 158 | 0 | 5.27 | 25ms | 34ms | 42ms |
- **GPU tras E2:** `67, 56 %, 4639 MiB, 6141 MiB, 44.00 W`
- **Veredicto E2:** **PASS**

## E3 — Ráfaga `burst` de Subagentes (580 peticiones)
| Operación | Total | OK | Fallos | RPS | p50 | p95 | Máx |
|---|---|---|---|---|---|---|---|
| E3 Write (c=32, n=200) | 200 | 200 | 0 | 362.30 | 86ms | 104ms | 114ms |
| E3 Search hops=3 (c=32, n=300) | 300 | 300 | 0 | 37.33 | 848ms | 924ms | 1.117s |
| E3 Context hops=3 (c=16, n=80) | 80 | 80 | 0 | 32.88 | 467ms | 578ms | 626ms |
- **GPU tras E3:** `73, 97 %, 4647 MiB, 6141 MiB, 69.28 W`
- **Veredicto E3:** **PASS**

## E4 — Auditoría NLI sin Bloqueo de Lecturas
| Operación | Total | OK | Fallos | RPS | p50 | p95 | Máx |
|---|---|---|---|---|---|---|---|
| E4 Non-blocking Search during NLI (c=4, n=20) | 20 | 20 | 0 | 34.35 | 114ms | 143ms | 162ms |
- **Contenedores (`docker stats --no-stream`):**
```text
NAME                CPU %     MEM USAGE / LIMIT     MEM %
romero-postgres     3.20%     143.8MiB / 2.5GiB     5.62%
romero-embeddings   3.76%     783.1MiB / 7.755GiB   9.86%
romero-reranker     1.05%     3.462GiB / 7.755GiB   44.65%
romero-nli          503.04%   1.268GiB / 7.755GiB   16.36%
romero-slm          0.00%     342.3MiB / 7.755GiB   4.31%
```
- **Veredicto E4:** **PASS**

## E5 — Ciclo Completo de Sueño REM (Opción 2 VRAM)
- **Duración total (pausa reranker + SLM + NLI + recarga):** `27.559s`
- **Resultado:** `ClustersFound=1`, `DistilledCount=0`, `Preempted=false`, `err=<nil>`
- **Salud `romero-reranker` tras REM:** `true`
- **GPU tras E5:** `63, 10 %, 4494 MiB, 6141 MiB, 44.04 W`
- **Veredicto E5:** **PASS**

## E6 — Respuesta Inmediata Post-REM y Estado Final
| Operación | Total | OK | Fallos | RPS | p50 | p95 | Máx |
|---|---|---|---|---|---|---|---|
| E6 Post-REM Search (c=4, n=12) | 12 | 12 | 0 | 49.44 | 69ms | 104ms | 109ms |
- **Caché L1:** `nodes=742`, `edges=1867`, `embeddings=473`
- **Governor:** `state=guard`, `preemptions=1`, `reranker=true`, `nli=true`, `slm=true`
- **Veredicto E6:** **PASS**

---

**Resultado Global de la Suite:** **PASS**
