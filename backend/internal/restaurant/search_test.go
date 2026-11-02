//go:build w2o2

package restaurant_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"

	"github.com/yashdagar/zomato-prep/backend/internal/cursor"
	"github.com/yashdagar/zomato-prep/backend/internal/db"
	"github.com/yashdagar/zomato-prep/backend/internal/restaurant"
)

const o2Lat, o2Lng = 28.46, 77.03

type o2Searcher interface {
	SearchRestaurantsNear(ctx context.Context, arg db.SearchRestaurantsNearParams) ([]db.SearchRestaurantsNearRow, error)
}

func o2FreshDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("set TEST_MYSQL_DSN=root:dev@tcp(127.0.0.1:3306)/zomato?parseTime=true to run")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	admin, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close() })

	name := fmt.Sprintf("zomato_it_%d", time.Now().UnixNano())
	if _, err = admin.ExecContext(t.Context(), "CREATE DATABASE "+name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = admin.ExecContext(context.Background(), "DROP DATABASE "+name) })

	cfg.DBName = name
	cfg.MultiStatements = true
	conn, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	files, err := filepath.Glob("../../db/migrations/*.up.sql")
	if err != nil || len(files) == 0 {
		t.Fatalf("no migrations found: %v", err)
	}
	sort.Strings(files)
	for _, f := range files {
		src, err := os.ReadFile(filepath.Clean(f))
		if err != nil {
			t.Fatal(err)
		}
		if _, err = conn.ExecContext(t.Context(), string(src)); err != nil {
			t.Fatalf("%s: %v", filepath.Base(f), err)
		}
	}
	return conn
}

