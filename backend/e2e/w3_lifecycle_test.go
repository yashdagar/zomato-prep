package e2e_test

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type cast struct {
	customer, staff, rider *client
}

func newCast(t *testing.T) cast {
	t.Helper()
	cust, _ := signup(t, "customer", 0)
	staff, _ := signup(t, "restaurant", 1)
	rider, _ := signup(t, "rider", 0)
	return cast{cust, staff, rider}
}

func (c cast) place(t *testing.T) order {
	t.Helper()
	return expectOrder(t, placeRaw(c.customer, key("life"), orderBody(farmhouseLine(1))), http.StatusCreated)
}

func (c cast) actor(role string) *client {
	switch role {
	case "customer":
		return c.customer
	case "restaurant":
		return c.staff
	}
	return c.rider
}

var eventSeq atomic.Int64

func eventKey() string {
	return fmt.Sprintf("e%d-%d", time.Now().UnixNano()%1e12, eventSeq.Add(1))
}

func send(t *testing.T, c *client, orderID int64, event string) resp {
	t.Helper()
	return c.post(fmt.Sprintf("/v1/orders/%d/events", orderID), map[string]any{"event_id": eventKey(), "event": event})
}

var allowedBy = map[string]string{
	"ACCEPT": "restaurant", "REJECT": "restaurant", "START_PREP": "restaurant", "MARK_READY": "restaurant",
	"PICK_UP": "rider", "DELIVER": "rider", "CANCEL": "customer",
}

var transitions = map[string]map[string]string{
	"PLACED":    {"ACCEPT": "ACCEPTED", "REJECT": "REJECTED", "CANCEL": "CANCELLED"},
	"ACCEPTED":  {"START_PREP": "PREPARING", "CANCEL": "CANCELLED"},
	"PREPARING": {"MARK_READY": "READY"},
	"READY":     {"PICK_UP": "PICKED_UP"},
	"PICKED_UP": {"DELIVER": "DELIVERED"},
	"DELIVERED": {}, "CANCELLED": {}, "REJECTED": {},
}

var pathTo = map[string][]string{
	"PLACED":    {},
	"ACCEPTED":  {"ACCEPT"},
	"PREPARING": {"ACCEPT", "START_PREP"},
	"READY":     {"ACCEPT", "START_PREP", "MARK_READY"},
	"PICKED_UP": {"ACCEPT", "START_PREP", "MARK_READY", "PICK_UP"},
	"DELIVERED": {"ACCEPT", "START_PREP", "MARK_READY", "PICK_UP", "DELIVER"},
	"CANCELLED": {"CANCEL"},
	"REJECTED":  {"REJECT"},
}

func (c cast) orderAt(t *testing.T, status string) order {
	t.Helper()
	o := c.place(t)
	for _, ev := range pathTo[status] {
		r := send(t, c.actor(allowedBy[ev]), o.ID, ev)
		o = expectOrder(t, r, http.StatusOK)
	}
	if o.Status != status {
		t.Fatalf("driving an order to %s ended at %s", status, o.Status)
	}
	return o
}

func TestW3B5_EveryAllowedTransitionMovesTheOrderAndRaisesItsVersion(t *testing.T) {
	c := newCast(t)
	for from, evs := range transitions {
		for ev, to := range evs {
			o := c.orderAt(t, from)
			after := expectOrder(t, send(t, c.actor(allowedBy[ev]), o.ID, ev), http.StatusOK)
			if after.Status != to || after.Version != o.Version+1 {
				t.Fatalf("%s on %s: status %s version %d, want %s and version %d", ev, from, after.Status, after.Version, to, o.Version+1)
			}
		}
	}
}

func TestW3B5_AnEventFromTheWrongRoleIsForbidden(t *testing.T) {
	c := newCast(t)
	for ev, role := range allowedBy {
		o := c.place(t)
		for _, other := range []string{"customer", "restaurant", "rider"} {
			if other == role {
				continue
			}
			expectError(t, send(t, c.actor(other), o.ID, ev), http.StatusForbidden, "forbidden", "")
		}
	}
}

