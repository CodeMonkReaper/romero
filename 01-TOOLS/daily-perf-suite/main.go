package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type phaseMetrics struct {
	Name      string
	Total     int
	OK        int64
	Fail      int64
	Elapsed   time.Duration
	RPS       float64
	Min       time.Duration
	P50       time.Duration
	P95       time.Duration
	P99       time.Duration
	Max       time.Duration
	SampleErr string
}

type bootstrapOutput struct {
	APIKey  string `json:"api_key"`
	Project struct {
		ID string `json:"ID"`
	} `json:"project"`
	Tenant struct {
		Slug string `json:"Slug"`
	} `json:"tenant"`
}

type systemStatus struct {
	Status            string `json:"status"`
	Database          string `json:"database"`
	ActiveProfile     string `json:"active_profile"`
	L1NodesCount      int    `json:"l1_nodes_count"`
	L1EdgesCount      int    `json:"l1_edges_count"`
	L1EmbeddingsCount int    `json:"l1_embeddings_count"`
	Governor          struct {
		Profile           string `json:"profile"`
		State             string `json:"state"`
		PreemptionsCount  int    `json:"preemptions_count"`
		RerankerAvailable bool   `json:"reranker_available"`
		NLIAvailable      bool   `json:"nli_available"`
		SLMAvailable      bool   `json:"slm_available"`
	} `json:"governor"`
}

type evalResult struct {
	CasesEvaluated         int     `json:"cases_evaluated"`
	NDCGAt5                float64 `json:"ndcg_at_5"`
	MultiHopRecall         float64 `json:"multi_hop_recall"`
	ObsoleteSuppression    float64 `json:"obsolete_suppression_rate"`
	ZeroTrustLeakage       float64 `json:"zero_trust_leakage_rate"`
	ContradictionCatchRate float64 `json:"contradiction_catch_rate"`
	Pass                   bool    `json:"pass"`
}

type consolidateResult struct {
	ClustersFound        int  `json:"ClustersFound"`
	DistilledCount       int  `json:"DistilledCount"`
	ConsolidatedEpisodes int  `json:"ConsolidatedEpisodes"`
	Preempted            bool `json:"Preempted"`
}

