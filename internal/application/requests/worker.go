// Package requests implements the durable background worker for deferred
// task_shared opaque write requests with lease fencing, bounded retries, and 30d purge.
package requests

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/romero-project/romero/internal/application/capture"
	"github.com/romero-project/romero/internal/application/feedback"
	"github.com/romero-project/romero/internal/domain/identity"
	"github.com/romero-project/romero/internal/domain/memory"
)

// ErrLeaseLost indicates the worker's execution lease expired or was fenced.
var ErrLeaseLost = errors.New("opaque request lease lost")

// OpaqueOperation enumerates deferred write operations handled by the worker.
type OpaqueOperation string

const (
	OpaqueOpCapture   OpaqueOperation = "capture"
	OpaqueOpUpdate    OpaqueOperation = "update"
	OpaqueOpSupersede OpaqueOperation = "supersede"
	OpaqueOpArchive   OpaqueOperation = "archive"
	OpaqueOpFeedback  OpaqueOperation = "feedback"
)

// FailureClass distinguishes transient retries (1s/2s/4s) from terminal failures.
type FailureClass string

const (
	FailureTransient FailureClass = "transient"
	FailurePermanent FailureClass = "permanent"
)

// Lease represents an active time-bounded execution lease for an opaque request.
type Lease struct {
	RequestID uuid.UUID
	Token     uuid.UUID
	ExpiresAt time.Time
}

// OpaqueRequestRecord is the persisted payload delivered to the execution worker.
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
	AttemptCount   int32
	MaxRetries     int32
}

// ApplyMutation runs inside a READ COMMITTED transaction while the lease is fenced.
type ApplyMutation func(ctx context.Context, reg memory.RepositoryRegistry, perms identity.TaskPermissionReader, record OpaqueRequestRecord) (*uuid.UUID, error)

// Queue abstracts durable opaque request claiming, lease renewal, execution, failure, and purge.
type Queue interface {
	Claim(ctx context.Context) (*Lease, error)
	Renew(ctx context.Context, lease Lease) (bool, error)
	Execute(ctx context.Context, lease Lease, apply ApplyMutation) error
	RecordFailure(ctx context.Context, lease Lease, class FailureClass, code, message string) error
	PurgeTerminal(ctx context.Context, retention time.Duration) (int64, error)
}

// PrincipalResolver loads the current Zero-Trust AuthContext for a queued principal.
type PrincipalResolver interface {
	ResolveAuthContext(ctx context.Context, tenantID, principalID uuid.UUID) (identity.AuthContext, error)
}

// MutationExecutor applies the deserialized opaque payload inside the worker's transaction.
type MutationExecutor interface {
	ExecuteOpaqueOperationTx(ctx context.Context, reg memory.RepositoryRegistry, auth identity.AuthContext, op OpaqueOperation, targetID *uuid.UUID, payload json.RawMessage) (*uuid.UUID, error)
}

// Config configures polling, lease renewal, and 30-day terminal retention purge intervals.
type Config struct {
	PollInterval  time.Duration
	RenewInterval time.Duration
	PurgeInterval time.Duration
	Retention     time.Duration
}

// Worker claims, renews, reauthorizes, executes, and purges opaque task_shared requests.
type Worker struct {
	queue      Queue
	principals PrincipalResolver
	executor   MutationExecutor
	cfg        Config
}

type classifiedError struct {
	class FailureClass
	code  string
	err   error
}

func (e *classifiedError) Error() string { return e.err.Error() }
func (e *classifiedError) Unwrap() error { return e.err }

func permanentErr(code, msg string) error {
	return &classifiedError{class: FailurePermanent, code: code, err: errors.New(msg)}
}

// NewWorker constructs a durable opaque request Worker.
func NewWorker(queue Queue, principals PrincipalResolver, executor MutationExecutor, cfg Config) (*Worker, error) {
	if queue == nil || executor == nil {
		return nil, errors.New("queue and executor are required")
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 250 * time.Millisecond
	}
	if cfg.RenewInterval <= 0 {
		cfg.RenewInterval = 20 * time.Second
	}
	if cfg.PurgeInterval <= 0 {
		cfg.PurgeInterval = time.Hour
	}
	if cfg.Retention <= 0 {
		cfg.Retention = 30 * 24 * time.Hour
	}
	return &Worker{
		queue:      queue,
		principals: principals,
		executor:   executor,
		cfg:        cfg,
	}, nil
}

