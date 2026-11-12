package e2e_test

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
)

type orderEvent struct {
	OrderID int64  `json:"order_id"`
	EventID string `json:"event_id"`
	From    string `json:"from"`
	To      string `json:"to"`
}

func eventIDs(t *testing.T, db *sql.DB, orderID int64) []string {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), "SELECT event_id FROM order_events WHERE order_id = ? ORDER BY id", orderID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		out = append(out, id)
	}
	return out
}

func recordsFor(all []*kgo.Record, key string) []*kgo.Record {
	var out []*kgo.Record
	for _, r := range all {
		if string(r.Key) == key {
			out = append(out, r)
		}
	}
	return out
}

func TestW3B6_EveryOrderEventIsPublishedInOrderWithItsOrderAsKey(t *testing.T) {
	db := apiDB(t)
	c := newCast(t)
	o := c.orderAt(t, "PREPARING")
	want := eventIDs(t, db, o.ID)
	k := fmt.Sprint(o.ID)
	got := recordsFor(records(t, "order-events", 15*time.Second, func(rs []*kgo.Record) bool { return len(recordsFor(rs, k)) >= 3 }), k)
	if len(got) != 3 {
		t.Fatalf("order-events has %d records keyed %s within 15 s, want 3 (PLACED, ACCEPTED, PREPARING)", len(got), k)
	}
	steps := []struct{ from, to string }{{"", "PLACED"}, {"PLACED", "ACCEPTED"}, {"ACCEPTED", "PREPARING"}}
	for i, r := range got {
		var e orderEvent
		if err := json.Unmarshal(r.Value, &e); err != nil {
			t.Fatalf("record value %s is not the event JSON: %v", r.Value, err)
		}
		if e.OrderID != o.ID || e.EventID != want[i] || e.From != steps[i].from || e.To != steps[i].to {
			t.Fatalf("record %d = %s, want order %d event %s from %q to %q", i, r.Value, o.ID, want[i], steps[i].from, steps[i].to)
		}
	}
}

func TestW3B6_PublishedRowsAreMarkedSentAndNeverPublishedAgain(t *testing.T) {
	db := apiDB(t)
	c := newCast(t)
	o := c.orderAt(t, "ACCEPTED")
	k := fmt.Sprint(o.ID)
	records(t, "order-events", 15*time.Second, func(rs []*kgo.Record) bool { return len(recordsFor(rs, k)) >= 2 })
	if !eventually(t, 5*time.Second, 200*time.Millisecond, func() bool {
		return count(t, db, "SELECT COUNT(*) FROM outbox WHERE key_ = ? AND sent_at IS NULL", k) == 0
	}) {
		t.Fatal("the order's outbox rows still have sent_at NULL after they were published")
	}
	time.Sleep(3 * time.Second)
	if got := recordsFor(records(t, "order-events", 3*time.Second, nil), k); len(got) != 2 {
		t.Fatalf("order-events has %d records for an order with 2 events, want 2: a sent row must not be published again", len(got))
	}
}

func outboxRow(t *testing.T, db *sql.DB, topic, key string, seq int) int64 {
	t.Helper()
	res := mustExec(t, db, "INSERT INTO outbox (topic, key_, payload) VALUES (?, ?, JSON_OBJECT('seq', ?))", topic, key, seq)
	id, _ := res.LastInsertId()
	t.Cleanup(func() { _, _ = db.Exec("DELETE FROM outbox WHERE id = ?", id) })
	return id
}

func seqs(t *testing.T, rs []*kgo.Record) []int {
	t.Helper()
	var out []int
	for _, r := range rs {
		var p struct {
			Seq int `json:"seq"`
		}
		if err := json.Unmarshal(r.Value, &p); err != nil {
			t.Fatalf("value %s: %v", r.Value, err)
		}
		out = append(out, p.Seq)
	}
	return out
}

func TestW3B6_EachRowGoesToItsTopicInIDOrder(t *testing.T) {
	db := apiDB(t)
	topic := key("w3b6-order")
	for i := range 5 {
		outboxRow(t, db, topic, "k", i)
	}
	got := records(t, topic, 15*time.Second, func(rs []*kgo.Record) bool { return len(rs) >= 5 })
	if fmt.Sprint(seqs(t, got)) != "[0 1 2 3 4]" {
		t.Fatalf("records on %s = %v, want [0 1 2 3 4] in id order", topic, seqs(t, got))
	}
}

