package retrieval_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/romero-project/romero/internal/application/retrieval"
	"github.com/romero-project/romero/internal/domain/identity"
	"github.com/romero-project/romero/internal/domain/memory"
	"github.com/romero-project/romero/internal/domain/provenance"
)

type fakeRetrievalStore struct {
	memories  map[uuid.UUID]memory.Memory
	versions  map[uuid.UUID][]memory.MemoryVersion
	relations map[uuid.UUID][]memory.MemoryRelation
	allowed   []memory.AuditEvent
	denied    []memory.AuditEvent
	failAudit bool
}

func newFakeRetrievalStore() *fakeRetrievalStore {
	return &fakeRetrievalStore{
		memories:  make(map[uuid.UUID]memory.Memory),
		versions:  make(map[uuid.UUID][]memory.MemoryVersion),
		relations: make(map[uuid.UUID][]memory.MemoryRelation),
	}
}

func (f *fakeRetrievalStore) ExecTx(_ context.Context, fn func(memory.RepositoryRegistry) error) error {
	return fn(&fakeReg{s: f})
}

func (f *fakeRetrievalStore) RecordAllowed(_ context.Context, ev memory.AuditEvent) error {
	if f.failAudit {
		return errors.New("audit down")
	}
	f.allowed = append(f.allowed, ev)
	return nil
}

func (f *fakeRetrievalStore) RecordDenied(_ context.Context, ev memory.AuditEvent) error {
	if f.failAudit {
		return errors.New("audit down")
	}
	f.denied = append(f.denied, ev)
	return nil
}

type fakeReg struct{ s *fakeRetrievalStore }

func (r *fakeReg) Sources() memory.SourceRepository    { return nil }
func (r *fakeReg) Memories() memory.MemoryRepository   { return r }
func (r *fakeReg) Feedback() memory.FeedbackRepository { return nil }
func (r *fakeReg) Jobs() memory.JobRepository          { return nil }
func (r *fakeReg) Audit() memory.AuditLogger           { return r.s }

func (r *fakeReg) GetMemoryByID(_ context.Context, tenantID, memoryID uuid.UUID) (*memory.Memory, error) {
	m, ok := r.s.memories[memoryID]
	if !ok || m.TenantID != tenantID {
		return nil, memory.ErrNotFound
	}
	cp := m
	return &cp, nil
}
func (r *fakeReg) GetMemoryByIdempotencyKey(_ context.Context, _ uuid.UUID, _ string) (*memory.Memory, error) {
	return nil, memory.ErrNotFound
}
func (r *fakeReg) LockActiveTopicMemory(_ context.Context, _ identity.MemoryScopeTarget, _ string) (*memory.Memory, error) {
	return nil, memory.ErrNotFound
}
func (r *fakeReg) InsertMemory(_ context.Context, _ memory.Memory) error      { return nil }
func (r *fakeReg) UpdateMemoryState(_ context.Context, _ memory.Memory) error { return nil }
func (r *fakeReg) ListVersionsByMemoryID(_ context.Context, memoryID uuid.UUID) ([]memory.MemoryVersion, error) {
	return append([]memory.MemoryVersion(nil), r.s.versions[memoryID]...), nil
}
func (r *fakeReg) InsertMemoryVersion(_ context.Context, _ memory.MemoryVersion) error { return nil }
func (r *fakeReg) CloseMemoryVersionValidity(_ context.Context, _ uuid.UUID, _ int, _ time.Time) error {
	return nil
}
func (r *fakeReg) GetMemoryVersionPointInTime(_ context.Context, memoryID uuid.UUID, validAt, knownAt time.Time) (*memory.MemoryVersion, error) {
	return memory.SelectVersionPointInTime(r.s.versions[memoryID], memory.PointInTimeQuery{ValidAt: validAt, KnownAt: knownAt})
}
func (r *fakeReg) ListActiveRelationsByMemoryID(_ context.Context, _, memoryID uuid.UUID) ([]memory.MemoryRelation, error) {
	return append([]memory.MemoryRelation(nil), r.s.relations[memoryID]...), nil
}
func (r *fakeReg) InsertMemoryRelation(_ context.Context, _ memory.MemoryRelation) error { return nil }
func (r *fakeReg) InsertSource(_ context.Context, _ provenance.MemorySource) error       { return nil }

