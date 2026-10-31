package e2e_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
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

func TestW3B1_LoginReturnsAWorkingTokenPair(t *testing.T) {
	c := newClient(t)
	email := uniqueEmail("login")
	if r := c.post("/v1/auth/signup", map[string]any{"email": email, "password": "correct horse battery staple"}); r.Status != http.StatusCreated {
		t.Fatalf("signup: %d %s, want 201", r.Status, r.Body)
	}
	r := c.post("/v1/auth/login", map[string]any{"email": email, "password": "correct horse battery staple"})
	if r.Status != http.StatusOK {
		t.Fatalf("login: %d %s, want 200", r.Status, r.Body)
	}
	var body struct {
		Access    string `json:"access_token"`
		Refresh   string `json:"refresh_token"`
		TokenType string `json:"token_type"`
		ExpiresIn int    `json:"expires_in"`
	}
	r.JSON(t, &body)
	if body.Access == "" || body.TokenType != "Bearer" || body.ExpiresIn != 900 {
		t.Fatalf("login body = %s, want an access_token, token_type Bearer and expires_in 900", r.Body)
	}
	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(body.Refresh) {
		t.Fatalf("refresh_token = %q, want 32 random bytes as 64 hex characters", body.Refresh)
	}
	if r := c.post("/v1/auth/refresh", map[string]string{"refresh_token": body.Refresh}); r.Status != http.StatusOK {
		t.Fatalf("refresh with the login token: %d %s, want 200", r.Status, r.Body)
	}
}

func TestW3B1_WrongPasswordAndUnknownEmailGetTheSameAnswer(t *testing.T) {
	c := newClient(t)
	email := uniqueEmail("enum")
	c.post("/v1/auth/signup", map[string]any{"email": email, "password": "correct horse battery staple"})
	wrong := c.post("/v1/auth/login", map[string]any{"email": email, "password": "wrong password"})
	unknown := c.post("/v1/auth/login", map[string]any{"email": uniqueEmail("ghost"), "password": "wrong password"})
	for name, r := range map[string]resp{"wrong password": wrong, "unknown email": unknown} {
		var body map[string]string
		r.JSON(t, &body)
		if r.Status != http.StatusUnauthorized || body["error"] != "invalid_credentials" {
			t.Fatalf("%s: %d %s, want 401 with error invalid_credentials", name, r.Status, r.Body)
		}
	}
}

func TestW3B1_DuplicateEmailIsAConflict(t *testing.T) {
	c := newClient(t)
	body := map[string]any{"email": uniqueEmail("dup"), "password": "correct horse battery staple"}
	c.post("/v1/auth/signup", body)
	if r := c.post("/v1/auth/signup", body); r.Status != http.StatusConflict {
		t.Fatalf("second signup with the same email: %d %s, want 409", r.Status, r.Body)
	}
}

func TestW3B1_SignupRejectsInvalidInput(t *testing.T) {
	cases := map[string]map[string]any{
		"malformed email":           {"email": "not-an-email", "password": "correct horse battery staple"},
		"empty password":            {"email": uniqueEmail("empty"), "password": ""},
		"password over 72 bytes":    {"email": uniqueEmail("long"), "password": strings.Repeat("a", 73)},
		"unknown role":              {"email": uniqueEmail("role"), "password": "correct horse battery staple", "role": "admin"},
		"restaurant without its id": {"email": uniqueEmail("rest"), "password": "correct horse battery staple", "role": "restaurant"},
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			r := newClient(t).post("/v1/auth/signup", body)
			var got map[string]string
			if r.Status != http.StatusBadRequest || json.Unmarshal(r.Body, &got) != nil || got["error"] == "" {
				t.Fatalf("%d %s, want 400 with a JSON error field", r.Status, r.Body)
			}
		})
	}
}

func TestW3B2_MeReturnsTheCallersAccount(t *testing.T) {
	c := newClient(t)
	email := uniqueEmail("me")
	r := c.post("/v1/auth/signup", map[string]any{"email": email, "password": "correct horse battery staple"})
	var tk tokens
	r.JSON(t, &tk)
	c.token = tk.Access
	me := c.get("/v1/me")
	var got map[string]any
	me.JSON(t, &got)
	rid, present := got["restaurant_id"]
	if me.Status != http.StatusOK || got["email"] != email || got["role"] != "customer" || !present || rid != nil {
		t.Fatalf("me = %d %s, want 200 with this email, role customer and restaurant_id null", me.Status, me.Body)
	}
}