func main() {
	baseURL := flag.String("url", "http://127.0.0.1:8420", "Romero REST base URL")
	romeroBin := flag.String("bin", filepath.Join("bin", "romero.exe"), "Path to romero binary")
	e2Dur := flag.Duration("e2-duration", 30*time.Second, "Duration of E2 sustained multi-agent test")
	outPath := flag.String("out", filepath.Join("02-DOCS", fmt.Sprintf("reporte-uso-cotidiano-ultra-%s.md", time.Now().Format("2006-01-02"))), "Output Markdown report path")
	flag.Parse()

	client := &http.Client{
		Timeout: 60 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        200,
			MaxIdleConnsPerHost: 200,
		},
	}

	ts := time.Now().UnixNano()
	slug := fmt.Sprintf("qa-daily-perf-%d", ts)

	fmt.Println("==> Bootstrapping isolated QA tenant:", slug)
	bootOut, err := runCmd(*romeroBin, "bootstrap",
		"--tenant-name", "QA Daily Perf",
		"--tenant-slug", slug,
		"--project-name", "QA Daily Project",
		"--project-slug", "qa-proj",
		"--principal-name", "qa-runner",
		"--principal-type", "service",
		"--scopes", "project_shared,agent_private",
		"--admin",
	)
	if err != nil {
		fatalf("bootstrap failed: %v\n%s", err, bootOut)
	}
	var boot bootstrapOutput
	if err := json.Unmarshal([]byte(bootOut), &boot); err != nil {
		fatalf("parse bootstrap JSON: %v\n%s", err, bootOut)
	}
	defer func() {
		fmt.Println("==> Cleaning up QA tenant:", slug)
		_, _ = runCmd("docker", "exec", "romero-postgres", "psql", "-U", "romero", "-d", "romero", "-c",
			fmt.Sprintf("DELETE FROM tenants WHERE slug = '%s';", slug))
	}()

	var report strings.Builder
	report.WriteString("# Reporte Automatizado — Uso Cotidiano de Alto Rendimiento (ROMERO `ultra`)\n\n")
	report.WriteString(fmt.Sprintf("**Fecha:** %s  \n", time.Now().Format("2006-01-02 15:04:05 -07:00")))
	report.WriteString(fmt.Sprintf("**Tenant QA (limpiado al cierre):** `%s`  \n\n", slug))

	// E1: Health & Eval
	fmt.Println("==> E1: Health & Cognitive Eval")
	status1, err := getStatus(client, *baseURL, boot.APIKey)
	if err != nil {
		fatalf("E1 status failed: %v", err)
	}
	evalOut, evalErr := runCmd(*romeroBin, "eval", "--hops", "3")
	var ev evalResult
	if evalErr == nil {
		_ = json.Unmarshal([]byte(evalOut), &ev)
	}
	e1Pass := status1.Status == "ok" && status1.Governor.RerankerAvailable && status1.Governor.NLIAvailable && status1.Governor.SLMAvailable && evalErr == nil && ev.Pass
	report.WriteString("## E1 — Arranque y Salud (`guard`)\n")
	report.WriteString(fmt.Sprintf("- **Estado:** `%s` | **Perfil:** `%s` | **Governor:** `%s`\n", status1.Status, status1.ActiveProfile, status1.Governor.State))
	report.WriteString(fmt.Sprintf("- **Motores:** reranker=`%v`, nli=`%v`, slm=`%v`\n", status1.Governor.RerankerAvailable, status1.Governor.NLIAvailable, status1.Governor.SLMAvailable))
	report.WriteString(fmt.Sprintf("- **Eval (`--hops 3`):** `pass=%v`, `ndcg@5=%.2f`, `multi_hop_recall=%.2f`, `zero_trust_leakage=%.2f`\n", ev.Pass, ev.NDCGAt5, ev.MultiHopRecall, ev.ZeroTrustLeakage))
	report.WriteString(fmt.Sprintf("- **Veredicto E1:** **%s**\n\n", verdict(e1Pass)))

	// Seed base memories for E2 searches
	for i := 0; i < 8; i++ {
		_, _, _ = postMemory(client, *baseURL, boot.APIKey, boot.Project.ID, "semantic", "observed",
			fmt.Sprintf("seed-daily-%d-%d", ts, i),
			fmt.Sprintf("Baseline architecture note %d for Vault JWT rotation and pgvector HNSW", i),
			fmt.Sprintf("Production service uses Vault transit engine for 24h JWT rotation and pgvector halfvec HNSW index #%d.", i),
			fmt.Sprintf("seed-%d-%d", ts, i))
	}
	time.Sleep(2 * time.Second)

	// E2: Sustained Multi-Agent (4 concurrent agents, 70% read / 30% write)
	fmt.Printf("==> E2: Sustained Multi-Agent (%s, 4 agents)\n", e2Dur.String())
	e2Read, e2Write := runSustainedE2(client, *baseURL, boot.APIKey, boot.Project.ID, ts, *e2Dur)
	gpuE2 := readGPU()
	e2Pass := e2Read.Fail == 0 && e2Write.Fail == 0 && e2Read.P95 <= 500*time.Millisecond && e2Write.P95 <= 220*time.Millisecond
	report.WriteString(fmt.Sprintf("## E2 — Jornada Sostenida Multi-Agente (%s, 4 agentes, 70%% lectura / 30%% escritura)\n", e2Dur.String()))
	report.WriteString(metricsTable([]phaseMetrics{e2Read, e2Write}))
	report.WriteString(fmt.Sprintf("- **GPU tras E2:** `%s`\n", gpuE2))
	report.WriteString(fmt.Sprintf("- **Veredicto E2:** **%s**\n\n", verdict(e2Pass)))

	// E3: Aggressive Burst (c=16-32, 580 requests)
	fmt.Println("==> E3: Aggressive Subagent Burst (580 requests)")
	e3Write := runFixedBurst("E3 Write (c=32, n=200)", 200, 32, func(i int) (int, string, error) {
		mType := "episodic"
		if i%4 == 0 {
			mType = "semantic"
		}
		return postMemory(client, *baseURL, boot.APIKey, boot.Project.ID, mType, "observed",
			fmt.Sprintf("burst-%d-%d", ts, i%20),
			fmt.Sprintf("Burst memory %d for subagent swarm", i),
			fmt.Sprintf("Burst content #%d testing 32-way concurrency on Vault transit, pgvector, BGE-M3 and Cross-Encoder.", i),
			fmt.Sprintf("burst-ext-%d-%d", ts, i))
	})
	time.Sleep(3 * time.Second)
	e3Search := runFixedBurst("E3 Search hops=3 (c=32, n=300)", 300, 32, func(i int) (int, string, error) {
		return doJSON(client, "POST", *baseURL+"/api/v1/memories/search", boot.APIKey, map[string]any{
			"query":       "Vault transit JWT rotation pgvector HNSW Cross-Encoder",
			"project_ids": []string{boot.Project.ID},
			"scopes":      []string{"project_shared"},
			"limit":       15,
			"hops":        3,
		})
	})
	e3Ctx := runFixedBurst("E3 Context hops=3 (c=16, n=80)", 80, 16, func(i int) (int, string, error) {
		return doJSON(client, "POST", *baseURL+"/api/v1/memories/context", boot.APIKey, map[string]any{
			"query":       "Vault transit JWT rotation pgvector HNSW Cross-Encoder",
			"project_ids": []string{boot.Project.ID},
			"scopes":      []string{"project_shared"},
			"max_tokens":  2048,
			"hops":        3,
		})
	})
	gpuE3 := readGPU()
	e3Pass := e3Write.Fail == 0 && e3Search.Fail == 0 && e3Ctx.Fail == 0 && e3Search.P95 <= 1600*time.Millisecond
	report.WriteString("## E3 — Ráfaga `burst` de Subagentes (580 peticiones)\n")
	report.WriteString(metricsTable([]phaseMetrics{e3Write, e3Search, e3Ctx}))
	report.WriteString(fmt.Sprintf("- **GPU tras E3:** `%s`\n", gpuE3))
	report.WriteString(fmt.Sprintf("- **Veredicto E3:** **%s**\n\n", verdict(e3Pass)))

	// E4: NLI Contradiction Audit under concurrent reads
	fmt.Println("==> E4: NLI Contradiction Audit under concurrent reads")
	_, _, _ = postMemory(client, *baseURL, boot.APIKey, boot.Project.ID, "semantic", "verified",
		fmt.Sprintf("nli-timeout-%d-a", ts), "HTTP client timeout is 30 seconds",
		"The production Romero HTTP client timeout is strictly configured to 30 seconds.", fmt.Sprintf("nli-a-%d", ts))
	_, _, _ = postMemory(client, *baseURL, boot.APIKey, boot.Project.ID, "semantic", "verified",
		fmt.Sprintf("nli-timeout-%d-b", ts), "HTTP client timeout is 5 seconds",
		"The production Romero HTTP client timeout is strictly configured to 5 seconds.", fmt.Sprintf("nli-b-%d", ts))
	e4Search := runFixedBurst("E4 Non-blocking Search during NLI (c=4, n=20)", 20, 4, func(i int) (int, string, error) {
		return doJSON(client, "POST", *baseURL+"/api/v1/memories/search", boot.APIKey, map[string]any{
			"query":       "HTTP client timeout seconds",
			"project_ids": []string{boot.Project.ID},
			"scopes":      []string{"project_shared"},
			"limit":       5,
			"hops":        3,
		})
	})
	dockerStats := readDockerStats()
	e4Pass := e4Search.Fail == 0
	report.WriteString("## E4 — Auditoría NLI sin Bloqueo de Lecturas\n")
	report.WriteString(metricsTable([]phaseMetrics{e4Search}))
	report.WriteString("- **Contenedores (`docker stats --no-stream`):**\n```text\n" + dockerStats + "\n```\n")
	report.WriteString(fmt.Sprintf("- **Veredicto E4:** **%s**\n\n", verdict(e4Pass)))

	// E5: Full REM Sleep Cycle (Option 2)
	fmt.Println("==> E5: REM Sleep Consolidation (Option 2)")
	days := []struct{ k, c string }{
		{"mon", "On Monday 02:00 UTC, Vault transit rotated JWT key v21 with a 2-hour overlap window and 0 auth errors."},
		{"tue", "On Tuesday 02:00 UTC, Vault transit rotated JWT key v22 with a 2-hour overlap window and 0 auth errors."},
		{"wed", "On Wednesday 02:00 UTC, Vault transit rotated JWT key v23 with a 2-hour overlap window and 0 auth errors."},
		{"thu", "On Thursday 02:00 UTC, Vault transit rotated JWT key v24 with a 2-hour overlap window and 0 auth errors."},
	}
	for _, d := range days {
		_, _, _ = postMemory(client, *baseURL, boot.APIKey, boot.Project.ID, "episodic", "verified",
			fmt.Sprintf("rem-suite-%d-%s", ts, d.k),
			fmt.Sprintf("Vault transit JWT key rotation on %s succeeded", d.k),
			d.c, fmt.Sprintf("rem-suite-%d-%s", ts, d.k))
	}
	waitForEmbeddings(fmt.Sprintf("rem-suite-%d-%%", ts), 4, 45*time.Second)
	remStart := time.Now()
	remOut, remErr := runCmd(*romeroBin, "consolidate", "--api-key", boot.APIKey, "--project-id", boot.Project.ID)
	remElapsed := time.Since(remStart)
	var remRes consolidateResult
	if remErr == nil {
		_ = json.Unmarshal([]byte(remOut), &remRes)
	}
	rerankerOK := checkHealth(client, "http://127.0.0.1:8082/health")
	gpuE5 := readGPU()
	e5Pass := remErr == nil && remRes.ClustersFound >= 1 && !remRes.Preempted && rerankerOK
	report.WriteString("## E5 — Ciclo Completo de Sueño REM (Opción 2 VRAM)\n")
	report.WriteString(fmt.Sprintf("- **Duración total (pausa reranker + SLM + NLI + recarga):** `%s`\n", remElapsed.Round(time.Millisecond)))
	report.WriteString(fmt.Sprintf("- **Resultado:** `ClustersFound=%d`, `DistilledCount=%d`, `Preempted=%v`, `err=%v`\n", remRes.ClustersFound, remRes.DistilledCount, remRes.Preempted, remErr))
	report.WriteString(fmt.Sprintf("- **Salud `romero-reranker` tras REM:** `%v`\n", rerankerOK))
	report.WriteString(fmt.Sprintf("- **GPU tras E5:** `%s`\n", gpuE5))
	report.WriteString(fmt.Sprintf("- **Veredicto E5:** **%s**\n\n", verdict(e5Pass)))

	// E6: Live Search & Final System Status
	fmt.Println("==> E6: Post-REM Search Responsiveness & System Status")
	e6Search := runFixedBurst("E6 Post-REM Search (c=4, n=12)", 12, 4, func(i int) (int, string, error) {
		return doJSON(client, "POST", *baseURL+"/api/v1/memories/search", boot.APIKey, map[string]any{
			"query":       "Vault transit JWT key rotation overlap",
			"project_ids": []string{boot.Project.ID},
			"scopes":      []string{"project_shared"},
			"limit":       5,
			"hops":        3,
		})
	})
	statusFinal, _ := getStatus(client, *baseURL, boot.APIKey)
	e6Pass := e6Search.Fail == 0 && statusFinal.Status == "ok" && statusFinal.Governor.RerankerAvailable
	report.WriteString("## E6 — Respuesta Inmediata Post-REM y Estado Final\n")
	report.WriteString(metricsTable([]phaseMetrics{e6Search}))
	report.WriteString(fmt.Sprintf("- **Caché L1:** `nodes=%d`, `edges=%d`, `embeddings=%d`\n", statusFinal.L1NodesCount, statusFinal.L1EdgesCount, statusFinal.L1EmbeddingsCount))
	report.WriteString(fmt.Sprintf("- **Governor:** `state=%s`, `preemptions=%d`, `reranker=%v`, `nli=%v`, `slm=%v`\n",
		statusFinal.Governor.State, statusFinal.Governor.PreemptionsCount, statusFinal.Governor.RerankerAvailable, statusFinal.Governor.NLIAvailable, statusFinal.Governor.SLMAvailable))
	report.WriteString(fmt.Sprintf("- **Veredicto E6:** **%s**\n\n", verdict(e6Pass)))

	overall := e1Pass && e2Pass && e3Pass && e4Pass && e5Pass && e6Pass
	report.WriteString(fmt.Sprintf("---\n\n**Resultado Global de la Suite:** **%s**\n", verdict(overall)))

	if err := os.MkdirAll(filepath.Dir(*outPath), 0o755); err != nil {
		fatalf("mkdir report dir: %v", err)
	}
	if err := os.WriteFile(*outPath, []byte(report.String()), 0o644); err != nil {
		fatalf("write report: %v", err)
	}
	fmt.Printf("==> Report written to %s (overall=%s)\n", *outPath, verdict(overall))
}

