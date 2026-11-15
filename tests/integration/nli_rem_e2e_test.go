package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/romero-project/romero/internal/application/capture"
	"github.com/romero-project/romero/internal/application/conflicts"
	"github.com/romero-project/romero/internal/application/consolidation"
	"github.com/romero-project/romero/internal/application/jobs"
	"github.com/romero-project/romero/internal/domain/governor"
	"github.com/romero-project/romero/internal/domain/identity"
	"github.com/romero-project/romero/internal/domain/memory"
	"github.com/romero-project/romero/internal/domain/provenance"
	"github.com/romero-project/romero/internal/infrastructure/neural"
	"github.com/romero-project/romero/internal/interfaces/rest"
	"github.com/romero-project/romero/tests/internal/testpg"
)

type pgIdentityTaskAccess struct {
	env *testpg.Env
}

func (a pgIdentityTaskAccess) HasTaskPermission(ctx context.Context, q identity.TaskPermissionQuery) (bool, error) {
	snap, err := a.env.Store.TaskPermissions().EvaluateTaskAccess(ctx, q)
	if err != nil {
		return false, err
	}
	if q.Permission == identity.TaskWrite {
		return snap.CanWrite, nil
	}
	return snap.CanRead, nil
}

func seedPhase03ProjectTenant(t *testing.T, env *testpg.Env, slug string) (tenantID, projectID, agentID uuid.UUID, apiKey string) {
	t.Helper()
	out, err := env.RunCLI(t,
		"bootstrap",
		"--dsn", testpg.TestDSN(),
		"--tenant-slug", slug,
		"--tenant-name", "Tenant "+slug,
		"--project-slug", "proj-"+slug,
		"--project-name", "Proj "+slug,
		"--principal-name", "admin-"+slug,
		"--principal-type", "orchestrator",
		"--scopes", "project_shared,org_shared,agent_private,task_shared",
		"--admin",
	)
	if err != nil {
		t.Fatalf("bootstrap %s: %v\n%s", slug, err, out)
	}
	var boot struct {
		Tenant struct {
			ID uuid.UUID `json:"id"`
		} `json:"tenant"`
		Project struct {
			ID uuid.UUID `json:"id"`
		} `json:"project"`
		APIKey string `json:"api_key"`
	}
	if err := json.Unmarshal([]byte(out), &boot); err != nil {
		t.Fatalf("unmarshal bootstrap: %v", err)
	}

	agID := uuid.New()
	if _, err := env.Pool.Exec(context.Background(),
		`INSERT INTO agents (id, tenant_id, external_key, name, role_type) VALUES ($1, $2, $3, $4, 'worker')`,
		agID, boot.Tenant.ID, "ext-"+slug, "agent-"+slug,
	); err != nil {
		t.Fatalf("insert agent: %v", err)
	}

	return boot.Tenant.ID, boot.Project.ID, agID, boot.APIKey
}

func doPhase03REST(t *testing.T, handler http.Handler, method, path, apiKey string, body any) (*http.Response, []byte) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec.Result(), rec.Body.Bytes()
}

