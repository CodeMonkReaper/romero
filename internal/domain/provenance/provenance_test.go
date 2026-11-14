package provenance_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/romero-project/romero/internal/domain/provenance"
)

func TestSourceTypeValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		st   provenance.SourceType
		want bool
	}{
		{name: "agent_execution", st: provenance.SourceAgentExecution, want: true},
		{name: "user_input", st: provenance.SourceUserInput, want: true},
		{name: "tool_output", st: provenance.SourceToolOutput, want: true},
		{name: "document_ingest", st: provenance.SourceDocumentIngest, want: true},
		{name: "rem_consolidation", st: provenance.SourceREMConsolidation, want: true},
		{name: "human_review", st: provenance.SourceHumanReview, want: true},
		{name: "empty", st: provenance.SourceType(""), want: false},
		{name: "unknown", st: provenance.SourceType("external_webhook"), want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.st.Valid(); got != tc.want {
				t.Errorf("SourceType(%q).Valid() = %v, want %v", tc.st, got, tc.want)
			}
		})
	}
}

func TestMemorySourceValidate(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	tenantID := uuid.New()
	validHash := strings.Repeat("a", 64)

	validSource := func() provenance.MemorySource {
		return provenance.MemorySource{
			ID:               uuid.New(),
			TenantID:         tenantID,
			SourceType:       provenance.SourceAgentExecution,
			ExternalSourceID: "run-001/step-4",
			RawExcerptHash:   validHash,
			CreatedAt:        now,
		}
	}

	tests := []struct {
		name    string
		mutate  func(*provenance.MemorySource)
		wantErr error
	}{
		{
			name:    "valid complete source",
			mutate:  func(*provenance.MemorySource) {},
			wantErr: nil,
		},
		{
			name: "valid source without optional hash",
			mutate: func(s *provenance.MemorySource) {
				s.RawExcerptHash = ""
			},
			wantErr: nil,
		},
		{
			name: "missing tenant_id",
			mutate: func(s *provenance.MemorySource) {
				s.TenantID = uuid.Nil
			},
			wantErr: provenance.ErrMissingTenant,
		},
		{
			name: "invalid source_type",
			mutate: func(s *provenance.MemorySource) {
				s.SourceType = "unknown"
			},
			wantErr: provenance.ErrInvalidSourceType,
		},
		{
			name: "blank external_source_id",
			mutate: func(s *provenance.MemorySource) {
				s.ExternalSourceID = "   \t  "
			},
			wantErr: provenance.ErrMissingExternalSourceID,
		},
		{
			name: "short raw_excerpt_hash",
			mutate: func(s *provenance.MemorySource) {
				s.RawExcerptHash = "deadbeef"
			},
			wantErr: provenance.ErrInvalidExcerptHash,
		},
		{
			name: "uppercase non-canonical raw_excerpt_hash",
			mutate: func(s *provenance.MemorySource) {
				s.RawExcerptHash = strings.Repeat("A", 64)
			},
			wantErr: provenance.ErrInvalidExcerptHash,
		},
		{
			name: "non-hex raw_excerpt_hash",
			mutate: func(s *provenance.MemorySource) {
				s.RawExcerptHash = strings.Repeat("z", 64)
			},
			wantErr: provenance.ErrInvalidExcerptHash,
		},
		{
			name: "missing created_at",
			mutate: func(s *provenance.MemorySource) {
				s.CreatedAt = time.Time{}
			},
			wantErr: provenance.ErrMissingCreatedAt,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			src := validSource()
			tc.mutate(&src)
			err := src.Validate()
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("Validate() unexpected error = %v", err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("Validate() error = %v, want errors.Is(_, %v)", err, tc.wantErr)
			}
		})
	}
}

func TestMemorySourceRef(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		src  provenance.MemorySource
		want string
	}{
		{
			name: "canonical type and external id",
			src: provenance.MemorySource{
				SourceType:       provenance.SourceToolOutput,
				ExternalSourceID: "pytest/run-42",
			},
			want: "tool_output:pytest/run-42",
		},
		{
			name: "blank external id falls back to source type",
			src: provenance.MemorySource{
				SourceType:       provenance.SourceHumanReview,
				ExternalSourceID: "   ",
			},
			want: "human_review",
		},
		{
			name: "invalid source type falls back to external id",
			src: provenance.MemorySource{
				SourceType:       provenance.SourceType("custom"),
				ExternalSourceID: "ticket-99",
			},
			want: "ticket-99",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.src.SourceRef(); got != tc.want {
				t.Errorf("SourceRef() = %q, want %q", got, tc.want)
			}
		})
	}
}
