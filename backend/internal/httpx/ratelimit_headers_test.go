//go:build w1o5

package httpx_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/yashdagar/zomato-prep/backend/internal/httpx"
	"github.com/yashdagar/zomato-prep/backend/internal/ratelimit"
)

func headerLimiter(limit int) *ratelimit.FixedWindow {
	now := time.Unix(1_700_000_000, 0)
	return ratelimit.NewFixedWindow(limit, time.Minute, func() time.Time { return now })
}

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
}

func TestW1O5_CheckReportsLimitRemainingAndReset(t *testing.T) {
	l := headerLimiter(2)
	first := l.Check("u")
	if !first.Allowed || first.Limit != 2 || first.Remaining != 1 || first.Reset != 40*time.Second {
		t.Fatalf("first = %+v, want allowed, limit 2, remaining 1, reset 40s", first)
	}
	second := l.Check("u")
	if !second.Allowed || second.Remaining != 0 {
		t.Fatalf("second = %+v, want allowed with remaining 0", second)
	}
	third := l.Check("u")
	if third.Allowed || third.Remaining != 0 || third.Reset != 40*time.Second {
		t.Fatalf("third = %+v, want rejected, remaining 0, reset 40s", third)
	}
}

func TestW1O5_AllowedResponsesCarryRateLimitHeaders(t *testing.T) {
	h := httpx.RateLimitHeaders(headerLimiter(3))(okHandler())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	for header, want := range map[string]string{
		"X-RateLimit-Limit":     "3",
		"X-RateLimit-Remaining": "2",
		"X-RateLimit-Reset":     "40",
	} {
		if got := rec.Header().Get(header); got != want {
			t.Fatalf("%s = %q, want %q", header, got, want)
		}
	}
}

func TestW1O5_RejectedResponseHasRetryAfterAndZeroRemaining(t *testing.T) {
	h := httpx.RateLimitHeaders(headerLimiter(1))(okHandler())
	for i := 0; i < 2; i++ {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/healthz", nil))
		if i == 0 {
			continue
		}
		if rec.Code != http.StatusTooManyRequests {
			t.Fatalf("status = %d, want 429", rec.Code)
		}
		if got := rec.Header().Get("Retry-After"); got != "40" {
			t.Fatalf("Retry-After = %q, want 40", got)
		}
		if got := rec.Header().Get("X-RateLimit-Remaining"); got != "0" {
			t.Fatalf("X-RateLimit-Remaining = %q, want 0", got)
		}
		var body map[string]string
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil || body["error"] != "rate_limited" {
			t.Fatalf("body = %s, want {\"error\":\"rate_limited\"}", rec.Body.String())
		}
	}
}
