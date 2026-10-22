package e2e_test

import (
	"net/http"
	"strings"
	"testing"
)

func TestW6B2_MetricsExposeTheFourSignals(t *testing.T) {
	c := newClient(t)
	r := c.get("/metrics")
	if r.Status != http.StatusOK {
		t.Fatalf("metrics: %d", r.Status)
	}
	body := string(r.Body)
	for _, name := range []string{"http_request_duration_seconds", "orders_placed_total", "ws_connections"} {
		if !strings.Contains(body, name) {
			t.Fatalf("metric %s missing from /metrics", name)
		}
	}
}

func TestW6B3_EveryResponseCarriesARequestID(t *testing.T) {
	c := newClient(t)
	r := c.get("/healthz")
	if r.Header.Get("X-Request-Id") == "" {
		t.Fatal("X-Request-Id header missing; chi's RequestID middleware should set it")
	}
}

func TestW6B7_AdminStatsCountsRecentOrders(t *testing.T) {
	cust, _ := signup(t, "customer", 0)
	rest, m := firstRestaurant(t, cust)
	restUser, _ := signup(t, "restaurant", rest.ID)
	before := stats(t, restUser)
	placeOrder(t, cust, rest, m, "stats-"+t.Name()+rest.Name)
	after := stats(t, restUser)
	if after < before+1 {
		t.Fatalf("orders in the last 5 min went %d -> %d, want +1", before, after)
	}
	if r := cust.get("/v1/admin/stats?minutes=5"); r.Status != http.StatusForbidden {
		t.Fatalf("customer reading admin stats: %d, want 403", r.Status)
	}
}

func stats(t *testing.T, c *client) int {
	t.Helper()
	r := c.get("/v1/admin/stats?minutes=5")
	if r.Status != http.StatusOK {
		t.Fatalf("stats: %d %s", r.Status, r.Body)
	}
	var s struct {
		Orders int `json:"orders"`
	}
	r.JSON(t, &s)
	return s.Orders
}
