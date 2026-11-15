package capture_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/romero-project/romero/internal/application/capture"
	"github.com/romero-project/romero/internal/domain/identity"
	"github.com/romero-project/romero/internal/domain/memory"
	"github.com/romero-project/romero/internal/domain/provenance"
)

type spyGraphNotifier struct {
	upserts       []uuid.UUID
	upsertedMems  []memory.Memory
	statusChanges []memory.MemoryStatus
	relations     []memory.MemoryRelation
}

func (s *spyGraphNotifier) ApplyMemoryUpsert(m memory.Memory) {
	s.upserts = append(s.upserts, m.ID)
	s.upsertedMems = append(s.upsertedMems, m)
}
func (s *spyGraphNotifier) ApplyMemoryStatusChange(_ uuid.UUID, _ *uuid.UUID, _ uuid.UUID, status memory.MemoryStatus, _ *time.Time) {
	s.statusChanges = append(s.statusChanges, status)
}
func (s *spyGraphNotifier) ApplyRelationsAdded(rels []memory.MemoryRelation) {
	s.relations = append(s.relations, rels...)
}
func (s *spyGraphNotifier) ApplyEmbeddingUpsert(uuid.UUID, *uuid.UUID, uuid.UUID, []float32) {}

func TestCaptureExplicitRelationsAndL1Sync(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	projectID := uuid.New()
	agentA := uuid.New()
	agentB := uuid.New()

	authA := identity.AuthContext{
		TenantID:        tenantID,
		PrincipalID:     uuid.New(),
		PrincipalType:   identity.PrincipalAgent,
		AgentID:         &agentA,
		AllowedProjects: []uuid.UUID{projectID},
		AllowedScopes:   []identity.ScopeType{identity.ScopeProjectShared, identity.ScopeAgentPrivate},
	}

	store := newFakeMemoryStore()
	svc, err := capture.NewService(store, store, nil, nil, nil)
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}
	notifier := &spyGraphNotifier{}
	svc.SetGraphNotifier(notifier)

	baseCmd := func() capture.CaptureCommand {
		return capture.CaptureCommand{
			ProjectID:        &projectID,
			Type:             memory.TypeSemantic,
			ScopeType:        identity.ScopeProjectShared,
			Summary:          "target memory",
			Content:          "target content",
			EpistemicStatus:  memory.EpistemicObserved,
			Importance:       0.7,
			SourceType:       provenance.SourceAgentExecution,
			ExternalSourceID: "ext-1",
		}
	}

	targetRes, err := svc.CaptureMemory(context.Background(), authA, baseCmd())
	if err != nil {
		t.Fatalf("capture target failed: %v", err)
	}

	// Capture with valid explicit relation -> persists relation & notifies L1.
	cmdWithRel := baseCmd()
	cmdWithRel.Summary = "dependent memory"
	cmdWithRel.Content = "depends on target"
	cmdWithRel.Relations = []memory.ExplicitRelationInput{
		{ToMemoryID: targetRes.Memory.ID, RelationType: memory.RelationDependsOn, Weight: 0.9, Notes: "causal"},
	}
	depRes, err := svc.CaptureMemory(context.Background(), authA, cmdWithRel)
	if err != nil {
		t.Fatalf("capture with relation failed: %v", err)
	}
	if len(store.relations) != 1 || store.relations[0].FromMemoryID != depRes.Memory.ID || store.relations[0].ToMemoryID != targetRes.Memory.ID {
		t.Fatalf("unexpected persisted relations: %+v", store.relations)
	}
	if len(notifier.upserts) != 2 || len(notifier.relations) != 1 {
		t.Fatalf("unexpected L1 notifications: upserts=%d rels=%d", len(notifier.upserts), len(notifier.relations))
	}

	// Missing target memory -> 404 opaque ErrNotFound + denied audit.
	cmdMissing := baseCmd()
	cmdMissing.Relations = []memory.ExplicitRelationInput{
		{ToMemoryID: uuid.New(), RelationType: memory.RelationDependsOn},
	}
	if _, err := svc.CaptureMemory(context.Background(), authA, cmdMissing); !errors.Is(err, capture.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for missing relation target, got %v", err)
	}
	if len(store.denied) == 0 {
		t.Fatal("expected denied audit event for missing relation target")
	}

	// Foreign agent_private memory -> 404 opaque ErrNotFound.
	privID := uuid.New()
	store.memories[privID] = memory.Memory{
		ID:           privID,
		TenantID:     tenantID,
		ProjectID:    &projectID,
		OwnerAgentID: &agentB,
		ScopeType:    identity.ScopeAgentPrivate,
		Status:       memory.StatusActive,
	}
	cmdForeign := baseCmd()
	cmdForeign.Relations = []memory.ExplicitRelationInput{
		{ToMemoryID: privID, RelationType: memory.RelationCausedBy},
	}
	if _, err := svc.CaptureMemory(context.Background(), authA, cmdForeign); !errors.Is(err, capture.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for foreign agent_private target, got %v", err)
	}

	// Reserved relation type -> ErrValidation.
	cmdReserved := baseCmd()
	cmdReserved.Relations = []memory.ExplicitRelationInput{
		{ToMemoryID: targetRes.Memory.ID, RelationType: memory.RelationContradicts},
	}
	if _, err := svc.CaptureMemory(context.Background(), authA, cmdReserved); !errors.Is(err, capture.ErrValidation) {
		t.Fatalf("expected ErrValidation for reserved relation type, got %v", err)
	}

	// Update + Archive notify L1.
	if _, err := svc.UpdateMemory(context.Background(), authA, depRes.Memory.ID, capture.UpdateCommand{Summary: "u", Content: "u2"}); err != nil {
		t.Fatalf("UpdateMemory failed: %v", err)
	}
	if _, err := svc.ArchiveMemory(context.Background(), authA, depRes.Memory.ID, capture.ArchiveCommand{Reason: "done"}); err != nil {
		t.Fatalf("ArchiveMemory failed: %v", err)
	}
	if len(notifier.statusChanges) != 1 || notifier.statusChanges[0] != memory.StatusArchived {
		t.Fatalf("expected StatusArchived notification, got %+v", notifier.statusChanges)
	}
}

