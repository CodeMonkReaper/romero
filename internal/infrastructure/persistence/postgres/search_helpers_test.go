package postgres

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/romero-project/romero/internal/domain/identity"
	"github.com/romero-project/romero/internal/domain/memory"
)

func TestFormatAndParseHalfVec(t *testing.T) {
	t.Parallel()

	t.Run("round trip 1024 dimensions", func(t *testing.T) {
		t.Parallel()
		vec := make([]float32, 1024)
		vec[0] = 0.25
		vec[1] = -0.5
		vec[1023] = 0.75

		encoded, err := formatHalfVec(vec)
		if err != nil {
			t.Fatalf("formatHalfVec failed: %v", err)
		}
		decoded, err := parseHalfVec(encoded)
		if err != nil {
			t.Fatalf("parseHalfVec failed: %v", err)
		}
		if len(decoded) != 1024 || decoded[0] != 0.25 || decoded[1] != -0.5 || decoded[1023] != 0.75 {
			t.Fatalf("unexpected decoded vector: len=%d [0]=%v [1]=%v [1023]=%v", len(decoded), decoded[0], decoded[1], decoded[1023])
		}
	})

	t.Run("rejects wrong dimension and NaN", func(t *testing.T) {
		t.Parallel()
		if _, err := formatHalfVec([]float32{1, 2}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("expected ErrInvalidInput for wrong dimension, got %v", err)
		}
		bad := make([]float32, 1024)
		bad[10] = float32(math.NaN())
		if _, err := formatHalfVec(bad); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("expected ErrInvalidInput for NaN, got %v", err)
		}
		if _, err := parseHalfVec("invalid"); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("expected ErrInvalidInput for invalid text, got %v", err)
		}
		if _, err := parseHalfVec("[]"); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("expected ErrInvalidInput for empty text, got %v", err)
		}
	})
}

func TestRepositoryInputGuards(t *testing.T) {
	t.Parallel()

	s := &Store{}
	ctx := context.Background()

	if err := s.Embeddings().UpsertMemoryEmbedding(ctx, memory.MemoryEmbedding{}); !errors.Is(err, memory.ErrInvalidIdentifier) {
		t.Fatalf("expected ErrInvalidIdentifier, got %v", err)
	}
	if _, err := s.Embeddings().GetMemoryEmbeddingsByIDs(ctx, uuid.Nil, nil); !errors.Is(err, memory.ErrInvalidIdentifier) {
		t.Fatalf("expected ErrInvalidIdentifier, got %v", err)
	}
	if got, err := s.Embeddings().GetMemoryEmbeddingsByIDs(ctx, uuid.New(), nil); err != nil || len(got) != 0 {
		t.Fatalf("expected empty map for empty IDs, got len=%d err=%v", len(got), err)
	}

	if _, err := s.JobQueue().ClaimPendingMemoryJobs(ctx, "", 10, time.Now()); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for empty jobType, got %v", err)
	}
	if err := s.JobQueue().CompleteMemoryJob(ctx, uuid.Nil, time.Now()); !errors.Is(err, memory.ErrInvalidIdentifier) {
		t.Fatalf("expected ErrInvalidIdentifier for nil jobID, got %v", err)
	}
	if err := s.JobQueue().FailMemoryJob(ctx, uuid.Nil, "err", time.Now(), false); !errors.Is(err, memory.ErrInvalidIdentifier) {
		t.Fatalf("expected ErrInvalidIdentifier for nil jobID, got %v", err)
	}

	if _, err := s.Search().SearchByTrigram(ctx, memory.SearchFilter{}); !errors.Is(err, memory.ErrInvalidIdentifier) {
		t.Fatalf("expected ErrInvalidIdentifier, got %v", err)
	}
	if got, err := s.Search().SearchByTrigram(ctx, memory.SearchFilter{TenantID: uuid.New()}); err != nil || len(got) != 0 {
		t.Fatalf("expected empty slice for empty scopes, got len=%d err=%v", len(got), err)
	}
	if got, err := s.Search().SearchByFTS(ctx, memory.SearchFilter{TenantID: uuid.New(), Scopes: []identity.ScopeType{identity.ScopeProjectShared}, QueryText: "   "}); err != nil || len(got) != 0 {
		t.Fatalf("expected empty slice for empty query, got len=%d err=%v", len(got), err)
	}
	if got, err := s.Search().SearchByVector(ctx, memory.SearchFilter{TenantID: uuid.New()}, make([]float32, 1024)); err != nil || len(got) != 0 {
		t.Fatalf("expected empty slice for empty scopes in SearchByVector, got len=%d err=%v", len(got), err)
	}
	if _, err := s.Search().SearchByVector(ctx, memory.SearchFilter{TenantID: uuid.New(), Scopes: []identity.ScopeType{identity.ScopeProjectShared}}, []float32{1}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for bad vector dims, got %v", err)
	}
	if got, err := s.Search().TraverseCausalSubgraph(ctx, uuid.New(), nil, 2, time.Now()); err != nil || len(got) != 0 {
		t.Fatalf("expected empty walk for empty seeds, got len=%d err=%v", len(got), err)
	}
}
