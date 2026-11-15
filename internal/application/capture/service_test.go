package capture_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/romero-project/romero/internal/application/capture"
	"github.com/romero-project/romero/internal/domain/identity"
	"github.com/romero-project/romero/internal/domain/memory"
	"github.com/romero-project/romero/internal/domain/provenance"
)

type fakeMemoryStore struct {
	mu        sync.Mutex
	sources   map[uuid.UUID]provenance.MemorySource
	memories  map[uuid.UUID]memory.Memory
	versions  map[uuid.UUID][]memory.MemoryVersion
	relations []memory.MemoryRelation
	jobs      []memory.MemoryJob
	allowed   []memory.AuditEvent
	denied    []memory.AuditEvent
	failAudit bool
}

func newFakeMemoryStore() *fakeMemoryStore {
	return &fakeMemoryStore{
		sources:  make(map[uuid.UUID]provenance.MemorySource),
		memories: make(map[uuid.UUID]memory.Memory),
		versions: make(map[uuid.UUID][]memory.MemoryVersion),
	}
}

func (f *fakeMemoryStore) ExecTx(_ context.Context, fn func(memory.RepositoryRegistry) error) error {
	return fn(&fakeRegistry{s: f})
}

func (f *fakeMemoryStore) RecordAllowed(_ context.Context, ev memory.AuditEvent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failAudit {
		return errors.New("audit down")
	}
	f.allowed = append(f.allowed, ev)
	return nil
}

func (f *fakeMemoryStore) RecordDenied(_ context.Context, ev memory.AuditEvent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failAudit {
		return errors.New("audit down")
	}
	f.denied = append(f.denied, ev)
	return nil
}

type fakeRegistry struct{ s *fakeMemoryStore }

func (r *fakeRegistry) Sources() memory.SourceRepository    { return r }
func (r *fakeRegistry) Memories() memory.MemoryRepository   { return r }
func (r *fakeRegistry) Feedback() memory.FeedbackRepository { return r }
func (r *fakeRegistry) Jobs() memory.JobRepository          { return r }
func (r *fakeRegistry) Audit() memory.AuditLogger           { return r.s }

func (r *fakeRegistry) InsertSource(_ context.Context, src provenance.MemorySource) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	r.s.sources[src.ID] = src
	return nil
}

func (r *fakeRegistry) GetMemoryByID(_ context.Context, tenantID, memoryID uuid.UUID) (*memory.Memory, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	m, ok := r.s.memories[memoryID]
	if !ok || m.TenantID != tenantID {
		return nil, memory.ErrNotFound
	}
	cp := m
	return &cp, nil
}

func (r *fakeRegistry) GetMemoryByIdempotencyKey(_ context.Context, tenantID uuid.UUID, key string) (*memory.Memory, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	for _, m := range r.s.memories {
		if m.TenantID == tenantID && m.IdempotencyKey == key {
			cp := m
			return &cp, nil
		}
	}
	return nil, memory.ErrNotFound
}

func (r *fakeRegistry) LockActiveTopicMemory(_ context.Context, scope identity.MemoryScopeTarget, topicKey string) (*memory.Memory, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	for _, m := range r.s.memories {
		if m.Status != memory.StatusActive || m.TopicKey != topicKey {
			continue
		}
		target := identity.MemoryScopeTarget{
			TenantID:     m.TenantID,
			ProjectID:    m.ProjectID,
			ScopeType:    m.ScopeType,
			OwnerAgentID: m.OwnerAgentID,
			TaskID:       m.TaskID,
		}
		if scope.SameTopicPartition(target) {
			cp := m
			return &cp, nil
		}
	}
	return nil, memory.ErrNotFound
}

func (r *fakeRegistry) InsertMemory(_ context.Context, m memory.Memory) error {
	if err := m.Validate(); err != nil {
		return err
	}
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	if m.Status == memory.StatusActive && m.TopicKey != "" {
		newScope := identity.MemoryScopeTarget{
			TenantID: m.TenantID, ProjectID: m.ProjectID, ScopeType: m.ScopeType,
			OwnerAgentID: m.OwnerAgentID, TaskID: m.TaskID,
		}
		for _, existing := range r.s.memories {
			if existing.Status == memory.StatusActive && existing.TopicKey == m.TopicKey {
				oldScope := identity.MemoryScopeTarget{
					TenantID: existing.TenantID, ProjectID: existing.ProjectID, ScopeType: existing.ScopeType,
					OwnerAgentID: existing.OwnerAgentID, TaskID: existing.TaskID,
				}
				if newScope.SameTopicPartition(oldScope) {
					return errors.New("idx_memories_active_topic_key unique violation")
				}
			}
		}
	}
	r.s.memories[m.ID] = m
	return nil
}

func (r *fakeRegistry) UpdateMemoryState(_ context.Context, m memory.Memory) error {
	if err := m.Validate(); err != nil {
		return err
	}
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	r.s.memories[m.ID] = m
	return nil
}

func (r *fakeRegistry) ListVersionsByMemoryID(_ context.Context, memoryID uuid.UUID) ([]memory.MemoryVersion, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	return append([]memory.MemoryVersion(nil), r.s.versions[memoryID]...), nil
}

func (r *fakeRegistry) InsertMemoryVersion(_ context.Context, v memory.MemoryVersion) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	r.s.versions[v.MemoryID] = append(r.s.versions[v.MemoryID], v)
	return nil
}

func (r *fakeRegistry) CloseMemoryVersionValidity(_ context.Context, memoryID uuid.UUID, version int, validUntil time.Time) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
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

func (r *fakeRegistry) GetMemoryVersionPointInTime(_ context.Context, _ uuid.UUID, _, _ time.Time) (*memory.MemoryVersion, error) {
	return nil, memory.ErrNotFound
}