func runSustainedE2(client *http.Client, baseURL, apiKey, projectID string, ts int64, dur time.Duration) (phaseMetrics, phaseMetrics) {
	var readLat, writeLat []time.Duration
	var rMu, wMu sync.Mutex
	var rOK, rFail, wOK, wFail int64
	var seq int64

	deadline := time.Now().Add(dur)
	var wg sync.WaitGroup
	for agent := 0; agent < 4; agent++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for time.Now().Before(deadline) {
				n := atomic.AddInt64(&seq, 1)
				t0 := time.Now()
				if n%10 < 7 {
					var code int
					var err error
					if n%2 == 0 {
						code, _, err = doJSON(client, "POST", baseURL+"/api/v1/memories/search", apiKey, map[string]any{
							"query":       "Vault JWT rotation pgvector HNSW",
							"project_ids": []string{projectID},
							"scopes":      []string{"project_shared"},
							"limit":       10,
							"hops":        3,
						})
					} else {
						code, _, err = doJSON(client, "POST", baseURL+"/api/v1/memories/context", apiKey, map[string]any{
							"query":       "Vault JWT rotation pgvector HNSW",
							"project_ids": []string{projectID},
							"scopes":      []string{"project_shared"},
							"max_tokens":  2048,
							"hops":        3,
						})
					}
					dt := time.Since(t0)
					rMu.Lock()
					readLat = append(readLat, dt)
					rMu.Unlock()
					if err == nil && code >= 200 && code < 300 {
						atomic.AddInt64(&rOK, 1)
					} else {
						atomic.AddInt64(&rFail, 1)
					}
				} else {
					code, _, err := postMemory(client, baseURL, apiKey, projectID, "semantic", "observed",
						fmt.Sprintf("e2-agent-%d-%d-%d", id, ts, n%15),
						fmt.Sprintf("Agent %d sustained observation %d", id, n),
						fmt.Sprintf("Sustained daily work note #%d from agent %d on Vault and pgvector.", n, id),
						fmt.Sprintf("e2-%d-%d-%d", ts, id, n))
					dt := time.Since(t0)
					wMu.Lock()
					writeLat = append(writeLat, dt)
					wMu.Unlock()
					if err == nil && code >= 200 && code < 300 {
						atomic.AddInt64(&wOK, 1)
					} else {
						atomic.AddInt64(&wFail, 1)
					}
				}
				time.Sleep(150 * time.Millisecond)
			}
		}(agent)
	}
	wg.Wait()

	return summarize("E2 Read (search/context)", rOK, rFail, dur, readLat, ""),
		summarize("E2 Write (memories)", wOK, wFail, dur, writeLat, "")
}

