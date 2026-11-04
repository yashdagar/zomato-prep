package reqlog_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/yashdagar/zomato-prep/backend/internal/auth"
	"github.com/yashdagar/zomato-prep/backend/internal/reqlog"
)

func lines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if l == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatalf("log line is not JSON: %q", l)
		}
		out = append(out, m)
	}
	return out
}

func newRouter(buf *bytes.Buffer, inner http.HandlerFunc) (*chi.Mux, *auth.Signer) {
	logger := slog.New(slog.NewJSONHandler(buf, nil))
	signer := auth.NewSigner("0123456789abcdef0123456789abcdef", time.Minute)
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(reqlog.Middleware(logger))
	r.Group(func(r chi.Router) {
		r.Use(signer.Middleware)
		r.Get("/w6b3/private/{id}", inner)
	})
	r.Get("/w6b3/public/{id}", inner)
	return r, signer
}

func TestW6B3_ResponseCarriesTheRequestID(t *testing.T) {
	var buf bytes.Buffer
	r, _ := newRouter(&buf, func(http.ResponseWriter, *http.Request) {})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/w6b3/public/1", nil))
	if rec.Header().Get("X-Request-Id") == "" {
		t.Fatal("X-Request-Id response header missing")
	}
}

func TestW6B3_IncomingRequestIDIsEchoed(t *testing.T) {
	var buf bytes.Buffer
	r, _ := newRouter(&buf, func(http.ResponseWriter, *http.Request) {})
	req := httptest.NewRequest(http.MethodGet, "/w6b3/public/1", nil)
	req.Header.Set("X-Request-Id", "abc-123")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if got := rec.Header().Get("X-Request-Id"); got != "abc-123" {
		t.Fatalf("X-Request-Id = %q, want abc-123", got)
	}
}

func TestW6B3_EveryLogLineIsJSONWithTheRequestID(t *testing.T) {
	var buf bytes.Buffer
	r, _ := newRouter(&buf, func(_ http.ResponseWriter, req *http.Request) {
		reqlog.From(req.Context()).Info("inside the handler")
	})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/w6b3/public/1", nil))
	id := rec.Header().Get("X-Request-Id")
	got := lines(t, &buf)
	if len(got) < 2 {
		t.Fatalf("got %d log lines, want the handler line and the access line", len(got))
	}
	for _, l := range got {
		if l["request_id"] != id {
			t.Fatalf("line %v has request_id %v, want %s", l, l["request_id"], id)
		}
	}
}

func TestW6B3_AccessLineHasMethodRouteStatusAndDuration(t *testing.T) {
	var buf bytes.Buffer
	r, _ := newRouter(&buf, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusAccepted) })
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/w6b3/public/77", nil))
	var access map[string]any
	for _, l := range lines(t, &buf) {
		if l["msg"] == "request" {
			access = l
		}
	}
	if access == nil {
		t.Fatal(`no access line with msg "request"`)
	}
	if access["method"] != "GET" || access["route"] != "/w6b3/public/{id}" || access["status"] != float64(http.StatusAccepted) {
		t.Fatalf("access line = %v", access)
	}
	if _, ok := access["duration_ms"].(float64); !ok {
		t.Fatalf("duration_ms missing or not a number: %v", access)
	}
}

func TestW6B3_UserIDAppearsOnceTheCallerIsAuthenticated(t *testing.T) {
	var buf bytes.Buffer
	r, signer := newRouter(&buf, func(_ http.ResponseWriter, req *http.Request) {
		reqlog.From(req.Context()).Info("inside the handler")
	})
	token, err := signer.Issue(42, "customer", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/w6b3/private/1", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	r.ServeHTTP(httptest.NewRecorder(), req)
	for _, l := range lines(t, &buf) {
		if l["msg"] == "inside the handler" {
			if l["user_id"] != float64(42) {
				t.Fatalf("handler line = %v, want user_id 42", l)
			}
			return
		}
	}
	t.Fatal("handler line not logged")
}

func TestW6B3_FromWithoutMiddlewareStillReturnsALogger(t *testing.T) {
	if reqlog.From(httptest.NewRequest(http.MethodGet, "/", nil).Context()) == nil {
		t.Fatal("From returned nil without the middleware")
	}
}
