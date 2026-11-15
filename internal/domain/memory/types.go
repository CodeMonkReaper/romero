// Package memory implements pure domain entities, tri-temporal clocks, epistemic
// rank hierarchy, Bayesian Beta(alpha, beta) calibration, and repository ports.
package memory

import "errors"

// MemoryType classifies the cognitive role of a stored memory.
type MemoryType string

const (
	TypeEpisodic   MemoryType = "episodic"
	TypeSemantic   MemoryType = "semantic"
	TypeProcedural MemoryType = "procedural"
	TypeRelational MemoryType = "relational"
)

// MemoryStatus represents the lifecycle state of a memory record.
type MemoryStatus string

const (
	StatusCandidate    MemoryStatus = "candidate"
	StatusActive       MemoryStatus = "active"
	StatusSuperseded   MemoryStatus = "superseded"
	StatusConflicted   MemoryStatus = "conflicted"
	StatusConsolidated MemoryStatus = "consolidated"
	StatusArchived     MemoryStatus = "archived"
	StatusRejected     MemoryStatus = "rejected"
)

// EpistemicStatus orders claims by verification rigor (rank 5 down to 1).
type EpistemicStatus string

const (
	EpistemicVerified   EpistemicStatus = "verified"
	EpistemicObserved   EpistemicStatus = "observed"
	EpistemicReported   EpistemicStatus = "reported"
	EpistemicInferred   EpistemicStatus = "inferred"
	EpistemicHypothesis EpistemicStatus = "hypothesis"
)

// RelationType enumerates directed edge kinds between memories.
type RelationType string

const (
	RelationSupports    RelationType = "supports"
	RelationContradicts RelationType = "contradicts"
	RelationSupersedes  RelationType = "supersedes"
	RelationCausedBy    RelationType = "caused_by"
	RelationDependsOn   RelationType = "depends_on"
	RelationDerivedFrom RelationType = "derived_from"
	RelationRelatedTo   RelationType = "related_to"
)

// FeedbackSignal represents an empirical outcome recorded against a memory.
type FeedbackSignal string

const (
	SignalRetrieved         FeedbackSignal = "retrieved"
	SignalUsedInContext     FeedbackSignal = "used_in_context"
	SignalHelpfulSuccess    FeedbackSignal = "helpful_success"
	SignalIgnored           FeedbackSignal = "ignored"
	SignalMisleadingFailure FeedbackSignal = "misleading_failure"
	SignalCorrectedByUser   FeedbackSignal = "corrected_by_user"
	SignalConfirmedVerified FeedbackSignal = "confirmed_verified"
)

// JobStatus tracks asynchronous enrichment work state in memory_jobs.
type JobStatus string

const (
	JobPending    JobStatus = "pending"
	JobProcessing JobStatus = "processing"
	JobCompleted  JobStatus = "completed"
	JobFailed     JobStatus = "failed"
	JobDeadLetter JobStatus = "dead_letter"
)

// AuditOutcome records whether an audited operation was permitted or rejected.
type AuditOutcome string

const (
	AuditAllowed AuditOutcome = "allowed"
	AuditDenied  AuditOutcome = "denied"
)

var (
	ErrInvalidMemoryType            = errors.New("invalid memory_type")
	ErrInvalidMemoryStatus          = errors.New("invalid memory status")
	ErrInvalidEpistemicStatus       = errors.New("invalid epistemic_status")
	ErrInvalidRelationType          = errors.New("invalid relation_type")
	ErrInvalidFeedbackSignal        = errors.New("invalid feedback_signal")
	ErrInvalidJobStatus             = errors.New("invalid job status")
	ErrInvalidAuditOutcome          = errors.New("invalid audit outcome")
	ErrInvalidBayesianParams        = errors.New("bayesian parameters must be finite positive numbers and utility within [-1, 1]")
	ErrInvalidBounds                = errors.New("numeric score out of valid bounds")
	ErrClosedMemoryConflict         = errors.New("closed memory cannot be mutated in place")
	ErrEpistemicDowngradeNotAllowed = errors.New("lower epistemic rank cannot supersede higher rank without explicit authorized override")
	ErrMissingChangeReason          = errors.New("change_reason is required for explicit epistemic downgrade")
	ErrInvalidTimestamp             = errors.New("required timestamp is missing")
	ErrInvalidTemporalWindow        = errors.New("valid_until must not precede valid_from")
	ErrInvalidPointInTimeQuery      = errors.New("point-in-time query requires either as_of or both valid_at and known_at")
	ErrMissingProvenance            = errors.New("primary_source_id is required")
	ErrInvalidIdentifier            = errors.New("required identifier is missing")
	ErrInvalidHash                  = errors.New("content_sha256 must be 64 lowercase hex characters")
	ErrSelfLoopRelation             = errors.New("from_memory_id and to_memory_id must differ")
	ErrValidation                   = errors.New("validation error")
	ErrForbidden                    = errors.New("forbidden")
	ErrNotFound                     = errors.New("not found")
)

func (m MemoryType) Valid() bool {
	switch m {
	case TypeEpisodic, TypeSemantic, TypeProcedural, TypeRelational:
		return true
	default:
		return false
	}
}

func (s MemoryStatus) Valid() bool {
	switch s {
	case StatusCandidate, StatusActive, StatusSuperseded, StatusConflicted, StatusConsolidated, StatusArchived, StatusRejected:
		return true
	default:
		return false
	}
}

// IsClosed reports whether a memory status prohibits in-place edits or supersession.
func (s MemoryStatus) IsClosed() bool {
	return s == StatusSuperseded || s == StatusArchived || s == StatusRejected
}

func (e EpistemicStatus) Valid() bool {
	return e.Rank() > 0
}

// Rank returns the epistemic rank (verified=5 down to hypothesis=1, 0 if invalid).
func (e EpistemicStatus) Rank() int {
	switch e {
	case EpistemicVerified:
		return 5
	case EpistemicObserved:
		return 4
	case EpistemicReported:
		return 3
	case EpistemicInferred:
		return 2
	case EpistemicHypothesis:
		return 1
	default:
		return 0
	}
}

// Rank returns the canonical epistemic hierarchy score (5..1, or 0 if invalid).
func Rank(status EpistemicStatus) int {
	return status.Rank()
}

func (r RelationType) Valid() bool {
	switch r {
	case RelationSupports, RelationContradicts, RelationSupersedes, RelationCausedBy, RelationDependsOn, RelationDerivedFrom, RelationRelatedTo:
		return true
	default:
		return false
	}
}

func (f FeedbackSignal) Valid() bool {
	switch f {
	case SignalRetrieved, SignalUsedInContext, SignalHelpfulSuccess, SignalIgnored, SignalMisleadingFailure, SignalCorrectedByUser, SignalConfirmedVerified:
		return true
	default:
		return false
	}
}

func (j JobStatus) Valid() bool {
	switch j {
	case JobPending, JobProcessing, JobCompleted, JobFailed, JobDeadLetter:
		return true
	default:
		return false
	}
}

func (a AuditOutcome) Valid() bool {
	return a == AuditAllowed || a == AuditDenied
}
