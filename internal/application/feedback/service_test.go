package feedback_test

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/romero-project/romero/internal/application/capture"
	"github.com/romero-project/romero/internal/application/feedback"
	"github.com/romero-project/romero/internal/domain/identity"
	"github.com/romero-project/romero/internal/domain/memory"
	"github.com/romero-project/romero/internal/domain/provenance"
)

type fakeFeedbackStore struct {
	memories  map[uuid.UUID]memory.Memory
	versions  map[uuid.UUID][]memory.MemoryVersion
	feedbacks []memory.MemoryFeedback
	allowed   []memory.AuditEvent
	denied    []memory.AuditEvent
	failAudit bool
}

func newFakeFeedbackStore() *fakeFeedbackStore {
	return &fakeFeedbackStore{
		memories: make(map[uuid.UUID]memory.Memory),
		versions: make(map[uuid.UUID][]memory.MemoryVersion),
	}
}

func (f *fakeFeedbackStore) ExecTx(_ context.Context, fn func(memory.RepositoryRegistry) error) error {
	return fn(&fakeReg{s: f})
}

func (f *fakeFeedbackStore) RecordAllowed(_ context.Context, ev memory.AuditEvent) error {
	if f.failAudit {
		return errors.New("audit down")
	}
	f.allowed = append(f.allowed, ev)
	return nil
}

func (f *fakeFeedbackStore) RecordDenied(_ context.Context, ev memory.AuditEvent) error {
	if f.failAudit {
		return errors.New("audit down")
	}
	f.denied = append(f.denied, ev)
	return nil
}

type fakeReg struct{ s *fakeFeedbackStore }

func (r *fakeReg) Sources() memory.SourceRepository    { return nil }
func (r *fakeReg) Memories() memory.MemoryRepository   { return r }
func (r *fakeReg) Feedback() memory.FeedbackRepository { return r }
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
func (r *fakeReg) InsertMemory(_ context.Context, _ memory.Memory) error { return nil }
func (r *fakeReg) UpdateMemoryState(_ context.Context, m memory.Memory) error {
	r.s.memories[m.ID] = m
	return nil
}
func (r *fakeReg) ListVersionsByMemoryID(_ context.Context, memoryID uuid.UUID) ([]memory.MemoryVersion, error) {
	return r.s.versions[memoryID], nil
}
func (r *fakeReg) InsertMemoryVersion(_ context.Context, v memory.MemoryVersion) error {
	r.s.versions[v.MemoryID] = append(r.s.versions[v.MemoryID], v)
	return nil
}
func (r *fakeReg) CloseMemoryVersionValidity(_ context.Context, memoryID uuid.UUID, version int, validUntil time.Time) error {
	vs := r.s.versions[memoryID]
	for i := range vs {
		if vs[i].Version == version {
			t := validUntil
			vs[i].ValidUntil = &t
		}
	}
	r.s.versions[memoryID] = vs
	return nil
}
func (r *fakeReg) GetMemoryVersionPointInTime(_ context.Context, _ uuid.UUID, _, _ time.Time) (*memory.MemoryVersion, error) {
	return nil, memory.ErrNotFound
}
func (r *fakeReg) ListActiveRelationsByMemoryID(_ context.Context, _, _ uuid.UUID) ([]memory.MemoryRelation, error) {
	return nil, nil
}
func (r *fakeReg) InsertMemoryRelation(_ context.Context, _ memory.MemoryRelation) error { return nil }
func (r *fakeReg) InsertMemoryFeedback(_ context.Context, fb memory.MemoryFeedback) error {
	r.s.feedbacks = append(r.s.feedbacks, fb)
	return nil
}
func (r *fakeReg) InsertSource(_ context.Context, _ provenance.MemorySource) error { return nil }

type fakeTaskAccess struct {
	snap feedback.TaskAccessSnapshot
	err  error
}