func (r *fakeRegistry) ListActiveRelationsByMemoryID(_ context.Context, _, _ uuid.UUID) ([]memory.MemoryRelation, error) {
	return nil, nil
}

func (r *fakeRegistry) InsertMemoryRelation(_ context.Context, rel memory.MemoryRelation) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	r.s.relations = append(r.s.relations, rel)
	return nil
}

func (r *fakeRegistry) InsertMemoryFeedback(_ context.Context, _ memory.MemoryFeedback) error {
	return nil
}

func (r *fakeRegistry) EnqueueMemoryJob(_ context.Context, job memory.MemoryJob) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	r.s.jobs = append(r.s.jobs, job)
	return nil
}

type fakeTaskAccess struct {
	snap capture.TaskAccessSnapshot
	err  error
}

func (f fakeTaskAccess) EvaluateTaskAccess(_ context.Context, _ identity.TaskPermissionQuery) (capture.TaskAccessSnapshot, error) {
	return f.snap, f.err
}

type fakeOpaqueQueue struct {
	mu   sync.Mutex
	reqs []capture.OpaqueRequest
	err  error
}

func (f *fakeOpaqueQueue) Enqueue(_ context.Context, req capture.OpaqueRequest) (capture.OpaqueReceipt, error) {
	if f.err != nil {
		return capture.OpaqueReceipt{}, f.err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if req.RequestID == uuid.Nil {
		req.RequestID = uuid.New()
	}
	f.reqs = append(f.reqs, req)
	return capture.OpaqueReceipt{RequestID: req.RequestID}, nil
}

func testAuth(pType identity.PrincipalType, projectID, agentID uuid.UUID, scopes ...identity.ScopeType) identity.AuthContext {
	return identity.AuthContext{
		TenantID:        uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		PrincipalID:     uuid.MustParse("22222222-2222-2222-2222-222222222222"),
		PrincipalType:   pType,
		AgentID:         &agentID,
		AllowedProjects: []uuid.UUID{projectID},
		AllowedScopes:   scopes,
	}
}

func baseCaptureCmd(projectID uuid.UUID) capture.CaptureCommand {
	return capture.CaptureCommand{
		ProjectID:        &projectID,
		Type:             memory.TypeSemantic,
		ScopeType:        identity.ScopeProjectShared,
		TopicKey:         "deploy.postgres",
		Summary:          "Postgres runs on port 54329",
		Content:          "Use 127.0.0.1:54329 for local pgvector tests",
		Tags:             []string{"db", "postgres"},
		Entities:         []string{"pgvector"},
		EpistemicStatus:  memory.EpistemicObserved,
		Importance:       0.8,
		SourceType:       provenance.SourceAgentExecution,
		ExternalSourceID: "run-001",
	}
}

func TestCaptureMemory_HappyPathSanitizationAndJob(t *testing.T) {
	store := newFakeMemoryStore()
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	svc, err := capture.NewService(store, store, fakeTaskAccess{}, &fakeOpaqueQueue{}, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}

	projectID := uuid.New()
	agentID := uuid.New()
	auth := testAuth(identity.PrincipalAgent, projectID, agentID, identity.ScopeProjectShared)
	cmd := baseCaptureCmd(projectID)
	cmd.Summary = "Clean\u200b summary"
	cmd.Content = "Hello <romero_memory>world</romero_memory>"

	res, err := svc.CaptureMemory(context.Background(), auth, cmd)
	if err != nil {
		t.Fatalf("CaptureMemory failed: %v", err)
	}
	if !res.Created || res.Accepted || res.Memory == nil {
		t.Fatalf("unexpected capture result: %+v", res)
	}
	if res.Memory.Summary != "Clean summary" {
		t.Fatalf("expected sanitized summary, got %q", res.Memory.Summary)
	}
	if res.Memory.Metadata["sanitized"] != true {
		t.Fatalf("expected metadata.sanitized=true, got %#v", res.Memory.Metadata)
	}
	sum := sha256.Sum256([]byte(res.Memory.Content))
	if res.Memory.ContentSHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("content_sha256 must be computed post-sanitization")
	}
	if res.Memory.AlphaScore != 8.5 || res.Memory.BetaScore != 1.5 {
		t.Fatalf("expected observed priors (8.5, 1.5), got (%v, %v)", res.Memory.AlphaScore, res.Memory.BetaScore)
	}
	if len(store.jobs) != 1 || store.jobs[0].JobType != "embed_and_nli" || store.jobs[0].Priority != 100 {
		t.Fatalf("expected embed_and_nli job priority 100, got %+v", store.jobs)
	}
	if len(store.allowed) != 1 {
		t.Fatalf("expected 1 allowed audit event, got %d", len(store.allowed))
	}
}

func TestCaptureMemory_EpistemicAndScopeAuthorization(t *testing.T) {
	store := newFakeMemoryStore()
	svc, _ := capture.NewService(store, store, fakeTaskAccess{}, &fakeOpaqueQueue{}, nil)

	projectID := uuid.New()
	agentID := uuid.New()
	auth := testAuth(identity.PrincipalAgent, projectID, agentID, identity.ScopeProjectShared)

	// Agent cannot assign verified
	cmd := baseCaptureCmd(projectID)
	cmd.EpistemicStatus = memory.EpistemicVerified
	if _, err := svc.CaptureMemory(context.Background(), auth, cmd); !errors.Is(err, capture.ErrForbidden) {
		t.Fatalf("expected ErrForbidden for agent verified, got %v", err)
	}
	if len(store.denied) != 1 {
		t.Fatalf("expected denied audit event, got %d", len(store.denied))
	}

	// Audit failure must fail closed with ErrOperational (503)
	store.failAudit = true
	if _, err := svc.CaptureMemory(context.Background(), auth, cmd); !errors.Is(err, capture.ErrOperational) {
		t.Fatalf("expected ErrOperational when audit fails, got %v", err)
	}
}