func TestW3B5_AnEventTheCurrentStatusDoesNotAllowIsIllegal(t *testing.T) {
	c := newCast(t)
	for from, evs := range transitions {
		o := c.orderAt(t, from)
		for ev, role := range allowedBy {
			if _, ok := evs[ev]; ok {
				continue
			}
			expectError(t, send(t, c.actor(role), o.ID, ev), http.StatusConflict, "illegal_transition", "")
		}
	}
}

func TestW3B5_RepeatingAnEventIDReturnsTheOrderUnchanged(t *testing.T) {
	c := newCast(t)
	o := c.place(t)
	body := map[string]any{"event_id": eventKey(), "event": "ACCEPT"}
	path := fmt.Sprintf("/v1/orders/%d/events", o.ID)
	first := expectOrder(t, c.staff.post(path, body), http.StatusOK)
	again := expectOrder(t, c.staff.post(path, body), http.StatusOK)
	if again.Version != first.Version || again.Status != "ACCEPTED" {
		t.Fatalf("repeated event: version %d status %s, want %d and ACCEPTED", again.Version, again.Status, first.Version)
	}
}

func TestW3B5_AnEventIDCannotBeReusedOnAnotherOrder(t *testing.T) {
	c := newCast(t)
	a, b := c.place(t), c.place(t)
	id := eventKey()
	c.staff.post(fmt.Sprintf("/v1/orders/%d/events", a.ID), map[string]any{"event_id": id, "event": "ACCEPT"})
	expectError(t, c.staff.post(fmt.Sprintf("/v1/orders/%d/events", b.ID), map[string]any{"event_id": id, "event": "ACCEPT"}), http.StatusConflict, "event_id_conflict", "")
}

func TestW3B5_OfTwoRacingEventsExactlyOneWins(t *testing.T) {
	c := newCast(t)
	staff2, _ := signup(t, "restaurant", 1)
	for range 5 {
		o := c.place(t)
		codes := make([]int, 2)
		bodies := make([]string, 2)
		var wg sync.WaitGroup
		for i, s := range []*client{c.staff, staff2} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				r := send(t, s, o.ID, "ACCEPT")
				codes[i], bodies[i] = r.Status, string(r.Body)
			}()
		}
		wg.Wait()
		ok := 0
		for i, code := range codes {
			switch {
			case code == http.StatusOK:
				ok++
			case code == http.StatusConflict && (strings.Contains(bodies[i], "concurrent_update") || strings.Contains(bodies[i], "illegal_transition")):
			default:
				t.Fatalf("racing ACCEPT got %d %s, want 200 or 409 concurrent_update", code, bodies[i])
			}
		}
		var after order
		c.customer.get(fmt.Sprintf("/v1/orders/%d", o.ID)).JSON(t, &after)
		if ok != 1 || after.Version != o.Version+1 {
			t.Fatalf("two racing ACCEPTs: %d succeeded, version %d; want exactly one and version %d", ok, after.Version, o.Version+1)
		}
	}
}

func TestW3B5_TheDetailListsEveryEventOldestFirst(t *testing.T) {
	c := newCast(t)
	o := c.orderAt(t, "READY")
	r := c.customer.get(fmt.Sprintf("/v1/orders/%d", o.ID))
	var d order
	r.JSON(t, &d)
	var got []string
	for _, e := range d.Events {
		got = append(got, e.From+">"+e.To)
	}
	want := ">PLACED,PLACED>ACCEPTED,ACCEPTED>PREPARING,PREPARING>READY"
	if strings.Join(got, ",") != want {
		t.Fatalf("events = %v, want %s", got, want)
	}
}

func TestW3B5_EveryAppliedEventIsRecordedWithAnOutboxRow(t *testing.T) {
	db := apiDB(t)
	c := newCast(t)
	o := c.orderAt(t, "PICKED_UP")
	events := count(t, db, "SELECT COUNT(*) FROM order_events WHERE order_id = ?", o.ID)
	outbox := count(t, db, "SELECT COUNT(*) FROM outbox WHERE key_ = ?", fmt.Sprint(o.ID))
	if events != 5 || outbox != 5 {
		t.Fatalf("order_events %d and outbox rows %d for an order that reached PICKED_UP, want 5 and 5", events, outbox)
	}
	send(t, c.staff, o.ID, "ACCEPT")
	if n := count(t, db, "SELECT COUNT(*) FROM outbox WHERE key_ = ?", fmt.Sprint(o.ID)); n != 5 {
		t.Fatal("a rejected event wrote an outbox row")
	}
}

