package e2e_test

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

var menuTables = map[string][]string{
	"restaurants":   {"id", "name", "lat", "lng", "is_active"},
	"items":         {"id", "restaurant_id", "name", "sort"},
	"item_sizes":    {"item_id", "size", "price_paise"},
	"option_groups": {"id", "item_id", "name", "min_select", "max_select"},
	"options":       {"id", "group_id", "name", "in_stock"},
	"option_prices": {"option_id", "size", "price_paise"},
}

func tables(t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), "SELECT table_name FROM information_schema.tables WHERE table_schema = DATABASE() ORDER BY table_name")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		out = append(out, n)
	}
	return out
}

func migratedScratch(t *testing.T) *sql.DB {
	t.Helper()
	db, _ := scratchDB(t)
	migrateUp(t, db)
	return db
}

func mustFail(t *testing.T, db *sql.DB, why, q string, args ...any) {
	t.Helper()
	if _, err := db.ExecContext(t.Context(), q, args...); err == nil {
		t.Fatalf("%s: %s succeeded, want MySQL to reject it", why, q)
	}
}

func TestW2B1_EveryMigrationHasAnUpAndADownFile(t *testing.T) {
	ms := migrations(t)
	if len(ms) == 0 {
		t.Fatal("no migrations in db/migrations; name them like 0001_restaurants.up.sql and 0001_restaurants.down.sql")
	}
	for _, m := range ms {
		if m.up == "" || m.down == "" {
			t.Fatalf("migration %04d needs both an .up.sql and a .down.sql file", m.version)
		}
	}
}

func TestW2B1_MigrationsCreateTheMenuTables(t *testing.T) {
	db := migratedScratch(t)
	for table, cols := range menuTables {
		for _, c := range cols {
			n := count(t, db, "SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = ? AND column_name = ?", table, c)
			if n != 1 {
				t.Fatalf("table %s has no column %s", table, c)
			}
		}
	}
}

func TestW2B1_DownMigrationsUndoEverythingAndUpWorksAgain(t *testing.T) {
	db := migratedScratch(t)
	ms := migrations(t)
	for i := len(ms) - 1; i >= 0; i-- {
		if err := runFile(t, db, ms[i].down); err != nil {
			t.Fatalf("%s: %v", filepath.Base(ms[i].down), err)
		}
	}
	if left := tables(t, db); len(left) > 0 {
		t.Fatalf("after every down migration these tables remain: %s", strings.Join(left, ", "))
	}
	migrateUp(t, db)
	if n := len(tables(t, db)); n < len(menuTables) {
		t.Fatalf("up after down created %d tables, want at least %d", n, len(menuTables))
	}
}

func TestW2B1_ForeignKeysRejectRowsWithoutAParent(t *testing.T) {
	db := migratedScratch(t)
	mustFail(t, db, "item for a missing restaurant", "INSERT INTO items (restaurant_id, name) VALUES (999, 'x')")
	mustExec(t, db, "INSERT INTO restaurants (id, name, lat, lng) VALUES (1, 'r', 28.46, 77.03)")
	mustExec(t, db, "INSERT INTO items (id, restaurant_id, name) VALUES (1, 1, 'i')")
	mustFail(t, db, "size for a missing item", "INSERT INTO item_sizes (item_id, size, price_paise) VALUES (999, 'M', 100)")
	mustFail(t, db, "group for a missing item", "INSERT INTO option_groups (item_id, name) VALUES (999, 'g')")
	mustExec(t, db, "INSERT INTO option_groups (id, item_id, name) VALUES (1, 1, 'g')")
	mustFail(t, db, "option for a missing group", "INSERT INTO options (group_id, name) VALUES (999, 'o')")
	mustExec(t, db, "INSERT INTO options (id, group_id, name) VALUES (1, 1, 'o')")
	mustFail(t, db, "price for a missing option", "INSERT INTO option_prices (option_id, size, price_paise) VALUES (999, 'M', 100)")
	mustFail(t, db, "deleting a restaurant that still has items", "DELETE FROM restaurants WHERE id = 1")
}

