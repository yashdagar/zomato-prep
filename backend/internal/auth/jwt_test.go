package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestW3B1_IssueAndParse(t *testing.T) {
	s := NewSigner("test-secret", 15*time.Minute)
	tok, err := s.Issue(42, "customer", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.Parse(tok)
	if err != nil || p.UserID != 42 || p.Role != "customer" {
		t.Fatalf("Parse() = %+v, %v", p, err)
	}
}

func TestW3B1_ExpiredTokenIsRejected(t *testing.T) {
	s := NewSigner("test-secret", time.Minute)
	tok, _ := s.Issue(1, "customer", time.Now().Add(-2*time.Minute))
	if _, err := s.Parse(tok); err == nil {
		t.Fatal("expired token must fail")
	}
}

func TestW3B2_Middleware(t *testing.T) {
	s := NewSigner("test-secret", time.Minute)
	tok, _ := s.Issue(7, "rider", time.Now())
	h := s.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := From(r.Context())
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
