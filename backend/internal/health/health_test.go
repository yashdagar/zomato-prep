package health_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yashdagar/zomato-prep/backend/internal/health"
)

type readyBody struct {
	Status  string            `json:"status"`
	Failing []string          `json:"failing"`
	Checks  map[string]string `json:"checks"`
}

func get(t *testing.T, h http.Handler, path string) (*httptest.ResponseRecorder, readyBody) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil))
	var body readyBody
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	return rec, body
}

func ok(context.Context) error { return nil }

func TestW7B3_HealthzIsAlwaysOkAndJSON(t *testing.T) {
	rec, _ := get(t, health.Healthz(), "/healthz")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}
	var body map[string]bool
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || !body["ok"] {
		t.Fatalf("body = %q, want {\"ok\":true}", rec.Body.String())
	}
}

func TestW7B3_ReadyzIsOkWhenEveryDependencyIsHealthy(t *testing.T) {
	h := health.Readyz(time.Second, map[string]health.Check{"mysql": ok, "redis": ok, "kafka": ok})
	rec, body := get(t, h, "/readyz")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if body.Status != "ok" {
		t.Fatalf("status field = %q, want ok", body.Status)
	}
	for _, name := range []string{"mysql", "redis", "kafka"} {
		if body.Checks[name] != "ok" {
			t.Fatalf("checks[%s] = %q, want ok", name, body.Checks[name])
		}
	}
}

func TestW7B3_ReadyzReturns503NamingEveryFailingDependency(t *testing.T) {
	fail := func(context.Context) error { return errors.New("connection refused") }
	h := health.Readyz(time.Second, map[string]health.Check{"redis": fail, "mysql": ok, "kafka": fail})
	rec, body := get(t, h, "/readyz")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	if body.Status != "unavailable" {
		t.Fatalf("status field = %q, want unavailable", body.Status)
	}
	if len(body.Failing) != 2 || body.Failing[0] != "kafka" || body.Failing[1] != "redis" {
		t.Fatalf("failing = %v, want [kafka redis] sorted", body.Failing)
	}
	if body.Checks["mysql"] != "ok" {
		t.Fatalf("checks[mysql] = %q, a healthy dependency must still be reported ok", body.Checks["mysql"])
	}
}

func TestW7B3_ReadyzNeverLeaksErrorText(t *testing.T) {
	leaky := func(context.Context) error { return errors.New("dial tcp 10.0.3.7:3306: password=hunter2") }
	rec, _ := get(t, health.Readyz(time.Second, map[string]health.Check{"mysql": leaky}), "/readyz")
	for _, secret := range []string{"10.0.3.7", "hunter2", "dial tcp"} {
		if strings.Contains(rec.Body.String(), secret) {
			t.Fatalf("response body leaks %q: %s", secret, rec.Body.String())
		}
	}
}

func TestW7B3_ReadyzFailsASlowDependencyAfterTheTimeout(t *testing.T) {
	hang := func(ctx context.Context) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
			return nil
		}
	}
	start := time.Now()
	rec, body := get(t, health.Readyz(50*time.Millisecond, map[string]health.Check{"kafka": hang, "mysql": ok}), "/readyz")
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("readyz took %v, the per-check timeout must bound it", elapsed)
	}
	if rec.Code != http.StatusServiceUnavailable || len(body.Failing) != 1 || body.Failing[0] != "kafka" {
		t.Fatalf("status = %d failing = %v, want 503 [kafka]", rec.Code, body.Failing)
	}
}

func TestW7B3_ReadyzRunsChecksConcurrently(t *testing.T) {
	slow := func(context.Context) error { time.Sleep(150 * time.Millisecond); return nil }
	h := health.Readyz(time.Second, map[string]health.Check{"a": slow, "b": slow, "c": slow})
	start := time.Now()
	rec, _ := get(t, h, "/readyz")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if elapsed := time.Since(start); elapsed > 320*time.Millisecond {
		t.Fatalf("three 150ms checks took %v, they must run concurrently", elapsed)
	}
}

func TestW7B3_HealthzNeverCallsDependencyChecks(t *testing.T) {
	var calls atomic.Int32
	counted := func(context.Context) error { calls.Add(1); return nil }
	h := health.Readyz(time.Second, map[string]health.Check{"mysql": counted})
	get(t, health.Healthz(), "/healthz")
	if calls.Load() != 0 {
		t.Fatalf("healthz ran %d dependency checks, it must be cheap", calls.Load())
	}
	get(t, h, "/readyz")
	if calls.Load() != 1 {
		t.Fatalf("readyz ran %d checks, want 1", calls.Load())
	}
}
