// Package postgres implements PostgreSQL 17 persistence, transactional UnitOfWork,
// out-of-rollback audit logging, task permission snapshots, and durable opaque requests.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/romero-project/romero/internal/domain/identity"
	"github.com/romero-project/romero/internal/domain/memory"
	"github.com/romero-project/romero/internal/infrastructure/persistence/sqlcgen"
)

const (
	// DefaultOpaqueLeaseDuration is the default worker lease window (60s per AC-T10).
	DefaultOpaqueLeaseDuration = 60 * time.Second
	// DefaultOpaqueMaxRetries is the default transient retry cap (3 retries: 1s, 2s, 4s).
	DefaultOpaqueMaxRetries int32 = 3
)

var (
	ErrNilPool             = errors.New("postgres connection pool is required")
	ErrAlreadyExists       = errors.New("resource already exists")
	ErrIdempotencyConflict = errors.New("idempotency key reused with different request payload")
	ErrLeaseLost           = errors.New("opaque request lease expired or superseded")
	ErrInvalidInput        = errors.New("invalid persistence input")
)

// Store wraps a PostgreSQL connection pool and exposes repositories and transactions.
type Store struct {
	pool    *pgxpool.Pool
	queries *sqlcgen.Queries
}

// NewStore validates pool and constructs a Store.
func NewStore(pool *pgxpool.Pool) (*Store, error) {
	if pool == nil {
		return nil, ErrNilPool
	}
	return &Store{
		pool:    pool,
		queries: sqlcgen.New(pool),
	}, nil
}

// ExecTx runs fn inside a PostgreSQL READ COMMITTED transaction implementing memory.UnitOfWork.
// Rollback is automatic if fn returns an error; allowed audit events roll back with the
// transaction while denied audit events are committed out-of-band via the Store pool.
func (s *Store) ExecTx(ctx context.Context, fn func(memory.RepositoryRegistry) error) error {
	if fn == nil {
		return fmt.Errorf("%w: nil transaction callback", ErrInvalidInput)
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	committed := false
	defer rollbackTx(&committed, tx)

	reg := &repoRegistry{
		txQueries:   s.queries.WithTx(tx),
		poolQueries: s.queries,
	}
	if err := fn(reg); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	committed = true
	return nil
}

// Audit returns an out-of-transaction AuditLogger backed by the Store pool.
func (s *Store) Audit() memory.AuditLogger {
	return &auditRepo{
		allowedQueries: s.queries,
		deniedQueries:  s.queries,
	}
}

// Memories returns a non-transactional MemoryRepository backed by the Store pool for read paths.
func (s *Store) Memories() memory.MemoryRepository {
	return &memoryRepo{q: s.queries}
}

// TaskPermissions returns a non-transactional TaskPermissionRepository backed by the Store pool.
func (s *Store) TaskPermissions() *TaskPermissionRepository {
	return &TaskPermissionRepository{q: s.queries}
}

// NewTxTaskPermissions binds a TaskPermissionRepository to an existing database transaction.
func (s *Store) NewTxTaskPermissions(tx pgx.Tx) (*TaskPermissionRepository, error) {
	if tx == nil {
		return nil, fmt.Errorf("%w: transaction is required", ErrInvalidInput)
	}
	return &TaskPermissionRepository{q: s.queries.WithTx(tx)}, nil
}

// Provisioning returns a ProvisioningStore backed by the Store pool.
func (s *Store) Provisioning() *ProvisioningStore {
	return &ProvisioningStore{
		pool:    s.pool,
		queries: s.queries,
	}
}

// OpaqueRequests returns the durable opaque request queue with leaseDuration (defaults to 60s if <= 0).
func (s *Store) OpaqueRequests(leaseDuration time.Duration) *OpaqueRequestStore {
	if leaseDuration <= 0 {
		leaseDuration = DefaultOpaqueLeaseDuration
	}
	return &OpaqueRequestStore{
		pool:          s.pool,
		queries:       s.queries,
		leaseDuration: leaseDuration,
	}
}

type repoRegistry struct {
	txQueries   *sqlcgen.Queries
	poolQueries *sqlcgen.Queries
}

func (r *repoRegistry) Sources() memory.SourceRepository {
	return &sourceRepo{q: r.txQueries}
}

func (r *repoRegistry) Memories() memory.MemoryRepository {
	return &memoryRepo{q: r.txQueries}
}

func (r *repoRegistry) Feedback() memory.FeedbackRepository {
	return &feedbackRepo{q: r.txQueries}
}

func (r *repoRegistry) Jobs() memory.JobRepository {
	return &jobRepo{q: r.txQueries}
}

func (r *repoRegistry) Audit() memory.AuditLogger {
	return &auditRepo{
		allowedQueries: r.txQueries,
		deniedQueries:  r.poolQueries,
	}
}

// TaskAccessSnapshot captures effective task_shared permissions from one READ COMMITTED query.
type TaskAccessSnapshot struct {
	CanRead  bool
	CanWrite bool
}

// TaskPermissionGrant represents an administrative grant or revocation of task_shared rights.
type TaskPermissionGrant struct {
	TenantID    uuid.UUID
	ProjectID   uuid.UUID
	TaskID      string
	PrincipalID uuid.UUID
	CanRead     bool
	CanWrite    bool
}

// ApplyMutation executes a claimed opaque request inside the worker's re-authorized transaction.
type ApplyMutation func(ctx context.Context, reg memory.RepositoryRegistry, perms identity.TaskPermissionReader, req OpaqueRequestRecord) (resultMemoryID *uuid.UUID, err error)

func rollbackTx(committed *bool, tx pgx.Tx) {
	if *committed {
		return
	}
	rbCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = tx.Rollback(rbCtx)
}
