package capture_test

import (
	"context"
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
	statusChanges []memory.MemoryStatus
	relations     []memory.MemoryRelation
}

func (s *spyGraphNotifier) ApplyMemoryUpsert(m memory.Memory) {
	s.upserts = append(s.upserts, m.ID)
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
