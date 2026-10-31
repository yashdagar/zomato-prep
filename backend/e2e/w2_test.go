package e2e_test

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"testing"
)

func TestW2B4_PaginationVisitsEveryRestaurantOnce(t *testing.T) {
	c := newClient(t)
	seen := map[int64]int{}
	cursor := ""
	for pages := 0; pages < 200; pages++ {
		path := "/v1/restaurants?lat=28.46&lng=77.03&radius_km=5&limit=2"
		if cursor != "" {
			path += "&cursor=" + cursor
		}
		r := c.get(path)
		if r.Status != http.StatusOK {
			t.Fatalf("page %d: %d %s", pages, r.Status, r.Body)
		}
		var p page[restaurant]
		r.JSON(t, &p)
		if len(p.Items) > 2 {
			t.Fatalf("limit=2 returned %d items", len(p.Items))
		}
		for _, it := range p.Items {
			seen[it.ID]++
		}
		if p.NextCursor == "" {
			break
		}
		cursor = p.NextCursor
	}
	if len(seen) < 10 {
		t.Fatalf("walked only %d restaurants; the seed puts 50 within 5 km", len(seen))
	}
	for id, n := range seen {
		if n != 1 {
			t.Fatalf("restaurant %d seen %d times", id, n)
		}
	}
}

func TestW2B4_LimitIsClampedAndBadCursorIs400(t *testing.T) {
	c := newClient(t)
	r := c.get("/v1/restaurants?lat=28.46&lng=77.03&radius_km=5&limit=500")
	if r.Status != http.StatusOK {
		t.Fatalf("limit=500: %d %s", r.Status, r.Body)
	}
	var p page[restaurant]
	r.JSON(t, &p)
	if len(p.Items) > 50 {
		t.Fatalf("limit=500 returned %d items, want at most 50", len(p.Items))
	}
	if r := c.get("/v1/restaurants?lat=28.46&lng=77.03&radius_km=5&cursor=not-base64!!"); r.Status != http.StatusBadRequest {
		t.Fatalf("bad cursor: %d, want 400", r.Status)
	}
	if r := c.get("/v1/restaurants?lat=abc&lng=77.03"); r.Status != http.StatusBadRequest {
		t.Fatalf("bad lat: %d, want 400", r.Status)
	}
}

func TestW2B5_MenuIsNestedAndMissingRestaurantIs404(t *testing.T) {
	c := newClient(t)
	_, m := firstRestaurant(t, c)
	item := m.Items[0]
	if item.Name == "" || len(item.Sizes) == 0 {
		t.Fatalf("item = %+v, want name and sizes", item)
	}
	if r := c.get("/v1/restaurants/999999999/menu"); r.Status != http.StatusNotFound {
		t.Fatalf("missing restaurant: %d, want 404", r.Status)
	}
}

func TestW2B6_PriceCollectsEveryError(t *testing.T) {
	c := newClient(t)
	_, m := firstRestaurant(t, c)
	item := m.Items[0]
	var single *menuGroup
	for i := range item.Groups {
		if item.Groups[i].MaxSelect == 1 && len(item.Groups[i].Options) >= 2 {
			single = &item.Groups[i]
			break
		}
	}
	if single == nil {
		t.Skip("seed needs a group with max_select 1 and two options (the crust group)")
	}
	body := map[string]any{"item_id": item.ID, "size": item.Sizes[0].Size, "chosen": map[string][]int64{fmt.Sprint(single.ID): {single.Options[0].ID, single.Options[1].ID}}, "qty": 1}
	r := c.post("/v1/menu/price", body)
	if r.Status != http.StatusOK {
		t.Fatalf("price: %d %s", r.Status, r.Body)
	}
	var q struct {
		Errors []string `json:"errors"`
	}
	r.JSON(t, &q)
	if len(q.Errors) == 0 {
		t.Fatalf("two picks in a max 1 group returned no errors: %s", r.Body)
	}
}