func TestPhase03_NLIDetectionAndConflictArbitration_E2E(t *testing.T) {
	pool := testpg.OpenMigratedPool(t)
	env := testpg.NewEnv(t, pool)
	ctx := context.Background()

	tenantID, projectID, _, apiKey := seedPhase03ProjectTenant(t, env, "p3-nli-"+uuid.NewString()[:6])
	caller, err := env.Auth.Authenticate(ctx, apiKey)
	if err != nil {
		t.Fatalf("auth admin: %v", err)
	}

	// Mock NLI server returning contradiction=0.92 for all pairs.
	nliSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Pairs []struct {
				Premise    string `json:"premise"`
				Hypothesis string `json:"hypothesis"`
			} `json:"pairs"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		scores := make([]map[string]float32, len(req.Pairs))
		for i := range req.Pairs {
			scores[i] = map[string]float32{
				"entailment":    0.03,
				"neutral":       0.05,
				"contradiction": 0.92,
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"scores": scores})
	}))
	defer nliSrv.Close()

	nliClient := neural.NewDeBERTaNLIClient(neural.NLIConfig{BaseURL: nliSrv.URL, Enabled: true})
	embedder := &deterministicEmbedder{}
	embWorker := jobs.NewEmbeddingWorker(
		env.Store.JobQueue(),
		env.Store.Search(),
		env.Store.Embeddings(),
		embedder,
		env.L1Graph,
		jobs.EmbeddingWorkerConfig{BatchSize: 10},
		nil,
	).WithNLI(env.Store, env.Store.SemanticNeighbors(), nliClient)

	conflictSvc, err := conflicts.NewService(
		env.Store,
		env.Store.Conflicts(),
		env.Store.Audit(),
		pgIdentityTaskAccess{env: env},
		env.L1Graph,
		nil,
	)
	if err != nil {
		t.Fatalf("conflicts.NewService: %v", err)
	}

	gov := governor.NewPowerGovernor(governor.Config{
		Profile:      governor.ProfileBalanced,
		NLIAvailable: true,
	})
	handler, _, err := rest.NewRouter(rest.RouterDeps{
		Auth:      env.Auth,
		Capture:   env.CapSvc,
		Retrieval: env.RetSvc,
		Context:   env.CtxSvc,
		Feedback:  env.FbSvc,
		Conflicts: conflictSvc,
		Governor:  gov,
		DB:        pool,
		L1Stats:   env.L1Graph,
		Version:   "0.3.0-e2e",
	})
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	// Capture M1 and M2 in same project_shared scope with overlapping pgvector-bucket text.
	res1, err := env.CapSvc.CaptureMemory(ctx, caller, capture.CaptureCommand{
		ProjectID:        &projectID,
		Type:             memory.TypeSemantic,
		ScopeType:        identity.ScopeProjectShared,
		TopicKey:         "db/pool-mode-old",
		Summary:          "Postgres pgbouncer uses session pooling mode",
		Content:          "Postgres pgbouncer uses session pooling mode for all services",
		EpistemicStatus:  memory.EpistemicHypothesis,
		Importance:       0.7,
		SourceType:       provenance.SourceAgentExecution,
		ExternalSourceID: "nli-m1",
	})
	if err != nil {
		t.Fatalf("capture M1: %v", err)
	}
	m1ID := res1.Memory.ID

	res2, err := env.CapSvc.CaptureMemory(ctx, caller, capture.CaptureCommand{
		ProjectID:        &projectID,
		Type:             memory.TypeSemantic,
		ScopeType:        identity.ScopeProjectShared,
		TopicKey:         "db/pool-mode-new",
		Summary:          "Postgres pgbouncer uses transaction pooling mode",
		Content:          "Postgres pgbouncer uses transaction pooling mode, never session mode",
		EpistemicStatus:  memory.EpistemicHypothesis,
		Importance:       0.9,
		SourceType:       provenance.SourceAgentExecution,
		ExternalSourceID: "nli-m2",
	})
	if err != nil {
		t.Fatalf("capture M2: %v", err)
	}
	m2ID := res2.Memory.ID

	// Process both embedding + NLI jobs.
	for i := 0; i < 4; i++ {
		n, err := embWorker.ProcessOnce(ctx)
		if err != nil {
			t.Fatalf("ProcessOnce(%d): %v", i, err)
		}
		if n == 0 {
			break
		}
	}

	// List active conflicts via REST.
	resp, body := doPhase03REST(t, handler, http.MethodGet, "/api/v1/conflicts?project_id="+projectID.String(), apiKey, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/v1/conflicts status=%d body=%s", resp.StatusCode, string(body))
	}
	var listOut struct {
		Conflicts []struct {
			ConflictID uuid.UUID `json:"conflict_id"`
			IsActive   bool      `json:"is_active"`
		} `json:"conflicts"`
	}
	if err := json.Unmarshal(body, &listOut); err != nil {
		t.Fatalf("unmarshal conflicts: %v", err)
	}
	if len(listOut.Conflicts) != 1 || !listOut.Conflicts[0].IsActive {
		t.Fatalf("expected 1 active conflict, got %+v", listOut.Conflicts)
	}
	conflictID := listOut.Conflicts[0].ConflictID

	// Resolve conflict in favor of M2 via REST.
	resp, body = doPhase03REST(t, handler, http.MethodPost, "/api/v1/conflicts/"+conflictID.String()+"/resolve", apiKey, map[string]any{
		"winner_memory_id": m2ID,
		"reason":           "confirmed transaction pooling",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST resolve status=%d body=%s", resp.StatusCode, string(body))
	}

	// Verify atomic DB state: M1 superseded, supersession edge to M2, M2 verified, conflict inactive.
	var m1Status string
	if err := pool.QueryRow(ctx, `SELECT status FROM memories WHERE tenant_id = $1 AND id = $2`, tenantID, m1ID).Scan(&m1Status); err != nil {
		t.Fatalf("query M1: %v", err)
	}
	if m1Status != string(memory.StatusSuperseded) {
		t.Fatalf("expected M1 superseded, got status=%s", m1Status)
	}
	var supersedesCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM memory_relations WHERE tenant_id = $1 AND from_memory_id = $2 AND to_memory_id = $3 AND relation_type = 'supersedes' AND is_active = true`, tenantID, m2ID, m1ID).Scan(&supersedesCount); err != nil {
		t.Fatalf("query supersedes edge: %v", err)
	}
	if supersedesCount != 1 {
		t.Fatalf("expected 1 active supersedes edge %s -> %s, got %d", m2ID, m1ID, supersedesCount)
	}

	var m2Alpha float32
	if err := pool.QueryRow(ctx, `SELECT alpha_score FROM memories WHERE tenant_id = $1 AND id = $2`, tenantID, m2ID).Scan(&m2Alpha); err != nil {
		t.Fatalf("query M2: %v", err)
	}
	if m2Alpha <= 1.5 {
		t.Fatalf("expected M2 boosted alpha_score > 1.5, got alpha_score=%v", m2Alpha)
	}

	var activeConflicts int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM memory_relations WHERE tenant_id = $1 AND relation_type = 'contradicts' AND is_active = true`, tenantID).Scan(&activeConflicts); err != nil {
		t.Fatalf("count active conflicts: %v", err)
	}
	if activeConflicts != 0 {
		t.Fatalf("expected 0 active conflicts after resolve, got %d", activeConflicts)
	}

	// Capture M3 -> NLI conflict with M2 -> Archive M3 -> orphan conflict cleaned up automatically.
	res3, err := env.CapSvc.CaptureMemory(ctx, caller, capture.CaptureCommand{
		ProjectID:        &projectID,
		Type:             memory.TypeSemantic,
		ScopeType:        identity.ScopeProjectShared,
		TopicKey:         "db/pool-mode-m3",
		Summary:          "Postgres pgbouncer uses statement pooling mode",
		Content:          "Postgres pgbouncer uses statement pooling mode everywhere",
		EpistemicStatus:  memory.EpistemicHypothesis,
		Importance:       0.6,
		SourceType:       provenance.SourceAgentExecution,
		ExternalSourceID: "nli-m3",
	})
	if err != nil {
		t.Fatalf("capture M3: %v", err)
	}
	for i := 0; i < 4; i++ {
		n, err := embWorker.ProcessOnce(ctx)
		if err != nil || n == 0 {
			break
		}
	}
	env.Advance(time.Minute)
	if _, err := env.CapSvc.ArchiveMemory(ctx, caller, res3.Memory.ID, capture.ArchiveCommand{Reason: "obsolete"}); err != nil {
		t.Fatalf("archive M3: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM memory_relations WHERE tenant_id = $1 AND relation_type = 'contradicts' AND is_active = true`, tenantID).Scan(&activeConflicts); err != nil {
		t.Fatalf("count active conflicts after archive: %v", err)
	}
	if activeConflicts != 0 {
		t.Fatalf("expected orphan conflict to be deactivated on archive, got %d", activeConflicts)
	}
}

