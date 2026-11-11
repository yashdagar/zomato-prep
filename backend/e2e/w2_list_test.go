package e2e_test

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

const (
	centreLat = 28.46
	centreLng = 77.03
)

type apiError struct {
	Error   string `json:"error"`
	Message string `json:"message"`
	Field   string `json:"field"`
}

func expectError(t *testing.T, r resp, status int, code, field string) {
	t.Helper()
	var e apiError
	if r.Status != status || json.Unmarshal(r.Body, &e) != nil {
		t.Fatalf("got %d %s, want %d with an error body", r.Status, r.Body, status)
	}
	if e.Error != code {
		t.Fatalf("error = %q in %s, want %q", e.Error, r.Body, code)
	}
	if strings.TrimSpace(e.Message) == "" {
		t.Fatalf("%s: the error body needs a message for people", r.Body)
	}
	if field != "" && e.Field != field {
		t.Fatalf("field = %q in %s, want %q", e.Field, r.Body, field)
	}
}

func listPath(lat, lng, radius float64, limit int, extra url.Values) string {
	v := url.Values{}
	v.Set("lat", fmt.Sprint(lat))
	v.Set("lng", fmt.Sprint(lng))
	if radius > 0 {
		v.Set("radius_km", fmt.Sprint(radius))
	}
	if limit != 0 {
		v.Set("limit", fmt.Sprint(limit))
	}
	for k, vs := range extra {
		for _, x := range vs {
			v.Add(k, x)
		}
	}
	return "/v1/restaurants?" + v.Encode()
}

type listed struct {
	ID        int64   `json:"id"`
	Name      string  `json:"name"`
	Lat       float64 `json:"lat"`
	Lng       float64 `json:"lng"`
	IsOpen    *bool   `json:"is_open"`
	DistanceM *int64  `json:"distance_m"`
}

// walk follows next_cursor from the first page to the last. afterPage runs
// after each page with its index, so a test can change data mid-walk.
func walk(t *testing.T, c *client, base string, afterPage func(page int, items []listed)) (ids []int64, pages [][]listed) {
	t.Helper()
	cursor := ""
	for n := 0; n < 500; n++ {
		path := base
		if cursor != "" {
			path += "&cursor=" + url.QueryEscape(cursor)
		}
		r := c.get(path)
		if r.Status != http.StatusOK {
			t.Fatalf("page %d %s: %d %s", n, path, r.Status, r.Body)
		}
		var p page[listed]
		r.JSON(t, &p)
		pages = append(pages, p.Items)
		for _, it := range p.Items {
			ids = append(ids, it.ID)
		}
		if afterPage != nil {
			afterPage(n, p.Items)
		}
		if p.NextCursor == "" {
			return ids, pages
		}
		cursor = p.NextCursor
	}
	t.Fatal("more than 500 pages; next_cursor never became empty")
	return nil, nil
}

func onceEach(t *testing.T, ids []int64) map[int64]bool {
	t.Helper()
	seen := map[int64]bool{}
	for _, id := range ids {
		if seen[id] {
			t.Fatalf("restaurant %d appeared twice in one walk", id)
		}
		seen[id] = true
	}
	return seen
}

// inBox lists active restaurants inside the box, leaving out any within 10 m
// of its edge, where rounding can reasonably go either way.
func inBox(t *testing.T, db *sql.DB, lat, lng, km float64, inside bool) map[int64]bool {
	t.Helper()
	margin := 0.01
	q := `SELECT id FROM restaurants WHERE is_active = 1
		AND ABS(lat - ?) * 111 <= ? AND ABS(lng - ?) * 111 * COS(RADIANS(?)) <= ?`
	args := []any{lat, km - margin, lng, lat, km - margin}
	if !inside {
		q = `SELECT id FROM restaurants WHERE is_active = 0
		   OR ABS(lat - ?) * 111 > ? OR ABS(lng - ?) * 111 * COS(RADIANS(?)) > ?`
		args = []any{lat, km + margin, lng, lat, km + margin}
	}
	rows, err := db.QueryContext(t.Context(), q, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	out := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		out[id] = true
	}
	return out
}

