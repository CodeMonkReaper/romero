package neural_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/romero-project/romero/internal/domain/memory"
	"github.com/romero-project/romero/internal/infrastructure/neural"
)

func TestRerankerClientAndDeterministic(t *testing.T) {
	ctx := context.Background()
	id1, id2 := uuid.New(), uuid.New()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rerank" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]any{
				{"index": 0, "score": 2.4},
				{"index": 1, "score": -1.2},
			},
		})
	}))
	defer srv.Close()

	client := neural.NewRerankerClient(neural.RerankerConfig{BaseURL: srv.URL, Enabled: true})
	scores, err := client.Rerank(ctx, "postgres port", []memory.RerankCandidate{
		{MemoryID: id1, Text: "postgres listens on 5432"},
		{MemoryID: id2, Text: "redis runs on 6379"},
	})
	if err != nil {
		t.Fatalf("Rerank failed: %v", err)
	}
	if scores[id1] != 2.4 || scores[id2] != -1.2 {
		t.Fatalf("unexpected scores: %+v", scores)
	}

	disabled := neural.NewRerankerClient(neural.RerankerConfig{Enabled: false})
	if _, err := disabled.Rerank(ctx, "q", []memory.RerankCandidate{{MemoryID: id1, Text: "x"}}); !errors.Is(err, neural.ErrProviderUnavailable) {
		t.Fatalf("expected ErrProviderUnavailable, got %v", err)
	}

	det := neural.DeterministicReranker{}
	detScores, err := det.Rerank(ctx, "postgres port", []memory.RerankCandidate{
		{MemoryID: id1, Text: "postgres port 5432"},
		{MemoryID: id2, Text: "unrelated redis"},
	})
	if err != nil || detScores[id1] <= detScores[id2] {
		t.Fatalf("expected deterministic reranker id1 > id2, got %+v (err=%v)", detScores, err)
	}
}

func TestRerankerClientTEIArrayResponse(t *testing.T) {
	ctx := context.Background()
	id1, id2 := uuid.New(), uuid.New()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rerank" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"index": 1, "score": 0.9394706},
			{"index": 0, "score": 0.00001631454},
		})
	}))
	defer srv.Close()

	client := neural.NewRerankerClient(neural.RerankerConfig{BaseURL: srv.URL, Enabled: true})
	scores, err := client.Rerank(ctx, "GPU ultra profile", []memory.RerankCandidate{
		{MemoryID: id1, Text: "unrelated database text"},
		{MemoryID: id2, Text: "ROMERO cognitive memory GPU ultra profile"},
	})
	if err != nil {
		t.Fatalf("Rerank TEI array response failed: %v", err)
	}
	if scores[id2] < 0.93 || scores[id1] > 0.001 {
		t.Fatalf("unexpected TEI array scores: %+v", scores)
	}
}

func TestDeBERTaNLIClientAndDeterministic(t *testing.T) {
	ctx := context.Background()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/nli" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"scores": []map[string]any{
				{"entailment": 0.91, "neutral": 0.07, "contradiction": 0.02},
			},
		})
	}))
	defer srv.Close()

	client := neural.NewDeBERTaNLIClient(neural.NLIConfig{BaseURL: srv.URL, Enabled: true})
	got, err := client.EvaluatePairs(ctx, []memory.NLIPairInput{{Premise: "a", Hypothesis: "b"}})
	if err != nil || len(got) != 1 || got[0].Entailment != 0.91 {
		t.Fatalf("EvaluatePairs unexpected: %+v err=%v", got, err)
	}

	det := neural.DeterministicNLIProvider{}
	detScores, err := det.EvaluatePairs(ctx, []memory.NLIPairInput{
		{Premise: "Postgres port is 5432", Hypothesis: "Postgres port is 5433"},
		{Premise: "Always run sqlc generate", Hypothesis: "Always run sqlc generate"},
	})
	if err != nil {
		t.Fatalf("deterministic NLI failed: %v", err)
	}
	if detScores[0].Contradiction < 0.80 || detScores[1].Entailment < 0.85 {
		t.Fatalf("unexpected deterministic NLI scores: %+v", detScores)
	}
}

