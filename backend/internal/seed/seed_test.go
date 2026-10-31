package seed_test

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"github.com/yashdagar/zomato-prep/backend/internal/seed"
)

func open(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("set TEST_MYSQL_DSN=root:dev@tcp(127.0.0.1:3306)/zomato?parseTime=true after make migrate to run")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func count(t *testing.T, db *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRowContext(t.Context(), query, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

func run(t *testing.T, db *sql.DB) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	if err := seed.Run(ctx, db); err != nil {
		t.Fatalf("seed.Run: %v", err)
	}
}

func TestW2B3_RunningTwiceGivesTheSameData(t *testing.T) {
	db := open(t)
	run(t, db)
	run(t, db)
	if n := count(t, db, "SELECT COUNT(*) FROM restaurants"); n != 50 {
		t.Fatalf("restaurants after two runs = %d, want 50", n)
	}
	if n := count(t, db, "SELECT COUNT(*) FROM items WHERE name = 'Farmhouse pizza'"); n != 1 {
		t.Fatalf("Farmhouse pizza rows after two runs = %d, want 1", n)
	}
}

func TestW2B3_EveryRestaurantIsActiveAndWithinFiveKm(t *testing.T) {
	db := open(t)
	run(t, db)
	far := count(t, db, `SELECT COUNT(*) FROM restaurants
		WHERE is_active <> 1
		   OR ABS(lat - 28.46) * 111 > 5
		   OR ABS(lng - 77.03) * 111 * COS(RADIANS(28.46)) > 5`)
	if far != 0 {
		t.Fatalf("%d restaurants are inactive or more than 5 km from 28.46,77.03", far)
	}
}

func TestW2B3_FarmhousePizzaHasThreeSizesAndBothGroups(t *testing.T) {
	db := open(t)
	run(t, db)
	sizes := count(t, db, `SELECT COUNT(*) FROM item_sizes s JOIN items i ON i.id = s.item_id
		WHERE i.name = 'Farmhouse pizza' AND s.size IN ('R', 'M', 'L')`)
	if sizes != 3 {
		t.Fatalf("Farmhouse pizza sizes R, M, L = %d rows, want 3", sizes)
	}
	crust := count(t, db, `SELECT COUNT(*) FROM option_groups g JOIN items i ON i.id = g.item_id
		WHERE i.name = 'Farmhouse pizza' AND g.name = 'Crust' AND g.min_select = 1 AND g.max_select = 1`)
	toppings := count(t, db, `SELECT COUNT(*) FROM option_groups g JOIN items i ON i.id = g.item_id
		WHERE i.name = 'Farmhouse pizza' AND g.name = 'Toppings' AND g.min_select = 0 AND g.max_select = 5`)
	if crust != 1 || toppings != 1 {
		t.Fatalf("crust group (1..1) = %d, toppings group (0..5) = %d, want one of each", crust, toppings)
	}
}

func TestW2B3_CheeseBurstIsOnlyPricedForMediumAndLarge(t *testing.T) {
	db := open(t)
	run(t, db)
	var sizes []string
	rows, err := db.QueryContext(t.Context(), `SELECT p.size FROM option_prices p JOIN options o ON o.id = p.option_id
		WHERE o.name = 'Cheese burst' ORDER BY p.size`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		sizes = append(sizes, s)
	}
	if len(sizes) != 2 || sizes[0] != "L" || sizes[1] != "M" {
		t.Fatalf("Cheese burst priced for %v, want exactly [L M] (no R and no *)", sizes)
	}
}
