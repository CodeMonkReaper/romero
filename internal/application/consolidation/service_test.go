package consolidation_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/romero-project/romero/internal/application/consolidation"
	"github.com/romero-project/romero/internal/domain/governor"
	"github.com/romero-project/romero/internal/domain/identity"
	"github.com/romero-project/romero/internal/domain/memory"
	"github.com/romero-project/romero/internal/domain/provenance"
)

type fakeUOW struct{ reg *fakeReg }

func (f *fakeUOW) ExecTx(_ context.Context, fn func(memory.RepositoryRegistry) error) error {
	return fn(f.reg)
}

type fakeReg struct {
	sources  []provenance.MemorySource
	mems     map[uuid.UUID]memory.Memory
	versions []memory.MemoryVersion
	rels     []memory.MemoryRelation
	jobs     []memory.MemoryJob
	audit    *fakeAudit
}

func newFakeReg() *fakeReg {
	return &fakeReg{mems: make(map[uuid.UUID]memory.Memory), audit: &fakeAudit{}}
}

func (r *fakeReg) Sources() memory.SourceRepository    { return r }
func (r *fakeReg) Memories() memory.MemoryRepository   { return r }
func (r *fakeReg) Feedback() memory.FeedbackRepository { return nil }
func (r *fakeReg) Jobs() memory.JobRepository          { return r }
func (r *fakeReg) Audit() memory.AuditLogger           { return r.audit }

func (r *fakeReg) InsertSource(_ context.Context, src provenance.MemorySource) error {
	r.sources = append(r.sources, src)
	return nil
}
func (r *fakeReg) GetMemoryByID(_ context.Context, tenantID, memoryID uuid.UUID) (*memory.Memory, error) {
	if m, ok := r.mems[memoryID]; ok && m.TenantID == tenantID {
		cp := m
		return &cp, nil
	}
	return nil, memory.ErrNotFound
}
func (r *fakeReg) GetMemoryByIdempotencyKey(context.Context, uuid.UUID, string) (*memory.Memory, error) {
	return nil, memory.ErrNotFound
}
func (r *fakeReg) LockActiveTopicMemory(_ context.Context, scope identity.MemoryScopeTarget, topicKey string) (*memory.Memory, error) {
	for _, m := range r.mems {
		if m.TenantID == scope.TenantID && m.Status == memory.StatusActive && m.TopicKey == topicKey {
			cp := m
			return &cp, nil
		}
	}
	return nil, memory.ErrNotFound
}
func (r *fakeReg) InsertMemory(_ context.Context, m memory.Memory) error {
	r.mems[m.ID] = m
	return nil
}
func (r *fakeReg) UpdateMemoryState(_ context.Context, m memory.Memory) error {
	r.mems[m.ID] = m
	return nil
}
func (r *fakeReg) ListVersionsByMemoryID(context.Context, uuid.UUID) ([]memory.MemoryVersion, error) {
	return nil, nil
}
func (r *fakeReg) InsertMemoryVersion(_ context.Context, v memory.MemoryVersion) error {
	r.versions = append(r.versions, v)
	return nil
}
func (r *fakeReg) CloseMemoryVersionValidity(context.Context, uuid.UUID, int, time.Time) error {
	return nil
}
func (r *fakeReg) GetMemoryVersionPointInTime(context.Context, uuid.UUID, time.Time, time.Time) (*memory.MemoryVersion, error) {
	return nil, memory.ErrNotFound
}
func (r *fakeReg) ListActiveRelationsByMemoryID(context.Context, uuid.UUID, uuid.UUID) ([]memory.MemoryRelation, error) {
	return nil, nil
}
func (r *fakeReg) InsertMemoryRelation(_ context.Context, rel memory.MemoryRelation) error {
	r.rels = append(r.rels, rel)
	return nil
}
func (r *fakeReg) EnqueueMemoryJob(_ context.Context, job memory.MemoryJob) error {
	r.jobs = append(r.jobs, job)
	return nil
}

type fakeAudit struct{ allowed, denied int }

func (a *fakeAudit) RecordAllowed(context.Context, memory.AuditEvent) error { a.allowed++; return nil }
func (a *fakeAudit) RecordDenied(context.Context, memory.AuditEvent) error  { a.denied++; return nil }