func (f fakeTaskAccess) EvaluateTaskAccess(_ context.Context, _ identity.TaskPermissionQuery) (feedback.TaskAccessSnapshot, error) {
	return f.snap, f.err
}

type fakeOpaqueFeedback struct {
	reqs []feedback.OpaqueFeedbackRequest
	err  error
}

func (f *fakeOpaqueFeedback) EnqueueFeedback(_ context.Context, req feedback.OpaqueFeedbackRequest) (feedback.OpaqueReceipt, error) {
	if f.err != nil {
		return feedback.OpaqueReceipt{}, f.err
	}
	if req.RequestID == uuid.Nil {
		req.RequestID = uuid.New()
	}
	f.reqs = append(f.reqs, req)
	return feedback.OpaqueReceipt{RequestID: req.RequestID}, nil
}

type fakeReplacer struct {
	called bool
	newID  uuid.UUID
	err    error
}

func (f *fakeReplacer) ExecuteSupersedeReplacementTx(_ context.Context, _ memory.RepositoryRegistry, _ identity.AuthContext, _ *memory.Memory, _ capture.CaptureCommand, _ string) (*capture.MemoryView, error) {
	f.called = true
	if f.err != nil {
		return nil, f.err
	}
	return &capture.MemoryView{ID: f.newID}, nil
}

func TestRecordFeedback_SevenSignalsAndQuarantine(t *testing.T) {
	tenantID := uuid.New()
	projectID := uuid.New()
	userAuth := identity.AuthContext{
		TenantID: tenantID, PrincipalID: uuid.New(), PrincipalType: identity.PrincipalUser,
		AllowedProjects: []uuid.UUID{projectID}, AllowedScopes: []identity.ScopeType{identity.ScopeProjectShared},
	}

	// Hypothesis (1.4, 2.6, 0.35) + misleading_failure -> Beta += 2.5 => 1.4 / 6.5 ~= 0.215 < 0.30 -> quarantined
	store := newFakeFeedbackStore()
	memID := uuid.New()
	srcID := uuid.New()
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	store.memories[memID] = memory.Memory{
		ID: memID, TenantID: tenantID, ProjectID: &projectID,
		Type: memory.TypeSemantic, ScopeType: identity.ScopeProjectShared, Status: memory.StatusActive,
		EpistemicStatus: memory.EpistemicHypothesis, AlphaScore: 1.4, BetaScore: 2.6, Confidence: 0.35,
		Version: 1, PrimarySourceID: srcID, Summary: "s", Content: "c",
		Clocks: memory.TriTemporalWindow{ObservedAt: now, CreatedAt: now, ValidFrom: now},
	}
	store.versions[memID] = []memory.MemoryVersion{{ID: uuid.New(), MemoryID: memID, Version: 1, ValidFrom: now}}

	svc, _ := feedback.NewService(store, store, fakeTaskAccess{}, &fakeOpaqueFeedback{}, &fakeReplacer{}, func() time.Time { return now })
	res, err := svc.RecordFeedback(context.Background(), userAuth, feedback.FeedbackCommand{
		MemoryID: memID, Signal: memory.SignalMisleadingFailure,
	})
	if err != nil {
		t.Fatalf("misleading_failure failed: %v", err)
	}
	if !res.Quarantined || res.NewStatus != memory.StatusConflicted {
		t.Fatalf("expected synchronous quarantine to conflicted, got %+v", res)
	}
	if math.Abs(float64(res.NewBeta)-5.1) > 0.001 || res.NewConfidence >= 0.30 {
		t.Fatalf("unexpected beta/confidence: %+v", res)
	}
	if len(store.versions[memID]) != 2 || store.versions[memID][0].ValidUntil == nil {
		t.Fatalf("expected v1 closed and v2 conflicted version inserted")
	}
}