func TestW2B4_WalkingEveryPageReturnsEachNearbyRestaurantOnce(t *testing.T) {
	db := apiDB(t)
	ids, _ := walk(t, newClient(t), listPath(centreLat, centreLng, 5, 7, nil), nil)
	seen := onceEach(t, ids)
	for id := range inBox(t, db, centreLat, centreLng, 5, true) {
		if !seen[id] {
			t.Fatalf("restaurant %d is active and inside the 5 km box but the walk never returned it", id)
		}
	}
	for id := range inBox(t, db, centreLat, centreLng, 5, false) {
		if seen[id] {
			t.Fatalf("restaurant %d is inactive or outside the 5 km box but was returned", id)
		}
	}
}

func TestW2B4_PagesAreFullUntilTheLast(t *testing.T) {
	_, pages := walk(t, newClient(t), listPath(centreLat, centreLng, 5, 7, nil), nil)
	for i, p := range pages {
		if len(p) > 7 {
			t.Fatalf("page %d has %d items with limit=7", i, len(p))
		}
		if i < len(pages)-1 && len(p) != 7 {
			t.Fatalf("page %d has %d items but is not the last page; only the last page may be short", i, len(p))
		}
	}
}

func TestW2B4_LimitDefaultsToTwentyAndIsClampedToOneThroughFifty(t *testing.T) {
	c := newClient(t)
	cases := []struct {
		limit string
		want  int
	}{{"", 20}, {"0", 1}, {"-3", 1}, {"1", 1}, {"50", 50}, {"500", 50}}
	for _, tc := range cases {
		path := listPath(centreLat, centreLng, 5, 0, nil)
		if tc.limit != "" {
			path += "&limit=" + tc.limit
		}
		r := c.get(path)
		var p page[listed]
		r.JSON(t, &p)
		if r.Status != http.StatusOK || len(p.Items) != tc.want {
			t.Fatalf("limit=%q: %d with %d items, want 200 with %d", tc.limit, r.Status, len(p.Items), tc.want)
		}
	}
}

func TestW2B4_RadiusDefaultsToFiveKm(t *testing.T) {
	c := newClient(t)
	a, _ := walk(t, c, listPath(centreLat, centreLng, 0, 50, nil), nil)
	b, _ := walk(t, c, listPath(centreLat, centreLng, 5, 50, nil), nil)
	if fmt.Sprint(a) != fmt.Sprint(b) {
		t.Fatalf("without radius_km got %d restaurants, with radius_km=5 got %d; the default must be 5", len(a), len(b))
	}
}

func TestW2B4_EveryItemIsInsideTheRadiusBoxAndHasItsFields(t *testing.T) {
	r := newClient(t).get(listPath(centreLat, centreLng, 2, 50, nil))
	var p page[listed]
	r.JSON(t, &p)
	if r.Status != http.StatusOK || len(p.Items) == 0 {
		t.Fatalf("radius 2: %d %s, want some restaurants", r.Status, r.Body)
	}
	for _, it := range p.Items {
		if it.ID <= 0 || it.Name == "" {
			t.Fatalf("item %+v needs an id and a name", it)
		}
		if math.Abs(it.Lat-centreLat)*111 > 2.01 || math.Abs(it.Lng-centreLng)*111*math.Cos(centreLat*math.Pi/180) > 2.01 {
			t.Fatalf("restaurant %d at %.6f,%.6f is outside the 2 km box", it.ID, it.Lat, it.Lng)
		}
	}
}

func TestW2B4_InactiveAndDistantRestaurantsAreLeftOut(t *testing.T) {
	db := apiDB(t)
	inactive := insertRestaurant(t, db, "Closed down", centreLat, centreLng, false)
	distant := insertRestaurant(t, db, "Eight km north", centreLat+8.0/111, centreLng, true)
	ids, _ := walk(t, newClient(t), listPath(centreLat, centreLng, 5, 50, nil), nil)
	seen := onceEach(t, ids)
	if seen[inactive] {
		t.Fatal("an inactive restaurant at the centre was listed")
	}
	if seen[distant] {
		t.Fatal("a restaurant 8 km away was listed for radius_km=5")
	}
}

