package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/romero-project/romero/internal/domain/memory"
	"github.com/romero-project/romero/internal/infrastructure/persistence/sqlcgen"
)

// OpaqueOperation enumerates write operations eligible for deferred non-reader acceptance.
type OpaqueOperation string

const (
	OpaqueOpCapture   OpaqueOperation = "capture"
	OpaqueOpUpdate    OpaqueOperation = "update"
	OpaqueOpSupersede OpaqueOperation = "supersede"
	OpaqueOpArchive   OpaqueOperation = "archive"
	OpaqueOpFeedback  OpaqueOperation = "feedback"
)

func (op OpaqueOperation) valid() bool {
	switch op {
	case OpaqueOpCapture, OpaqueOpUpdate, OpaqueOpSupersede, OpaqueOpArchive, OpaqueOpFeedback:
		return true
	default:
		return false
	}
}

// OpaqueRequestStatusValue enumerates lifecycle states of an opaque async write request.
type OpaqueRequestStatusValue string

const (
	OpaqueStatusPending   OpaqueRequestStatusValue = "pending"
	OpaqueStatusRunning   OpaqueRequestStatusValue = "running"
	OpaqueStatusCompleted OpaqueRequestStatusValue = "completed"
	OpaqueStatusFailed    OpaqueRequestStatusValue = "failed"
)

// FailureClass distinguishes transient worker errors from terminal authorization/domain failures.
type FailureClass string

const (
	FailureTransient FailureClass = "transient"
	FailurePermanent FailureClass = "permanent"
)

// OpaqueRequest carries the application-built payload queued before returning 202.
type OpaqueRequest struct {
	RequestID      uuid.UUID
	TenantID       uuid.UUID
	ProjectID      uuid.UUID
	TaskID         string
	PrincipalID    uuid.UUID
	Operation      OpaqueOperation
	TargetMemoryID *uuid.UUID
	PayloadVersion int16
	Payload        json.RawMessage
	RequestSHA256  string
	IdempotencyKey string
	MaxRetries     *int32
}

// OpaqueReceipt holds only the server-assigned opaque request identifier.
type OpaqueReceipt struct {
	RequestID uuid.UUID
}

// OpaqueRequestRecord is the internal persisted record delivered to the execution worker.
type OpaqueRequestRecord struct {
	RequestID      uuid.UUID
	TenantID       uuid.UUID
	ProjectID      uuid.UUID
	TaskID         string
	PrincipalID    uuid.UUID
	Operation      OpaqueOperation
	TargetMemoryID *uuid.UUID
	PayloadVersion int16
	Payload        json.RawMessage
	RequestSHA256  string
	IdempotencyKey string
	Status         OpaqueRequestStatusValue
	AttemptCount   int32
	MaxRetries     int32
	NextAttemptAt  time.Time
	CreatedAt      time.Time
}

// OpaqueRequestMetadata contains status and scope fields for authorized status checks.
type OpaqueRequestMetadata struct {
	RequestID uuid.UUID
	TenantID  uuid.UUID
	ProjectID uuid.UUID
	TaskID    string
	Status    OpaqueRequestStatusValue
}

// Lease represents an active time-bounded execution lease for an opaque request.
type Lease struct {
	RequestID uuid.UUID
	Token     uuid.UUID
	ExpiresAt time.Time
}

// OpaqueRequestStore persists, leases, executes, retries, and purges opaque task writes.
type OpaqueRequestStore struct {
	pool          *pgxpool.Pool
	queries       *sqlcgen.Queries
	leaseDuration time.Duration
}

// Enqueue persists req before returning an opaque 202 receipt and enforces per-(tenant, principal) idempotency.
func (o *OpaqueRequestStore) Enqueue(ctx context.Context, req OpaqueRequest) (OpaqueReceipt, error) {
	if err := validateOpaqueEnqueue(req); err != nil {
		return OpaqueReceipt{}, err
	}
	reqID := req.RequestID
	if reqID == uuid.Nil {
		reqID = uuid.New()
	}
	version := req.PayloadVersion
	if version < 1 {
		version = 1
	}
	retries := DefaultOpaqueMaxRetries
	if req.MaxRetries != nil {
		if *req.MaxRetries < 0 {
			return OpaqueReceipt{}, fmt.Errorf("%w: max_retries must be non-negative", ErrInvalidInput)
		}
		retries = *req.MaxRetries
	}
	payload := req.Payload
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}

	err := o.queries.InsertOpaqueRequest(ctx, sqlcgen.InsertOpaqueRequestParams{
		RequestID:      reqID,
		TenantID:       req.TenantID,
		ProjectID:      req.ProjectID,
		TaskID:         req.TaskID,
		PrincipalID:    req.PrincipalID,
		Operation:      string(req.Operation),
		TargetMemoryID: toNullUUID(req.TargetMemoryID),
		PayloadVersion: version,
		Payload:        payload,
		RequestSha256:  req.RequestSHA256,
		IdempotencyKey: toPgText(req.IdempotencyKey),
		MaxRetries:     retries,
	})
	if err == nil {
		return OpaqueReceipt{RequestID: reqID}, nil
	}

	var pgErr *pgconn.PgError
	if req.IdempotencyKey != "" && errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "idx_opaque_requests_idempotency" {
		existing, lookupErr := o.queries.GetOpaqueRequestByIdempotencyKey(ctx, sqlcgen.GetOpaqueRequestByIdempotencyKeyParams{
			TenantID:       req.TenantID,
			PrincipalID:    req.PrincipalID,
			IdempotencyKey: toPgText(req.IdempotencyKey),
		})
		if lookupErr != nil {
			return OpaqueReceipt{}, lookupErr
		}
		if existing.RequestSha256 == req.RequestSHA256 {
			return OpaqueReceipt{RequestID: existing.RequestID}, nil
		}
		return OpaqueReceipt{}, ErrIdempotencyConflict
	}
	return OpaqueReceipt{}, err
}