func runFixedBurst(name string, total, concurrency int, fn func(i int) (int, string, error)) phaseMetrics {
	lat := make([]time.Duration, total)
	jobs := make(chan int, total)
	for i := 0; i < total; i++ {
		jobs <- i
	}
	close(jobs)

	var ok, fail int64
	var sampleErr string
	var errMu sync.Mutex

	start := time.Now()
	var wg sync.WaitGroup
	for w := 0; w < concurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range jobs {
				t0 := time.Now()
				code, body, err := fn(idx)
				lat[idx] = time.Since(t0)
				if err == nil && code >= 200 && code < 300 {
					atomic.AddInt64(&ok, 1)
				} else {
					atomic.AddInt64(&fail, 1)
					errMu.Lock()
					if sampleErr == "" {
						if err != nil {
							sampleErr = err.Error()
						} else {
							sampleErr = fmt.Sprintf("HTTP %d: %s", code, body)
						}
					}
					errMu.Unlock()
				}
			}
		}()
	}
	wg.Wait()
	return summarize(name, ok, fail, time.Since(start), lat, sampleErr)
}

func summarize(name string, ok, fail int64, elapsed time.Duration, lat []time.Duration, sampleErr string) phaseMetrics {
	total := int(ok + fail)
	m := phaseMetrics{Name: name, Total: total, OK: ok, Fail: fail, Elapsed: elapsed, SampleErr: sampleErr}
	if elapsed.Seconds() > 0 {
		m.RPS = float64(total) / elapsed.Seconds()
	}
	if len(lat) == 0 {
		return m
	}
	sort.Slice(lat, func(i, j int) bool { return lat[i] < lat[j] })
	pct := func(p float64) time.Duration {
		return lat[int(float64(len(lat)-1)*p)]
	}
	m.Min, m.P50, m.P95, m.P99, m.Max = pct(0), pct(0.50), pct(0.95), pct(0.99), pct(1.0)
	return m
}