func TestW2B6_PriceHappyPath(t *testing.T) {
	c := newClient(t)
	_, m := firstRestaurant(t, c)
	r := c.post("/v1/menu/price", validLine(t, m))
	if r.Status != http.StatusOK {
		t.Fatalf("price: %d %s", r.Status, r.Body)
	}
	var q struct {
		UnitPaise  int64    `json:"unit_paise"`
		TotalPaise int64    `json:"total_paise"`
		Errors     []string `json:"errors"`
	}
	r.JSON(t, &q)
	if len(q.Errors) != 0 || q.UnitPaise <= 0 || q.TotalPaise != q.UnitPaise {
		t.Fatalf("quote = %s, want no errors and total = unit for qty 1", r.Body)
	}
}

func TestW2B4_InvalidQueryParametersAre400WithAnErrorField(t *testing.T) {
	c := newClient(t)
	cases := map[string]string{
		"missing lat":       "/v1/restaurants?lng=77.03",
		"lat not finite":    "/v1/restaurants?lat=NaN&lng=77.03",
		"lng infinite":      "/v1/restaurants?lat=28.46&lng=Inf",
		"lat out of range":  "/v1/restaurants?lat=91&lng=77.03",
		"limit not integer": "/v1/restaurants?lat=28.46&lng=77.03&limit=abc",
		"negative radius":   "/v1/restaurants?lat=28.46&lng=77.03&radius_km=-1",
		"radius not number": "/v1/restaurants?lat=28.46&lng=77.03&radius_km=far",
	}
	for name, path := range cases {
		t.Run(name, func(t *testing.T) {
			r := c.get(path)
			var body map[string]any
			if r.Status != http.StatusBadRequest || json.Unmarshal(r.Body, &body) != nil || body["error"] == nil {
				t.Fatalf("%s: %d %s, want 400 with a JSON error field", path, r.Status, r.Body)
			}
		})
	}
}

func TestW2B4_LimitBelowOneIsClampedToOne(t *testing.T) {
	r := newClient(t).get("/v1/restaurants?lat=28.46&lng=77.03&radius_km=5&limit=0")
	var p page[restaurant]
	r.JSON(t, &p)
	if r.Status != http.StatusOK || len(p.Items) != 1 {
		t.Fatalf("limit=0: %d with %d items, want 200 with exactly 1", r.Status, len(p.Items))
	}
}

func TestW2B4_NothingNearbyIsAnEmptyLastPage(t *testing.T) {
	r := newClient(t).get("/v1/restaurants?lat=0&lng=0&radius_km=1")
	var raw map[string]json.RawMessage
	r.JSON(t, &raw)
	if r.Status != http.StatusOK || string(raw["items"]) != "[]" || string(raw["next_cursor"]) != `""` {
		t.Fatalf("empty area: %d %s, want 200 with items [] and next_cursor \"\"", r.Status, r.Body)
	}
}

func TestW2B4_ItemsAreInsideTheRadiusBox(t *testing.T) {
	r := newClient(t).get("/v1/restaurants?lat=28.46&lng=77.03&radius_km=2&limit=50")
	var p struct {
		Items []struct {
			ID  int64   `json:"id"`
			Lat float64 `json:"lat"`
			Lng float64 `json:"lng"`
		} `json:"items"`
	}
	r.JSON(t, &p)
	if r.Status != http.StatusOK || len(p.Items) == 0 {
		t.Fatalf("radius 2: %d %s, want some restaurants", r.Status, r.Body)
	}
	for _, it := range p.Items {
		if math.Abs(it.Lat-28.46) > 2.0/111+1e-6 || math.Abs(it.Lng-77.03) > 2.0/(111*math.Cos(28.46*math.Pi/180))+1e-6 {
			t.Fatalf("restaurant %d at %.6f,%.6f is outside the 2 km box", it.ID, it.Lat, it.Lng)
		}
	}
}

func TestW2B5_GroupsCarryOptionsWithPrices(t *testing.T) {
	_, m := firstRestaurant(t, newClient(t))
	groups := 0
	for _, it := range m.Items {
		for _, g := range it.Groups {
			groups++
			if len(g.Options) == 0 {
				t.Fatalf("group %q has no options", g.Name)
			}
			for _, o := range g.Options {
				if len(o.Prices) == 0 {
					t.Fatalf("option %q has no prices map entries", o.Name)
				}
			}
		}
	}
	if groups == 0 {
		t.Fatal("the seeded menu should have option groups")
	}
}

