package e2e_test

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"testing"
)

type fullOption struct {
	ID      int64            `json:"id"`
	Name    string           `json:"name"`
	InStock bool             `json:"in_stock"`
	Prices  map[string]int64 `json:"prices"`
}

type fullGroup struct {
	ID        int64        `json:"id"`
	Name      string       `json:"name"`
	MinSelect int          `json:"min_select"`
	MaxSelect int          `json:"max_select"`
	Options   []fullOption `json:"options"`
}

type fullItem struct {
	ID     int64       `json:"id"`
	Name   string      `json:"name"`
	Sizes  []menuSize  `json:"sizes"`
	Groups []fullGroup `json:"groups"`
}

type fullMenu struct {
	RestaurantID int64      `json:"restaurant_id"`
	Items        []fullItem `json:"items"`
}

var farmhouse = fullItem{
	ID:    1,
	Name:  "Farmhouse pizza",
	Sizes: []menuSize{{"R", 19900}, {"M", 39900}, {"L", 59900}},
	Groups: []fullGroup{
		{ID: 1, Name: "Crust", MinSelect: 1, MaxSelect: 1, Options: []fullOption{
			{1, "Classic hand tossed", true, map[string]int64{"*": 0}},
			{2, "Cheese burst", true, map[string]int64{"M": 9900, "L": 12900}},
		}},
		{ID: 2, Name: "Toppings", MinSelect: 0, MaxSelect: 5, Options: []fullOption{
			{3, "Olives", true, map[string]int64{"*": 3500}},
			{4, "Jalapeno", true, map[string]int64{"*": 3500}},
			{5, "Paneer", true, map[string]int64{"*": 6000}},
			{6, "Mushroom", true, map[string]int64{"*": 4500}},
			{7, "Red paprika", true, map[string]int64{"*": 4000}},
		}},
	},
}

func getMenu(t *testing.T, c *client, id int64) fullMenu {
	t.Helper()
	r := c.get(fmt.Sprintf("/v1/restaurants/%d/menu", id))
	if r.Status != http.StatusOK {
		t.Fatalf("menu %d: %d %s", id, r.Status, r.Body)
	}
	var m fullMenu
	r.JSON(t, &m)
	return m
}

func pretty(v any) string {
	b, _ := json.MarshalIndent(v, "", "  ")
	return string(b)
}

func TestW2B5_TheSeededMenuIsNestedSizesGroupsOptionsAndPrices(t *testing.T) {
	m := getMenu(t, newClient(t), 1)
	if m.RestaurantID != 1 {
		t.Fatalf("restaurant_id = %d, want 1", m.RestaurantID)
	}
	for _, it := range m.Items {
		if it.ID == 1 {
			if !reflect.DeepEqual(it, farmhouse) {
				t.Fatalf("Farmhouse pizza =\n%s\nwant\n%s", pretty(it), pretty(farmhouse))
			}
			return
		}
	}
	t.Fatalf("restaurant 1's menu has no item 1: %s", pretty(m))
}

type menuSpec struct {
	sort   int
	sizes  []menuSize
	groups [][]int64
}

// insertMenu adds items to a restaurant: each group gets options priced 100,
// 200, ... in paise for size *, and everything is removed when the test ends.
func insertMenu(t *testing.T, db *sql.DB, restaurantID int64, specs []menuSpec) []fullItem {
	t.Helper()
	var out []fullItem
	var itemIDs []int64
	for n, spec := range specs {
		res := mustExec(t, db, "INSERT INTO items (restaurant_id, name, sort) VALUES (?, ?, ?)", restaurantID, fmt.Sprintf("Test item %d", n), spec.sort)
		itemID, _ := res.LastInsertId()
		itemIDs = append(itemIDs, itemID)
		it := fullItem{ID: itemID, Name: fmt.Sprintf("Test item %d", n), Sizes: spec.sizes, Groups: []fullGroup{}}
		for _, s := range spec.sizes {
			mustExec(t, db, "INSERT INTO item_sizes (item_id, size, price_paise) VALUES (?, ?, ?)", itemID, s.Size, s.PricePaise)
		}
		for g, opts := range spec.groups {
			res := mustExec(t, db, "INSERT INTO option_groups (item_id, name, min_select, max_select) VALUES (?, ?, 0, ?)", itemID, fmt.Sprintf("Group %d", g), len(opts))
			groupID, _ := res.LastInsertId()
			grp := fullGroup{ID: groupID, Name: fmt.Sprintf("Group %d", g), MinSelect: 0, MaxSelect: len(opts), Options: []fullOption{}}
			for o, price := range opts {
				res := mustExec(t, db, "INSERT INTO options (group_id, name, in_stock) VALUES (?, ?, ?)", groupID, fmt.Sprintf("Option %d", o), o%2 == 0)
				optionID, _ := res.LastInsertId()
				mustExec(t, db, "INSERT INTO option_prices (option_id, size, price_paise) VALUES (?, '*', ?)", optionID, price)
				grp.Options = append(grp.Options, fullOption{optionID, fmt.Sprintf("Option %d", o), o%2 == 0, map[string]int64{"*": price}})
			}
			it.Groups = append(it.Groups, grp)
		}
		out = append(out, it)
	}
	t.Cleanup(func() {
		for _, id := range itemIDs {
			for _, q := range []string{
				"DELETE p FROM option_prices p JOIN options o ON o.id = p.option_id JOIN option_groups g ON g.id = o.group_id WHERE g.item_id = ?",
				"DELETE o FROM options o JOIN option_groups g ON g.id = o.group_id WHERE g.item_id = ?",
				"DELETE FROM option_groups WHERE item_id = ?",
				"DELETE FROM item_sizes WHERE item_id = ?",
				"DELETE FROM items WHERE id = ?",
			} {
				_, _ = db.Exec(q, id)
			}
		}
	})
	return out
}