type fakeConsolidationRepo struct {
	cands    []memory.Memory
	vecs     map[uuid.UUID][]float32
	verified []memory.Memory
}

func (f *fakeConsolidationRepo) ListREMConsolidationCandidates(context.Context, uuid.UUID, *uuid.UUID, float32) ([]memory.Memory, map[uuid.UUID][]float32, error) {
	return f.cands, f.vecs, nil
}
func (f *fakeConsolidationRepo) ListActiveVerifiedByProject(context.Context, uuid.UUID, *uuid.UUID, time.Time) ([]memory.Memory, error) {
	return f.verified, nil
}

type fakeSLM struct {
	draft   memory.DistilledMemoryDraft
	delay   time.Duration
	unloads atomic.Int32
}

func (f *fakeSLM) DistillCluster(ctx context.Context, _ []memory.Memory) (memory.DistilledMemoryDraft, error) {
	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
			return memory.DistilledMemoryDraft{}, ctx.Err()
		}
	}
	return f.draft, nil
}
func (f *fakeSLM) UnloadVRAM(context.Context) error { f.unloads.Add(1); return nil }

type fakeNLI struct {
	scores    []memory.NLIScore
	lastPairs []memory.NLIPairInput
}

func (f *fakeNLI) EvaluatePairs(_ context.Context, pairs []memory.NLIPairInput) ([]memory.NLIScore, error) {
	f.lastPairs = append([]memory.NLIPairInput(nil), pairs...)
	return f.scores, nil
}

type fakeL1 struct {
	upserts  int
	statuses int
	rels     int
}

func (l *fakeL1) ApplyMemoryUpsert(memory.Memory) { l.upserts++ }
func (l *fakeL1) ApplyMemoryStatusChange(uuid.UUID, *uuid.UUID, uuid.UUID, memory.MemoryStatus, *time.Time) {
	l.statuses++
}
func (l *fakeL1) ApplyRelationsAdded([]memory.MemoryRelation)                      { l.rels++ }
func (l *fakeL1) ApplyEmbeddingUpsert(uuid.UUID, *uuid.UUID, uuid.UUID, []float32) {}

func makeClusterFixtures(tenantID, projID uuid.UUID, now time.Time) ([]memory.Memory, map[uuid.UUID][]float32) {
	eps := make([]memory.Memory, 3)
	vecs := make(map[uuid.UUID][]float32, 3)
	for i := range eps {
		id := uuid.New()
		eps[i] = memory.Memory{
			ID: id, TenantID: tenantID, ProjectID: &projID,
			Type: memory.TypeEpisodic, ScopeType: identity.ScopeProjectShared, Status: memory.StatusActive,
			Summary: "run tests before merge", Content: "run tests before merge",
			EpistemicStatus: memory.EpistemicObserved, AlphaScore: 8, BetaScore: 2, Confidence: 0.80, Version: 1,
			PrimarySourceID: uuid.New(), Clocks: memory.TriTemporalWindow{ObservedAt: now, ValidFrom: now, CreatedAt: now},
		}
		vecs[id] = []float32{1, 0, 0}
	}
	return eps, vecs
}

