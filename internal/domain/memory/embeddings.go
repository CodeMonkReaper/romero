package memory

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// EmbeddingOutput represents a dense and optional sparse embedding vector produced by an encoder.
type EmbeddingOutput struct {
	Dense      []float32
	SparseIdx  []int32
	SparseVals []float32
}

// MemoryEmbedding persists the vector representation of a memory record.
type MemoryEmbedding struct {
	MemoryID      uuid.UUID
	ModelName     string
	DenseVec      []float32
	SparseIndices []int32
	SparseValues  []float32
	UpdatedAt     time.Time
}

// EmbeddingProvider abstracts local or remote neural embedding encoders behind a domain port.
type EmbeddingProvider interface {
	ModelName() string
	Dimensions() int
	Embed(ctx context.Context, text string) (EmbeddingOutput, error)
	EmbedBatch(ctx context.Context, texts []string) ([]EmbeddingOutput, error)
}
