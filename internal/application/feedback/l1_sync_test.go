package feedback_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/romero-project/romero/internal/application/capture"
	"github.com/romero-project/romero/internal/application/feedback"
	"github.com/romero-project/romero/internal/domain/identity"
	"github.com/romero-project/romero/internal/domain/memory"
	"github.com/romero-project/romero/internal/domain/provenance"
)

type spyFeedbackGraphNotifier struct {
	upserts       []memory.Memory
	statusChanges []memory.MemoryStatus
	relations     []memory.MemoryRelation
}

func (s *spyFeedbackGraphNotifier) ApplyMemoryUpsert(m memory.Memory) {
	s.upserts = append(s.upserts, m)
}
func (s *spyFeedbackGraphNotifier) ApplyMemoryStatusChange(_ uuid.UUID, _ *uuid.UUID, _ uuid.UUID, status memory.MemoryStatus, _ *time.Time) {
	s.statusChanges = append(s.statusChanges, status)
}
func (s *spyFeedbackGraphNotifier) ApplyRelationsAdded(rels []memory.MemoryRelation) {
	s.relations = append(s.relations, rels...)
}
func (s *spyFeedbackGraphNotifier) ApplyEmbeddingUpsert(uuid.UUID, *uuid.UUID, uuid.UUID, []float32) {
}

type fakeL1Replacer struct{}

func (fakeL1Replacer) ExecuteSupersedeReplacementTx(_ context.Context, _ memory.RepositoryRegistry, auth identity.AuthContext, oldMem *memory.Memory, cmd capture.CaptureCommand, _ string) (*capture.MemoryView, error) {
	now := time.Now().UTC()
	return &capture.MemoryView{
		ID:              uuid.New(),
		TenantID:        auth.TenantID,
		ProjectID:       oldMem.ProjectID,
		Type:            cmd.Type,
		ScopeType:       cmd.ScopeType,
		Status:          memory.StatusActive,
		Summary:         cmd.Summary,
		Content:         cmd.Content,
		EpistemicStatus: cmd.EpistemicStatus,
		Importance:      cmd.Importance,
		PrimarySourceID: uuid.New(),
		ObservedAt:      now,
		ValidFrom:       now,
		CreatedAt:       now,
		UpdatedAt:       now,
	}, nil
}

func TestFeedbackService_QuarantineNotifiesL1(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	projectID := uuid.New()
	memID := uuid.New()

	store := newFakeFeedbackStore()
	store.memories[memID] = memory.Memory{
		ID:              memID,
		TenantID:        tenantID,
		ProjectID:       &projectID,
		Type:            memory.TypeSemantic,
		ScopeType:       identity.ScopeProjectShared,
		Status:          memory.StatusActive,
		Summary:         "low conf",
		Content:         "body",
		EpistemicStatus: memory.EpistemicObserved,
		AlphaScore:      3.5,
		BetaScore:       6.5,
		Confidence:      0.35,
		Version:         1,
	}

	svc, err := feedback.NewService(store, store, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}
	notifier := &spyFeedbackGraphNotifier{}
	svc.SetGraphNotifier(notifier)

	auth := identity.AuthContext{
		TenantID:        tenantID,
		PrincipalID:     uuid.New(),
		PrincipalType:   identity.PrincipalUser,
		AllowedProjects: []uuid.UUID{projectID},
		AllowedScopes:   []identity.ScopeType{identity.ScopeProjectShared},
	}

	res, err := svc.RecordFeedback(context.Background(), auth, feedback.FeedbackCommand{
		MemoryID: memID,
		Signal:   memory.SignalMisleadingFailure,
	})
	if err != nil {
		t.Fatalf("RecordFeedback failed: %v", err)
	}
	if !res.Quarantined || res.NewStatus != memory.StatusConflicted {
		t.Fatalf("expected quarantine to conflicted, got %+v", res)
	}
	if len(notifier.statusChanges) != 1 || notifier.statusChanges[0] != memory.StatusConflicted {
		t.Fatalf("expected L1 StatusConflicted notification, got %+v", notifier.statusChanges)
	}
}

