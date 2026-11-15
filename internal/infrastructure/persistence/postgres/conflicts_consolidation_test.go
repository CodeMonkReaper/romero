package postgres_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/romero-project/romero/internal/domain/identity"
	"github.com/romero-project/romero/internal/domain/memory"
	"github.com/romero-project/romero/internal/domain/provenance"
	"github.com/romero-project/romero/internal/infrastructure/persistence/postgres"
)

func testUnitVec(dim int, primaryIdx int, secondaryWeight float32) []float32 {
	vec := make([]float32, dim)
	vec[primaryIdx] = 1.0
	if secondaryWeight != 0 {
		alt := (primaryIdx + 1) % dim
		vec[alt] = secondaryWeight
	}
	return vec
}

func seedTestTenantProjectSource(t *testing.T, ctx context.Context, pool *pgxpool.Pool, store *postgres.Store, now time.Time) (uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID) {
	t.Helper()
	ids := seedTenantProjectAgentPrincipal(t, ctx, pool, []string{"agent_private", "task_shared", "project_shared", "org_shared", "global_public"})
	sourceID := uuid.New()
	err := store.ExecTx(ctx, func(reg memory.RepositoryRegistry) error {
		return reg.Sources().InsertSource(ctx, provenance.MemorySource{
			ID:         sourceID,
			TenantID:   ids.tenantID,
			ProjectID:  &ids.projectID,
			SourceType:       provenance.SourceAgentExecution,
			ExternalSourceID: "ext-" + sourceID.String(),
			CreatedAt:        now,
		})
	})
	if err != nil {
		t.Fatalf("InsertSource failed: %v", err)
	}
	return ids.tenantID, ids.projectID, ids.agentID, sourceID
}

func insertTestMemoryWithVec(
	t *testing.T,
	ctx context.Context,
	store *postgres.Store,
	m memory.Memory,
	vec []float32,
) {
	t.Helper()
	sum := sha256.Sum256([]byte(m.Content))
	m.ContentSHA256 = hex.EncodeToString(sum[:])
	if m.TokenCount == 0 {
		m.TokenCount = 12
	}
	if m.Version == 0 {
		m.Version = 1
	}
	if err := store.ExecTx(ctx, func(reg memory.RepositoryRegistry) error {
		return reg.Memories().InsertMemory(ctx, m)
	}); err != nil {
		t.Fatalf("InsertMemory failed: %v", err)
	}
	if vec != nil {
		if err := store.Embeddings().UpsertMemoryEmbedding(ctx, memory.MemoryEmbedding{
			MemoryID:  m.ID,
			DenseVec:  vec,
			ModelName: "bge-m3",
			UpdatedAt: m.UpdatedAt,
		}); err != nil {
			t.Fatalf("UpsertMemoryEmbedding failed: %v", err)
		}
	}
}

