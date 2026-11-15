package memory_test

import (
	"errors"
	"math"
	"testing"

	"github.com/google/uuid"
	"github.com/romero-project/romero/internal/domain/memory"
)

func TestValidateExplicitRelations(t *testing.T) {
	t.Parallel()

	targetA := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	targetB := uuid.MustParse("00000000-0000-0000-0000-000000000002")

	tests := []struct {
		name      string
		in        []memory.ExplicitRelationInput
		wantErr   bool
		wantCount int
		wantW0    float32
	}{
		{
			name:      "empty slice allowed",
			in:        nil,
			wantCount: 0,
		},
		{
			name: "valid relation defaults zero weight to 1.0",
			in: []memory.ExplicitRelationInput{
				{ToMemoryID: targetA, RelationType: memory.RelationDependsOn, Notes: "required"},
			},
			wantCount: 1,
			wantW0:    1.0,
		},
		{
			name: "all five allowed relation types",
			in: []memory.ExplicitRelationInput{
				{ToMemoryID: targetA, RelationType: memory.RelationDependsOn, Weight: 0.9},
				{ToMemoryID: targetA, RelationType: memory.RelationCausedBy, Weight: 0.8},
				{ToMemoryID: targetB, RelationType: memory.RelationSupports, Weight: 0.7},
				{ToMemoryID: targetB, RelationType: memory.RelationDerivedFrom, Weight: 0.6},
				{ToMemoryID: targetB, RelationType: memory.RelationRelatedTo, Weight: 0.5},
			},
			wantCount: 5,
			wantW0:    0.9,
		},
		{
			name: "rejects nil target id",
			in: []memory.ExplicitRelationInput{
				{ToMemoryID: uuid.Nil, RelationType: memory.RelationDependsOn, Weight: 0.5},
			},
			wantErr: true,
		},
		{
			name: "rejects reserved supersedes",
			in: []memory.ExplicitRelationInput{
				{ToMemoryID: targetA, RelationType: memory.RelationSupersedes, Weight: 1.0},
			},
			wantErr: true,
		},
		{
			name: "rejects reserved contradicts",
			in: []memory.ExplicitRelationInput{
				{ToMemoryID: targetA, RelationType: memory.RelationContradicts, Weight: 1.0},
			},
			wantErr: true,
		},
		{
			name: "rejects unknown relation type",
			in: []memory.ExplicitRelationInput{
				{ToMemoryID: targetA, RelationType: memory.RelationType("blocks"), Weight: 0.5},
			},
			wantErr: true,
		},
		{
			name: "rejects negative weight",
			in: []memory.ExplicitRelationInput{
				{ToMemoryID: targetA, RelationType: memory.RelationDependsOn, Weight: -0.1},
			},
			wantErr: true,
		},
		{
			name: "rejects weight above 1.0",
			in: []memory.ExplicitRelationInput{
				{ToMemoryID: targetA, RelationType: memory.RelationDependsOn, Weight: 1.01},
			},
			wantErr: true,
		},
		{
			name: "rejects NaN weight",
			in: []memory.ExplicitRelationInput{
				{ToMemoryID: targetA, RelationType: memory.RelationDependsOn, Weight: float32(math.NaN())},
			},
			wantErr: true,
		},
		{
			name: "rejects duplicate (to_memory_id, relation_type)",
			in: []memory.ExplicitRelationInput{
				{ToMemoryID: targetA, RelationType: memory.RelationSupports, Weight: 0.5},
				{ToMemoryID: targetA, RelationType: memory.RelationSupports, Weight: 0.8},
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := memory.ValidateExplicitRelations(tc.in)
			if tc.wantErr {
				if !errors.Is(err, memory.ErrValidation) {
					t.Fatalf("expected ErrValidation, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != tc.wantCount {
				t.Fatalf("got %d relations, want %d", len(got), tc.wantCount)
			}
			if tc.wantCount > 0 && got[0].Weight != tc.wantW0 {
				t.Fatalf("got[0].Weight = %v, want %v", got[0].Weight, tc.wantW0)
			}
		})
	}
}