// GetStatus returns scope metadata and lifecycle status for requestID within tenantID.
func (o *OpaqueRequestStore) GetStatus(ctx context.Context, tenantID, requestID uuid.UUID) (*OpaqueRequestMetadata, error) {
	if tenantID == uuid.Nil || requestID == uuid.Nil {
		return nil, memory.ErrInvalidIdentifier
	}
	row, err := o.queries.GetOpaqueRequestByID(ctx, sqlcgen.GetOpaqueRequestByIDParams{
		RequestID: requestID,
		TenantID:  tenantID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, memory.ErrNotFound
		}
		return nil, err
	}
	return &OpaqueRequestMetadata{
		RequestID: row.RequestID,
		TenantID:  row.TenantID,
		ProjectID: row.ProjectID,
		TaskID:    row.TaskID,
		Status:    OpaqueRequestStatusValue(row.Status),
	}, nil
}

// Claim leases the next pending or lease-expired running request using FOR UPDATE SKIP LOCKED.
// Returns (nil, nil) when no eligible request exists.
func (o *OpaqueRequestStore) Claim(ctx context.Context) (*Lease, error) {
	leaseSeconds := max(int32(o.leaseDuration.Seconds()), 1)
	row, err := o.queries.ClaimNextOpaqueRequest(ctx, sqlcgen.ClaimNextOpaqueRequestParams{
		LeaseToken:   uuid.NullUUID{UUID: uuid.New(), Valid: true},
		LeaseSeconds: leaseSeconds,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if !row.LeaseToken.Valid || row.LeaseExpiresAt == nil {
		return nil, fmt.Errorf("claimed row %s missing lease metadata", row.RequestID)
	}
	return &Lease{
		RequestID: row.RequestID,
		Token:     row.LeaseToken.UUID,
		ExpiresAt: *row.LeaseExpiresAt,
	}, nil
}

// Renew extends lease_expires_at while the lease token remains active and un-expired.
func (o *OpaqueRequestStore) Renew(ctx context.Context, lease Lease) (bool, error) {
	if lease.RequestID == uuid.Nil || lease.Token == uuid.Nil {
		return false, ErrLeaseLost
	}
	leaseSeconds := max(int32(o.leaseDuration.Seconds()), 1)
	rows, err := o.queries.RenewOpaqueRequestLease(ctx, sqlcgen.RenewOpaqueRequestLeaseParams{
		RequestID:    lease.RequestID,
		LeaseToken:   uuid.NullUUID{UUID: lease.Token, Valid: true},
		LeaseSeconds: leaseSeconds,
	})
	if err != nil {
		return false, err
	}
	return rows == 1, nil
}

// Execute runs apply inside a READ COMMITTED transaction, providing both repository registry
// and transaction-bound TaskPermissionReader, and atomically completes the request only if the
// lease token is still active and unexpired against PostgreSQL clock_timestamp().
func (o *OpaqueRequestStore) Execute(ctx context.Context, lease Lease, apply ApplyMutation) error {
	if apply == nil || lease.RequestID == uuid.Nil || lease.Token == uuid.Nil {
		return ErrLeaseLost
	}

	tx, err := o.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	committed := false
	defer rollbackTx(&committed, tx)

	txQueries := o.queries.WithTx(tx)
	row, err := txQueries.GetOpaqueRequestForExecution(ctx, sqlcgen.GetOpaqueRequestForExecutionParams{
		RequestID:  lease.RequestID,
		LeaseToken: uuid.NullUUID{UUID: lease.Token, Valid: true},
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrLeaseLost
		}
		return err
	}

	record := OpaqueRequestRecord{
		RequestID:      row.RequestID,
		TenantID:       row.TenantID,
		ProjectID:      row.ProjectID,
		TaskID:         row.TaskID,
		PrincipalID:    row.PrincipalID,
		Operation:      OpaqueOperation(row.Operation),
		TargetMemoryID: fromNullUUID(row.TargetMemoryID),
		PayloadVersion: row.PayloadVersion,
		Payload:        json.RawMessage(row.Payload),
		RequestSHA256:  row.RequestSha256,
		IdempotencyKey: fromPgText(row.IdempotencyKey),
		Status:         OpaqueRequestStatusValue(row.Status),
		AttemptCount:   row.AttemptCount,
		MaxRetries:     row.MaxRetries,
		NextAttemptAt:  row.NextAttemptAt,
		CreatedAt:      row.CreatedAt,
	}

	reg := &repoRegistry{txQueries: txQueries, poolQueries: o.queries}
	txPerms := &TaskPermissionRepository{q: txQueries}
	resultMemoryID, err := apply(ctx, reg, txPerms, record)
	if err != nil {
		return err
	}

	updated, err := txQueries.CompleteOpaqueRequest(ctx, sqlcgen.CompleteOpaqueRequestParams{
		RequestID:      lease.RequestID,
		LeaseToken:     uuid.NullUUID{UUID: lease.Token, Valid: true},
		ResultMemoryID: toNullUUID(resultMemoryID),
	})
	if err != nil {
		return err
	}
	if updated != 1 {
		return ErrLeaseLost
	}

	if err := tx.Commit(ctx); err != nil {
		return err
	}
	committed = true
	return nil
}

// RecordFailure transitions a leased request to backoff pending (1s, 2s, 4s) on transient errors
// within retry limits, or to terminal failed status once retries are exhausted or permanent.
func (o *OpaqueRequestStore) RecordFailure(ctx context.Context, lease Lease, class FailureClass, code, message string) error {
	if lease.RequestID == uuid.Nil || lease.Token == uuid.Nil {
		return ErrLeaseLost
	}

	return o.poolTx(ctx, func(txQueries *sqlcgen.Queries) error {
		row, err := txQueries.GetOpaqueRequestForExecution(ctx, sqlcgen.GetOpaqueRequestForExecutionParams{
			RequestID:  lease.RequestID,
			LeaseToken: uuid.NullUUID{UUID: lease.Token, Valid: true},
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrLeaseLost
			}
			return err
		}

		if class == FailureTransient && row.AttemptCount < row.MaxRetries {
			backoffSecs := int32(1 << min(row.AttemptCount, 10))
			n, err := txQueries.ScheduleOpaqueRequestRetry(ctx, sqlcgen.ScheduleOpaqueRequestRetryParams{
				RequestID:        lease.RequestID,
				LeaseToken:       uuid.NullUUID{UUID: lease.Token, Valid: true},
				LastErrorCode:    toPgText(code),
				LastErrorMessage: toPgText(message),
				BackoffSeconds:   backoffSecs,
			})
			if err != nil {
				return err
			}
			if n != 1 {
				return ErrLeaseLost
			}
			return nil
		}

		n, err := txQueries.MarkOpaqueRequestFailedPermanent(ctx, sqlcgen.MarkOpaqueRequestFailedPermanentParams{
			RequestID:        lease.RequestID,
			LeaseToken:       uuid.NullUUID{UUID: lease.Token, Valid: true},
			LastErrorCode:    toPgText(code),
			LastErrorMessage: toPgText(message),
		})
		if err != nil {
			return err
		}
		if n != 1 {
			return ErrLeaseLost
		}
		return nil
	})
}

// PurgeTerminal deletes completed or failed requests whose completed_at exceeds retention.
func (o *OpaqueRequestStore) PurgeTerminal(ctx context.Context, retention time.Duration) (int64, error) {
	if retention < 0 {
		return 0, fmt.Errorf("%w: retention must be non-negative", ErrInvalidInput)
	}
	return o.queries.PurgeTerminalOpaqueRequests(ctx, retention.Seconds())
}

func (o *OpaqueRequestStore) poolTx(ctx context.Context, fn func(*sqlcgen.Queries) error) error {
	tx, err := o.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	committed := false
	defer rollbackTx(&committed, tx)
	if err := fn(o.queries.WithTx(tx)); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	committed = true
	return nil
}

func validateOpaqueEnqueue(req OpaqueRequest) error {
	if req.TenantID == uuid.Nil || req.ProjectID == uuid.Nil || req.PrincipalID == uuid.Nil || strings.TrimSpace(req.TaskID) == "" {
		return fmt.Errorf("%w: tenant_id, project_id, principal_id, and task_id are required", ErrInvalidInput)
	}
	if !req.Operation.valid() {
		return fmt.Errorf("%w: invalid opaque operation %q", ErrInvalidInput, req.Operation)
	}
	if req.IdempotencyKey != "" && strings.TrimSpace(req.IdempotencyKey) == "" {
		return fmt.Errorf("%w: idempotency_key cannot be whitespace-only", ErrInvalidInput)
	}
	if !isLowerHexSHA256(req.RequestSHA256) {
		return fmt.Errorf("%w: request_sha256 must be 64 lowercase hex characters", ErrInvalidInput)
	}
	return nil
}

func isLowerHexSHA256(s string) bool {
	if len(s) != 64 {
		return false
	}
	for i := range len(s) {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
