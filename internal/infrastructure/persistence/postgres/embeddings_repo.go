package postgres

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/romero-project/romero/internal/domain/memory"
	"github.com/romero-project/romero/internal/infrastructure/persistence/sqlcgen"
)

const expectedEmbeddingDim = 1024

type embeddingRepo struct {
	q *sqlcgen.Queries
}

// Embeddings returns a non-transactional EmbeddingRepository backed by the Store pool.
func (s *Store) Embeddings() memory.EmbeddingRepository {
	return &embeddingRepo{q: s.queries}
}

func (r *embeddingRepo) UpsertMemoryEmbedding(ctx context.Context, emb memory.MemoryEmbedding) error {
	if emb.MemoryID == uuid.Nil {
		return memory.ErrInvalidIdentifier
	}
	vecText, err := formatHalfVec(emb.DenseVec)
	if err != nil {
		return err
	}
	modelName := strings.TrimSpace(emb.ModelName)
	if modelName == "" {
		modelName = "BAAI/bge-m3"
	}
	updatedAt := emb.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = time.Now().UTC()
	}

	return r.q.UpsertMemoryEmbedding(ctx, sqlcgen.UpsertMemoryEmbeddingParams{
		MemoryID:     emb.MemoryID,
		ModelName:    modelName,
		DenseVecText: vecText,
		UpdatedAt:    updatedAt,
	})
}

func (r *embeddingRepo) GetMemoryEmbeddingsByIDs(ctx context.Context, tenantID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID][]float32, error) {
	if tenantID == uuid.Nil {
		return nil, memory.ErrInvalidIdentifier
	}
	if len(ids) == 0 {
		return map[uuid.UUID][]float32{}, nil
	}
	rows, err := r.q.GetMemoryEmbeddingsByIDs(ctx, sqlcgen.GetMemoryEmbeddingsByIDsParams{
		TenantID: tenantID,
		Ids:      ids,
	})
	if err != nil {
		return nil, err
	}
	out := make(map[uuid.UUID][]float32, len(rows))
	for _, row := range rows {
		vec, parseErr := parseHalfVec(row.DenseVecText)
		if parseErr != nil {
			return nil, parseErr
		}
		out[row.MemoryID] = vec
	}
	return out, nil
}

func formatHalfVec(vec []float32) (string, error) {
	if len(vec) != expectedEmbeddingDim {
		return "", fmt.Errorf("%w: expected %d embedding dimensions, got %d", ErrInvalidInput, expectedEmbeddingDim, len(vec))
	}
	var b strings.Builder
	b.Grow(len(vec)*8 + 2)
	b.WriteByte('[')
	for i, v := range vec {
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			return "", fmt.Errorf("%w: non-finite embedding value at index %d", ErrInvalidInput, i)
		}
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatFloat(float64(v), 'g', 6, 32))
	}
	b.WriteByte(']')
	return b.String(), nil
}

func parseHalfVec(raw string) ([]float32, error) {
	trimmed := strings.TrimSpace(raw)
	if len(trimmed) < 2 || trimmed[0] != '[' || trimmed[len(trimmed)-1] != ']' {
		return nil, fmt.Errorf("%w: invalid vector text format", ErrInvalidInput)
	}
	inner := strings.TrimSpace(trimmed[1 : len(trimmed)-1])
	if inner == "" {
		return nil, fmt.Errorf("%w: empty vector text", ErrInvalidInput)
	}
	parts := strings.Split(inner, ",")
	out := make([]float32, 0, len(parts))
	for _, p := range parts {
		f, err := strconv.ParseFloat(strings.TrimSpace(p), 32)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid vector float %q: %v", ErrInvalidInput, p, err)
		}
		out = append(out, float32(f))
	}
	return out, nil
}
