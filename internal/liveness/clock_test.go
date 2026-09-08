package liveness_test

import (
	"testing"
	"time"

	"github.com/abn/relay/internal/liveness"
)

func TestVirtualClockTimerFiring(t *testing.T) {
	start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	clock := liveness.NewVirtualClock(start)

	timer1 := clock.NewTimer(10 * time.Second)
	timer2 := clock.NewTimer(30 * time.Second)

	// Before advance, channels should be empty
	select {
	case <-timer1.C():
		t.Fatal("timer1 fired prematurely")
	default:
	}
	select {
	case <-timer2.C():
		t.Fatal("timer2 fired prematurely")
	default:
	}

	// Advance 10s: timer1 fires, timer2 does not
	clock.Advance(10 * time.Second)
	select {
	case tm := <-timer1.C():
		expected := start.Add(10 * time.Second)
		if !tm.Equal(expected) {
			t.Errorf("expected trigger time %v, got %v", expected, tm)
		}
	default:
		t.Fatal("timer1 failed to fire after 10s")
	}

	select {
	case <-timer2.C():
		t.Fatal("timer2 fired after 10s")
	default:
	}

	// Advance another 20s: timer2 fires
	clock.Advance(20 * time.Second)
	select {
	case tm := <-timer2.C():
		expected := start.Add(30 * time.Second)
		if !tm.Equal(expected) {
			t.Errorf("expected trigger time %v, got %v", expected, tm)
		}
	default:
		t.Fatal("timer2 failed to fire after 30s")
	}
}

func TestVirtualClockTimerStop(t *testing.T) {
	start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	clock := liveness.NewVirtualClock(start)

	timer := clock.NewTimer(10 * time.Second)
	if !timer.Stop() {
		t.Fatal("expected Stop() to return true for active timer")
	}
	if timer.Stop() {
		t.Fatal("expected second Stop() to return false")
	}

	clock.Advance(20 * time.Second)
	select {
	case <-timer.C():
		t.Fatal("stopped timer fired")
	default:
	}
}

func TestVirtualClockTimerReset(t *testing.T) {
	start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	clock := liveness.NewVirtualClock(start)

	timer := clock.NewTimer(10 * time.Second)
	clock.Advance(5 * time.Second)

	// Reset to 15s from current (t=5s, trigger at t=20s)
	if !timer.Reset(15 * time.Second) {
		t.Fatal("expected Reset() to return true for active timer")
	}

	clock.Advance(10 * time.Second) // t=15s
	select {
	case <-timer.C():
		t.Fatal("timer fired before reset deadline")
	default:
	}

	clock.Advance(5 * time.Second) // t=20s
	select {
	case <-timer.C():
	default:
		t.Fatal("timer failed to fire at reset deadline")
	}
}
