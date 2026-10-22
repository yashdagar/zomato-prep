// Package e2e holds black-box tests that run against a live API. They skip
// unless TEST_API_URL is set, e.g. TEST_API_URL=http://localhost:8080.
package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func baseURL(t *testing.T) string {
	t.Helper()
	u := os.Getenv("TEST_API_URL")
	if u == "" {
		t.Skip("set TEST_API_URL=http://localhost:8080 to run")
	}
	return strings.TrimRight(u, "/")
}

type resp struct {
	Status int
	Header http.Header
	Body   []byte
}

func (r resp) JSON(t *testing.T, v any) {
	t.Helper()
	if err := json.Unmarshal(r.Body, v); err != nil {
		t.Fatalf("decode %s: %v", r.Body, err)
	}
}

type client struct {
	t     *testing.T
	base  string
	token string
	http  *http.Client
}

func newClient(t *testing.T) *client {
	t.Helper()
	return &client{t: t, base: baseURL(t), http: &http.Client{Timeout: 10 * time.Second}}
}

func (c *client) do(method, path string, body any, headers ...string) resp {
	c.t.Helper()
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			c.t.Fatal(err)
		}
		rd = bytes.NewReader(b)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, rd)
	if err != nil {
		c.t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	res, err := c.http.Do(req)
	if err != nil {
		c.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = res.Body.Close() }()
	b, _ := io.ReadAll(res.Body)
	return resp{Status: res.StatusCode, Header: res.Header, Body: b}
}

func (c *client) get(path string, headers ...string) resp {
	return c.do(http.MethodGet, path, nil, headers...)
}

func (c *client) post(path string, body any, headers ...string) resp {
	return c.do(http.MethodPost, path, body, headers...)
}

type tokens struct {
	Access  string `json:"access_token"`
	Refresh string `json:"refresh_token"`
}

func uniqueEmail(prefix string) string {
	return fmt.Sprintf("%s-%d@test.local", prefix, time.Now().UnixNano())
}

// signup creates a user and returns a client that sends its access token.
// The signup body accepts an optional role (customer, restaurant, rider)
// and restaurant_id so tests can create every role; production would
// restrict that to an admin.
func signup(t *testing.T, role string, restaurantID int64) (*client, tokens) {
	t.Helper()
	c := newClient(t)
	body := map[string]any{"email": uniqueEmail(role), "password": "correct horse battery staple", "role": role}
	if restaurantID != 0 {
		body["restaurant_id"] = restaurantID
	}
	r := c.post("/v1/auth/signup", body)
	if r.Status != http.StatusOK && r.Status != http.StatusCreated {
		t.Fatalf("signup %s: %d %s", role, r.Status, r.Body)
	}
	var tk tokens
	r.JSON(t, &tk)
	if tk.Access == "" || tk.Refresh == "" {
		t.Fatalf("signup returned %s, want access_token and refresh_token", r.Body)
	}
	c.token = tk.Access
	return c, tk
}

type restaurant struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type page[T any] struct {
	Items      []T    `json:"items"`
	NextCursor string `json:"next_cursor"`
}

type menuOption struct {
	ID      int64            `json:"id"`
	Name    string           `json:"name"`
	InStock bool             `json:"in_stock"`
	Prices  map[string]int64 `json:"prices"`
}

type menuGroup struct {
	ID        int64        `json:"id"`
	Name      string       `json:"name"`
	MinSelect int          `json:"min_select"`
	MaxSelect int          `json:"max_select"`
	Options   []menuOption `json:"options"`
}

type menuSize struct {
	Size       string `json:"size"`
	PricePaise int64  `json:"price_paise"`
}

type menuItem struct {
	ID     int64       `json:"id"`
	Name   string      `json:"name"`
	Sizes  []menuSize  `json:"sizes"`
	Groups []menuGroup `json:"groups"`
}

type menu struct {
	Items []menuItem `json:"items"`
}

func (o menuOption) availableIn(size string) bool {
	if !o.InStock {
		return false
	}
	_, any := o.Prices["*"]
	_, exact := o.Prices[size]
	return any || exact
}

// firstRestaurant returns a restaurant near the seed point and its menu.
func firstRestaurant(t *testing.T, c *client) (restaurant, menu) {
	t.Helper()
	r := c.get("/v1/restaurants?lat=28.46&lng=77.03&radius_km=5&limit=5")
	if r.Status != http.StatusOK {
		t.Fatalf("list restaurants: %d %s", r.Status, r.Body)
	}
	var p page[restaurant]
	r.JSON(t, &p)
	if len(p.Items) == 0 {
		t.Fatal("no restaurants within 5 km of 28.46,77.03; run the seed (W2 B3)")
	}
	for _, rest := range p.Items {
		mr := c.get(fmt.Sprintf("/v1/restaurants/%d/menu", rest.ID))
		if mr.Status != http.StatusOK {
			continue
		}
		var m menu
		mr.JSON(t, &m)
		if len(m.Items) > 0 && len(m.Items[0].Sizes) > 0 {
			return rest, m
		}
	}
	t.Fatal("no restaurant with a menu found; the seed should give one a full menu")
	return restaurant{}, menu{}
}

// validLine picks the first item and size and satisfies every group's
// min_select with options available in that size.
func validLine(t *testing.T, m menu) map[string]any {
	t.Helper()
	item := m.Items[0]
	size := item.Sizes[0].Size
	chosen := map[string][]int64{}
	for _, g := range item.Groups {
		var picks []int64
		for _, o := range g.Options {
			if len(picks) >= g.MinSelect {
				break
			}
			if o.availableIn(size) {
				picks = append(picks, o.ID)
			}
		}
		if len(picks) < g.MinSelect {
			t.Fatalf("group %q needs %d options available in size %s, seed has fewer", g.Name, g.MinSelect, size)
		}
		if len(picks) > 0 {
			chosen[fmt.Sprint(g.ID)] = picks
		}
	}
	return map[string]any{"item_id": item.ID, "size": size, "chosen": chosen, "qty": 1}
}

type order struct {
	ID           int64  `json:"id"`
	Status       string `json:"status"`
	Version      int64  `json:"version"`
	TotalPaise   int64  `json:"total_paise"`
	RestaurantID int64  `json:"restaurant_id"`
	Events       []struct {
		EventID string `json:"event_id"`
		From    string `json:"from_status"`
		To      string `json:"to_status"`
	} `json:"events"`
}

func placeOrder(t *testing.T, c *client, rest restaurant, m menu, key string) order {
	t.Helper()
	r := c.post("/v1/orders", map[string]any{"restaurant_id": rest.ID, "lines": []any{validLine(t, m)}}, "Idempotency-Key", key)
	if r.Status != http.StatusOK && r.Status != http.StatusCreated {
		t.Fatalf("place order: %d %s", r.Status, r.Body)
	}
	var o order
	r.JSON(t, &o)
	if o.ID == 0 || o.Status != "PLACED" {
		t.Fatalf("placed order = %s, want an id and status PLACED", r.Body)
	}
	return o
}

func transition(t *testing.T, c *client, orderID int64, event string) resp {
	t.Helper()
	return c.post(fmt.Sprintf("/v1/orders/%d/events", orderID), map[string]any{"event_id": fmt.Sprintf("%s-%d", event, time.Now().UnixNano()), "event": event})
}

func eventually(t *testing.T, within time.Duration, every time.Duration, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(every)
	}
	return cond()
}