func metricsTable(rows []phaseMetrics) string {
	var b strings.Builder
	b.WriteString("| Operación | Total | OK | Fallos | RPS | p50 | p95 | Máx |\n")
	b.WriteString("|---|---|---|---|---|---|---|---|\n")
	for _, r := range rows {
		b.WriteString(fmt.Sprintf("| %s | %d | %d | %d | %.2f | %s | %s | %s |\n",
			r.Name, r.Total, r.OK, r.Fail, r.RPS,
			r.P50.Round(time.Millisecond), r.P95.Round(time.Millisecond), r.Max.Round(time.Millisecond)))
	}
	return b.String()
}

func postMemory(client *http.Client, baseURL, apiKey, projectID, memType, epistemic, topicKey, summary, content, extID string) (int, string, error) {
	return doJSON(client, "POST", baseURL+"/api/v1/memories", apiKey, map[string]any{
		"project_id":         projectID,
		"type":               memType,
		"scope_type":         "project_shared",
		"topic_key":          topicKey,
		"summary":            summary,
		"content":            content,
		"tags":               []string{"qa", "daily-perf"},
		"epistemic_status":   epistemic,
		"importance":         0.85,
		"source_type":        "agent_execution",
		"external_source_id": extID,
	})
}

func getStatus(client *http.Client, baseURL, apiKey string) (systemStatus, error) {
	code, body, err := doJSON(client, "GET", baseURL+"/api/v1/system/status", apiKey, nil)
	if err != nil {
		return systemStatus{}, err
	}
	if code < 200 || code >= 300 {
		return systemStatus{}, fmt.Errorf("HTTP %d: %s", code, body)
	}
	var st systemStatus
	if err := json.Unmarshal([]byte(body), &st); err != nil {
		return systemStatus{}, err
	}
	return st, nil
}

