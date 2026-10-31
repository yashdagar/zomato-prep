package notify

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

type failingSender struct{}

func (failingSender) Send(context.Context, int64, int64, string) error {
	return errors.New("push provider down")
}

func openDB(t *testing.T) *sql.DB {
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

func newEvent(t *testing.T, db *sql.DB) Event {
	t.Helper()
	res, err := db.ExecContext(t.Context(), `INSERT INTO orders (user_id, restaurant_id, status, total_paise) VALUES (4242, 1, 'ACCEPTED', 100)`)
	if err != nil {
		t.Fatal(err)
	}
	orderID, _ := res.LastInsertId()
	e := Event{OrderID: orderID, EventID: fmt.Sprintf("w3b7-%d", time.Now().UnixNano()), From: "PLACED", To: "ACCEPTED"}
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = db.ExecContext(ctx, `DELETE FROM notifications WHERE event_id = ?`, e.EventID)
		_, _ = db.ExecContext(ctx, `DELETE FROM processed_events WHERE event_id = ?`, e.EventID)
		_, _ = db.ExecContext(ctx, `DELETE FROM orders WHERE id = ?`, orderID)
	})
	return e
}

func count(t *testing.T, db *sql.DB, query, eventID string) int {
	t.Helper()
	var n int
	if err := db.QueryRowContext(t.Context(), query, eventID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestW3B7_SameEventTwiceMakesOneNotification(t *testing.T) {
	db := openDB(t)
	e := newEvent(t, db)
	p := NewProcessor(db, &LogSender{})
	first, err := p.Process(t.Context(), e)
	if err != nil || !first {
		t.Fatalf("first delivery: new=%v err=%v, want a new event", first, err)
	}
	again, err := p.Process(t.Context(), e)
	if err != nil || again {
		t.Fatalf("redelivery: new=%v err=%v, want a recognised duplicate", again, err)
	}
	if n := count(t, db, `SELECT COUNT(*) FROM notifications WHERE event_id = ?`, e.EventID); n != 1 {
		t.Fatalf("notifications for the event = %d, want exactly 1", n)
	}
}

func TestW3B7_FailedSendLeavesNothingSoARetryCanSucceed(t *testing.T) {
	db := openDB(t)
	e := newEvent(t, db)
	if _, err := NewProcessor(db, failingSender{}).Process(t.Context(), e); err == nil {
		t.Fatal("Process must return the send error")
	}
	if n := count(t, db, `SELECT COUNT(*) FROM processed_events WHERE event_id = ?`, e.EventID); n != 0 {
		t.Fatal("a failed send left a processed_events row behind; the dedupe row and the send must share one transaction")
	}
	if ok, err := NewProcessor(db, &LogSender{}).Process(t.Context(), e); err != nil || !ok {
		t.Fatalf("retry after the failure: new=%v err=%v, want it processed", ok, err)
	}
	if n := count(t, db, `SELECT COUNT(*) FROM notifications WHERE event_id = ?`, e.EventID); n != 1 {
		t.Fatalf("notifications after the retry = %d, want 1", n)
	}
}

func TestW3B7_FailuresGoToRetryTwiceThenDLQ(t *testing.T) {
	for attempt, want := range map[int]string{1: RetryTopic, 2: RetryTopic, 3: DLQTopic, 4: DLQTopic} {
		if got := NextTopic(attempt); got != want {
			t.Errorf("NextTopic(%d) = %s, want %s: three attempts in total, then the DLQ", attempt, got, want)
		}
	}
}

func TestW3B7_FailEveryFailsEveryNthSend(t *testing.T) {
	s := &LogSender{FailEvery: 5}
	var failed []int
	for i := 1; i <= 10; i++ {
		if s.Send(t.Context(), 1, 1, "ACCEPTED") != nil {
			failed = append(failed, i)
		}
	}
	if fmt.Sprint(failed) != "[5 10]" {
		t.Fatalf("FailEvery 5 failed calls %v, want [5 10]", failed)
	}
}