func o2Insert(t *testing.T, conn *sql.DB, name string, dLat float64, active bool) int64 {
	t.Helper()
	res, err := conn.ExecContext(t.Context(),
		"INSERT INTO restaurants (name, lat, lng, is_active) VALUES (?, ?, ?, ?)",
		name, fmt.Sprintf("%.6f", o2Lat+dLat), fmt.Sprintf("%.6f", o2Lng), active)
	if err != nil {
		t.Fatal(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func o2Service(conn *sql.DB) *restaurant.Service {
	q := db.New(conn)
	return restaurant.NewService(q).WithSearch(q)
}

func o2Names(items []restaurant.Restaurant) []string {
	names := make([]string, len(items))
	for i, r := range items {
		names[i] = r.Name
	}
	return names
}

func o2Search(t *testing.T, svc *restaurant.Service, q string, limit int) []restaurant.Restaurant {
	t.Helper()
	items, _, err := svc.List(t.Context(), restaurant.Near{Lat: o2Lat, Lng: o2Lng, RadiusKm: 5, Limit: limit, Query: q})
	if err != nil {
		t.Fatalf("List with q=%q: %v", q, err)
	}
	return items
}

func o2Seed(t *testing.T, conn *sql.DB) {
	t.Helper()
	o2Insert(t, conn, "Pizza Palace", 0.001, true)
	o2Insert(t, conn, "Pizzeria Roma", 0.002, true)
	o2Insert(t, conn, "Burger Barn", 0.003, true)
	o2Insert(t, conn, "Dominos Pizza", 0.004, true)
	o2Insert(t, conn, "Pasta House", 0.005, true)
	o2Insert(t, conn, "Pizza Closed", 0.006, false)
	o2Insert(t, conn, "Pizza Faraway", 1, true)
}

func TestW2O2_GeneratedSearchQueryHasTheExpectedSignature(t *testing.T) {
	var _ o2Searcher = db.New(nil)
}

func TestW2O2_MigrationsCreateAFulltextIndexOnName(t *testing.T) {
	conn := o2FreshDB(t)
	var cols string
	err := conn.QueryRowContext(t.Context(), `
		SELECT GROUP_CONCAT(column_name ORDER BY seq_in_index)
		FROM information_schema.statistics
		WHERE table_schema = DATABASE() AND table_name = 'restaurants' AND index_type = 'FULLTEXT'`).Scan(&cols)
	if err != nil {
		t.Fatal(err)
	}
	if cols != "name" {
		t.Fatalf("FULLTEXT index columns = %q, want name", cols)
	}
}

func TestW2O2_SearchMatchesWholeWords(t *testing.T) {
	conn := o2FreshDB(t)
	o2Seed(t, conn)
	got := o2Names(o2Search(t, o2Service(conn), "pizza", 20))
	if want := []string{"Pizza Palace", "Dominos Pizza"}; !slices.Equal(got, want) {
		t.Fatalf("q=pizza returned %v, want %v", got, want)
	}
}

func TestW2O2_SearchMatchesWordPrefixes(t *testing.T) {
	conn := o2FreshDB(t)
	o2Seed(t, conn)
	got := o2Names(o2Search(t, o2Service(conn), "piz", 20))
	if want := []string{"Pizza Palace", "Pizzeria Roma", "Dominos Pizza"}; !slices.Equal(got, want) {
		t.Fatalf("q=piz returned %v, want %v", got, want)
	}
}

func TestW2O2_SearchIsCaseInsensitive(t *testing.T) {
	conn := o2FreshDB(t)
	o2Seed(t, conn)
	got := o2Names(o2Search(t, o2Service(conn), "PIZZA", 20))
	if want := []string{"Pizza Palace", "Dominos Pizza"}; !slices.Equal(got, want) {
		t.Fatalf("q=PIZZA returned %v, want %v", got, want)
	}
}

func TestW2O2_EveryWordMustMatch(t *testing.T) {
	conn := o2FreshDB(t)
	o2Seed(t, conn)
	got := o2Names(o2Search(t, o2Service(conn), "pizza palace", 20))
	if want := []string{"Pizza Palace"}; !slices.Equal(got, want) {
		t.Fatalf("q=%q returned %v, want %v", "pizza palace", got, want)
	}
}

func TestW2O2_SearchStillAppliesTheGeoBoxAndTheActiveFlag(t *testing.T) {
	conn := o2FreshDB(t)
	o2Seed(t, conn)
	for _, r := range o2Search(t, o2Service(conn), "pizza", 20) {
		if r.Name == "Pizza Closed" || r.Name == "Pizza Faraway" {
			t.Fatalf("result %q is inactive or outside the radius", r.Name)
		}
	}
}

func TestW2O2_BooleanOperatorsInTheQueryAreTreatedAsPlainText(t *testing.T) {
	conn := o2FreshDB(t)
	o2Seed(t, conn)
	svc := o2Service(conn)
	for _, q := range []string{`"piz`, `pizza -palace`, `+pizza*`, `(pizza)`, `pizza ~roma @2`} {
		items, _, err := svc.List(t.Context(), restaurant.Near{Lat: o2Lat, Lng: o2Lng, RadiusKm: 5, Limit: 20, Query: q})
		if err != nil {
			t.Fatalf("q=%q: %v", q, err)
		}
		if q == `"piz` && len(items) != 3 {
			t.Fatalf("q=%q returned %v, want the three restaurants starting with piz", q, o2Names(items))
		}
		if q == `pizza -palace` && slices.Contains(o2Names(items), "Burger Barn") {
			t.Fatalf("q=%q treated - as an exclusion operator", q)
		}
	}
}

func TestW2O2_KeysetPaginationIsPreservedWhileSearching(t *testing.T) {
	conn := o2FreshDB(t)
	var want []int64
	for i := range 7 {
		want = append(want, o2Insert(t, conn, fmt.Sprintf("Pizza Branch %d", i), float64(i)*0.001, true))
		o2Insert(t, conn, fmt.Sprintf("Noodle Branch %d", i), float64(i)*0.001, true)
	}
	svc := o2Service(conn)
	n := restaurant.Near{Lat: o2Lat, Lng: o2Lng, RadiusKm: 5, Limit: 3, Query: "pizza"}
	var got []int64
	var sizes []int
	for range 20 {
		items, next, err := svc.List(t.Context(), n)
		if err != nil {
			t.Fatal(err)
		}
		sizes = append(sizes, len(items))
		for _, r := range items {
			got = append(got, r.ID)
		}
		if next == "" {
			break
		}
		if n.After, err = cursor.Decode(next); err != nil {
			t.Fatalf("undecodable cursor %q: %v", next, err)
		}
	}
	if !slices.Equal(got, want) {
		t.Fatalf("walk returned %v, want every pizza restaurant once in id order %v", got, want)
	}
	if !slices.Equal(sizes, []int{3, 3, 1}) {
		t.Fatalf("page sizes %v, want [3 3 1]", sizes)
	}
}

func TestW2O2_ListEndpointFiltersByQ(t *testing.T) {
	conn := o2FreshDB(t)
	o2Seed(t, conn)
	h := restaurant.NewHandler(o2Service(conn))
	get := func(q url.Values) (int, string) {
		q.Set("lat", "28.46")
		q.Set("lng", "77.03")
		rec := httptest.NewRecorder()
		h.List(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/restaurants?"+q.Encode(), nil))
		return rec.Code, rec.Body.String()
	}

	code, body := get(url.Values{"q": {"piz"}})
	if code != http.StatusOK {
		t.Fatalf("q=piz: %d %s", code, body)
	}
	var p struct {
		Items []struct {
			Name string `json:"name"`
		} `json:"items"`
		NextCursor string `json:"next_cursor"`
	}
	if err := json.Unmarshal([]byte(body), &p); err != nil {
		t.Fatal(err)
	}
	if len(p.Items) != 3 || p.NextCursor != "" {
		t.Fatalf("q=piz: %s, want 3 items and no next_cursor", body)
	}

	code, body = get(url.Values{})
	if code != http.StatusOK || !strings.Contains(body, "Burger Barn") {
		t.Fatalf("without q every active nearby restaurant should be listed: %d %s", code, body)
	}
	for _, bad := range []string{"+-()", "\"\"", strings.Repeat("a", 101)} {
		if code, body := get(url.Values{"q": {bad}}); code != http.StatusBadRequest {
			t.Fatalf("q=%q: %d %s, want 400", bad, code, body)
		}
	}
}