type fakeTaskAccess struct {
	snap retrieval.TaskAccessSnapshot
	err  error
}

func (f fakeTaskAccess) EvaluateTaskAccess(_ context.Context, _ identity.TaskPermissionQuery) (retrieval.TaskAccessSnapshot, error) {
	return f.snap, f.err
}

type fakeOpaqueStatus struct {
	meta map[uuid.UUID]retrieval.OpaqueRequestMetadata
	err  error
}

func (f fakeOpaqueStatus) GetStatus(_ context.Context, tenantID, requestID uuid.UUID) (*retrieval.OpaqueRequestMetadata, error) {
	if f.err != nil {
		return nil, f.err
	}
	m, ok := f.meta[requestID]
	if !ok || m.TenantID != tenantID {
		return nil, retrieval.ErrNotFound
	}
	cp := m
	return &cp, nil
}

func TestGetMemory_CurrentAndPointInTimeAndXML(t *testing.T) {
	store := newFakeRetrievalStore()
	tenantID := uuid.New()
	projectID := uuid.New()
	memID := uuid.New()
	t1 := time.Date(2026, 11, 10, 10, 0, 0, 0, time.UTC)
	t2 := t1.Add(time.Hour)
	t3 := t2.Add(time.Hour)

	store.memories[memID] = memory.Memory{
		ID: memID, TenantID: tenantID, ProjectID: &projectID,
		Type: memory.TypeSemantic, ScopeType: identity.ScopeProjectShared, Status: memory.StatusActive,
		Summary: "v2 summary", Content: "v2 <secret>content</secret>", EpistemicStatus: memory.EpistemicObserved,
		Confidence: 0.85, Version: 2,
		Clocks: memory.TriTemporalWindow{ObservedAt: t1, CreatedAt: t1, ValidFrom: t2},
	}
	store.versions[memID] = []memory.MemoryVersion{
		{ID: uuid.New(), MemoryID: memID, Version: 1, Summary: "v1 summary", Content: "v1 content", EpistemicStatus: memory.EpistemicReported, AlphaScore: 5.25, BetaScore: 2.25, Status: memory.StatusActive, ValidFrom: t1, ValidUntil: &t2, CreatedAt: t1},
		{ID: uuid.New(), MemoryID: memID, Version: 2, Summary: "v2 summary", Content: "v2 <secret>content</secret>", EpistemicStatus: memory.EpistemicObserved, AlphaScore: 8.5, BetaScore: 1.5, Status: memory.StatusActive, ValidFrom: t2, CreatedAt: t2},
	}

	svc, err := retrieval.NewService(store, store, fakeTaskAccess{}, fakeOpaqueStatus{}, nil)
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}
	auth := identity.AuthContext{
		TenantID: tenantID, PrincipalID: uuid.New(), PrincipalType: identity.PrincipalUser,
		AllowedProjects: []uuid.UUID{projectID}, AllowedScopes: []identity.ScopeType{identity.ScopeProjectShared},
	}

	// Current mode
	cur, err := svc.GetMemory(context.Background(), auth, retrieval.GetMemoryQuery{MemoryID: memID})
	if err != nil {
		t.Fatalf("GetMemory current failed: %v", err)
	}
	if !strings.Contains(cur.UntrustedXMLEnvelope, `untrusted_content="true"`) || !strings.Contains(cur.UntrustedXMLEnvelope, "&lt;secret&gt;") {
		t.Fatalf("expected escaped untrusted XML envelope, got %q", cur.UntrustedXMLEnvelope)
	}

	// Point-in-time AsOf t1 -> v1
	pit1, err := svc.GetMemory(context.Background(), auth, retrieval.GetMemoryQuery{MemoryID: memID, AsOf: &t1})
	if err != nil {
		t.Fatalf("GetMemory AsOf t1 failed: %v", err)
	}
	if pit1.PointInTimeVersion == nil || pit1.PointInTimeVersion.Version != 1 {
		t.Fatalf("expected v1 at t1, got %+v", pit1.PointInTimeVersion)
	}

	// Point-in-time (ValidAt: t1, KnownAt: t3) -> v1
	pit2, err := svc.GetMemory(context.Background(), auth, retrieval.GetMemoryQuery{MemoryID: memID, ValidAt: t1, KnownAt: t3})
	if err != nil || pit2.PointInTimeVersion.Version != 1 {
		t.Fatalf("expected v1 for (t1, t3), got %+v err=%v", pit2, err)
	}

	// known_at < t1 -> ErrNotFound
	beforeT1 := t1.Add(-time.Minute)
	if _, err := svc.GetMemory(context.Background(), auth, retrieval.GetMemoryQuery{MemoryID: memID, ValidAt: t1, KnownAt: beforeT1}); !errors.Is(err, retrieval.ErrNotFound) {
		t.Fatalf("expected ErrNotFound before creation, got %v", err)
	}

	// Invalid temporal params (AsOf + ValidAt) -> ErrValidation
	if _, err := svc.GetMemory(context.Background(), auth, retrieval.GetMemoryQuery{MemoryID: memID, AsOf: &t1, ValidAt: t1}); !errors.Is(err, retrieval.ErrValidation) {
		t.Fatalf("expected ErrValidation for mixed temporal params, got %v", err)
	}
}

