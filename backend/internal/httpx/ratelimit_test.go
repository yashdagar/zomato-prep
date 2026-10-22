package httpx_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yashdagar/zomato-prep/backend/internal/httpx"
)

type fakeLimiter struct{ seen []string }

func (f *fakeLimiter) Allow(key string) bool {
	f.seen = append(f.seen, key)
	return len(f.seen) <= 2
}

func TestW1B5_KeyIsUserIDThenIP(t *testing.T) {
	l := &fakeLimiter{}
	h := httpx.RateLimit(l)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/healthz", nil)
	req.Header.Set("X-User-ID", "42")
	req.RemoteAddr = "10.0.0.1:5555"
	h.ServeHTTP(httptest.NewRecorder(), req)

	req2 := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/healthz", nil)
	req2.RemoteAddr = "10.0.0.1:5555"
	h.ServeHTTP(httptest.NewRecorder(), req2)

	if len(l.seen) != 2 || l.seen[0] != "42" {
		t.Fatalf("keys = %v, want the user id first", l.seen)
	}
	if l.seen[1] == "" || l.seen[1] == "10.0.0.1:5555" {
		t.Fatalf("ip key = %q, want the host without the port", l.seen[1])
	}
}

func TestW1B5_RejectWrites429WithRetryAfterAndJSON(t *testing.T) {
	l := &fakeLimiter{seen: []string{"a", "b"}}
	h := httpx.RateLimit(l)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Fatal("Retry-After header missing")
	}
	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil || body["error"] != "rate_limited" {
		t.Fatalf("body = %s, want {\"error\":\"rate_limited\"}", rec.Body.String())
	}
}

func TestW1B5_AllowedRequestPassesThrough(t *testing.T) {
	l := &fakeLimiter{}
	called := false
	h := httpx.RateLimit(l)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { called = true; w.WriteHeader(http.StatusOK) }))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/healthz", nil))
	if !called || rec.Code != http.StatusOK {
		t.Fatalf("called=%v status=%d", called, rec.Code)
	}
}
