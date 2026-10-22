package menu

import (
	"strings"
	"testing"
)

func farmhouse() Item {
	return Item{
		ID: "farm", Name: "Farmhouse",
		BaseBySize: map[string]int{"R": 25900, "M": 45900, "L": 65900},
		Groups: []Group{
			{ID: "crust", Name: "Crust", MinSelect: 1, MaxSelect: 1, Options: []Option{
				{ID: "hand", Name: "Hand tossed", PriceBySize: map[string]int{"*": 0}},
				{ID: "burst", Name: "Cheese burst", PriceBySize: map[string]int{"M": 9900, "L": 12900}, AllowedSizes: map[string]bool{"M": true, "L": true}},
			}},
			{ID: "top", Name: "Toppings", MinSelect: 0, MaxSelect: 5, Options: []Option{
				{ID: "olive", Name: "Olives", PriceBySize: map[string]int{"R": 3500, "M": 5000, "L": 7000}},
				{ID: "paneer", Name: "Paneer", PriceBySize: map[string]int{"R": 6000, "M": 8000, "L": 11000}},
			}},
		},
	}
}

func TestW2B6_PriceHappyPath(t *testing.T) {
	q := Price(farmhouse(), Selection{ItemID: "farm", Size: "M", Chosen: map[string][]string{"crust": {"burst"}, "top": {"olive", "paneer"}}, Qty: 2},
		[]Rule{CheapestFree{GroupID: "top", N: 1}})
	if len(q.Errors) != 0 {
		t.Fatalf("unexpected errors %v", q.Errors)
	}
	if q.UnitPaise != 45900+9900+5000+8000-5000 || q.TotalPaise != q.UnitPaise*2 {
		t.Fatalf("unit %d total %d", q.UnitPaise, q.TotalPaise)
	}
}

func TestW2B6_PriceCollectsEveryError(t *testing.T) {
	tests := []struct {
		name string
		sel  Selection
		want []string
	}{
		{
			"two crusts in size R",
			Selection{Size: "R", Chosen: map[string][]string{"crust": {"burst", "hand"}}, Qty: 1},
			[]string{"Crust: pick 1..1, got 2", "Cheese burst not available in R"},
		},
		{
			"unknown size and missing crust",
			Selection{Size: "XL", Chosen: map[string][]string{}, Qty: 1},
			[]string{"size XL not offered", "Crust: pick 1..1, got 0"},
		},
		{
			"duplicate topping",
			Selection{Size: "M", Chosen: map[string][]string{"crust": {"hand"}, "top": {"olive", "olive"}}, Qty: 1},
			[]string{"Toppings: duplicate option olive"},
		},
		{
			"unknown option",
			Selection{Size: "M", Chosen: map[string][]string{"crust": {"hand"}, "top": {"corn"}}, Qty: 1},
			[]string{"Toppings: unknown option corn"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			q := Price(farmhouse(), tc.sel, nil)
			got := strings.Join(q.Errors, " | ")
			for _, w := range tc.want {
				if !strings.Contains(got, w) {
					t.Fatalf("missing %q in %q", w, got)
				}
			}
			if len(q.Errors) != len(tc.want) {
				t.Fatalf("got %d errors, want %d: %q", len(q.Errors), len(tc.want), got)
			}
		})
	}
}
