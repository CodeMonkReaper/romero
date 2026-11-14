package memory_test

import (
	"errors"
	"math"
	"testing"

	"github.com/romero-project/romero/internal/domain/identity"
	"github.com/romero-project/romero/internal/domain/memory"
)

func approxEqual32(a, b, eps float32) bool {
	return float32(math.Abs(float64(a-b))) <= eps
}

func TestEpistemicRankAndPriors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		status         memory.EpistemicStatus
		wantValid      bool
		wantRank       int
		wantAlpha      float32
		wantBeta       float32
		wantConfidence float32
	}{
		{name: "verified", status: memory.EpistemicVerified, wantValid: true, wantRank: 5, wantAlpha: 19.0, wantBeta: 1.0, wantConfidence: 0.95},
		{name: "observed", status: memory.EpistemicObserved, wantValid: true, wantRank: 4, wantAlpha: 8.5, wantBeta: 1.5, wantConfidence: 0.85},
		{name: "reported", status: memory.EpistemicReported, wantValid: true, wantRank: 3, wantAlpha: 5.25, wantBeta: 2.25, wantConfidence: 0.70},
		{name: "inferred", status: memory.EpistemicInferred, wantValid: true, wantRank: 2, wantAlpha: 3.0, wantBeta: 2.0, wantConfidence: 0.60},
		{name: "hypothesis", status: memory.EpistemicHypothesis, wantValid: true, wantRank: 1, wantAlpha: 1.4, wantBeta: 2.6, wantConfidence: 0.35},
		{name: "invalid", status: memory.EpistemicStatus("rumor"), wantValid: false, wantRank: 0, wantAlpha: 0, wantBeta: 0, wantConfidence: 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.status.Valid(); got != tc.wantValid {
				t.Errorf("Valid() = %v, want %v", got, tc.wantValid)
			}
			if got := memory.Rank(tc.status); got != tc.wantRank {
				t.Errorf("Rank() = %d, want %d", got, tc.wantRank)
			}
			if got := tc.status.Rank(); got != tc.wantRank {
				t.Errorf("status.Rank() = %d, want %d", got, tc.wantRank)
			}
			a, b, c := memory.InitialPriors(tc.status)
			if !approxEqual32(a, tc.wantAlpha, 1e-6) || !approxEqual32(b, tc.wantBeta, 1e-6) || !approxEqual32(c, tc.wantConfidence, 1e-6) {
				t.Errorf("InitialPriors() = (%v, %v, %v), want (%v, %v, %v)", a, b, c, tc.wantAlpha, tc.wantBeta, tc.wantConfidence)
			}
		})
	}
}