func TestGetMemory_TaskSharedFourCombinationsAndAuditFailClosed(t *testing.T) {
	tenantID := uuid.New()
	projectID := uuid.New()
	memID := uuid.New()
	auth := identity.AuthContext{
		TenantID: tenantID, PrincipalID: uuid.New(), PrincipalType: identity.PrincipalAgent,
		AllowedProjects: []uuid.UUID{projectID}, AllowedScopes: []identity.ScopeType{identity.ScopeTaskShared},
	}

	combos := []struct {
		name    string
		snap    retrieval.TaskAccessSnapshot
		wantErr error
	}{
		{"neither", retrieval.TaskAccessSnapshot{CanRead: false, CanWrite: false}, retrieval.ErrNotFound},
		{"write_only", retrieval.TaskAccessSnapshot{CanRead: false, CanWrite: true}, retrieval.ErrNotFound},
		{"read_only", retrieval.TaskAccessSnapshot{CanRead: true, CanWrite: false}, nil},
		{"read_write", retrieval.TaskAccessSnapshot{CanRead: true, CanWrite: true}, nil},
	}

	for _, tc := range combos {
		t.Run(tc.name, func(t *testing.T) {
			store := newFakeRetrievalStore()
			store.memories[memID] = memory.Memory{
				ID: memID, TenantID: tenantID, ProjectID: &projectID, TaskID: "t-1",
				Type: memory.TypeSemantic, ScopeType: identity.ScopeTaskShared, Status: memory.StatusActive,
				Summary: "s", Content: "c", EpistemicStatus: memory.EpistemicObserved, Confidence: 0.85, Version: 1,
			}
			svc, _ := retrieval.NewService(store, store, fakeTaskAccess{snap: tc.snap}, fakeOpaqueStatus{}, nil)
			_, err := svc.GetMemory(context.Background(), auth, retrieval.GetMemoryQuery{MemoryID: memID})
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("%s: want %v, got %v", tc.name, tc.wantErr, err)
			}
		})
	}

	// Audit failure -> ErrOperational (503) without leaking existence
	store := newFakeRetrievalStore()
	store.failAudit = true
	svc, _ := retrieval.NewService(store, store, fakeTaskAccess{}, fakeOpaqueStatus{}, nil)
	if _, err := svc.GetMemory(context.Background(), auth, retrieval.GetMemoryQuery{MemoryID: uuid.New()}); !errors.Is(err, retrieval.ErrOperational) {
		t.Fatalf("expected ErrOperational on audit failure, got %v", err)
	}
}