// ProcessOne claims and processes at most one opaque request, returning true if one was claimed.
func (w *Worker) ProcessOne(ctx context.Context) (bool, error) {
	lease, err := w.queue.Claim(ctx)
	if err != nil {
		return false, err
	}
	if lease == nil {
		return false, nil
	}

	execCtx, cancelExec := context.WithCancel(ctx)
	defer cancelExec()

	var renewLost sync.Mutex
	leaseLost := false
	doneRenew := make(chan struct{})
	go func() {
		defer close(doneRenew)
		ticker := time.NewTicker(w.cfg.RenewInterval)
		defer ticker.Stop()
		for {
			select {
			case <-execCtx.Done():
				return
			case <-ticker.C:
				ok, rErr := w.queue.Renew(execCtx, *lease)
				if rErr != nil || !ok {
					renewLost.Lock()
					leaseLost = true
					renewLost.Unlock()
					cancelExec()
					return
				}
			}
		}
	}()

	execErr := w.queue.Execute(execCtx, *lease, w.applyRecord)
	cancelExec()
	<-doneRenew

	if execErr == nil {
		return true, nil
	}

	renewLost.Lock()
	lost := leaseLost
	renewLost.Unlock()
	if lost || errors.Is(execErr, ErrLeaseLost) {
		return true, nil
	}

	class, code := classifyError(execErr)
	if rfErr := w.queue.RecordFailure(ctx, *lease, class, code, execErr.Error()); rfErr != nil && !errors.Is(rfErr, ErrLeaseLost) {
		return true, rfErr
	}
	return true, nil
}

// Run polls the opaque queue and runs periodic 30-day terminal retention purges until ctx is canceled.
func (w *Worker) Run(ctx context.Context) error {
	purgeTicker := time.NewTicker(w.cfg.PurgeInterval)
	defer purgeTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-purgeTicker.C:
			_, _ = w.queue.PurgeTerminal(ctx, w.cfg.Retention)
		default:
		}

		processed, err := w.ProcessOne(ctx)
		if err != nil || !processed {
			pollTimer := time.NewTimer(w.cfg.PollInterval)
			select {
			case <-ctx.Done():
				pollTimer.Stop()
				return ctx.Err()
			case <-purgeTicker.C:
				pollTimer.Stop()
				_, _ = w.queue.PurgeTerminal(ctx, w.cfg.Retention)
			case <-pollTimer.C:
			}
		}
	}
}

func (w *Worker) applyRecord(
	ctx context.Context,
	reg memory.RepositoryRegistry,
	perms identity.TaskPermissionReader,
	record OpaqueRequestRecord,
) (*uuid.UUID, error) {
	if perms == nil {
		return nil, errors.New("task permission reader unavailable")
	}
	allowed, err := perms.HasTaskPermission(ctx, identity.TaskPermissionQuery{
		TenantID:    record.TenantID,
		ProjectID:   record.ProjectID,
		PrincipalID: record.PrincipalID,
		TaskID:      record.TaskID,
		Permission:  identity.TaskWrite,
	})
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, permanentErr("permission_revoked", "task write permission revoked")
	}

	auth := identity.AuthContext{
		TenantID:        record.TenantID,
		PrincipalID:     record.PrincipalID,
		PrincipalType:   identity.PrincipalAgent,
		AllowedProjects: []uuid.UUID{record.ProjectID},
		AllowedScopes:   []identity.ScopeType{identity.ScopeTaskShared},
	}
	if w.principals != nil {
		resolved, rErr := w.principals.ResolveAuthContext(ctx, record.TenantID, record.PrincipalID)
		if rErr != nil {
			if isPermanentAuthErr(rErr) {
				return nil, permanentErr("principal_revoked", rErr.Error())
			}
			return nil, rErr
		}
		auth = resolved
	}
	if !slices.Contains(auth.AllowedScopes, identity.ScopeTaskShared) || !slices.Contains(auth.AllowedProjects, record.ProjectID) {
		return nil, permanentErr("scope_revoked", "principal project or task_shared scope revoked")
	}

	if record.Operation != OpaqueOpCapture {
		if record.TargetMemoryID == nil || *record.TargetMemoryID == uuid.Nil {
			return nil, permanentErr("invalid_target", "target_memory_id is required")
		}
		mem, mErr := reg.Memories().GetMemoryByID(ctx, record.TenantID, *record.TargetMemoryID)
		if mErr != nil {
			if errors.Is(mErr, memory.ErrNotFound) {
				return nil, permanentErr("target_not_found", "target memory not found")
			}
			return nil, mErr
		}
		if mem.ScopeType != identity.ScopeTaskShared || mem.ProjectID == nil || *mem.ProjectID != record.ProjectID || mem.TaskID != record.TaskID {
			return nil, permanentErr("target_scope_mismatch", "target memory outside authorized task scope")
		}
	}

	return w.executor.ExecuteOpaqueOperationTx(ctx, reg, auth, record.Operation, record.TargetMemoryID, record.Payload)
}

