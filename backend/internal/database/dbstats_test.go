package database_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yashdagar/zomato-prep/backend/internal/database"
	"github.com/yashdagar/zomato-prep/backend/internal/metrics"
)

func TestW6B5_DBPoolStatsAreExported(t *testing.T) {
	t.Setenv("DB_MAX_OPEN_CONNS", "9")
	db, err := database.Open(t.Context(), dsn(t))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	metrics.RegisterDBStats(db)
	rec := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := rec.Body.String()
	if !strings.Contains(body, "\ndb_connections_max_open 9\n") {
		t.Fatalf("db_connections_max_open 9 missing from /metrics:\n%s", body)
	}
	for _, name := range []string{"db_connections_open ", "db_connections_in_use ", "db_connection_waits_total "} {
		if !strings.Contains(body, "\n"+name) {
			t.Fatalf("%s missing from /metrics", name)
		}
	}
}
