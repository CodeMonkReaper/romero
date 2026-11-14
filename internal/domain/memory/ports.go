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