func TestFeedbackService_BayesianAndCorrectedByUserNotifyL1(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	projectID := uuid.New()
	memID := uuid.New()

	store := newFakeFeedbackStore()
	store.memories[memID] = memory.Memory{
		ID:              memID,
		TenantID:        tenantID,
		ProjectID:       &projectID,
		Type:            memory.TypeSemantic,
		ScopeType:       identity.ScopeProjectShared,
		Status:          memory.StatusActive,
		Summary:         "active fact",
		Content:         "initial body",
		EpistemicStatus: memory.EpistemicObserved,
		AlphaScore:      2.0,
		BetaScore:       2.0,
		Confidence:      0.5,
		Version:         1,
	}

	svc, err := feedback.NewService(store, store, nil, nil, fakeL1Replacer{}, nil)
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}
	notifier := &spyFeedbackGraphNotifier{}
	svc.SetGraphNotifier(notifier)

	auth := identity.AuthContext{
		TenantID:        tenantID,
		PrincipalID:     uuid.New(),
		PrincipalType:   identity.PrincipalUser,
		AllowedProjects: []uuid.UUID{projectID},
		AllowedScopes:   []identity.ScopeType{identity.ScopeProjectShared},
	}

	// 1. Non-quarantining helpful_success updates L1 via ApplyMemoryUpsert with new Bayesian scores.
	res1, err := svc.RecordFeedback(context.Background(), auth, feedback.FeedbackCommand{
		MemoryID: memID,
		Signal:   memory.SignalHelpfulSuccess,
	})
	if err != nil {
		t.Fatalf("RecordFeedback helpful_success failed: %v", err)
	}
	if len(notifier.upserts) != 1 || notifier.upserts[0].ID != memID || notifier.upserts[0].AlphaScore != res1.NewAlpha {
		t.Fatalf("expected L1 ApplyMemoryUpsert with updated alpha=%v, got %+v", res1.NewAlpha, notifier.upserts)
	}

	// 2. corrected_by_user with ReplacementMemory emits StatusSuperseded, replacement upsert, and supersedes relation.
	res2, err := svc.RecordFeedback(context.Background(), auth, feedback.FeedbackCommand{
		MemoryID: memID,
		Signal:   memory.SignalCorrectedByUser,
		ReplacementMemory: &capture.CaptureCommand{
			ProjectID:        &projectID,
			Type:             memory.TypeSemantic,
			ScopeType:        identity.ScopeProjectShared,
			Summary:          "corrected fact",
			Content:          "corrected body",
			EpistemicStatus:  memory.EpistemicVerified,
			Importance:       0.9,
			SourceType:       provenance.SourceAgentExecution,
			ExternalSourceID: "u-fix",
		},
	})
	if err != nil {
		t.Fatalf("RecordFeedback corrected_by_user failed: %v", err)
	}
	if res2.ReplacementMemoryID == nil {
		t.Fatal("expected ReplacementMemoryID")
	}
	if len(notifier.statusChanges) != 1 || notifier.statusChanges[0] != memory.StatusSuperseded {
		t.Fatalf("expected StatusSuperseded notification, got %+v", notifier.statusChanges)
	}
	if len(notifier.relations) != 1 || notifier.relations[0].RelationType != memory.RelationSupersedes ||
		notifier.relations[0].FromMemoryID != *res2.ReplacementMemoryID || notifier.relations[0].ToMemoryID != memID {
		t.Fatalf("expected supersedes relation in L1, got %+v", notifier.relations)
	}

	// 3. Direct ExecuteFeedbackTx and ExecuteOpaqueFeedbackTx coverage.
	mem2ID := uuid.New()
	store.memories[mem2ID] = memory.Memory{
		ID: mem2ID, TenantID: tenantID, ProjectID: &projectID, Type: memory.TypeSemantic,
		ScopeType: identity.ScopeProjectShared, Status: memory.StatusActive,
		Summary: "tx", Content: "tx", EpistemicStatus: memory.EpistemicObserved,
		AlphaScore: 2.0, BetaScore: 2.0, Confidence: 0.5, Version: 1,
	}
	if err := store.ExecTx(context.Background(), func(reg memory.RepositoryRegistry) error {
		if _, txErr := svc.ExecuteFeedbackTx(context.Background(), reg, auth, feedback.FeedbackCommand{
			MemoryID: mem2ID, Signal: memory.SignalHelpfulSuccess,
		}); txErr != nil {
			t.Fatalf("ExecuteFeedbackTx failed: %v", txErr)
		}
		if _, txErr := svc.ExecuteOpaqueFeedbackTx(context.Background(), reg, auth, feedback.FeedbackCommand{
			MemoryID: mem2ID, Signal: memory.SignalHelpfulSuccess,
		}); txErr != nil {
			t.Fatalf("ExecuteOpaqueFeedbackTx failed: %v", txErr)
		}
		return nil
	}); err != nil {
		t.Fatalf("ExecTx failed: %v", err)
	}
}
