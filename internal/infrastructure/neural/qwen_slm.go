package neural

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/romero-project/romero/internal/domain/memory"
)

const (
	defaultSLMModel   = "Qwen/Qwen2.5-3B-Instruct"
	defaultSLMTimeout = 15 * time.Second
)

// SLMConfig configures the persistent HTTP client for REM sleep SLM distillation.
type SLMConfig struct {
	BaseURL   string
	ModelName string
	Timeout   time.Duration
	Enabled   bool
}

// QwenSLMClient implements memory.SLMConsolidationProvider over a persistent HTTP pool.
type QwenSLMClient struct {
	baseURL    string
	modelName  string
	enabled    bool
	httpClient *http.Client
}

// NewQwenSLMClient constructs a QwenSLMClient with keep-alive pooling and DisableCompression=true.
func NewQwenSLMClient(cfg SLMConfig) *QwenSLMClient {
	modelName := strings.TrimSpace(cfg.ModelName)
	if modelName == "" {
		modelName = defaultSLMModel
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultSLMTimeout
	}
	return &QwenSLMClient{
		baseURL:    strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/"),
		modelName:  modelName,
		enabled:    cfg.Enabled,
		httpClient: newNeuralHTTPClient(timeout, 16, 8),
	}
}

func (c *QwenSLMClient) DistillCluster(ctx context.Context, episodes []memory.Memory) (memory.DistilledMemoryDraft, error) {
	if !c.enabled || c.baseURL == "" {
		return memory.DistilledMemoryDraft{}, ErrProviderUnavailable
	}
	if len(episodes) == 0 {
		return memory.DistilledMemoryDraft{}, fmt.Errorf("%w: empty episode cluster", ErrProviderUnavailable)
	}

	type wireEpisode struct {
		ID       string   `json:"id"`
		TopicKey string   `json:"topic_key,omitempty"`
		Summary  string   `json:"summary"`
		Content  string   `json:"content"`
		Tags     []string `json:"tags,omitempty"`
		Entities []string `json:"entities,omitempty"`
	}
	wireEps := make([]wireEpisode, len(episodes))
	for i, ep := range episodes {
		wireEps[i] = wireEpisode{
			ID:       ep.ID.String(),
			TopicKey: ep.TopicKey,
			Summary:  ep.Summary,
			Content:  ep.Content,
			Tags:     ep.Tags,
			Entities: ep.Entities,
		}
	}
	payload, err := json.Marshal(struct {
		Model    string        `json:"model"`
		Episodes []wireEpisode `json:"episodes"`
	}{
		Model:    c.modelName,
		Episodes: wireEps,
	})
	if err != nil {
		return memory.DistilledMemoryDraft{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/consolidate", bytes.NewReader(payload))
	if err != nil {
		return memory.DistilledMemoryDraft{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return memory.DistilledMemoryDraft{}, fmt.Errorf("%w: %v", ErrProviderUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, resp.Body)
		return memory.DistilledMemoryDraft{}, fmt.Errorf("%w: status %d", ErrProviderUnavailable, resp.StatusCode)
	}

	var body struct {
		MemoryType string   `json:"memory_type"`
		TopicKey   string   `json:"topic_key"`
		Summary    string   `json:"summary"`
		Content    string   `json:"content"`
		Tags       []string `json:"tags"`
		Entities   []string `json:"entities"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return memory.DistilledMemoryDraft{}, fmt.Errorf("%w: decode slm response: %v", ErrProviderUnavailable, err)
	}

	memType := memory.MemoryType(strings.TrimSpace(body.MemoryType))
	if memType != memory.TypeProcedural && memType != memory.TypeSemantic {
		memType = memory.TypeProcedural
	}
	summary := strings.TrimSpace(body.Summary)
	if len(summary) > 600 {
		summary = summary[:600]
	}
	return memory.DistilledMemoryDraft{
		MemoryType: memType,
		TopicKey:   strings.TrimSpace(body.TopicKey),
		Summary:    summary,
		Content:    strings.TrimSpace(body.Content),
		Tags:       body.Tags,
		Entities:   body.Entities,
	}, nil
}

func (c *QwenSLMClient) UnloadVRAM(ctx context.Context) error {
	if !c.enabled || c.baseURL == "" {
		return nil
	}
	payload, err := json.Marshal(struct {
		Model string `json:"model"`
	}{Model: c.modelName})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/unload", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrProviderUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%w: unload status %d", ErrProviderUnavailable, resp.StatusCode)
	}
	return nil
}

// DeterministicSLMConsolidator provides a deterministic CPU reference for romero eval.
type DeterministicSLMConsolidator struct {
	UnloadCalls atomic.Int64
}

func (d *DeterministicSLMConsolidator) DistillCluster(_ context.Context, episodes []memory.Memory) (memory.DistilledMemoryDraft, error) {
	if len(episodes) == 0 {
		return memory.DistilledMemoryDraft{}, fmt.Errorf("%w: empty cluster", ErrProviderUnavailable)
	}
	tagSet := make(map[string]struct{})
	entSet := make(map[string]struct{})
	summaries := make([]string, 0, len(episodes))
	for _, ep := range episodes {
		if s := strings.TrimSpace(ep.Summary); s != "" {
			summaries = append(summaries, s)
		}
		for _, t := range ep.Tags {
			tagSet[t] = struct{}{}
		}
		for _, e := range ep.Entities {
			entSet[e] = struct{}{}
		}
	}
	summary := ""
	if len(summaries) > 0 {
		summary = summaries[0]
	}
	if len(summary) > 600 {
		summary = summary[:600]
	}
	content := strings.Join(summaries, "\n")
	return memory.DistilledMemoryDraft{
		MemoryType: memory.TypeProcedural,
		TopicKey:   episodes[0].TopicKey,
		Summary:    summary,
		Content:    content,
		Tags:       sortedKeys(tagSet),
		Entities:   sortedKeys(entSet),
	}, nil
}

func (d *DeterministicSLMConsolidator) UnloadVRAM(_ context.Context) error {
	d.UnloadCalls.Add(1)
	return nil
}

func sortedKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