func TestCaptureMemory_IdempotencyAndConflict(t *testing.T) {
	store := newFakeMemoryStore()
	svc, _ := capture.NewService(store, store, fakeTaskAccess{}, &fakeOpaqueQueue{}, nil)

	projectID := uuid.New()
	agentID := uuid.New()
	auth := testAuth(identity.PrincipalUser, projectID, agentID, identity.ScopeProjectShared, identity.ScopeOrgShared)

	cmd := baseCaptureCmd(projectID)
	cmd.IdempotencyKey = "idem-123"
	first, err := svc.CaptureMemory(context.Background(), auth, cmd)
	if err != nil || !first.Created {
		t.Fatalf("first capture failed: res=%+v err=%v", first, err)
	}

	// Same key, same content & scope -> 200 without duplicating
	second, err := svc.CaptureMemory(context.Background(), auth, cmd)
	if err != nil {
		t.Fatalf("idempotent replay failed: %v", err)
	}
	if second.Created || second.Memory.ID != first.Memory.ID {
		t.Fatalf("expected idempotent replay of %s, got Created=%v ID=%s", first.Memory.ID, second.Created, second.Memory.ID)
	}

	// Same key, different content -> 409 Conflict
	cmdDiff := cmd
	cmdDiff.Content = "Different content"
	if _, err := svc.CaptureMemory(context.Background(), auth, cmdDiff); !errors.Is(err, capture.ErrConflict) {
		t.Fatalf("expected ErrConflict on idempotency content mismatch, got %v", err)
	}
}

func TestCaptureMemory_TopicSupersessionAndConflict(t *testing.T) {
	store := newFakeMemoryStore()
	svc, _ := capture.NewService(store, store, fakeTaskAccess{}, &fakeOpaqueQueue{}, nil)

	projectID := uuid.New()
	agentID := uuid.New()
	auth := testAuth(identity.PrincipalUser, projectID, agentID, identity.ScopeProjectShared)

	// 1. Observed memory on topic_key
	c1 := baseCaptureCmd(projectID)
	c1.EpistemicStatus = memory.EpistemicObserved
	r1, err := svc.CaptureMemory(context.Background(), auth, c1)
	if err != nil {
		t.Fatalf("c1 failed: %v", err)
	}

	// 2. Lower rank (inferred) on same topic_key -> conflicted + contradicts
	c2 := baseCaptureCmd(projectID)
	c2.Content = "Maybe port 5432"
	c2.EpistemicStatus = memory.EpistemicInferred
	r2, err := svc.CaptureMemory(context.Background(), auth, c2)
	if err != nil {
		t.Fatalf("c2 failed: %v", err)
	}
	if r2.Memory.Status != memory.StatusConflicted {
		t.Fatalf("expected conflicted status for lower rank, got %s", r2.Memory.Status)
	}
	if store.memories[r1.Memory.ID].Status != memory.StatusActive {
		t.Fatalf("expected original active memory to remain active")
	}

	// 3. Equal/higher rank (verified) -> supersedes r1
	c3 := baseCaptureCmd(projectID)
	c3.Content = "Verified port 54329"
	c3.EpistemicStatus = memory.EpistemicVerified
	r3, err := svc.CaptureMemory(context.Background(), auth, c3)
	if err != nil {
		t.Fatalf("c3 failed: %v", err)
	}
	if r3.Memory.Status != memory.StatusActive {
		t.Fatalf("expected new memory active, got %s", r3.Memory.Status)
	}
	if store.memories[r1.Memory.ID].Status != memory.StatusSuperseded {
		t.Fatalf("expected r1 superseded, got %s", store.memories[r1.Memory.ID].Status)
	}
}

func TestCaptureMemory_TaskSharedOpaqueQueue(t *testing.T) {
	store := newFakeMemoryStore()
	opaque := &fakeOpaqueQueue{}
	access := fakeTaskAccess{snap: capture.TaskAccessSnapshot{CanRead: false, CanWrite: true}}
	svc, _ := capture.NewService(store, store, access, opaque, nil)

	projectID := uuid.New()
	agentID := uuid.New()
	auth := testAuth(identity.PrincipalAgent, projectID, agentID, identity.ScopeTaskShared)

	cmd := baseCaptureCmd(projectID)
	cmd.ScopeType = identity.ScopeTaskShared
	cmd.TaskID = "task-42"

	res, err := svc.CaptureMemory(context.Background(), auth, cmd)
	if err != nil {
		t.Fatalf("opaque capture failed: %v", err)
	}
	if !res.Accepted || res.OpaqueReceipt == nil || res.Memory != nil {
		t.Fatalf("expected 202 opaque receipt without Memory, got %+v", res)
	}
	if len(opaque.reqs) != 1 || opaque.reqs[0].Operation != capture.OpaqueOpCapture {
		t.Fatalf("expected 1 queued capture opaque request, got %+v", opaque.reqs)
	}
	if len(store.memories) != 0 {
		t.Fatalf("expected no synchronous memory write on opaque path")
	}
}

