package neural_test

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/romero-project/romero/internal/infrastructure/neural"
)

func TestBGEM3Client(t *testing.T) {
	t.Parallel()

	t.Run("disabled or empty url returns ErrProviderUnavailable", func(t *testing.T) {
		t.Parallel()
		c := neural.NewBGEM3Client(neural.BGEM3Config{Enabled: false, BaseURL: "http://127.0.0.1:8080"})
		if _, err := c.Embed(context.Background(), "hello"); !errors.Is(err, neural.ErrProviderUnavailable) {
			t.Fatalf("expected ErrProviderUnavailable, got %v", err)
		}
		if c.ModelName() != "BAAI/bge-m3" || c.Dimensions() != 1024 {
			t.Fatalf("unexpected defaults: model=%q dim=%d", c.ModelName(), c.Dimensions())
		}
	})

	t.Run("embeds and L2-normalizes 1024-dim response", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/embed" {
				http.NotFound(w, r)
				return
			}
			vec := make([]float32, 1024)
			vec[0] = 3.0
			vec[1] = 4.0
			_ = json.NewEncoder(w).Encode(map[string]any{
				"embeddings": [][]float32{vec},
			})
		}))
		defer srv.Close()

		c := neural.NewBGEM3Client(neural.BGEM3Config{
			Enabled:    true,
			BaseURL:    srv.URL,
			Dimensions: 1024,
			Timeout:    2 * time.Second,
		})
		out, err := c.Embed(context.Background(), "test query")
		if err != nil {
			t.Fatalf("Embed failed: %v", err)
		}
		if len(out.Dense) != 1024 {
			t.Fatalf("got %d dims, want 1024", len(out.Dense))
		}
		if math.Abs(float64(out.Dense[0]-0.6)) > 1e-5 || math.Abs(float64(out.Dense[1]-0.8)) > 1e-5 {
			t.Fatalf("expected L2-normalized [0.6, 0.8, ...], got [%v, %v]", out.Dense[0], out.Dense[1])
		}
	})

	t.Run("server 503 returns ErrProviderUnavailable", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		}))
		defer srv.Close()

		c := neural.NewBGEM3Client(neural.BGEM3Config{Enabled: true, BaseURL: srv.URL, Dimensions: 1024})
		if _, err := c.Embed(context.Background(), "q"); !errors.Is(err, neural.ErrProviderUnavailable) {
			t.Fatalf("expected ErrProviderUnavailable on 503, got %v", err)
		}
	})

	t.Run("supports openai data format, raw matrix, empty batch, and rejects bad dim or zero norm", func(t *testing.T) {
		t.Parallel()
		vec := make([]float32, 1024)
		vec[0] = 2.0

		srvData := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{{"embedding": vec}},
			})
		}))
		defer srvData.Close()
		c1 := neural.NewBGEM3Client(neural.BGEM3Config{Enabled: true, BaseURL: srvData.URL})
		if outs, err := c1.EmbedBatch(context.Background(), nil); err != nil || outs != nil {
			t.Fatalf("empty batch got (%v, %v), want (nil, nil)", outs, err)
		}
		out, err := c1.Embed(context.Background(), "ok")
		if err != nil || math.Abs(float64(out.Dense[0]-1.0)) > 1e-5 {
			t.Fatalf("data format Embed failed: out=%v err=%v", len(out.Dense), err)
		}

		srvMatrix := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode([][]float32{vec})
		}))
		defer srvMatrix.Close()
		c2 := neural.NewBGEM3Client(neural.BGEM3Config{Enabled: true, BaseURL: srvMatrix.URL})
		if outs, err := c2.EmbedBatch(context.Background(), []string{"ok"}); err != nil || len(outs) != 1 {
			t.Fatalf("matrix format EmbedBatch failed: len=%d err=%v", len(outs), err)
		}

		srvBadDim := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode([][]float32{{1, 2}})
		}))
		defer srvBadDim.Close()
		c3 := neural.NewBGEM3Client(neural.BGEM3Config{Enabled: true, BaseURL: srvBadDim.URL})
		if _, err := c3.Embed(context.Background(), "bad"); !errors.Is(err, neural.ErrProviderUnavailable) {
			t.Fatalf("expected ErrProviderUnavailable for wrong dim, got %v", err)
		}

		srvZero := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode([][]float32{make([]float32, 1024)})
		}))
		defer srvZero.Close()
		c4 := neural.NewBGEM3Client(neural.BGEM3Config{Enabled: true, BaseURL: srvZero.URL})
		if _, err := c4.Embed(context.Background(), "zero"); !errors.Is(err, neural.ErrProviderUnavailable) {
			t.Fatalf("expected ErrProviderUnavailable for zero norm, got %v", err)
		}
	})
}
