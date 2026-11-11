package restaurant_test

import (
	"context"
	"database/sql"

	"github.com/yashdagar/zomato-prep/backend/internal/db"
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
