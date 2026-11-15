package memory

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/romero-project/romero/internal/domain/identity"
	"github.com/romero-project/romero/internal/domain/provenance"
)

// SourceRepository persists immutable provenance records.
type SourceRepository interface {
	InsertSource(ctx context.Context, src provenance.MemorySource) error
}

// MemoryRepository defines transactional persistence operations for memories,
// immutable versions, and bitemporal relations.
type MemoryRepository interface {
	GetMemoryByID(ctx context.Context, tenantID, memoryID uuid.UUID) (*Memory, error)
	GetMemoryByIdempotencyKey(ctx context.Context, tenantID uuid.UUID, key string) (*Memory, error)
	LockActiveTopicMemory(ctx context.Context, scope identity.MemoryScopeTarget, topicKey string) (*Memory, error)
	InsertMemory(ctx context.Context, m Memory) error
	UpdateMemoryState(ctx context.Context, m Memory) error
	ListVersionsByMemoryID(ctx context.Context, memoryID uuid.UUID) ([]MemoryVersion, error)
	InsertMemoryVersion(ctx context.Context, v MemoryVersion) error
	CloseMemoryVersionValidity(ctx context.Context, memoryID uuid.UUID, version int, validUntil time.Time) error
	GetMemoryVersionPointInTime(ctx context.Context, memoryID uuid.UUID, validAt, knownAt time.Time) (*MemoryVersion, error)
	ListActiveRelationsByMemoryID(ctx context.Context, tenantID, memoryID uuid.UUID) ([]MemoryRelation, error)
	InsertMemoryRelation(ctx context.Context, rel MemoryRelation) error
}

// FeedbackRepository stores empirical feedback events.
type FeedbackRepository interface {
	InsertMemoryFeedback(ctx context.Context, fb MemoryFeedback) error
}

// JobRepository enqueues asynchronous processing tasks atomically with mutations.
type JobRepository interface {
	EnqueueMemoryJob(ctx context.Context, job MemoryJob) error
}

// AuditLogger persists allowed events inside a transaction and denied events out-of-rollback.
type AuditLogger interface {
	RecordAllowed(ctx context.Context, ev AuditEvent) error
	RecordDenied(ctx context.Context, ev AuditEvent) error
}

// RepositoryRegistry exposes domain repositories bound to a single transactional UnitOfWork.
type RepositoryRegistry interface {
	Sources() SourceRepository
	Memories() MemoryRepository
	Feedback() FeedbackRepository
	Jobs() JobRepository
	Audit() AuditLogger
}

// UnitOfWork executes domain operations inside an ACID database transaction.
type UnitOfWork interface {
	ExecTx(ctx context.Context, fn func(RepositoryRegistry) error) error
}

// SearchFilter carries Zero-Trust scope constraints and query parameters for tri-modal retrieval.
type SearchFilter struct {
	TenantID            uuid.UUID
	ProjectIDs          []uuid.UUID
	Scopes              []identity.ScopeType
	CallerAgentID       *uuid.UUID
	AllowedTasks        []string
	QueryText           string
	IncludeSuperseded   bool
	IncludeConsolidated bool
	Limit               int
	Now                 time.Time
}

// CausalWalkRow represents a single edge traversed in a recursive SQL subgraph query.
type CausalWalkRow struct {
	FromID       uuid.UUID
	ToID         uuid.UUID
	RelationType RelationType
	Weight       float32
	Depth        int
}

// EmbeddingRepository persists and retrieves dense/sparse embeddings in PostgreSQL.
type EmbeddingRepository interface {
	UpsertMemoryEmbedding(ctx context.Context, emb MemoryEmbedding) error
	GetMemoryEmbeddingsByIDs(ctx context.Context, tenantID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID][]float32, error)
}

// JobQueueRepository claims and updates asynchronous enrichment jobs with SKIP LOCKED semantics.
type JobQueueRepository interface {
	ClaimPendingMemoryJobs(ctx context.Context, jobType string, limit int, now time.Time) ([]MemoryJob, error)
	CompleteMemoryJob(ctx context.Context, jobID uuid.UUID, completedAt time.Time) error
	FailMemoryJob(ctx context.Context, jobID uuid.UUID, lastError string, nextScheduledAt time.Time, deadLetter bool) error
}

// SearchRepository executes Zero-Trust tri-modal candidate queries and graph snapshot loads.
type SearchRepository interface {
	SearchByTrigram(ctx context.Context, f SearchFilter) ([]uuid.UUID, error)
	SearchByFTS(ctx context.Context, f SearchFilter) ([]uuid.UUID, error)
	SearchByVector(ctx context.Context, f SearchFilter, queryVec []float32) ([]uuid.UUID, error)
	GetMemoriesByIDs(ctx context.Context, tenantID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]Memory, error)
	LoadProjectGraphSnapshot(ctx context.Context, tenantID uuid.UUID, projectID *uuid.UUID, now time.Time) ([]Memory, []MemoryRelation, map[uuid.UUID][]float32, error)
	TraverseCausalSubgraph(ctx context.Context, tenantID uuid.UUID, seeds []uuid.UUID, hops int, now time.Time) ([]CausalWalkRow, error)
}

// GraphMutationNotifier synchronizes write-through mutations into the in-RAM L1 Cognitive Graph.
type GraphMutationNotifier interface {
	ApplyMemoryUpsert(m Memory)
	ApplyMemoryStatusChange(tenantID uuid.UUID, projectID *uuid.UUID, memoryID uuid.UUID, status MemoryStatus, validUntil *time.Time)
	ApplyRelationsAdded(rels []MemoryRelation)
	ApplyEmbeddingUpsert(tenantID uuid.UUID, projectID *uuid.UUID, memoryID uuid.UUID, denseVec []float32)
}
