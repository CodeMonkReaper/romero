package memory

import (
	"math"
	"strings"

	"github.com/romero-project/romero/internal/domain/identity"
)

// QuarantineThreshold defines the expected confidence below which an active memory
// transitions synchronously to StatusConflicted.
const QuarantineThreshold float32 = 0.30

// FeedbackDeltaResult summarizes synchronous Beta(alpha, beta) calibration and state changes.
type FeedbackDeltaResult struct {
	NewAlpha              float32
	NewBeta               float32
	AlphaDelta            float32
	BetaDelta             float32
	NewConfidence         float32
	NewUtility            float32
	UtilityDelta          float32
	NewEpistemic          EpistemicStatus
	EpistemicChanged      bool
	NewStatus             MemoryStatus
	Quarantined           bool
	ReleaseActiveTopicKey bool
	AccessIncrement       int
	TouchLastAccessed     bool
}

// SupersessionOutcome identifies whether an incoming topic_key memory supersedes
// the current active memory or enters quarantine as conflicted.
type SupersessionOutcome string

const (
	OutcomeSupersedeOld       SupersessionOutcome = "supersede_old"
	OutcomeKeepOldConflictNew SupersessionOutcome = "keep_old_conflict_new"
)

// SupersessionDecision holds the state transition decided for a topic_key collision.
type SupersessionDecision struct {
	Outcome            SupersessionOutcome
	OldNewStatus       MemoryStatus
	NewMemoryStatus    MemoryStatus
	Relation           RelationType
	CloseOldValidUntil bool
}

// InitialPriors returns canonical Beta(alpha_0, beta_0) priors and expected confidence.
func InitialPriors(status EpistemicStatus) (alpha, beta, confidence float32) {
	switch status {
	case EpistemicVerified:
		return 19.0, 1.0, 0.95
	case EpistemicObserved:
		return 8.5, 1.5, 0.85
	case EpistemicReported:
		return 5.25, 2.25, 0.70
	case EpistemicInferred:
		return 3.0, 2.0, 0.60
	case EpistemicHypothesis:
		return 1.4, 2.6, 0.35
	default:
		return 0, 0, 0
	}
}

// ExpectedConfidence computes alpha / (alpha + beta) for valid finite positive parameters.
func ExpectedConfidence(alpha, beta float32) (float32, error) {
	if !isFinitePositive32(alpha) || !isFinitePositive32(beta) {
		return 0, ErrInvalidBayesianParams
	}
	return float32(float64(alpha) / (float64(alpha) + float64(beta))), nil
}

// CanApplyFeedbackSignal enforces principal authority for feedback signals.
func CanApplyFeedbackSignal(callerType identity.PrincipalType, signal FeedbackSignal) error {
	if !validPrincipalType(callerType) {
		return ErrForbidden
	}
	if !signal.Valid() {
		return ErrInvalidFeedbackSignal
	}
	if signal == SignalConfirmedVerified && callerType == identity.PrincipalAgent {
		return ErrForbidden
	}
	return nil
}

