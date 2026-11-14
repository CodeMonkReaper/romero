package identity_test

import (
	"errors"
	"github.com/google/uuid"
	"github.com/romero-project/romero/internal/domain/identity"
	"testing"
)

func TestAuthorizationBoundary(t *testing.T) {
	tenant, principal, project, agent := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	a := identity.AuthContext{TenantID: tenant, PrincipalID: principal, PrincipalType: identity.PrincipalAgent, AgentID: &agent, AllowedProjects: []uuid.UUID{project}, AllowedScopes: []identity.ScopeType{identity.ScopeAgentPrivate, identity.ScopeTaskShared, identity.ScopeProjectShared, identity.ScopeOrgShared, identity.ScopeGlobalPublic}}
	if !errors.Is(a.CanAssignEpistemic("verified"), identity.ErrForbidden) {
		t.Error("agent may not self-verify")
	}
	if !errors.Is(a.CanWriteScope(&project, identity.ScopeTaskShared, ""), identity.ErrMissingTaskID) {
		t.Error("task scope requires task id")
	}
	if !errors.Is(a.CanWriteScope(nil, identity.ScopeGlobalPublic, ""), identity.ErrForbidden) {
		t.Error("public write requires admin")
	}
	m := identity.MemoryScopeTarget{TenantID: tenant, ProjectID: &project, ScopeType: identity.ScopeAgentPrivate, OwnerAgentID: &agent}
	if !a.CanReadMemory(m, "") {
		t.Error("owner must read private memory")
	}
}

func TestScopeMatrix(t *testing.T) {
	tenant, principal, project, otherProject, agent, otherAgent := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	for _, scope := range []identity.ScopeType{identity.ScopeAgentPrivate, identity.ScopeTaskShared, identity.ScopeProjectShared, identity.ScopeOrgShared, identity.ScopeGlobalPublic} {
		t.Run(string(scope), func(t *testing.T) {
			a := identity.AuthContext{TenantID: tenant, PrincipalID: principal, PrincipalType: identity.PrincipalUser, AgentID: &agent, AllowedProjects: []uuid.UUID{project}, AllowedScopes: []identity.ScopeType{scope}, IsAdmin: true}
			m := identity.MemoryScopeTarget{TenantID: tenant, ProjectID: &project, ScopeType: scope, OwnerAgentID: &agent, TaskID: "task"}
			if err := a.CanWriteScope(&project, scope, "task"); err != nil {
				t.Fatal(err)
			}
			if !a.CanReadMemory(m, "task") {
				t.Fatal("authorized read denied")
			}
			m.TenantID = uuid.New()
			if a.CanReadMemory(m, "task") {
				t.Error("cross tenant read allowed")
			}
			m.TenantID = tenant
			if scope == identity.ScopeAgentPrivate || scope == identity.ScopeTaskShared || scope == identity.ScopeProjectShared {
				m.ProjectID = &otherProject
				if a.CanReadMemory(m, "task") {
					t.Error("cross project read allowed")
				}
				m.ProjectID = &project
				if !errors.Is(a.CanWriteScope(&otherProject, scope, "task"), identity.ErrForbidden) {
					t.Error("cross project write allowed")
				}
			}
			if scope == identity.ScopeAgentPrivate {
				m.OwnerAgentID = &otherAgent
				if a.CanReadMemory(m, "task") {
					t.Error("cross agent read allowed")
				}
				m.OwnerAgentID = nil
				if a.CanReadMemory(m, "") {
					t.Error("nil owner allowed")
				}
				a.AgentID = nil
				if !errors.Is(a.CanWriteScope(&project, scope, "task"), identity.ErrForbidden) {
					t.Error("nil agent write allowed")
				}
			}
			if scope == identity.ScopeTaskShared {
				if a.CanReadMemory(m, "other") {
					t.Error("cross task read allowed")
				}
				m.TaskID = ""
				if a.CanReadMemory(m, "") {
					t.Error("blank task read allowed")
				}
			}
			a.AllowedScopes = nil
			if a.CanReadMemory(m, "task") || !errors.Is(a.CanWriteScope(&project, scope, "task"), identity.ErrForbidden) {
				t.Error("missing scope grant allowed")
			}
		})
	}
	zero := identity.AuthContext{}
	if zero.CanReadMemory(identity.MemoryScopeTarget{}, "") || !errors.Is(zero.CanWriteScope(nil, identity.ScopeOrgShared, ""), identity.ErrForbidden) || !errors.Is(zero.CanAssignEpistemic("observed"), identity.ErrForbidden) {
		t.Error("zero auth must deny")
	}
}

