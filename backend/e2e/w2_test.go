package e2e_test

import (
	"fmt"
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
