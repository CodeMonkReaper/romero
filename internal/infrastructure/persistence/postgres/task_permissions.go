package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/romero-project/romero/internal/domain/identity"
	"github.com/romero-project/romero/internal/domain/memory"
	"github.com/romero-project/romero/internal/infrastructure/persistence/sqlcgen"
)

// TaskPermissionRepository implements identity.TaskPermissionReader and snapshot queries
// in READ COMMITTED isolation against PostgreSQL 17.
type TaskPermissionRepository struct {
	q *sqlcgen.Queries
}

// EvaluateTaskAccess reads project, scope, and task grants in a single SQL statement snapshot.
// Missing or inactive tenant/project/principal rows evaluate as denied (false/false, nil).
// Query failures return an error so the caller denies access with 503 rather than masking outages.
func (r *TaskPermissionRepository) EvaluateTaskAccess(ctx context.Context, q identity.TaskPermissionQuery) (TaskAccessSnapshot, error) {
	if r == nil || r.q == nil {
		return TaskAccessSnapshot{}, fmt.Errorf("%w: task permission repository uninitialized", ErrInvalidInput)
	}
	if q.TenantID == uuid.Nil || q.ProjectID == uuid.Nil || q.PrincipalID == uuid.Nil || strings.TrimSpace(q.TaskID) == "" {
		return TaskAccessSnapshot{}, nil
	}
	row, err := r.q.EvaluateTaskAccessSnapshot(ctx, sqlcgen.EvaluateTaskAccessSnapshotParams{
		ProjectID:   q.ProjectID,
		TaskID:      q.TaskID,
		PrincipalID: q.PrincipalID,
		TenantID:    q.TenantID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return TaskAccessSnapshot{}, nil
		}
		return TaskAccessSnapshot{}, err
	}
	return TaskAccessSnapshot{
		CanRead:  row.CanRead,
		CanWrite: row.CanWrite,
	}, nil
}

// HasTaskPermission implements identity.TaskPermissionReader without caching.
func (r *TaskPermissionRepository) HasTaskPermission(ctx context.Context, q identity.TaskPermissionQuery) (bool, error) {
	snap, err := r.EvaluateTaskAccess(ctx, q)
	if err != nil {
		return false, err
	}
	switch q.Permission {
	case identity.TaskRead:
		return snap.CanRead, nil
	case identity.TaskWrite:
		return snap.CanWrite, nil
	default:
		return false, nil
	}
}

// GrantOrRevokeTaskPermission atomically updates a principal's task_shared permission row and
// writes its allowed audit event inside one ACID transaction (AC-T4).
func (s *Store) GrantOrRevokeTaskPermission(ctx context.Context, grant TaskPermissionGrant, auditEv memory.AuditEvent) error {
	if grant.TenantID == uuid.Nil || grant.ProjectID == uuid.Nil || grant.PrincipalID == uuid.Nil || strings.TrimSpace(grant.TaskID) == "" {
		return fmt.Errorf("%w: tenant_id, project_id, principal_id, and task_id are required", ErrInvalidInput)
	}
	if err := auditEv.Validate(); err != nil {
		return err
	}
	if auditEv.Outcome != memory.AuditAllowed {
		return fmt.Errorf("%w: administrative grant audit must record allowed outcome", memory.ErrInvalidAuditOutcome)
	}

	return s.ExecTx(ctx, func(reg memory.RepositoryRegistry) error {
		tr := reg.(*repoRegistry)
		if _, err := tr.txQueries.UpsertTaskPermission(ctx, sqlcgen.UpsertTaskPermissionParams{
			TenantID:    grant.TenantID,
			ProjectID:   grant.ProjectID,
			TaskID:      grant.TaskID,
			PrincipalID: grant.PrincipalID,
			CanRead:     grant.CanRead,
			CanWrite:    grant.CanWrite,
		}); err != nil {
			return err
		}
		return reg.Audit().RecordAllowed(ctx, auditEv)
	})
}