// ApplyFeedback computes synchronous Beta(alpha, beta) recalibration, utility update,
// and quarantine transition when confidence drops below QuarantineThreshold.
func ApplyFeedback(
	currentAlpha float32,
	currentBeta float32,
	currentUtility float32,
	currentEpistemic EpistemicStatus,
	currentStatus MemoryStatus,
	signal FeedbackSignal,
) (FeedbackDeltaResult, error) {
	if !currentStatus.Valid() {
		return FeedbackDeltaResult{}, ErrInvalidMemoryStatus
	}
	if currentStatus.IsClosed() {
		return FeedbackDeltaResult{}, ErrClosedMemoryConflict
	}
	if !currentEpistemic.Valid() {
		return FeedbackDeltaResult{}, ErrInvalidEpistemicStatus
	}
	if !signal.Valid() {
		return FeedbackDeltaResult{}, ErrInvalidFeedbackSignal
	}
	if !isFinitePositive32(currentAlpha) || !isFinitePositive32(currentBeta) || !isFiniteInRange32(currentUtility, -1.0, 1.0) {
		return FeedbackDeltaResult{}, ErrInvalidBayesianParams
	}

	res := FeedbackDeltaResult{
		NewAlpha:     currentAlpha,
		NewBeta:      currentBeta,
		NewUtility:   currentUtility,
		NewEpistemic: currentEpistemic,
		NewStatus:    currentStatus,
	}

	switch signal {
	case SignalRetrieved, SignalUsedInContext:
		res.AccessIncrement = 1
		res.TouchLastAccessed = true
	case SignalHelpfulSuccess:
		res.AlphaDelta = 1.0
		res.NewAlpha = currentAlpha + 1.0
		res.NewUtility = clamp32(float32(math.Min(1.0, 0.85*float64(currentUtility)+0.15)), -1.0, 1.0)
	case SignalConfirmedVerified:
		res.AlphaDelta = 3.0
		res.NewAlpha = currentAlpha + 3.0
		res.NewEpistemic = EpistemicVerified
		res.EpistemicChanged = currentEpistemic != EpistemicVerified
	case SignalIgnored:
		res.BetaDelta = 0.15
		res.NewBeta = currentBeta + 0.15
	case SignalMisleadingFailure:
		res.BetaDelta = 2.5
		res.NewBeta = currentBeta + 2.5
		res.NewUtility = clamp32(float32(math.Max(-1.0, 0.75*float64(currentUtility)-0.25)), -1.0, 1.0)
	case SignalCorrectedByUser:
		res.BetaDelta = 4.0
		res.NewBeta = currentBeta + 4.0
	}

	res.UtilityDelta = res.NewUtility - currentUtility

	conf, err := ExpectedConfidence(res.NewAlpha, res.NewBeta)
	if err != nil {
		return FeedbackDeltaResult{}, err
	}
	res.NewConfidence = conf

	if currentStatus == StatusActive && conf < QuarantineThreshold {
		res.NewStatus = StatusConflicted
		res.Quarantined = true
		res.ReleaseActiveTopicKey = true
	}

	return res, nil
}

// DecideTopicSupersession resolves automatic or explicit topic_key supersession
// honoring the epistemic hierarchy and verified->observed override rules.
func DecideTopicSupersession(
	oldStatus EpistemicStatus,
	newStatus EpistemicStatus,
	explicit bool,
	callerType identity.PrincipalType,
	reason string,
) (SupersessionDecision, error) {
	if !oldStatus.Valid() || !newStatus.Valid() {
		return SupersessionDecision{}, ErrInvalidEpistemicStatus
	}
	if !validPrincipalType(callerType) {
		return SupersessionDecision{}, ErrForbidden
	}
	if callerType == identity.PrincipalAgent && newStatus == EpistemicVerified {
		return SupersessionDecision{}, ErrForbidden
	}

	if newStatus.Rank() >= oldStatus.Rank() {
		return SupersessionDecision{
			Outcome:            OutcomeSupersedeOld,
			OldNewStatus:       StatusSuperseded,
			NewMemoryStatus:    StatusActive,
			Relation:           RelationSupersedes,
			CloseOldValidUntil: true,
		}, nil
	}

	if !explicit {
		return SupersessionDecision{
			Outcome:            OutcomeKeepOldConflictNew,
			OldNewStatus:       StatusActive,
			NewMemoryStatus:    StatusConflicted,
			Relation:           RelationContradicts,
			CloseOldValidUntil: false,
		}, nil
	}

	if oldStatus == EpistemicVerified && newStatus == EpistemicObserved {
		if callerType != identity.PrincipalUser && callerType != identity.PrincipalOrchestrator {
			return SupersessionDecision{}, ErrForbidden
		}
		if strings.TrimSpace(reason) == "" {
			return SupersessionDecision{}, ErrMissingChangeReason
		}
		return SupersessionDecision{
			Outcome:            OutcomeSupersedeOld,
			OldNewStatus:       StatusSuperseded,
			NewMemoryStatus:    StatusActive,
			Relation:           RelationSupersedes,
			CloseOldValidUntil: true,
		}, nil
	}

	return SupersessionDecision{}, ErrEpistemicDowngradeNotAllowed
}

func validPrincipalType(p identity.PrincipalType) bool {
	switch p {
	case identity.PrincipalUser, identity.PrincipalService, identity.PrincipalOrchestrator, identity.PrincipalAgent:
		return true
	default:
		return false
	}
}

func isFinitePositive32(v float32) bool {
	f := float64(v)
	return !math.IsNaN(f) && !math.IsInf(f, 0) && v > 0
}

func isFiniteInRange32(v, min, max float32) bool {
	f := float64(v)
	return !math.IsNaN(f) && !math.IsInf(f, 0) && v >= min && v <= max
}

func clamp32(v, min, max float32) float32 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}