func TestW2B5_RestaurantWithoutItemsHasAnEmptyMenu(t *testing.T) {
	c := newClient(t)
	r := c.get("/v1/restaurants?lat=28.46&lng=77.03&radius_km=5&limit=50")
	var p page[restaurant]
	r.JSON(t, &p)
	for _, rest := range p.Items {
		mr := c.get(fmt.Sprintf("/v1/restaurants/%d/menu", rest.ID))
		var raw map[string]json.RawMessage
		mr.JSON(t, &raw)
		if string(raw["items"]) == "[]" {
			if mr.Status != http.StatusOK {
				t.Fatalf("empty menu: %d, want 200", mr.Status)
			}
			return
		}
	}
	t.Skip("every nearby restaurant has a menu; the seed gives only restaurant 1 one")
}

func TestW2B5_NonNumericIDIs400(t *testing.T) {
	if r := newClient(t).get("/v1/restaurants/abc/menu"); r.Status != http.StatusBadRequest {
		t.Fatalf("/v1/restaurants/abc/menu: %d, want 400", r.Status)
	}
}

func TestW2B6_UnknownItemIs404(t *testing.T) {
	r := newClient(t).post("/v1/menu/price", map[string]any{"item_id": 999999999, "size": "M", "chosen": map[string][]int64{}, "qty": 1})
	if r.Status != http.StatusNotFound {
		t.Fatalf("unknown item: %d %s, want 404", r.Status, r.Body)
	}
}

func TestW2B6_MalformedBodyOrQuantityIs400(t *testing.T) {
	_, m := firstRestaurant(t, newClient(t))
	item := m.Items[0]
	cases := map[string]any{
		"not json":      "just text",
		"zero quantity": map[string]any{"item_id": item.ID, "size": item.Sizes[0].Size, "chosen": map[string][]int64{}, "qty": 0},
		"missing size":  map[string]any{"item_id": item.ID, "chosen": map[string][]int64{}, "qty": 1},
		"unknown field": map[string]any{"item_id": item.ID, "size": item.Sizes[0].Size, "chosen": map[string][]int64{}, "qty": 1, "discount": 100},
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			r := newClient(t).post("/v1/menu/price", body)
			var got map[string]any
			if r.Status != http.StatusBadRequest || json.Unmarshal(r.Body, &got) != nil || got["error"] == nil {
				t.Fatalf("%d %s, want 400 with a JSON error field", r.Status, r.Body)
			}
		})
	}
}

func TestW2B6_SizeThatIsNotOfferedIsReportedAsAnError(t *testing.T) {
	_, m := firstRestaurant(t, newClient(t))
	line := validLine(t, m)
	line["size"] = "XXL"
	r := newClient(t).post("/v1/menu/price", line)
	var q struct {
		Errors []string `json:"errors"`
	}
	r.JSON(t, &q)
	if r.Status != http.StatusOK || len(q.Errors) == 0 {
		t.Fatalf("size XXL: %d %s, want 200 with at least one error", r.Status, r.Body)
	}
}

func TestW2B6_QuantityMultipliesTheUnitPrice(t *testing.T) {
	_, m := firstRestaurant(t, newClient(t))
	line := validLine(t, m)
	line["qty"] = 3
	r := newClient(t).post("/v1/menu/price", line)
	var q struct {
		UnitPaise  int64    `json:"unit_paise"`
		TotalPaise int64    `json:"total_paise"`
		Errors     []string `json:"errors"`
	}
	r.JSON(t, &q)
	if r.Status != http.StatusOK || len(q.Errors) != 0 || q.UnitPaise <= 0 || q.TotalPaise != 3*q.UnitPaise {
		t.Fatalf("qty 3: %d %s, want no errors and total = 3 x unit", r.Status, r.Body)
	}
}

func TestW2B7_ListMarksEachRestaurantOpenOrClosed(t *testing.T) {
	r := newClient(t).get("/v1/restaurants?lat=28.46&lng=77.03&radius_km=5&limit=50")
	var p struct {
		Items []map[string]any `json:"items"`
	}
	r.JSON(t, &p)
	if r.Status != http.StatusOK || len(p.Items) == 0 {
		t.Fatalf("list: %d %s", r.Status, r.Body)
	}
	for _, it := range p.Items {
		if _, ok := it["is_open"].(bool); !ok {
			t.Fatalf("restaurant %v has is_open = %#v, want a boolean", it["id"], it["is_open"])
		}
	}
}
