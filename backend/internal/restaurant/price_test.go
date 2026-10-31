package restaurant_test

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/yashdagar/zomato-prep/backend/internal/db"
	"github.com/yashdagar/zomato-prep/backend/internal/restaurant"
)

type fakeItem struct{ fakeQueries }

func (f *fakeItem) GetItem(_ context.Context, id uint64) (db.Item, error) {
	for _, it := range f.items {
		if it.ID == id {
			return it, nil
		}
	}
	return db.Item{}, sql.ErrNoRows
}

func pizza() *fakeItem {
	return &fakeItem{fakeQueries{
		items:   []db.Item{{ID: 10, RestaurantID: 1, Name: "Farmhouse pizza"}},
		sizes:   []db.ItemSize{{ItemID: 10, Size: "R", PricePaise: 19900}, {ItemID: 10, Size: "M", PricePaise: 39900}},
		groups:  []db.OptionGroup{{ID: 20, ItemID: 10, Name: "Crust", MinSelect: 1, MaxSelect: 1}, {ID: 21, ItemID: 10, Name: "Toppings", MinSelect: 0, MaxSelect: 3}},
		options: []db.Option{{ID: 30, GroupID: 20, Name: "Classic", InStock: true}, {ID: 31, GroupID: 20, Name: "Cheese burst", InStock: true}, {ID: 32, GroupID: 21, Name: "Olives", InStock: false}, {ID: 33, GroupID: 21, Name: "Paneer", InStock: true}},
		prices:  []db.OptionPrice{{OptionID: 30, Size: "*", PricePaise: 0}, {OptionID: 31, Size: "M", PricePaise: 9900}, {OptionID: 32, Size: "*", PricePaise: 3500}, {OptionID: 33, Size: "*", PricePaise: 6000}},
	}}
}

func quote(t *testing.T, size string, chosen map[string][]int64) []string {
	t.Helper()
	q, err := restaurant.NewPricer(pizza()).Quote(context.Background(), restaurant.PriceRequest{ItemID: 10, Size: size, Chosen: chosen, Qty: 1})
	if err != nil {
		t.Fatal(err)
	}
	return q.Errors
}

func TestW2B6_OutOfStockOptionIsReported(t *testing.T) {
	errs := quote(t, "M", map[string][]int64{"20": {30}, "21": {32}})
	if !slices.ContainsFunc(errs, func(e string) bool { return strings.Contains(e, "Olives") }) {
		t.Fatalf("errors = %v, want one about the out-of-stock Olives", errs)
	}
}

func TestW2B6_SizeOnlyOptionIsUnavailableInOtherSizes(t *testing.T) {
	if errs := quote(t, "R", map[string][]int64{"20": {31}}); len(errs) == 0 {
		t.Fatal("Cheese burst is priced only for M, so choosing it on R must be an error")
	}
	if errs := quote(t, "M", map[string][]int64{"20": {31}}); len(errs) != 0 {
		t.Fatalf("Cheese burst on M: errors %v, want none", errs)
	}
}

func TestW2B6_StarPricedOptionIsAvailableInEverySize(t *testing.T) {
	for _, size := range []string{"R", "M"} {
		if errs := quote(t, size, map[string][]int64{"20": {30}, "21": {33}}); len(errs) != 0 {
			t.Fatalf("size %s with * priced options: errors %v, want none", size, errs)
		}
	}
}

func TestW2B6_MissingItemIsErrItemNotFound(t *testing.T) {
	_, err := restaurant.NewPricer(pizza()).Quote(context.Background(), restaurant.PriceRequest{ItemID: 99, Size: "M", Qty: 1})
	if !errors.Is(err, restaurant.ErrItemNotFound) {
		t.Fatalf("missing item: err = %v, want one wrapping restaurant.ErrItemNotFound", err)
	}
}
