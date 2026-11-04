//go:build w6o3

package cache_test

import (
	"math"
	"testing"
	"time"

	"github.com/yashdagar/zomato-prep/backend/internal/cache"
)

var expiry = time.Date(2030, 1, 1, 12, 0, 0, 0, time.UTC)

func fixed(v float64) func() float64 { return func() float64 { return v } }

func TestW6O3_NeverRefreshesLongBeforeExpiry(t *testing.T) {
	now := expiry.Add(-time.Minute)
	if cache.ShouldRefresh(now, expiry, 100*time.Millisecond, 1, fixed(0.5)) {
		t.Fatal("refreshed a minute early for a 100 ms recompute")
	}
}

func TestW6O3_AlwaysRefreshesAtOrAfterExpiry(t *testing.T) {
	if !cache.ShouldRefresh(expiry, expiry, time.Second, 1, fixed(1)) {
		t.Fatal("did not refresh at the expiry instant")
	}
	if !cache.ShouldRefresh(expiry.Add(time.Second), expiry, 0, 1, fixed(1)) {
		t.Fatal("did not refresh after expiry")
	}
}

func TestW6O3_ASmallRandomValueRefreshesEarlier(t *testing.T) {
	now := expiry.Add(-2 * time.Second)
	recompute := time.Second
	early := cache.ShouldRefresh(now, expiry, recompute, 1, fixed(math.Exp(-3)))
	late := cache.ShouldRefresh(now, expiry, recompute, 1, fixed(math.Exp(-1)))
	if !early || late {
		t.Fatalf("early=%v late=%v, want true and false: a smaller random value moves the refresh point earlier", early, late)
	}
}

func TestW6O3_ARandomValueOfOneAddsNoEarlyRefresh(t *testing.T) {
	now := expiry.Add(-time.Nanosecond)
	if cache.ShouldRefresh(now, expiry, time.Hour, 1, fixed(1)) {
		t.Fatal("rnd=1 gives ln(1)=0, so nothing should refresh before expiry")
	}
}

func TestW6O3_BetaScalesTheEarlyWindow(t *testing.T) {
	now := expiry.Add(-3 * time.Second)
	rnd := fixed(math.Exp(-1))
	if cache.ShouldRefresh(now, expiry, time.Second, 1, rnd) {
		t.Fatal("beta 1: 1 s of early window should not reach 3 s")
	}
	if !cache.ShouldRefresh(now, expiry, time.Second, 4, rnd) {
		t.Fatal("beta 4: 4 s of early window should reach 3 s")
	}
}

func TestW6O3_ZeroBetaOrZeroRecomputeOnlyRefreshesAfterExpiry(t *testing.T) {
	now := expiry.Add(-time.Millisecond)
	if cache.ShouldRefresh(now, expiry, time.Hour, 0, fixed(0.0001)) {
		t.Fatal("beta 0 must disable early refresh")
	}
	if cache.ShouldRefresh(now, expiry, 0, 1, fixed(0.0001)) {
		t.Fatal("a recompute time of 0 must disable early refresh")
	}
}

func TestW6O3_ARandomValueOfZeroDoesNotOverflow(t *testing.T) {
	now := expiry.Add(-time.Hour)
	_ = cache.ShouldRefresh(now, expiry, time.Second, 1, fixed(0))
}

func TestW6O3_RefreshProbabilityRisesAsExpiryApproaches(t *testing.T) {
	count := func(before time.Duration) int {
		n := 0
		for i := 1; i <= 1000; i++ {
			if cache.ShouldRefresh(expiry.Add(-before), expiry, time.Second, 1, fixed(float64(i)/1000)) {
				n++
			}
		}
		return n
	}
	far, near := count(5*time.Second), count(500*time.Millisecond)
	if near <= far {
		t.Fatalf("refresh count near expiry %d should exceed far from expiry %d", near, far)
	}
}
