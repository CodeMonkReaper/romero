package neural

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/romero-project/romero/internal/domain/memory"
)

const (
	defaultRerankerModel   = "BAAI/bge-reranker-v2-m3"
	defaultRerankerTimeout = 2 * time.Second
)

// RerankerConfig configures the persistent HTTP client for Cross-Encoder reranking.
type RerankerConfig struct {
	BaseURL   string
	ModelName string
	Timeout   time.Duration
	Enabled   bool
}

// RerankerClient implements memory.RerankerProvider over a persistent keep-alive HTTP pool.
type RerankerClient struct {
	baseURL    string
	modelName  string
	enabled    bool
	httpClient *http.Client
}

// NewRerankerClient constructs a RerankerClient with keep-alive pooling and DisableCompression=true.
func NewRerankerClient(cfg RerankerConfig) *RerankerClient {
	modelName := strings.TrimSpace(cfg.ModelName)
	if modelName == "" {
		modelName = defaultRerankerModel
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultRerankerTimeout
	}
	return &RerankerClient{
		baseURL:    strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/"),
		modelName:  modelName,
		enabled:    cfg.Enabled,
		httpClient: newNeuralHTTPClient(timeout, 32, 16),
	}
}

func (c *RerankerClient) Rerank(ctx context.Context, query string, candidates []memory.RerankCandidate) (map[uuid.UUID]float32, error) {
	if !c.enabled || c.baseURL == "" {
		return nil, ErrProviderUnavailable
	}
	if len(candidates) == 0 {
		return map[uuid.UUID]float32{}, nil
	}

	texts := make([]string, len(candidates))
	for i, cand := range candidates {
		texts[i] = cand.Text
	}
	payload, err := json.Marshal(struct {
		Model string   `json:"model"`
		Query string   `json:"query"`
		Texts []string `json:"texts"`
	}{
		Model: c.modelName,
		Query: query,
		Texts: texts,
	})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/rerank", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrProviderUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil, fmt.Errorf("%w: status %d", ErrProviderUnavailable, resp.StatusCode)
	}

	var body struct {
		Results []struct {
			Index int     `json:"index"`
			Score float32 `json:"score"`
		} `json:"results"`
		Scores []float32 `json:"scores"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("%w: decode rerank response: %v", ErrProviderUnavailable, err)
	}

	out := make(map[uuid.UUID]float32, len(candidates))
	if len(body.Results) > 0 {
		for _, r := range body.Results {
			if r.Index >= 0 && r.Index < len(candidates) {
				out[candidates[r.Index].MemoryID] = r.Score
			}
		}
		return out, nil
	}
	if len(body.Scores) != len(candidates) {
		return nil, fmt.Errorf("%w: score count mismatch %d != %d", ErrProviderUnavailable, len(body.Scores), len(candidates))
	}
	for i, cand := range candidates {
		out[cand.MemoryID] = body.Scores[i]
	}
	return out, nil
}

// DeterministicReranker provides a deterministic CPU reference implementation for romero eval.
type DeterministicReranker struct{}

func (DeterministicReranker) Rerank(_ context.Context, query string, candidates []memory.RerankCandidate) (map[uuid.UUID]float32, error) {
	qTokens := tokenizeLower(query)
	out := make(map[uuid.UUID]float32, len(candidates))
	for _, cand := range candidates {
		cTokens := tokenizeLower(cand.Text)
		if len(qTokens) == 0 || len(cTokens) == 0 {
			out[cand.MemoryID] = 0
			continue
		}
		matches := 0
		for tok := range qTokens {
			if _, ok := cTokens[tok]; ok {
				matches++
			}
		}
		ratio := float32(matches) / float32(len(qTokens))
		// Map ratio in [0, 1] to logit in [-4, +4].
		out[cand.MemoryID] = (ratio * 8.0) - 4.0
	}
	return out, nil
}

func tokenizeLower(s string) map[string]struct{} {
	fields := strings.Fields(strings.ToLower(s))
	set := make(map[string]struct{}, len(fields))
	for _, f := range fields {
		clean := strings.Trim(f, ".,;:!?\"'()[]{}")
		if clean != "" {
			set[clean] = struct{}{}
		}
	}
	return set
}