func TestW3B6_ARowAnotherTransactionHoldsDoesNotHoldUpTheRest(t *testing.T) {
	db := apiDB(t)
	topic := key("w3b6-locked")
	locked := outboxRow(t, db, topic, "k", 0)
	tx, err := db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(t.Context(), "SELECT id FROM outbox WHERE id = ? FOR UPDATE", locked); err != nil {
		t.Fatal(err)
	}
	outboxRow(t, db, topic, "k", 1)
	got := records(t, topic, 8*time.Second, func(rs []*kgo.Record) bool { return len(rs) >= 1 })
	if fmt.Sprint(seqs(t, got)) != "[1]" {
		t.Fatalf("while row 0 was locked by another transaction, %s got %v, want [1]: skip locked rows instead of waiting for them", topic, seqs(t, got))
	}
	_ = tx.Rollback()
	got = records(t, topic, 10*time.Second, func(rs []*kgo.Record) bool { return len(rs) >= 2 })
	if len(got) != 2 {
		t.Fatalf("after the lock was released %s has %d records, want 2", topic, len(got))
	}
}

func customerID(t *testing.T, c *client) int64 {
	t.Helper()
	var me struct {
		ID int64 `json:"id"`
	}
	c.get("/v1/me").JSON(t, &me)
	return me.ID
}

func notifications(t *testing.T, db *sql.DB, eventID string) int {
	t.Helper()
	return count(t, db, "SELECT COUNT(*) FROM notifications WHERE event_id = ?", eventID)
}

func waitNotified(t *testing.T, db *sql.DB, eventIDs []string, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for {
		missing := ""
		for _, id := range eventIDs {
			if notifications(t, db, id) == 0 {
				missing = id
				break
			}
		}
		if missing == "" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("event %s has no notifications row after %v", missing, within)
		}
		time.Sleep(250 * time.Millisecond)
	}
}

func TestW3B7_EveryEventGetsExactlyOneNotificationForTheCustomer(t *testing.T) {
	db := apiDB(t)
	startWorker(t)
	c := newCast(t)
	o := c.orderAt(t, "ACCEPTED")
	ids := eventIDs(t, db, o.ID)
	waitNotified(t, db, ids, 60*time.Second)
	cust := customerID(t, c.customer)
	for _, id := range ids {
		if n := count(t, db, "SELECT COUNT(*) FROM notifications WHERE event_id = ? AND user_id = ? AND order_id = ?", id, cust, o.ID); n != 1 {
			t.Fatalf("event %s: %d notifications for customer %d and order %d, want 1", id, n, cust, o.ID)
		}
	}
}

func TestW3B7_ARedeliveredEventDoesNotNotifyTwice(t *testing.T) {
	db := apiDB(t)
	startWorker(t)
	c := newCast(t)
	o := c.place(t)
	ids := eventIDs(t, db, o.ID)
	waitNotified(t, db, ids, 60*time.Second)
	value, _ := json.Marshal(orderEvent{OrderID: o.ID, EventID: ids[0], From: "", To: "PLACED"})
	for range 3 {
		produce(t, "order-events", fmt.Sprint(o.ID), string(value))
	}
	marker := c.place(t)
	waitNotified(t, db, eventIDs(t, db, marker.ID), 60*time.Second)
	if n := notifications(t, db, ids[0]); n != 1 {
		t.Fatalf("the PLACED event delivered four times made %d notifications, want 1", n)
	}
}

func TestW3B7_AFailedSendIsRetriedUntilItSucceeds(t *testing.T) {
	db := apiDB(t)
	startWorker(t, "FAIL_EVERY=2")
	c := newCast(t)
	var ids []string
	var keys []string
	for range 4 {
		o := c.place(t)
		ids = append(ids, eventIDs(t, db, o.ID)...)
		keys = append(keys, fmt.Sprint(o.ID))
	}
	waitNotified(t, db, ids, 90*time.Second)
	for _, id := range ids {
		if n := notifications(t, db, id); n != 1 {
			t.Fatalf("event %s has %d notifications, want 1", id, n)
		}
	}
	retried := false
	for _, r := range records(t, "order-events.retry", 5*time.Second, nil) {
		for _, k := range keys {
			if string(r.Key) == k && header(r, "attempt") == "2" {
				retried = true
			}
		}
	}
	if !retried {
		t.Fatal("with every second send failing, none of these orders went through order-events.retry with attempt=2")
	}
}