type fakeTaskAccessChecker struct {
	snap capture.TaskAccessSnapshot
	err  error
}

func (f fakeTaskAccessChecker) EvaluateTaskAccess(_ context.Context, _ identity.TaskPermissionQuery) (capture.TaskAccessSnapshot, error) {
	return f.snap, f.err
}

func TestCaptureTopicKeySupersedeAndOpaqueRelationsL1Sync(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	projectID := uuid.New()
	agentA := uuid.New()

	userAuth := identity.AuthContext{
		TenantID:        tenantID,
		PrincipalID:     uuid.New(),
		PrincipalType:   identity.PrincipalUser,
		AgentID:         &agentA,
		AllowedProjects: []uuid.UUID{projectID},
		AllowedScopes:   []identity.ScopeType{identity.ScopeProjectShared, identity.ScopeTaskShared, identity.ScopeOrgShared},
	}
	agentAuth := identity.AuthContext{
		TenantID:        tenantID,
		PrincipalID:     uuid.New(),
		PrincipalType:   identity.PrincipalAgent,
		AgentID:         &agentA,
		AllowedProjects: []uuid.UUID{projectID},
		AllowedScopes:   []identity.ScopeType{identity.ScopeProjectShared, identity.ScopeTaskShared},
	}

	store := newFakeMemoryStore()
	svc, err := capture.NewService(store, store, fakeTaskAccessChecker{snap: capture.TaskAccessSnapshot{CanRead: true, CanWrite: true}}, nil, nil)
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}
	notifier := &spyGraphNotifier{}
	svc.SetGraphNotifier(notifier)

	// 1. topic_key supersession: observed -> verified (user) emits StatusSuperseded, Upsert, RelationSupersedes with exact DB ID.
	m1, err := svc.CaptureMemory(context.Background(), userAuth, capture.CaptureCommand{
		ProjectID: &projectID, Type: memory.TypeSemantic, ScopeType: identity.ScopeProjectShared,
		TopicKey: "auth-policy", Summary: "v1", Content: "v1 body",
		EpistemicStatus: memory.EpistemicObserved, Importance: 0.6,
		SourceType: provenance.SourceAgentExecution, ExternalSourceID: "u-1",
	})
	if err != nil {
		t.Fatalf("capture m1 failed: %v", err)
	}
	m2, err := svc.CaptureMemory(context.Background(), userAuth, capture.CaptureCommand{
		ProjectID: &projectID, Type: memory.TypeSemantic, ScopeType: identity.ScopeProjectShared,
		TopicKey: "auth-policy", Summary: "v2", Content: "v2 body",
		EpistemicStatus: memory.EpistemicVerified, Importance: 0.9,
		SourceType: provenance.SourceAgentExecution, ExternalSourceID: "u-2",
	})
	if err != nil {
		t.Fatalf("capture m2 failed: %v", err)
	}
	if len(notifier.statusChanges) != 1 || notifier.statusChanges[0] != memory.StatusSuperseded {
		t.Fatalf("expected StatusSuperseded notification, got %+v", notifier.statusChanges)
	}
	if len(notifier.relations) != 1 || notifier.relations[0].RelationType != memory.RelationSupersedes ||
		notifier.relations[0].FromMemoryID != m2.Memory.ID || notifier.relations[0].ToMemoryID != m1.Memory.ID ||
		notifier.relations[0].ID != store.relations[0].ID {
		t.Fatalf("expected L1 supersedes relation with exact persisted ID, got l1=%+v store=%+v", notifier.relations, store.relations)
	}

	// 2. topic_key conflict: verified -> observed (agent) emits StatusConflicted upsert + RelationContradicts.
	m3, err := svc.CaptureMemory(context.Background(), agentAuth, capture.CaptureCommand{
		ProjectID: &projectID, Type: memory.TypeSemantic, ScopeType: identity.ScopeProjectShared,
		TopicKey: "auth-policy", Summary: "v3", Content: "v3 body",
		EpistemicStatus: memory.EpistemicObserved, Importance: 0.5,
		SourceType: provenance.SourceAgentExecution, ExternalSourceID: "a-1",
	})
	if err != nil {
		t.Fatalf("capture m3 conflict failed: %v", err)
	}
	if m3.Memory.Status != memory.StatusConflicted {
		t.Fatalf("expected StatusConflicted, got %s", m3.Memory.Status)
	}
	if len(notifier.relations) != 2 || notifier.relations[1].RelationType != memory.RelationContradicts ||
		notifier.relations[1].ID != store.relations[1].ID {
		t.Fatalf("expected L1 contradicts relation with exact persisted ID, got %+v", notifier.relations)
	}

	// 3. Explicit SupersedeMemory emits StatusSuperseded, Upsert, and RelationSupersedes.
	supRes, err := svc.SupersedeMemory(context.Background(), userAuth, m2.Memory.ID, capture.ExplicitSupersedeCommand{
		ChangeReason: "newer policy",
		Capture: capture.CaptureCommand{
			ProjectID: &projectID, Type: memory.TypeSemantic, ScopeType: identity.ScopeProjectShared,
			Summary: "v4", Content: "v4 body", EpistemicStatus: memory.EpistemicVerified,
			Importance: 0.95, SourceType: provenance.SourceAgentExecution, ExternalSourceID: "u-3",
		},
	})
	if err != nil {
		t.Fatalf("SupersedeMemory failed: %v", err)
	}
	if len(notifier.relations) != 3 || notifier.relations[2].RelationType != memory.RelationSupersedes ||
		notifier.relations[2].FromMemoryID != supRes.NewMemory.ID || notifier.relations[2].ToMemoryID != m2.Memory.ID {
		t.Fatalf("expected L1 supersedes relation from SupersedeMemory, got %+v", notifier.relations)
	}

	// 4. ExecuteCaptureTx / ExecuteOpaqueCaptureTx / ExecuteOpaqueTargetTx coverage & task_shared link check.
	if err := store.ExecTx(context.Background(), func(reg memory.RepositoryRegistry) error {
		taskTarget, _, txErr := svc.ExecuteCaptureTx(context.Background(), reg, agentAuth, capture.CaptureCommand{
			ProjectID: &projectID, TaskID: "task-1", Type: memory.TypeSemantic, ScopeType: identity.ScopeTaskShared,
			Summary: "task mem", Content: "task body", EpistemicStatus: memory.EpistemicObserved,
			Importance: 0.5, SourceType: provenance.SourceAgentExecution, ExternalSourceID: "a-2",
		})
		if txErr != nil || taskTarget == nil {
			t.Fatalf("ExecuteCaptureTx failed: %v", txErr)
		}
		if _, _, txErr := svc.ExecuteOpaqueCaptureTx(context.Background(), reg, agentAuth, capture.CaptureCommand{
			ProjectID: &projectID, TaskID: "task-1", Type: memory.TypeSemantic, ScopeType: identity.ScopeTaskShared,
			Summary: "linked task mem", Content: "linked body", EpistemicStatus: memory.EpistemicObserved,
			Importance: 0.5, SourceType: provenance.SourceAgentExecution, ExternalSourceID: "a-3",
			Relations: []memory.ExplicitRelationInput{{ToMemoryID: taskTarget.ID, RelationType: memory.RelationDependsOn}},
		}); txErr != nil {
			t.Fatalf("ExecuteOpaqueCaptureTx same-task link failed: %v", txErr)
		}
		if _, _, txErr := svc.ExecuteOpaqueCaptureTx(context.Background(), reg, agentAuth, capture.CaptureCommand{
			ProjectID: &projectID, TaskID: "task-2", Type: memory.TypeSemantic, ScopeType: identity.ScopeTaskShared,
			Summary: "cross task mem", Content: "cross body", EpistemicStatus: memory.EpistemicObserved,
			Importance: 0.5, SourceType: provenance.SourceAgentExecution, ExternalSourceID: "a-4",
			Relations: []memory.ExplicitRelationInput{{ToMemoryID: taskTarget.ID, RelationType: memory.RelationDependsOn}},
		}); !errors.Is(txErr, capture.ErrNotFound) {
			t.Fatalf("expected ErrNotFound for cross-task opaque relation, got %v", txErr)
		}
		updPayload, _ := json.Marshal(capture.UpdateCommand{Summary: "u", Content: "c"})
		if _, txErr := svc.ExecuteOpaqueTargetTx(context.Background(), reg, agentAuth, string(capture.OpaqueOpUpdate), &taskTarget.ID, updPayload); txErr != nil {
			t.Fatalf("ExecuteOpaqueTargetTx update failed: %v", txErr)
		}
		archPayload, _ := json.Marshal(capture.ArchiveCommand{Reason: "done"})
		if _, txErr := svc.ExecuteOpaqueTargetTx(context.Background(), reg, agentAuth, string(capture.OpaqueOpArchive), &taskTarget.ID, archPayload); txErr != nil {
			t.Fatalf("ExecuteOpaqueTargetTx archive failed: %v", txErr)
		}

		// Opaque supersede + ExecuteSupersedeReplacementTx + canReadMemoryTarget(ScopeTaskShared)
		taskTarget2, _, txErr := svc.ExecuteCaptureTx(context.Background(), reg, agentAuth, capture.CaptureCommand{
			ProjectID: &projectID, TaskID: "task-1", Type: memory.TypeSemantic, ScopeType: identity.ScopeTaskShared,
			Summary: "task mem 2", Content: "task body 2", EpistemicStatus: memory.EpistemicObserved,
			Importance: 0.5, SourceType: provenance.SourceAgentExecution, ExternalSourceID: "a-5",
		})
		if txErr != nil {
			t.Fatalf("ExecuteCaptureTx 2 failed: %v", txErr)
		}
		supPayload, _ := json.Marshal(capture.ExplicitSupersedeCommand{
			ChangeReason: "opaque replace",
			Capture: capture.CaptureCommand{
				ProjectID: &projectID, TaskID: "task-1", Type: memory.TypeSemantic, ScopeType: identity.ScopeTaskShared,
				Summary: "task mem 2b", Content: "task body 2b", EpistemicStatus: memory.EpistemicObserved,
				Importance: 0.6, SourceType: provenance.SourceAgentExecution, ExternalSourceID: "a-6",
			},
		})
		if _, txErr := svc.ExecuteOpaqueTargetTx(context.Background(), reg, agentAuth, "unknown_op", &taskTarget2.ID, supPayload); !errors.Is(txErr, capture.ErrValidation) {
			t.Fatalf("expected ErrValidation for unknown opaque op, got %v", txErr)
		}
		if _, txErr := svc.ExecuteOpaqueTargetTx(context.Background(), reg, agentAuth, string(capture.OpaqueOpUpdate), &taskTarget2.ID, []byte("{bad")); !errors.Is(txErr, capture.ErrValidation) {
			t.Fatalf("expected ErrValidation for malformed update JSON, got %v", txErr)
		}
		if _, txErr := svc.ExecuteOpaqueTargetTx(context.Background(), reg, agentAuth, string(capture.OpaqueOpSupersede), &taskTarget2.ID, []byte("{bad")); !errors.Is(txErr, capture.ErrValidation) {
			t.Fatalf("expected ErrValidation for malformed supersede JSON, got %v", txErr)
		}
		if _, txErr := svc.ExecuteOpaqueTargetTx(context.Background(), reg, agentAuth, string(capture.OpaqueOpArchive), &taskTarget2.ID, []byte("{bad")); !errors.Is(txErr, capture.ErrValidation) {
			t.Fatalf("expected ErrValidation for malformed archive JSON, got %v", txErr)
		}
		if _, txErr := svc.ExecuteOpaqueTargetTx(context.Background(), reg, agentAuth, string(capture.OpaqueOpSupersede), &taskTarget2.ID, supPayload); txErr != nil {
			t.Fatalf("ExecuteOpaqueTargetTx supersede failed: %v", txErr)
		}
		if _, txErr := svc.ExecuteOpaqueTargetTx(context.Background(), reg, agentAuth, string(capture.OpaqueOpUpdate), nil, updPayload); !errors.Is(txErr, capture.ErrValidation) {
			t.Fatalf("expected ErrValidation for nil targetID, got %v", txErr)
		}

		taskTarget3, _, txErr := svc.ExecuteCaptureTx(context.Background(), reg, agentAuth, capture.CaptureCommand{
			ProjectID: &projectID, TaskID: "task-1", Type: memory.TypeSemantic, ScopeType: identity.ScopeTaskShared,
			Summary: "task mem 3", Content: "task body 3", EpistemicStatus: memory.EpistemicObserved,
			Importance: 0.5, SourceType: provenance.SourceAgentExecution, ExternalSourceID: "a-7",
		})
		if txErr != nil {
			t.Fatalf("ExecuteCaptureTx 3 failed: %v", txErr)
		}
		oldMem, _ := reg.Memories().GetMemoryByID(context.Background(), tenantID, taskTarget3.ID)
		repView, txErr := svc.ExecuteSupersedeReplacementTx(context.Background(), reg, agentAuth, oldMem, capture.CaptureCommand{
			ProjectID: &projectID, TaskID: "task-1", Type: memory.TypeSemantic, ScopeType: identity.ScopeTaskShared,
			Summary: "rep 3", Content: "rep body 3", EpistemicStatus: memory.EpistemicObserved,
			Importance: 0.6, SourceType: provenance.SourceAgentExecution, ExternalSourceID: "a-8",
		}, "corrected")
		if txErr != nil {
			t.Fatalf("ExecuteSupersedeReplacementTx failed: %v", txErr)
		}
		if _, txErr := svc.ExecuteSupersedeReplacementTx(context.Background(), reg, agentAuth, nil, capture.CaptureCommand{}, "c"); !errors.Is(txErr, capture.ErrNotFound) {
			t.Fatalf("expected ErrNotFound for nil oldMem, got %v", txErr)
		}
		if _, txErr := svc.ExecuteSupersedeReplacementTx(context.Background(), reg, agentAuth, oldMem, capture.CaptureCommand{
			ProjectID: &projectID, TaskID: "task-1", Type: memory.TypeSemantic, ScopeType: identity.ScopeTaskShared,
			Summary: "bad ep", Content: "body", EpistemicStatus: memory.EpistemicVerified,
			Importance: 0.6, SourceType: provenance.SourceAgentExecution, ExternalSourceID: "a-8b",
		}, "corrected"); !errors.Is(txErr, capture.ErrForbidden) {
			t.Fatalf("expected ErrForbidden for agent assigning verified in ExecuteSupersedeReplacementTx, got %v", txErr)
		}
		missingID := uuid.New()
		if _, txErr := svc.ExecuteOpaqueTargetTx(context.Background(), reg, agentAuth, string(capture.OpaqueOpUpdate), &missingID, updPayload); !errors.Is(txErr, capture.ErrNotFound) {
			t.Fatalf("expected ErrNotFound for missing opaque target, got %v", txErr)
		}
		if _, txErr := svc.ExecuteOpaqueTargetTx(context.Background(), reg, agentAuth, string(capture.OpaqueOpUpdate), &taskTarget.ID, updPayload); !errors.Is(txErr, capture.ErrConflict) {
			t.Fatalf("expected ErrConflict for archived opaque target, got %v", txErr)
		}
		crossScopeSup, _ := json.Marshal(capture.ExplicitSupersedeCommand{
			ChangeReason: "cross",
			Capture: capture.CaptureCommand{
				ProjectID: &projectID, TaskID: "task-1", Type: memory.TypeSemantic, ScopeType: identity.ScopeProjectShared,
				Summary: "x", Content: "y", EpistemicStatus: memory.EpistemicObserved,
				Importance: 0.5, SourceType: provenance.SourceAgentExecution, ExternalSourceID: "a-8c",
			},
		})
		if _, txErr := svc.ExecuteOpaqueTargetTx(context.Background(), reg, agentAuth, string(capture.OpaqueOpSupersede), &repView.ID, crossScopeSup); !errors.Is(txErr, capture.ErrForbidden) {
			t.Fatalf("expected ErrForbidden for cross-scope opaque supersede, got %v", txErr)
		}
		return nil
	}); err != nil {
		t.Fatalf("ExecTx failed: %v", err)
	}

	// Non-opaque CaptureMemory linking to ScopeTaskShared exercises canReadMemoryTarget(ScopeTaskShared) -> allowed.
	taskSharedID := uuid.New()
	store.memories[taskSharedID] = memory.Memory{
		ID: taskSharedID, TenantID: tenantID, ProjectID: &projectID, TaskID: "task-1",
		ScopeType: identity.ScopeTaskShared, Status: memory.StatusActive,
	}
	if _, err := svc.CaptureMemory(context.Background(), agentAuth, capture.CaptureCommand{
		ProjectID: &projectID, Type: memory.TypeSemantic, ScopeType: identity.ScopeProjectShared,
		Summary: "links task", Content: "links task body", EpistemicStatus: memory.EpistemicObserved,
		Importance: 0.6, SourceType: provenance.SourceAgentExecution, ExternalSourceID: "a-9",
		Relations: []memory.ExplicitRelationInput{{ToMemoryID: taskSharedID, RelationType: memory.RelationDependsOn}},
	}); err != nil {
		t.Fatalf("CaptureMemory linking to readable task_shared failed: %v", err)
	}

	// Non-opaque CaptureMemory linking to unreadable task_shared (CanRead=false) -> ErrNotFound.
	denySvc, _ := capture.NewService(store, store, fakeTaskAccessChecker{snap: capture.TaskAccessSnapshot{CanRead: false, CanWrite: false}}, nil, nil)
	if _, err := denySvc.CaptureMemory(context.Background(), agentAuth, capture.CaptureCommand{
		ProjectID: &projectID, Type: memory.TypeSemantic, ScopeType: identity.ScopeProjectShared,
		Summary: "links denied task", Content: "links denied body", EpistemicStatus: memory.EpistemicObserved,
		Importance: 0.6, SourceType: provenance.SourceAgentExecution, ExternalSourceID: "a-10",
		Relations: []memory.ExplicitRelationInput{{ToMemoryID: taskSharedID, RelationType: memory.RelationDependsOn}},
	}); !errors.Is(err, capture.ErrNotFound) {
		t.Fatalf("expected ErrNotFound when task_shared target is unreadable, got %v", err)
	}

	// Nil TaskAccessChecker when linking to task_shared -> ErrOperational.
	nilCheckerSvc, _ := capture.NewService(store, store, nil, nil, nil)
	if _, err := nilCheckerSvc.CaptureMemory(context.Background(), agentAuth, capture.CaptureCommand{
		ProjectID: &projectID, Type: memory.TypeSemantic, ScopeType: identity.ScopeProjectShared,
		Summary: "nil checker", Content: "nil checker body", EpistemicStatus: memory.EpistemicObserved,
		Importance: 0.6, SourceType: provenance.SourceAgentExecution, ExternalSourceID: "a-11",
		Relations: []memory.ExplicitRelationInput{{ToMemoryID: taskSharedID, RelationType: memory.RelationDependsOn}},
	}); !errors.Is(err, capture.ErrOperational) {
		t.Fatalf("expected ErrOperational when TaskAccessChecker is nil, got %v", err)
	}

	if _, err := nilCheckerSvc.CaptureMemory(context.Background(), agentAuth, capture.CaptureCommand{
		ProjectID: &projectID, TaskID: "task-1", Type: memory.TypeSemantic, ScopeType: identity.ScopeTaskShared,
		Summary: "nil checker task write", Content: "body", EpistemicStatus: memory.EpistemicObserved,
		Importance: 0.6, SourceType: provenance.SourceAgentExecution, ExternalSourceID: "a-11b",
	}); !errors.Is(err, capture.ErrOperational) {
		t.Fatalf("expected ErrOperational for task_shared write with nil TaskAccessChecker, got %v", err)
	}
	if _, err := svc.CaptureMemory(context.Background(), agentAuth, capture.CaptureCommand{
		TaskID: "task-1", Type: memory.TypeSemantic, ScopeType: identity.ScopeTaskShared,
		Summary: "missing project", Content: "body", EpistemicStatus: memory.EpistemicObserved,
		Importance: 0.6, SourceType: provenance.SourceAgentExecution, ExternalSourceID: "a-11c",
	}); !errors.Is(err, capture.ErrForbidden) {
		t.Fatalf("expected ErrForbidden for task_shared write without project_id, got %v", err)
	}
	// Self-loop relation on CaptureMemory -> ErrValidation.
	if _, err := svc.CaptureMemory(context.Background(), userAuth, capture.CaptureCommand{
		ProjectID: &projectID, Type: memory.TypeSemantic, ScopeType: identity.ScopeGlobalPublic,
		Summary: "no global", Content: "body", EpistemicStatus: memory.EpistemicObserved,
		Importance: 0.6, SourceType: provenance.SourceAgentExecution, ExternalSourceID: "a-11d",
	}); !errors.Is(err, capture.ErrForbidden) {
		t.Fatalf("expected ErrForbidden for disallowed global_public scope, got %v", err)
	}
	// Self-loop / invalid relation / task_shared write denied / missing target branches.
	if _, err := denySvc.CaptureMemory(context.Background(), agentAuth, capture.CaptureCommand{
		ProjectID: &projectID, TaskID: "task-1", Type: memory.TypeSemantic, ScopeType: identity.ScopeTaskShared,
		Summary: "denied task write", Content: "body", EpistemicStatus: memory.EpistemicObserved,
		Importance: 0.6, SourceType: provenance.SourceAgentExecution, ExternalSourceID: "a-12",
	}); !errors.Is(err, capture.ErrForbidden) {
		t.Fatalf("expected ErrForbidden for task_shared write denied, got %v", err)
	}
	if _, err := svc.CaptureMemory(context.Background(), agentAuth, capture.CaptureCommand{
		ProjectID: &projectID, Type: memory.TypeSemantic, ScopeType: identity.ScopeTaskShared,
		Summary: "missing task_id", Content: "body", EpistemicStatus: memory.EpistemicObserved,
		Importance: 0.6, SourceType: provenance.SourceAgentExecution, ExternalSourceID: "a-13",
	}); !errors.Is(err, capture.ErrValidation) {
		t.Fatalf("expected ErrValidation for missing task_id on task_shared, got %v", err)
	}
	if _, err := svc.SupersedeMemory(context.Background(), agentAuth, uuid.New(), capture.ExplicitSupersedeCommand{
		ChangeReason: "missing",
		Capture: capture.CaptureCommand{
			ProjectID: &projectID, Type: memory.TypeSemantic, ScopeType: identity.ScopeProjectShared,
			Summary: "x", Content: "y", EpistemicStatus: memory.EpistemicObserved,
			Importance: 0.5, SourceType: provenance.SourceAgentExecution, ExternalSourceID: "a-14",
		},
	}); !errors.Is(err, capture.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for missing supersede target, got %v", err)
	}
	if _, err := svc.UpdateMemory(context.Background(), agentAuth, uuid.New(), capture.UpdateCommand{Summary: "x", Content: "y"}); !errors.Is(err, capture.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for missing update target, got %v", err)
	}
	if _, err := svc.ArchiveMemory(context.Background(), agentAuth, uuid.New(), capture.ArchiveCommand{Reason: "x"}); !errors.Is(err, capture.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for missing archive target, got %v", err)
	}

	// Idempotency replay & conflict, invalid importance, and supersede epistemic downgrade/missing reason.
	idemCmd := capture.CaptureCommand{
		ProjectID: &projectID, Type: memory.TypeSemantic, ScopeType: identity.ScopeProjectShared,
		Summary: "idem", Content: "idem body", EpistemicStatus: memory.EpistemicObserved,
		Importance: 0.6, SourceType: provenance.SourceAgentExecution, ExternalSourceID: "a-15",
		IdempotencyKey: "idem-k1",
	}
	if res1, err := svc.CaptureMemory(context.Background(), agentAuth, idemCmd); err != nil || !res1.Created {
		t.Fatalf("initial idempotent capture failed: %v", err)
	}
	if res2, err := svc.CaptureMemory(context.Background(), agentAuth, idemCmd); err != nil || res2.Created {
		t.Fatalf("expected idempotent replay Created=false, got res=%+v err=%v", res2, err)
	}
	idemCmd.Content = "different content"
	if _, err := svc.CaptureMemory(context.Background(), agentAuth, idemCmd); !errors.Is(err, capture.ErrConflict) {
		t.Fatalf("expected ErrConflict for idempotency key mismatch, got %v", err)
	}
	badImp := idemCmd
	badImp.IdempotencyKey = ""
	badImp.Importance = 1.5
	if _, err := svc.CaptureMemory(context.Background(), agentAuth, badImp); !errors.Is(err, capture.ErrValidation) {
		t.Fatalf("expected ErrValidation for importance > 1, got %v", err)
	}
	if _, err := svc.CaptureMemory(context.Background(), userAuth, capture.CaptureCommand{
		ProjectID: &projectID, Type: memory.TypeSemantic, ScopeType: identity.ScopeProjectShared,
		Summary: "", Content: "body", EpistemicStatus: memory.EpistemicVerified,
		Importance: 0.5, SourceType: provenance.SourceAgentExecution, ExternalSourceID: "a-16",
	}); !errors.Is(err, capture.ErrValidation) {
		t.Fatalf("expected ErrValidation for empty Summary, got %v", err)
	}
	if _, err := svc.SupersedeMemory(context.Background(), agentAuth, supRes.NewMemory.ID, capture.ExplicitSupersedeCommand{
		ChangeReason: "downgrade",
		Capture: capture.CaptureCommand{
			ProjectID: &projectID, Type: memory.TypeSemantic, ScopeType: identity.ScopeProjectShared,
			Summary: "down", Content: "body", EpistemicStatus: memory.EpistemicObserved,
			Importance: 0.5, SourceType: provenance.SourceAgentExecution, ExternalSourceID: "a-17",
		},
	}); !errors.Is(err, capture.ErrForbidden) {
		t.Fatalf("expected ErrForbidden for agent superseding verified memory, got %v", err)
	}
	// Invalid importance on UpdateMemory.
	negImp := float32(-0.5)
	if _, err := svc.UpdateMemory(context.Background(), userAuth, m3.Memory.ID, capture.UpdateCommand{
		Summary: "s", Content: "c", Importance: &negImp,
	}); !errors.Is(err, capture.ErrValidation) {
		t.Fatalf("expected ErrValidation for negative importance on UpdateMemory, got %v", err)
	}
	// Missing auth TenantID -> ErrUnauthorized across methods.
	if _, err := svc.CaptureMemory(context.Background(), identity.AuthContext{}, idemCmd); !errors.Is(err, capture.ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized, got %v", err)
	}
	if _, err := svc.UpdateMemory(context.Background(), identity.AuthContext{}, m3.Memory.ID, capture.UpdateCommand{}); !errors.Is(err, capture.ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized on UpdateMemory, got %v", err)
	}
	if _, err := svc.SupersedeMemory(context.Background(), identity.AuthContext{}, m3.Memory.ID, capture.ExplicitSupersedeCommand{}); !errors.Is(err, capture.ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized on SupersedeMemory, got %v", err)
	}
	if _, err := svc.ArchiveMemory(context.Background(), identity.AuthContext{}, m3.Memory.ID, capture.ArchiveCommand{}); !errors.Is(err, capture.ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized on ArchiveMemory, got %v", err)
	}
	if _, err := capture.NewService(nil, store, nil, nil, nil); err == nil {
		t.Fatal("expected error for nil uow")
	}
	if _, err := capture.NewService(store, nil, nil, nil, nil); err == nil {
		t.Fatal("expected error for nil audit")
	}
	// Closed memory update/archive/supersede -> ErrConflict.
	if _, err := svc.UpdateMemory(context.Background(), userAuth, m1.Memory.ID, capture.UpdateCommand{Summary: "s", Content: "c"}); !errors.Is(err, capture.ErrConflict) {
		t.Fatalf("expected ErrConflict updating superseded memory, got %v", err)
	}
	if _, err := svc.ArchiveMemory(context.Background(), userAuth, m1.Memory.ID, capture.ArchiveCommand{Reason: "r"}); !errors.Is(err, capture.ErrConflict) {
		t.Fatalf("expected ErrConflict archiving superseded memory, got %v", err)
	}
	if _, err := svc.SupersedeMemory(context.Background(), userAuth, m1.Memory.ID, capture.ExplicitSupersedeCommand{
		ChangeReason: "r",
		Capture: capture.CaptureCommand{
			ProjectID: &projectID, Type: memory.TypeSemantic, ScopeType: identity.ScopeProjectShared,
			Summary: "s", Content: "c", EpistemicStatus: memory.EpistemicVerified,
			Importance: 0.5, SourceType: provenance.SourceAgentExecution, ExternalSourceID: "a-18",
		},
	}); !errors.Is(err, capture.ErrConflict) {
		t.Fatalf("expected ErrConflict superseding superseded memory, got %v", err)
	}
}
