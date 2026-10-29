package ratelimit

import (
	"testing"
	"time"
)

type testStep struct {
	wait   time.Duration
	expect bool
}

type testCase struct {
	name     string
	capacity int
	refill   float64
	steps    []testStep
}

func TestTokenBucketRateLimiter(t *testing.T) {
	tests := []testCase{
		// Define test cases for the token bucket rate limiter
		{
			name: "Burst till rejection", capacity: 2, refill: 1,
			steps: []testStep{{0, true}, {0, true}, {0, false}},
		},
		{
			name: "Long wait should not exceed the max capacity", capacity: 2, refill: 10,
			steps: []testStep{{10 * time.Second, true}, {0, true}, {0, false}},
		},
		{
			name: "Check at the boundary, expect reject", capacity: 1, refill: 1,
			steps: []testStep{{0, true}, {0, false}, {1 * time.Second, true}, {999 * time.Millisecond, false}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			clock := &fakeClock{t: time.Unix(1_700_000_000, 0)}
			bucket := NewTokenBucket(tc.capacity, tc.refill, clock.Now)

			for i, step := range tc.steps {
				clock.Advance(step.wait)

				if allow := bucket.Allow("any key"); allow != step.expect {
					t.Fatalf("Failed step [%d]: expected: %v, got: %v", i, step.expect, allow)
				}
			}
		})
	}
}