func TestPhase03_REMCycleAndPreemption_E2E(t *testing.T) {
	pool := testpg.OpenMigratedPool(t)
	env := testpg.NewEnv(t, pool)
	ctx := context.Background()

	tenantID, projectID, _, apiKey := seedPhase03ProjectTenant(t, env, "p3-rem-"+uuid.NewString()[:6])
	caller, err := env.Auth.Authenticate(ctx, apiKey)
	if err != nil {
		t.Fatalf("auth admin: %v", err)
	}

	// Seed 3 active episodic memories with high confidence and identical unit embeddings.
	epVec := make([]float32, 1024)
	epVec[0] = 0.8
	epVec[1] = 0.6
	for i := 0; i < 3; i++ {
		res, err := env.CapSvc.CaptureMemory(ctx, caller, capture.CaptureCommand{
			ProjectID:        &projectID,
			Type:             memory.TypeEpisodic,
			ScopeType:        identity.ScopeProjectShared,
			TopicKey:         "ops/runbook-" + string(rune('a'+i)),
			Summary:          "Runbook incident recovery step",
			Content:          "Verify primary postgres healthcheck before promoting replica",
			EpistemicStatus:  memory.EpistemicVerified,
			Importance:       0.8,
			SourceType:       provenance.SourceAgentExecution,
			ExternalSourceID: "rem-ep-" + string(rune('a'+i)),
		})
		if err != nil {
			t.Fatalf("capture episode %d: %v", i, err)
		}
		if err := env.Store.Embeddings().UpsertMemoryEmbedding(ctx, memory.MemoryEmbedding{
			MemoryID:  res.Memory.ID,
			ModelName: "test-model",
			DenseVec:  epVec,
			UpdatedAt: time.Now().UTC(),
		}); err != nil {
			t.Fatalf("upsert embedding %d: %v", i, err)
		}
		if _, err := pool.Exec(ctx, `UPDATE memories SET alpha_score = 9.0, beta_score = 1.0 WHERE id = $1`, res.Memory.ID); err != nil {
			t.Fatalf("boost alpha %d: %v", i, err)
		}
	}

	var slowMode atomic.Bool
	slowMode.Store(true)
	var unloadCalls atomic.Int32

	slmSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/unload" {
			unloadCalls.Add(1)
			w.WriteHeader(http.StatusOK)
			return
		}
		if slowMode.Load() {
			select {
			case <-r.Context().Done():
				http.Error(w, "preempted", http.StatusRequestTimeout)
				return
			case <-time.After(250 * time.Millisecond):
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"memory_type": "procedural",
			"topic_key":   "ops/runbook-consolidated",
			"summary":     "Runbook incident recovery procedure",
			"content":     "Always verify primary postgres healthcheck before promoting replica",
			"tags":        []string{"ops", "runbook"},
		})
	}))
	defer slmSrv.Close()

	nliSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Pairs []struct{} `json:"pairs"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		scores := make([]map[string]float32, len(req.Pairs))
		for i := range req.Pairs {
			scores[i] = map[string]float32{"entailment": 0.92, "neutral": 0.06, "contradiction": 0.02}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"scores": scores})
	}))
	defer nliSrv.Close()

	slmClient := neural.NewQwenSLMClient(neural.SLMConfig{BaseURL: slmSrv.URL, Enabled: true, Timeout: 5 * time.Second})
	nliClient := neural.NewDeBERTaNLIClient(neural.NLIConfig{BaseURL: nliSrv.URL, Enabled: true})
	gov := governor.NewPowerGovernor(governor.Config{
		Profile:      governor.ProfileBalanced,
		BurstWindow:  80 * time.Millisecond,
		OnUnloadVRAM: slmClient.UnloadVRAM,
		NLIAvailable: true,
		SLMAvailable: true,
	})

	conflictSvc, err := conflicts.NewService(env.Store, env.Store.Conflicts(), env.Store.Audit(), pgIdentityTaskAccess{env: env}, env.L1Graph, nil)
	if err != nil {
		t.Fatalf("conflicts.NewService: %v", err)
	}
	consSvc, err := consolidation.NewService(env.Store, env.Store.Consolidation(), slmClient, nliClient, env.Store.Audit(), env.L1Graph, gov, nil)
	if err != nil {
		t.Fatalf("consolidation.NewService: %v", err)
	}

	handler, _, err := rest.NewRouter(rest.RouterDeps{
		Auth:          env.Auth,
		Capture:       env.CapSvc,
		Retrieval:     env.RetSvc,
		Context:       env.CtxSvc,
		Feedback:      env.FbSvc,
		Conflicts:     conflictSvc,
		Consolidation: consSvc,
		Governor:      gov,
		DB:            pool,
		L1Stats:       env.L1Graph,
		Version:       "0.3.0-e2e",
	})
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	// Part A: Start slow REM consolidation, preempt it via an agent REST request -> 0 episodes consolidated.
	remDone := make(chan *consolidation.ConsolidateResult, 1)
	go func() {
		res, _ := consSvc.Consolidate(ctx, caller, consolidation.ConsolidateCommand{ProjectID: &projectID})
		remDone <- res
	}()

	time.Sleep(25 * time.Millisecond)
	preemptStart := time.Now()
	resp, _ := doPhase03REST(t, handler, http.MethodGet, "/api/v1/conflicts", apiKey, nil)
	preemptElapsed := time.Since(preemptStart)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("preempting request status = %d, want 200", resp.StatusCode)
	}
	if preemptElapsed > 100*time.Millisecond {
		t.Fatalf("agent request blocked by REM: took %v", preemptElapsed)
	}

	preemptedRes := <-remDone
	if preemptedRes == nil || !preemptedRes.Preempted || preemptedRes.ConsolidatedEpisodes != 0 {
		t.Fatalf("expected preempted REM result with 0 episodes, got %+v", preemptedRes)
	}

	// Part B: Disable slow mode, wait for BurstWindow to pass, run POST /api/v1/admin/consolidate.
	slowMode.Store(false)
	time.Sleep(110 * time.Millisecond)

	resp, body := doPhase03REST(t, handler, http.MethodPost, "/api/v1/admin/consolidate", apiKey, map[string]any{
		"project_id": projectID,
		"dry_run":    false,
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /api/v1/admin/consolidate status=%d body=%s", resp.StatusCode, string(body))
	}
	var consOut struct {
		ClustersFound        int         `json:"clusters_found"`
		DistilledCount       int         `json:"distilled_count"`
		ConsolidatedEpisodes int         `json:"consolidated_episodes"`
		CreatedMemoryIDs     []uuid.UUID `json:"created_memory_ids"`
	}
	if err := json.Unmarshal(body, &consOut); err != nil {
		t.Fatalf("unmarshal consolidate: %v", err)
	}
	if consOut.ClustersFound != 1 || consOut.DistilledCount != 1 || consOut.ConsolidatedEpisodes != 3 || len(consOut.CreatedMemoryIDs) != 1 {
		t.Fatalf("unexpected consolidation output: %+v", consOut)
	}

	var consolidatedCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM memories WHERE tenant_id = $1 AND memory_type = 'episodic' AND status = 'consolidated'`, tenantID).Scan(&consolidatedCount); err != nil {
		t.Fatalf("count consolidated episodes: %v", err)
	}
	if consolidatedCount != 3 {
		t.Fatalf("expected 3 consolidated episodes in PG17, got %d", consolidatedCount)
	}
}