func TestW2B5_ItemsAreOrderedBySortThenIDAndSizesByPrice(t *testing.T) {
	db := apiDB(t)
	rest := insertRestaurant(t, db, "Menu order test", 12.9716, 77.5946, true)
	items := insertMenu(t, db, rest, []menuSpec{
		{sort: 2, sizes: []menuSize{{"L", 300}, {"S", 100}, {"M", 200}}},
		{sort: 0, sizes: []menuSize{{"R", 100}}},
		{sort: 2, sizes: []menuSize{{"R", 100}}},
		{sort: 1, sizes: []menuSize{{"R", 100}}},
	})
	m := getMenu(t, newClient(t), rest)
	want := []int64{items[1].ID, items[3].ID, items[0].ID, items[2].ID}
	var got []int64
	for _, it := range m.Items {
		got = append(got, it.ID)
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("item order = %v, want %v (sort ascending, then id)", got, want)
	}
	if fmt.Sprint(m.Items[2].Sizes) != fmt.Sprint([]menuSize{{"S", 100}, {"M", 200}, {"L", 300}}) {
		t.Fatalf("sizes = %v, want cheapest first", m.Items[2].Sizes)
	}
}

func TestW2B5_TheMenuRunsAtMostFiveStatementsWhateverItsSize(t *testing.T) {
	db := apiDB(t)
	rest := insertRestaurant(t, db, "Big menu test", 12.9716, 77.5946, true)
	sizes := []menuSize{{"R", 100}, {"L", 200}}
	var specs []menuSpec
	for i := range 6 {
		specs = append(specs, menuSpec{sort: i, sizes: sizes, groups: [][]int64{{100, 200, 300}, {400, 500}, {600}}})
	}
	want := insertMenu(t, db, rest, specs)
	var m fullMenu
	qs := dataStatements(statements(t, func() { m = getMenu(t, newClient(t), rest) }), "open_slots")
	if len(qs) > 5 {
		t.Fatalf("GET /v1/restaurants/%d/menu with 6 items, 18 groups and 36 options ran %d statements, want at most 5; batch by ids instead of querying per item or group:\n%s", rest, len(qs), pretty(qs))
	}
	if !reflect.DeepEqual(m.Items, want) {
		t.Fatalf("menu =\n%s\nwant\n%s", pretty(m.Items), pretty(want))
	}
}

func TestW2B5_ARestaurantWithoutItemsHasAnEmptyMenu(t *testing.T) {
	db := apiDB(t)
	rest := insertRestaurant(t, db, "No menu yet", 12.9716, 77.5946, true)
	r := newClient(t).get(fmt.Sprintf("/v1/restaurants/%d/menu", rest))
	var raw map[string]json.RawMessage
	r.JSON(t, &raw)
	if r.Status != http.StatusOK || string(raw["items"]) != "[]" {
		t.Fatalf("restaurant without items: %d %s, want 200 with items []", r.Status, r.Body)
	}
}

func TestW2B5_MissingOrInactiveRestaurantIs404(t *testing.T) {
	db := apiDB(t)
	c := newClient(t)
	expectError(t, c.get("/v1/restaurants/999999999/menu"), http.StatusNotFound, "restaurant_not_found", "")
	inactive := insertRestaurant(t, db, "Inactive with menu", 12.9716, 77.5946, false)
	insertMenu(t, db, inactive, []menuSpec{{sizes: []menuSize{{"R", 100}}}})
	expectError(t, c.get(fmt.Sprintf("/v1/restaurants/%d/menu", inactive)), http.StatusNotFound, "restaurant_not_found", "")
}

func TestW2B5_AnIDThatIsNotAPositiveIntegerIs400(t *testing.T) {
	c := newClient(t)
	for _, id := range []string{"abc", "0", "-4", "1.5"} {
		expectError(t, c.get("/v1/restaurants/"+id+"/menu"), http.StatusBadRequest, "invalid_parameter", "id")
	}
}
