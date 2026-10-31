package authmw_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/yashdagar/zomato-prep/backend/internal/auth"
)

func TestW3B2_Middleware(t *testing.T) {
	s := auth.NewSigner("test-secret", time.Minute)
	tok, _ := s.Issue(7, "rider", time.Now())
	h := s.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := auth.From(r.Context())
		if !ok || p.UserID != 7 {
			t.Errorf("principal missing in context: %+v %v", p, ok)
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/v1/me", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("with token: %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/v1/me", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("without token: %d", rec.Code)
	}
}

func TestW3B2_BadCredentialsGetTheSameUnauthorizedAnswer(t *testing.T) {
	s := auth.NewSigner("test-secret", time.Minute)
	expired, _ := s.Issue(7, "customer", time.Now().Add(-2*time.Minute))
	foreign, _ := auth.NewSigner("other-secret", time.Minute).Issue(7, "customer", time.Now())
	valid, _ := s.Issue(7, "customer", time.Now())
	headers := map[string]string{
		"basic scheme":     "Basic " + valid,
		"empty bearer":     "Bearer ",
		"not a jwt":        "Bearer not-a-jwt",
		"expired token":    "Bearer " + expired,
		"other secret":     "Bearer " + foreign,
		"lowercase bearer": "bearer" + valid,
	}
	for name, header := range headers {
		t.Run(name, func(t *testing.T) {
			reached := false
			h := s.Middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true }))
			rec := httptest.NewRecorder()
			req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/v1/me", nil)
			req.Header.Set("Authorization", header)
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusUnauthorized || reached {
				t.Fatalf("status %d, handler reached %v; want 401 and the handler never called", rec.Code, reached)
			}
			var body map[string]string
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body["error"] != "unauthorized" {
				t.Fatalf("body = %s, want {\"error\":\"unauthorized\"}", rec.Body)
			}
		})
	}
}

func TestW3B2_FromReportsFalseWithoutAPrincipal(t *testing.T) {
	if p, ok := auth.From(context.Background()); ok || p.UserID != 0 {
		t.Fatalf("From(empty ctx) = %+v, %v; want zero value and false", p, ok)
	}
}
