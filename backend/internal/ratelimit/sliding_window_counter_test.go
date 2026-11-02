//go:build w1o2

package ratelimit

import (
	"testing"
	"time"
)

func TestW1O2_SlidingWindowCounterLimit(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	l := NewSlidingWindowCounter(4, 10*time.Second, clock.Now)
	for i, want := range []bool{true, true, true, true, false} {
		if got := l.Allow("u"); got != want {
			t.Fatalf("request %d: Allow() = %v, want %v", i, got, want)
		}
	}
}

func TestW1O2_PreviousWindowIsWeightedByOverlap(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	l := NewSlidingWindowCounter(10, 10*time.Second, clock.Now)
	for i := 0; i < 10; i++ {
		if !l.Allow("u") {
			t.Fatalf("request %d in the first window must pass", i)
		}
	}
	if l.Allow("u") {
		t.Fatal("the eleventh request in the first window must fail")
	}
	clock.Advance(15 * time.Second)
	allowed := 0
	for i := 0; i < 20; i++ {
		if l.Allow("u") {
			allowed++
		}
	}
	if allowed != 5 {
		t.Fatalf("allowed %d requests five seconds into the next window, want 5 (10 x 0.5 previous plus 5 new = 10)", allowed)
	}
}

func TestW1O2_IdleWindowsAreForgotten(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	l := NewSlidingWindowCounter(3, 10*time.Second, clock.Now)
	for i := 0; i < 3; i++ {
		if !l.Allow("u") {
			t.Fatalf("request %d must pass", i)
		}
	}
	clock.Advance(25 * time.Second)
	for i := 0; i < 3; i++ {
		if !l.Allow("u") {
			t.Fatalf("request %d after two idle windows must pass", i)
		}
	}
	if l.Allow("u") {
		t.Fatal("the fourth request must fail")
	}
}

func TestW1O2_KeysAreIndependent(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	l := NewSlidingWindowCounter(1, 10*time.Second, clock.Now)
	if !l.Allow("a") || !l.Allow("b") {
		t.Fatal("first request per key must pass")
	}
	if l.Allow("a") {
		t.Fatal("second request for a must fail")
	}
}