func TestRecordFeedback_AuthorityOpaqueAndCorrectedReplacement(t *testing.T) {
	tenantID := uuid.New()
	projectID := uuid.New()
	agentAuth := identity.AuthContext{
		TenantID: tenantID, PrincipalID: uuid.New(), PrincipalType: identity.PrincipalAgent,
		AllowedProjects: []uuid.UUID{projectID}, AllowedScopes: []identity.ScopeType{identity.ScopeProjectShared, identity.ScopeTaskShared},
	}
	userAuth := agentAuth
	userAuth.PrincipalType = identity.PrincipalUser

	store := newFakeFeedbackStore()
	memID := uuid.New()
	srcID := uuid.New()
	now := time.Now().UTC()
	store.memories[memID] = memory.Memory{
		ID: memID, TenantID: tenantID, ProjectID: &projectID,
		Type: memory.TypeSemantic, ScopeType: identity.ScopeProjectShared, Status: memory.StatusActive,
		EpistemicStatus: memory.EpistemicObserved, AlphaScore: 8.5, BetaScore: 1.5, Confidence: 0.85,
		Version: 1, PrimarySourceID: srcID, Summary: "s", Content: "c",
		Clocks: memory.TriTemporalWindow{ObservedAt: now, CreatedAt: now, ValidFrom: now},
	}

	repl := &fakeReplacer{newID: uuid.New()}
	opaque := &fakeOpaqueFeedback{}
	svc, _ := feedback.NewService(store, store, fakeTaskAccess{snap: feedback.TaskAccessSnapshot{CanRead: false, CanWrite: true}}, opaque, repl, nil)

	// 1. Agent cannot send confirmed_verified -> ErrForbidden
	if _, err := svc.RecordFeedback(context.Background(), agentAuth, feedback.FeedbackCommand{
		MemoryID: memID, Signal: memory.SignalConfirmedVerified,
	}); !errors.Is(err, feedback.ErrForbidden) {
		t.Fatalf("expected ErrForbidden for agent confirmed_verified, got %v", err)
	}

	// 2. Write-only task_shared -> opaque 202
	opRes, err := svc.RecordFeedback(context.Background(), agentAuth, feedback.FeedbackCommand{
		MemoryID: memID, TaskID: "t-1", Signal: memory.SignalHelpfulSuccess,
	})
	if err != nil || !opRes.Accepted || opRes.OpaqueReceipt == nil {
		t.Fatalf("expected opaque 202 feedback, got %+v err=%v", opRes, err)
	}

	// 3. User corrected_by_user with ReplacementMemory -> calls ExecuteSupersedeReplacementTx
	repCmd := capture.CaptureCommand{
		ProjectID: &projectID, Type: memory.TypeSemantic, ScopeType: identity.ScopeProjectShared,
		Summary: "fixed", Content: "fixed content", EpistemicStatus: memory.EpistemicVerified,
		SourceType: provenance.SourceUserInput, ExternalSourceID: "u-1",
	}
	corrRes, err := svc.RecordFeedback(context.Background(), userAuth, feedback.FeedbackCommand{
		MemoryID: memID, Signal: memory.SignalCorrectedByUser, ReplacementMemory: &repCmd,
	})
	if err != nil || !repl.called || corrRes.ReplacementMemoryID == nil || *corrRes.ReplacementMemoryID != repl.newID {
		t.Fatalf("expected atomic replacement, got res=%+v err=%v called=%v", corrRes, err, repl.called)
	}
}