func TestW2B1_EachSizeHasOnePrice(t *testing.T) {
	db := migratedScratch(t)
	mustExec(t, db, "INSERT INTO restaurants (id, name, lat, lng) VALUES (1, 'r', 28.46, 77.03)")
	mustExec(t, db, "INSERT INTO items (id, restaurant_id, name) VALUES (1, 1, 'i')")
	mustExec(t, db, "INSERT INTO item_sizes (item_id, size, price_paise) VALUES (1, 'M', 100)")
	mustFail(t, db, "a second price for item 1 size M", "INSERT INTO item_sizes (item_id, size, price_paise) VALUES (1, 'M', 200)")
	mustExec(t, db, "INSERT INTO option_groups (id, item_id, name) VALUES (1, 1, 'g')")
	mustExec(t, db, "INSERT INTO options (id, group_id, name) VALUES (1, 1, 'o')")
	mustExec(t, db, "INSERT INTO option_prices (option_id, size, price_paise) VALUES (1, '*', 0)")
	mustFail(t, db, "a second * price for option 1", "INSERT INTO option_prices (option_id, size, price_paise) VALUES (1, '*', 50)")
}

func TestW2B1_PricesCannotBeNegative(t *testing.T) {
	db := migratedScratch(t)
	mustExec(t, db, "INSERT INTO restaurants (id, name, lat, lng) VALUES (1, 'r', 28.46, 77.03)")
	mustExec(t, db, "INSERT INTO items (id, restaurant_id, name) VALUES (1, 1, 'i')")
	mustFail(t, db, "a negative item price", "INSERT INTO item_sizes (item_id, size, price_paise) VALUES (1, 'M', -1)")
	mustExec(t, db, "INSERT INTO option_groups (id, item_id, name) VALUES (1, 1, 'g')")
	mustExec(t, db, "INSERT INTO options (id, group_id, name) VALUES (1, 1, 'o')")
	mustFail(t, db, "a negative option price", "INSERT INTO option_prices (option_id, size, price_paise) VALUES (1, 'M', -1)")
}

func TestW2B1_NewRowsGetTheDocumentedDefaults(t *testing.T) {
	db := migratedScratch(t)
	mustExec(t, db, "INSERT INTO restaurants (id, name, lat, lng) VALUES (1, 'r', 28.46, 77.03)")
	mustExec(t, db, "INSERT INTO items (id, restaurant_id, name) VALUES (1, 1, 'i')")
	mustExec(t, db, "INSERT INTO option_groups (id, item_id, name) VALUES (1, 1, 'g')")
	mustExec(t, db, "INSERT INTO options (id, group_id, name) VALUES (1, 1, 'o')")
	if n := count(t, db, "SELECT COUNT(*) FROM restaurants WHERE id = 1 AND is_active = 1"); n != 1 {
		t.Fatal("a new restaurant should be active unless is_active is set")
	}
	if n := count(t, db, "SELECT COUNT(*) FROM items WHERE id = 1 AND sort = 0"); n != 1 {
		t.Fatal("a new item should have sort 0 unless sort is set")
	}
	if n := count(t, db, "SELECT COUNT(*) FROM option_groups WHERE id = 1 AND min_select = 0 AND max_select = 1"); n != 1 {
		t.Fatal("a new option group should have min_select 0 and max_select 1 unless they are set")
	}
	if n := count(t, db, "SELECT COUNT(*) FROM options WHERE id = 1 AND in_stock = 1"); n != 1 {
		t.Fatal("a new option should be in stock unless in_stock is set")
	}
}

func TestW2B1_TheGeoBoxQueryCanUseAnIndex(t *testing.T) {
	db := migratedScratch(t)
	keys := explainKeys(t, db, "SELECT id FROM restaurants WHERE lat BETWEEN 28.41 AND 28.51 AND lng BETWEEN 76.98 AND 77.08 ORDER BY id")
	if keys["restaurants"] == "" {
		t.Fatal("EXPLAIN finds no index on restaurants usable for a lat/lng range; add one so the nearby query does not scan the whole table")
	}
}

// explainKeys returns possible_keys per table from a traditional EXPLAIN.
func explainKeys(t *testing.T, db *sql.DB, q string) map[string]string {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), "EXPLAIN "+q)
	if err != nil {
		t.Fatalf("EXPLAIN %s: %v", q, err)
	}
	defer func() { _ = rows.Close() }()
	cols, _ := rows.Columns()
	out := map[string]string{}
	for rows.Next() {
		vals := make([]sql.NullString, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatal(err)
		}
		var table, keys string
		for i, c := range cols {
			switch strings.ToLower(c) {
			case "table":
				table = vals[i].String
			case "possible_keys":
				keys = vals[i].String
			}
		}
		if table != "" && out[table] == "" {
			out[table] = keys
		}
	}
	return out
}
