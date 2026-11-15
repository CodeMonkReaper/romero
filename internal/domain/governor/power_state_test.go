package governor

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestPowerGovernorPreemptionUnder10ms(t *testing.T) {
	var unloaded atomic.Int32
	gov := NewPowerGovernor(Config{
		Profile:     ProfileUltra,
		BurstWindow: 500 * time.Millisecond,
		OnUnloadVRAM: func(ctx context.Context) error {
			unloaded.Add(1)
			return nil
		},
	})

	if snap := gov.Status(); snap.State != StateGuard || snap.Profile != ProfileUltra {
		t.Fatalf("unexpected initial status: %+v", snap)
	}

	remStarted := make(chan struct{})
	remCanceled := make(chan time.Duration, 1)

	errCh := make(chan error, 1)
	go func() {
		errCh <- gov.RunInREMSleep(context.Background(), func(remCtx context.Context) error {
			close(remStarted)
			<-remCtx.Done()
			remCanceled <- time.Since(time.Now())
			return remCtx.Err()
		})
	}()

	<-remStarted
	if snap := gov.Status(); snap.State != StateREMSleep {
		t.Fatalf("expected rem_sleep state, got %s", snap.State)
	}

	t0 := time.Now()
	done := gov.BeginAgentRequest()
	elapsed := time.Since(t0)
	if elapsed > 10*time.Millisecond {
		t.Fatalf("BeginAgentRequest preemption took %v (> 10ms)", elapsed)
	}

	if snap := gov.Status(); snap.State == StateREMSleep || snap.PreemptionsCount != 1 {
		t.Fatalf("expected preempted active state and PreemptionsCount=1, got %+v", snap)
	}
	done()

	if err := <-errCh; err == nil {
		t.Fatal("expected context canceled error from RunInREMSleep")
	}

	// Wait briefly for async VRAM unload callback
	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) && unloaded.Load() == 0 {
		time.Sleep(2 * time.Millisecond)
	}
	if unloaded.Load() != 1 {
		t.Fatalf("expected async OnUnloadVRAM to be called once, got %d", unloaded.Load())
	}
}

func TestPowerGovernorBurstAndAvailability(t *testing.T) {
	gov := NewPowerGovernor(Config{})
	if snap := gov.Status(); snap.Profile != ProfileBalanced || snap.State != StateGuard {
		t.Fatalf("unexpected defaults: %+v", snap)
	}

	done1 := gov.BeginAgentRequest()
	done2 := gov.BeginAgentRequest()
	if snap := gov.Status(); snap.State != StateBurst || snap.InFlightRequests != 2 {
		t.Fatalf("expected burst with 2 in-flight, got %+v", snap)
	}
	if err := gov.RunInREMSleep(context.Background(), func(context.Context) error { return nil }); err != ErrREMPreempted {
		t.Fatalf("expected ErrREMPreempted while requests in flight, got %v", err)
	}
	done1()
	done2()

	if snap := gov.Status(); snap.State != StateGuard || snap.InFlightRequests != 0 {
		t.Fatalf("expected guard after requests finish, got %+v", snap)
	}

	gov.SetProviderAvailability(true, true, false)
	snap := gov.Status()
	if !snap.RerankerAvailable || !snap.NLIAvailable || snap.SLMAvailable {
		t.Fatalf("unexpected provider availability snapshot: %+v", snap)
	}

	if err := gov.RunInREMSleep(context.Background(), func(context.Context) error { return nil }); err != nil {
		t.Fatalf("expected clean REM execution when idle, got %v", err)
	}
	if gov.Status().State != StateGuard {
		t.Fatalf("expected return to guard after REM completion, got %s", gov.Status().State)
	}
}