func TestFeedbackService_AllSignalsAndErrorEdges(t *testing.T) {
	store := newFakeFeedbackStore()
	if _, err := feedback.NewService(nil, store, nil, nil, nil, nil); err == nil {
		t.Fatal("expected error for nil uow")
	}
	if _, err := feedback.NewService(store, nil, nil, nil, nil, nil); err == nil {
		t.Fatal("expected error for nil audit")
	}

	tenantID := uuid.New()
	projectID := uuid.New()
	userAuth := identity.AuthContext{
		TenantID: tenantID, PrincipalID: uuid.New(), PrincipalType: identity.PrincipalUser,
		AllowedProjects: []uuid.UUID{projectID}, AllowedScopes: []identity.ScopeType{identity.ScopeProjectShared, identity.ScopeTaskShared},
	}

	svc, _ := feedback.NewService(store, store, fakeTaskAccess{snap: feedback.TaskAccessSnapshot{CanRead: true, CanWrite: true}}, &fakeOpaqueFeedback{}, &fakeReplacer{newID: uuid.New()}, nil)

	// Validation & Auth edges
	if _, err := svc.RecordFeedback(context.Background(), identity.AuthContext{}, feedback.FeedbackCommand{MemoryID: uuid.New(), Signal: memory.SignalRetrieved}); !errors.Is(err, feedback.ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized, got %v", err)
	}
	if _, err := svc.RecordFeedback(context.Background(), userAuth, feedback.FeedbackCommand{Signal: memory.SignalRetrieved}); !errors.Is(err, feedback.ErrValidation) {
		t.Fatalf("expected ErrValidation for nil MemoryID, got %v", err)
	}
	if _, err := svc.RecordFeedback(context.Background(), userAuth, feedback.FeedbackCommand{MemoryID: uuid.New(), Signal: "bad"}); !errors.Is(err, feedback.ErrValidation) {
		t.Fatalf("expected ErrValidation for invalid Signal, got %v", err)
	}
	if _, err := svc.RecordFeedback(context.Background(), userAuth, feedback.FeedbackCommand{MemoryID: uuid.New(), Signal: memory.SignalRetrieved}); !errors.Is(err, feedback.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for missing memory, got %v", err)
	}

	// Exercise remaining signals: retrieved, used_in_context, helpful_success, ignored, confirmed_verified
	memID := uuid.New()
	now := time.Now().UTC()
	store.memories[memID] = memory.Memory{
		ID: memID, TenantID: tenantID, ProjectID: &projectID, TaskID: "t-1",
		Type: memory.TypeSemantic, ScopeType: identity.ScopeTaskShared, Status: memory.StatusActive,
		EpistemicStatus: memory.EpistemicObserved, AlphaScore: 8.5, BetaScore: 1.5, Confidence: 0.85,
		Version: 1, PrimarySourceID: uuid.New(), Summary: "s", Content: "c",
		Clocks: memory.TriTemporalWindow{ObservedAt: now, CreatedAt: now, ValidFrom: now},
	}

	for _, sig := range []memory.FeedbackSignal{
		memory.SignalRetrieved,
		memory.SignalUsedInContext,
		memory.SignalHelpfulSuccess,
		memory.SignalIgnored,
		memory.SignalConfirmedVerified,
	} {
		res, err := svc.RecordFeedback(context.Background(), userAuth, feedback.FeedbackCommand{
			MemoryID: memID, TaskID: "t-1", Signal: sig,
		})
		if err != nil {
			t.Fatalf("signal %s failed: %v", sig, err)
		}
		if sig == memory.SignalConfirmedVerified && res.NewEpistemic != memory.EpistemicVerified {
			t.Fatalf("expected confirmed_verified to promote epistemic to verified")
		}
	}

	// Closed memory -> ErrConflict
	archived := store.memories[memID]
	archived.Status = memory.StatusArchived
	store.memories[memID] = archived
	if _, err := svc.RecordFeedback(context.Background(), userAuth, feedback.FeedbackCommand{
		MemoryID: memID, Signal: memory.SignalHelpfulSuccess,
	}); !errors.Is(err, feedback.ErrConflict) {
		t.Fatalf("expected ErrConflict on archived memory feedback, got %v", err)
	}
}