func TestW3B7_AnEventThatFailsThreeTimesEndsInTheDLQ(t *testing.T) {
	db := apiDB(t)
	startWorker(t, "FAIL_EVERY=1")
	c := newCast(t)
	o := c.place(t)
	k := fmt.Sprint(o.ID)
	dlq := recordsFor(records(t, "order-events.dlq", 60*time.Second, func(rs []*kgo.Record) bool { return len(recordsFor(rs, k)) >= 1 }), k)
	if len(dlq) != 1 || header(dlq[0], "attempt") != "3" {
		t.Fatalf("order-events.dlq has %d records for order %s, want 1 with attempt=3", len(dlq), k)
	}
	var attempts []string
	for _, r := range recordsFor(records(t, "order-events.retry", 5*time.Second, nil), k) {
		attempts = append(attempts, header(r, "attempt"))
	}
	if fmt.Sprint(attempts) != "[2 3]" {
		t.Fatalf("order-events.retry attempts for order %s = %v, want [2 3]", k, attempts)
	}
	if n := notifications(t, db, eventIDs(t, db, o.ID)[0]); n != 0 {
		t.Fatalf("an event whose every send failed has %d notifications, want 0", n)
	}
}

func TestW3B7_ARecordThatIsNotAnEventGoesStraightToTheDLQ(t *testing.T) {
	startWorker(t)
	k := key("garbage")
	produce(t, "order-events", k, "this is not an event")
	dlq := recordsFor(records(t, "order-events.dlq", 30*time.Second, func(rs []*kgo.Record) bool { return len(recordsFor(rs, k)) >= 1 }), k)
	if len(dlq) != 1 || string(dlq[0].Value) != "this is not an event" {
		t.Fatalf("order-events.dlq has %d records keyed %s, want the unreadable record once", len(dlq), k)
	}
	if n := len(recordsFor(records(t, "order-events.retry", 3*time.Second, nil), k)); n != 0 {
		t.Fatalf("an unreadable record was retried %d times; it can never succeed, so send it to the DLQ directly", n)
	}
}

func TestW3B7_AWorkerKilledMidStreamNeitherLosesNorDuplicates(t *testing.T) {
	db := apiDB(t)
	first := startWorker(t)
	c := newCast(t)
	var ids []string
	for range 30 {
		ids = append(ids, eventIDs(t, db, c.place(t).ID)...)
	}
	eventually(t, 30*time.Second, 50*time.Millisecond, func() bool { return notifications(t, db, ids[2]) > 0 })
	first.kill()
	startWorker(t)
	waitNotified(t, db, ids, 90*time.Second)
	for _, id := range ids {
		if n := notifications(t, db, id); n != 1 {
			t.Fatalf("after kill -9 and a restart, event %s has %d notifications, want 1", id, n)
		}
	}
}

func TestW3B8_TheAPIExitsCleanlyWithinTenSecondsOfSIGTERM(t *testing.T) {
	base, p := startAPI(t)
	if r := clientFor(t, base).get("/healthz"); r.Status != http.StatusOK {
		t.Fatalf("second API instance: %d", r.Status)
	}
	code, took := p.stop(10 * time.Second)
	if code != 0 {
		t.Fatalf("the API exited with status %d after SIGTERM (in %v), want 0\n%s", code, took.Round(time.Millisecond), lastOutput(p.out.String()))
	}
	if c, err := net.DialTimeout("tcp", base[len("http://"):], 300*time.Millisecond); err == nil {
		_ = c.Close()
		t.Fatal("something still listens on the API's address after it exited")
	}
}

func TestW3B8_TheWorkerExitsCleanlyWithinTenSecondsOfSIGTERM(t *testing.T) {
	p := startWorker(t)
	time.Sleep(3 * time.Second)
	code, took := p.stop(10 * time.Second)
	if code != 0 {
		t.Fatalf("the worker exited with status %d after SIGTERM (in %v), want 0\n%s", code, took.Round(time.Millisecond), lastOutput(p.out.String()))
	}
}

func TestW3B8_TheAPIRefusesToStartWithoutAStrongJWTSecret(t *testing.T) {
	bin := binary(t, "TEST_API_BIN", "TEST_API_ERROR")
	for _, secret := range []string{"", "short"} {
		p := startProc(t, "API with JWT_SECRET="+strconv.Quote(secret), bin, apiEnv(t, "JWT_SECRET="+secret, "HTTP_ADDR="+freeAddr(t)))
		select {
		case <-p.done:
			if code, _ := p.stop(time.Second); code == 0 {
				t.Fatalf("with JWT_SECRET=%q the API exited with status 0, want a failure status", secret)
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("with JWT_SECRET=%q the API kept running; refuse to start with a secret under 32 bytes", secret)
		}
	}
}