func TestUpdateSupersedeArchive_LifecycleAndClosedConflict(t *testing.T) {
	store := newFakeMemoryStore()
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	svc, _ := capture.NewService(store, store, fakeTaskAccess{}, &fakeOpaqueQueue{}, func() time.Time {
		now = now.Add(time.Minute)
		return now
	})

	projectID := uuid.New()
	agentID := uuid.New()
	auth := testAuth(identity.PrincipalUser, projectID, agentID, identity.ScopeProjectShared)

	c1 := baseCaptureCmd(projectID)
	c1.EpistemicStatus = memory.EpistemicVerified
	r1, err := svc.CaptureMemory(context.Background(), auth, c1)
	if err != nil {
		t.Fatalf("capture failed: %v", err)
	}

	// Update creates v2
	upRes, err := svc.UpdateMemory(context.Background(), auth, r1.Memory.ID, capture.UpdateCommand{
		Summary:      "Updated summary",
		Content:      "Updated content",
		ChangeReason: "clarification",
	})
	if err != nil {
		t.Fatalf("UpdateMemory failed: %v", err)
	}
	if upRes.Memory.Version != 2 {
		t.Fatalf("expected version 2, got %d", upRes.Memory.Version)
	}
	if len(store.versions[r1.Memory.ID]) != 2 || store.versions[r1.Memory.ID][0].ValidUntil == nil {
		t.Fatalf("expected v1 closed and v2 created, got %+v", store.versions[r1.Memory.ID])
	}

	// Explicit supersede verified -> observed by user with reason
	c2 := baseCaptureCmd(projectID)
	c2.Content = "Replaced with observed"
	c2.EpistemicStatus = memory.EpistemicObserved
	supRes, err := svc.SupersedeMemory(context.Background(), auth, r1.Memory.ID, capture.ExplicitSupersedeCommand{
		Capture:      c2,
		ChangeReason: "manual override",
	})
	if err != nil {
		t.Fatalf("SupersedeMemory failed: %v", err)
	}
	if supRes.OldMemory.Status != memory.StatusSuperseded || supRes.NewMemory.Status != memory.StatusActive {
		t.Fatalf("unexpected supersede states: old=%s new=%s", supRes.OldMemory.Status, supRes.NewMemory.Status)
	}

	// Mutating closed (superseded) memory -> ErrConflict
	if _, err := svc.UpdateMemory(context.Background(), auth, r1.Memory.ID, capture.UpdateCommand{
		Summary: "Should fail",
		Content: "Should fail",
	}); !errors.Is(err, capture.ErrConflict) {
		t.Fatalf("expected ErrConflict updating superseded memory, got %v", err)
	}

	// Archive the new active memory
	archRes, err := svc.ArchiveMemory(context.Background(), auth, supRes.NewMemory.ID, capture.ArchiveCommand{
		Reason: "no longer needed",
	})
	if err != nil {
		t.Fatalf("ArchiveMemory failed: %v", err)
	}
	if archRes.Memory.Status != memory.StatusArchived {
		t.Fatalf("expected archived status, got %s", archRes.Memory.Status)
	}

	// Cross-tenant access -> ErrNotFound + audit denied
	otherAuth := auth
	otherAuth.TenantID = uuid.New()
	if _, err := svc.ArchiveMemory(context.Background(), otherAuth, supRes.NewMemory.ID, capture.ArchiveCommand{Reason: "x"}); !errors.Is(err, capture.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-tenant archive, got %v", err)
	}
}

func TestCaptureService_ValidationAndAuthEdges(t *testing.T) {
	store := newFakeMemoryStore()
	if _, err := capture.NewService(nil, store, nil, nil, nil); err == nil {
		t.Fatal("expected error for nil uow")
	}
	if _, err := capture.NewService(store, nil, nil, nil, nil); err == nil {
		t.Fatal("expected error for nil audit")
	}
	svc, err := capture.NewService(store, store, fakeTaskAccess{}, &fakeOpaqueQueue{}, nil)
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}

	projectID := uuid.New()
	agentID := uuid.New()
	auth := testAuth(identity.PrincipalUser, projectID, agentID, identity.ScopeProjectShared, identity.ScopeTaskShared)

	if _, err := svc.CaptureMemory(context.Background(), identity.AuthContext{}, baseCaptureCmd(projectID)); !errors.Is(err, capture.ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized, got %v", err)
	}

	badCases := []struct {
		name string
		mod  func(*capture.CaptureCommand)
	}{
		{"invalid_type", func(c *capture.CaptureCommand) { c.Type = "bad" }},
		{"invalid_epistemic", func(c *capture.CaptureCommand) { c.EpistemicStatus = "bad" }},
		{"invalid_scope", func(c *capture.CaptureCommand) { c.ScopeType = "bad" }},
		{"missing_task_id", func(c *capture.CaptureCommand) { c.ScopeType = identity.ScopeTaskShared; c.TaskID = "" }},
		{"invalid_source_type", func(c *capture.CaptureCommand) { c.SourceType = "bad" }},
		{"missing_external_source", func(c *capture.CaptureCommand) { c.ExternalSourceID = " " }},
		{"invalid_importance", func(c *capture.CaptureCommand) { c.Importance = 1.5 }},
		{"empty_summary", func(c *capture.CaptureCommand) { c.Summary = " " }},
	}
	for _, tc := range badCases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := baseCaptureCmd(projectID)
			tc.mod(&cmd)
			if _, err := svc.CaptureMemory(context.Background(), auth, cmd); !errors.Is(err, capture.ErrValidation) {
				t.Fatalf("%s: expected ErrValidation, got %v", tc.name, err)
			}
		})
	}

	// Scope forbidden (agent_private without allowed scope)
	cmdForbidden := baseCaptureCmd(projectID)
	cmdForbidden.ScopeType = identity.ScopeAgentPrivate
	if _, err := svc.CaptureMemory(context.Background(), auth, cmdForbidden); !errors.Is(err, capture.ErrForbidden) {
		t.Fatalf("expected ErrForbidden for disallowed scope, got %v", err)
	}
}

