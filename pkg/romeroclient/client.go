// Package romeroclient provides a standalone, typed Go HTTP client for the ROMERO REST API.
package romeroclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Option configures a Client instance.
type Option func(*Client)

// WithHTTPClient overrides the underlying *http.Client used for requests.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) {
		if hc != nil {
			c.httpClient = hc
		}
	}
}

// WithUserAgent overrides the User-Agent header sent on API requests.
func WithUserAgent(ua string) Option {
	return func(c *Client) {
		if strings.TrimSpace(ua) != "" {
			c.userAgent = strings.TrimSpace(ua)
		}
	}
}

// Client calls ROMERO's Zero-Trust HTTP REST API using an authenticated API key.
type Client struct {
	baseURL    string
	apiKey     string
	userAgent  string
	httpClient *http.Client
}

// New constructs a Client targeting baseURL (e.g. "http://127.0.0.1:8420") with an API key.
func New(baseURL, apiKey string, opts ...Option) *Client {
	c := &Client{
		baseURL:    strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		apiKey:     strings.TrimSpace(apiKey),
		userAgent:  "romeroclient-go/1.0",
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// CaptureMemory calls POST /api/v1/memories.
func (c *Client) CaptureMemory(ctx context.Context, req CaptureRequest) (*CaptureResponse, error) {
	var out CaptureResponse
	status, err := c.doJSON(ctx, http.MethodPost, "/api/v1/memories", req, &out)
	if err != nil {
		return nil, err
	}
	out.Accepted = status == http.StatusAccepted
	return &out, nil
}

// GetMemory calls GET /api/v1/memories/{id} with optional Point-in-Time parameters.
func (c *Client) GetMemory(ctx context.Context, id uuid.UUID, opts *GetMemoryOptions) (*GetMemoryResponse, error) {
	path := "/api/v1/memories/" + id.String()
	if opts != nil {
		q := url.Values{}
		if opts.AsOf != "" {
			q.Set("as_of", opts.AsOf)
		}
		if opts.ValidAt != "" {
			q.Set("valid_at", opts.ValidAt)
		}
		if opts.KnownAt != "" {
			q.Set("known_at", opts.KnownAt)
		}
		if encoded := q.Encode(); encoded != "" {
			path += "?" + encoded
		}
	}
	var out GetMemoryResponse
	if _, err := c.doJSON(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateMemory calls PATCH /api/v1/memories/{id}.
func (c *Client) UpdateMemory(ctx context.Context, id uuid.UUID, req UpdateMemoryRequest) (*MutationResponse, error) {
	var out MutationResponse
	status, err := c.doJSON(ctx, http.MethodPatch, "/api/v1/memories/"+id.String(), req, &out)
	if err != nil {
		return nil, err
	}
	out.Accepted = status == http.StatusAccepted
	return &out, nil
}

// SupersedeMemory calls POST /api/v1/memories/{id}/supersede.
func (c *Client) SupersedeMemory(ctx context.Context, id uuid.UUID, req SupersedeMemoryRequest) (*SupersedeMemoryResponse, error) {
	var out SupersedeMemoryResponse
	status, err := c.doJSON(ctx, http.MethodPost, "/api/v1/memories/"+id.String()+"/supersede", req, &out)
	if err != nil {
		return nil, err
	}
	out.Accepted = status == http.StatusAccepted
	return &out, nil
}

// ArchiveMemory calls POST /api/v1/memories/{id}/archive.
func (c *Client) ArchiveMemory(ctx context.Context, id uuid.UUID, req ArchiveMemoryRequest) (*MutationResponse, error) {
	var out MutationResponse
	status, err := c.doJSON(ctx, http.MethodPost, "/api/v1/memories/"+id.String()+"/archive", req, &out)
	if err != nil {
		return nil, err
	}
	out.Accepted = status == http.StatusAccepted
	return &out, nil
}

// RecordFeedback calls POST /api/v1/memories/feedback.
func (c *Client) RecordFeedback(ctx context.Context, req FeedbackRequest) (*FeedbackResponse, error) {
	var out FeedbackResponse
	status, err := c.doJSON(ctx, http.MethodPost, "/api/v1/memories/feedback", req, &out)
	if err != nil {
		return nil, err
	}
	out.Accepted = status == http.StatusAccepted
	return &out, nil
}

// GetRequestStatus calls GET /api/v1/memory-requests/{request_id}.
func (c *Client) GetRequestStatus(ctx context.Context, requestID uuid.UUID) (*RequestStatusResponse, error) {
	var out RequestStatusResponse
	if _, err := c.doJSON(ctx, http.MethodGet, "/api/v1/memory-requests/"+requestID.String(), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SearchMemories calls POST /api/v1/memories/search.
func (c *Client) SearchMemories(ctx context.Context, req SearchRequest) (*SearchResponse, error) {
	var out SearchResponse
	if _, err := c.doJSON(ctx, http.MethodPost, "/api/v1/memories/search", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// BuildContext calls POST /api/v1/memories/context.
func (c *Client) BuildContext(ctx context.Context, req BuildContextRequest) (*BuildContextResponse, error) {
	var out BuildContextResponse
	if _, err := c.doJSON(ctx, http.MethodPost, "/api/v1/memories/context", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetSubgraph calls GET /api/v1/memories/{id}/subgraph.
func (c *Client) GetSubgraph(ctx context.Context, id uuid.UUID, hops int) (*SubgraphResponse, error) {
	path := "/api/v1/memories/" + id.String() + "/subgraph"
	if hops > 0 {
		path += "?hops=" + strconv.Itoa(hops)
	}
	var out SubgraphResponse
	if _, err := c.doJSON(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListConflicts calls GET /api/v1/conflicts.
func (c *Client) ListConflicts(ctx context.Context, opts *ListConflictsOptions) ([]ConflictRecord, error) {
	path := "/api/v1/conflicts"
	if opts != nil {
		q := url.Values{}
		if opts.ProjectID != nil {
			q.Set("project_id", opts.ProjectID.String())
		}
		if opts.Limit > 0 {
			q.Set("limit", strconv.Itoa(opts.Limit))
		}
		if opts.Offset > 0 {
			q.Set("offset", strconv.Itoa(opts.Offset))
		}
		if encoded := q.Encode(); encoded != "" {
			path += "?" + encoded
		}
	}
	var wrapper struct {
		Conflicts []ConflictRecord `json:"conflicts"`
	}
	if _, err := c.doJSON(ctx, http.MethodGet, path, nil, &wrapper); err != nil {
		return nil, err
	}
	return wrapper.Conflicts, nil
}

// ResolveConflict calls POST /api/v1/conflicts/{id}/resolve.
func (c *Client) ResolveConflict(ctx context.Context, conflictID uuid.UUID, req ResolveConflictRequest) (*ConflictRecord, error) {
	var out ConflictRecord
	if _, err := c.doJSON(ctx, http.MethodPost, "/api/v1/conflicts/"+conflictID.String()+"/resolve", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Consolidate calls POST /api/v1/admin/consolidate.
func (c *Client) Consolidate(ctx context.Context, req ConsolidateRequest) (*ConsolidateResponse, error) {
	var out ConsolidateResponse
	if _, err := c.doJSON(ctx, http.MethodPost, "/api/v1/admin/consolidate", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SystemStatus calls GET /api/v1/system/status.
func (c *Client) SystemStatus(ctx context.Context) (*SystemStatusResponse, error) {
	var out SystemStatusResponse
	if _, err := c.doJSON(ctx, http.MethodGet, "/api/v1/system/status", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) doJSON(ctx context.Context, method, path string, reqBody any, out any) (int, error) {
	var bodyReader io.Reader
	if reqBody != nil {
		raw, err := json.Marshal(reqBody)
		if err != nil {
			return 0, fmt.Errorf("romeroclient: encode request: %w", err)
		}
		bodyReader = bytes.NewReader(raw)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bodyReader)
	if err != nil {
		return 0, fmt.Errorf("romeroclient: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("romeroclient: execute %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	const maxRespBytes = 4 << 20
	rawResp, err := io.ReadAll(io.LimitReader(resp.Body, maxRespBytes+1))
	if err != nil {
		return resp.StatusCode, fmt.Errorf("romeroclient: read response: %w", err)
	}
	if len(rawResp) > maxRespBytes {
		return resp.StatusCode, fmt.Errorf("romeroclient: response exceeds %d bytes", maxRespBytes)
	}
	if resp.StatusCode >= 400 {
		var problem struct {
			Title  string `json:"title"`
			Detail string `json:"detail"`
		}
		_ = json.Unmarshal(rawResp, &problem)
		return resp.StatusCode, &APIError{
			StatusCode: resp.StatusCode,
			Title:      problem.Title,
			Detail:     problem.Detail,
			RawBody:    string(rawResp),
		}
	}

	if out != nil && len(bytes.TrimSpace(rawResp)) > 0 {
		if err := json.Unmarshal(rawResp, out); err != nil {
			return resp.StatusCode, fmt.Errorf("romeroclient: decode response: %w", err)
		}
	}
	return resp.StatusCode, nil
}
