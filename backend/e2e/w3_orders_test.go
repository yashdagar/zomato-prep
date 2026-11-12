package e2e_test

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

func key(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

func farmhouseLine(qty int) map[string]any {
	return map[string]any{"item_id": 1, "size": "M", "chosen": map[string][]int64{crust: {burst}, toppings: {olives, paneer}}, "qty": qty}
}

func placeRaw(c *client, idemKey string, body map[string]any) resp {
	if idemKey == "" {
		return c.post("/v1/orders", body)
	}
	return c.post("/v1/orders", body, "Idempotency-Key", idemKey)
}

func orderBody(lines ...map[string]any) map[string]any {
	ls := make([]any, len(lines))
	for i, l := range lines {
		ls[i] = l
	}
	return map[string]any{"restaurant_id": 1, "lines": ls}
}

func expectOrder(t *testing.T, r resp, status int) order {
	t.Helper()
	if r.Status != status {
		t.Fatalf("%d %s, want %d with an order", r.Status, r.Body, status)
	}
	var o order
	r.JSON(t, &o)
	if o.ID <= 0 {
		t.Fatalf("order = %s, want an id", r.Body)
	}
	return o
}

func TestW3B4_PlacingNeedsAnIdempotencyKeyOfOneTo36Characters(t *testing.T) {
	c, _ := signup(t, "customer", 0)
	expectError(t, placeRaw(c, "", orderBody(farmhouseLine(1))), http.StatusBadRequest, "invalid_parameter", "Idempotency-Key")
	expectError(t, placeRaw(c, strings.Repeat("k", 37), orderBody(farmhouseLine(1))), http.StatusBadRequest, "invalid_parameter", "Idempotency-Key")
	expectOrder(t, placeRaw(c, fmt.Sprintf("%036d", time.Now().UnixNano()), orderBody(farmhouseLine(1))), http.StatusCreated)
}

func TestW3B4_RepeatingAKeyReturnsTheOrderItCreated(t *testing.T) {
	c, _ := signup(t, "customer", 0)
	k := key("same")
	first := expectOrder(t, placeRaw(c, k, orderBody(farmhouseLine(1))), http.StatusCreated)
	again := expectOrder(t, placeRaw(c, k, orderBody(farmhouseLine(3))), http.StatusOK)
	if again.ID != first.ID || again.TotalPaise != first.TotalPaise {
		t.Fatalf("repeat with the same key = order %d total %d, want order %d total %d unchanged", again.ID, again.TotalPaise, first.ID, first.TotalPaise)
	}
	if other := expectOrder(t, placeRaw(c, k+"-2", orderBody(farmhouseLine(1))), http.StatusCreated); other.ID == first.ID {
		t.Fatal("a different key must create a different order")
	}
}

func TestW3B4_KeysAreScopedPerCustomer(t *testing.T) {
	a, _ := signup(t, "customer", 0)
	b, _ := signup(t, "customer", 0)
	k := key("shared")
	if expectOrder(t, placeRaw(a, k, orderBody(farmhouseLine(1))), http.StatusCreated).ID == expectOrder(t, placeRaw(b, k, orderBody(farmhouseLine(1))), http.StatusCreated).ID {
		t.Fatal("two customers using the same Idempotency-Key must get two different orders")
	}
}

func TestW3B4_FiftyConcurrentRequestsWithOneKeyMakeOneOrder(t *testing.T) {
	db := apiDB(t)
	c, _ := signup(t, "customer", 0)
	var me struct {
		ID int64 `json:"id"`
	}
	c.get("/v1/me").JSON(t, &me)
	k := key("race")
	var mu sync.Mutex
	ids := map[int64]bool{}
	statuses := map[int]int{}
	var wg sync.WaitGroup
	for i := range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := c.post("/v1/orders", orderBody(farmhouseLine(1)), "Idempotency-Key", k, "X-User-ID", fmt.Sprintf("%s-%d", k, i))
			mu.Lock()
			defer mu.Unlock()
			statuses[r.Status]++
			if r.Status == http.StatusOK || r.Status == http.StatusCreated {
				var o order
				r.JSON(t, &o)
				ids[o.ID] = true
			}
		}()
	}
	wg.Wait()
	for s := range statuses {
		if s != http.StatusOK && s != http.StatusCreated && s != http.StatusConflict {
			t.Fatalf("statuses %v, want only 201, 200 and 409 request_in_progress", statuses)
		}
	}
	if len(ids) != 1 || statuses[http.StatusCreated] != 1 {
		t.Fatalf("50 concurrent requests with one key: statuses %v and %d distinct orders, want exactly one 201 and one order", statuses, len(ids))
	}
	if n := count(t, db, "SELECT COUNT(*) FROM orders WHERE user_id = ?", me.ID); n != 1 {
		t.Fatalf("orders rows for the customer = %d, want 1", n)
	}
}

func TestW3B4_TheTotalIsPricedOnTheServer(t *testing.T) {
	c, _ := signup(t, "customer", 0)
	body := orderBody(farmhouseLine(2), map[string]any{"item_id": 1, "size": "R", "chosen": map[string][]int64{crust: {classic}}, "qty": 1})
	body["total_paise"] = 1
	o := expectOrder(t, placeRaw(c, key("total"), body), http.StatusCreated)
	if want := int64(2*55800 + 19900); o.TotalPaise != want {
		t.Fatalf("total_paise = %d, want %d (2 x 55800 + 19900), whatever the client sends", o.TotalPaise, want)
	}
	if o.Status != "PLACED" || o.Version != 1 || o.RestaurantID != 1 {
		t.Fatalf("new order = %+v, want PLACED, version 1, restaurant 1", o)
	}
}