func TestSemanticNeighborsSamePartitionOnly(t *testing.T) {
	pool := openTestPool(t)
	store, err := postgres.NewStore(pool)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)

	tenantID, projectID, agentA, sourceID := seedTestTenantProjectSource(t, ctx, pool, store, now)
	agentB := uuid.New()
	_, err = pool.Exec(ctx, `INSERT INTO agents (id, tenant_id, external_key, name) VALUES ($1, $2, $3, $4)`, agentB, tenantID, "agent-b-"+agentB.String()[:8], "Agent B")
	if err != nil {
		t.Fatalf("insert agentB: %v", err)
	}

	baseVec := testUnitVec(1024, 0, 0)
	nearVec := testUnitVec(1024, 0, 0.2) // cos ~ 0.98
	farVec := testUnitVec(1024, 1, 0)    // cos = 0.0

	target := memory.Memory{
		ID: uuid.New(), TenantID: tenantID, ProjectID: &projectID, OwnerAgentID: &agentA,
		Type: memory.TypeSemantic, ScopeType: identity.ScopeAgentPrivate, Status: memory.StatusActive,
		TopicKey: "db.port", Summary: "Postgres port is 5432", Content: "Postgres port is 5432",
		EpistemicStatus: memory.EpistemicReported, AlphaScore: 2, BetaScore: 1, Confidence: 0.66,
		PrimarySourceID: sourceID, Clocks: memory.TriTemporalWindow{ObservedAt: now, ValidFrom: now, CreatedAt: now}, UpdatedAt: now,
	}
	insertTestMemoryWithVec(t, ctx, store, target, baseVec)

	// 1. Same partition, different topic_key, high similarity -> MUST MATCH.
	samePartitionNeighbor := memory.Memory{
		ID: uuid.New(), TenantID: tenantID, ProjectID: &projectID, OwnerAgentID: &agentA,
		Type: memory.TypeSemantic, ScopeType: identity.ScopeAgentPrivate, Status: memory.StatusActive,
		TopicKey: "db.port.override", Summary: "Postgres port is 5433", Content: "Postgres port is 5433",
		EpistemicStatus: memory.EpistemicVerified, AlphaScore: 3, BetaScore: 1, Confidence: 0.75,
		PrimarySourceID: sourceID, Clocks: memory.TriTemporalWindow{ObservedAt: now, ValidFrom: now, CreatedAt: now}, UpdatedAt: now,
	}
	insertTestMemoryWithVec(t, ctx, store, samePartitionNeighbor, nearVec)

	// 2. Same partition, SAME topic_key -> MUST BE EXCLUDED.
	sameTopic := target
	sameTopic.ID = uuid.New()
	sameTopic.Status = memory.StatusConflicted
	insertTestMemoryWithVec(t, ctx, store, sameTopic, baseVec)

	// 3. Different agent in agent_private -> MUST BE EXCLUDED (Zero-Trust).
	otherAgent := samePartitionNeighbor
	otherAgent.ID = uuid.New()
	otherAgent.OwnerAgentID = &agentB
	insertTestMemoryWithVec(t, ctx, store, otherAgent, baseVec)

	// 4. Different scope (project_shared) -> MUST BE EXCLUDED (Zero-Trust).
	projShared := samePartitionNeighbor
	projShared.ID = uuid.New()
	projShared.ScopeType = identity.ScopeProjectShared
	projShared.OwnerAgentID = nil
	insertTestMemoryWithVec(t, ctx, store, projShared, baseVec)

	// 5. Orthogonal vector -> MUST BE EXCLUDED.
	farNeighbor := samePartitionNeighbor
	farNeighbor.ID = uuid.New()
	farNeighbor.TopicKey = "unrelated.topic"
	insertTestMemoryWithVec(t, ctx, store, farNeighbor, farVec)

	// 6. Superseded memory -> MUST BE EXCLUDED.
	superseded := samePartitionNeighbor
	superseded.ID = uuid.New()
	superseded.TopicKey = "old.port"
	superseded.Status = memory.StatusSuperseded
	insertTestMemoryWithVec(t, ctx, store, superseded, baseVec)

	got, err := store.SemanticNeighbors().FindSameScopeSemanticNeighbors(ctx, target, baseVec, 0.82, 5, now)
	if err != nil {
		t.Fatalf("FindSameScopeSemanticNeighbors failed: %v", err)
	}
	if len(got) != 1 || got[0].ID != samePartitionNeighbor.ID {
		t.Fatalf("expected [%s], got %+v", samePartitionNeighbor.ID, got)
	}
}

func TestConflictsLifecycleAndOrphanCleanup(t *testing.T) {
	pool := openTestPool(t)
	store, err := postgres.NewStore(pool)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)

	tenantID, projectID, _, sourceID := seedTestTenantProjectSource(t, ctx, pool, store, now)

	m1 := memory.Memory{
		ID: uuid.New(), TenantID: tenantID, ProjectID: &projectID,
		Type: memory.TypeSemantic, ScopeType: identity.ScopeProjectShared, Status: memory.StatusActive,
		TopicKey: "k1", Summary: "s1", Content: "c1",
		EpistemicStatus: memory.EpistemicVerified, AlphaScore: 4, BetaScore: 1, Confidence: 0.80,
		PrimarySourceID: sourceID, Clocks: memory.TriTemporalWindow{ObservedAt: now, ValidFrom: now, CreatedAt: now}, UpdatedAt: now,
	}
	m2 := m1
	m2.ID, m2.TopicKey, m2.EpistemicStatus, m2.Status = uuid.New(), "k2", memory.EpistemicReported, memory.StatusConflicted
	m3 := m1
	m3.ID, m3.TopicKey, m3.EpistemicStatus, m3.Status = uuid.New(), "k3", memory.EpistemicReported, memory.StatusConflicted

	insertTestMemoryWithVec(t, ctx, store, m1, nil)
	insertTestMemoryWithVec(t, ctx, store, m2, nil)
	insertTestMemoryWithVec(t, ctx, store, m3, nil)

	ent := float32(0.05)
	con := float32(0.91)
	rel1 := memory.MemoryRelation{
		ID: uuid.New(), TenantID: tenantID, ProjectID: &projectID,
		FromMemoryID: m1.ID, ToMemoryID: m2.ID, RelationType: memory.RelationContradicts,
		Weight: 0.91, NLIEntailment: &ent, NLIContradiction: &con,
		SourceID: &sourceID, ValidFrom: now, IsActive: true, CreatedAt: now,
	}
	rel2 := rel1
	rel2.ID, rel2.ToMemoryID = uuid.New(), m3.ID

	if err := store.ExecTx(ctx, func(reg memory.RepositoryRegistry) error {
		if err := reg.Memories().InsertMemoryRelation(ctx, rel1); err != nil {
			return err
		}
		return reg.Memories().InsertMemoryRelation(ctx, rel2)
	}); err != nil {
		t.Fatalf("InsertMemoryRelation failed: %v", err)
	}

	list, err := store.Conflicts().ListActiveConflicts(ctx, tenantID, &projectID, 10, 0, now)
	if err != nil {
		t.Fatalf("ListActiveConflicts failed: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 active conflicts, got %d", len(list))
	}

	// Lock and deactivate rel1 inside tx.
	if err := store.ExecTx(ctx, func(reg memory.RepositoryRegistry) error {
		txProvider := reg.(interface {
			Conflicts() memory.ConflictRepository
		})
		pair, err := txProvider.Conflicts().GetConflictForUpdate(ctx, tenantID, rel1.ID)
		if err != nil {
			return err
		}
		if pair.FromMemory.ID != m1.ID || pair.ToMemory.ID != m2.ID {
			t.Fatalf("unexpected pair endpoints: %+v", pair)
		}
		return txProvider.Conflicts().DeactivateConflictEdge(ctx, tenantID, rel1.ID, now)
	}); err != nil {
		t.Fatalf("ExecTx GetConflictForUpdate/DeactivateConflictEdge failed: %v", err)
	}

	// Deactivating rel1 again must return ErrNotFound.
	if err := store.Conflicts().DeactivateConflictEdge(ctx, tenantID, rel1.ID, now); !errors.Is(err, memory.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	// Orphan cleanup by m1.ID closes rel2 and returns m3.ID as peer.
	peers, err := store.SemanticNeighbors().DeactivateConflictsByMemoryID(ctx, tenantID, m1.ID, now)
	if err != nil {
		t.Fatalf("DeactivateConflictsByMemoryID failed: %v", err)
	}
	if len(peers) != 1 || peers[0] != m3.ID {
		t.Fatalf("expected [%s], got %+v", m3.ID, peers)
	}
	has, err := store.SemanticNeighbors().HasActiveConflicts(ctx, tenantID, m3.ID, now)
	if err != nil || has {
		t.Fatalf("expected HasActiveConflicts=false, got %v (err=%v)", has, err)
	}
}

