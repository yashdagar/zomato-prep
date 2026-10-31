// Package schema_test checks migrations against a real MySQL. The tests run
// only with TEST_MYSQL_DSN set, after make migrate, and roll back any rows.
package schema_test

import (
	"database/sql"
	"errors"
	"os"
	"slices"
	"testing"

	"github.com/go-sql-driver/mysql"
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

func indexColumns(t *testing.T, db *sql.DB, table, index string) []string {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), `SELECT column_name FROM information_schema.statistics
		WHERE table_schema = DATABASE() AND table_name = ? AND index_name = ? ORDER BY seq_in_index`, table, index)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var cols []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			t.Fatal(err)
		}
		cols = append(cols, c)
	}
	return cols
}

func mysqlErrno(err error) uint16 {
	var me *mysql.MySQLError
	if errors.As(err, &me) {
		return me.Number
	}
	return 0
}

func tx(t *testing.T, db *sql.DB) *sql.Tx {
	t.Helper()
	tx, err := db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })
	return tx
}

func TestW3B3_IndexesHaveTheirColumnsInOrder(t *testing.T) {
	db := open(t)
	want := map[[2]string][]string{
		{"orders", "idx_orders_user_page"}:         {"user_id", "created_at", "id"},
		{"orders", "idx_orders_rest_status"}:       {"restaurant_id", "status"},
		{"order_events", "uq_order_events_event"}:  {"event_id"},
		{"order_events", "idx_order_events_order"}: {"order_id", "id"},
		{"outbox", "idx_outbox_unsent"}:            {"sent_at", "id"},
	}
	for k, cols := range want {
		if got := indexColumns(t, db, k[0], k[1]); !slices.Equal(got, cols) {
			t.Errorf("%s.%s columns = %v, want %v", k[0], k[1], got, cols)
		}
	}
}

func TestW3B3_NewOrderStartsAtVersionOne(t *testing.T) {
	db := open(t)
	x := tx(t, db)
	res, err := x.ExecContext(t.Context(), `INSERT INTO orders (user_id, restaurant_id, status, total_paise) VALUES (999999, 999999, 'PLACED', 100)`)
	if err != nil {
		t.Fatalf("insert an order for ids that exist nowhere (no FK on user_id or restaurant_id): %v", err)
	}
	id, _ := res.LastInsertId()
	var version int
	var rider sql.NullInt64
	if err := x.QueryRowContext(t.Context(), `SELECT version, rider_id FROM orders WHERE id = ?`, id).Scan(&version, &rider); err != nil {
		t.Fatal(err)
	}
	if version != 1 || rider.Valid {
		t.Fatalf("new order version = %d, rider_id valid = %v; want version 1 and rider_id NULL", version, rider.Valid)
	}
}

func TestW3B3_EventIDIsUnique(t *testing.T) {
	db := open(t)
	x := tx(t, db)
	res, err := x.ExecContext(t.Context(), `INSERT INTO orders (user_id, restaurant_id, status, total_paise) VALUES (1, 1, 'PLACED', 100)`)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	insert := `INSERT INTO order_events (event_id, order_id, from_status, to_status, actor) VALUES ('schema-test-event-0000000000000000', ?, '', 'PLACED', 'customer')`
	if _, err := x.ExecContext(t.Context(), insert, id); err != nil {
		t.Fatal(err)
	}
	if _, err := x.ExecContext(t.Context(), insert, id); mysqlErrno(err) != 1062 {
		t.Fatalf("second event with the same event_id: err = %v, want MySQL error 1062", err)
	}
}

func TestW3B3_OrderLineNeedsAnExistingOrder(t *testing.T) {
	db := open(t)
	x := tx(t, db)
	_, err := x.ExecContext(t.Context(), `INSERT INTO order_lines (order_id, item_id, config_json, unit_paise, qty) VALUES (18446744073709551000, 1, '{}', 100, 1)`)
	if mysqlErrno(err) != 1452 {
		t.Fatalf("line for a missing order: err = %v, want MySQL error 1452 (foreign key)", err)
	}
}

func TestW3B3_OutboxRowStartsUnsent(t *testing.T) {
	db := open(t)
	x := tx(t, db)
	res, err := x.ExecContext(t.Context(), `INSERT INTO outbox (topic, key_, payload) VALUES ('order-events', '1', JSON_OBJECT('order_id', 1))`)
	if err != nil {
		t.Fatalf("insert outbox row: %v", err)
	}
	id, _ := res.LastInsertId()
	var sent sql.NullTime
	if err := x.QueryRowContext(t.Context(), `SELECT sent_at FROM outbox WHERE id = ?`, id).Scan(&sent); err != nil {
		t.Fatal(err)
	}
	if sent.Valid {
		t.Fatal("a new outbox row must have sent_at NULL")
	}
}