func TestConsolidate_AuthorizationAndSuccessAndNLIRollback(t *testing.T) {
	ctx := context.Background()
	tenantID, projID := uuid.New(), uuid.New()
	now := time.Now().UTC()
	eps, vecs := makeClusterFixtures(tenantID, projID, now)

	verified := memory.Memory{
		ID: uuid.New(), TenantID: tenantID, ProjectID: &projID,
		Type: memory.TypeSemantic, ScopeType: identity.ScopeProjectShared, Status: memory.StatusActive,
		EpistemicStatus: memory.EpistemicVerified, Summary: "always run go vet", Content: "always run go vet",
	}

	reg := newFakeReg()
	for _, ep := range eps {
		reg.mems[ep.ID] = ep
	}
	repo := &fakeConsolidationRepo{cands: eps, vecs: vecs, verified: []memory.Memory{verified}}
	slm := &fakeSLM{draft: memory.DistilledMemoryDraft{MemoryType: memory.TypeProcedural, TopicKey: "ci.rule", Summary: "Run tests before merge", Content: "1. Run go test."}}
	nli := &fakeNLI{scores: []memory.NLIScore{
		{Entailment: 0.90}, {Entailment: 0.88}, {Entailment: 0.85}, // 3 source episodes pass
		{Contradiction: 0.05}, // 1 verified memory passes
	}}
	l1 := &fakeL1{}
	gov := governor.NewPowerGovernor(governor.Config{Profile: governor.ProfileUltra})

	svc, err := consolidation.NewService(&fakeUOW{reg: reg}, repo, slm, nli, reg.audit, l1, gov, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}

	// Agent principal must get ErrForbidden (403).
	agentAuth := identity.AuthContext{TenantID: tenantID, PrincipalID: uuid.New(), PrincipalType: identity.PrincipalAgent}
	if _, err := svc.Consolidate(ctx, agentAuth, consolidation.ConsolidateCommand{ProjectID: &projID}); !errors.Is(err, consolidation.ErrForbidden) {
		t.Fatalf("expected ErrForbidden for agent principal, got %v", err)
	}

	// Orchestrator succeeds.
	orchAuth := identity.AuthContext{
		TenantID: tenantID, PrincipalID: uuid.New(), PrincipalType: identity.PrincipalOrchestrator,
		AllowedProjects: []uuid.UUID{projID},
	}
	res, err := svc.Consolidate(ctx, orchAuth, consolidation.ConsolidateCommand{ProjectID: &projID})
	if err != nil || res.DistilledCount != 1 || res.ConsolidatedEpisodes != 3 || len(res.CreatedMemoryIDs) != 1 {
		t.Fatalf("Consolidate failed: res=%+v err=%v", res, err)
	}
	if len(reg.sources) != 1 || reg.sources[0].SourceType != provenance.SourceREMConsolidation {
		t.Fatalf("expected rem_consolidation source, got %+v", reg.sources)
	}
	if len(reg.rels) != 3 || reg.rels[0].RelationType != memory.RelationDerivedFrom {
		t.Fatalf("expected 3 derived_from edges, got %+v", reg.rels)
	}
	if l1.upserts != 1 || l1.statuses != 3 || l1.rels != 1 {
		t.Fatalf("expected L1 sync, got %+v", l1)
	}
	if slm.unloads.Load() != 1 {
		t.Fatalf("expected UnloadVRAM called once after normal REM consolidation, got %d", slm.unloads.Load())
	}

	// If NLI cross-check detects contradiction against verified, draft is discarded and episodes stay active.
	reg2 := newFakeReg()
	for _, ep := range eps {
		reg2.mems[ep.ID] = ep
	}
	nliReject := &fakeNLI{scores: []memory.NLIScore{
		{Entailment: 0.90}, {Entailment: 0.90}, {Entailment: 0.90},
		{Contradiction: 0.85}, // contradicts verified!
	}}
	svc2, _ := consolidation.NewService(&fakeUOW{reg: reg2}, repo, slm, nliReject, reg2.audit, nil, nil, func() time.Time { return now })
	res2, err := svc2.Consolidate(ctx, orchAuth, consolidation.ConsolidateCommand{ProjectID: &projID})
	if err != nil || res2.DistilledCount != 0 || res2.ConsolidatedEpisodes != 0 {
		t.Fatalf("expected discarded draft on NLI verified contradiction, got res=%+v err=%v", res2, err)
	}
}

