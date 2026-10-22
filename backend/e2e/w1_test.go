package e2e_test

import (
	"fmt"
	"net/http"
	"testing"
	"time"
)

func TestW1B6_HealthzReturns429AfterTheBurst(t *testing.T) {
	c := newClient(t)
	user := fmt.Sprintf("burst-%d", time.Now().UnixNano())
	var codes []int
	for i := 0; i < 8; i++ {
		codes = append(codes, c.get("/healthz", "X-User-ID", user).Status)
	}
	if codes[0] != http.StatusOK {
		t.Fatalf("first call = %d, want 200", codes[0])
	}
	limited := 0
	for _, s := range codes {
		if s == http.StatusTooManyRequests {
			limited++
		}
	}
	if limited == 0 {
		t.Fatalf("codes = %v, want at least one 429 within 8 calls (capacity 5)", codes)
	}
	if codes[len(codes)-1] != http.StatusTooManyRequests {
		t.Fatalf("codes = %v, want the last call rejected", codes)
	}
}

func TestW1D3_LimitRecoversAfterASecond(t *testing.T) {
	c := newClient(t)
	user := fmt.Sprintf("recover-%d", time.Now().UnixNano())
	for i := 0; i < 8; i++ {
		c.get("/healthz", "X-User-ID", user)
	}
	if r := c.get("/healthz", "X-User-ID", user); r.Status != http.StatusTooManyRequests {
		t.Fatalf("expected 429 right after the burst, got %d", r.Status)
	}
	time.Sleep(1200 * time.Millisecond)
	if r := c.get("/healthz", "X-User-ID", user); r.Status != http.StatusOK {
		t.Fatalf("after 1.2 s: %d, want 200 (refill 1/s)", r.Status)
	}
}

func TestW1B5_RejectedBodyAndHeader(t *testing.T) {
	c := newClient(t)
	user := fmt.Sprintf("body-%d", time.Now().UnixNano())
	var r resp
	for i := 0; i < 8; i++ {
		r = c.get("/healthz", "X-User-ID", user)
	}
	if r.Status != http.StatusTooManyRequests {
		t.Fatalf("status = %d", r.Status)
	}
	if r.Header.Get("Retry-After") == "" {
		t.Fatal("Retry-After missing on 429")
	}
	var body map[string]string
	r.JSON(t, &body)
	if body["error"] != "rate_limited" {
		t.Fatalf("body = %s", r.Body)
	}
}
