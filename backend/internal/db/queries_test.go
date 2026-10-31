package db_test

import (
	"context"
	"database/sql"
	"os"
	"testing"

	_ "github.com/go-sql-driver/mysql"

	"github.com/yashdagar/zomato-prep/backend/internal/db"
)

type week2Queries interface {
	ListRestaurantsNear(ctx context.Context, arg db.ListRestaurantsNearParams) ([]db.ListRestaurantsNearRow, error)
	GetRestaurant(ctx context.Context, id uint64) (db.GetRestaurantRow, error)
	ListItemsByRestaurant(ctx context.Context, restaurantID uint64) ([]db.Item, error)
	ListSizesByItemIDs(ctx context.Context, itemIds []uint64) ([]db.ItemSize, error)
	ListGroupsByItemIDs(ctx context.Context, itemIds []uint64) ([]db.OptionGroup, error)
	ListOptionsByGroupIDs(ctx context.Context, groupIds []uint64) ([]db.Option, error)
	ListOptionPricesByOptionIDs(ctx context.Context, optionIds []uint64) ([]db.OptionPrice, error)
}

func TestW2B2_GeneratedQueriesHaveTheExpectedSignatures(t *testing.T) {
	var q week2Queries = db.New(nil)
	var _ db.Querier = db.New(nil)
	if q == nil {
		t.Fatal("db.New returned nil")
	}
}

func withTx(t *testing.T) (*db.Queries, *sql.Tx) {
	t.Helper()
	dsn := os.Getenv("TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("set TEST_MYSQL_DSN=root:dev@tcp(127.0.0.1:3306)/zomato?parseTime=true after make migrate to run")
	}
	conn, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	tx, err := conn.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })
	return db.New(conn).WithTx(tx), tx
}

func exec(t *testing.T, tx *sql.Tx, query string, args ...any) int64 {
	t.Helper()
	res, err := tx.ExecContext(t.Context(), query, args...)
	if err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	id, _ := res.LastInsertId()
	return id
}

func TestW2B2_ListRestaurantsNearFiltersByBoxActiveAndCursor(t *testing.T) {
	q, tx := withTx(t)
	inside := exec(t, tx, "INSERT INTO restaurants (name, lat, lng, is_active) VALUES ('inside', 10.000000, 20.000000, 1)")
	second := exec(t, tx, "INSERT INTO restaurants (name, lat, lng, is_active) VALUES ('inside 2', 10.001000, 20.001000, 1)")
	exec(t, tx, "INSERT INTO restaurants (name, lat, lng, is_active) VALUES ('closed', 10.000500, 20.000500, 0)")
	exec(t, tx, "INSERT INTO restaurants (name, lat, lng, is_active) VALUES ('far', 11.000000, 21.000000, 1)")
	box := db.ListRestaurantsNearParams{MinLat: "9.99", MaxLat: "10.01", MinLng: "19.99", MaxLng: "20.01", Limit: 10}
	rows, err := q.ListRestaurantsNear(t.Context(), box)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || int64(rows[0].ID) != inside || int64(rows[1].ID) != second {
		t.Fatalf("got %+v, want the two active restaurants inside the box ordered by id", rows)
	}
	box.AfterID = uint64(inside)
	rows, err = q.ListRestaurantsNear(t.Context(), box)
	if err != nil || len(rows) != 1 || int64(rows[0].ID) != second {
		t.Fatalf("after_id = first id: got %+v, %v, want only the second restaurant", rows, err)
	}
	box.AfterID, box.Limit = 0, 1
	if rows, _ := q.ListRestaurantsNear(t.Context(), box); len(rows) != 1 {
		t.Fatalf("limit 1 returned %d rows", len(rows))
	}
}

func TestW2B2_GetRestaurantReportsNoRows(t *testing.T) {
	q, _ := withTx(t)
	if _, err := q.GetRestaurant(t.Context(), 1<<62); err != sql.ErrNoRows {
		t.Fatalf("missing id: err = %v, want sql.ErrNoRows", err)
	}
}

func TestW2B2_MenuQueriesTakeIDSlices(t *testing.T) {
	q, tx := withTx(t)
	rest := exec(t, tx, "INSERT INTO restaurants (name, lat, lng) VALUES ('menu test', 1, 1)")
	a := exec(t, tx, "INSERT INTO items (restaurant_id, name, sort) VALUES (?, 'second', 2)", rest)
	b := exec(t, tx, "INSERT INTO items (restaurant_id, name, sort) VALUES (?, 'first', 1)", rest)
	exec(t, tx, "INSERT INTO item_sizes (item_id, size, price_paise) VALUES (?, 'R', 100), (?, 'M', 200), (?, 'R', 300)", a, a, b)
	g := exec(t, tx, "INSERT INTO option_groups (item_id, name, min_select, max_select) VALUES (?, 'Crust', 1, 1)", a)
	o := exec(t, tx, "INSERT INTO options (group_id, name, in_stock) VALUES (?, 'Thin', 1)", g)
	exec(t, tx, "INSERT INTO option_prices (option_id, size, price_paise) VALUES (?, '*', 0), (?, 'M', 50)", o, o)

	items, err := q.ListItemsByRestaurant(t.Context(), uint64(rest))
	if err != nil || len(items) != 2 || items[0].Name != "first" {
		t.Fatalf("items = %+v, %v, want two items ordered by sort", items, err)
	}
	sizes, err := q.ListSizesByItemIDs(t.Context(), []uint64{uint64(a), uint64(b)})
	if err != nil || len(sizes) != 3 {
		t.Fatalf("sizes for two items = %d, %v, want 3", len(sizes), err)
	}
	groups, err := q.ListGroupsByItemIDs(t.Context(), []uint64{uint64(a), uint64(b)})
	if err != nil || len(groups) != 1 {
		t.Fatalf("groups = %+v, %v, want 1", groups, err)
	}
	opts, err := q.ListOptionsByGroupIDs(t.Context(), []uint64{uint64(g)})
	if err != nil || len(opts) != 1 {
		t.Fatalf("options = %+v, %v, want 1", opts, err)
	}
	prices, err := q.ListOptionPricesByOptionIDs(t.Context(), []uint64{uint64(o)})
	if err != nil || len(prices) != 2 {
		t.Fatalf("option prices = %+v, %v, want 2 (the * row and M)", prices, err)
	}
}