func checkHealth(client *http.Client, url string) bool {
	resp, err := client.Get(url)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func doJSON(client *http.Client, method, url, apiKey string, payload any) (int, string, error) {
	var bodyReader io.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return 0, "", err
		}
		bodyReader = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		return 0, "", err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return resp.StatusCode, string(raw), nil
}

func waitForEmbeddings(topicLike string, want int, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	q := fmt.Sprintf("SELECT count(*) FROM memories m JOIN memory_embeddings me ON me.memory_id = m.id WHERE m.topic_key LIKE '%s' AND m.status = 'active';", topicLike)
	for time.Now().Before(deadline) {
		out, err := runCmd("docker", "exec", "romero-postgres", "psql", "-U", "romero", "-d", "romero", "-t", "-A", "-c", q)
		if err == nil && strings.TrimSpace(out) == fmt.Sprintf("%d", want) {
			return
		}
		time.Sleep(1 * time.Second)
	}
}

func runCmd(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func readGPU() string {
	out, err := runCmd("nvidia-smi", "--query-gpu=temperature.gpu,utilization.gpu,memory.used,memory.total,power.draw", "--format=csv,noheader")
	if err != nil {
		return "nvidia-smi unavailable"
	}
	return out
}

func readDockerStats() string {
	out, err := runCmd("docker", "stats", "--no-stream", "romero-postgres", "romero-embeddings", "romero-reranker", "romero-nli", "romero-slm",
		"--format", "table {{.Name}}\t{{.CPUPerc}}\t{{.MemUsage}}\t{{.MemPerc}}")
	if err != nil {
		return "docker stats unavailable"
	}
	return out
}

func verdict(ok bool) string {
	if ok {
		return "PASS"
	}
	return "FAIL"
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
