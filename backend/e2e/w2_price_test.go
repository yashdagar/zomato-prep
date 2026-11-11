package e2e_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"testing"
)

const (
	crust    = "1"
	toppings = "2"
	classic  = 1
	burst    = 2
	olives   = 3
	jalapeno = 4
	paneer   = 5
	mushroom = 6
	paprika  = 7
)

type selectionError struct {
	Code     string `json:"code"`
	GroupID  *int64 `json:"group_id"`
	OptionID *int64 `json:"option_id"`
	Message  string `json:"message"`
}

func (e selectionError) key() string {
	g, o := "-", "-"
	if e.GroupID != nil {
		g = fmt.Sprint(*e.GroupID)
	}
	if e.OptionID != nil {
		o = fmt.Sprint(*e.OptionID)
	}
	return e.Code + "/" + g + "/" + o
}

type quote struct {
	Description string           `json:"description"`
	UnitPaise   int64            `json:"unit_paise"`
	TotalPaise  int64            `json:"total_paise"`
	Errors      []selectionError `json:"errors"`
}

func priceLine(size string, qty int, chosen map[string][]int64) map[string]any {
	if chosen == nil {
		chosen = map[string][]int64{}
	}
	return map[string]any{"item_id": 1, "size": size, "chosen": chosen, "qty": qty}
}

func price(t *testing.T, body any) (quote, resp) {
	t.Helper()
	r := newClient(t).post("/v1/menu/price", body)
	if r.Status != http.StatusOK {
		t.Fatalf("POST /v1/menu/price %s: %d %s, want 200", pretty(body), r.Status, r.Body)
	}
	var raw map[string]json.RawMessage
	r.JSON(t, &raw)
	if string(raw["errors"]) == "null" || raw["errors"] == nil {
		t.Fatalf("errors must always be an array, got %s", r.Body)
	}
	var q quote
	r.JSON(t, &q)
	return q, r
}

func expectPrice(t *testing.T, body map[string]any, unit int64) {
	t.Helper()
	q, r := price(t, body)
	if len(q.Errors) != 0 {
		t.Fatalf("%s: errors %s, want none", pretty(body["chosen"]), r.Body)
	}
	if q.UnitPaise != unit || q.TotalPaise != unit*int64(body["qty"].(int)) {
		t.Fatalf("size %s chosen %v qty %v: unit %d total %d, want unit %d total %d", body["size"], body["chosen"], body["qty"], q.UnitPaise, q.TotalPaise, unit, unit*int64(body["qty"].(int)))
	}
	if !strings.HasPrefix(q.Description, "Farmhouse pizza") {
		t.Fatalf("description = %q, want it to start with the item's name", q.Description)
	}
}

func TestW2B6_TheWorkedExamplePricesCorrectly(t *testing.T) {
	expectPrice(t, priceLine("M", 2, map[string][]int64{crust: {burst}, toppings: {olives, paneer}}), 39900+9900+6000)
}

func TestW2B6_OnlyTheCheapestToppingIsFree(t *testing.T) {
	expectPrice(t, priceLine("L", 1, map[string][]int64{crust: {classic}, toppings: {paneer, mushroom, paprika}}), 59900+6000+4500)
	expectPrice(t, priceLine("R", 1, map[string][]int64{crust: {classic}, toppings: {mushroom}}), 19900)
	expectPrice(t, priceLine("M", 1, map[string][]int64{crust: {burst}}), 39900+9900)
}

func TestW2B6_StarPricesApplyToEverySize(t *testing.T) {
	for size, base := range map[string]int64{"R": 19900, "M": 39900, "L": 59900} {
		expectPrice(t, priceLine(size, 1, map[string][]int64{crust: {classic}, toppings: {olives, paneer}}), base+6000)
	}
}