func TestW2B4_ChangesDuringAWalkNeitherSkipNorRepeatRestaurants(t *testing.T) {
	db := apiDB(t)
	c := newClient(t)
	base := listPath(centreLat, centreLng, 5, 5, nil)
	before, _ := walk(t, c, base, nil)
	var deactivated int64
	during, _ := walk(t, c, base, func(page int, items []listed) {
		if page == 1 {
			deactivated = items[0].ID
			mustExec(t, db, "UPDATE restaurants SET is_active = 0 WHERE id = ?", deactivated)
			t.Cleanup(func() { _, _ = db.Exec("UPDATE restaurants SET is_active = 1 WHERE id = ?", deactivated) })
			insertRestaurant(t, db, "Opened mid walk", centreLat, centreLng, true)
		}
	})
	seen := onceEach(t, during)
	for _, id := range before {
		if !seen[id] {
			t.Fatalf("restaurant %d was skipped after restaurant %d on an earlier page was deactivated; offset paging shifts rows, keyset paging does not", id, deactivated)
		}
	}
}

func TestW2B4_InvalidParametersAre400WithTheFieldNamed(t *testing.T) {
	c := newClient(t)
	cases := []struct{ query, field string }{
		{"lng=77.03", "lat"},
		{"lat=28.46", "lng"},
		{"lat=abc&lng=77.03", "lat"},
		{"lat=NaN&lng=77.03", "lat"},
		{"lat=28.46&lng=Inf", "lng"},
		{"lat=91&lng=77.03", "lat"},
		{"lat=-90.5&lng=77.03", "lat"},
		{"lat=28.46&lng=180.1", "lng"},
		{"lat=28.46&lng=77.03&limit=abc", "limit"},
		{"lat=28.46&lng=77.03&limit=2.5", "limit"},
		{"lat=28.46&lng=77.03&radius_km=-1", "radius_km"},
		{"lat=28.46&lng=77.03&radius_km=0", "radius_km"},
		{"lat=28.46&lng=77.03&radius_km=50.5", "radius_km"},
		{"lat=28.46&lng=77.03&radius_km=far", "radius_km"},
	}
	for _, tc := range cases {
		t.Run(tc.query, func(t *testing.T) {
			expectError(t, c.get("/v1/restaurants?"+tc.query), http.StatusBadRequest, "invalid_parameter", tc.field)
		})
	}
}

func TestW2B4_ACursorTheAPIDidNotIssueIs400(t *testing.T) {
	c := newClient(t)
	for _, cur := range []string{"not-base64!!", "AAAA", "%7B%7D", "eyJpZCI6Im9uZSJ9"} {
		expectError(t, c.get(listPath(centreLat, centreLng, 5, 5, nil)+"&cursor="+cur), http.StatusBadRequest, "invalid_cursor", "")
	}
}

func TestW2B4_NothingNearbyIsAnEmptyLastPage(t *testing.T) {
	r := newClient(t).get(listPath(0, 0, 1, 0, nil))
	var raw map[string]json.RawMessage
	r.JSON(t, &raw)
	if r.Status != http.StatusOK || string(raw["items"]) != "[]" || string(raw["next_cursor"]) != `""` {
		t.Fatalf("empty area: %d %s, want 200 with items [] and next_cursor \"\"", r.Status, r.Body)
	}
}

func TestW2B4_TheListQueryCanUseAnIndex(t *testing.T) {
	db := apiDB(t)
	qs := dataStatements(statements(t, func() {
		newClient(t).get(listPath(centreLat, centreLng, 5, 10, nil))
	}), "open_slots")
	checked := 0
	for _, q := range qs {
		if !strings.Contains(strings.ToLower(q), "restaurants") {
			continue
		}
		checked++
		if keys := explainKeys(t, db, stripComments(q)); keys["restaurants"] == "" {
			t.Fatalf("EXPLAIN finds no usable index on restaurants for:\n%s", q)
		}
	}
	if checked == 0 {
		t.Fatalf("the list request ran no query on restaurants; saw %q", qs)
	}
}

func insertRestaurant(t *testing.T, db *sql.DB, name string, lat, lng float64, active bool) int64 {
	t.Helper()
	res := mustExec(t, db, "INSERT INTO restaurants (name, lat, lng, is_active) VALUES (?, ?, ?, ?)", name, fmt.Sprintf("%.6f", lat), fmt.Sprintf("%.6f", lng), active)
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, q := range []string{"DELETE FROM open_slots WHERE restaurant_id = ?", "DELETE FROM restaurants WHERE id = ?"} {
			_, _ = db.Exec(q, id)
		}
	})
	return id
}

func stripComments(q string) string {
	var lines []string
	for _, l := range strings.Split(q, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "--") {
			continue
		}
		lines = append(lines, l)
	}
	return strings.Join(lines, "\n")
}
