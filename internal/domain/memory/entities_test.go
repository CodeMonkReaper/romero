package memory_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/romero-project/romero/internal/domain/identity"
	"github.com/romero-project/romero/internal/domain/memory"
)

func TestMemoryAndEntitiesValidate(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	tenantID := uuid.New()
	projID := uuid.New()
	agentID := uuid.New()
	memID := uuid.New()
	otherMemID := uuid.New()
	srcID := uuid.New()
	validHash := strings.Repeat("b", 64)

	validMemory := func() memory.Memory {
		return memory.Memory{
			ID:              memID,
			TenantID:        tenantID,
			ProjectID:       &projID,
			Type:            memory.TypeSemantic,
			ScopeType:       identity.ScopeProjectShared,
			Status:          memory.StatusActive,
			TopicKey:        "db/pool",
			Summary:         "pool max conns",
			Content:         "use 20 max connections",
			ContentSHA256:   validHash,
			TokenCount:      6,
			EpistemicStatus: memory.EpistemicObserved,
			AlphaScore:      8.5,
			BetaScore:       1.5,
			Confidence:      0.85,
			Importance:      0.5,
			UtilityScore:    0.0,
			Version:         1,
			PrimarySourceID: srcID,
			Clocks: memory.TriTemporalWindow{
				ObservedAt: now,
				CreatedAt:  now,
				ValidFrom:  now,
			},
			UpdatedAt: now,
		}
	}

	t.Run("valid memory and scope targets", func(t *testing.T) {
		t.Parallel()
		m := validMemory()
		if err := m.Validate(); err != nil {
			t.Fatalf("Validate() unexpected error = %v", err)
		}
		st := m.ScopeTarget()
		if st.TenantID != m.TenantID || st.ScopeType != m.ScopeType {
			t.Errorf("ScopeTarget() = %+v, mismatch", st)
		}

		// agent_private valid with agentID
		mAgent := validMemory()
		mAgent.ScopeType = identity.ScopeAgentPrivate
		mAgent.OwnerAgentID = &agentID
		if err := mAgent.Validate(); err != nil {
			t.Errorf("agent_private Validate() unexpected error = %v", err)
		}

		// task_shared valid with taskID
		mTask := validMemory()
		mTask.ScopeType = identity.ScopeTaskShared
		mTask.TaskID = "task-42"
		if err := mTask.Validate(); err != nil {
			t.Errorf("task_shared Validate() unexpected error = %v", err)
		}

		// org_shared and global_public valid without project
		for _, sc := range []identity.ScopeType{identity.ScopeOrgShared, identity.ScopeGlobalPublic} {
			mTenant := validMemory()
			mTenant.ScopeType = sc
			mTenant.ProjectID = nil
			if err := mTenant.Validate(); err != nil {
				t.Errorf("%s Validate() unexpected error = %v", sc, err)
			}
		}
	})

	memoryFailures := []struct {
		name    string
		mutate  func(*memory.Memory)
		wantErr error
	}{
		{
			name:    "missing primary_source_id",
			mutate:  func(m *memory.Memory) { m.PrimarySourceID = uuid.Nil },
			wantErr: memory.ErrMissingProvenance,
		},
		{
			name:    "missing id",
			mutate:  func(m *memory.Memory) { m.ID = uuid.Nil },
			wantErr: memory.ErrInvalidIdentifier,
		},
		{
			name:    "invalid memory_type",
			mutate:  func(m *memory.Memory) { m.Type = "unknown" },
			wantErr: memory.ErrInvalidMemoryType,
		},
		{
			name:    "agent_private without owner_agent_id",
			mutate:  func(m *memory.Memory) { m.ScopeType = identity.ScopeAgentPrivate; m.OwnerAgentID = nil },
			wantErr: memory.ErrInvalidIdentifier,
		},
		{
			name:    "task_shared without task_id",
			mutate:  func(m *memory.Memory) { m.ScopeType = identity.ScopeTaskShared; m.TaskID = " " },
			wantErr: identity.ErrMissingTaskID,
		},
		{
			name:    "project_shared without project_id",
			mutate:  func(m *memory.Memory) { m.ScopeType = identity.ScopeProjectShared; m.ProjectID = nil },
			wantErr: memory.ErrInvalidIdentifier,
		},
		{
			name:    "invalid content_sha256",
			mutate:  func(m *memory.Memory) { m.ContentSHA256 = "short" },
			wantErr: memory.ErrInvalidHash,
		},
		{
			name:    "importance out of range",
			mutate:  func(m *memory.Memory) { m.Importance = 1.5 },
			wantErr: memory.ErrInvalidBayesianParams,
		},
		{
			name:    "missing updated_at",
			mutate:  func(m *memory.Memory) { m.UpdatedAt = time.Time{} },
			wantErr: memory.ErrInvalidTimestamp,
		},
	}

	for _, tc := range memoryFailures {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := validMemory()
			tc.mutate(&m)
			if err := m.Validate(); !errors.Is(err, tc.wantErr) {
				t.Errorf("Validate() error = %v, want %v", err, tc.wantErr)
			}
		})
	}

	t.Run("mutable vs closed statuses", func(t *testing.T) {
		t.Parallel()
		for _, st := range []memory.MemoryStatus{memory.StatusActive, memory.StatusConflicted} {
			m := validMemory()
			m.Status = st
			if err := m.ValidateMutable(); err != nil {
				t.Errorf("status %s ValidateMutable() unexpected error = %v", st, err)
			}
		}
		for _, st := range []memory.MemoryStatus{memory.StatusSuperseded, memory.StatusArchived, memory.StatusRejected} {
			m := validMemory()
			m.Status = st
			if err := m.ValidateMutable(); !errors.Is(err, memory.ErrClosedMemoryConflict) {
				t.Errorf("status %s ValidateMutable() = %v, want ErrClosedMemoryConflict", st, err)
			}
		}
	})

	t.Run("version validation", func(t *testing.T) {
		t.Parallel()
		v := memory.MemoryVersion{
			ID:              uuid.New(),
			MemoryID:        memID,
			Version:         1,
			Summary:         "v1",
			Content:         "body",
			EpistemicStatus: memory.EpistemicObserved,
			AlphaScore:      8.5,
			BetaScore:       1.5,
			Importance:      0.5,
			UtilityScore:    0.0,
			Status:          memory.StatusActive,
			ValidFrom:       now,
			CreatedAt:       now,
		}
		if err := v.Validate(); err != nil {
			t.Fatalf("version Validate() unexpected error = %v", err)
		}
		v.Version = 0
		if err := v.Validate(); !errors.Is(err, memory.ErrInvalidBounds) {
			t.Errorf("version 0 Validate() = %v, want ErrInvalidBounds", err)
		}
	})

	t.Run("relation validation", func(t *testing.T) {
		t.Parallel()
		rel := memory.MemoryRelation{
			ID:           uuid.New(),
			TenantID:     tenantID,
			FromMemoryID: memID,
			ToMemoryID:   otherMemID,
			RelationType: memory.RelationSupersedes,
			Weight:       1.0,
			ValidFrom:    now,
			IsActive:     true,
			CreatedAt:    now,
		}
		if err := rel.Validate(); err != nil {
			t.Fatalf("rel.Validate() unexpected error = %v", err)
		}

		badLoop := rel
		badLoop.ToMemoryID = memID
		if err := badLoop.Validate(); !errors.Is(err, memory.ErrSelfLoopRelation) {
			t.Errorf("self-loop error = %v, want ErrSelfLoopRelation", err)
		}

		badRelType := rel
		badRelType.RelationType = "copies"
		if err := badRelType.Validate(); !errors.Is(err, memory.ErrInvalidRelationType) {
			t.Errorf("bad relation_type error = %v, want ErrInvalidRelationType", err)
		}
	})

	t.Run("feedback job and audit validation", func(t *testing.T) {
		t.Parallel()
		fb := memory.MemoryFeedback{
			ID:         uuid.New(),
			TenantID:   tenantID,
			MemoryID:   memID,
			Signal:     memory.SignalHelpfulSuccess,
			AlphaDelta: 1.0,
			CreatedAt:  now,
		}
		if err := fb.Validate(); err != nil {
			t.Errorf("feedback Validate() unexpected error = %v", err)
		}

		job := memory.MemoryJob{
			ID:          uuid.New(),
			TenantID:    tenantID,
			JobType:     "embed_and_nli",
			Priority:    100,
			Status:      memory.JobPending,
			MaxAttempts: 5,
			ScheduledAt: now,
			CreatedAt:   now,
		}
		if err := job.Validate(); err != nil {
			t.Errorf("job Validate() unexpected error = %v", err)
		}

		// TenantID nil allowed for 401 unauthenticated denied events
		ev := memory.AuditEvent{
			ID:           uuid.New(),
			Operation:    "authenticate",
			ResourceType: "principal",
			Outcome:      memory.AuditDenied,
			CreatedAt:    now,
		}
		if err := ev.Validate(); err != nil {
			t.Errorf("401 audit Validate() unexpected error = %v", err)
		}
		ev.Outcome = "unknown"
		if err := ev.Validate(); !errors.Is(err, memory.ErrInvalidAuditOutcome) {
			t.Errorf("bad audit outcome = %v, want ErrInvalidAuditOutcome", err)
		}
	})
}