func TestPrincipalAuthority(t *testing.T) {
	for _, kind := range []identity.PrincipalType{identity.PrincipalUser, identity.PrincipalOrchestrator} {
		t.Run(string(kind)+"_without_admin", func(t *testing.T) {
			a := identity.AuthContext{TenantID: uuid.New(), PrincipalID: uuid.New(), PrincipalType: kind, AllowedScopes: []identity.ScopeType{identity.ScopeGlobalPublic}}
			if !errors.Is(a.CanWriteScope(nil, identity.ScopeGlobalPublic, ""), identity.ErrForbidden) {
				t.Fatal("public write requires admin even for privileged principal types")
			}
		})
	}
	for _, kind := range []identity.PrincipalType{identity.PrincipalUser, identity.PrincipalService, identity.PrincipalOrchestrator, identity.PrincipalAgent, "unknown"} {
		for _, status := range []string{"verified", "observed", "reported", "inferred", "hypothesis", "unknown"} {
			a := identity.AuthContext{TenantID: uuid.New(), PrincipalID: uuid.New(), PrincipalType: kind, IsAdmin: true, AllowedScopes: []identity.ScopeType{identity.ScopeGlobalPublic}}
			want := kind != "unknown" && status != "unknown" && (kind != identity.PrincipalAgent || status != "verified")
			if got := a.CanAssignEpistemic(status) == nil; got != want {
				t.Errorf("%s assigning %s: got %v want %v", kind, status, got, want)
			}
			adminAllowed := kind == identity.PrincipalUser || kind == identity.PrincipalOrchestrator
			if got := a.CanWriteScope(nil, identity.ScopeGlobalPublic, "") == nil; got != adminAllowed {
				t.Errorf("%s public write: %v", kind, got)
			}
		}
	}
}

func TestTopicPartitions(t *testing.T) {
	tenant, project, owner := uuid.New(), uuid.New(), uuid.New()
	for _, scope := range []identity.ScopeType{identity.ScopeAgentPrivate, identity.ScopeTaskShared, identity.ScopeProjectShared, identity.ScopeOrgShared, identity.ScopeGlobalPublic} {
		t.Run(string(scope), func(t *testing.T) {
			m := identity.MemoryScopeTarget{TenantID: tenant, ProjectID: &project, ScopeType: scope, OwnerAgentID: &owner, TaskID: "task"}
			other := m
			if !m.SameTopicPartition(other) {
				t.Error("same scope must compete")
			}
			other.TenantID = uuid.New()
			if m.SameTopicPartition(other) {
				t.Error("tenants cannot compete")
			}
			other = m
			other.ScopeType = "unknown"
			if m.SameTopicPartition(other) {
				t.Error("different scope cannot compete")
			}
			other = m
			p := uuid.New()
			other.ProjectID = &p
			want := scope == identity.ScopeOrgShared || scope == identity.ScopeGlobalPublic
			if m.SameTopicPartition(other) != want {
				t.Error("wrong project partition")
			}
			other = m
			o := uuid.New()
			other.OwnerAgentID = &o
			want = scope != identity.ScopeAgentPrivate
			if m.SameTopicPartition(other) != want {
				t.Error("wrong owner partition")
			}
			other = m
			other.TaskID = "other"
			want = scope != identity.ScopeTaskShared
			if m.SameTopicPartition(other) != want {
				t.Error("wrong task partition")
			}
		})
	}
	if (identity.MemoryScopeTarget{}).SameTopicPartition(identity.MemoryScopeTarget{}) {
		t.Error("zero target has no valid partition")
	}
}

func TestMalformedScopeDenies(t *testing.T) {
	a := identity.AuthContext{TenantID: uuid.New(), PrincipalID: uuid.New(), PrincipalType: identity.PrincipalAgent, AllowedScopes: []identity.ScopeType{"unknown", identity.ScopeAgentPrivate, identity.ScopeTaskShared, identity.ScopeProjectShared}}
	if a.CanReadMemory(identity.MemoryScopeTarget{TenantID: a.TenantID, ScopeType: "unknown"}, "") || !errors.Is(a.CanWriteScope(nil, "unknown", ""), identity.ErrForbidden) {
		t.Error("unknown scope must deny")
	}
	for _, scope := range []identity.ScopeType{identity.ScopeAgentPrivate, identity.ScopeTaskShared, identity.ScopeProjectShared} {
		m := identity.MemoryScopeTarget{TenantID: a.TenantID, ScopeType: scope}
		if a.CanReadMemory(m, "") || !errors.Is(a.CanWriteScope(nil, scope, "task"), identity.ErrForbidden) || m.SameTopicPartition(m) {
			t.Errorf("malformed %s must deny", scope)
		}
	}
}
