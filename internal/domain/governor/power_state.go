// Package governor implements the elastic PowerGovernor state machine
// (lite/balanced/ultra profiles and guard/burst/rem_sleep operational states)
// with sub-10ms preemption of REM Sleep consolidation upon real-time agent traffic.
package governor

import (
	"context"
	"errors"
	"sync"
	"time"
)

// Profile defines the active cognitive hardware profile.
type Profile string

const (
	ProfileLite     Profile = "lite"
	ProfileBalanced Profile = "balanced"
	ProfileUltra    Profile = "ultra"
)

// State defines the current operational state of the PowerGovernor.
type State string

const (
	StateGuard    State = "guard"
	StateBurst    State = "burst"
	StateREMSleep State = "rem_sleep"
)

// ErrREMPreempted is returned when a REM Sleep cycle is preempted by agent traffic.
var ErrREMPreempted = errors.New("rem_sleep preempted by real-time agent traffic")

// Config configures PowerGovernor thresholds and VRAM unload callbacks.
type Config struct {
	Profile           Profile
	BurstWindow       time.Duration
	UnloadTimeout     time.Duration
	OnUnloadVRAM      func(ctx context.Context) error
	RerankerAvailable bool
	NLIAvailable      bool
	SLMAvailable      bool
}

// Snapshot is a point-in-time read-only view of PowerGovernor state for system/status.
type Snapshot struct {
	Profile           Profile   `json:"profile"`
	State             State     `json:"state"`
	InFlightRequests  int64     `json:"in_flight_requests"`
	LastActivityAt    time.Time `json:"last_activity_at"`
	PreemptionsCount  int64     `json:"preemptions_count"`
	RerankerAvailable bool      `json:"reranker_available"`
	NLIAvailable      bool      `json:"nli_available"`
	SLMAvailable      bool      `json:"slm_available"`
}

// PowerGovernor coordinates real-time agent traffic and background REM Sleep cycles.
type PowerGovernor struct {
	mu                sync.Mutex
	profile           Profile
	state             State
	inFlight          int64
	lastActivityAt    time.Time
	preemptionsCount  int64
	burstWindow       time.Duration
	unloadTimeout     time.Duration
	onUnloadVRAM      func(ctx context.Context) error
	remCancel         context.CancelFunc
	rerankerAvailable bool
	nliAvailable      bool
	slmAvailable      bool
}

// NewPowerGovernor constructs a PowerGovernor starting in Guard state.
func NewPowerGovernor(cfg Config) *PowerGovernor {
	if cfg.Profile == "" {
		cfg.Profile = ProfileBalanced
	}
	if cfg.BurstWindow <= 0 {
		cfg.BurstWindow = 2 * time.Second
	}
	if cfg.UnloadTimeout <= 0 {
		cfg.UnloadTimeout = 5 * time.Second
	}
	return &PowerGovernor{
		profile:           cfg.Profile,
		state:             StateGuard,
		lastActivityAt:    time.Now().UTC(),
		burstWindow:       cfg.BurstWindow,
		unloadTimeout:     cfg.UnloadTimeout,
		onUnloadVRAM:      cfg.OnUnloadVRAM,
		rerankerAvailable: cfg.RerankerAvailable,
		nliAvailable:      cfg.NLIAvailable,
		slmAvailable:      cfg.SLMAvailable,
	}
}

// BeginAgentRequest marks an incoming real-time agent operation, immediately preempting
// any active REM Sleep cycle (< 10ms) and scheduling async VRAM release.
func (g *PowerGovernor) BeginAgentRequest() func() {
	g.mu.Lock()
	g.inFlight++
	g.lastActivityAt = time.Now().UTC()

	var cancel context.CancelFunc
	if g.state == StateREMSleep && g.remCancel != nil {
		cancel = g.remCancel
		g.remCancel = nil
		g.preemptionsCount++
	}
	if g.inFlight > 1 {
		g.state = StateBurst
	} else {
		g.state = StateGuard
	}
	unloadFn := g.onUnloadVRAM
	unloadTimeout := g.unloadTimeout
	g.mu.Unlock()

	if cancel != nil {
		cancel()
		if unloadFn != nil {
			go func() {
				ctx, c := context.WithTimeout(context.Background(), unloadTimeout)
				defer c()
				_ = unloadFn(ctx)
			}()
		}
	}

	return func() {
		g.mu.Lock()
		defer g.mu.Unlock()
		if g.inFlight > 0 {
			g.inFlight--
		}
		g.lastActivityAt = time.Now().UTC()
		if g.state != StateREMSleep {
			if g.inFlight > 1 {
				g.state = StateBurst
			} else {
				g.state = StateGuard
			}
		}
	}
}

// RunInREMSleep transitions to rem_sleep if no real-time agent requests are in flight
// and executes fn under a preemptible child context.
func (g *PowerGovernor) RunInREMSleep(ctx context.Context, fn func(remCtx context.Context) error) error {
	g.mu.Lock()
	if g.inFlight > 0 || g.state == StateREMSleep {
		g.mu.Unlock()
		return ErrREMPreempted
	}
	remCtx, cancel := context.WithCancel(ctx)
	g.state = StateREMSleep
	g.remCancel = cancel
	g.mu.Unlock()

	defer func() {
		cancel()
		g.mu.Lock()
		g.remCancel = nil
		if g.state == StateREMSleep {
			if g.inFlight > 1 {
				g.state = StateBurst
			} else {
				g.state = StateGuard
			}
		}
		g.mu.Unlock()
	}()

	return fn(remCtx)
}

// Status returns a thread-safe snapshot of the PowerGovernor.
func (g *PowerGovernor) Status() Snapshot {
	g.mu.Lock()
	defer g.mu.Unlock()
	return Snapshot{
		Profile:           g.profile,
		State:             g.state,
		InFlightRequests:  g.inFlight,
		LastActivityAt:    g.lastActivityAt,
		PreemptionsCount:  g.preemptionsCount,
		RerankerAvailable: g.rerankerAvailable,
		NLIAvailable:      g.nliAvailable,
		SLMAvailable:      g.slmAvailable,
	}
}

// SetProviderAvailability updates runtime health flags for local neural models.
func (g *PowerGovernor) SetProviderAvailability(reranker, nli, slm bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.rerankerAvailable = reranker
	g.nliAvailable = nli
	g.slmAvailable = slm
}
