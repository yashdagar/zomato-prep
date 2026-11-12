package e2e_test

import (
	"database/sql"
	"testing"
)

var orderTables = map[string][]string{
	"users":        {"id", "email", "password_hash", "role", "restaurant_id"},
	"orders":       {"id", "user_id", "restaurant_id", "rider_id", "status", "version", "total_paise", "created_at"},
	"order_lines":  {"id", "order_id", "item_id", "config_json", "unit_paise", "qty"},
	"order_events": {"id", "event_id", "order_id", "from_status", "to_status", "actor", "created_at"},
	"outbox":       {"id", "topic", "key_", "payload", "created_at", "sent_at"},
}

func scratchOrder(t *testing.T, db *sql.DB) int64 {
	t.Helper()
	res := mustExec(t, db, "INSERT INTO users (email, password_hash, role) VALUES ('schema@test.local', REPEAT('x', 60), 'customer')")
	user, _ := res.LastInsertId()
	mustExec(t, db, "INSERT INTO restaurants (id, name, lat, lng) VALUES (1, 'r', 28.46, 77.03)")
	res = mustExec(t, db, "INSERT INTO orders (user_id, restaurant_id, status, total_paise) VALUES (?, 1, 'PLACED', 100)", user)
	id, _ := res.LastInsertId()
	return id
}

func TestW3B3_MigrationsCreateTheOrderTables(t *testing.T) {
	db := migratedScratch(t)
	for table, cols := range orderTables {
		for _, c := range cols {
			if n := count(t, db, "SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = ? AND column_name = ?", table, c); n != 1 {
				t.Fatalf("table %s has no column %s", table, c)
			}
		}
	}
}

func TestW3B3_ANewOrderStartsAtVersionOneWithNoRider(t *testing.T) {
	db := migratedScratch(t)
	id := scratchOrder(t, db)
	if n := count(t, db, "SELECT COUNT(*) FROM orders WHERE id = ? AND version = 1 AND rider_id IS NULL AND created_at IS NOT NULL", id); n != 1 {
		t.Fatal("a new order should get version 1, rider_id NULL and created_at set by default")
	}
}

func TestW3B3_AnEventIDIsUniqueAcrossAllOrders(t *testing.T) {
	db := migratedScratch(t)
	a := scratchOrder(t, db)
	res := mustExec(t, db, "INSERT INTO orders (user_id, restaurant_id, status, total_paise) SELECT user_id, restaurant_id, status, total_paise FROM orders WHERE id = ?", a)
	b, _ := res.LastInsertId()
	insert := "INSERT INTO order_events (event_id, order_id, from_status, to_status, actor) VALUES ('same-event-id', ?, '', 'PLACED', 'customer')"
	mustExec(t, db, insert, a)
	mustFail(t, db, "the same event_id on another order", insert, b)
}

func TestW3B3_LinesAndEventsNeedAnExistingOrder(t *testing.T) {
	db := migratedScratch(t)
	mustFail(t, db, "a line for a missing order", "INSERT INTO order_lines (order_id, item_id, config_json, unit_paise, qty) VALUES (999999, 1, '{}', 100, 1)")
	mustFail(t, db, "an event for a missing order", "INSERT INTO order_events (event_id, order_id, from_status, to_status, actor) VALUES ('orphan', 999999, '', 'PLACED', 'customer')")
}

func TestW3B3_AnOutboxRowStartsUnsent(t *testing.T) {
	db := migratedScratch(t)
	res := mustExec(t, db, "INSERT INTO outbox (topic, key_, payload) VALUES ('order-events', '1', JSON_OBJECT('order_id', 1))")
	id, _ := res.LastInsertId()
	if n := count(t, db, "SELECT COUNT(*) FROM outbox WHERE id = ? AND sent_at IS NULL AND created_at IS NOT NULL", id); n != 1 {
		t.Fatal("a new outbox row should have sent_at NULL and created_at set")
	}
}

func TestW3B3_TheHotQueriesCanUseAnIndex(t *testing.T) {
	db := migratedScratch(t)
	queries := map[string]string{
		"orders":       "SELECT id FROM orders WHERE user_id = 7 AND (created_at, id) < ('2026-01-01', 99) ORDER BY created_at DESC, id DESC LIMIT 20",
		"order_events": "SELECT event_id FROM order_events WHERE order_id = 7 ORDER BY id",
		"outbox":       "SELECT id FROM outbox WHERE sent_at IS NULL ORDER BY id LIMIT 100",
	}
	for table, q := range queries {
		if keys := explainKeys(t, db, q); keys[table] == "" {
			t.Fatalf("EXPLAIN finds no usable index on %s for:\n%s", table, q)
		}
	}
	if keys := explainKeys(t, db, "SELECT id FROM orders WHERE restaurant_id = 3 AND status = 'PLACED'"); keys["orders"] == "" {
		t.Fatal("EXPLAIN finds no usable index on orders for a restaurant's orders by status")
	}
}
