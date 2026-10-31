package restaurant_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/yashdagar/zomato-prep/backend/internal/db"
	"github.com/yashdagar/zomato-prep/backend/internal/restaurant"
)

type fakeQueries struct {
	calls       int
	items       []db.Item
	sizes       []db.ItemSize
	groups      []db.OptionGroup
	options     []db.Option
	prices      []db.OptionPrice
	restaurants map[uint64]bool
}

func (f *fakeQueries) ListRestaurantsNear(context.Context, db.ListRestaurantsNearParams) ([]db.ListRestaurantsNearRow, error) {
	f.calls++
	return nil, nil
}

func (f *fakeQueries) GetRestaurant(_ context.Context, id uint64) (db.GetRestaurantRow, error) {
	f.calls++
	if !f.restaurants[id] {
		return db.GetRestaurantRow{}, sql.ErrNoRows
	}
	return db.GetRestaurantRow{ID: id}, nil
}

func (f *fakeQueries) ListItemsByRestaurant(_ context.Context, id uint64) ([]db.Item, error) {
	f.calls++
	var out []db.Item
	for _, it := range f.items {
		if it.RestaurantID == id {
			out = append(out, it)
		}
	}
	return out, nil
}

func (f *fakeQueries) ListSizesByItemIDs(context.Context, []uint64) ([]db.ItemSize, error) {
	f.calls++
	return f.sizes, nil
}

func (f *fakeQueries) ListGroupsByItemIDs(context.Context, []uint64) ([]db.OptionGroup, error) {
	f.calls++
	return f.groups, nil
}

func (f *fakeQueries) ListOptionsByGroupIDs(context.Context, []uint64) ([]db.Option, error) {
	f.calls++
	return f.options, nil
}

func (f *fakeQueries) ListOptionPricesByOptionIDs(context.Context, []uint64) ([]db.OptionPrice, error) {
	f.calls++
	return f.prices, nil
}

func pizzaMenu() *fakeQueries {
	return &fakeQueries{
		restaurants: map[uint64]bool{1: true, 2: true},
		items:       []db.Item{{ID: 10, RestaurantID: 1, Name: "Farmhouse pizza", Sort: 1}, {ID: 11, RestaurantID: 1, Name: "Garlic bread", Sort: 2}},
		sizes:       []db.ItemSize{{ItemID: 10, Size: "R", PricePaise: 19900}, {ItemID: 10, Size: "M", PricePaise: 39900}, {ItemID: 11, Size: "R", PricePaise: 9900}},
		groups:      []db.OptionGroup{{ID: 20, ItemID: 10, Name: "Crust", MinSelect: 1, MaxSelect: 1}, {ID: 21, ItemID: 10, Name: "Toppings", MinSelect: 0, MaxSelect: 5}},
		options:     []db.Option{{ID: 30, GroupID: 20, Name: "Classic", InStock: true}, {ID: 31, GroupID: 20, Name: "Cheese burst", InStock: true}, {ID: 32, GroupID: 21, Name: "Olives", InStock: false}},
		prices:      []db.OptionPrice{{OptionID: 30, Size: "*", PricePaise: 0}, {OptionID: 31, Size: "M", PricePaise: 9900}, {OptionID: 32, Size: "*", PricePaise: 3500}},
	}
}

func TestW2B5_MenuUsesAtMostFiveQueries(t *testing.T) {
	q := pizzaMenu()
	if _, err := restaurant.NewService(q).Menu(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if q.calls > 5 {
		t.Fatalf("Menu made %d queries, want at most 5 whatever the size of the menu", q.calls)
	}
}

func TestW2B5_MenuNestsSizesGroupsOptionsAndPricesUnderTheirParents(t *testing.T) {
	items, err := restaurant.NewService(pizzaMenu()).Menu(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Name != "Farmhouse pizza" || items[1].Name != "Garlic bread" {
		t.Fatalf("items = %+v, want pizza then garlic bread", items)
	}
	pizza, bread := items[0], items[1]
	if len(pizza.Sizes) != 2 || len(bread.Sizes) != 1 || len(bread.Groups) != 0 {
		t.Fatalf("sizes or groups attached to the wrong item: pizza %+v, bread %+v", pizza, bread)
	}
	if len(pizza.Groups) != 2 || len(pizza.Groups[0].Options) != 2 || len(pizza.Groups[1].Options) != 1 {
		t.Fatalf("pizza groups = %+v, want crust with 2 options and toppings with 1", pizza.Groups)
	}
	burst := pizza.Groups[0].Options[1]
	if burst.Name != "Cheese burst" || burst.Prices["M"] != 9900 || len(burst.Prices) != 1 {
		t.Fatalf("cheese burst = %+v, want only an M price of 9900", burst)
	}
	if olives := pizza.Groups[1].Options[0]; olives.InStock {
		t.Fatalf("olives = %+v, want in_stock false", olives)
	}
}

func TestW2B5_RestaurantWithoutItemsIsAnEmptyMenuNotAnError(t *testing.T) {
	items, err := restaurant.NewService(pizzaMenu()).Menu(context.Background(), 2)
	if err != nil || items == nil || len(items) != 0 {
		t.Fatalf("Menu(2) = %#v, %v; want an empty, non-nil slice and no error", items, err)
	}
}

func TestW2B5_MissingRestaurantIsErrNotFound(t *testing.T) {
	_, err := restaurant.NewService(pizzaMenu()).Menu(context.Background(), 99)
	if !errors.Is(err, restaurant.ErrNotFound) {
		t.Fatalf("Menu(99) error = %v, want one wrapping restaurant.ErrNotFound", err)
	}
}
