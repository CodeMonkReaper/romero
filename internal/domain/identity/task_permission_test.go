package identity_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/romero-project/romero/internal/domain/identity"
)

type permissionLookup func(context.Context, identity.TaskPermissionQuery) (bool, error)

func (f permissionLookup) HasTaskPermission(ctx context.Context, q identity.TaskPermissionQuery) (bool, error) {
	return f(ctx, q)
}

func taskFixture() (identity.AuthContext, identity.MemoryScopeTarget) {
	project := uuid.New()
	actor := identity.AuthContext{
		TenantID: uuid.New(), PrincipalID: uuid.New(), PrincipalType: identity.PrincipalOrchestrator,
		IsAdmin: true, AllowedProjects: []uuid.UUID{project}, AllowedScopes: []identity.ScopeType{identity.ScopeTaskShared},
	}
	return actor, identity.MemoryScopeTarget{TenantID: actor.TenantID, ProjectID: &project, ScopeType: identity.ScopeTaskShared, TaskID: "known"}
}

func TestKnownTaskIDWithoutAssignment(t *testing.T) {
	actor, target := taskFixture()
	if actor.CanReadMemory(target, "known") {
		t.Error("knowing the task ID granted read without an assignment")
	}
	if !errors.Is(actor.CanWriteScope(target.ProjectID, identity.ScopeTaskShared, "known"), identity.ErrForbidden) {
		t.Error("knowing the task ID granted write without an assignment")
	}
}

func TestTaskPermissionsAreIndependentAndCurrent(t *testing.T) {
	actor, target := taskFixture()
	actor.PrincipalType = identity.PrincipalAgent
	actor.IsAdmin = false
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	grants := map[identity.TaskPermissionQuery]bool{}
	store := permissionLookup(func(gotCtx context.Context, q identity.TaskPermissionQuery) (bool, error) {
		if gotCtx != ctx {
			t.Fatal("lookup lost the operation context")
		}
		return grants[q], nil
	})
	read := identity.TaskPermissionQuery{TenantID: actor.TenantID, PrincipalID: actor.PrincipalID, ProjectID: *target.ProjectID, TaskID: target.TaskID, Permission: identity.TaskRead}
	write := read
	write.Permission = identity.TaskWrite
	for _, pair := range [][2]bool{{false, false}, {true, false}, {false, true}, {true, true}, {false, false}} {
		grants[read], grants[write] = pair[0], pair[1]
		for _, q := range []identity.TaskPermissionQuery{read, write} {
			err := actor.AuthorizeTaskMemory(ctx, store, target, q.Permission)
			if grants[q] && err != nil || !grants[q] && !errors.Is(err, identity.ErrForbidden) {
				t.Fatalf("grants %v, %s: got %v", pair, q.Permission, err)
			}
		}
	}
}

func TestTaskPermissionRequiresMatchingBoundary(t *testing.T) {
	actor, target := taskFixture()
	for _, tc := range []struct {
		name   string
		change func(*identity.AuthContext, *identity.MemoryScopeTarget)
	}{
		{"invalid actor", func(a *identity.AuthContext, _ *identity.MemoryScopeTarget) { a.PrincipalID = uuid.Nil }},
		{"other tenant", func(_ *identity.AuthContext, m *identity.MemoryScopeTarget) { m.TenantID = uuid.New() }},
		{"other scope", func(_ *identity.AuthContext, m *identity.MemoryScopeTarget) {
			m.ScopeType = identity.ScopeProjectShared
		}},
		{"scope grant missing", func(a *identity.AuthContext, _ *identity.MemoryScopeTarget) { a.AllowedScopes = nil }},
		{"project grant missing", func(a *identity.AuthContext, _ *identity.MemoryScopeTarget) { a.AllowedProjects = nil }},
		{"missing project", func(_ *identity.AuthContext, m *identity.MemoryScopeTarget) { m.ProjectID = nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := permissionLookup(func(context.Context, identity.TaskPermissionQuery) (bool, error) {
				t.Fatal("lookup for an invalid boundary")
				return true, nil
			})
			a, m := actor, target
			tc.change(&a, &m)
			for _, p := range []identity.TaskPermission{identity.TaskRead, identity.TaskWrite} {
				if err := a.AuthorizeTaskMemory(context.Background(), store, m, p); !errors.Is(err, identity.ErrForbidden) {
					t.Errorf("%s: got %v", p, err)
				}
			}
		})
	}
}

func TestTaskGrantIsBoundToResourceProject(t *testing.T) {
	actor, target := taskFixture()
	firstProject, secondProject := *target.ProjectID, uuid.New()
	actor.AllowedProjects = append(actor.AllowedProjects, secondProject)
	for _, p := range []identity.TaskPermission{identity.TaskRead, identity.TaskWrite} {
		grant := identity.TaskPermissionQuery{TenantID: actor.TenantID, PrincipalID: actor.PrincipalID, ProjectID: firstProject, TaskID: target.TaskID, Permission: p}
		store := permissionLookup(func(_ context.Context, q identity.TaskPermissionQuery) (bool, error) { return q == grant, nil })
		target.ProjectID = &secondProject
		if err := actor.AuthorizeTaskMemory(context.Background(), store, target, p); !errors.Is(err, identity.ErrForbidden) {
			t.Fatalf("%s grant from another allowed project: %v", p, err)
		}
		grant.ProjectID = secondProject
		if err := actor.AuthorizeTaskMemory(context.Background(), store, target, p); err != nil {
			t.Fatalf("%s grant from resource project: %v", p, err)
		}
	}
}

func TestTaskPermissionFailure(t *testing.T) {
	actor, target := taskFixture()
	ctx := context.Background()
	if err := actor.AuthorizeTaskMemory(ctx, nil, target, identity.TaskRead); !errors.Is(err, identity.ErrTaskPermissionsUnavailable) {
		t.Fatalf("missing store: %v", err)
	}
	down := errors.New("database unavailable")
	store := permissionLookup(func(context.Context, identity.TaskPermissionQuery) (bool, error) { return true, down })
	if err := actor.AuthorizeTaskMemory(ctx, store, target, identity.TaskWrite); !errors.Is(err, identity.ErrTaskPermissionsUnavailable) {
		t.Fatalf("lookup failure: %v", err)
	}
	if err := actor.AuthorizeTaskMemory(ctx, store, target, "unknown"); !errors.Is(err, identity.ErrForbidden) {
		t.Fatalf("unknown permission: %v", err)
	}
	target.TaskID = "  "
	if err := actor.AuthorizeTaskMemory(ctx, store, target, identity.TaskWrite); !errors.Is(err, identity.ErrMissingTaskID) {
		t.Fatalf("empty task: %v", err)
	}
}

func TestLookupFailureCannotBeMistakenForPolicyDenial(t *testing.T) {
	actor, target := taskFixture()
	for _, cause := range []error{identity.ErrForbidden, identity.ErrNotFound, identity.ErrMissingTaskID} {
		store := permissionLookup(func(context.Context, identity.TaskPermissionQuery) (bool, error) {
			return false, fmt.Errorf("lookup: %w", cause)
		})
		err := actor.AuthorizeTaskMemory(context.Background(), store, target, identity.TaskRead)
		if !errors.Is(err, identity.ErrTaskPermissionsUnavailable) || errors.Is(err, cause) {
			t.Errorf("store error %v: got %v", cause, err)
		}
	}
}