func TestGetRequestStatus_AuthorizationAndOpacity(t *testing.T) {
	tenantID := uuid.New()
	projectID := uuid.New()
	reqID := uuid.New()
	auth := identity.AuthContext{
		TenantID: tenantID, PrincipalID: uuid.New(), PrincipalType: identity.PrincipalAgent,
		AllowedProjects: []uuid.UUID{projectID}, AllowedScopes: []identity.ScopeType{identity.ScopeTaskShared},
	}
	statusStore := fakeOpaqueStatus{
		meta: map[uuid.UUID]retrieval.OpaqueRequestMetadata{
			reqID: {RequestID: reqID, TenantID: tenantID, ProjectID: projectID, TaskID: "t-1", Status: "pending"},
		},
	}

	// CanRead=true -> returns {RequestID, Status}
	store := newFakeRetrievalStore()
	svcOK, _ := retrieval.NewService(store, store, fakeTaskAccess{snap: retrieval.TaskAccessSnapshot{CanRead: true}}, statusStore, nil)
	st, err := svcOK.GetRequestStatus(context.Background(), auth, reqID, "corr-1")
	if err != nil || st.RequestID != reqID || st.Status != "pending" {
		t.Fatalf("unexpected GetRequestStatus result: %+v err=%v", st, err)
	}

	// CanRead=false -> ErrNotFound
	svcDeny, _ := retrieval.NewService(store, store, fakeTaskAccess{snap: retrieval.TaskAccessSnapshot{CanRead: false, CanWrite: true}}, statusStore, nil)
	if _, err := svcDeny.GetRequestStatus(context.Background(), auth, reqID, ""); !errors.Is(err, retrieval.ErrNotFound) {
		t.Fatalf("expected ErrNotFound without read permission, got %v", err)
	}

	// Audit failure -> ErrOperational
	store.failAudit = true
	if _, err := svcDeny.GetRequestStatus(context.Background(), auth, uuid.New(), ""); !errors.Is(err, retrieval.ErrOperational) {
		t.Fatalf("expected ErrOperational on audit failure, got %v", err)
	}
}