func TestApplyFeedbackSignalsAndQuarantine(t *testing.T) {
	t.Parallel()

	nan32 := float32(math.NaN())
	inf32 := float32(math.Inf(1))

	tests := []struct {
		name             string
		alpha            float32
		beta             float32
		utility          float32
		epistemic        memory.EpistemicStatus
		status           memory.MemoryStatus
		signal           memory.FeedbackSignal
		wantAlpha        float32
		wantBeta         float32
		wantUtility      float32
		wantEpistemic    memory.EpistemicStatus
		wantStatus       memory.MemoryStatus
		wantQuarantined  bool
		wantTopicRelease bool
		wantAccessInc    int
		wantErr          error
	}{
		{
			name:          "retrieved increments access without bayesian delta",
			alpha:         8.5,
			beta:          1.5,
			utility:       0.2,
			epistemic:     memory.EpistemicObserved,
			status:        memory.StatusActive,
			signal:        memory.SignalRetrieved,
			wantAlpha:     8.5,
			wantBeta:      1.5,
			wantUtility:   0.2,
			wantEpistemic: memory.EpistemicObserved,
			wantStatus:    memory.StatusActive,
			wantAccessInc: 1,
		},
		{
			name:          "used_in_context increments access without bayesian delta",
			alpha:         8.5,
			beta:          1.5,
			utility:       0.0,
			epistemic:     memory.EpistemicObserved,
			status:        memory.StatusActive,
			signal:        memory.SignalUsedInContext,
			wantAlpha:     8.5,
			wantBeta:      1.5,
			wantUtility:   0.0,
			wantEpistemic: memory.EpistemicObserved,
			wantStatus:    memory.StatusActive,
			wantAccessInc: 1,
		},
		{
			name:          "helpful_success increases alpha and utility",
			alpha:         8.5,
			beta:          1.5,
			utility:       0.0,
			epistemic:     memory.EpistemicObserved,
			status:        memory.StatusActive,
			signal:        memory.SignalHelpfulSuccess,
			wantAlpha:     9.5,
			wantBeta:      1.5,
			wantUtility:   0.15,
			wantEpistemic: memory.EpistemicObserved,
			wantStatus:    memory.StatusActive,
		},
		{
			name:          "helpful_success clamps utility at 1.0",
			alpha:         8.5,
			beta:          1.5,
			utility:       1.0,
			epistemic:     memory.EpistemicObserved,
			status:        memory.StatusActive,
			signal:        memory.SignalHelpfulSuccess,
			wantAlpha:     9.5,
			wantBeta:      1.5,
			wantUtility:   1.0,
			wantEpistemic: memory.EpistemicObserved,
			wantStatus:    memory.StatusActive,
		},
		{
			name:          "confirmed_verified adds 3 to alpha and promotes to verified",
			alpha:         8.5,
			beta:          1.5,
			utility:       0.25,
			epistemic:     memory.EpistemicObserved,
			status:        memory.StatusActive,
			signal:        memory.SignalConfirmedVerified,
			wantAlpha:     11.5,
			wantBeta:      1.5,
			wantUtility:   0.25,
			wantEpistemic: memory.EpistemicVerified,
			wantStatus:    memory.StatusActive,
		},
		{
			name:          "ignored adds 0.15 to beta",
			alpha:         3.0,
			beta:          2.0,
			utility:       0.1,
			epistemic:     memory.EpistemicInferred,
			status:        memory.StatusActive,
			signal:        memory.SignalIgnored,
			wantAlpha:     3.0,
			wantBeta:      2.15,
			wantUtility:   0.1,
			wantEpistemic: memory.EpistemicInferred,
			wantStatus:    memory.StatusActive,
		},
		{
			name:             "AC-08 hypothesis misleading_failure triggers synchronous quarantine",
			alpha:            1.4,
			beta:             2.6,
			utility:          0.0,
			epistemic:        memory.EpistemicHypothesis,
			status:           memory.StatusActive,
			signal:           memory.SignalMisleadingFailure,
			wantAlpha:        1.4,
			wantBeta:         5.1,
			wantUtility:      -0.25,
			wantEpistemic:    memory.EpistemicHypothesis,
			wantStatus:       memory.StatusConflicted,
			wantQuarantined:  true,
			wantTopicRelease: true,
		},
		{
			name:          "misleading_failure clamps utility at -1.0",
			alpha:         8.5,
			beta:          1.5,
			utility:       -1.0,
			epistemic:     memory.EpistemicObserved,
			status:        memory.StatusActive,
			signal:        memory.SignalMisleadingFailure,
			wantAlpha:     8.5,
			wantBeta:      4.0,
			wantUtility:   -1.0,
			wantEpistemic: memory.EpistemicObserved,
			wantStatus:    memory.StatusActive,
		},
		{
			name:          "exact 0.30 confidence does not quarantine active memory",
			alpha:         3.0,
			beta:          7.0,
			utility:       0.0,
			epistemic:     memory.EpistemicInferred,
			status:        memory.StatusActive,
			signal:        memory.SignalRetrieved,
			wantAlpha:     3.0,
			wantBeta:      7.0,
			wantUtility:   0.0,
			wantEpistemic: memory.EpistemicInferred,
			wantStatus:    memory.StatusActive,
			wantAccessInc: 1,
		},
		{
			name:          "already conflicted memory does not re-trigger quarantine",
			alpha:         1.4,
			beta:          5.1,
			utility:       -0.25,
			epistemic:     memory.EpistemicHypothesis,
			status:        memory.StatusConflicted,
			signal:        memory.SignalIgnored,
			wantAlpha:     1.4,
			wantBeta:      5.25,
			wantUtility:   -0.25,
			wantEpistemic: memory.EpistemicHypothesis,
			wantStatus:    memory.StatusConflicted,
		},
		{
			name:             "corrected_by_user adds 4.0 to beta and can quarantine",
			alpha:            1.4,
			beta:             2.6,
			utility:          0.0,
			epistemic:        memory.EpistemicHypothesis,
			status:           memory.StatusActive,
			signal:           memory.SignalCorrectedByUser,
			wantAlpha:        1.4,
			wantBeta:         6.6,
			wantUtility:      0.0,
			wantEpistemic:    memory.EpistemicHypothesis,
			wantStatus:       memory.StatusConflicted,
			wantQuarantined:  true,
			wantTopicRelease: true,
		},
		{
			name:      "closed memory rejects feedback",
			alpha:     8.5,
			beta:      1.5,
			utility:   0.0,
			epistemic: memory.EpistemicObserved,
			status:    memory.StatusSuperseded,
			signal:    memory.SignalHelpfulSuccess,
			wantErr:   memory.ErrClosedMemoryConflict,
		},
		{
			name:      "invalid status rejected",
			alpha:     8.5,
			beta:      1.5,
			utility:   0.0,
			epistemic: memory.EpistemicObserved,
			status:    memory.MemoryStatus("deleted"),
			signal:    memory.SignalHelpfulSuccess,
			wantErr:   memory.ErrInvalidMemoryStatus,
		},
		{
			name:      "invalid epistemic status rejected",
			alpha:     8.5,
			beta:      1.5,
			utility:   0.0,
			epistemic: memory.EpistemicStatus("guess"),
			status:    memory.StatusActive,
			signal:    memory.SignalHelpfulSuccess,
			wantErr:   memory.ErrInvalidEpistemicStatus,
		},
		{
			name:      "non-positive alpha rejected",
			alpha:     0,
			beta:      1.5,
			utility:   0.0,
			epistemic: memory.EpistemicObserved,
			status:    memory.StatusActive,
			signal:    memory.SignalHelpfulSuccess,
			wantErr:   memory.ErrInvalidBayesianParams,
		},
		{
			name:      "NaN beta rejected",
			alpha:     8.5,
			beta:      nan32,
			utility:   0.0,
			epistemic: memory.EpistemicObserved,
			status:    memory.StatusActive,
			signal:    memory.SignalHelpfulSuccess,
			wantErr:   memory.ErrInvalidBayesianParams,
		},
		{
			name:      "Inf alpha rejected",
			alpha:     inf32,
			beta:      1.5,
			utility:   0.0,
			epistemic: memory.EpistemicObserved,
			status:    memory.StatusActive,
			signal:    memory.SignalHelpfulSuccess,
			wantErr:   memory.ErrInvalidBayesianParams,
		},
		{
			name:      "utility out of [-1, 1] rejected",
			alpha:     8.5,
			beta:      1.5,
			utility:   1.5,
			epistemic: memory.EpistemicObserved,
			status:    memory.StatusActive,
			signal:    memory.SignalHelpfulSuccess,
			wantErr:   memory.ErrInvalidBayesianParams,
		},
		{
			name:      "invalid signal rejected",
			alpha:     8.5,
			beta:      1.5,
			utility:   0.0,
			epistemic: memory.EpistemicObserved,
			status:    memory.StatusActive,
			signal:    memory.FeedbackSignal("unknown"),
			wantErr:   memory.ErrInvalidFeedbackSignal,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res, err := memory.ApplyFeedback(tc.alpha, tc.beta, tc.utility, tc.epistemic, tc.status, tc.signal)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("ApplyFeedback() error = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ApplyFeedback() unexpected error = %v", err)
			}
			if !approxEqual32(res.NewAlpha, tc.wantAlpha, 1e-5) {
				t.Errorf("NewAlpha = %v, want %v", res.NewAlpha, tc.wantAlpha)
			}
			if !approxEqual32(res.NewBeta, tc.wantBeta, 1e-5) {
				t.Errorf("NewBeta = %v, want %v", res.NewBeta, tc.wantBeta)
			}
			if !approxEqual32(res.NewUtility, tc.wantUtility, 1e-5) {
				t.Errorf("NewUtility = %v, want %v", res.NewUtility, tc.wantUtility)
			}
			if res.NewEpistemic != tc.wantEpistemic {
				t.Errorf("NewEpistemic = %q, want %q", res.NewEpistemic, tc.wantEpistemic)
			}
			if res.NewStatus != tc.wantStatus {
				t.Errorf("NewStatus = %q, want %q", res.NewStatus, tc.wantStatus)
			}
			if res.Quarantined != tc.wantQuarantined {
				t.Errorf("Quarantined = %v, want %v", res.Quarantined, tc.wantQuarantined)
			}
			if res.ReleaseActiveTopicKey != tc.wantTopicRelease {
				t.Errorf("ReleaseActiveTopicKey = %v, want %v", res.ReleaseActiveTopicKey, tc.wantTopicRelease)
			}
			if res.AccessIncrement != tc.wantAccessInc {
				t.Errorf("AccessIncrement = %d, want %d", res.AccessIncrement, tc.wantAccessInc)
			}
			wantConf := tc.wantAlpha / (tc.wantAlpha + tc.wantBeta)
			if !approxEqual32(res.NewConfidence, wantConf, 1e-5) {
				t.Errorf("NewConfidence = %v, want %v", res.NewConfidence, wantConf)
			}
		})
	}
}