func TestConsolidate_PreemptedUnder10ms(t *testing.T) {
	ctx := context.Background()
	tenantID, projID := uuid.New(), uuid.New()
	now := time.Now().UTC()
	eps, vecs := makeClusterFixtures(tenantID, projID, now)

	reg := newFakeReg()
	for _, ep := range eps {
		reg.mems[ep.ID] = ep
	}
	repo := &fakeConsolidationRepo{cands: eps, vecs: vecs}
	slm := &fakeSLM{delay: 250 * time.Millisecond}
	var unloadCalled atomic.Bool
	gov := governor.NewPowerGovernor(governor.Config{
		Profile: governor.ProfileUltra,
		OnUnloadVRAM: func(context.Context) error {
			unloadCalled.Store(true)
			return nil
		},
	})
	svc, _ := consolidation.NewService(&fakeUOW{reg: reg}, repo, slm, &fakeNLI{}, reg.audit, nil, gov, nil)

	adminAuth := identity.AuthContext{TenantID: tenantID, PrincipalID: uuid.New(), PrincipalType: identity.PrincipalUser, IsAdmin: true}

	var wg sync.WaitGroup
	var res *consolidation.ConsolidateResult
	var consErr error
	wg.Add(1)
	go func() {
		defer wg.Done()
		res, consErr = svc.Consolidate(ctx, adminAuth, consolidation.ConsolidateCommand{ProjectID: &projID})
	}()

	time.Sleep(20 * time.Millisecond)
	start := time.Now()
	endReq := gov.BeginAgentRequest()
	defer endReq()
	wg.Wait()
	elapsed := time.Since(start)

	if consErr != nil || res == nil || !res.Preempted || res.ConsolidatedEpisodes != 0 {
		t.Fatalf("expected preempted with 0 consolidated episodes, got res=%+v err=%v", res, consErr)
	}
	if elapsed > 50*time.Millisecond {
		t.Fatalf("preemption took too long: %v", elapsed)
	}
	time.Sleep(20 * time.Millisecond)
	if !unloadCalled.Load() {
		t.Fatalf("expected async UnloadVRAM to be invoked on preemption")
	}
}

func TestConsolidate_DryRunAndValidationErrors(t *testing.T) {
	ctx := context.Background()
	tenantID, projID := uuid.New(), uuid.New()
	now := time.Now().UTC()
	eps, vecs := makeClusterFixtures(tenantID, projID, now)

	reg := newFakeReg()
	repo := &fakeConsolidationRepo{cands: eps, vecs: vecs}
	if _, err := consolidation.NewService(nil, repo, nil, nil, reg.audit, nil, nil, nil); err == nil {
		t.Fatalf("expected error on nil deps")
	}
	svc, _ := consolidation.NewService(&fakeUOW{reg: reg}, repo, &fakeSLM{}, &fakeNLI{}, reg.audit, nil, nil, nil)

	if _, err := svc.Consolidate(ctx, identity.AuthContext{}, consolidation.ConsolidateCommand{}); !errors.Is(err, consolidation.ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized, got %v", err)
	}
	orchOtherProj := identity.AuthContext{TenantID: tenantID, PrincipalID: uuid.New(), PrincipalType: identity.PrincipalOrchestrator, AllowedProjects: []uuid.UUID{uuid.New()}}
	if _, err := svc.Consolidate(ctx, orchOtherProj, consolidation.ConsolidateCommand{}); !errors.Is(err, consolidation.ErrForbidden) {
		t.Fatalf("expected ErrForbidden when non-admin orchestrator omits project_id, got %v", err)
	}
	if _, err := svc.Consolidate(ctx, orchOtherProj, consolidation.ConsolidateCommand{ProjectID: &projID}); !errors.Is(err, consolidation.ErrForbidden) {
		t.Fatalf("expected ErrForbidden for disallowed project, got %v", err)
	}
	admin := identity.AuthContext{TenantID: tenantID, PrincipalID: uuid.New(), PrincipalType: identity.PrincipalUser, IsAdmin: true}
	res, err := svc.Consolidate(ctx, admin, consolidation.ConsolidateCommand{ProjectID: &projID, DryRun: true})
	if err != nil || res.ClustersFound != 1 || res.DistilledCount != 0 {
		t.Fatalf("expected DryRun to find 1 cluster and distill 0, got res=%+v err=%v", res, err)
	}

	// Reported episode in cluster produces EpistemicInferred distilled memory.
	eps[0].EpistemicStatus = memory.EpistemicReported
	regInf := newFakeReg()
	for _, ep := range eps {
		regInf.mems[ep.ID] = ep
	}
	nliPass := &fakeNLI{scores: []memory.NLIScore{{Entailment: 0.90}, {Entailment: 0.90}, {Entailment: 0.90}}}
	svcInf, _ := consolidation.NewService(&fakeUOW{reg: regInf}, repo, &fakeSLM{draft: memory.DistilledMemoryDraft{MemoryType: memory.TypeSemantic, Summary: "Inferred summary"}}, nliPass, regInf.audit, nil, nil, nil)
	resInf, err := svcInf.Consolidate(ctx, admin, consolidation.ConsolidateCommand{ProjectID: &projID})
	if err != nil || len(resInf.CreatedMemoryIDs) != 1 || regInf.mems[resInf.CreatedMemoryIDs[0]].EpistemicStatus != memory.EpistemicInferred {
		t.Fatalf("expected EpistemicInferred memory, got res=%+v err=%v", resInf, err)
	}
}