func TestCaptureService_TaskSharedSyncAndErrors(t *testing.T) {
	projectID := uuid.New()
	agentID := uuid.New()
	auth := testAuth(identity.PrincipalUser, projectID, agentID, identity.ScopeTaskShared)

	// 1. TaskAccessChecker failure -> ErrOperational
	store := newFakeMemoryStore()
	svcErr, _ := capture.NewService(store, store, fakeTaskAccess{err: errors.New("db down")}, &fakeOpaqueQueue{}, nil)
	cmd := baseCaptureCmd(projectID)
	cmd.ScopeType = identity.ScopeTaskShared
	cmd.TaskID = "t-1"
	if _, err := svcErr.CaptureMemory(context.Background(), auth, cmd); !errors.Is(err, capture.ErrOperational) {
		t.Fatalf("expected ErrOperational on task access error, got %v", err)
	}

	// 2. CanWrite=false -> ErrForbidden
	svcNoWrite, _ := capture.NewService(store, store, fakeTaskAccess{snap: capture.TaskAccessSnapshot{CanRead: true, CanWrite: false}}, &fakeOpaqueQueue{}, nil)
	if _, err := svcNoWrite.CaptureMemory(context.Background(), auth, cmd); !errors.Is(err, capture.ErrForbidden) {
		t.Fatalf("expected ErrForbidden when !CanWrite, got %v", err)
	}

	// 3. Opaque queue errors (conflict & operational)
	svcOpaqueConflict, _ := capture.NewService(store, store, fakeTaskAccess{snap: capture.TaskAccessSnapshot{CanRead: false, CanWrite: true}}, &fakeOpaqueQueue{err: errors.New("idempotency conflict")}, nil)
	if _, err := svcOpaqueConflict.CaptureMemory(context.Background(), auth, cmd); !errors.Is(err, capture.ErrConflict) {
		t.Fatalf("expected ErrConflict on opaque idempotency conflict, got %v", err)
	}
	svcOpaqueDown, _ := capture.NewService(store, store, fakeTaskAccess{snap: capture.TaskAccessSnapshot{CanRead: false, CanWrite: true}}, &fakeOpaqueQueue{err: errors.New("pg down")}, nil)
	if _, err := svcOpaqueDown.CaptureMemory(context.Background(), auth, cmd); !errors.Is(err, capture.ErrOperational) {
		t.Fatalf("expected ErrOperational on opaque queue error, got %v", err)
	}

	// 4. CanRead=true && CanWrite=true -> synchronous task_shared capture, update, supersede, archive
	svcSync, _ := capture.NewService(store, store, fakeTaskAccess{snap: capture.TaskAccessSnapshot{CanRead: true, CanWrite: true}}, &fakeOpaqueQueue{}, nil)
	res, err := svcSync.CaptureMemory(context.Background(), auth, cmd)
	if err != nil || !res.Created || res.Accepted {
		t.Fatalf("expected sync task_shared capture, got res=%+v err=%v", res, err)
	}

	// Non-reader attempting direct UpdateMemory without task_id selector -> ErrNotFound
	svcNoRead, _ := capture.NewService(store, store, fakeTaskAccess{snap: capture.TaskAccessSnapshot{CanRead: false, CanWrite: true}}, &fakeOpaqueQueue{}, nil)
	if _, err := svcNoRead.UpdateMemory(context.Background(), auth, res.Memory.ID, capture.UpdateCommand{
		Summary: "x", Content: "y",
	}); !errors.Is(err, capture.ErrNotFound) {
		t.Fatalf("expected ErrNotFound when !CanRead without task selector, got %v", err)
	}

	// Non-reader WITH task_id selector -> opaque 202 for Update, Supersede, Archive
	opaqueQ := &fakeOpaqueQueue{}
	svcOpaqueRW, _ := capture.NewService(store, store, fakeTaskAccess{snap: capture.TaskAccessSnapshot{CanRead: false, CanWrite: true}}, opaqueQ, nil)
	upRes, err := svcOpaqueRW.UpdateMemory(context.Background(), auth, res.Memory.ID, capture.UpdateCommand{
		TaskID: "t-1", Summary: "s", Content: "c",
	})
	if err != nil || !upRes.Accepted || upRes.OpaqueReceipt == nil {
		t.Fatalf("expected opaque 202 update, got res=%+v err=%v", upRes, err)
	}
	supRes, err := svcOpaqueRW.SupersedeMemory(context.Background(), auth, res.Memory.ID, capture.ExplicitSupersedeCommand{
		TaskID: "t-1", Capture: cmd, ChangeReason: "reason",
	})
	if err != nil || !supRes.Accepted || supRes.OpaqueReceipt == nil {
		t.Fatalf("expected opaque 202 supersede, got res=%+v err=%v", supRes, err)
	}
	archRes, err := svcOpaqueRW.ArchiveMemory(context.Background(), auth, res.Memory.ID, capture.ArchiveCommand{
		TaskID: "t-1", Reason: "done",
	})
	if err != nil || !archRes.Accepted || archRes.OpaqueReceipt == nil {
		t.Fatalf("expected opaque 202 archive, got res=%+v err=%v", archRes, err)
	}
	if len(opaqueQ.reqs) != 3 {
		t.Fatalf("expected 3 opaque requests, got %d", len(opaqueQ.reqs))
	}
}