func TestW3B2_InvalidBearerTokenIsUnauthorized(t *testing.T) {
	c := newClient(t)
	c.token = "not-a-jwt"
	if r := c.get("/v1/me"); r.Status != http.StatusUnauthorized {
		t.Fatalf("garbage token: %d, want 401", r.Status)
	}
}

func TestW3B2_PublicRoutesNeedNoToken(t *testing.T) {
	c := newClient(t)
	if r := c.get("/healthz"); r.Status != http.StatusOK {
		t.Fatalf("healthz without a token: %d, want 200", r.Status)
	}
	r := c.post("/v1/auth/login", map[string]any{"email": uniqueEmail("public"), "password": "x"})
	var body map[string]string
	r.JSON(t, &body)
	if body["error"] == "unauthorized" {
		t.Fatalf("login without a token was stopped by the auth middleware: %s", r.Body)
	}
}

func TestW3B4_TotalIsPricedOnTheServer(t *testing.T) {
	c, _ := signup(t, "customer", 0)
	rest, m := firstRestaurant(t, c)
	line := validLine(t, m)
	line["qty"] = 2
	q := c.post("/v1/menu/price", line)
	var quote struct {
		UnitPaise int64 `json:"unit_paise"`
	}
	q.JSON(t, &quote)
	r := c.post("/v1/orders", map[string]any{"restaurant_id": rest.ID, "lines": []any{line}, "total_paise": 1}, "Idempotency-Key", fmt.Sprintf("tot-%d", time.Now().UnixNano()))
	var o order
	r.JSON(t, &o)
	if r.Status != http.StatusCreated || o.TotalPaise != 2*quote.UnitPaise {
		t.Fatalf("order = %d %s, want 201 and total_paise = 2 x unit price %d (a client total is ignored)", r.Status, r.Body, quote.UnitPaise)
	}
}

func TestW3B4_RejectedOrderReleasesTheKey(t *testing.T) {
	c, _ := signup(t, "customer", 0)
	rest, m := firstRestaurant(t, c)
	key := fmt.Sprintf("rel-%d", time.Now().UnixNano())
	bad := validLine(t, m)
	bad["size"] = "XXL"
	r := c.post("/v1/orders", map[string]any{"restaurant_id": rest.ID, "lines": []any{bad}}, "Idempotency-Key", key)
	var body struct {
		Error  string   `json:"error"`
		Errors []string `json:"errors"`
	}
	r.JSON(t, &body)
	if r.Status != http.StatusBadRequest || len(body.Errors) == 0 {
		t.Fatalf("unknown size: %d %s, want 400 with the pricing errors listed", r.Status, r.Body)
	}
	if o := placeOrder(t, c, rest, m, key); o.ID == 0 {
		t.Fatal("a valid retry with the same key must create the order once the failed attempt released it")
	}
}

func TestW3B4_OnlyCustomersPlaceOrders(t *testing.T) {
	cust, _ := signup(t, "customer", 0)
	rest, m := firstRestaurant(t, cust)
	rider, _ := signup(t, "rider", 0)
	r := rider.post("/v1/orders", map[string]any{"restaurant_id": rest.ID, "lines": []any{validLine(t, m)}}, "Idempotency-Key", fmt.Sprintf("rider-%d", time.Now().UnixNano()))
	if r.Status != http.StatusForbidden {
		t.Fatalf("rider placing an order: %d, want 403", r.Status)
	}
}

func TestW3B4_KeysAreScopedPerCustomer(t *testing.T) {
	a, _ := signup(t, "customer", 0)
	b, _ := signup(t, "customer", 0)
	rest, m := firstRestaurant(t, a)
	key := fmt.Sprintf("shared-%d", time.Now().UnixNano())
	if placeOrder(t, a, rest, m, key).ID == placeOrder(t, b, rest, m, key).ID {
		t.Fatal("two customers using the same Idempotency-Key must get two different orders")
	}
}