type errConsolidationRepo struct {
	candsErr    error
	verifiedErr error
	cands       []memory.Memory
	vecs        map[uuid.UUID][]float32
}

func (e errConsolidationRepo) ListREMConsolidationCandidates(context.Context, uuid.UUID, *uuid.UUID, float32) ([]memory.Memory, map[uuid.UUID][]float32, error) {
	return e.cands, e.vecs, e.candsErr
}
func (e errConsolidationRepo) ListActiveVerifiedByProject(context.Context, uuid.UUID, *uuid.UUID, time.Time) ([]memory.Memory, error) {
	return nil, e.verifiedErr
}

type errUOW struct{ err error }

func (e errUOW) ExecTx(context.Context, func(memory.RepositoryRegistry) error) error { return e.err }

type errSLM struct{ err error }

func (e errSLM) DistillCluster(context.Context, []memory.Memory) (memory.DistilledMemoryDraft, error) {
	return memory.DistilledMemoryDraft{}, e.err
}
func (e errSLM) UnloadVRAM(context.Context) error { return nil }

type errNLI struct{ err error }

func (e errNLI) EvaluatePairs(context.Context, []memory.NLIPairInput) ([]memory.NLIScore, error) {
	return nil, e.err
}

func TestConsolidate_ErrorPathsAndPartitionFiltering(t *testing.T) {
	ctx := context.Background()
	tenantID, projID := uuid.New(), uuid.New()
	now := time.Now().UTC()
	eps, vecs := makeClusterFixtures(tenantID, projID, now)
	admin := identity.AuthContext{TenantID: tenantID, PrincipalID: uuid.New(), PrincipalType: identity.PrincipalUser, IsAdmin: true}

	// 1. Candidate load error -> ErrOperational.
	svc1, _ := consolidation.NewService(&fakeUOW{reg: newFakeReg()}, errConsolidationRepo{candsErr: errors.New("db down")}, &fakeSLM{}, &fakeNLI{}, &fakeAudit{}, nil, nil, nil)
	if _, err := svc1.Consolidate(ctx, admin, consolidation.ConsolidateCommand{ProjectID: &projID}); !errors.Is(err, consolidation.ErrOperational) {
		t.Fatalf("expected ErrOperational on candsErr, got %v", err)
	}

	// 2. Verified load error -> ErrOperational.
	svc2, _ := consolidation.NewService(&fakeUOW{reg: newFakeReg()}, errConsolidationRepo{cands: eps, vecs: vecs, verifiedErr: errors.New("db down")}, &fakeSLM{}, &fakeNLI{}, &fakeAudit{}, nil, nil, nil)
	if _, err := svc2.Consolidate(ctx, admin, consolidation.ConsolidateCommand{ProjectID: &projID}); !errors.Is(err, consolidation.ErrOperational) {
		t.Fatalf("expected ErrOperational on verifiedErr, got %v", err)
	}

	// 3. SLM error & NLI error skip cluster cleanly.
	repo := &fakeConsolidationRepo{cands: eps, vecs: vecs}
	svc3, _ := consolidation.NewService(&fakeUOW{reg: newFakeReg()}, repo, errSLM{err: errors.New("slm fail")}, &fakeNLI{}, &fakeAudit{}, nil, nil, nil)
	if res, err := svc3.Consolidate(ctx, admin, consolidation.ConsolidateCommand{ProjectID: &projID}); err != nil || res.DistilledCount != 0 {
		t.Fatalf("expected slm skip, got res=%+v err=%v", res, err)
	}
	svc4, _ := consolidation.NewService(&fakeUOW{reg: newFakeReg()}, repo, &fakeSLM{draft: memory.DistilledMemoryDraft{Summary: "s"}}, errNLI{err: errors.New("nli fail")}, &fakeAudit{}, nil, nil, nil)
	if res, err := svc4.Consolidate(ctx, admin, consolidation.ConsolidateCommand{ProjectID: &projID}); err != nil || res.DistilledCount != 0 {
		t.Fatalf("expected nli skip, got res=%+v err=%v", res, err)
	}

	// 4. Tx error -> ErrOperational.
	nliPass := &fakeNLI{scores: []memory.NLIScore{{Entailment: 0.9}, {Entailment: 0.9}, {Entailment: 0.9}}}
	svc5, _ := consolidation.NewService(errUOW{err: errors.New("tx fail")}, repo, &fakeSLM{draft: memory.DistilledMemoryDraft{Summary: "s"}}, nliPass, &fakeAudit{}, nil, nil, nil)
	if _, err := svc5.Consolidate(ctx, admin, consolidation.ConsolidateCommand{ProjectID: &projID}); !errors.Is(err, consolidation.ErrOperational) {
		t.Fatalf("expected ErrOperational on tx fail, got %v", err)
	}
}

