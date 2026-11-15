package feedback_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/romero-project/romero/internal/application/feedback"
	"github.com/romero-project/romero/internal/domain/identity"
	"github.com/romero-project/romero/internal/domain/memory"
)

type spyFeedbackGraphNotifier struct {
	statusChanges []memory.MemoryStatus
}

func (s *spyFeedbackGraphNotifier) ApplyMemoryUpsert(memory.Memory) {}
func (s *spyFeedbackGraphNotifier) ApplyMemoryStatusChange(_ uuid.UUID, _ *uuid.UUID, _ uuid.UUID, status memory.MemoryStatus, _ *time.Time) {
	s.statusChanges = append(s.statusChanges, status)
}
func (s *spyFeedbackGraphNotifier) ApplyRelationsAdded([]memory.MemoryRelation) {}
func (s *spyFeedbackGraphNotifier) ApplyEmbeddingUpsert(uuid.UUID, *uuid.UUID, uuid.UUID, []float32) {
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
