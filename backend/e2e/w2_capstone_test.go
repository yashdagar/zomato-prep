package e2e_test

import (
	"bufio"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

type fault int

const (
	noFault fault = iota
	skipsLastOfEachPage
	repeatsLastOfEachPage
	listsInactive
	ignoresRadius
	stopsAfterFirstPage
	ignoresLimit
	acceptsAnyCursor
)

// fakeList is a small, correct implementation of GET /v1/restaurants over
// the test database, with one fault switched on at a time. Every other path
// goes to the real API.
func fakeList(t *testing.T, db *sql.DB, f fault) *httptest.Server {
	t.Helper()
	real, err := url.Parse(os.Getenv("TEST_API_URL"))
	if err != nil {
		t.Fatal(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(real)
	bad := func(w http.ResponseWriter, code, field string) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": code, "message": code, "field": field})
	}
	num := func(q url.Values, k string, def, lo, hi float64) (float64, bool) {
		s := q.Get(k)
		if s == "" {
			return def, !math.IsNaN(def)
		}
		v, err := strconv.ParseFloat(s, 64)
		return v, err == nil && !math.IsNaN(v) && !math.IsInf(v, 0) && v >= lo && v <= hi
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/restaurants" {
			proxy.ServeHTTP(w, r)
			return
		}
		q := r.URL.Query()
		lat, ok := num(q, "lat", math.NaN(), -90, 90)
		if !ok {
			bad(w, "invalid_parameter", "lat")
			return
		}
		lng, ok := num(q, "lng", math.NaN(), -180, 180)
		if !ok {
			bad(w, "invalid_parameter", "lng")
			return
		}
		radius, ok := num(q, "radius_km", 5, 0, 50)
		if !ok || radius == 0 {
			bad(w, "invalid_parameter", "radius_km")
			return
		}
		limit := 20
		if s := q.Get("limit"); s != "" {
			v, err := strconv.Atoi(s)
			if err != nil {
				bad(w, "invalid_parameter", "limit")
				return
			}
			limit = min(max(v, 1), 50)
		}
		if f == ignoresLimit {
			limit = 50
		}
		var after int64
		if c := q.Get("cursor"); c != "" {
			b, err := base64.RawURLEncoding.DecodeString(c)
			v, perr := strconv.ParseInt(strings.TrimPrefix(string(b), "after:"), 10, 64)
			if err != nil || perr != nil || !strings.HasPrefix(string(b), "after:") {
				if f != acceptsAnyCursor {
					bad(w, "invalid_cursor", "")
					return
				}
				v = 0
			}
			after = v
		}
		dLat := radius / 111
		dLng := radius / (111 * math.Cos(lat*math.Pi/180))
		where := "lat BETWEEN ? AND ? AND lng BETWEEN ? AND ?"
		args := []any{lat - dLat, lat + dLat, lng - dLng, lng + dLng}
		if f == ignoresRadius {
			where, args = "1 = 1", nil
		}
		if f != listsInactive {
			where += " AND is_active = 1"
		}
		args = append(args, after, limit+1)
		rows, err := db.QueryContext(r.Context(), "SELECT id, name, lat, lng FROM restaurants WHERE "+where+" AND id > ? ORDER BY id LIMIT ?", args...)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer func() { _ = rows.Close() }()
		type item struct {
			ID     int64   `json:"id"`
			Name   string  `json:"name"`
			Lat    float64 `json:"lat"`
			Lng    float64 `json:"lng"`
			IsOpen bool    `json:"is_open"`
		}
		items := []item{}
		for rows.Next() {
			var it item
			if err := rows.Scan(&it.ID, &it.Name, &it.Lat, &it.Lng); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			items = append(items, it)
		}
		next := ""
		if len(items) > limit {
			items = items[:limit]
			last := items[len(items)-1].ID
			switch f {
			case skipsLastOfEachPage:
				items = items[:len(items)-1]
			case repeatsLastOfEachPage:
				last--
			case stopsAfterFirstPage:
				last = -1
			}
			if last >= 0 {
				next = base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf("after:%d", last)))
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"items": items, "next_cursor": next})
	}))
	t.Cleanup(srv.Close)
	return srv
}