func TestW2B6_AnExactSizePriceWinsOverTheStarPrice(t *testing.T) {
	db := apiDB(t)
	mustExec(t, db, "INSERT INTO option_prices (option_id, size, price_paise) VALUES (?, 'L', 5000)", olives)
	t.Cleanup(func() { _, _ = db.Exec("DELETE FROM option_prices WHERE option_id = ? AND size = 'L'", olives) })
	expectPrice(t, priceLine("L", 1, map[string][]int64{crust: {classic}, toppings: {olives, jalapeno}}), 59900+5000)
	expectPrice(t, priceLine("M", 1, map[string][]int64{crust: {classic}, toppings: {olives, paneer}}), 39900+6000)
}

func TestW2B6_QuantityMultipliesTheUnitPriceUpToTwenty(t *testing.T) {
	expectPrice(t, priceLine("R", 3, map[string][]int64{crust: {classic}}), 19900)
	expectPrice(t, priceLine("R", 20, map[string][]int64{crust: {classic}}), 19900)
}

func TestW2B6_EverySelectionProblemIsReportedWithItsCode(t *testing.T) {
	ptr := func(v int64) *int64 { return &v }
	e := func(code string, g, o int64) string {
		se := selectionError{Code: code}
		if g != 0 {
			se.GroupID = ptr(g)
		}
		if o != 0 {
			se.OptionID = ptr(o)
		}
		return se.key()
	}
	cases := []struct {
		name string
		body map[string]any
		want []string
	}{
		{"size not offered", priceLine("XL", 1, map[string][]int64{crust: {classic}}), []string{e("size_not_offered", 0, 0)}},
		{"option not priced for the size", priceLine("R", 1, map[string][]int64{crust: {burst}}), []string{e("option_unavailable", 1, burst)}},
		{"two crusts", priceLine("M", 1, map[string][]int64{crust: {classic, burst}}), []string{e("selection_count", 1, 0)}},
		{"no crust", priceLine("M", 1, map[string][]int64{toppings: {olives}}), []string{e("selection_count", 1, 0)}},
		{"duplicate topping", priceLine("M", 1, map[string][]int64{crust: {classic}, toppings: {olives, olives}}), []string{e("duplicate_option", 2, olives)}},
		{"unknown topping", priceLine("M", 1, map[string][]int64{crust: {classic}, toppings: {99}}), []string{e("unknown_option", 2, 99)}},
		{"topping chosen as a crust", priceLine("M", 1, map[string][]int64{crust: {olives}}), []string{e("unknown_option", 1, olives)}},
		{"group of another item", priceLine("M", 1, map[string][]int64{crust: {classic}, "99": {1}}), []string{e("unknown_group", 99, 0)}},
		{"six toppings", priceLine("M", 1, map[string][]int64{crust: {classic}, toppings: {olives, jalapeno, paneer, mushroom, paprika, olives}}), []string{e("selection_count", 2, 0), e("duplicate_option", 2, olives)}},
		{"everything at once", priceLine("XL", 1, map[string][]int64{crust: {classic, burst}, toppings: {olives, olives, 99}, "99": {1}}), []string{
			e("size_not_offered", 0, 0), e("selection_count", 1, 0), e("duplicate_option", 2, olives), e("unknown_option", 2, 99), e("unknown_group", 99, 0),
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q, r := price(t, tc.body)
			var got []string
			for _, se := range q.Errors {
				if strings.TrimSpace(se.Message) == "" {
					t.Fatalf("every error needs a message for people: %s", r.Body)
				}
				got = append(got, se.key())
			}
			sort.Strings(got)
			want := append([]string(nil), tc.want...)
			sort.Strings(want)
			if fmt.Sprint(got) != fmt.Sprint(want) {
				t.Fatalf("errors (code/group/option) = %v, want %v\n%s", got, want, r.Body)
			}
			if q.UnitPaise != 0 || q.TotalPaise != 0 {
				t.Fatalf("an invalid selection was priced at unit %d total %d, want 0 and 0", q.UnitPaise, q.TotalPaise)
			}
		})
	}
}