func TestConsolidationCandidatesAndVerified(t *testing.T) {
	pool := openTestPool(t)
	store, err := postgres.NewStore(pool)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)

	tenantID, projectID, _, sourceID := seedTestTenantProjectSource(t, ctx, pool, store, now)
	vec := testUnitVec(1024, 3, 0)

	// High-confidence active episodic memory -> INCLUDED.
	epHigh := memory.Memory{
		ID: uuid.New(), TenantID: tenantID, ProjectID: &projectID,
		Type: memory.TypeEpisodic, ScopeType: identity.ScopeProjectShared, Status: memory.StatusActive,
		Summary: "episode high", Content: "episode high",
		EpistemicStatus: memory.EpistemicObserved, AlphaScore: 9, BetaScore: 1, Confidence: 0.90,
		PrimarySourceID: sourceID, Clocks: memory.TriTemporalWindow{ObservedAt: now, ValidFrom: now, CreatedAt: now}, UpdatedAt: now,
	}
	insertTestMemoryWithVec(t, ctx, store, epHigh, vec)

	// Low-confidence episodic memory -> EXCLUDED.
	epLow := epHigh
	epLow.ID, epLow.AlphaScore, epLow.BetaScore, epLow.Confidence = uuid.New(), 1, 9, 0.10
	insertTestMemoryWithVec(t, ctx, store, epLow, vec)

	// Verified semantic memory -> EXCLUDED from candidates, INCLUDED in verified list.
	verifiedMem := epHigh
	verifiedMem.ID = uuid.New()
	verifiedMem.Type = memory.TypeSemantic
	verifiedMem.EpistemicStatus = memory.EpistemicVerified
	insertTestMemoryWithVec(t, ctx, store, verifiedMem, vec)

	cands, vecs, err := store.Consolidation().ListREMConsolidationCandidates(ctx, tenantID, &projectID, 0.70)
	if err != nil {
		t.Fatalf("ListREMConsolidationCandidates failed: %v", err)
	}
	if len(cands) != 1 || cands[0].ID != epHigh.ID || len(vecs[epHigh.ID]) != 1024 {
		t.Fatalf("unexpected candidates=%+v vecLen=%d", cands, len(vecs[epHigh.ID]))
	}

	verified, err := store.Consolidation().ListActiveVerifiedByProject(ctx, tenantID, &projectID, now)
	if err != nil {
		t.Fatalf("ListActiveVerifiedByProject failed: %v", err)
	}
	if len(verified) != 1 || verified[0].ID != verifiedMem.ID {
		t.Fatalf("unexpected verified=%+v", verified)
	}
}