func TestSupersedeAndArchive_ErrorEdges(t *testing.T) {
	store := newFakeMemoryStore()
	svc, _ := capture.NewService(store, store, fakeTaskAccess{}, &fakeOpaqueQueue{}, nil)

	projectID := uuid.New()
	agentID := uuid.New()
	userAuth := testAuth(identity.PrincipalUser, projectID, agentID, identity.ScopeProjectShared)
	agentAuth := testAuth(identity.PrincipalAgent, projectID, agentID, identity.ScopeProjectShared)

	cVerified := baseCaptureCmd(projectID)
	cVerified.EpistemicStatus = memory.EpistemicVerified
	r1, err := svc.CaptureMemory(context.Background(), userAuth, cVerified)
	if err != nil {
		t.Fatalf("capture failed: %v", err)
	}

	// Agent cannot explicitly downgrade verified -> observed (ErrForbidden)
	cObs := baseCaptureCmd(projectID)
	cObs.EpistemicStatus = memory.EpistemicObserved
	if _, err := svc.SupersedeMemory(context.Background(), agentAuth, r1.Memory.ID, capture.ExplicitSupersedeCommand{
		Capture:      cObs,
		ChangeReason: "agent trying downgrade",
	}); !errors.Is(err, capture.ErrForbidden) {
		t.Fatalf("expected ErrForbidden for agent verified->observed, got %v", err)
	}

	// User missing ChangeReason on verified -> observed -> ErrValidation
	if _, err := svc.SupersedeMemory(context.Background(), userAuth, r1.Memory.ID, capture.ExplicitSupersedeCommand{
		Capture: cObs,
	}); !errors.Is(err, capture.ErrValidation) {
		t.Fatalf("expected ErrValidation for missing ChangeReason, got %v", err)
	}

	// Verified -> Hypothesis -> ErrConflict
	cHyp := baseCaptureCmd(projectID)
	cHyp.EpistemicStatus = memory.EpistemicHypothesis
	if _, err := svc.SupersedeMemory(context.Background(), userAuth, r1.Memory.ID, capture.ExplicitSupersedeCommand{
		Capture:      cHyp,
		ChangeReason: "cannot downgrade to hypothesis",
	}); !errors.Is(err, capture.ErrConflict) {
		t.Fatalf("expected ErrConflict for verified->hypothesis, got %v", err)
	}

	// Archive then try to archive again -> ErrConflict
	if _, err := svc.ArchiveMemory(context.Background(), userAuth, r1.Memory.ID, capture.ArchiveCommand{Reason: "done"}); err != nil {
		t.Fatalf("archive failed: %v", err)
	}
	if _, err := svc.ArchiveMemory(context.Background(), userAuth, r1.Memory.ID, capture.ArchiveCommand{Reason: "again"}); !errors.Is(err, capture.ErrConflict) {
		t.Fatalf("expected ErrConflict on second archive, got %v", err)
	}
	if _, err := svc.SupersedeMemory(context.Background(), userAuth, r1.Memory.ID, capture.ExplicitSupersedeCommand{
		Capture: cObs, ChangeReason: "closed",
	}); !errors.Is(err, capture.ErrConflict) {
		t.Fatalf("expected ErrConflict superseding archived memory, got %v", err)
	}
}