func TestW3B5_ConcurrentAcceptsProduceOneSuccess(t *testing.T) {
	cust, _ := signup(t, "customer", 0)
	rest, m := firstRestaurant(t, cust)
	staff1, _ := signup(t, "restaurant", rest.ID)
	staff2, _ := signup(t, "restaurant", rest.ID)
	o := placeOrder(t, cust, rest, m, fmt.Sprintf("cc-%d", time.Now().UnixNano()))
	codes := make(chan int, 2)
	for i, c := range []*client{staff1, staff2} {
		go func() {
			codes <- c.post(fmt.Sprintf("/v1/orders/%d/events", o.ID), map[string]any{"event_id": fmt.Sprintf("acc-%d-%d", o.ID, i), "event": "ACCEPT"}).Status
		}()
	}
	a, b := <-codes, <-codes
	if !((a == http.StatusOK && b == http.StatusConflict) || (a == http.StatusConflict && b == http.StatusOK)) {
		t.Fatalf("two concurrent ACCEPTs returned %d and %d, want one 200 and one 409", a, b)
	}
	var after order
	cust.get(fmt.Sprintf("/v1/orders/%d", o.ID)).JSON(t, &after)
	if after.Version != o.Version+1 {
		t.Fatalf("version after two concurrent ACCEPTs = %d, want exactly one increment from %d", after.Version, o.Version)
	}
}

func TestW3B5_AnotherRestaurantCannotSeeTheOrder(t *testing.T) {
	cust, _ := signup(t, "customer", 0)
	rest, m := firstRestaurant(t, cust)
	other, _ := signup(t, "restaurant", rest.ID+1)
	o := placeOrder(t, cust, rest, m, fmt.Sprintf("vis-%d", time.Now().UnixNano()))
	if r := other.get(fmt.Sprintf("/v1/orders/%d", o.ID)); r.Status != http.StatusNotFound {
		t.Fatalf("another restaurant reading the order: %d, want 404", r.Status)
	}
	if r := transition(t, other, o.ID, "ACCEPT"); r.Status != http.StatusNotFound {
		t.Fatalf("another restaurant accepting the order: %d, want 404", r.Status)
	}
}

func TestW3B5_EventIDCannotBeReusedOnAnotherOrder(t *testing.T) {
	cust, _ := signup(t, "customer", 0)
	rest, m := firstRestaurant(t, cust)
	staff, _ := signup(t, "restaurant", rest.ID)
	first := placeOrder(t, cust, rest, m, fmt.Sprintf("ra-%d", time.Now().UnixNano()))
	second := placeOrder(t, cust, rest, m, fmt.Sprintf("rb-%d", time.Now().UnixNano()))
	id := fmt.Sprintf("reuse-%d", time.Now().UnixNano())
	staff.post(fmt.Sprintf("/v1/orders/%d/events", first.ID), map[string]any{"event_id": id, "event": "ACCEPT"})
	r := staff.post(fmt.Sprintf("/v1/orders/%d/events", second.ID), map[string]any{"event_id": id, "event": "ACCEPT"})
	if r.Status != http.StatusConflict {
		t.Fatalf("same event_id on a second order: %d, want 409", r.Status)
	}
}

func TestW3B5_OrderListPagesNewestFirstWithACursor(t *testing.T) {
	cust, _ := signup(t, "customer", 0)
	rest, m := firstRestaurant(t, cust)
	want := map[int64]bool{}
	for i := 0; i < 3; i++ {
		want[placeOrder(t, cust, rest, m, fmt.Sprintf("pg-%d-%d", time.Now().UnixNano(), i)).ID] = true
	}
	seen := map[int64]int{}
	var last int64
	cursor := ""
	for n := 0; n < 5; n++ {
		path := "/v1/orders?limit=2"
		if cursor != "" {
			path += "&cursor=" + cursor
		}
		var p page[order]
		cust.get(path).JSON(t, &p)
		if len(p.Items) > 2 {
			t.Fatalf("limit=2 returned %d orders", len(p.Items))
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
	for id := range want {
		if seen[id] != 1 {
			t.Fatalf("order %d seen %d times across pages, want exactly once", id, seen[id])
		}
	}
	if len(seen) != 3 {
		t.Fatalf("pages held %d orders, want only this customer's 3", len(seen))
	}
}
