//go:build w2o3

package restaurant_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"

	"github.com/yashdagar/zomato-prep/backend/internal/cursor"
	"github.com/yashdagar/zomato-prep/backend/internal/db"
	"github.com/yashdagar/zomato-prep/backend/internal/restaurant"
)

const o3Lat, o3Lng = 28.46, 77.03

type o3Point struct {
	id       int64
	lat, lng float64
}

type o3Distancer interface {
	ListRestaurantsByDistance(ctx context.Context, arg db.ListRestaurantsByDistanceParams) ([]db.ListRestaurantsByDistanceRow, error)
}

func o3FreshDB(t *testing.T) *sql.DB {
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

func o3At(t *testing.T, conn *sql.DB, northKm, eastKm float64, active bool) o3Point {
	t.Helper()
	lat := math.Round((o3Lat+northKm/111.195)*1e6) / 1e6
	lng := math.Round((o3Lng+eastKm/(111.195*math.Cos(o3Lat*math.Pi/180)))*1e6) / 1e6
	res, err := conn.ExecContext(t.Context(),
		"INSERT INTO restaurants (name, lat, lng, is_active) VALUES (?, ?, ?, ?)",
		"Test restaurant", fmt.Sprintf("%.6f", lat), fmt.Sprintf("%.6f", lng), active)
	if err != nil {
		t.Fatal(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return o3Point{id: id, lat: lat, lng: lng}
}

func o3Metres(p o3Point) int64 {
	return int64(math.Round(restaurant.HaversineKm(o3Lat, o3Lng, p.lat, p.lng) * 1000))
}

func o3Service(conn *sql.DB) *restaurant.Service {
	q := db.New(conn)
	return restaurant.NewService(q).WithNearby(q)
}

func o3Seed(t *testing.T, conn *sql.DB) (want []o3Point, corner o3Point) {
	t.Helper()
	e := o3At(t, conn, 3, 0, true)
	g := o3At(t, conn, 0, 2.5, true)
	h := o3At(t, conn, 0, 2.5, true)
	a := o3At(t, conn, 0.5, 0, true)
	o3At(t, conn, 0.2, 0, false)
	b := o3At(t, conn, 0, 1, true)
	c := o3At(t, conn, -1.5, -1.5, true)
	corner = o3At(t, conn, 3.6, 3.6, true)
	o3At(t, conn, 0, 30, true)
	return []o3Point{a, b, c, g, h, e}, corner
}

func o3Walk(t *testing.T, svc *restaurant.Service, limit int) (got []restaurant.NearbyRestaurant, sizes []int) {
	t.Helper()
	q := restaurant.NearbyQuery{Lat: o3Lat, Lng: o3Lng, RadiusKm: 5, Limit: limit}
	for range 50 {
		items, next, err := svc.ListByDistance(t.Context(), q)
		if err != nil {
			t.Fatal(err)
		}
		sizes = append(sizes, len(items))
		got = append(got, items...)
		if next == "" {
			return got, sizes
		}
		if len(items) == 0 {
			t.Fatal("empty page still returned a cursor")
		}
		q.Cursor = next
	}
	t.Fatal("pagination did not finish after 50 pages")
	return nil, nil
}

func TestW2O3_GeneratedDistanceQueryHasTheExpectedSignature(t *testing.T) {
	var _ o3Distancer = db.New(nil)
}

func TestW2O3_MigrationsCreateACompositeIndexOnActiveLatLng(t *testing.T) {
	conn := o3FreshDB(t)
	rows, err := conn.QueryContext(t.Context(), `
		SELECT index_name, GROUP_CONCAT(column_name ORDER BY seq_in_index)
		FROM information_schema.statistics
		WHERE table_schema = DATABASE() AND table_name = 'restaurants' AND index_type = 'BTREE'
		GROUP BY index_name`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var all []string
	for rows.Next() {
		var name, cols string
		if err := rows.Scan(&name, &cols); err != nil {
			t.Fatal(err)
		}
		all = append(all, name+"("+cols+")")
		if cols == "is_active,lat,lng" {
			return
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	t.Fatalf("no index on (is_active, lat, lng); indexes are %v", all)
}

func TestW2O3_ResultsAreOrderedByDistanceWithIDAsTheTieBreaker(t *testing.T) {
	conn := o3FreshDB(t)
	want, _ := o3Seed(t, conn)

	got, _ := o3Walk(t, o3Service(conn), 50)

	ids := make([]int64, len(got))
	for i, r := range got {
		ids[i] = r.ID
	}
	wantIDs := make([]int64, len(want))
	for i, p := range want {
		wantIDs[i] = p.id
	}
	if !slices.Equal(ids, wantIDs) {
		t.Fatalf("ids = %v, want %v nearest first", ids, wantIDs)
	}
}

func TestW2O3_DistanceMetresMatchHaversine(t *testing.T) {
	conn := o3FreshDB(t)
	want, _ := o3Seed(t, conn)

	got, _ := o3Walk(t, o3Service(conn), 50)

	for i, r := range got {
		if d := r.DistanceM - o3Metres(want[i]); d < -2 || d > 2 {
			t.Fatalf("restaurant %d: distance_m = %d, want %d +- 2", r.ID, r.DistanceM, o3Metres(want[i]))
		}
	}
}

func TestW2O3_RadiusIsACircleNotTheBoundingBox(t *testing.T) {
	conn := o3FreshDB(t)
	_, corner := o3Seed(t, conn)

	got, _ := o3Walk(t, o3Service(conn), 50)

	for _, r := range got {
		if r.ID == corner.id {
			t.Fatalf("restaurant %d is %.2f km away, outside the 5 km radius, but inside its bounding box", corner.id, restaurant.HaversineKm(o3Lat, o3Lng, corner.lat, corner.lng))
		}
	}
	if len(got) != 6 {
		t.Fatalf("got %d restaurants, want the 6 active ones within 5 km", len(got))
	}
}

func TestW2O3_KeysetPaginationWalksTiesExactlyOnce(t *testing.T) {
	conn := o3FreshDB(t)
	want, _ := o3Seed(t, conn)

	for _, limit := range []int{1, 2, 4} {
		got, sizes := o3Walk(t, o3Service(conn), limit)
		if len(got) != len(want) {
			t.Fatalf("limit %d: walked %d restaurants (page sizes %v), want %d", limit, len(got), sizes, len(want))
		}
		for i, r := range got {
			if r.ID != want[i].id {
				t.Fatalf("limit %d: position %d is restaurant %d, want %d", limit, i, r.ID, want[i].id)
			}
		}
	}
}

func TestW2O3_MalformedCursorIsRejected(t *testing.T) {
	conn := o3FreshDB(t)
	svc := o3Service(conn)
	for _, bad := range []string{"!!!", "bm90LWEtY3Vyc29y", cursor.Encode(cursor.Key{ID: 3})} {
		_, _, err := svc.ListByDistance(t.Context(), restaurant.NearbyQuery{Lat: o3Lat, Lng: o3Lng, RadiusKm: 5, Limit: 5, Cursor: bad})
		if err == nil {
			t.Fatalf("cursor %q should be rejected", bad)
		}
	}
}

func TestW2O3_ListEndpointSortsByDistance(t *testing.T) {
	conn := o3FreshDB(t)
	want, _ := o3Seed(t, conn)
	h := restaurant.NewHandler(o3Service(conn))
	get := func(q url.Values) (int, string) {
		q.Set("lat", strconv.FormatFloat(o3Lat, 'f', 2, 64))
		q.Set("lng", strconv.FormatFloat(o3Lng, 'f', 2, 64))
		rec := httptest.NewRecorder()
		h.List(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/restaurants?"+q.Encode(), nil))
		return rec.Code, rec.Body.String()
	}

	var ids []int64
	cur := ""
	for range 10 {
		q := url.Values{"sort": {"distance"}, "radius_km": {"5"}, "limit": {"4"}}
		if cur != "" {
			q.Set("cursor", cur)
		}
		code, body := get(q)
		if code != http.StatusOK {
			t.Fatalf("status %d: %s", code, body)
		}
		var p struct {
			Items []struct {
				ID        int64 `json:"id"`
				DistanceM int64 `json:"distance_m"`
			} `json:"items"`
			NextCursor string `json:"next_cursor"`
		}
		if err := json.Unmarshal([]byte(body), &p); err != nil {
			t.Fatal(err)
		}
		for _, it := range p.Items {
			if it.DistanceM <= 0 {
				t.Fatalf("item %d has distance_m %d, want a positive number of metres", it.ID, it.DistanceM)
			}
			ids = append(ids, it.ID)
		}
		if p.NextCursor == "" {
			break
		}
		cur = p.NextCursor
	}
	wantIDs := make([]int64, len(want))
	for i, p := range want {
		wantIDs[i] = p.id
	}
	if !slices.Equal(ids, wantIDs) {
		t.Fatalf("ids = %v, want %v nearest first", ids, wantIDs)
	}

	for _, bad := range []url.Values{
		{"sort": {"price"}},
		{"sort": {"distance"}, "cursor": {"not-a-cursor!!"}},
	} {
		if code, body := get(bad); code != http.StatusBadRequest {
			t.Fatalf("%v: %d %s, want 400", bad, code, body)
		}
	}
}
