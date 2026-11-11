//go:build w2o3

package e2e_test

import (
	"math"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

const (
	distLat = 13.0827
	distLng = 80.2707
)

type placed struct {
	id     int64
	lat    float64
	lng    float64
	active bool
	metres float64
}

func haversineMetres(lat1, lng1, lat2, lng2 float64) float64 {
	const r = 6371000.0
	p1, p2 := lat1*math.Pi/180, lat2*math.Pi/180
	dp, dl := (lat2-lat1)*math.Pi/180, (lng2-lng1)*math.Pi/180
	a := math.Sin(dp/2)*math.Sin(dp/2) + math.Cos(p1)*math.Cos(p2)*math.Sin(dl/2)*math.Sin(dl/2)
	return 2 * r * math.Asin(math.Sqrt(a))
}

// distanceFixture places restaurants by km north and east of the centre and
// returns the active ones within 5 km, nearest first, ids breaking ties.
func distanceFixture(t *testing.T) []placed {
	t.Helper()
	db := apiDB(t)
	at := func(north, east float64, active bool) placed {
		lat := distLat + north/111.0
		lng := distLng + east/(111.0*math.Cos(distLat*math.Pi/180))
		id := insertRestaurant(t, db, "Distance test", lat, lng, active)
		lat, lng = math.Round(lat*1e6)/1e6, math.Round(lng*1e6)/1e6
		return placed{id, lat, lng, active, haversineMetres(distLat, distLng, lat, lng)}
	}
	e := at(3, 0, true)
	g := at(0, 2.5, true)
	h := at(0, 2.5, true)
	a := at(0.5, 0, true)
	at(0.2, 0, false)
	b := at(0, 1, true)
	c := at(-1.5, -1.5, true)
	at(3.6, 3.6, true)
	at(0, 30, true)
	return []placed{a, b, c, g, h, e}
}

func distanceWalk(t *testing.T, limit int) ([]listed, [][]listed) {
	t.Helper()
	_, pages := walk(t, newClient(t), listPath(distLat, distLng, 5, limit, url.Values{"sort": {"distance"}}), nil)
	var all []listed
	for _, p := range pages {
		all = append(all, p...)
	}
	return all, pages
}

func TestW2O3_ResultsAreNearestFirstWithIDBreakingTies(t *testing.T) {
	want := distanceFixture(t)
	got, _ := distanceWalk(t, 50)
	if len(got) != len(want) {
		t.Fatalf("got %d restaurants, want the %d active ones within 5 km", len(got), len(want))
	}
	for i := range want {
		if got[i].ID != want[i].id {
			t.Fatalf("position %d is restaurant %d, want %d (nearest first, then lower id)", i, got[i].ID, want[i].id)
		}
	}
}

func TestW2O3_DistanceMetresIsTheGreatCircleDistance(t *testing.T) {
	want := distanceFixture(t)
	got, _ := distanceWalk(t, 50)
	for i, p := range want {
		if i >= len(got) || got[i].DistanceM == nil {
			t.Fatalf("restaurant %d has no distance_m", p.id)
		}
		if math.Abs(float64(*got[i].DistanceM)-p.metres) > 2 {
			t.Fatalf("restaurant %d: distance_m = %d, want %.0f within 2 m (haversine, earth radius 6371 km)", p.id, *got[i].DistanceM, p.metres)
		}
	}
}

func TestW2O3_TheRadiusIsACircleNotItsBoundingBox(t *testing.T) {
	want := distanceFixture(t)
	got, _ := distanceWalk(t, 50)
	ok := map[int64]bool{}
	for _, p := range want {
		ok[p.id] = true
	}
	for _, g := range got {
		if !ok[g.ID] {
			t.Fatalf("restaurant %d was listed but is inactive or more than 5 km away (a corner of the bounding box is 7 km out)", g.ID)
		}
	}
}

func TestW2O3_PagingWalksTiesExactlyOnceAtEveryLimit(t *testing.T) {
	want := distanceFixture(t)
	for limit := 1; limit <= 4; limit++ {
		got, pages := distanceWalk(t, limit)
		if len(got) != len(want) {
			t.Fatalf("limit %d: walked %d restaurants in %d pages, want %d", limit, len(got), len(pages), len(want))
		}
		for i := range want {
			if got[i].ID != want[i].id {
				t.Fatalf("limit %d: position %d is restaurant %d, want %d", limit, i, got[i].ID, want[i].id)
			}
		}
	}
}

func TestW2O3_ACursorTheAPIDidNotIssueIs400(t *testing.T) {
	c := newClient(t)
	base := listPath(distLat, distLng, 5, 2, url.Values{"sort": {"distance"}})
	for _, bad := range []string{"garbage!!", "AAAA", "e30"} {
		expectError(t, c.get(base+"&cursor="+bad), http.StatusBadRequest, "invalid_cursor", "")
	}
	expectError(t, c.get(listPath(distLat, distLng, 5, 2, url.Values{"sort": {"name"}})), http.StatusBadRequest, "invalid_parameter", "sort")
}

func TestW2O3_TheDistanceQueryCanUseAnIndex(t *testing.T) {
	distanceFixture(t)
	db := apiDB(t)
	qs := dataStatements(statements(t, func() { distanceWalk(t, 50) }), "open_slots")
	checked := 0
	for _, q := range qs {
		if strings.Contains(strings.ToLower(q), "restaurants") {
			checked++
			if keys := explainKeys(t, db, stripComments(q)); keys["restaurants"] == "" {
				t.Fatalf("EXPLAIN finds no usable index on restaurants for:\n%s", q)
			}
		}
	}
	if checked == 0 {
		t.Fatal("sort=distance ran no query on restaurants")
	}
}
