// Package identity contains the authorization policy for authenticated principals.
package identity

import (
	"errors"
	"github.com/google/uuid"
	"slices"
	"strings"
)

type PrincipalType string

const (
	PrincipalUser         PrincipalType = "user"
	PrincipalService      PrincipalType = "service"
	PrincipalOrchestrator PrincipalType = "orchestrator"
	PrincipalAgent        PrincipalType = "agent"
)

type ScopeType string

const (
	ScopeAgentPrivate  ScopeType = "agent_private"
	ScopeTaskShared    ScopeType = "task_shared"
	ScopeProjectShared ScopeType = "project_shared"
	ScopeOrgShared     ScopeType = "org_shared"
	ScopeGlobalPublic  ScopeType = "global_public"
)

var (
	ErrForbidden     = errors.New("forbidden")
	ErrMissingTaskID = errors.New("task_id is required")
	ErrNotFound      = errors.New("not found")
)

type AuthContext struct {
	TenantID        uuid.UUID
	PrincipalID     uuid.UUID
	PrincipalType   PrincipalType
	AgentID         *uuid.UUID
	AllowedProjects []uuid.UUID
	AllowedScopes   []ScopeType
	IsAdmin         bool
}
type MemoryScopeTarget struct {
	TenantID     uuid.UUID
	ProjectID    *uuid.UUID
	ScopeType    ScopeType
	OwnerAgentID *uuid.UUID
	TaskID       string
}

// SameTopicPartition reports whether topic keys compete inside the same scope boundary.
func (m MemoryScopeTarget) SameTopicPartition(other MemoryScopeTarget) bool {
	if m.TenantID == uuid.Nil || m.TenantID != other.TenantID || m.ScopeType != other.ScopeType {
		return false
	}
	switch m.ScopeType {
	case ScopeAgentPrivate:
		return sameID(m.ProjectID, other.ProjectID) && sameID(m.OwnerAgentID, other.OwnerAgentID)
	case ScopeTaskShared:
		return sameID(m.ProjectID, other.ProjectID) && strings.TrimSpace(m.TaskID) != "" && m.TaskID == other.TaskID
	case ScopeProjectShared:
		return sameID(m.ProjectID, other.ProjectID)
	case ScopeOrgShared, ScopeGlobalPublic:
		return true
	default:
		return false
	}
}
func sameID(a, b *uuid.UUID) bool { return a != nil && b != nil && *a != uuid.Nil && *a == *b }

func (a AuthContext) valid() bool {
	return a.TenantID != uuid.Nil && a.PrincipalID != uuid.Nil && slices.Contains([]PrincipalType{PrincipalUser, PrincipalService, PrincipalOrchestrator, PrincipalAgent}, a.PrincipalType)
}
func (a AuthContext) projectAllowed(projectID *uuid.UUID) bool {
	return projectID != nil && *projectID != uuid.Nil && slices.Contains(a.AllowedProjects, *projectID)
}

// CanWriteScope checks writes that do not require a task assignment.
// For task_shared, use AuthorizeTaskMemory with the current permission store.
func (a AuthContext) CanWriteScope(projectID *uuid.UUID, scope ScopeType, taskID string) error {
	if !a.valid() || !slices.Contains(a.AllowedScopes, scope) {
		return ErrForbidden
	}
	switch scope {
	case ScopeAgentPrivate:
		if !a.projectAllowed(projectID) || a.AgentID == nil || *a.AgentID == uuid.Nil {
			return ErrForbidden
		}
	case ScopeTaskShared:
		if !a.projectAllowed(projectID) {
			return ErrForbidden
		}
		if strings.TrimSpace(taskID) == "" {
			return ErrMissingTaskID
		}
		return ErrForbidden
	case ScopeProjectShared:
		if !a.projectAllowed(projectID) {
			return ErrForbidden
		}
	case ScopeOrgShared:
	case ScopeGlobalPublic:
		if !a.IsAdmin || (a.PrincipalType != PrincipalUser && a.PrincipalType != PrincipalOrchestrator) {
			return ErrForbidden
		}
	default:
		return ErrForbidden
	}
	return nil
}

// CanReadMemory checks visibility without a task permission store.
// For task_shared, use AuthorizeTaskMemory. A task selector cannot grant access.
func (a AuthContext) CanReadMemory(m MemoryScopeTarget, _ string) bool {
	if !a.valid() || m.TenantID != a.TenantID || !slices.Contains(a.AllowedScopes, m.ScopeType) {
		return false
	}
	switch m.ScopeType {
	case ScopeAgentPrivate:
		return a.projectAllowed(m.ProjectID) && a.AgentID != nil && *a.AgentID != uuid.Nil && m.OwnerAgentID != nil && *a.AgentID == *m.OwnerAgentID
	case ScopeProjectShared:
		return a.projectAllowed(m.ProjectID)
	case ScopeOrgShared, ScopeGlobalPublic:
		return true
	default:
		return false
	}
}
func (a AuthContext) CanAssignEpistemic(status string) error {
	if !a.valid() {
		return ErrForbidden
	}
	switch status {
	case "verified":
		if a.PrincipalType == PrincipalAgent {
			return ErrForbidden
		}
	case "observed", "reported", "inferred", "hypothesis":
	default:
		return ErrForbidden
	}
	return nil
}
