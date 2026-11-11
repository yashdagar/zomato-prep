//go:build w2o2

package e2e_test

import (
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
)

const (
	searchLat = 19.0760
	searchLng = 72.8777
)

func searchFixture(t *testing.T) map[string]int64 {
	t.Helper()
	db := apiDB(t)
	ids := map[string]int64{}
	for _, r := range []struct {
		name   string
		dLat   float64
		active bool
	}{
		{"Pizza Palace", 0.001, true},
		{"Pizzeria Roma", 0.002, true},
		{"Burger Barn", 0.003, true},
		{"Dominos Pizza", 0.004, true},
		{"Pasta House", 0.005, true},
		{"Pizza Closed", 0.006, false},
		{"Pizza Faraway", 1, true},
		{"Late Night Pizza Corner", 0.007, true},
	} {
		ids[r.name] = insertRestaurant(t, db, r.name, searchLat+r.dLat, searchLng, r.active)
	}
	return ids
}

func search(t *testing.T, q string, limit int) []string {
	t.Helper()
	extra := url.Values{"q": {q}}
	_, pages := walk(t, newClient(t), listPath(searchLat, searchLng, 5, limit, extra), nil)
	var names []string
	for _, p := range pages {
		for _, it := range p {
			names = append(names, it.Name)
		}
	}
	slices.Sort(names)
	return names
}

func expectNames(t *testing.T, q string, want ...string) {
	t.Helper()
	slices.Sort(want)
	if got := search(t, q, 50); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("q=%q returned %q, want %q", q, got, want)
	}
}

func TestW2O2_AWordMatchesWholeWordsAndWordStarts(t *testing.T) {
	searchFixture(t)
	expectNames(t, "pizza", "Pizza Palace", "Dominos Pizza", "Late Night Pizza Corner")
	expectNames(t, "piz", "Pizza Palace", "Pizzeria Roma", "Dominos Pizza", "Late Night Pizza Corner")
	expectNames(t, "alace")
}

func TestW2O2_SearchIgnoresCase(t *testing.T) {
	searchFixture(t)
	expectNames(t, "PIZZA", "Pizza Palace", "Dominos Pizza", "Late Night Pizza Corner")
	expectNames(t, "bUrGeR", "Burger Barn")
}

func TestW2O2_EveryWordMustMatch(t *testing.T) {
	searchFixture(t)
	expectNames(t, "pizza palace", "Pizza Palace")
	expectNames(t, "night corn", "Late Night Pizza Corner")
	expectNames(t, "pizza burger")
}

func TestW2O2_OperatorCharactersAreSearchedAsPlainText(t *testing.T) {
	searchFixture(t)
	expectNames(t, `"piz`, "Pizza Palace", "Pizzeria Roma", "Dominos Pizza", "Late Night Pizza Corner")
	expectNames(t, "pizza -palace", "Pizza Palace")
	expectNames(t, "+pizza*", "Pizza Palace", "Dominos Pizza", "Late Night Pizza Corner")
	expectNames(t, "(pizza)", "Pizza Palace", "Dominos Pizza", "Late Night Pizza Corner")
}

func TestW2O2_SearchKeepsTheGeoAndActiveFilters(t *testing.T) {
	searchFixture(t)
	for _, n := range search(t, "pizza", 50) {
		if n == "Pizza Closed" || n == "Pizza Faraway" {
			t.Fatalf("q=pizza returned %q, which is inactive or outside the radius", n)
		}
	}
}

func TestW2O2_SearchResultsPageLikeAnyList(t *testing.T) {
	searchFixture(t)
	extra := url.Values{"q": {"piz"}}
	ids, pages := walk(t, newClient(t), listPath(searchLat, searchLng, 5, 3, extra), nil)
	onceEach(t, ids)
	if len(ids) != 4 || len(pages) != 2 || len(pages[0]) != 3 {
		t.Fatalf("q=piz with limit=3 walked %d restaurants in %d pages, want 4 in pages of 3 and 1", len(ids), len(pages))
	}
}

func TestW2O2_AQueryWithNoSearchableWordIs400(t *testing.T) {
	c := newClient(t)
	for _, bad := range []string{"+-()", `""`, "   ", strings.Repeat("a", 101)} {
		expectError(t, c.get(listPath(searchLat, searchLng, 5, 5, url.Values{"q": {bad}})), http.StatusBadRequest, "invalid_parameter", "q")
	}
}

func TestW2O2_RestaurantNamesHaveAFulltextIndex(t *testing.T) {
	db := apiDB(t)
	n := count(t, db, `SELECT COUNT(*) FROM information_schema.statistics
		WHERE table_schema = DATABASE() AND table_name = 'restaurants' AND index_type = 'FULLTEXT' AND column_name = 'name'`)
	if n == 0 {
		t.Fatal("restaurants.name has no FULLTEXT index; add one in a new migration")
	}
}
