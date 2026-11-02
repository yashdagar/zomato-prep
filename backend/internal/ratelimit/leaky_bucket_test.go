//go:build w1o3

package ratelimit

import (
	"testing"
	"time"
)

func TestW1O3_LeakyBucket(t *testing.T) {
	tests := []struct {
		name     string
		capacity int
		leak     float64
		steps    []struct {
			advance time.Duration
			want    bool
		}
	}{
		{
			name: "fills up then rejects", capacity: 2, leak: 1,
			steps: []struct {
				advance time.Duration
				want    bool
			}{{0, true}, {0, true}, {0, false}},
		},
		{
			name: "leaks one unit per second", capacity: 2, leak: 1,
			steps: []struct {
				advance time.Duration
				want    bool
			}{{0, true}, {0, true}, {0, false}, {500 * time.Millisecond, false}, {500 * time.Millisecond, true}, {500 * time.Millisecond, false}, {500 * time.Millisecond, true}},
		},
		{
			name: "never goes below empty after a long idle", capacity: 2, leak: 10,
			steps: []struct {
				advance time.Duration
				want    bool
			}{{time.Hour, true}, {0, true}, {0, false}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			clock := &fakeClock{t: time.Unix(1_700_000_000, 0)}
			l := NewLeakyBucket(tc.capacity, tc.leak, clock.Now)
			for i, s := range tc.steps {
				clock.Advance(s.advance)
				if got := l.Allow("u"); got != s.want {
					t.Fatalf("step %d: Allow() = %v, want %v", i, got, s.want)
				}
			}
		})
	}
}

func TestW1O3_KeysAreIndependent(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	l := NewLeakyBucket(1, 1, clock.Now)
	if !l.Allow("a") || !l.Allow("b") {
		t.Fatal("first request per key must pass")
	}
	if l.Allow("a") {
		t.Fatal("second request for a must fail")
	}
}
