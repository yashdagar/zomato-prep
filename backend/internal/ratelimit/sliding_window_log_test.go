//go:build w1o1

package ratelimit

import (
	"testing"
	"time"
)

func TestW1O1_SlidingWindowLogLimit(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	l := NewSlidingWindowLog(3, 10*time.Second, clock.Now)
	for i, want := range []bool{true, true, true, false} {
		if got := l.Allow("u"); got != want {
			t.Fatalf("request %d: Allow() = %v, want %v", i, got, want)
		}
	}
}

func TestW1O1_OldRequestsLeaveTheWindowOneByOne(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	l := NewSlidingWindowLog(2, 10*time.Second, clock.Now)
	steps := []struct {
		advance time.Duration
		want    bool
	}{
		{0, true},
		{4 * time.Second, true},
		{time.Second, false},
		{5 * time.Second, true},
		{0, false},
		{4 * time.Second, true},
	}
	for i, s := range steps {
		clock.Advance(s.advance)
		if got := l.Allow("u"); got != s.want {
			t.Fatalf("step %d: Allow() = %v, want %v", i, got, s.want)
		}
	}
}

func TestW1O1_RejectedRequestsAreNotRecorded(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	l := NewSlidingWindowLog(1, 10*time.Second, clock.Now)
	if !l.Allow("u") {
		t.Fatal("first request must pass")
	}
	clock.Advance(5 * time.Second)
	if l.Allow("u") {
		t.Fatal("second request inside the window must fail")
	}
	clock.Advance(5 * time.Second)
	if !l.Allow("u") {
		t.Fatal("the rejected request at 5s must not have been recorded, so the first one expiring at 10s frees the slot")
	}
}

func TestW1O1_NoBoundaryBurst(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	l := NewSlidingWindowLog(2, 10*time.Second, clock.Now)
	clock.Advance(9 * time.Second)
	for i := 0; i < 2; i++ {
		if !l.Allow("u") {
			t.Fatalf("request %d must pass", i)
		}
	}
	clock.Advance(2 * time.Second)
	if l.Allow("u") {
		t.Fatal("two seconds later both requests are still inside the window, a fixed window would allow a burst here")
	}
	clock.Advance(8 * time.Second)
	if !l.Allow("u") {
		t.Fatal("ten seconds after the earlier requests the window is empty again")
	}
}

func TestW1O1_KeysAreIndependent(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	l := NewSlidingWindowLog(1, 10*time.Second, clock.Now)
	if !l.Allow("a") || !l.Allow("b") {
		t.Fatal("first request per key must pass")
	}
	if l.Allow("a") {
		t.Fatal("second request for a must fail")
	}
}
