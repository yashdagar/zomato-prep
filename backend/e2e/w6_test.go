package e2e_test

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
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
	placeOrder(t, cust, rest, m, fmt.Sprintf("s-%d", time.Now().UnixNano()))
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

type stockMenu struct {
	Items []struct {
		ID      int64 `json:"id"`
		InStock bool  `json:"in_stock"`
	} `json:"items"`
}

func menuPath(restaurantID int64) string {
	return fmt.Sprintf("/v1/restaurants/%d/menu", restaurantID)
}

func stockPath(restaurantID, itemID int64) string {
	return fmt.Sprintf("/v1/admin/restaurants/%d/items/%d", restaurantID, itemID)
}

func TestW6B1_SecondMenuReadIsACacheHit(t *testing.T) {
	c := newClient(t)
	rest, _ := firstRestaurant(t, c)
	c.get(menuPath(rest.ID))
	r := c.get(menuPath(rest.ID))
	if r.Status != http.StatusOK || r.Header.Get("X-Cache") != "HIT" {
		t.Fatalf("second menu read: %d X-Cache=%q, want 200 and HIT", r.Status, r.Header.Get("X-Cache"))
	}
}

func TestW6B1_StockUpdateInvalidatesTheCachedMenu(t *testing.T) {
	cust, _ := signup(t, "customer", 0)
	rest, m := firstRestaurant(t, cust)
	staff, _ := signup(t, "restaurant", rest.ID)
	itemID := m.Items[0].ID
	cust.get(menuPath(rest.ID))
	if r := cust.get(menuPath(rest.ID)); r.Header.Get("X-Cache") != "HIT" {
		t.Fatalf("menu not cached before the update: X-Cache=%q", r.Header.Get("X-Cache"))
	}
	t.Cleanup(func() { staff.do(http.MethodPut, stockPath(rest.ID, itemID), map[string]bool{"in_stock": true}) })
	if r := staff.do(http.MethodPut, stockPath(rest.ID, itemID), map[string]bool{"in_stock": false}); r.Status != http.StatusOK && r.Status != http.StatusNoContent {
		t.Fatalf("stock update: %d %s", r.Status, r.Body)
	}
	r := cust.get(menuPath(rest.ID))
	if r.Header.Get("X-Cache") == "HIT" {
		t.Fatal("menu after a stock update was served from cache, the key must be deleted on write")
	}
	var sm stockMenu
	r.JSON(t, &sm)
	for _, it := range sm.Items {
		if it.ID == itemID && it.InStock {
			t.Fatal("menu still shows the item in stock after it was marked out of stock")
		}
	}
	if r := cust.get(menuPath(rest.ID)); r.Header.Get("X-Cache") != "HIT" {
		t.Fatalf("the rebuilt menu should be cached again, X-Cache=%q", r.Header.Get("X-Cache"))
	}
}

func TestW6B1_OnlyThatRestaurantsStaffCanChangeStock(t *testing.T) {
	cust, _ := signup(t, "customer", 0)
	rest, m := firstRestaurant(t, cust)
	body := map[string]bool{"in_stock": true}
	if r := cust.do(http.MethodPut, stockPath(rest.ID, m.Items[0].ID), body); r.Status != http.StatusForbidden {
		t.Fatalf("customer changing stock: %d, want 403", r.Status)
	}
	other := int64(0)
	lr := cust.get("/v1/restaurants?lat=28.46&lng=77.03&radius_km=5&limit=5")
	var p page[restaurant]
	lr.JSON(t, &p)
	for _, x := range p.Items {
		if x.ID != rest.ID {
			other = x.ID
			break
		}
	}
	if other == 0 {
		t.Fatal("need a second restaurant near the seed point")
	}
	staff, _ := signup(t, "restaurant", other)
	if r := staff.do(http.MethodPut, stockPath(rest.ID, m.Items[0].ID), body); r.Status != http.StatusForbidden {
		t.Fatalf("another restaurant's staff changing stock: %d, want 403", r.Status)
	}
	owner, _ := signup(t, "restaurant", rest.ID)
	if r := owner.do(http.MethodPut, stockPath(rest.ID, 999999999), body); r.Status != http.StatusNotFound {
		t.Fatalf("unknown item: %d, want 404", r.Status)
	}
}

func TestW6B1_OnlyTheFirstListPageIsCached(t *testing.T) {
	c := newClient(t)
	const q = "/v1/restaurants?lat=28.46&lng=77.03&radius_km=5&limit=2"
	c.get(q)
	first := c.get(q)
	if first.Header.Get("X-Cache") != "HIT" {
		t.Fatalf("second read of the first page: X-Cache=%q, want HIT", first.Header.Get("X-Cache"))
	}
	var p page[restaurant]
	first.JSON(t, &p)
	if p.NextCursor == "" {
		t.Fatal("need a second page to check")
	}
	next := q + "&cursor=" + p.NextCursor
	c.get(next)
	if r := c.get(next); r.Header.Get("X-Cache") == "HIT" {
		t.Fatal("a cursor page was served from cache, only the first page is cached")
	}
}

func metricValue(t *testing.T, c *client, series string) float64 {
	t.Helper()
	r := c.get("/metrics")
	if r.Status != http.StatusOK {
		t.Fatalf("metrics: %d", r.Status)
	}
	for _, line := range strings.Split(string(r.Body), "\n") {
		if strings.HasPrefix(line, series+" ") {
			v, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimPrefix(line, series+" ")), 64)
			if err != nil {
				t.Fatalf("parse %q: %v", line, err)
			}
			return v
		}
	}
	return 0
}