func TestCanApplyFeedbackSignal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		caller  identity.PrincipalType
		signal  memory.FeedbackSignal
		wantErr error
	}{
		{name: "agent denied confirmed_verified", caller: identity.PrincipalAgent, signal: memory.SignalConfirmedVerified, wantErr: memory.ErrForbidden},
		{name: "user allowed confirmed_verified", caller: identity.PrincipalUser, signal: memory.SignalConfirmedVerified},
		{name: "orchestrator allowed confirmed_verified", caller: identity.PrincipalOrchestrator, signal: memory.SignalConfirmedVerified},
		{name: "service allowed confirmed_verified", caller: identity.PrincipalService, signal: memory.SignalConfirmedVerified},
		{name: "agent allowed helpful_success", caller: identity.PrincipalAgent, signal: memory.SignalHelpfulSuccess},
		{name: "unknown caller denied", caller: identity.PrincipalType("anon"), signal: memory.SignalHelpfulSuccess, wantErr: memory.ErrForbidden},
		{name: "unknown signal rejected", caller: identity.PrincipalUser, signal: memory.FeedbackSignal("nope"), wantErr: memory.ErrInvalidFeedbackSignal},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := memory.CanApplyFeedbackSignal(tc.caller, tc.signal)
			if tc.wantErr == nil && err != nil {
				t.Fatalf("CanApplyFeedbackSignal() unexpected error = %v", err)
			}
			if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Errorf("CanApplyFeedbackSignal() error = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestDecideTopicSupersession(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		oldStatus   memory.EpistemicStatus
		newStatus   memory.EpistemicStatus
		explicit    bool
		caller      identity.PrincipalType
		reason      string
		wantOutcome memory.SupersessionOutcome
		wantOld     memory.MemoryStatus
		wantNew     memory.MemoryStatus
		wantRel     memory.RelationType
		wantErr     error
	}{
		{
			name:        "auto lower rank on verified becomes conflicted",
			oldStatus:   memory.EpistemicVerified,
			newStatus:   memory.EpistemicObserved,
			explicit:    false,
			caller:      identity.PrincipalAgent,
			wantOutcome: memory.OutcomeKeepOldConflictNew,
			wantOld:     memory.StatusActive,
			wantNew:     memory.StatusConflicted,
			wantRel:     memory.RelationContradicts,
		},
		{
			name:        "auto equal rank observed supersedes observed",
			oldStatus:   memory.EpistemicObserved,
			newStatus:   memory.EpistemicObserved,
			explicit:    false,
			caller:      identity.PrincipalAgent,
			wantOutcome: memory.OutcomeSupersedeOld,
			wantOld:     memory.StatusSuperseded,
			wantNew:     memory.StatusActive,
			wantRel:     memory.RelationSupersedes,
		},
		{
			name:        "auto higher rank verified supersedes observed for orchestrator",
			oldStatus:   memory.EpistemicObserved,
			newStatus:   memory.EpistemicVerified,
			explicit:    false,
			caller:      identity.PrincipalOrchestrator,
			wantOutcome: memory.OutcomeSupersedeOld,
			wantOld:     memory.StatusSuperseded,
			wantNew:     memory.StatusActive,
			wantRel:     memory.RelationSupersedes,
		},
		{
			name:        "explicit verified->observed allowed for user with reason",
			oldStatus:   memory.EpistemicVerified,
			newStatus:   memory.EpistemicObserved,
			explicit:    true,
			caller:      identity.PrincipalUser,
			reason:      "production schema updated after migration 00002",
			wantOutcome: memory.OutcomeSupersedeOld,
			wantOld:     memory.StatusSuperseded,
			wantNew:     memory.StatusActive,
			wantRel:     memory.RelationSupersedes,
		},
		{
			name:      "explicit verified->observed forbidden for service principal",
			oldStatus: memory.EpistemicVerified,
			newStatus: memory.EpistemicObserved,
			explicit:  true,
			caller:    identity.PrincipalService,
			reason:    "automated sync",
			wantErr:   memory.ErrForbidden,
		},
		{
			name:      "explicit verified->observed rejected without reason",
			oldStatus: memory.EpistemicVerified,
			newStatus: memory.EpistemicObserved,
			explicit:  true,
			caller:    identity.PrincipalOrchestrator,
			reason:    "   ",
			wantErr:   memory.ErrMissingChangeReason,
		},
		{
			name:      "explicit verified->observed forbidden for agent",
			oldStatus: memory.EpistemicVerified,
			newStatus: memory.EpistemicObserved,
			explicit:  true,
			caller:    identity.PrincipalAgent,
			reason:    "agent wants override",
			wantErr:   memory.ErrForbidden,
		},
		{
			name:      "explicit verified->hypothesis rejected even for user",
			oldStatus: memory.EpistemicVerified,
			newStatus: memory.EpistemicHypothesis,
			explicit:  true,
			caller:    identity.PrincipalUser,
			reason:    "guess",
			wantErr:   memory.ErrEpistemicDowngradeNotAllowed,
		},
		{
			name:      "agent cannot supply verified on auto capture",
			oldStatus: memory.EpistemicObserved,
			newStatus: memory.EpistemicVerified,
			explicit:  false,
			caller:    identity.PrincipalAgent,
			wantErr:   memory.ErrForbidden,
		},
		{
			name:      "invalid old epistemic status rejected",
			oldStatus: memory.EpistemicStatus("bad"),
			newStatus: memory.EpistemicObserved,
			explicit:  false,
			caller:    identity.PrincipalUser,
			wantErr:   memory.ErrInvalidEpistemicStatus,
		},
		{
			name:      "invalid principal type rejected",
			oldStatus: memory.EpistemicObserved,
			newStatus: memory.EpistemicObserved,
			explicit:  false,
			caller:    identity.PrincipalType("unknown"),
			wantErr:   memory.ErrForbidden,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := memory.DecideTopicSupersession(tc.oldStatus, tc.newStatus, tc.explicit, tc.caller, tc.reason)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("DecideTopicSupersession() error = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("DecideTopicSupersession() unexpected error = %v", err)
			}
			if got.Outcome != tc.wantOutcome || got.OldNewStatus != tc.wantOld || got.NewMemoryStatus != tc.wantNew || got.Relation != tc.wantRel {
				t.Errorf("got %+v, want outcome=%s old=%s new=%s rel=%s", got, tc.wantOutcome, tc.wantOld, tc.wantNew, tc.wantRel)
			}
		})
	}
}
