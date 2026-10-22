package e2e_test

import (
	"fmt"
	"net/http"
	"testing"
	"time"
)

func TestW3B1_SignupLoginRefreshRotation(t *testing.T) {
	c, tk := signup(t, "customer", 0)
	r := c.post("/v1/auth/refresh", map[string]string{"refresh_token": tk.Refresh})
	if r.Status != http.StatusOK {
		t.Fatalf("refresh: %d %s", r.Status, r.Body)
	}
	var fresh tokens
	r.JSON(t, &fresh)
	if fresh.Refresh == "" || fresh.Refresh == tk.Refresh {
		t.Fatal("refresh must rotate the refresh token")
	}
	if r := c.post("/v1/auth/refresh", map[string]string{"refresh_token": tk.Refresh}); r.Status != http.StatusUnauthorized {
		t.Fatalf("reused refresh token: %d, want 401", r.Status)
	}
	if r := c.post("/v1/auth/login", map[string]string{"email": "nobody@test.local", "password": "x"}); r.Status != http.StatusUnauthorized {
		t.Fatalf("bad login: %d, want 401", r.Status)
	}
}

func TestW3B2_MeNeedsABearerToken(t *testing.T) {
	c, _ := signup(t, "customer", 0)
	r := c.get("/v1/me")
	if r.Status != http.StatusOK {
		t.Fatalf("me: %d %s", r.Status, r.Body)
	}
	var me struct {
		ID   int64  `json:"id"`
		Role string `json:"role"`
	}
	r.JSON(t, &me)
	if me.ID == 0 || me.Role != "customer" {
		t.Fatalf("me = %s", r.Body)
	}
	anon := newClient(t)
	if r := anon.get("/v1/me"); r.Status != http.StatusUnauthorized {
		t.Fatalf("anonymous me: %d, want 401", r.Status)
	}
	if r := anon.get("/v1/orders"); r.Status != http.StatusUnauthorized {
		t.Fatalf("anonymous orders: %d, want 401", r.Status)
	}
}

func TestW3B4_IdempotencyKeyIsRequiredAndDedupes(t *testing.T) {
	c, _ := signup(t, "customer", 0)
	rest, m := firstRestaurant(t, c)
	if r := c.post("/v1/orders", map[string]any{"restaurant_id": rest.ID, "lines": []any{validLine(t, m)}}); r.Status != http.StatusBadRequest {
		t.Fatalf("no Idempotency-Key: %d, want 400", r.Status)
	}
	key := fmt.Sprintf("k-%d", time.Now().UnixNano())
	first := placeOrder(t, c, rest, m, key)
	second := placeOrder(t, c, rest, m, key)
	if first.ID != second.ID {
		t.Fatalf("same key created two orders: %d and %d", first.ID, second.ID)
	}
	if third := placeOrder(t, c, rest, m, key+"-other"); third.ID == first.ID {
		t.Fatal("a different key must create a different order")
	}
}

func TestW3D1_FiftyConcurrentPlacementsMakeOneOrder(t *testing.T) {
	c, _ := signup(t, "customer", 0)
	rest, m := firstRestaurant(t, c)
	key := fmt.Sprintf("race-%d", time.Now().UnixNano())
	line := validLine(t, m)
	ids := make(chan int64, 50)
	for i := 0; i < 50; i++ {
		go func() {
			r := c.post("/v1/orders", map[string]any{"restaurant_id": rest.ID, "lines": []any{line}}, "Idempotency-Key", key)
			var o order
			if r.Status == http.StatusOK || r.Status == http.StatusCreated {
				_ = r.JSON
				r.JSON(t, &o)
			}
			ids <- o.ID
		}()
	}
	distinct := map[int64]bool{}
	for i := 0; i < 50; i++ {
		if id := <-ids; id != 0 {
			distinct[id] = true
		}
	}
	if len(distinct) != 1 {
		t.Fatalf("50 concurrent placements with one key produced %d distinct orders (some may have been 409 in-flight, that is fine), want exactly 1", len(distinct))
	}
}

func TestW3B5_TransitionsRespectTableAndRole(t *testing.T) {
	cust, _ := signup(t, "customer", 0)
	rest, m := firstRestaurant(t, cust)
	restUser, _ := signup(t, "restaurant", rest.ID)
	rider, _ := signup(t, "rider", 0)
	o := placeOrder(t, cust, rest, m, fmt.Sprintf("t-%d", time.Now().UnixNano()))

	if r := transition(t, cust, o.ID, "ACCEPT"); r.Status != http.StatusForbidden {
		t.Fatalf("customer ACCEPT: %d, want 403", r.Status)
	}
	if r := transition(t, rider, o.ID, "DELIVER"); r.Status != http.StatusConflict {
		t.Fatalf("DELIVER on PLACED: %d, want 409", r.Status)
	}
	r := restUser.post(fmt.Sprintf("/v1/orders/%d/events", o.ID), map[string]any{"event_id": "evt-" + fmt.Sprint(o.ID), "event": "ACCEPT"})
	if r.Status != http.StatusOK {
		t.Fatalf("restaurant ACCEPT: %d %s", r.Status, r.Body)
	}
	var after order
	r.JSON(t, &after)
	if after.Status != "ACCEPTED" || after.Version != o.Version+1 {
		t.Fatalf("after ACCEPT = %s, want ACCEPTED and version+1", r.Body)
	}
	dup := restUser.post(fmt.Sprintf("/v1/orders/%d/events", o.ID), map[string]any{"event_id": "evt-" + fmt.Sprint(o.ID), "event": "ACCEPT"})
	if dup.Status != http.StatusOK {
		t.Fatalf("duplicate event_id: %d, want 200 and no change", dup.Status)
	}
	var same order
	dup.JSON(t, &same)
	if same.Version != after.Version {
		t.Fatalf("duplicate event changed the version: %d -> %d", after.Version, same.Version)
	}
	if r := transition(t, cust, o.ID, "CANCEL"); r.Status != http.StatusOK {
		t.Fatalf("customer CANCEL while ACCEPTED: %d, want 200", r.Status)
	}
	if r := transition(t, restUser, o.ID, "START_PREP"); r.Status != http.StatusConflict {
		t.Fatalf("START_PREP after CANCEL: %d, want 409", r.Status)
	}
}

func TestW3B5_OrderListAndDetailWithEvents(t *testing.T) {
	cust, _ := signup(t, "customer", 0)
	rest, m := firstRestaurant(t, cust)
	o := placeOrder(t, cust, rest, m, fmt.Sprintf("l-%d", time.Now().UnixNano()))
	r := cust.get("/v1/orders?limit=10")
	if r.Status != http.StatusOK {
		t.Fatalf("list: %d %s", r.Status, r.Body)
	}
	var p page[order]
	r.JSON(t, &p)
	found := false
	for _, it := range p.Items {
		if it.ID == o.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("order %d missing from the customer's list", o.ID)
	}
	d := cust.get(fmt.Sprintf("/v1/orders/%d", o.ID))
	if d.Status != http.StatusOK {
		t.Fatalf("detail: %d %s", d.Status, d.Body)
	}
	var detail order
	d.JSON(t, &detail)
	if len(detail.Events) == 0 || detail.Events[0].To != "PLACED" {
		t.Fatalf("detail events = %s, want the PLACED event", d.Body)
	}
	other, _ := signup(t, "customer", 0)
	if r := other.get(fmt.Sprintf("/v1/orders/%d", o.ID)); r.Status != http.StatusNotFound && r.Status != http.StatusForbidden {
		t.Fatalf("another customer reading the order: %d, want 404 or 403", r.Status)
	}
}
