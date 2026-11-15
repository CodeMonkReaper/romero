// Package neural implements persistent HTTP clients for local neural inference daemons.
package neural

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/romero-project/romero/internal/domain/memory"
)

const (
	defaultBGEM3Model      = "BAAI/bge-m3"
	defaultBGEM3Dimensions = 1024
	defaultBGEM3Timeout    = 2 * time.Second
)

// ErrProviderUnavailable indicates the local embedding daemon is disabled, unreachable, or timed out.
var ErrProviderUnavailable = errors.New("embedding provider unavailable")

// BGEM3Config configures the persistent HTTP client for BGE-M3 embeddings.
type BGEM3Config struct {
	BaseURL    string
	ModelName  string
	Dimensions int
	Timeout    time.Duration
	Enabled    bool
}

// BGEM3Client implements memory.EmbeddingProvider over a persistent keep-alive HTTP pool.
type BGEM3Client struct {
	baseURL    string
	modelName  string
	dimensions int
	enabled    bool
	httpClient *http.Client
}

// NewBGEM3Client constructs a BGEM3Client with keep-alive pooling and DisableCompression=true.
func NewBGEM3Client(cfg BGEM3Config) *BGEM3Client {
	modelName := strings.TrimSpace(cfg.ModelName)
	if modelName == "" {
		modelName = defaultBGEM3Model
	}
	dims := cfg.Dimensions
	if dims <= 0 {
		dims = defaultBGEM3Dimensions
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultBGEM3Timeout
	}
	transport := &http.Transport{
		MaxIdleConns:        32,
		MaxIdleConnsPerHost: 16,
		IdleConnTimeout:     90 * time.Second,
		DisableCompression:  true,
	}
	return &BGEM3Client{
		baseURL:    strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/"),
		modelName:  modelName,
		dimensions: dims,
		enabled:    cfg.Enabled,
		httpClient: &http.Client{
			Timeout:   timeout,
			Transport: transport,
		},
	}
}

func (c *BGEM3Client) ModelName() string { return c.modelName }
func (c *BGEM3Client) Dimensions() int   { return c.dimensions }

func (c *BGEM3Client) Embed(ctx context.Context, text string) (memory.EmbeddingOutput, error) {
	outs, err := c.EmbedBatch(ctx, []string{text})
	if err != nil {
		return memory.EmbeddingOutput{}, err
	}
	if len(outs) != 1 {
		return memory.EmbeddingOutput{}, fmt.Errorf("%w: unexpected embedding count %d", ErrProviderUnavailable, len(outs))
	}
	return outs[0], nil
}

func (c *BGEM3Client) EmbedBatch(ctx context.Context, texts []string) ([]memory.EmbeddingOutput, error) {
	if !c.enabled || c.baseURL == "" {
		return nil, ErrProviderUnavailable
	}
	if len(texts) == 0 {
		return nil, nil
	}

	payload, err := json.Marshal(struct {
		Inputs []string `json:"inputs"`
	}{Inputs: texts})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/embed", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrProviderUnavailable, err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrProviderUnavailable, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 16*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("%w: read body: %v", ErrProviderUnavailable, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%w: status %d", ErrProviderUnavailable, resp.StatusCode)
	}

	vecs, err := parseEmbeddingsPayload(body)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrProviderUnavailable, err)
	}
	if len(vecs) != len(texts) {
		return nil, fmt.Errorf("%w: expected %d vectors, got %d", ErrProviderUnavailable, len(texts), len(vecs))
	}

	out := make([]memory.EmbeddingOutput, len(vecs))
	for i, raw := range vecs {
		if len(raw) != c.dimensions {
			return nil, fmt.Errorf("%w: expected %d dimensions, got %d", ErrProviderUnavailable, c.dimensions, len(raw))
		}
		normed, nErr := l2Normalize(raw)
		if nErr != nil {
			return nil, fmt.Errorf("%w: %v", ErrProviderUnavailable, nErr)
		}
		out[i] = memory.EmbeddingOutput{Dense: normed}
	}
	return out, nil
}

func parseEmbeddingsPayload(body []byte) ([][]float32, error) {
	var wrapped struct {
		Embeddings [][]float32 `json:"embeddings"`
		Data       []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &wrapped); err == nil {
		if len(wrapped.Embeddings) > 0 {
			return wrapped.Embeddings, nil
		}
		if len(wrapped.Data) > 0 {
			out := make([][]float32, len(wrapped.Data))
			for i, d := range wrapped.Data {
				out[i] = d.Embedding
			}
			return out, nil
		}
	}
	var matrix [][]float32
	if err := json.Unmarshal(body, &matrix); err == nil && len(matrix) > 0 {
		return matrix, nil
	}
	return nil, errors.New("unrecognized embedding response payload")
}

func l2Normalize(v []float32) ([]float32, error) {
	var sumSq float64
	for i, val := range v {
		f := float64(val)
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return nil, fmt.Errorf("non-finite value at index %d", i)
		}
		sumSq += f * f
	}
	if sumSq == 0 {
		return nil, errors.New("zero-norm embedding vector")
	}
	invNorm := 1.0 / math.Sqrt(sumSq)
	out := make([]float32, len(v))
	for i, val := range v {
		out[i] = float32(float64(val) * invNorm)
	}
	return out, nil
}