func TestRetrievalService_ValidationAndErrorEdges(t *testing.T) {
	store := newFakeRetrievalStore()
	if _, err := retrieval.NewService(nil, store, nil, nil, nil); err == nil {
		t.Fatal("expected error for nil uow")
	}
	if _, err := retrieval.NewService(store, nil, nil, nil, nil); err == nil {
		t.Fatal("expected error for nil audit")
	}

	svc, _ := retrieval.NewService(store, store, fakeTaskAccess{err: errors.New("db down")}, fakeOpaqueStatus{err: errors.New("db down")}, nil)
	tenantID := uuid.New()
	projectID := uuid.New()
	auth := identity.AuthContext{
		TenantID: tenantID, PrincipalID: uuid.New(), PrincipalType: identity.PrincipalUser,
		AllowedProjects: []uuid.UUID{projectID}, AllowedScopes: []identity.ScopeType{identity.ScopeTaskShared, identity.ScopeProjectShared},
	}

	if _, err := svc.GetMemory(context.Background(), identity.AuthContext{}, retrieval.GetMemoryQuery{MemoryID: uuid.New()}); !errors.Is(err, retrieval.ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized, got %v", err)
	}
	if _, err := svc.GetMemory(context.Background(), auth, retrieval.GetMemoryQuery{}); !errors.Is(err, retrieval.ErrValidation) {
		t.Fatalf("expected ErrValidation for nil MemoryID, got %v", err)
	}
	if _, err := svc.GetRequestStatus(context.Background(), identity.AuthContext{}, uuid.New(), ""); !errors.Is(err, retrieval.ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized, got %v", err)
	}
	if _, err := svc.GetRequestStatus(context.Background(), auth, uuid.Nil, ""); !errors.Is(err, retrieval.ErrValidation) {
		t.Fatalf("expected ErrValidation for nil RequestID, got %v", err)
	}
	if _, err := svc.GetRequestStatus(context.Background(), auth, uuid.New(), ""); !errors.Is(err, retrieval.ErrOperational) {
		t.Fatalf("expected ErrOperational on status store error, got %v", err)
	}

	// Task_shared memory with TaskAccessChecker error -> ErrOperational
	memID := uuid.New()
	store.memories[memID] = memory.Memory{
		ID: memID, TenantID: tenantID, ProjectID: &projectID, TaskID: "t-1",
		Type: memory.TypeSemantic, ScopeType: identity.ScopeTaskShared, Status: memory.StatusActive,
		Summary: "s", Content: "c", EpistemicStatus: memory.EpistemicObserved, Confidence: 0.85, Version: 1,
	}
	if _, err := svc.GetMemory(context.Background(), auth, retrieval.GetMemoryQuery{MemoryID: memID}); !errors.Is(err, retrieval.ErrOperational) {
		t.Fatalf("expected ErrOperational on task access error, got %v", err)
	}

	// Cross-tenant memory -> ErrNotFound
	otherAuth := auth
	otherAuth.TenantID = uuid.New()
	if _, err := svc.GetMemory(context.Background(), otherAuth, retrieval.GetMemoryQuery{MemoryID: memID}); !errors.Is(err, retrieval.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-tenant read, got %v", err)
	}

	// Cross-project status check -> ErrNotFound
	reqID := uuid.New()
	svcStatus, _ := retrieval.NewService(store, store, fakeTaskAccess{snap: retrieval.TaskAccessSnapshot{CanRead: true}}, fakeOpaqueStatus{
		meta: map[uuid.UUID]retrieval.OpaqueRequestMetadata{
			reqID: {RequestID: reqID, TenantID: tenantID, ProjectID: uuid.New(), TaskID: "t-1", Status: "completed"},
		},
	}, nil)
	if _, err := svcStatus.GetRequestStatus(context.Background(), auth, reqID, ""); !errors.Is(err, retrieval.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unauthorized project status, got %v", err)
	}
}