func TestFeedbackService_CoverageEdges(t *testing.T) {
	store := newFakeFeedbackStore()
	tenantID := uuid.New()
	projectID := uuid.New()
	userAuth := identity.AuthContext{
		TenantID: tenantID, PrincipalID: uuid.New(), PrincipalType: identity.PrincipalUser,
		AllowedProjects: []uuid.UUID{projectID}, AllowedScopes: []identity.ScopeType{identity.ScopeProjectShared, identity.ScopeTaskShared},
	}
	memID := uuid.New()
	now := time.Now().UTC()
	store.memories[memID] = memory.Memory{
		ID: memID, TenantID: tenantID, ProjectID: &projectID, TaskID: "t-1",
		Type: memory.TypeSemantic, ScopeType: identity.ScopeTaskShared, Status: memory.StatusActive,
		EpistemicStatus: memory.EpistemicObserved, AlphaScore: 8.5, BetaScore: 1.5, Confidence: 0.85,
		Version: 1, PrimarySourceID: uuid.New(), Summary: "s", Content: "c",
		Clocks: memory.TriTemporalWindow{ObservedAt: now, CreatedAt: now, ValidFrom: now},
	}

	// 1. Opaque path errors: nil opaque, idempotency conflict, db error, !CanWrite
	svcNilOpaque, _ := feedback.NewService(store, store, fakeTaskAccess{snap: feedback.TaskAccessSnapshot{CanRead: false, CanWrite: true}}, nil, nil, nil)
	if _, err := svcNilOpaque.RecordFeedback(context.Background(), userAuth, feedback.FeedbackCommand{
		MemoryID: memID, TaskID: "t-1", Signal: memory.SignalHelpfulSuccess,
	}); !errors.Is(err, feedback.ErrOperational) {
		t.Fatalf("expected ErrOperational when opaque is nil, got %v", err)
	}

	svcOpaqueConflict, _ := feedback.NewService(store, store, fakeTaskAccess{snap: feedback.TaskAccessSnapshot{CanRead: false, CanWrite: true}}, &fakeOpaqueFeedback{err: errors.New("idempotency conflict")}, nil, nil)
	if _, err := svcOpaqueConflict.RecordFeedback(context.Background(), userAuth, feedback.FeedbackCommand{
		MemoryID: memID, TaskID: "t-1", Signal: memory.SignalHelpfulSuccess,
	}); !errors.Is(err, feedback.ErrConflict) {
		t.Fatalf("expected ErrConflict on opaque conflict, got %v", err)
	}

	svcOpaqueErr, _ := feedback.NewService(store, store, fakeTaskAccess{snap: feedback.TaskAccessSnapshot{CanRead: false, CanWrite: true}}, &fakeOpaqueFeedback{err: errors.New("pg down")}, nil, nil)
	if _, err := svcOpaqueErr.RecordFeedback(context.Background(), userAuth, feedback.FeedbackCommand{
		MemoryID: memID, TaskID: "t-1", Signal: memory.SignalHelpfulSuccess,
	}); !errors.Is(err, feedback.ErrOperational) {
		t.Fatalf("expected ErrOperational on opaque error, got %v", err)
	}

	svcNoWrite, _ := feedback.NewService(store, store, fakeTaskAccess{snap: feedback.TaskAccessSnapshot{CanRead: false, CanWrite: false}}, &fakeOpaqueFeedback{}, nil, nil)
	if _, err := svcNoWrite.RecordFeedback(context.Background(), userAuth, feedback.FeedbackCommand{
		MemoryID: memID, TaskID: "t-1", Signal: memory.SignalHelpfulSuccess,
	}); !errors.Is(err, feedback.ErrForbidden) {
		t.Fatalf("expected ErrForbidden when !CanWrite, got %v", err)
	}

	// 2. Direct authorizeTarget errors on task_shared: !CanRead -> ErrNotFound, CanRead && !CanWrite -> ErrForbidden
	if _, err := svcNoWrite.RecordFeedback(context.Background(), userAuth, feedback.FeedbackCommand{
		MemoryID: memID, Signal: memory.SignalHelpfulSuccess,
	}); !errors.Is(err, feedback.ErrNotFound) {
		t.Fatalf("expected ErrNotFound when !CanRead, got %v", err)
	}
	svcReadOnly, _ := feedback.NewService(store, store, fakeTaskAccess{snap: feedback.TaskAccessSnapshot{CanRead: true, CanWrite: false}}, &fakeOpaqueFeedback{}, nil, nil)
	if _, err := svcReadOnly.RecordFeedback(context.Background(), userAuth, feedback.FeedbackCommand{
		MemoryID: memID, Signal: memory.SignalHelpfulSuccess,
	}); !errors.Is(err, feedback.ErrForbidden) {
		t.Fatalf("expected ErrForbidden when CanRead && !CanWrite, got %v", err)
	}

	// 3. corrected_by_user with nil replacer -> ErrOperational
	svcNilRepl, _ := feedback.NewService(store, store, fakeTaskAccess{snap: feedback.TaskAccessSnapshot{CanRead: true, CanWrite: true}}, &fakeOpaqueFeedback{}, nil, nil)
	repCmd := capture.CaptureCommand{
		ProjectID: &projectID, Type: memory.TypeSemantic, ScopeType: identity.ScopeProjectShared,
		Summary: "s", Content: "c", EpistemicStatus: memory.EpistemicObserved,
		SourceType: provenance.SourceUserInput, ExternalSourceID: "u-1",
	}
	if _, err := svcNilRepl.RecordFeedback(context.Background(), userAuth, feedback.FeedbackCommand{
		MemoryID: memID, Signal: memory.SignalCorrectedByUser, ReplacementMemory: &repCmd,
	}); !errors.Is(err, feedback.ErrOperational) {
		t.Fatalf("expected ErrOperational when replacer is nil, got %v", err)
	}
}

