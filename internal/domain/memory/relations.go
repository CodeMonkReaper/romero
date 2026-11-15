package memory

import (
	"fmt"
	"math"
	"strings"

	"github.com/google/uuid"
)

// ExplicitRelationInput describes an optional causal/semantic link requested at capture time.
type ExplicitRelationInput struct {
	ToMemoryID   uuid.UUID
	RelationType RelationType
	Weight       float32
	Notes        string
}

type relationPairKey struct {
	toID uuid.UUID
	typ  RelationType
}

// IsAllowedExplicitRelationType reports whether relType can be declared at CaptureMemory time (AC-11).
func IsAllowedExplicitRelationType(relType RelationType) bool {
	switch relType {
	case RelationDependsOn, RelationCausedBy, RelationSupports, RelationDerivedFrom, RelationRelatedTo:
		return true
	default:
		return false
	}
}

// ValidateExplicitRelations validates explicit relations allowed at capture time (AC-11).
func ValidateExplicitRelations(rels []ExplicitRelationInput) ([]ExplicitRelationInput, error) {
	if len(rels) == 0 {
		return nil, nil
	}
	seen := make(map[relationPairKey]struct{}, len(rels))
	out := make([]ExplicitRelationInput, 0, len(rels))
	for i, r := range rels {
		if r.ToMemoryID == uuid.Nil {
			return nil, fmt.Errorf("%w: relations[%d].to_memory_id is required", ErrValidation, i)
		}
		if !IsAllowedExplicitRelationType(r.RelationType) {
			return nil, fmt.Errorf("%w: relations[%d].relation_type %q is not allowed at capture", ErrValidation, i, r.RelationType)
		}
		w64 := float64(r.Weight)
		if math.IsNaN(w64) || math.IsInf(w64, 0) {
			return nil, fmt.Errorf("%w: relations[%d].weight must be finite", ErrValidation, i)
		}
		if r.Weight == 0 {
			r.Weight = 1.0
		} else if r.Weight < 0 || r.Weight > 1.0 {
			return nil, fmt.Errorf("%w: relations[%d].weight must be in (0.0, 1.0]", ErrValidation, i)
		}
		key := relationPairKey{toID: r.ToMemoryID, typ: r.RelationType}
		if _, exists := seen[key]; exists {
			return nil, fmt.Errorf("%w: duplicate relation (%s, %s)", ErrValidation, r.ToMemoryID, r.RelationType)
		}
		seen[key] = struct{}{}
		r.Notes = strings.TrimSpace(r.Notes)
		out = append(out, r)
	}
	return out, nil
}