func TestConsolidate_EngineUnavailableAndNLIDirection(t *testing.T) {
	ctx := context.Background()
	tenantID, projID := uuid.New(), uuid.New()
	now := time.Now().UTC()
	eps, vecs := makeClusterFixtures(tenantID, projID, now)
	admin := identity.AuthContext{TenantID: tenantID, PrincipalID: uuid.New(), PrincipalType: identity.PrincipalUser, IsAdmin: true}
	repo := &fakeConsolidationRepo{cands: eps, vecs: vecs}

	// Nil SLM when !DryRun -> ErrEngineUnavailable; DryRun still succeeds.
	svcNil, _ := consolidation.NewService(&fakeUOW{reg: newFakeReg()}, repo, nil, &fakeNLI{}, &fakeAudit{}, nil, nil, nil)
	if _, err := svcNil.Consolidate(ctx, admin, consolidation.ConsolidateCommand{ProjectID: &projID}); !errors.Is(err, consolidation.ErrEngineUnavailable) {
		t.Fatalf("expected ErrEngineUnavailable with nil slm, got %v", err)
	}
	if res, err := svcNil.Consolidate(ctx, admin, consolidation.ConsolidateCommand{ProjectID: &projID, DryRun: true}); err != nil || res.ClustersFound != 1 {
		t.Fatalf("expected DryRun to work even when engines are nil, got res=%+v err=%v", res, err)
	}

	// Provider unavailable from SLM or NLI -> ErrEngineUnavailable without mutating memories.
	regMut := newFakeReg()
	svcSLMDown, _ := consolidation.NewService(&fakeUOW{reg: regMut}, repo, errSLM{err: memory.ErrProviderUnavailable}, &fakeNLI{}, regMut.audit, nil, nil, nil)
	if _, err := svcSLMDown.Consolidate(ctx, admin, consolidation.ConsolidateCommand{ProjectID: &projID}); !errors.Is(err, consolidation.ErrEngineUnavailable) {
		t.Fatalf("expected ErrEngineUnavailable on SLM outage, got %v", err)
	}
	svcNLIDown, _ := consolidation.NewService(&fakeUOW{reg: regMut}, repo, &fakeSLM{draft: memory.DistilledMemoryDraft{Summary: "Distilled"}}, errNLI{err: memory.ErrProviderUnavailable}, regMut.audit, nil, nil, nil)
	if _, err := svcNLIDown.Consolidate(ctx, admin, consolidation.ConsolidateCommand{ProjectID: &projID}); !errors.Is(err, consolidation.ErrEngineUnavailable) {
		t.Fatalf("expected ErrEngineUnavailable on NLI outage, got %v", err)
	}
	if len(regMut.mems) != 0 {
		t.Fatalf("expected 0 memory writes when engine unavailable, got %d", len(regMut.mems))
	}

	// NLI pair direction: distilled draft MUST be Premise and episode MUST be Hypothesis.
	regDir := newFakeReg()
	for _, ep := range eps {
		regDir.mems[ep.ID] = ep
	}
	nliRec := &fakeNLI{scores: []memory.NLIScore{{Entailment: 0.90}, {Entailment: 0.90}, {Entailment: 0.90}}}
	svcDir, _ := consolidation.NewService(&fakeUOW{reg: regDir}, repo, &fakeSLM{draft: memory.DistilledMemoryDraft{MemoryType: memory.TypeSemantic, Summary: "DISTILLED_DRAFT_SUMMARY", Content: "DISTILLED_DRAFT_CONTENT"}}, nliRec, regDir.audit, nil, nil, nil)
	if _, err := svcDir.Consolidate(ctx, admin, consolidation.ConsolidateCommand{ProjectID: &projID}); err != nil {
		t.Fatalf("Consolidate failed: %v", err)
	}
	if len(nliRec.lastPairs) != 3 {
		t.Fatalf("expected 3 NLI pairs, got %d", len(nliRec.lastPairs))
	}
	if nliRec.lastPairs[0].Premise != "DISTILLED_DRAFT_SUMMARY\nDISTILLED_DRAFT_CONTENT" {
		t.Fatalf("expected distilled draft in Premise, got %q", nliRec.lastPairs[0].Premise)
	}
}

