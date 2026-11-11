package e2e_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func backendDir(t *testing.T) string {
	t.Helper()
	return needDB(t, "TEST_BACKEND_DIR")
}

func command(t *testing.T, timeout time.Duration, env []string, name string, args ...string) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = backendDir(t)
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func sqlcBinary(t *testing.T) string {
	t.Helper()
	p, err := exec.LookPath("sqlc")
	if err != nil {
		t.Fatal("sqlc is not on your PATH; install it (brew install sqlc, or go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest)")
	}
	return p
}

func TestW2B2_QueriesCompileAgainstTheMigrations(t *testing.T) {
	if _, err := os.Stat(backendDir(t) + "/sqlc.yaml"); err != nil {
		t.Fatal("no sqlc.yaml in backend/")
	}
	if out, err := command(t, time.Minute, nil, sqlcBinary(t), "compile"); err != nil {
		t.Fatalf("sqlc compile: %v\n%s", err, out)
	}
}

func TestW2B2_GeneratedCodeIsUpToDate(t *testing.T) {
	if out, err := command(t, time.Minute, nil, sqlcBinary(t), "diff"); err != nil {
		t.Fatalf("sqlc diff reports generated code that does not match the queries; run sqlc generate and commit the result\n%s", out)
	}
}

func seedInto(t *testing.T, dsn string) {
	t.Helper()
	out, err := command(t, 2*time.Minute, []string{"MYSQL_DSN=" + dsn}, "go", "run", "./cmd/seed")
	if err != nil {
		t.Fatalf("go run ./cmd/seed: %v\n%s", err, out)
	}
}

func seededScratch(t *testing.T) (*sql.DB, string) {
	t.Helper()
	db, dsn := scratchDB(t)
	migrateUp(t, db)
	seedInto(t, dsn)
	return db, dsn
}

// contents fingerprints every table, leaving out columns that MySQL fills
// with the current time, so two seed runs compare equal when they wrote the
// same data.
func contents(t *testing.T, db *sql.DB) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, table := range tables(t, db) {
		rows, err := db.QueryContext(t.Context(), `SELECT column_name FROM information_schema.columns
			WHERE table_schema = DATABASE() AND table_name = ?
			  AND data_type NOT IN ('datetime', 'timestamp')
			ORDER BY ordinal_position`, table)
		if err != nil {
			t.Fatal(err)
		}
		var cols []string
		for rows.Next() {
			var c string
			if err := rows.Scan(&c); err != nil {
				t.Fatal(err)
			}
			cols = append(cols, "COALESCE(CAST(`"+c+"` AS CHAR), 'NULL')")
		}
		_ = rows.Close()
		var sum sql.NullString
		q := "SELECT MD5(GROUP_CONCAT(row_text ORDER BY row_text SEPARATOR '\\n')) FROM (SELECT CONCAT_WS('|', " + strings.Join(cols, ", ") + ") AS row_text FROM `" + table + "`) t"
		if _, err := db.ExecContext(t.Context(), "SET SESSION group_concat_max_len = 67108864"); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRowContext(t.Context(), q).Scan(&sum); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		out[table] = sum.String
	}
	return out
}

func TestW2B3_SeedCreatesFiftyActiveRestaurantsWithinFiveKm(t *testing.T) {
	db, _ := seededScratch(t)
	if n := count(t, db, "SELECT COUNT(*) FROM restaurants WHERE id BETWEEN 1 AND 50"); n != 50 {
		t.Fatalf("restaurants with ids 1 to 50 = %d, want 50", n)
	}
	if n := count(t, db, "SELECT COUNT(*) FROM restaurants"); n != 50 {
		t.Fatalf("restaurants = %d, want exactly 50", n)
	}
	far := count(t, db, `SELECT COUNT(*) FROM restaurants
		WHERE is_active <> 1
		   OR SQRT(POW((lat - 28.46) * 111, 2) + POW((lng - 77.03) * 111 * COS(RADIANS(28.46)), 2)) > 5.001`)
	if far != 0 {
		t.Fatalf("%d restaurants are inactive or more than 5 km from 28.46,77.03", far)
	}
	if n := count(t, db, "SELECT COUNT(DISTINCT name) FROM restaurants"); n != 50 {
		t.Fatalf("restaurant names: %d distinct, want 50", n)
	}
}

func TestW2B3_RunningTheSeedTwiceGivesIdenticalData(t *testing.T) {
	db, dsn := seededScratch(t)
	first := contents(t, db)
	seedInto(t, dsn)
	second := contents(t, db)
	for table, sum := range first {
		if second[table] != sum {
			t.Fatalf("table %s changed between two seed runs; the seed must be deterministic and replace what it wrote", table)
		}
	}
}

