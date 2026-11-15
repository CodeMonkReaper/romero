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

	"github.com/romero-project/romero/internal/domain/memory"
)

const (
	defaultNLIModel   = "MoritzLaurer/mDeBERTa-v3-base-mnli-xnli"
	defaultNLITimeout = 3 * time.Second
)

// NLIConfig configures the persistent HTTP client for NLI pair evaluation.
type NLIConfig struct {
	BaseURL   string
	ModelName string
	Timeout   time.Duration
	Enabled   bool
}

// DeBERTaNLIClient implements memory.NLIProvider over a persistent keep-alive HTTP pool.
type DeBERTaNLIClient struct {
	baseURL    string
	modelName  string
	enabled    bool
	httpClient *http.Client
}

// NewDeBERTaNLIClient constructs a DeBERTaNLIClient with keep-alive pooling and DisableCompression=true.
func NewDeBERTaNLIClient(cfg NLIConfig) *DeBERTaNLIClient {
	modelName := strings.TrimSpace(cfg.ModelName)
	if modelName == "" {
		modelName = defaultNLIModel
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultNLITimeout
	}
	transport := &http.Transport{
		MaxIdleConns:        32,
		MaxIdleConnsPerHost: 16,
		IdleConnTimeout:     90 * time.Second,
		DisableCompression:  true,
	}
	return &DeBERTaNLIClient{
		baseURL:   strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/"),
		modelName: modelName,
		enabled:   cfg.Enabled,
		httpClient: &http.Client{
			Timeout:   timeout,
			Transport: transport,
		},
	}
}

func (c *DeBERTaNLIClient) EvaluatePairs(ctx context.Context, pairs []memory.NLIPairInput) ([]memory.NLIScore, error) {
	if !c.enabled || c.baseURL == "" {
		return nil, ErrProviderUnavailable
	}
	if len(pairs) == 0 {
		return nil, nil
	}

	type wirePair struct {
		Premise    string `json:"premise"`
		Hypothesis string `json:"hypothesis"`
	}
	wirePairs := make([]wirePair, len(pairs))
	for i, p := range pairs {
		wirePairs[i] = wirePair{Premise: p.Premise, Hypothesis: p.Hypothesis}
	}
	payload, err := json.Marshal(struct {
		Model string     `json:"model"`
		Pairs []wirePair `json:"pairs"`
	}{
		Model: c.modelName,
		Pairs: wirePairs,
	})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/nli", bytes.NewReader(payload))
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
		Scores []struct {
			Entailment    float32 `json:"entailment"`
			Neutral       float32 `json:"neutral"`
			Contradiction float32 `json:"contradiction"`
		} `json:"scores"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("%w: decode nli response: %v", ErrProviderUnavailable, err)
	}
	if len(body.Scores) != len(pairs) {
		return nil, fmt.Errorf("%w: nli score count mismatch %d != %d", ErrProviderUnavailable, len(body.Scores), len(pairs))
	}

	out := make([]memory.NLIScore, len(body.Scores))
	for i, s := range body.Scores {
		out[i] = memory.NLIScore{
			Entailment:    s.Entailment,
			Neutral:       s.Neutral,
			Contradiction: s.Contradiction,
		}
	}
	return out, nil
}

// DeterministicNLIProvider provides a deterministic CPU NLI reference for romero eval.
type DeterministicNLIProvider struct{}

func (DeterministicNLIProvider) EvaluatePairs(_ context.Context, pairs []memory.NLIPairInput) ([]memory.NLIScore, error) {
	out := make([]memory.NLIScore, len(pairs))
	for i, pair := range pairs {
		out[i] = classifyPairDeterministic(pair.Premise, pair.Hypothesis)
	}
	return out, nil
}

func classifyPairDeterministic(premise, hypothesis string) memory.NLIScore {
	pTok := tokenizeLower(premise)
	hTok := tokenizeLower(hypothesis)
	if len(pTok) == 0 || len(hTok) == 0 {
		return memory.NLIScore{Neutral: 1.0}
	}

	// Check explicit negation/antonym or conflicting numeric tokens.
	if hasPolarityConflict(pTok, hTok) || hasNumericConflict(pTok, hTok) {
		return memory.NLIScore{Entailment: 0.03, Neutral: 0.05, Contradiction: 0.92}
	}

	shared := 0
	for tok := range hTok {
		if _, ok := pTok[tok]; ok {
			shared++
		}
	}
	overlap := float32(shared) / float32(len(hTok))
	if overlap >= 0.45 {
		return memory.NLIScore{Entailment: 0.90, Neutral: 0.08, Contradiction: 0.02}
	}
	return memory.NLIScore{Entailment: 0.15, Neutral: 0.75, Contradiction: 0.10}
}

func hasPolarityConflict(a, b map[string]struct{}) bool {
	negWords := []string{"not", "never", "no", "disabled", "disable", "forbid", "forbidden", "avoid", "false"}
	aNeg := false
	bNeg := false
	for _, w := range negWords {
		if _, ok := a[w]; ok {
			aNeg = true
		}
		if _, ok := b[w]; ok {
			bNeg = true
		}
	}
	return aNeg != bNeg
}

func hasNumericConflict(a, b map[string]struct{}) bool {
	aNums := extractNumbers(a)
	bNums := extractNumbers(b)
	if len(aNums) == 0 || len(bNums) == 0 {
		return false
	}
	for n := range aNums {
		if _, ok := bNums[n]; !ok {
			return true
		}
	}
	return false
}

func extractNumbers(tokens map[string]struct{}) map[string]struct{} {
	out := make(map[string]struct{})
	for tok := range tokens {
		isNum := true
		for _, r := range tok {
			if r < '0' || r > '9' {
				isNum = false
				break
			}
		}
		if isNum && len(tok) > 0 {
			out[tok] = struct{}{}
		}
	}
	return out
}