func TestW2B6_AnOutOfStockOptionIsUnavailable(t *testing.T) {
	db := apiDB(t)
	mustExec(t, db, "UPDATE options SET in_stock = 0 WHERE id = ?", paneer)
	t.Cleanup(func() { _, _ = db.Exec("UPDATE options SET in_stock = 1 WHERE id = ?", paneer) })
	q, r := price(t, priceLine("M", 1, map[string][]int64{crust: {classic}, toppings: {paneer}}))
	if len(q.Errors) != 1 || q.Errors[0].Code != "option_unavailable" || q.Errors[0].OptionID == nil || *q.Errors[0].OptionID != paneer {
		t.Fatalf("out of stock Paneer: %s, want one option_unavailable error for option %d", r.Body, paneer)
	}
}

func TestW2B6_AnUnknownItemIs404(t *testing.T) {
	r := newClient(t).post("/v1/menu/price", map[string]any{"item_id": 999999999, "size": "M", "chosen": map[string][]int64{}, "qty": 1})
	expectError(t, r, http.StatusNotFound, "item_not_found", "")
}

func TestW2B6_ABodyThatIsNotOneValidObjectIs400(t *testing.T) {
	cases := map[string]string{
		"not json":      `just text`,
		"an array":      `[{"item_id":1,"size":"M","chosen":{},"qty":1}]`,
		"unknown field": `{"item_id":1,"size":"M","chosen":{},"qty":1,"discount":100}`,
		"wrong type":    `{"item_id":"one","size":"M","chosen":{},"qty":1}`,
		"two objects":   `{"item_id":1,"size":"M","chosen":{},"qty":1}{"item_id":1}`,
		"cut off":       `{"item_id":1,"size":"M"`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			expectError(t, postRaw(t, "/v1/menu/price", body), http.StatusBadRequest, "invalid_body", "")
		})
	}
}

func TestW2B6_FieldsOutOfRangeAre400WithTheFieldNamed(t *testing.T) {
	cases := []struct {
		name, field string
		body        map[string]any
	}{
		{"item_id zero", "item_id", map[string]any{"item_id": 0, "size": "M", "chosen": map[string][]int64{}, "qty": 1}},
		{"item_id missing", "item_id", map[string]any{"size": "M", "chosen": map[string][]int64{}, "qty": 1}},
		{"size empty", "size", map[string]any{"item_id": 1, "size": "", "chosen": map[string][]int64{}, "qty": 1}},
		{"size missing", "size", map[string]any{"item_id": 1, "chosen": map[string][]int64{}, "qty": 1}},
		{"qty zero", "qty", map[string]any{"item_id": 1, "size": "M", "chosen": map[string][]int64{}, "qty": 0}},
		{"qty 21", "qty", map[string]any{"item_id": 1, "size": "M", "chosen": map[string][]int64{}, "qty": 21}},
		{"chosen key not a number", "chosen", map[string]any{"item_id": 1, "size": "M", "chosen": map[string][]int64{"crust": {1}}, "qty": 1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			expectError(t, newClient(t).post("/v1/menu/price", tc.body), http.StatusBadRequest, "invalid_parameter", tc.field)
		})
	}
}

func TestW2B6_ABodyOver64KiBIs413(t *testing.T) {
	ids := make([]string, 0, 20000)
	for i := range 20000 {
		ids = append(ids, fmt.Sprint(i+1))
	}
	body := `{"item_id":1,"size":"M","qty":1,"chosen":{"2":[` + strings.Join(ids, ",") + `]}}`
	if len(body) <= 64<<10 {
		t.Fatalf("test body is only %d bytes", len(body))
	}
	expectError(t, postRaw(t, "/v1/menu/price", body), http.StatusRequestEntityTooLarge, "body_too_large", "")
}

func postRaw(t *testing.T, path, body string) resp {
	t.Helper()
	c := newClient(t)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, c.base+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", c.limitKey)
	res, err := c.http.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	defer func() { _ = res.Body.Close() }()
	b, _ := io.ReadAll(res.Body)
	return resp{Status: res.StatusCode, Header: res.Header, Body: b}
}