func TestW6B2_RouteLabelIsThePatternNotTheRawPath(t *testing.T) {
	c := newClient(t)
	rest, _ := firstRestaurant(t, c)
	c.get(menuPath(rest.ID))
	c.get("/v1/definitely-not-a-route-" + fmt.Sprint(time.Now().UnixNano()))
	body := string(c.get("/metrics").Body)
	if !strings.Contains(body, `route="/v1/restaurants/{id}/menu"`) {
		t.Fatal("http_request_duration_seconds has no route label with the chi pattern /v1/restaurants/{id}/menu")
	}
	if strings.Contains(body, menuPath(rest.ID)) || strings.Contains(body, "definitely-not-a-route") {
		t.Fatal("raw paths leaked into metric labels, label by route pattern to bound cardinality")
	}
}

func TestW6B2_OrdersPlacedCounterGoesUp(t *testing.T) {
	cust, _ := signup(t, "customer", 0)
	rest, m := firstRestaurant(t, cust)
	before := metricValue(t, cust, "orders_placed_total")
	placeOrder(t, cust, rest, m, fmt.Sprintf("m-%d", time.Now().UnixNano()))
	if after := metricValue(t, cust, "orders_placed_total"); after != before+1 {
		t.Fatalf("orders_placed_total went %v -> %v, want +1", before, after)
	}
}

func TestW6B2_CacheHitsAndMissesAreCounted(t *testing.T) {
	c := newClient(t)
	rest, _ := firstRestaurant(t, c)
	c.get(menuPath(rest.ID))
	const hit = `cache_requests_total{result="hit"}`
	before := metricValue(t, c, hit)
	c.get(menuPath(rest.ID))
	if after := metricValue(t, c, hit); after <= before {
		t.Fatalf("%s went %v -> %v after a cached read", hit, before, after)
	}
}

func TestW6B2_PprofListensOnlyOnThePrivatePort(t *testing.T) {
	addr := os.Getenv("TEST_PPROF_ADDR")
	if addr == "" {
		addr = "127.0.0.1:6060"
	}
	res, err := http.Get("http://" + addr + "/debug/pprof/")
	if err != nil {
		t.Fatalf("pprof on %s: %v", addr, err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("pprof index: %d, want 200", res.StatusCode)
	}
	if r := newClient(t).get("/debug/pprof/"); r.Status != http.StatusNotFound {
		t.Fatalf("pprof on the public port: %d, want 404", r.Status)
	}
}

func TestW6B2_WorkerExposesConsumerLag(t *testing.T) {
	u := os.Getenv("TEST_WORKER_URL")
	if u == "" {
		t.Skip("set TEST_WORKER_URL=http://localhost:8081 to run")
	}
	deadline := time.Now().Add(20 * time.Second)
	for {
		res, err := http.Get(strings.TrimRight(u, "/") + "/metrics")
		if err == nil {
			b, _ := io.ReadAll(res.Body)
			_ = res.Body.Close()
			if strings.Contains(string(b), "kafka_consumer_lag{") {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("worker /metrics never showed kafka_consumer_lag")
		}
		time.Sleep(time.Second)
	}
}

func TestW6B3_ClientRequestIDIsEchoed(t *testing.T) {
	r := newClient(t).get("/healthz", "X-Request-Id", "w6b3-echo-123")
	if got := r.Header.Get("X-Request-Id"); got != "w6b3-echo-123" {
		t.Fatalf("X-Request-Id = %q, want the id the client sent", got)
	}
}

func TestW6B7_MinutesOutsideTheWindowIsRejected(t *testing.T) {
	cust, _ := signup(t, "customer", 0)
	rest, _ := firstRestaurant(t, cust)
	staff, _ := signup(t, "restaurant", rest.ID)
	for _, q := range []string{"0", "31", "-1", "abc"} {
		if r := staff.get("/v1/admin/stats?minutes=" + q); r.Status != http.StatusBadRequest {
			t.Fatalf("minutes=%s: %d, want 400", q, r.Status)
		}
	}
	if r := staff.get("/v1/admin/stats?minutes=30"); r.Status != http.StatusOK {
		t.Fatalf("minutes=30: %d, want 200", r.Status)
	}
}

func goroutineCount(t *testing.T) int {
	t.Helper()
	addr := os.Getenv("TEST_PPROF_ADDR")
	if addr == "" {
		addr = "127.0.0.1:6060"
	}
	res, err := http.Get("http://" + addr + "/debug/pprof/goroutine?debug=1")
	if err != nil {
		t.Fatalf("pprof: %v", err)
	}
	defer func() { _ = res.Body.Close() }()
	b, _ := io.ReadAll(res.Body)
	first, _, _ := strings.Cut(string(b), "\n")
	var n int
	if _, err := fmt.Sscanf(first, "goroutine profile: total %d", &n); err != nil {
		t.Fatalf("parse %q: %v", first, err)
	}
	return n
}

func TestW6B6_GoroutinesReturnToBaselineAfterABurst(t *testing.T) {
	c := newClient(t)
	rest, _ := firstRestaurant(t, c)
	c.get(menuPath(rest.ID))
	base := goroutineCount(t)
	var wg sync.WaitGroup
	for i := 0; i < 150; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cc := newClient(t)
			cc.get(menuPath(rest.ID))
			cc.get("/v1/restaurants?lat=28.46&lng=77.03&radius_km=5&limit=5")
		}()
	}
	wg.Wait()
	if !eventually(t, 15*time.Second, 500*time.Millisecond, func() bool { return goroutineCount(t) <= base+15 }) {
		t.Fatalf("goroutines %d after the burst, baseline %d: something leaks", goroutineCount(t), base)
	}
}