func TestCaptureService_AdditionalCoverageEdges(t *testing.T) {
	store := newFakeMemoryStore()
	svc, _ := capture.NewService(store, store, fakeTaskAccess{snap: capture.TaskAccessSnapshot{CanRead: false, CanWrite: false}}, &fakeOpaqueQueue{}, nil)

	projectID := uuid.New()
	agentID := uuid.New()
	auth := testAuth(identity.PrincipalUser, projectID, agentID, identity.ScopeProjectShared, identity.ScopeTaskShared)

	// Nil IDs -> ErrValidation
	if _, err := svc.UpdateMemory(context.Background(), auth, uuid.Nil, capture.UpdateCommand{Summary: "s", Content: "c"}); !errors.Is(err, capture.ErrValidation) {
		t.Fatalf("expected ErrValidation for nil UpdateMemory ID, got %v", err)
	}
	if _, err := svc.SupersedeMemory(context.Background(), auth, uuid.Nil, capture.ExplicitSupersedeCommand{Capture: baseCaptureCmd(projectID)}); !errors.Is(err, capture.ErrValidation) {
		t.Fatalf("expected ErrValidation for nil SupersedeMemory ID, got %v", err)
	}
	if _, err := svc.ArchiveMemory(context.Background(), auth, uuid.Nil, capture.ArchiveCommand{Reason: "r"}); !errors.Is(err, capture.ErrValidation) {
		t.Fatalf("expected ErrValidation for nil ArchiveMemory ID, got %v", err)
	}

	// Update validation errors
	if _, err := svc.UpdateMemory(context.Background(), auth, uuid.New(), capture.UpdateCommand{Summary: "", Content: "c"}); !errors.Is(err, capture.ErrValidation) {
		t.Fatalf("expected ErrValidation for empty summary, got %v", err)
	}
	badImp := float32(-0.1)
	if _, err := svc.UpdateMemory(context.Background(), auth, uuid.New(), capture.UpdateCommand{Summary: "s", Content: "c", Importance: &badImp}); !errors.Is(err, capture.ErrValidation) {
		t.Fatalf("expected ErrValidation for negative importance, got %v", err)
	}

	// Opaque selector with !CanWrite -> ErrForbidden
	if _, err := svc.UpdateMemory(context.Background(), auth, uuid.New(), capture.UpdateCommand{TaskID: "t-1", Summary: "s", Content: "c"}); !errors.Is(err, capture.ErrForbidden) {
		t.Fatalf("expected ErrForbidden when !CanWrite on task selector, got %v", err)
	}

	// Custom ObservedAt / ValidFrom and UpdateMemory with tags/entities/importance/metadata
	obs := time.Date(2026, 11, 10, 10, 0, 0, 0, time.UTC)
	vf := time.Date(2026, 11, 11, 10, 0, 0, 0, time.UTC)
	cmd := baseCaptureCmd(projectID)
	cmd.ObservedAt = &obs
	cmd.ValidFrom = &vf
	res, err := svc.CaptureMemory(context.Background(), auth, cmd)
	if err != nil {
		t.Fatalf("capture failed: %v", err)
	}
	if !res.Memory.ObservedAt.Equal(obs) || !res.Memory.ValidFrom.Equal(vf) {
		t.Fatalf("expected custom ObservedAt/ValidFrom")
	}

	imp := float32(0.95)
	upRes, err := svc.UpdateMemory(context.Background(), auth, res.Memory.ID, capture.UpdateCommand{
		Summary:    "New\u200b summary",
		Content:    "New content",
		Tags:       []string{"v2"},
		Entities:   []string{"ent2"},
		Importance: &imp,
		Metadata:   map[string]any{"k": "v"},
	})
	if err != nil {
		t.Fatalf("update failed: %v", err)
	}
	if upRes.Memory.Importance != 0.95 || upRes.Memory.Metadata["sanitized"] != true || upRes.Memory.Metadata["k"] != "v" {
		t.Fatalf("unexpected update projection: %+v", upRes.Memory)
	}

	// NotFound on missing memory ID
	if _, err := svc.UpdateMemory(context.Background(), auth, uuid.New(), capture.UpdateCommand{Summary: "s", Content: "c"}); !errors.Is(err, capture.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if _, err := svc.SupersedeMemory(context.Background(), auth, uuid.New(), capture.ExplicitSupersedeCommand{Capture: baseCaptureCmd(projectID)}); !errors.Is(err, capture.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestCaptureService_ScopeAndProjectionEdges(t *testing.T) {
	if capture.MemoryViewFromDomain(nil) != nil {
		t.Fatal("expected nil MemoryView for nil domain Memory")
	}
	store := newFakeMemoryStore()
	svc, _ := capture.NewService(store, store, fakeTaskAccess{snap: capture.TaskAccessSnapshot{CanRead: true, CanWrite: false}}, &fakeOpaqueQueue{}, nil)

	projectID := uuid.New()
	agentA := uuid.New()
	agentB := uuid.New()
	authA := testAuth(identity.PrincipalAgent, projectID, agentA, identity.ScopeAgentPrivate, identity.ScopeTaskShared)
	authB := testAuth(identity.PrincipalAgent, projectID, agentB, identity.ScopeAgentPrivate, identity.ScopeTaskShared)

	// Agent A captures agent_private memory
	cmd := baseCaptureCmd(projectID)
	cmd.ScopeType = identity.ScopeAgentPrivate
	rA, err := svc.CaptureMemory(context.Background(), authA, cmd)
	if err != nil {
		t.Fatalf("agentA capture failed: %v", err)
	}

	// Agent B cannot read/update Agent A's agent_private memory -> ErrNotFound
	if _, err := svc.UpdateMemory(context.Background(), authB, rA.Memory.ID, capture.UpdateCommand{Summary: "x", Content: "y"}); !errors.Is(err, capture.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-agent private update, got %v", err)
	}

	// SupersedeMemory inheriting oldMem's ScopeType/ProjectID/TopicKey when omitted
	userAuth := testAuth(identity.PrincipalUser, projectID, agentA, identity.ScopeProjectShared)
	cmdProj := baseCaptureCmd(projectID)
	rProj, err := svc.CaptureMemory(context.Background(), userAuth, cmdProj)
	if err != nil {
		t.Fatalf("project capture failed: %v", err)
	}
	cmdSub := baseCaptureCmd(projectID)
	cmdSub.ProjectID = nil
	cmdSub.TopicKey = ""
	supRes, err := svc.SupersedeMemory(context.Background(), userAuth, rProj.Memory.ID, capture.ExplicitSupersedeCommand{
		Capture:      cmdSub,
		ChangeReason: "inherit scope",
	})
	if err != nil {
		t.Fatalf("SupersedeMemory failed: %v", err)
	}
	if supRes.NewMemory.TopicKey != rProj.Memory.TopicKey {
		t.Fatalf("expected inherited topic_key %q, got %q", rProj.Memory.TopicKey, supRes.NewMemory.TopicKey)
	}
}

func TestSupersedeMemory_DestinationScopeAuthorizationAndCoverage(t *testing.T) {
	store := newFakeMemoryStore()
	projectA := uuid.New()
	projectB := uuid.New()
	agentID := uuid.New()
	auth := testAuth(identity.PrincipalUser, projectA, agentID, identity.ScopeProjectShared, identity.ScopeTaskShared)

	svc, _ := capture.NewService(store, store, fakeTaskAccess{snap: capture.TaskAccessSnapshot{CanRead: true, CanWrite: false}}, &fakeOpaqueQueue{}, nil)

	cProj := baseCaptureCmd(projectA)
	cProj.EpistemicStatus = memory.EpistemicObserved
	rProj, err := svc.CaptureMemory(context.Background(), auth, cProj)
	if err != nil {
		t.Fatalf("capture failed: %v", err)
	}

	// Destination ScopeGlobalPublic without IsAdmin -> ErrForbidden
	cGlobal := baseCaptureCmd(projectA)
	cGlobal.ScopeType = identity.ScopeGlobalPublic
	if _, err := svc.SupersedeMemory(context.Background(), auth, rProj.Memory.ID, capture.ExplicitSupersedeCommand{
		Capture:      cGlobal,
		ChangeReason: "escalate to global",
	}); !errors.Is(err, capture.ErrForbidden) {
		t.Fatalf("expected ErrForbidden for non-admin global_public supersede destination, got %v", err)
	}

	// Destination unauthorized ProjectB -> ErrForbidden
	cOtherProj := baseCaptureCmd(projectB)
	if _, err := svc.SupersedeMemory(context.Background(), auth, rProj.Memory.ID, capture.ExplicitSupersedeCommand{
		Capture:      cOtherProj,
		ChangeReason: "cross-project",
	}); !errors.Is(err, capture.ErrForbidden) {
		t.Fatalf("expected ErrForbidden for unauthorized project supersede destination, got %v", err)
	}

	// Destination ScopeTaskShared where CanWrite=false -> ErrForbidden
	cTask := baseCaptureCmd(projectA)
	cTask.ScopeType = identity.ScopeTaskShared
	cTask.TaskID = "task-99"
	if _, err := svc.SupersedeMemory(context.Background(), auth, rProj.Memory.ID, capture.ExplicitSupersedeCommand{
		Capture:      cTask,
		ChangeReason: "cross-task",
	}); !errors.Is(err, capture.ErrForbidden) {
		t.Fatalf("expected ErrForbidden for task_shared destination without write, got %v", err)
	}

	// Original memory must still be active after failed destination checks
	if store.memories[rProj.Memory.ID].Status != memory.StatusActive {
		t.Fatalf("expected original memory to remain active")
	}

	// Task_shared read-only actor trying direct UpdateMemory -> ErrForbidden
	svcReadOnly, _ := capture.NewService(store, store, fakeTaskAccess{snap: capture.TaskAccessSnapshot{CanRead: true, CanWrite: true}}, &fakeOpaqueQueue{}, nil)
	tCmd := baseCaptureCmd(projectA)
	tCmd.ScopeType = identity.ScopeTaskShared
	tCmd.TaskID = "task-ro"
	tRes, err := svcReadOnly.CaptureMemory(context.Background(), auth, tCmd)
	if err != nil {
		t.Fatalf("task capture failed: %v", err)
	}
	if _, err := svc.UpdateMemory(context.Background(), auth, tRes.Memory.ID, capture.UpdateCommand{
		Summary: "s", Content: "c",
	}); !errors.Is(err, capture.ErrForbidden) {
		t.Fatalf("expected ErrForbidden for read-only task_shared update, got %v", err)
	}
}

func TestCaptureService_SyncTaskSelectorAndArchiveNotFound(t *testing.T) {
	store := newFakeMemoryStore()
	projectID := uuid.New()
	agentID := uuid.New()
	auth := testAuth(identity.PrincipalUser, projectID, agentID, identity.ScopeTaskShared)
	svc, _ := capture.NewService(store, store, fakeTaskAccess{snap: capture.TaskAccessSnapshot{CanRead: true, CanWrite: true}}, &fakeOpaqueQueue{}, nil)

	cmd := baseCaptureCmd(projectID)
	cmd.ScopeType = identity.ScopeTaskShared
	cmd.TaskID = "t-sync"
	res, err := svc.CaptureMemory(context.Background(), auth, cmd)
	if err != nil {
		t.Fatalf("capture failed: %v", err)
	}

	// Passing TaskID when CanRead && CanWrite must execute synchronously (not opaque)
	upRes, err := svc.UpdateMemory(context.Background(), auth, res.Memory.ID, capture.UpdateCommand{
		TaskID: "t-sync", Summary: "sync up", Content: "sync content",
	})
	if err != nil || upRes.Accepted || upRes.Memory.Version != 2 {
		t.Fatalf("expected synchronous update, got res=%+v err=%v", upRes, err)
	}

	// ArchiveMemory on missing ID -> ErrNotFound
	if _, err := svc.ArchiveMemory(context.Background(), auth, uuid.New(), capture.ArchiveCommand{Reason: "missing"}); !errors.Is(err, capture.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	// Store error during ArchiveMemory -> non-nil error
	if _, err := svc.ArchiveMemory(context.Background(), auth, res.Memory.ID, capture.ArchiveCommand{Reason: "ok"}); err != nil {
		t.Fatalf("archive failed: %v", err)
	}
}

func TestCaptureService_ExecuteSupersedeReplacementTx(t *testing.T) {
	store := newFakeMemoryStore()
	svc, _ := capture.NewService(store, store, fakeTaskAccess{}, &fakeOpaqueQueue{}, nil)
	projectID := uuid.New()
	agentID := uuid.New()
	auth := testAuth(identity.PrincipalUser, projectID, agentID, identity.ScopeProjectShared)

	res, err := svc.CaptureMemory(context.Background(), auth, baseCaptureCmd(projectID))
	if err != nil {
		t.Fatalf("capture failed: %v", err)
	}
	repCmd := baseCaptureCmd(projectID)
	repCmd.Content = "corrected content"
	repCmd.EpistemicStatus = memory.EpistemicVerified

	var repView *capture.MemoryView
	err = store.ExecTx(context.Background(), func(reg memory.RepositoryRegistry) error {
		oldMem, getErr := reg.Memories().GetMemoryByID(context.Background(), auth.TenantID, res.Memory.ID)
		if getErr != nil {
			return getErr
		}
		v, txErr := svc.ExecuteSupersedeReplacementTx(context.Background(), reg, auth, oldMem, repCmd, "corrected_by_user")
		repView = v
		return txErr
	})
	if err != nil || repView == nil || store.memories[res.Memory.ID].Status != memory.StatusSuperseded {
		t.Fatalf("ExecuteSupersedeReplacementTx failed: err=%v repView=%+v", err, repView)
	}
}

func TestCaptureService_ExecuteSupersedeReplacementTx_ScopeDenied(t *testing.T) {
	store := newFakeMemoryStore()
	svc, _ := capture.NewService(store, store, fakeTaskAccess{snap: capture.TaskAccessSnapshot{CanRead: true, CanWrite: false}}, &fakeOpaqueQueue{}, nil)
	projectID := uuid.New()
	agentID := uuid.New()
	auth := testAuth(identity.PrincipalUser, projectID, agentID, identity.ScopeProjectShared, identity.ScopeTaskShared)

	res, err := svc.CaptureMemory(context.Background(), auth, baseCaptureCmd(projectID))
	if err != nil {
		t.Fatalf("capture failed: %v", err)
	}
	repCmd := baseCaptureCmd(projectID)
	repCmd.ScopeType = identity.ScopeGlobalPublic

	err = store.ExecTx(context.Background(), func(reg memory.RepositoryRegistry) error {
		oldMem, _ := reg.Memories().GetMemoryByID(context.Background(), auth.TenantID, res.Memory.ID)
		_, txErr := svc.ExecuteSupersedeReplacementTx(context.Background(), reg, auth, oldMem, repCmd, "corrected_by_user")
		return txErr
	})
	if !errors.Is(err, capture.ErrForbidden) {
		t.Fatalf("expected ErrForbidden for unauthorized destination scope, got %v", err)
	}
}