type suiteResult struct {
	passed, failed int
	output         string
	buildFailed    bool
}

func runYourTests(t *testing.T, apiURL string) suiteResult {
	t.Helper()
	dir := backendDir(t)
	if _, err := os.Stat(dir + "/integration"); err != nil {
		t.Fatal("write your integration tests in backend/integration, as a Go package whose tests read TEST_API_URL and TEST_MYSQL_DSN")
	}
	out, _ := command(t, 5*time.Minute, []string{"TEST_API_URL=" + apiURL}, "go", "test", "-count=1", "-p", "1", "-json", "./integration/...")
	var res suiteResult
	var text []string
	sc := bufio.NewScanner(strings.NewReader(out))
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		var e struct{ Action, Test, Output string }
		if json.Unmarshal(sc.Bytes(), &e) != nil {
			text = append(text, sc.Text())
			continue
		}
		if e.Output != "" {
			text = append(text, strings.TrimRight(e.Output, "\n"))
			if strings.Contains(e.Output, "[build failed]") || strings.Contains(e.Output, "[setup failed]") {
				res.buildFailed = true
			}
		}
		if e.Test == "" || strings.Contains(e.Test, "/") {
			continue
		}
		switch e.Action {
		case "pass":
			res.passed++
		case "fail":
			res.failed++
		}
	}
	if len(text) > 40 {
		text = text[len(text)-40:]
	}
	res.output = strings.Join(text, "\n")
	return res
}

func TestW2B8_YourIntegrationTestsPassAgainstYourAPI(t *testing.T) {
	res := runYourTests(t, baseURL(t))
	if res.buildFailed {
		t.Fatalf("backend/integration does not compile:\n%s", res.output)
	}
	if res.failed > 0 || res.passed < 3 {
		t.Fatalf("against your API: %d passed, %d failed; want at least 3 tests and all passing\n%s", res.passed, res.failed, res.output)
	}
}

func TestW2B8_YourIntegrationTestsPassAgainstAnotherCorrectAPI(t *testing.T) {
	res := runYourTests(t, fakeList(t, apiDB(t), noFault).URL)
	if res.failed > 0 || res.buildFailed {
		t.Fatalf("against a different but correct implementation of the contract your tests fail; they depend on something the contract does not promise, such as the cursor format or the exact order\n%s", res.output)
	}
}

func catches(t *testing.T, f fault, bug string) {
	t.Helper()
	baseURL(t)
	res := runYourTests(t, fakeList(t, apiDB(t), f).URL)
	if res.buildFailed {
		t.Fatalf("backend/integration does not compile:\n%s", res.output)
	}
	if res.failed == 0 {
		t.Fatalf("your integration tests all pass against an API that %s; add a test that catches it", bug)
	}
}

func TestW2B8_YourTestsCatchAPageThatDropsARow(t *testing.T) {
	catches(t, skipsLastOfEachPage, "drops the last restaurant of every full page")
}

func TestW2B8_YourTestsCatchAPageThatRepeatsARow(t *testing.T) {
	catches(t, repeatsLastOfEachPage, "repeats the last restaurant of a page at the start of the next")
}

func TestW2B8_YourTestsCatchInactiveRestaurantsBeingListed(t *testing.T) {
	catches(t, listsInactive, "lists inactive restaurants")
}

func TestW2B8_YourTestsCatchTheRadiusBeingIgnored(t *testing.T) {
	catches(t, ignoresRadius, "lists restaurants however far away they are")
}

func TestW2B8_YourTestsCatchAWalkThatEndsEarly(t *testing.T) {
	catches(t, stopsAfterFirstPage, "returns an empty next_cursor after the first page")
}

func TestW2B8_YourTestsCatchTheLimitBeingIgnored(t *testing.T) {
	catches(t, ignoresLimit, "ignores limit and always returns up to 50")
}

func TestW2B8_YourTestsCatchAForgedCursorBeingAccepted(t *testing.T) {
	catches(t, acceptsAnyCursor, "accepts any cursor and starts again from the beginning")
}