func TestW3B5_OrdersAreVisibleOnlyToTheirCustomerRestaurantAndRiders(t *testing.T) {
	c := newCast(t)
	o := c.place(t)
	path := fmt.Sprintf("/v1/orders/%d", o.ID)
	otherCustomer, _ := signup(t, "customer", 0)
	otherStaff, _ := signup(t, "restaurant", 2)
	for name, cl := range map[string]*client{"customer": c.customer, "its restaurant": c.staff, "a rider": c.rider} {
		if r := cl.get(path); r.Status != http.StatusOK {
			t.Fatalf("%s reading the order: %d %s", name, r.Status, r.Body)
		}
	}
	expectError(t, otherCustomer.get(path), http.StatusNotFound, "order_not_found", "")
	expectError(t, otherStaff.get(path), http.StatusNotFound, "order_not_found", "")
	expectError(t, send(t, otherStaff, o.ID, "ACCEPT"), http.StatusNotFound, "order_not_found", "")
	expectError(t, c.customer.get("/v1/orders/999999999"), http.StatusNotFound, "order_not_found", "")
	expectError(t, c.customer.get("/v1/orders/abc"), http.StatusBadRequest, "invalid_parameter", "id")
}

func TestW3B5_AnEventRequestWithBadFieldsIs400(t *testing.T) {
	c := newCast(t)
	o := c.place(t)
	path := fmt.Sprintf("/v1/orders/%d/events", o.ID)
	expectError(t, c.staff.post(path, map[string]any{"event_id": "e1", "event": "FLY"}), http.StatusBadRequest, "invalid_parameter", "event")
	expectError(t, c.staff.post(path, map[string]any{"event_id": strings.Repeat("e", 37), "event": "ACCEPT"}), http.StatusBadRequest, "invalid_parameter", "event_id")
	expectError(t, c.staff.post(path, map[string]any{"event": "ACCEPT"}), http.StatusBadRequest, "invalid_parameter", "event_id")
	expectError(t, rawRequest(t, c.staff, http.MethodPost, path, `{"event":`), http.StatusBadRequest, "invalid_body", "")
}

func TestW3B5_TheOrderListPagesTheCallersOrdersNewestFirst(t *testing.T) {
	c := newCast(t)
	want := map[int64]bool{}
	for range 5 {
		want[c.place(t).ID] = true
		time.Sleep(5 * time.Millisecond)
	}
	other, _ := signup(t, "customer", 0)
	expectOrder(t, placeRaw(other, key("other"), orderBody(farmhouseLine(1))), http.StatusCreated)
	seen := map[int64]int{}
	var last int64
	cursor := ""
	for n := 0; n < 10; n++ {
		path := "/v1/orders?limit=2"
		if cursor != "" {
			path += "&cursor=" + url.QueryEscape(cursor)
		}
		r := c.customer.get(path)
		if r.Status != http.StatusOK {
			t.Fatalf("%s: %d %s", path, r.Status, r.Body)
		}
		var p page[order]
		r.JSON(t, &p)
		if len(p.Items) > 2 || (p.NextCursor != "" && len(p.Items) != 2) {
			t.Fatalf("limit=2 page has %d orders", len(p.Items))
		}
		for _, o := range p.Items {
			if last != 0 && o.ID > last {
				t.Fatalf("order %d came after %d, want newest first", o.ID, last)
			}
			last = o.ID
			seen[o.ID]++
		}
		if p.NextCursor == "" {
			break
		}
		cursor = p.NextCursor
	}
	if len(seen) != 5 {
		t.Fatalf("pages held %d orders, want only this customer's 5", len(seen))
	}
	for id := range want {
		if seen[id] != 1 {
			t.Fatalf("order %d seen %d times, want once", id, seen[id])
		}
	}
	expectError(t, c.customer.get("/v1/orders?limit=abc"), http.StatusBadRequest, "invalid_parameter", "limit")
	expectError(t, c.customer.get("/v1/orders?cursor=forged"), http.StatusBadRequest, "invalid_cursor", "")
}