func TestConsolidate_TopicKeyCollisionAndLifecycleHooks(t *testing.T) {
	ctx := context.Background()
	tenantID, projID := uuid.New(), uuid.New()
	now := time.Now().UTC()
	eps, vecs := makeClusterFixtures(tenantID, projID, now)
	admin := identity.AuthContext{TenantID: tenantID, PrincipalID: uuid.New(), PrincipalType: identity.PrincipalUser, IsAdmin: true}

	reg := newFakeReg()
	for _, ep := range eps {
		reg.mems[ep.ID] = ep
	}
	existing := memory.Memory{
		ID:        uuid.New(),
		TenantID:  tenantID,
		ProjectID: &projID,
		ScopeType: identity.ScopeProjectShared,
		Status:    memory.StatusActive,
		TopicKey:  "vault-rotation",
	}
	reg.mems[existing.ID] = existing

	repo := &fakeConsolidationRepo{cands: eps, vecs: vecs}
	nli := &fakeNLI{scores: []memory.NLIScore{{Entailment: 0.90}, {Entailment: 0.90}, {Entailment: 0.90}}}
	slm := &fakeSLM{draft: memory.DistilledMemoryDraft{
		MemoryType: memory.TypeProcedural,
		TopicKey:   "vault-rotation",
		Summary:    "Vault rotation procedure",
		Content:    "Consolidated Vault rotation procedure.",
	}}

	svc, err := consolidation.NewService(&fakeUOW{reg: reg}, repo, slm, nli, reg.audit, nil, nil, nil)
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}

	var beforeCalls, afterCalls int
	svc.SetREMLifecycleHooks(
		func(context.Context) error { beforeCalls++; return nil },
		func(context.Context) error { afterCalls++; return nil },
	)

	res, err := svc.Consolidate(ctx, admin, consolidation.ConsolidateCommand{ProjectID: &projID})
	if err != nil {
		t.Fatalf("Consolidate failed: %v", err)
	}
	if res.DistilledCount != 1 || len(res.CreatedMemoryIDs) != 1 {
		t.Fatalf("expected 1 distilled memory, got %+v", res)
	}
	if beforeCalls != 1 || afterCalls != 1 {
		t.Fatalf("expected lifecycle hooks called once each, got before=%d after=%d", beforeCalls, afterCalls)
	}
	created := reg.mems[res.CreatedMemoryIDs[0]]
	expectedPrefix := "vault-rotation-rem-" + created.ID.String()[:8]
	if created.TopicKey != expectedPrefix {
		t.Fatalf("expected disambiguated topic_key %q, got %q", expectedPrefix, created.TopicKey)
	}
}
