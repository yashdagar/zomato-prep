package metrics_test

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/yashdagar/zomato-prep/backend/internal/metrics"
)

func scrape(t *testing.T) string {
	t.Helper()
	rec := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("metrics handler: %d", rec.Code)
	}
	return rec.Body.String()
}

func value(t *testing.T, body, series string) float64 {
	t.Helper()
	for _, line := range strings.Split(body, "\n") {
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

func serve(h http.Handler, method, path string) {
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(method, path, nil))
}

func TestW6B2_RequestDurationIsLabelledByRoutePattern(t *testing.T) {
	r := chi.NewRouter()
	r.Use(metrics.Middleware)
	r.Get("/w6b2/items/{id}", func(w http.ResponseWriter, _ *http.Request) {})
	serve(r, http.MethodGet, "/w6b2/items/11")
	serve(r, http.MethodGet, "/w6b2/items/12")
	body := scrape(t)
	series := `http_request_duration_seconds_count{method="GET",route="/w6b2/items/{id}",status="200"}`
	if got := value(t, body, series); got != 2 {
		t.Fatalf("%s = %v, want 2", series, got)
	}
	if strings.Contains(body, "/w6b2/items/11") || strings.Contains(body, "/w6b2/items/12") {
		t.Fatal("raw paths leaked into labels")
	}
}

func TestW6B2_RequestDurationRecordsTheStatus(t *testing.T) {
	r := chi.NewRouter()
	r.Use(metrics.Middleware)
	r.Get("/w6b2/teapot", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })
	serve(r, http.MethodGet, "/w6b2/teapot")
	series := `http_request_duration_seconds_count{method="GET",route="/w6b2/teapot",status="418"}`
	if got := value(t, scrape(t), series); got != 1 {
		t.Fatalf("%s = %v, want 1", series, got)
	}
}

func TestW6B2_UnmatchedPathsShareOneRouteLabel(t *testing.T) {
	r := chi.NewRouter()
	r.Use(metrics.Middleware)
	r.Get("/w6b2/known", func(http.ResponseWriter, *http.Request) {})
	serve(r, http.MethodGet, "/w6b2/attack-1")
	serve(r, http.MethodGet, "/w6b2/attack-2")
	body := scrape(t)
	if strings.Contains(body, "attack-") {
		t.Fatal("unmatched raw paths became label values")
	}
	series := `http_request_duration_seconds_count{method="GET",route="unmatched",status="404"}`
	if got := value(t, body, series); got < 2 {
		t.Fatalf("%s = %v, want at least 2", series, got)
	}
}

func TestW6B2_OrdersPlacedCounterIncrements(t *testing.T) {
	before := value(t, scrape(t), "orders_placed_total")
	metrics.OrdersPlaced.Inc()
	metrics.OrdersPlaced.Inc()
	if after := value(t, scrape(t), "orders_placed_total"); after != before+2 {
		t.Fatalf("orders_placed_total %v -> %v, want +2", before, after)
	}
}

func TestW6B2_WSConnectionsGaugeGoesUpAndDown(t *testing.T) {
	before := value(t, scrape(t), "ws_connections")
	metrics.WSConnections.Inc()
	metrics.WSConnections.Inc()
	if got := value(t, scrape(t), "ws_connections"); got != before+2 {
		t.Fatalf("ws_connections = %v, want %v", got, before+2)
	}
	metrics.WSConnections.Dec()
	if got := value(t, scrape(t), "ws_connections"); got != before+1 {
		t.Fatalf("ws_connections = %v, want %v", got, before+1)
	}
}

func TestW6B2_ConsumerLagIsPerGroupAndTopic(t *testing.T) {
	metrics.SetConsumerLag("w6b2-group", "w6b2-topic", 42)
	series := `kafka_consumer_lag{group="w6b2-group",topic="w6b2-topic"}`
	if got := value(t, scrape(t), series); got != 42 {
		t.Fatalf("%s = %v, want 42", series, got)
	}
	metrics.SetConsumerLag("w6b2-group", "w6b2-topic", 0)
	if got := value(t, scrape(t), series); got != 0 {
		t.Fatalf("%s = %v after the lag cleared, want 0", series, got)
	}
}

func TestW6B2_CacheRequestsAreSplitByResult(t *testing.T) {
	hit := `cache_requests_total{result="hit"}`
	miss := `cache_requests_total{result="miss"}`
	before := scrape(t)
	metrics.CacheRequests.WithLabelValues("hit").Inc()
	metrics.CacheRequests.WithLabelValues("miss").Inc()
	metrics.CacheRequests.WithLabelValues("miss").Inc()
	after := scrape(t)
	if value(t, after, hit) != value(t, before, hit)+1 || value(t, after, miss) != value(t, before, miss)+2 {
		t.Fatalf("hit/miss counters did not move by +1/+2:\n%s", after)
	}
}