func TestFeedbackService_ConfirmedVerifiedCreatesVersionSnapshot(t *testing.T) {
	store := newFakeFeedbackStore()
	tenantID := uuid.New()
	projectID := uuid.New()
	userAuth := identity.AuthContext{
		TenantID: tenantID, PrincipalID: uuid.New(), PrincipalType: identity.PrincipalUser,
		AllowedProjects: []uuid.UUID{projectID}, AllowedScopes: []identity.ScopeType{identity.ScopeProjectShared},
	}
	memID := uuid.New()
	now := time.Now().UTC()
	store.memories[memID] = memory.Memory{
		ID: memID, TenantID: tenantID, ProjectID: &projectID,
		Type: memory.TypeSemantic, ScopeType: identity.ScopeProjectShared, Status: memory.StatusActive,
		EpistemicStatus: memory.EpistemicObserved, AlphaScore: 8.5, BetaScore: 1.5, Confidence: 0.85,
		Version: 1, PrimarySourceID: uuid.New(), Summary: "s", Content: "c",
		Clocks: memory.TriTemporalWindow{ObservedAt: now, CreatedAt: now, ValidFrom: now},
	}
	store.versions[memID] = []memory.MemoryVersion{{ID: uuid.New(), MemoryID: memID, Version: 1, ValidFrom: now}}

	svc, _ := feedback.NewService(store, store, fakeTaskAccess{}, &fakeOpaqueFeedback{}, nil, nil)
	res, err := svc.RecordFeedback(context.Background(), userAuth, feedback.FeedbackCommand{
		MemoryID: memID, Signal: memory.SignalConfirmedVerified,
	})
	if err != nil {
		t.Fatalf("confirmed_verified failed: %v", err)
	}
	if res.NewEpistemic != memory.EpistemicVerified || res.NewStatus != memory.StatusActive {
		t.Fatalf("unexpected result: %+v", res)
	}
	if len(store.versions[memID]) != 2 || store.versions[memID][0].ValidUntil == nil || store.versions[memID][1].ValidUntil != nil || store.versions[memID][1].EpistemicStatus != memory.EpistemicVerified {
		t.Fatalf("expected v1 closed and v2 verified with nil ValidUntil, got %+v", store.versions[memID])
	}
}