func TestRetrievalService_CoverageEdges(t *testing.T) {
	store := newFakeRetrievalStore()
	tenantID := uuid.New()
	projectID := uuid.New()
	auth := identity.AuthContext{
		TenantID: tenantID, PrincipalID: uuid.New(), PrincipalType: identity.PrincipalUser,
		AllowedProjects: []uuid.UUID{projectID}, AllowedScopes: []identity.ScopeType{identity.ScopeTaskShared, identity.ScopeProjectShared},
	}

	// opaqueStatus == nil -> ErrOperational
	svcNilOpaque, _ := retrieval.NewService(store, store, nil, nil, nil)
	if _, err := svcNilOpaque.GetRequestStatus(context.Background(), auth, uuid.New(), ""); !errors.Is(err, retrieval.ErrOperational) {
		t.Fatalf("expected ErrOperational when opaqueStatus is nil, got %v", err)
	}

	// TaskAccessChecker == nil on task_shared memory -> ErrOperational
	memID := uuid.New()
	store.memories[memID] = memory.Memory{
		ID: memID, TenantID: tenantID, ProjectID: &projectID, TaskID: "t-1",
		Type: memory.TypeSemantic, ScopeType: identity.ScopeTaskShared, Status: memory.StatusActive,
		Summary: "s", Content: "c", EpistemicStatus: memory.EpistemicObserved, Confidence: 0.85, Version: 1,
	}
	if _, err := svcNilOpaque.GetMemory(context.Background(), auth, retrieval.GetMemoryQuery{MemoryID: memID}); !errors.Is(err, retrieval.ErrOperational) {
		t.Fatalf("expected ErrOperational when access is nil for task_shared memory, got %v", err)
	}

	// TaskAccessChecker == nil on GetRequestStatus -> ErrOperational
	reqID := uuid.New()
	svcNilAccess, _ := retrieval.NewService(store, store, nil, fakeOpaqueStatus{
		meta: map[uuid.UUID]retrieval.OpaqueRequestMetadata{
			reqID: {RequestID: reqID, TenantID: tenantID, ProjectID: projectID, TaskID: "t-1", Status: "running"},
		},
	}, nil)
	if _, err := svcNilAccess.GetRequestStatus(context.Background(), auth, reqID, ""); !errors.Is(err, retrieval.ErrOperational) {
		t.Fatalf("expected ErrOperational when access is nil for GetRequestStatus, got %v", err)
	}

	// TaskAccessChecker error on GetRequestStatus -> ErrOperational
	svcAccessErr, _ := retrieval.NewService(store, store, fakeTaskAccess{err: errors.New("db down")}, fakeOpaqueStatus{
		meta: map[uuid.UUID]retrieval.OpaqueRequestMetadata{
			reqID: {RequestID: reqID, TenantID: tenantID, ProjectID: projectID, TaskID: "t-1", Status: "running"},
		},
	}, nil)
	if _, err := svcAccessErr.GetRequestStatus(context.Background(), auth, reqID, ""); !errors.Is(err, retrieval.ErrOperational) {
		t.Fatalf("expected ErrOperational on task access error in GetRequestStatus, got %v", err)
	}

	// RecordAllowed failure -> error
	projMemID := uuid.New()
	store.memories[projMemID] = memory.Memory{
		ID: projMemID, TenantID: tenantID, ProjectID: &projectID,
		Type: memory.TypeSemantic, ScopeType: identity.ScopeProjectShared, Status: memory.StatusActive,
		Summary: "s", Content: "c", EpistemicStatus: memory.EpistemicObserved, Confidence: 0.85, Version: 1,
	}
	store.failAudit = true
	svcFailAudit, _ := retrieval.NewService(store, store, fakeTaskAccess{snap: retrieval.TaskAccessSnapshot{CanRead: true}}, fakeOpaqueStatus{
		meta: map[uuid.UUID]retrieval.OpaqueRequestMetadata{
			reqID: {RequestID: reqID, TenantID: tenantID, ProjectID: projectID, TaskID: "t-1", Status: "running"},
		},
	}, nil)
	if _, err := svcFailAudit.GetMemory(context.Background(), auth, retrieval.GetMemoryQuery{MemoryID: projMemID}); err == nil {
		t.Fatal("expected error when RecordAllowed fails")
	}
	if _, err := svcFailAudit.GetRequestStatus(context.Background(), auth, reqID, ""); !errors.Is(err, retrieval.ErrOperational) {
		t.Fatalf("expected ErrOperational when RecordAllowed fails in GetRequestStatus, got %v", err)
	}
}

