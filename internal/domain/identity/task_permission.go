package identity

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
)

type TaskPermission string

const (
	TaskRead  TaskPermission = "read"
	TaskWrite TaskPermission = "write"
)

type TaskPermissionQuery struct {
	TenantID, ProjectID, PrincipalID uuid.UUID
	TaskID                           string
	Permission                       TaskPermission
}

// TaskPermissionReader checks the effective permission at the start of each operation.
type TaskPermissionReader interface {
	HasTaskPermission(context.Context, TaskPermissionQuery) (bool, error)
}

var ErrTaskPermissionsUnavailable = errors.New("task permission store unavailable")

// AuthorizeTaskMemory checks task access for the authenticated actor. The caller
// supplies the resource's server-resolved scope (or the destination for a create).
// ErrForbidden is a policy denial. Store errors leave the operation unauthorized.
func (a AuthContext) AuthorizeTaskMemory(ctx context.Context, store TaskPermissionReader, target MemoryScopeTarget, permission TaskPermission) error {
	if !a.valid() || target.TenantID != a.TenantID || target.ScopeType != ScopeTaskShared ||
		!slices.Contains(a.AllowedScopes, ScopeTaskShared) || !a.projectAllowed(target.ProjectID) {
		return ErrForbidden
	}
	if permission != TaskRead && permission != TaskWrite {
		return ErrForbidden
	}
	if strings.TrimSpace(target.TaskID) == "" {
		return ErrMissingTaskID
	}
	if store == nil {
		return ErrTaskPermissionsUnavailable
	}
	allowed, err := store.HasTaskPermission(ctx, TaskPermissionQuery{
		TenantID: a.TenantID, ProjectID: *target.ProjectID, PrincipalID: a.PrincipalID,
		TaskID: target.TaskID, Permission: permission,
	})
	if err != nil {
		return fmt.Errorf("%w: %v", ErrTaskPermissionsUnavailable, err)
	}
	if !allowed {
		return ErrForbidden
	}
	return nil
}