func TestW2B3_SeedReplacesRowsThatWereAlreadyThere(t *testing.T) {
	db, dsn := scratchDB(t)
	migrateUp(t, db)
	mustExec(t, db, "INSERT INTO restaurants (id, name, lat, lng) VALUES (77, 'Leftover', 28.46, 77.03)")
	mustExec(t, db, "INSERT INTO items (id, restaurant_id, name) VALUES (77, 77, 'Leftover item')")
	seedInto(t, dsn)
	if n := count(t, db, "SELECT COUNT(*) FROM restaurants WHERE id = 77 OR name = 'Leftover'"); n != 0 {
		t.Fatal("a restaurant that was in the table before seeding is still there; the seed must reset the menu tables")
	}
	if n := count(t, db, "SELECT COUNT(*) FROM items"); n != 1 {
		t.Fatalf("items after seeding = %d, want only the Farmhouse pizza", n)
	}
}

type priceRow struct {
	size  string
	paise int
}

func priceRows(t *testing.T, db *sql.DB, q string, args ...any) []priceRow {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), q, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []priceRow
	for rows.Next() {
		var p priceRow
		if err := rows.Scan(&p.size, &p.paise); err != nil {
			t.Fatal(err)
		}
		out = append(out, p)
	}
	return out
}

func TestW2B3_FarmhousePizzaMatchesTheSpecification(t *testing.T) {
	db, _ := seededScratch(t)
	if n := count(t, db, "SELECT COUNT(*) FROM items WHERE id = 1 AND restaurant_id = 1 AND name = 'Farmhouse pizza'"); n != 1 {
		t.Fatal("want item 1, Farmhouse pizza, on restaurant 1")
	}
	sizes := priceRows(t, db, "SELECT size, price_paise FROM item_sizes WHERE item_id = 1 ORDER BY size")
	if fmt.Sprint(sizes) != fmt.Sprint([]priceRow{{"L", 59900}, {"M", 39900}, {"R", 19900}}) {
		t.Fatalf("Farmhouse sizes = %v, want R 19900, M 39900, L 59900", sizes)
	}
	groups := []struct {
		id       int
		name     string
		min, max int
	}{{1, "Crust", 1, 1}, {2, "Toppings", 0, 5}}
	for _, g := range groups {
		if n := count(t, db, "SELECT COUNT(*) FROM option_groups WHERE id = ? AND item_id = 1 AND name = ? AND min_select = ? AND max_select = ?", g.id, g.name, g.min, g.max); n != 1 {
			t.Fatalf("want option group %d %s (%d..%d) on item 1", g.id, g.name, g.min, g.max)
		}
	}
	options := []struct {
		id     int
		group  int
		name   string
		prices []priceRow
	}{
		{1, 1, "Classic hand tossed", []priceRow{{"*", 0}}},
		{2, 1, "Cheese burst", []priceRow{{"L", 12900}, {"M", 9900}}},
		{3, 2, "Olives", []priceRow{{"*", 3500}}},
		{4, 2, "Jalapeno", []priceRow{{"*", 3500}}},
		{5, 2, "Paneer", []priceRow{{"*", 6000}}},
		{6, 2, "Mushroom", []priceRow{{"*", 4500}}},
		{7, 2, "Red paprika", []priceRow{{"*", 4000}}},
	}
	for _, o := range options {
		if n := count(t, db, "SELECT COUNT(*) FROM options WHERE id = ? AND group_id = ? AND name = ? AND in_stock = 1", o.id, o.group, o.name); n != 1 {
			t.Fatalf("want option %d %s, in stock, in group %d", o.id, o.name, o.group)
		}
		got := priceRows(t, db, "SELECT size, price_paise FROM option_prices WHERE option_id = ? ORDER BY size", o.id)
		if fmt.Sprint(got) != fmt.Sprint(o.prices) {
			t.Fatalf("%s prices = %v, want %v", o.name, got, o.prices)
		}
	}
	if n := count(t, db, "SELECT COUNT(*) FROM options"); n != len(options) {
		t.Fatalf("options = %d, want %d", n, len(options))
	}
}

func TestW2B3_OnlyRestaurantOneHasAMenu(t *testing.T) {
	db, _ := seededScratch(t)
	if n := count(t, db, "SELECT COUNT(*) FROM items WHERE restaurant_id <> 1"); n != 0 {
		t.Fatalf("%d items belong to restaurants other than 1", n)
	}
}

func TestW2B3_SeedExitsWithAnErrorWhenTheDatabaseIsUnreachable(t *testing.T) {
	out, err := command(t, time.Minute, []string{"MYSQL_DSN=root:dev@tcp(127.0.0.1:1)/zomato?timeout=1s"}, "go", "run", "./cmd/seed")
	if err == nil {
		t.Fatalf("go run ./cmd/seed exited 0 with an unreachable database; it must fail so scripts and CI notice\n%s", out)
	}
}