func TestGetMemory_FiltersInvisibleRelations_CurrentAndPointInTime(t *testing.T) {
	t.Parallel()
	tenantID := uuid.New()
	projectID := uuid.New()
	callerAgent := uuid.New()
	otherAgent := uuid.New()
	sourceID := uuid.New()
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)

	m1 := memory.Memory{
		ID: uuid.New(), TenantID: tenantID, ProjectID: &projectID,
		Type: memory.TypeSemantic, ScopeType: identity.ScopeProjectShared, Status: memory.StatusActive,
		Summary: "shared node", Content: "shared body", EpistemicStatus: memory.EpistemicObserved,
		Confidence: 0.9, PrimarySourceID: sourceID, Version: 2,
	}
	m2 := memory.Memory{
		ID: uuid.New(), TenantID: tenantID, ProjectID: &projectID,
		Type: memory.TypeSemantic, ScopeType: identity.ScopeProjectShared, Status: memory.StatusActive,
		Summary: "visible peer", Content: "visible peer body", EpistemicStatus: memory.EpistemicObserved,
		Confidence: 0.85, PrimarySourceID: sourceID, Version: 1,
	}
	m3 := memory.Memory{
		ID: uuid.New(), TenantID: tenantID, ProjectID: &projectID, OwnerAgentID: &otherAgent,
		Type: memory.TypeSemantic, ScopeType: identity.ScopeAgentPrivate, Status: memory.StatusActive,
		Summary: "private other agent", Content: "secret body", EpistemicStatus: memory.EpistemicObserved,
		Confidence: 0.95, PrimarySourceID: sourceID, Version: 1,
	}

	store := newFakeRetrievalStore()
	store.memories[m1.ID] = m1
	store.memories[m2.ID] = m2
	store.memories[m3.ID] = m3
	store.versions[m1.ID] = []memory.MemoryVersion{
		{ID: uuid.New(), MemoryID: m1.ID, Version: 1, Summary: "v1", Content: "v1 body", EpistemicStatus: memory.EpistemicReported, AlphaScore: 3, BetaScore: 1, Status: memory.StatusActive, ValidFrom: now.Add(-2 * time.Hour), CreatedAt: now.Add(-2 * time.Hour)},
		{ID: uuid.New(), MemoryID: m1.ID, Version: 2, Summary: "v2", Content: "v2 body", EpistemicStatus: memory.EpistemicObserved, AlphaScore: 9, BetaScore: 1, Status: memory.StatusActive, ValidFrom: now.Add(-10 * time.Minute), CreatedAt: now.Add(-10 * time.Minute)},
	}
	relVisible := memory.MemoryRelation{
		ID: uuid.New(), TenantID: tenantID, FromMemoryID: m1.ID, ToMemoryID: m2.ID,
		RelationType: memory.RelationSupports, Weight: 0.9, IsActive: true,
	}
	relHidden := memory.MemoryRelation{
		ID: uuid.New(), TenantID: tenantID, FromMemoryID: m1.ID, ToMemoryID: m3.ID,
		RelationType: memory.RelationDependsOn, Weight: 0.8, IsActive: true,
	}
	store.relations[m1.ID] = []memory.MemoryRelation{relVisible, relHidden}

	svc, err := retrieval.NewService(store, store, fakeTaskAccess{snap: retrieval.TaskAccessSnapshot{CanRead: false}}, nil, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	auth := identity.AuthContext{
		TenantID:        tenantID,
		PrincipalID:     uuid.New(),
		AgentID:         &callerAgent,
		PrincipalType:   identity.PrincipalAgent,
		AllowedProjects: []uuid.UUID{projectID},
		AllowedScopes:   []identity.ScopeType{identity.ScopeAgentPrivate, identity.ScopeProjectShared},
	}

	// Current mode: only relVisible is returned; relHidden (pointing to otherAgent's private memory) is omitted.
	cur, err := svc.GetMemory(context.Background(), auth, retrieval.GetMemoryQuery{MemoryID: m1.ID})
	if err != nil {
		t.Fatalf("GetMemory current: %v", err)
	}
	if len(cur.Relations) != 1 || cur.Relations[0].ID != relVisible.ID {
		t.Fatalf("expected only 1 visible relation (%s), got %+v", relVisible.ID, cur.Relations)
	}
	if len(cur.Versions) != 2 {
		t.Fatalf("expected 2 versions in current mode, got %d", len(cur.Versions))
	}

	// Point-in-Time mode: Versions filtered to CreatedAt <= asOf, and Relations still filtered to visible endpoints.
	asOf := now.Add(-1 * time.Hour)
	pit, err := svc.GetMemory(context.Background(), auth, retrieval.GetMemoryQuery{MemoryID: m1.ID, AsOf: &asOf})
	if err != nil {
		t.Fatalf("GetMemory Point-in-Time: %v", err)
	}
	if len(pit.Versions) != 1 || pit.Versions[0].Version != 1 {
		t.Fatalf("expected 1 version <= asOf (v1), got %+v", pit.Versions)
	}
	if len(pit.Relations) != 1 || pit.Relations[0].ID != relVisible.ID {
		t.Fatalf("expected only 1 visible relation (%s) in Point-in-Time mode, got %+v", relVisible.ID, pit.Relations)
	}
}
