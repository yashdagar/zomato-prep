//go:build w6o1

package killswitch_test

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/yashdagar/zomato-prep/backend/internal/killswitch"
)

func body(h http.Handler) string {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	return rec.Body.String()
}

func text(s string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(s)) })
}

func TestW6O1_UnknownFlagsAreEnabled(t *testing.T) {
	s := killswitch.New(nil)
	if !s.Enabled("search_ranking") {
		t.Fatal("a flag nobody turned off must be enabled")
	}
}

func TestW6O1_NewHonoursTheInitialValues(t *testing.T) {
	s := killswitch.New(map[string]bool{"search_ranking": false, "checkout": true})
	if s.Enabled("search_ranking") || !s.Enabled("checkout") {
		t.Fatalf("snapshot %v", s.Snapshot())
	}
}

func TestW6O1_FromEnvTurnsAFlagOffForOffFalseZeroAndDisabled(t *testing.T) {
	for _, v := range []string{"off", "OFF", "false", "0", "disabled"} {
		t.Setenv("W6O1_SEARCH_RANKING", v)
		s := killswitch.FromEnv("W6O1_", "search_ranking")
		if s.Enabled("search_ranking") {
			t.Fatalf("W6O1_SEARCH_RANKING=%s left the flag on", v)
		}
	}
}

func TestW6O1_FromEnvLeavesOtherValuesAndUnsetFlagsOn(t *testing.T) {
	t.Setenv("W6O1_SEARCH_RANKING", "on")
	s := killswitch.FromEnv("W6O1_", "search_ranking", "recommendations")
	if !s.Enabled("search_ranking") || !s.Enabled("recommendations") {
		t.Fatalf("snapshot %v", s.Snapshot())
	}
}

func TestW6O1_SetFlipsAFlagAtRuntime(t *testing.T) {
	s := killswitch.New(nil)
	s.Set("search_ranking", false)
	if s.Enabled("search_ranking") {
		t.Fatal("Set(false) did not turn the flag off")
	}
	s.Set("search_ranking", true)
	if !s.Enabled("search_ranking") {
		t.Fatal("Set(true) did not turn the flag back on")
	}
}

func TestW6O1_GuardServesTheFallbackWhileTheFlagIsOff(t *testing.T) {
	s := killswitch.New(nil)
	h := s.Guard("search_ranking", text("ranked"), text("unranked"))
	if got := body(h); got != "ranked" {
		t.Fatalf("flag on served %q", got)
	}
	s.Set("search_ranking", false)
	if got := body(h); got != "unranked" {
		t.Fatalf("flag off served %q", got)
	}
	s.Set("search_ranking", true)
	if got := body(h); got != "ranked" {
		t.Fatalf("flag back on served %q", got)
	}
}

func TestW6O1_OneFlagDoesNotAffectAnother(t *testing.T) {
	s := killswitch.New(nil)
	search := s.Guard("search_ranking", text("ranked"), text("unranked"))
	checkout := s.Guard("checkout", text("checkout"), text("closed"))
	s.Set("search_ranking", false)
	if body(search) != "unranked" || body(checkout) != "checkout" {
		t.Fatal("turning search ranking off must leave checkout working")
	}
}

func TestW6O1_SnapshotIsACopy(t *testing.T) {
	s := killswitch.New(map[string]bool{"a": true})
	snap := s.Snapshot()
	snap["a"] = false
	if !s.Enabled("a") {
		t.Fatal("changing the snapshot changed the switches")
	}
}

func TestW6O1_ConcurrentSetAndEnabledAreSafe(t *testing.T) {
	s := killswitch.New(nil)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); s.Set("x", i%2 == 0) }()
		go func() { defer wg.Done(); _ = s.Enabled("x"); _ = s.Snapshot() }()
	}
	wg.Wait()
}