func TestQwenSLMClientAndDeterministic(t *testing.T) {
	ctx := context.Background()
	unloaded := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/consolidate":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"memory_type": "procedural",
				"topic_key":   "deploy.steps",
				"summary":     "Run migrations before restart",
				"content":     "1. Run goose up. 2. Restart API.",
				"tags":        []string{"deploy"},
				"entities":    []string{"goose"},
			})
		case "/unload":
			unloaded = true
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	client := neural.NewQwenSLMClient(neural.SLMConfig{BaseURL: srv.URL, Enabled: true})
	draft, err := client.DistillCluster(ctx, []memory.Memory{{ID: uuid.New(), Summary: "ep1", Content: "ep1"}})
	if err != nil || draft.MemoryType != memory.TypeProcedural || draft.TopicKey != "deploy.steps" {
		t.Fatalf("DistillCluster unexpected: %+v err=%v", draft, err)
	}
	if err := client.UnloadVRAM(ctx); err != nil || !unloaded {
		t.Fatalf("UnloadVRAM failed: err=%v unloaded=%v", err, unloaded)
	}

	det := &neural.DeterministicSLMConsolidator{}
	detDraft, err := det.DistillCluster(ctx, []memory.Memory{
		{ID: uuid.New(), Summary: "Run tests", Content: "Run tests", Tags: []string{"ci"}},
	})
	if err != nil || detDraft.MemoryType != memory.TypeProcedural {
		t.Fatalf("deterministic DistillCluster failed: %+v err=%v", detDraft, err)
	}
	if err := det.UnloadVRAM(ctx); err != nil || det.UnloadCalls.Load() != 1 {
		t.Fatalf("deterministic UnloadVRAM failed: err=%v calls=%d", err, det.UnloadCalls.Load())
	}
}

func TestQwenSLMClientOllamaProtocol(t *testing.T) {
	ctx := context.Background()
	var sawDistill, sawUnload bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/generate" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if ka, ok := req["keep_alive"].(float64); ok && ka == 0 {
			sawUnload = true
			_ = json.NewEncoder(w).Encode(map[string]any{"done": true})
			return
		}
		if req["format"] != "json" || req["stream"] != false {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		sawDistill = true
		inner, _ := json.Marshal(map[string]any{
			"memory_type": "semantic",
			"topic_key":   "gpu.ollama",
			"summary":     "Ollama unloads Qwen2.5-3B immediately with keep_alive=0",
			"content":     "Distilled knowledge from episodic memories.",
			"tags":        []string{"ollama", "rem"},
			"entities":    []string{"Qwen2.5-3B"},
		})
		_ = json.NewEncoder(w).Encode(map[string]any{"response": string(inner)})
	}))
	defer srv.Close()

	client := neural.NewQwenSLMClient(neural.SLMConfig{
		BaseURL:   srv.URL,
		ModelName: "qwen2.5:3b-instruct-q4_K_M",
		Protocol:  neural.SLMProtocolOllama,
		Enabled:   true,
	})
	draft, err := client.DistillCluster(ctx, []memory.Memory{{ID: uuid.New(), Summary: "ep1", Content: "ep1"}})
	if err != nil {
		t.Fatalf("Ollama DistillCluster failed: %v", err)
	}
	if !sawDistill || draft.MemoryType != memory.TypeSemantic || draft.TopicKey != "gpu.ollama" {
		t.Fatalf("unexpected Ollama draft: sawDistill=%v draft=%+v", sawDistill, draft)
	}
	if err := client.UnloadVRAM(ctx); err != nil || !sawUnload {
		t.Fatalf("Ollama UnloadVRAM failed: err=%v sawUnload=%v", err, sawUnload)
	}
}
