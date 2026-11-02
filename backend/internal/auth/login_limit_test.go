//go:build w3o3

package auth_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/yashdagar/zomato-prep/backend/internal/auth"
	"github.com/yashdagar/zomato-prep/backend/internal/ratelimit"
)

type loginProbe struct {
	h       http.Handler
	reached int
	body    string
}

func newLoginProbe(ipLimit, emailLimit int) *loginProbe {
	p := &loginProbe{}
	now := func() time.Time { return time.Unix(1_700_000_000, 0) }
	byIP := ratelimit.NewFixedWindow(ipLimit, time.Minute, now)
	byEmail := ratelimit.NewFixedWindow(emailLimit, time.Minute, now)
	p.h = auth.LoginRateLimit(byIP, byEmail)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.reached++
		b, _ := io.ReadAll(r.Body)
		p.body = string(b)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	return p
}

func (p *loginProbe) login(t *testing.T, ip, email string, extra ...string) *httptest.ResponseRecorder {
	t.Helper()
	body := `{"email":` + strconv.Quote(email) + `,"password":"x"}`
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/login", strings.NewReader(body))
	req.RemoteAddr = ip + ":5555"
	for i := 0; i+1 < len(extra); i += 2 {
		req.Header.Set(extra[i], extra[i+1])
	}
	rec := httptest.NewRecorder()
	p.h.ServeHTTP(rec, req)
	return rec
}

func TestW3O3_SixthLoginFromOneIPIsRejectedWith429(t *testing.T) {
	p := newLoginProbe(5, 100)
	for i := 1; i <= 5; i++ {
		if rec := p.login(t, "10.0.0.1", "u"+strconv.Itoa(i)+"@x.com"); rec.Code != http.StatusUnauthorized {
			t.Fatalf("login %d status = %d, want the handler's 401", i, rec.Code)
		}
	}
	if rec := p.login(t, "10.0.0.1", "u6@x.com"); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("login 6 status = %d, want 429", rec.Code)
	}
}

func TestW3O3_RejectionCarriesRetryAfterAndJSONBody(t *testing.T) {
	p := newLoginProbe(1, 100)
	p.login(t, "10.0.0.1", "a@x.com")
	rec := p.login(t, "10.0.0.1", "b@x.com")
	if rec.Header().Get("Retry-After") == "" {
		t.Fatal("Retry-After header missing")
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body["error"] != "rate_limited" {
		t.Fatalf("body = %s, want {\"error\":\"rate_limited\"}", rec.Body)
	}
}

func TestW3O3_RejectedLoginNeverReachesTheHandler(t *testing.T) {
	p := newLoginProbe(2, 100)
	for i := 0; i < 5; i++ {
		p.login(t, "10.0.0.1", "a@x.com")
	}
	if p.reached != 2 {
		t.Fatalf("handler reached %d times, want 2", p.reached)
	}
}

func TestW3O3_OtherIPsAreNotAffectedByOneIPsLimit(t *testing.T) {
	p := newLoginProbe(1, 100)
	p.login(t, "10.0.0.1", "a@x.com")
	if rec := p.login(t, "10.0.0.2", "b@x.com"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("second IP status = %d, want 401 from the handler", rec.Code)
	}
}

func TestW3O3_OneEmailFromManyIPsIsLimitedByTheEmailKey(t *testing.T) {
	p := newLoginProbe(100, 3)
	for i := 1; i <= 3; i++ {
		p.login(t, "10.0.0."+strconv.Itoa(i), "victim@x.com")
	}
	if rec := p.login(t, "10.0.0.9", "victim@x.com"); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("4th attempt on one email from a fresh IP = %d, want 429", rec.Code)
	}
	if rec := p.login(t, "10.0.0.9", "someone-else@x.com"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("another email from that IP = %d, want 401 from the handler", rec.Code)
	}
}

func TestW3O3_EmailKeyIgnoresCaseAndSurroundingSpaces(t *testing.T) {
	p := newLoginProbe(100, 2)
	p.login(t, "10.0.0.1", "Victim@X.com")
	p.login(t, "10.0.0.2", "  victim@x.com ")
	if rec := p.login(t, "10.0.0.3", "VICTIM@x.COM"); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("3rd spelling of one email = %d, want 429", rec.Code)
	}
}

func TestW3O3_ForgedUserIDHeaderCannotDodgeTheLimit(t *testing.T) {
	p := newLoginProbe(2, 100)
	p.login(t, "10.0.0.1", "a@x.com", "X-User-ID", "1")
	p.login(t, "10.0.0.1", "b@x.com", "X-User-ID", "2")
	if rec := p.login(t, "10.0.0.1", "c@x.com", "X-User-ID", "3"); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("3rd login with a fresh forged X-User-ID = %d, want 429", rec.Code)
	}
}

func TestW3O3_HandlerStillReadsTheFullBody(t *testing.T) {
	p := newLoginProbe(5, 5)
	p.login(t, "10.0.0.1", "a@x.com")
	if !strings.Contains(p.body, `"email":"a@x.com"`) || !strings.Contains(p.body, `"password":"x"`) {
		t.Fatalf("handler saw body %q, want the original JSON", p.body)
	}
}

func TestW3O3_MalformedBodyStillReachesTheHandlerForItsOwn400(t *testing.T) {
	p := newLoginProbe(5, 5)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/login", strings.NewReader("not json"))
	req.RemoteAddr = "10.0.0.1:5555"
	p.h.ServeHTTP(httptest.NewRecorder(), req)
	if p.reached != 1 || p.body != "not json" {
		t.Fatalf("reached=%d body=%q, want the handler to see the untouched malformed body", p.reached, p.body)
	}
}
