package capture_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/romero-project/romero/internal/application/capture"
	"github.com/romero-project/romero/internal/domain/identity"
	"github.com/romero-project/romero/internal/domain/memory"
)

type orphanTestReg struct {
	*fakeRegistry
	sn *fakeSemanticNeighborRepo
}

func (r *orphanTestReg) SemanticNeighbors() memory.SemanticNeighborRepository { return r.sn }

type fakeSemanticNeighborRepo struct {
	peers       []uuid.UUID
	hasActive   bool
	deactivated []uuid.UUID
}

func (f *fakeSemanticNeighborRepo) FindSameScopeSemanticNeighbors(context.Context, memory.Memory, []float32, float32, int, time.Time) ([]memory.Memory, error) {
	return nil, nil
}
func (f *fakeSemanticNeighborRepo) DeactivateConflictsByMemoryID(_ context.Context, _ uuid.UUID, id uuid.UUID, _ time.Time) ([]uuid.UUID, error) {
	f.deactivated = append(f.deactivated, id)
	return f.peers, nil
}
func (f *fakeSemanticNeighborRepo) HasActiveConflicts(context.Context, uuid.UUID, uuid.UUID, time.Time) (bool, error) {
	return f.hasActive, nil
}

type orphanUOW struct{ reg memory.RepositoryRegistry }

func (o *orphanUOW) ExecTx(_ context.Context, fn func(memory.RepositoryRegistry) error) error {
	return fn(o.reg)
}

func TestArchiveMemory_RestoresOrphanConflictedPeer(t *testing.T) {
	ctx := context.Background()
	tenantID, projID := uuid.New(), uuid.New()
	now := time.Now().UTC()

	m1 := memory.Memory{
		ID: uuid.New(), TenantID: tenantID, ProjectID: &projID,
		Type: memory.TypeSemantic, ScopeType: identity.ScopeProjectShared, Status: memory.StatusActive,
		Summary: "m1", Content: "m1", ContentSHA256: strings.Repeat("a", 64), TokenCount: 1, EpistemicStatus: memory.EpistemicObserved,
		AlphaScore: 2, BetaScore: 1, Confidence: 0.66, Version: 1, PrimarySourceID: uuid.New(),
		Clocks: memory.TriTemporalWindow{ObservedAt: now, ValidFrom: now, CreatedAt: now},
	}
	peer := m1
	peer.ID = uuid.New()
	peer.Status = memory.StatusConflicted

	store := newFakeMemoryStore()
	store.memories[m1.ID] = m1
	store.memories[peer.ID] = peer
	sn := &fakeSemanticNeighborRepo{peers: []uuid.UUID{peer.ID}, hasActive: false}
	notifier := &spyGraphNotifier{}

	svc, err := capture.NewService(&orphanUOW{reg: &orphanTestReg{fakeRegistry: &fakeRegistry{s: store}, sn: sn}}, store, nil, nil, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}
	svc.SetGraphNotifier(notifier)

	auth := identity.AuthContext{
		TenantID: tenantID, PrincipalID: uuid.New(), PrincipalType: identity.PrincipalAgent,
		AllowedProjects: []uuid.UUID{projID}, AllowedScopes: []identity.ScopeType{identity.ScopeProjectShared},
	}
	if _, err := svc.ArchiveMemory(ctx, auth, m1.ID, capture.ArchiveCommand{Reason: "cleanup"}); err != nil {
		t.Fatalf("ArchiveMemory failed: %v", err)
	}
	if len(sn.deactivated) != 1 || sn.deactivated[0] != m1.ID {
		t.Fatalf("expected DeactivateConflictsByMemoryID(%s), got %+v", m1.ID, sn.deactivated)
	}
	if store.memories[peer.ID].Status != memory.StatusActive {
		t.Fatalf("expected peer restored to active, got %s", store.memories[peer.ID].Status)
	}
	if len(notifier.upserts) == 0 {
		t.Fatalf("expected restored peer synced to L1")
	}
}