func classifyError(err error) (FailureClass, string) {
	var ce *classifiedError
	if errors.As(err, &ce) {
		return ce.class, ce.code
	}
	var syn *json.SyntaxError
	if errors.As(err, &syn) {
		return FailurePermanent, "invalid_payload"
	}
	switch {
	case errors.Is(err, capture.ErrForbidden), errors.Is(err, feedback.ErrForbidden), errors.Is(err, identity.ErrForbidden):
		return FailurePermanent, "forbidden"
	case errors.Is(err, capture.ErrNotFound), errors.Is(err, feedback.ErrNotFound), errors.Is(err, memory.ErrNotFound):
		return FailurePermanent, "not_found"
	case errors.Is(err, capture.ErrConflict), errors.Is(err, feedback.ErrConflict), errors.Is(err, memory.ErrClosedMemoryConflict):
		return FailurePermanent, "conflict"
	case errors.Is(err, capture.ErrValidation), errors.Is(err, feedback.ErrValidation):
		return FailurePermanent, "validation_error"
	default:
		return FailureTransient, "transient_error"
	}
}

func isPermanentAuthErr(err error) bool {
	return errors.Is(err, identity.ErrNotFound) || errors.Is(err, identity.ErrForbidden) || errors.Is(err, memory.ErrNotFound)
}

// ServiceExecutor adapts CaptureService and FeedbackService to MutationExecutor.
type ServiceExecutor struct {
	Capture  *capture.Service
	Feedback *feedback.Service
}

// ExecuteOpaqueOperationTx dispatches a queued opaque payload to T006 or T007 inside the worker transaction.
func (e *ServiceExecutor) ExecuteOpaqueOperationTx(
	ctx context.Context,
	reg memory.RepositoryRegistry,
	auth identity.AuthContext,
	op OpaqueOperation,
	targetID *uuid.UUID,
	payload json.RawMessage,
) (*uuid.UUID, error) {
	switch op {
	case OpaqueOpCapture:
		var env struct {
			Capture capture.CaptureCommand `json:"capture"`
		}
		if err := json.Unmarshal(payload, &env); err != nil {
			return nil, fmt.Errorf("%w: %v", capture.ErrValidation, err)
		}
		if err := auth.CanAssignEpistemic(string(env.Capture.EpistemicStatus)); err != nil {
			return nil, capture.ErrForbidden
		}
		v, _, err := e.Capture.ExecuteCaptureTx(ctx, reg, auth, env.Capture)
		if err != nil {
			return nil, err
		}
		return &v.ID, nil
	case OpaqueOpFeedback:
		var cmd feedback.FeedbackCommand
		if err := json.Unmarshal(payload, &cmd); err != nil {
			return nil, fmt.Errorf("%w: %v", feedback.ErrValidation, err)
		}
		if err := memory.CanApplyFeedbackSignal(auth.PrincipalType, cmd.Signal); err != nil {
			return nil, feedback.ErrForbidden
		}
		if targetID != nil && *targetID != uuid.Nil {
			cmd.MemoryID = *targetID
		}
		res, err := e.Feedback.ExecuteOpaqueFeedbackTx(ctx, reg, auth, cmd)
		if err != nil {
			return nil, err
		}
		return &res.MemoryID, nil
	default:
		return e.Capture.ExecuteOpaqueTargetTx(ctx, reg, auth, string(op), targetID, payload)
	}
}
