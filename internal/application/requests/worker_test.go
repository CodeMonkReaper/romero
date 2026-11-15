package requests_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/romero-project/romero/internal/application/capture"
	"github.com/romero-project/romero/internal/application/feedback"
	"github.com/romero-project/romero/internal/application/requests"
	"github.com/romero-project/romero/internal/domain/identity"
	"github.com/romero-project/romero/internal/domain/memory"
	"github.com/romero-project/romero/internal/domain/provenance"
)

type fakeQueue struct {
	mu           sync.Mutex
	records      map[uuid.UUID]requests.OpaqueRequestRecord
	order        []uuid.UUID
	lease        *requests.Lease
	renewOK      bool
	renewErr     error
	executeErr   error
	failures     []failureCall
	purgedCount  int64
	purgeRetSeen time.Duration
	memories     map[uuid.UUID]memory.Memory
	taskPermOK   bool
	taskPermErr  error
}

type failureCall struct {
	lease requests.Lease
	class requests.FailureClass
	code  string
	msg   string
}

func newFakeQueue() *fakeQueue {
	return &fakeQueue{
		records:    make(map[uuid.UUID]requests.OpaqueRequestRecord),
		memories:   make(map[uuid.UUID]memory.Memory),
		renewOK:    true,
		taskPermOK: true,
	}
}

func (q *fakeQueue) Claim(_ context.Context) (*requests.Lease, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.order) == 0 {
		return nil, nil
	}
	id := q.order[0]
	q.order = q.order[1:]
	l := &requests.Lease{RequestID: id, Token: uuid.New(), ExpiresAt: time.Now().Add(60 * time.Second)}
	q.lease = l
	return l, nil
}

func (q *fakeQueue) Renew(_ context.Context, _ requests.Lease) (bool, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.renewOK, q.renewErr
}

func (q *fakeQueue) Execute(ctx context.Context, lease requests.Lease, apply requests.ApplyMutation) error {
	q.mu.Lock()
	if q.executeErr != nil {
		err := q.executeErr
		q.mu.Unlock()
		return err
	}
	rec := q.records[lease.RequestID]
	q.mu.Unlock()
	_, err := apply(ctx, &fakeReg{q: q}, q, rec)
	return err
}

func (q *fakeQueue) RecordFailure(_ context.Context, lease requests.Lease, class requests.FailureClass, code, msg string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.failures = append(q.failures, failureCall{lease: lease, class: class, code: code, msg: msg})
	return nil
}

func (q *fakeQueue) PurgeTerminal(_ context.Context, retention time.Duration) (int64, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.purgeRetSeen = retention
	return q.purgedCount, nil
}

func (q *fakeQueue) HasTaskPermission(_ context.Context, _ identity.TaskPermissionQuery) (bool, error) {
	if q.taskPermErr != nil {
		return false, q.taskPermErr
	}
	return q.taskPermOK, nil
}

type fakeReg struct{ q *fakeQueue }

func (r *fakeReg) Sources() memory.SourceRepository    { return nil }
func (r *fakeReg) Memories() memory.MemoryRepository   { return r }
func (r *fakeReg) Feedback() memory.FeedbackRepository { return nil }
func (r *fakeReg) Jobs() memory.JobRepository          { return nil }
func (r *fakeReg) Audit() memory.AuditLogger           { return nil }

