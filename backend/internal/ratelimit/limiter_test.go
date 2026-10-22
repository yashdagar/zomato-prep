package ratelimit

import (
	"sync"
	"testing"
	"time"
)

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func TestW1B2_TokenBucket(t *testing.T) {
	tests := []struct {
		name     string
		capacity int
		refill   float64
		steps    []struct {
			advance time.Duration
			want    bool
		}
	}{
		{
			name: "burst up to capacity then reject", capacity: 2, refill: 1,
			steps: []struct {
				advance time.Duration
				want    bool
			}{{0, true}, {0, true}, {0, false}},
		},
		{
			name: "refills one token per second", capacity: 1, refill: 1,
			steps: []struct {
				advance time.Duration
				want    bool
			}{{0, true}, {0, false}, {time.Second, true}, {500 * time.Millisecond, false}},
		},
		{
			name: "never exceeds capacity after a long idle", capacity: 2, refill: 10,
			steps: []struct {
				advance time.Duration
				want    bool
			}{{time.Hour, true}, {0, true}, {0, false}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			clock := &fakeClock{t: time.Unix(1_700_000_000, 0)}
			l := NewTokenBucket(tc.capacity, tc.refill, clock.Now)
			for i, s := range tc.steps {
				clock.Advance(s.advance)
				if got := l.Allow("u1"); got != s.want {
					t.Fatalf("step %d: Allow() = %v, want %v", i, got, s.want)
				}
			}
		})
	}
}

func TestW1B2_KeysAreIndependent(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	l := NewTokenBucket(1, 1, clock.Now)
	if !l.Allow("a") || !l.Allow("b") {
		t.Fatal("first request per key must pass")
	}
	if l.Allow("a") {
		t.Fatal("second request for a must fail")
	}
}

func TestW1B3_FixedWindowBoundary(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	l := NewFixedWindow(2, time.Second, clock.Now)
	for i, want := range []bool{true, true, false} {
		if got := l.Allow("u"); got != want {
			t.Fatalf("request %d: Allow() = %v, want %v", i, got, want)
		}
	}
	clock.Advance(time.Second)
	if !l.Allow("u") {
		t.Fatal("new window must allow again")
	}
}

func TestW1B4_TokenBucketConcurrent(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	const capacity = 100
	l := NewTokenBucket(capacity, 0, clock.Now)

	var wg sync.WaitGroup
	var mu sync.Mutex
	allowed := 0
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 1000; i++ {
				if l.Allow("hot") {
					mu.Lock()
					allowed++
					mu.Unlock()
				}
			}
		}()
	}
	wg.Wait()
	if allowed != capacity {
		t.Fatalf("allowed %d, want exactly %d", allowed, capacity)
	}
}