func TestW3B4_PlacingRecordsLinesAPlacedEventAndAnOutboxRow(t *testing.T) {
	db := apiDB(t)
	c, _ := signup(t, "customer", 0)
	o := expectOrder(t, placeRaw(c, key("rows"), orderBody(farmhouseLine(2), farmhouseLine(1))), http.StatusCreated)
	if n := count(t, db, "SELECT COUNT(*) FROM order_lines WHERE order_id = ? AND item_id = 1 AND unit_paise = 55800", o.ID); n != 2 {
		t.Fatalf("order_lines for the order = %d with unit_paise 55800, want 2", n)
	}
	if n := count(t, db, "SELECT COUNT(*) FROM order_events WHERE order_id = ? AND from_status = '' AND to_status = 'PLACED' AND actor = 'customer'", o.ID); n != 1 {
		t.Fatal("want exactly one order_events row from '' to PLACED by the customer")
	}
	if n := count(t, db, "SELECT COUNT(*) FROM outbox WHERE key_ = ?", fmt.Sprint(o.ID)); n != 1 {
		t.Fatalf("outbox rows keyed by the order id = %d, want 1", n)
	}
}

func TestW3B4_AnInvalidOrderListsEveryProblemAndLeavesTheKeyFree(t *testing.T) {
	db := apiDB(t)
	c, _ := signup(t, "customer", 0)
	k := key("bad")
	bad := orderBody(farmhouseLine(1), map[string]any{"item_id": 1, "size": "XL", "chosen": map[string][]int64{crust: {classic}}, "qty": 1}, map[string]any{"item_id": 999999, "size": "M", "chosen": map[string][]int64{}, "qty": 1})
	r := placeRaw(c, k, bad)
	expectError(t, r, http.StatusBadRequest, "invalid_order", "")
	var body struct {
		Errors []struct {
			Line *int   `json:"line"`
			Code string `json:"code"`
		} `json:"errors"`
	}
	r.JSON(t, &body)
	var got []string
	for _, e := range body.Errors {
		if e.Line == nil {
			t.Fatalf("every error needs the index of its line: %s", r.Body)
		}
		got = append(got, fmt.Sprintf("%d/%s", *e.Line, e.Code))
	}
	sort.Strings(got)
	if strings.Join(got, ",") != "1/size_not_offered,2/unknown_item" {
		t.Fatalf("errors = %v, want line 1 size_not_offered and line 2 unknown_item", got)
	}
	var me struct {
		ID int64 `json:"id"`
	}
	c.get("/v1/me").JSON(t, &me)
	if n := count(t, db, "SELECT COUNT(*) FROM orders WHERE user_id = ?", me.ID); n != 0 {
		t.Fatal("a rejected order left an orders row behind")
	}
	expectOrder(t, placeRaw(c, k, orderBody(farmhouseLine(1))), http.StatusCreated)
}

func TestW3B4_AnItemFromAnotherRestaurantIsUnknown(t *testing.T) {
	c, _ := signup(t, "customer", 0)
	body := orderBody(farmhouseLine(1))
	body["restaurant_id"] = 2
	r := placeRaw(c, key("other"), body)
	expectError(t, r, http.StatusBadRequest, "invalid_order", "")
	if !strings.Contains(string(r.Body), `"unknown_item"`) {
		t.Fatalf("%s, want an unknown_item error: item 1 is on restaurant 1's menu", r.Body)
	}
}

func TestW3B4_AMalformedRequestIs400WithTheFieldNamed(t *testing.T) {
	c, _ := signup(t, "customer", 0)
	cases := []struct {
		name, field string
		body        map[string]any
	}{
		{"no restaurant", "restaurant_id", map[string]any{"lines": []any{farmhouseLine(1)}}},
		{"no lines", "lines", map[string]any{"restaurant_id": 1, "lines": []any{}}},
		{"qty 0", "lines", orderBody(farmhouseLine(0))},
		{"qty 51", "lines", orderBody(farmhouseLine(51))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			expectError(t, placeRaw(c, key("field"), tc.body), http.StatusBadRequest, "invalid_parameter", tc.field)
		})
	}
	r := rawRequest(t, c, http.MethodPost, "/v1/orders", `{"restaurant_id": 1, "lines": [`, "Idempotency-Key", key("raw"))
	expectError(t, r, http.StatusBadRequest, "invalid_body", "")
}

func TestW3B4_OnlyCustomersPlaceOrders(t *testing.T) {
	rider, _ := signup(t, "rider", 0)
	staff, _ := signup(t, "restaurant", 1)
	expectError(t, placeRaw(rider, key("rider"), orderBody(farmhouseLine(1))), http.StatusForbidden, "forbidden", "")
	expectError(t, placeRaw(staff, key("staff"), orderBody(farmhouseLine(1))), http.StatusForbidden, "forbidden", "")
	expectError(t, placeRaw(newClient(t), key("anon"), orderBody(farmhouseLine(1))), http.StatusUnauthorized, "unauthorized", "")
}