func (r *fakeReg) GetMemoryByID(_ context.Context, tenantID, memoryID uuid.UUID) (*memory.Memory, error) {
	m, ok := r.q.memories[memoryID]
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
func (r *fakeReg) ListVersionsByMemoryID(_ context.Context, _ uuid.UUID) ([]memory.MemoryVersion, error) {
	return nil, nil
}
func (r *fakeReg) InsertMemoryVersion(_ context.Context, _ memory.MemoryVersion) error { return nil }
func (r *fakeReg) CloseMemoryVersionValidity(_ context.Context, _ uuid.UUID, _ int, _ time.Time) error {
	return nil
}
func (r *fakeReg) GetMemoryVersionPointInTime(_ context.Context, _ uuid.UUID, _, _ time.Time) (*memory.MemoryVersion, error) {
	return nil, memory.ErrNotFound
}
func (r *fakeReg) ListActiveRelationsByMemoryID(_ context.Context, _, _ uuid.UUID) ([]memory.MemoryRelation, error) {
	return nil, nil
}
func (r *fakeReg) InsertMemoryRelation(_ context.Context, _ memory.MemoryRelation) error { return nil }
func (r *fakeReg) InsertSource(_ context.Context, _ provenance.MemorySource) error       { return nil }

type fakePrincipals struct {
	auth identity.AuthContext
	err  error
}

func (p fakePrincipals) ResolveAuthContext(_ context.Context, _, _ uuid.UUID) (identity.AuthContext, error) {
	return p.auth, p.err
}

type fakeExecutor struct {
	called bool
	err    error
}

func (e *fakeExecutor) ExecuteOpaqueOperationTx(_ context.Context, _ memory.RepositoryRegistry, _ identity.AuthContext, _ requests.OpaqueOperation, _ *uuid.UUID, _ json.RawMessage) (*uuid.UUID, error) {
	e.called = true
	if e.err != nil {
		return nil, e.err
	}
	id := uuid.New()
	return &id, nil
}

func TestWorker_EmptyQueueAndHappyPath(t *testing.T) {
	q := newFakeQueue()
	tenantID := uuid.New()
	projectID := uuid.New()
	principalID := uuid.New()
	exec := &fakeExecutor{}
	w, err := requests.NewWorker(q, fakePrincipals{auth: identity.AuthContext{
		TenantID: tenantID, PrincipalID: principalID, PrincipalType: identity.PrincipalAgent,
		AllowedProjects: []uuid.UUID{projectID}, AllowedScopes: []identity.ScopeType{identity.ScopeTaskShared},
	}}, exec, requests.Config{})
	if err != nil {
		t.Fatalf("NewWorker failed: %v", err)
	}

	// Empty queue
	processed, err := w.ProcessOne(context.Background())
	if err != nil || processed {
		t.Fatalf("expected (false, nil) on empty queue, got (%v, %v)", processed, err)
	}

	// Happy path capture
	reqID := uuid.New()
	q.records[reqID] = requests.OpaqueRequestRecord{
		RequestID: reqID, TenantID: tenantID, ProjectID: projectID, TaskID: "t-1",
		PrincipalID: principalID, Operation: requests.OpaqueOpCapture, Payload: json.RawMessage(`{}`),
	}
	q.order = append(q.order, reqID)

	processed, err = w.ProcessOne(context.Background())
	if err != nil || !processed || !exec.called || len(q.failures) != 0 {
		t.Fatalf("expected successful processing, got processed=%v err=%v called=%v failures=%d", processed, err, exec.called, len(q.failures))
	}
}

func TestWorker_PermissionRevokedAndTargetScopeMismatchArePermanent(t *testing.T) {
	q := newFakeQueue()
	tenantID := uuid.New()
	projectID := uuid.New()
	principalID := uuid.New()
	exec := &fakeExecutor{}
	w, _ := requests.NewWorker(q, fakePrincipals{auth: identity.AuthContext{
		TenantID: tenantID, PrincipalID: principalID, PrincipalType: identity.PrincipalAgent,
		AllowedProjects: []uuid.UUID{projectID}, AllowedScopes: []identity.ScopeType{identity.ScopeTaskShared},
	}}, exec, requests.Config{})

	// 1. Revoked task permission -> FailurePermanent
	q.taskPermOK = false
	req1 := uuid.New()
	q.records[req1] = requests.OpaqueRequestRecord{
		RequestID: req1, TenantID: tenantID, ProjectID: projectID, TaskID: "t-1",
		PrincipalID: principalID, Operation: requests.OpaqueOpCapture, Payload: json.RawMessage(`{}`),
	}
	q.order = append(q.order, req1)
	if _, err := w.ProcessOne(context.Background()); err != nil {
		t.Fatalf("expected ProcessOne to record failure and return nil, got %v", err)
	}
	if len(q.failures) != 1 || q.failures[0].class != requests.FailurePermanent || exec.called {
		t.Fatalf("expected FailurePermanent without executing, got %+v", q.failures)
	}

	// 2. Target memory belongs to a different task -> FailurePermanent
	q.taskPermOK = true
	memID := uuid.New()
	q.memories[memID] = memory.Memory{
		ID: memID, TenantID: tenantID, ProjectID: &projectID, TaskID: "other-task",
		ScopeType: identity.ScopeTaskShared, Status: memory.StatusActive,
	}
	req2 := uuid.New()
	q.records[req2] = requests.OpaqueRequestRecord{
		RequestID: req2, TenantID: tenantID, ProjectID: projectID, TaskID: "t-1",
		PrincipalID: principalID, Operation: requests.OpaqueOpUpdate, TargetMemoryID: &memID, Payload: json.RawMessage(`{}`),
	}
	q.order = append(q.order, req2)
	if _, err := w.ProcessOne(context.Background()); err != nil {
		t.Fatalf("expected ProcessOne to record permanent failure, got %v", err)
	}
	if len(q.failures) != 2 || q.failures[1].class != requests.FailurePermanent {
		t.Fatalf("expected FailurePermanent for cross-task target, got %+v", q.failures)
	}
}

func TestWorker_TransientVsConflictAndLeaseLost(t *testing.T) {
	q := newFakeQueue()
	tenantID := uuid.New()
	projectID := uuid.New()
	principalID := uuid.New()
	exec := &fakeExecutor{err: errors.New("temporary connection reset")}
	w, _ := requests.NewWorker(q, fakePrincipals{auth: identity.AuthContext{
		TenantID: tenantID, PrincipalID: principalID, PrincipalType: identity.PrincipalAgent,
		AllowedProjects: []uuid.UUID{projectID}, AllowedScopes: []identity.ScopeType{identity.ScopeTaskShared},
	}}, exec, requests.Config{})

	// 1. Transient error -> FailureTransient
	req1 := uuid.New()
	q.records[req1] = requests.OpaqueRequestRecord{
		RequestID: req1, TenantID: tenantID, ProjectID: projectID, TaskID: "t-1",
		PrincipalID: principalID, Operation: requests.OpaqueOpCapture, Payload: json.RawMessage(`{}`),
	}
	q.order = append(q.order, req1)
	if _, err := w.ProcessOne(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(q.failures) != 1 || q.failures[0].class != requests.FailureTransient {
		t.Fatalf("expected FailureTransient, got %+v", q.failures)
	}

	// 2. Domain conflict -> FailurePermanent
	exec.err = capture.ErrConflict
	req2 := uuid.New()
	q.records[req2] = q.records[req1]
	q.order = append(q.order, req2)
	if _, err := w.ProcessOne(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(q.failures) != 2 || q.failures[1].class != requests.FailurePermanent {
		t.Fatalf("expected FailurePermanent for ErrConflict, got %+v", q.failures)
	}

	// 3. ErrLeaseLost -> does NOT call RecordFailure
	q.executeErr = requests.ErrLeaseLost
	req3 := uuid.New()
	q.records[req3] = q.records[req1]
	q.order = append(q.order, req3)
	if _, err := w.ProcessOne(context.Background()); err != nil {
		t.Fatalf("unexpected error on lease lost: %v", err)
	}
	if len(q.failures) != 2 {
		t.Fatalf("expected no RecordFailure on ErrLeaseLost, got %d", len(q.failures))
	}
}

func TestWorker_RunPurgeAndCleanShutdown(t *testing.T) {
	q := newFakeQueue()
	q.purgedCount = 5
	w, _ := requests.NewWorker(q, nil, &fakeExecutor{}, requests.Config{
		PollInterval:  10 * time.Millisecond,
		RenewInterval: 20 * time.Millisecond,
		PurgeInterval: 15 * time.Millisecond,
		Retention:     30 * 24 * time.Hour,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()
	if err := w.Run(ctx); err != nil && !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		t.Fatalf("Run failed: %v", err)
	}
	if q.purgeRetSeen != 30*24*time.Hour {
		t.Fatalf("expected 30d purge retention, got %v", q.purgeRetSeen)
	}
}

type fullTestReg struct {
	*fakeReg
	memories map[uuid.UUID]memory.Memory
	versions map[uuid.UUID][]memory.MemoryVersion
}

func (r *fullTestReg) Sources() memory.SourceRepository    { return r }
func (r *fullTestReg) Memories() memory.MemoryRepository   { return r }
func (r *fullTestReg) Feedback() memory.FeedbackRepository { return r }
func (r *fullTestReg) Jobs() memory.JobRepository          { return r }
func (r *fullTestReg) Audit() memory.AuditLogger           { return r }
func (r *fullTestReg) ExecTx(_ context.Context, fn func(memory.RepositoryRegistry) error) error {
	return fn(r)
}
func (r *fullTestReg) RecordAllowed(_ context.Context, _ memory.AuditEvent) error { return nil }
func (r *fullTestReg) RecordDenied(_ context.Context, _ memory.AuditEvent) error  { return nil }
func (r *fullTestReg) InsertSource(_ context.Context, _ provenance.MemorySource) error {
	return nil
}
func (r *fullTestReg) InsertMemory(_ context.Context, m memory.Memory) error {
	r.memories[m.ID] = m
	return nil
}
func (r *fullTestReg) UpdateMemoryState(_ context.Context, m memory.Memory) error {
	r.memories[m.ID] = m
	return nil
}
func (r *fullTestReg) GetMemoryByID(_ context.Context, tenantID, id uuid.UUID) (*memory.Memory, error) {
	m, ok := r.memories[id]
	if !ok || m.TenantID != tenantID {
		return nil, memory.ErrNotFound
	}
	cp := m
	return &cp, nil
}
func (r *fullTestReg) InsertMemoryVersion(_ context.Context, v memory.MemoryVersion) error {
	r.versions[v.MemoryID] = append(r.versions[v.MemoryID], v)
	return nil
}
func (r *fullTestReg) CloseMemoryVersionValidity(_ context.Context, _ uuid.UUID, _ int, _ time.Time) error {
	return nil
}
func (r *fullTestReg) InsertMemoryFeedback(_ context.Context, _ memory.MemoryFeedback) error {
	return nil
}
func (r *fullTestReg) EnqueueMemoryJob(_ context.Context, _ memory.MemoryJob) error { return nil }
func (r *fullTestReg) InsertMemoryRelation(_ context.Context, _ memory.MemoryRelation) error {
	return nil
}

func TestServiceExecutor_AllOpaqueOperations(t *testing.T) {
	reg := &fullTestReg{
		fakeReg:  &fakeReg{q: newFakeQueue()},
		memories: make(map[uuid.UUID]memory.Memory),
		versions: make(map[uuid.UUID][]memory.MemoryVersion),
	}
	capSvc, _ := capture.NewService(reg, reg, nil, nil, nil)
	fbSvc, _ := feedback.NewService(reg, reg, nil, nil, capSvc, nil)
	exec := &requests.ServiceExecutor{Capture: capSvc, Feedback: fbSvc}

	tenantID := uuid.New()
	projectID := uuid.New()
	auth := identity.AuthContext{
		TenantID: tenantID, PrincipalID: uuid.New(), PrincipalType: identity.PrincipalAgent,
		AllowedProjects: []uuid.UUID{projectID}, AllowedScopes: []identity.ScopeType{identity.ScopeTaskShared},
	}

	capPayload, _ := json.Marshal(map[string]any{
		"capture": capture.CaptureCommand{
			ProjectID: &projectID, TaskID: "t-1", Type: memory.TypeSemantic,
			ScopeType: identity.ScopeTaskShared, Summary: "sum", Content: "cont",
			EpistemicStatus: memory.EpistemicObserved, Importance: 0.7,
			SourceType: provenance.SourceAgentExecution, ExternalSourceID: "ext-1",
		},
	})
	id1, err := exec.ExecuteOpaqueOperationTx(context.Background(), reg, auth, requests.OpaqueOpCapture, nil, capPayload)
	if err != nil || id1 == nil {
		t.Fatalf("OpaqueOpCapture failed: id=%v err=%v", id1, err)
	}

	updPayload, _ := json.Marshal(capture.UpdateCommand{Summary: "sum2", Content: "cont2"})
	if _, err := exec.ExecuteOpaqueOperationTx(context.Background(), reg, auth, requests.OpaqueOpUpdate, id1, updPayload); err != nil {
		t.Fatalf("OpaqueOpUpdate failed: %v", err)
	}

	archPayload, _ := json.Marshal(capture.ArchiveCommand{Reason: "done"})
	if _, err := exec.ExecuteOpaqueOperationTx(context.Background(), reg, auth, requests.OpaqueOpArchive, id1, archPayload); err != nil {
		t.Fatalf("OpaqueOpArchive failed: %v", err)
	}
}

type feedbackAccessDenyRead struct{}

func (feedbackAccessDenyRead) EvaluateTaskAccess(_ context.Context, _ identity.TaskPermissionQuery) (feedback.TaskAccessSnapshot, error) {
	return feedback.TaskAccessSnapshot{CanRead: false, CanWrite: true}, nil
}

func TestServiceExecutor_OpaqueSupersedeFeedbackAndRenewalCancel(t *testing.T) {
	reg := &fullTestReg{
		fakeReg:  &fakeReg{q: newFakeQueue()},
		memories: make(map[uuid.UUID]memory.Memory),
		versions: make(map[uuid.UUID][]memory.MemoryVersion),
	}
	capSvc, _ := capture.NewService(reg, reg, nil, nil, nil)
	// Feedback service has TaskAccessChecker that returns CanRead=false, CanWrite=true
	fbSvc, _ := feedback.NewService(reg, reg, feedbackAccessDenyRead{}, nil, capSvc, nil)
	exec := &requests.ServiceExecutor{Capture: capSvc, Feedback: fbSvc}

	tenantID := uuid.New()
	projectID := uuid.New()
	auth := identity.AuthContext{
		TenantID: tenantID, PrincipalID: uuid.New(), PrincipalType: identity.PrincipalAgent,
		AllowedProjects: []uuid.UUID{projectID}, AllowedScopes: []identity.ScopeType{identity.ScopeTaskShared},
	}

	capPayload, _ := json.Marshal(map[string]any{
		"capture": capture.CaptureCommand{
			ProjectID: &projectID, TaskID: "t-1", Type: memory.TypeSemantic,
			ScopeType: identity.ScopeTaskShared, Summary: "sum", Content: "cont",
			EpistemicStatus: memory.EpistemicObserved, Importance: 0.7,
			SourceType: provenance.SourceAgentExecution, ExternalSourceID: "ext-1",
		},
	})
	id1, err := exec.ExecuteOpaqueOperationTx(context.Background(), reg, auth, requests.OpaqueOpCapture, nil, capPayload)
	if err != nil || id1 == nil {
		t.Fatalf("OpaqueOpCapture failed: %v", err)
	}

	// 1. OpaqueOpFeedback succeeds for write-only non-reader
	fbPayload, _ := json.Marshal(feedback.FeedbackCommand{
		MemoryID: *id1, TaskID: "t-1", Signal: memory.SignalHelpfulSuccess,
	})
	if _, err := exec.ExecuteOpaqueOperationTx(context.Background(), reg, auth, requests.OpaqueOpFeedback, id1, fbPayload); err != nil {
		t.Fatalf("OpaqueOpFeedback failed for write-only non-reader: %v", err)
	}

	// 2. OpaqueOpSupersede rejects cross-scope destination (org_shared)
	badSupPayload, _ := json.Marshal(capture.ExplicitSupersedeCommand{
		ChangeReason: "escalate",
		Capture: capture.CaptureCommand{
			ProjectID: &projectID, TaskID: "t-1", Type: memory.TypeSemantic,
			ScopeType: identity.ScopeOrgShared, Summary: "s2", Content: "c2",
			EpistemicStatus: memory.EpistemicObserved, Importance: 0.7,
			SourceType: provenance.SourceAgentExecution, ExternalSourceID: "ext-2",
		},
	})
	if _, err := exec.ExecuteOpaqueOperationTx(context.Background(), reg, auth, requests.OpaqueOpSupersede, id1, badSupPayload); !errors.Is(err, capture.ErrForbidden) {
		t.Fatalf("expected ErrForbidden for cross-scope opaque supersede, got %v", err)
	}

	// 3. Valid OpaqueOpSupersede succeeds
	goodSupPayload, _ := json.Marshal(capture.ExplicitSupersedeCommand{
		ChangeReason: "update",
		Capture: capture.CaptureCommand{
			ProjectID: &projectID, TaskID: "t-1", Type: memory.TypeSemantic,
			ScopeType: identity.ScopeTaskShared, Summary: "s2", Content: "c2",
			EpistemicStatus: memory.EpistemicObserved, Importance: 0.7,
			SourceType: provenance.SourceAgentExecution, ExternalSourceID: "ext-2",
		},
	})
	if _, err := exec.ExecuteOpaqueOperationTx(context.Background(), reg, auth, requests.OpaqueOpSupersede, id1, goodSupPayload); err != nil {
		t.Fatalf("valid OpaqueOpSupersede failed: %v", err)
	}

	// 4. Lease renewal returning false cancels execCtx without calling RecordFailure
	q := newFakeQueue()
	q.renewOK = false
	reqID := uuid.New()
	q.records[reqID] = requests.OpaqueRequestRecord{
		RequestID: reqID, TenantID: tenantID, ProjectID: projectID, TaskID: "t-1",
		PrincipalID: auth.PrincipalID, Operation: requests.OpaqueOpCapture, Payload: json.RawMessage(`{}`),
	}
	q.order = append(q.order, reqID)
	slowExec := &slowCancelExecutor{}
	w, _ := requests.NewWorker(q, nil, slowExec, requests.Config{RenewInterval: 5 * time.Millisecond})
	if _, err := w.ProcessOne(context.Background()); err != nil {
		t.Fatalf("ProcessOne failed on renewal loss: %v", err)
	}
	if !slowExec.canceled || len(q.failures) != 0 {
		t.Fatalf("expected execCtx canceled and 0 RecordFailure calls, got canceled=%v failures=%d", slowExec.canceled, len(q.failures))
	}
}

type slowCancelExecutor struct {
	canceled bool
}

func (s *slowCancelExecutor) ExecuteOpaqueOperationTx(ctx context.Context, _ memory.RepositoryRegistry, _ identity.AuthContext, _ requests.OpaqueOperation, _ *uuid.UUID, _ json.RawMessage) (*uuid.UUID, error) {
	<-ctx.Done()
	s.canceled = true
	return nil, ctx.Err()
}

func TestWorker_NewWorkerValidationAndErrorClassification(t *testing.T) {
	q := newFakeQueue()
	if _, err := requests.NewWorker(nil, nil, &fakeExecutor{}, requests.Config{}); err == nil {
		t.Fatal("expected error for nil queue")
	}
	if _, err := requests.NewWorker(q, nil, nil, requests.Config{}); err == nil {
		t.Fatal("expected error for nil executor")
	}

	tenantID := uuid.New()
	projectID := uuid.New()
	principalID := uuid.New()
	auth := identity.AuthContext{
		TenantID: tenantID, PrincipalID: principalID, PrincipalType: identity.PrincipalAgent,
		AllowedProjects: []uuid.UUID{projectID}, AllowedScopes: []identity.ScopeType{identity.ScopeTaskShared},
	}

	for _, tc := range []struct {
		name string
		err  error
		code string
	}{
		{"forbidden", capture.ErrForbidden, "forbidden"},
		{"not_found", capture.ErrNotFound, "not_found"},
		{"validation", capture.ErrValidation, "validation_error"},
		{"json_syntax", &json.SyntaxError{}, "invalid_payload"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fq := newFakeQueue()
			exec := &fakeExecutor{err: tc.err}
			w, _ := requests.NewWorker(fq, fakePrincipals{auth: auth}, exec, requests.Config{})
			id := uuid.New()
			fq.records[id] = requests.OpaqueRequestRecord{
				RequestID: id, TenantID: tenantID, ProjectID: projectID, TaskID: "t-1",
				PrincipalID: principalID, Operation: requests.OpaqueOpCapture, Payload: json.RawMessage(`{}`),
			}
			fq.order = append(fq.order, id)
			if _, err := w.ProcessOne(context.Background()); err != nil {
				t.Fatalf("ProcessOne failed: %v", err)
			}
			if len(fq.failures) != 1 || fq.failures[0].class != requests.FailurePermanent || fq.failures[0].code != tc.code {
				t.Fatalf("expected permanent %s, got %+v", tc.code, fq.failures)
			}
		})
	}
}

func TestServiceExecutor_OpaqueCaptureRelationsWriteOnlyActor(t *testing.T) {
	reg := &fullTestReg{
		fakeReg:  &fakeReg{q: newFakeQueue()},
		memories: make(map[uuid.UUID]memory.Memory),
		versions: make(map[uuid.UUID][]memory.MemoryVersion),
	}
	capSvc, _ := capture.NewService(reg, reg, nil, nil, nil)
	fbSvc, _ := feedback.NewService(reg, reg, nil, nil, capSvc, nil)
	exec := &requests.ServiceExecutor{Capture: capSvc, Feedback: fbSvc}

	tenantID := uuid.New()
	projectID := uuid.New()
	auth := identity.AuthContext{
		TenantID: tenantID, PrincipalID: uuid.New(), PrincipalType: identity.PrincipalAgent,
		AllowedProjects: []uuid.UUID{projectID}, AllowedScopes: []identity.ScopeType{identity.ScopeTaskShared},
	}

	targetSameTask := uuid.New()
	reg.memories[targetSameTask] = memory.Memory{
		ID: targetSameTask, TenantID: tenantID, ProjectID: &projectID, TaskID: "t-1",
		ScopeType: identity.ScopeTaskShared, Status: memory.StatusActive,
	}
	targetOtherTask := uuid.New()
	reg.memories[targetOtherTask] = memory.Memory{
		ID: targetOtherTask, TenantID: tenantID, ProjectID: &projectID, TaskID: "t-other",
		ScopeType: identity.ScopeTaskShared, Status: memory.StatusActive,
	}

	// Same-task relation succeeds for write-only opaque actor.
	okPayload, _ := json.Marshal(map[string]any{
		"capture": capture.CaptureCommand{
			ProjectID: &projectID, TaskID: "t-1", Type: memory.TypeSemantic,
			ScopeType: identity.ScopeTaskShared, Summary: "linked", Content: "body",
			EpistemicStatus: memory.EpistemicObserved, Importance: 0.7,
			SourceType: provenance.SourceAgentExecution, ExternalSourceID: "ext-rel-1",
			Relations: []memory.ExplicitRelationInput{{ToMemoryID: targetSameTask, RelationType: memory.RelationDependsOn}},
		},
	})
	if _, err := exec.ExecuteOpaqueOperationTx(context.Background(), reg, auth, requests.OpaqueOpCapture, nil, okPayload); err != nil {
		t.Fatalf("expected same-task opaque relation capture to succeed, got %v", err)
	}

	// Cross-task relation fails with opaque ErrNotFound.
	badPayload, _ := json.Marshal(map[string]any{
		"capture": capture.CaptureCommand{
			ProjectID: &projectID, TaskID: "t-1", Type: memory.TypeSemantic,
			ScopeType: identity.ScopeTaskShared, Summary: "cross", Content: "body",
			EpistemicStatus: memory.EpistemicObserved, Importance: 0.7,
			SourceType: provenance.SourceAgentExecution, ExternalSourceID: "ext-rel-2",
			Relations: []memory.ExplicitRelationInput{{ToMemoryID: targetOtherTask, RelationType: memory.RelationDependsOn}},
		},
	})
	if _, err := exec.ExecuteOpaqueOperationTx(context.Background(), reg, auth, requests.OpaqueOpCapture, nil, badPayload); !errors.Is(err, capture.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-task opaque relation, got %v", err)
	}

	// Unknown operation returns validation error.
	if _, err := exec.ExecuteOpaqueOperationTx(context.Background(), reg, auth, requests.OpaqueOperation("invalid_op"), nil, okPayload); err == nil {
		t.Fatal("expected error for unknown opaque operation")
	}

	// Nil Capture/Feedback dependencies fail cleanly.
	emptyExec := &requests.ServiceExecutor{}
	if _, err := emptyExec.ExecuteOpaqueOperationTx(context.Background(), reg, auth, requests.OpaqueOpCapture, nil, okPayload); err == nil {
		t.Fatal("expected error when Capture is nil")
	}
	if _, err := emptyExec.ExecuteOpaqueOperationTx(context.Background(), reg, auth, requests.OpaqueOpFeedback, nil, okPayload); err == nil {
		t.Fatal("expected error when Feedback is nil")
	}

	// Malformed JSON and forbidden epistemic/feedback signals in ExecuteOpaqueOperationTx.
	if _, err := exec.ExecuteOpaqueOperationTx(context.Background(), reg, auth, requests.OpaqueOpCapture, nil, []byte("{bad")); !errors.Is(err, capture.ErrValidation) {
		t.Fatalf("expected ErrValidation for malformed capture JSON, got %v", err)
	}
	if _, err := exec.ExecuteOpaqueOperationTx(context.Background(), reg, auth, requests.OpaqueOpFeedback, nil, []byte("{bad")); !errors.Is(err, feedback.ErrValidation) {
		t.Fatalf("expected ErrValidation for malformed feedback JSON, got %v", err)
	}
	badEpistemic, _ := json.Marshal(map[string]any{
		"capture": capture.CaptureCommand{
			ProjectID: &projectID, TaskID: "t-1", Type: memory.TypeSemantic,
			ScopeType: identity.ScopeTaskShared, Summary: "verified", Content: "body",
			EpistemicStatus: memory.EpistemicVerified, Importance: 0.7,
			SourceType: provenance.SourceAgentExecution, ExternalSourceID: "ext-rel-3",
		},
	})
	if _, err := exec.ExecuteOpaqueOperationTx(context.Background(), reg, auth, requests.OpaqueOpCapture, nil, badEpistemic); !errors.Is(err, capture.ErrForbidden) {
		t.Fatalf("expected ErrForbidden for agent assigning verified epistemic, got %v", err)
	}
}
