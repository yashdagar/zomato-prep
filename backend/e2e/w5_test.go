package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func wsURL(base string) string {
	return strings.Replace(strings.Replace(base, "https://", "wss://", 1), "http://", "ws://", 1)
}

func TestW5B2_CustomerSocketReceivesTheAcceptEvent(t *testing.T) {
	cust, tk := signup(t, "customer", 0)
	rest, m := firstRestaurant(t, cust)
	restUser, _ := signup(t, "restaurant", rest.ID)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	ws, _, err := websocket.Dial(ctx, wsURL(cust.base)+"/v1/ws?token="+tk.Access, nil)
	if err != nil {
		t.Fatalf("dial ws: %v", err)
	}
	defer func() { _ = ws.Close(websocket.StatusNormalClosure, "") }()

	o := placeOrder(t, cust, rest, m, fmt.Sprintf("ws-%d", time.Now().UnixNano()))
	if r := transition(t, restUser, o.ID, "ACCEPT"); r.Status != http.StatusOK {
		t.Fatalf("ACCEPT: %d %s", r.Status, r.Body)
	}
	for {
		_, data, err := ws.Read(ctx)
		if err != nil {
			t.Fatalf("no ACCEPTED event for order %d within 15 s: %v", o.ID, err)
		}
		var ev struct {
			OrderID int64  `json:"order_id"`
			To      string `json:"to"`
		}
		if json.Unmarshal(data, &ev) == nil && ev.OrderID == o.ID && ev.To == "ACCEPTED" {
			return
		}
	}
}

func TestW5B1_SocketWithoutTokenIsRejected(t *testing.T) {
	c := newClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ws, res, err := websocket.Dial(ctx, wsURL(c.base)+"/v1/ws", nil)
	if err == nil {
		_ = ws.Close(websocket.StatusNormalClosure, "")
		t.Fatal("dial without a token succeeded, want 401")
	}
	if res != nil && res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", res.StatusCode)
	}
}

func TestW5B3_RiderLocationAndOffline(t *testing.T) {
	rider, _ := signup(t, "rider", 0)
	if r := rider.post("/v1/rider/online", nil); r.Status != http.StatusOK && r.Status != http.StatusNoContent {
		t.Fatalf("online: %d %s", r.Status, r.Body)
	}
	if r := rider.post("/v1/rider/location", map[string]float64{"lat": 28.461, "lng": 77.031}); r.Status != http.StatusOK && r.Status != http.StatusNoContent {
		t.Fatalf("location: %d %s", r.Status, r.Body)
	}
	if r := rider.post("/v1/rider/offline", nil); r.Status != http.StatusOK && r.Status != http.StatusNoContent {
		t.Fatalf("offline: %d %s", r.Status, r.Body)
	}
	cust, _ := signup(t, "customer", 0)
	if r := cust.post("/v1/rider/location", map[string]float64{"lat": 1, "lng": 1}); r.Status != http.StatusForbidden {
		t.Fatalf("customer posting a location: %d, want 403", r.Status)
	}
}

func TestW5D3_TwoRidersOneClaim(t *testing.T) {
	cust, _ := signup(t, "customer", 0)
	rest, m := firstRestaurant(t, cust)
	restUser, _ := signup(t, "restaurant", rest.ID)
	a, _ := signup(t, "rider", 0)
	b, _ := signup(t, "rider", 0)
	for _, r := range []*client{a, b} {
		r.post("/v1/rider/online", nil)
		r.post("/v1/rider/location", map[string]float64{"lat": 28.4605, "lng": 77.0305})
	}
	o := placeOrder(t, cust, rest, m, fmt.Sprintf("claim-%d", time.Now().UnixNano()))
	for _, ev := range []string{"ACCEPT", "START_PREP", "MARK_READY"} {
		if r := transition(t, restUser, o.ID, ev); r.Status != http.StatusOK {
			t.Fatalf("%s: %d %s", ev, r.Status, r.Body)
		}
	}
	accepted, conflicts := 0, 0
	ok := eventually(t, 20*time.Second, 500*time.Millisecond, func() bool {
		for _, r := range []*client{a, b} {
			switch res := r.post(fmt.Sprintf("/v1/orders/%d/accept", o.ID), nil); res.Status {
			case http.StatusOK:
				accepted++
			case http.StatusConflict:
				conflicts++
			}
		}
		return accepted >= 1
	})
	if !ok {
		t.Fatalf("no rider could accept order %d within 20 s; is the worker assigning on READY?", o.ID)
	}
	if accepted != 1 {
		t.Fatalf("accepted=%d conflicts=%d, want exactly one 200", accepted, conflicts)
	}
	if conflicts == 0 {
		t.Fatalf("the other rider never got a 409")
	}
}
