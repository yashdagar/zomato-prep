package outbox

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/twmb/franz-go/pkg/kgo"
)

func deps(t *testing.T) (*sql.DB, []string) {
	t.Helper()
	dsn, brokers := os.Getenv("TEST_MYSQL_DSN"), os.Getenv("TEST_KAFKA_BROKERS")
	if dsn == "" || brokers == "" {
		t.Skip("set TEST_MYSQL_DSN and TEST_KAFKA_BROKERS=127.0.0.1:9092 after make up and make migrate to run")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db, strings.Split(brokers, ",")
}

func producer(t *testing.T, brokers []string) *kgo.Client {
	t.Helper()
	cl, err := kgo.NewClient(kgo.SeedBrokers(brokers...), kgo.AllowAutoTopicCreation())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cl.Close)
	return cl
}

func insertRows(t *testing.T, db *sql.DB, topic string, n int) []int64 {
	t.Helper()
	var ids []int64
	for i := 0; i < n; i++ {
		res, err := db.ExecContext(t.Context(), `INSERT INTO outbox (topic, key_, payload) VALUES (?, ?, JSON_OBJECT('seq', ?))`, topic, "order-7", i)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := res.LastInsertId()
		ids = append(ids, id)
	}
	t.Cleanup(func() {
		for _, id := range ids {
			_, _ = db.ExecContext(context.Background(), `DELETE FROM outbox WHERE id = ?`, id)
		}
	})
	return ids
}

func consume(t *testing.T, brokers []string, topic string, want int) []*kgo.Record {
	t.Helper()
	cl, err := kgo.NewClient(kgo.SeedBrokers(brokers...), kgo.ConsumeTopics(topic), kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()))
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	var got []*kgo.Record
	for len(got) < want && ctx.Err() == nil {
		cl.PollFetches(ctx).EachRecord(func(r *kgo.Record) { got = append(got, r) })
	}
	return got
}

func unsent(t *testing.T, db *sql.DB, ids []int64) int {
	t.Helper()
	n := 0
	for _, id := range ids {
		var sent sql.NullTime
		if err := db.QueryRowContext(t.Context(), `SELECT sent_at FROM outbox WHERE id = ?`, id).Scan(&sent); err != nil {
			t.Fatal(err)
		}
		if !sent.Valid {
			n++
		}
	}
	return n
}

func TestW3B6_TickPublishesUnsentRowsInOrderAndMarksThemSent(t *testing.T) {
	db, brokers := deps(t)
	topic := fmt.Sprintf("w3b6-%d", time.Now().UnixNano())
	ids := insertRows(t, db, topic, 3)
	r := NewRelay(db, producer(t, brokers))
	if _, err := r.Tick(t.Context()); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if n := unsent(t, db, ids); n != 0 {
		t.Fatalf("%d of the 3 rows still have sent_at NULL after Tick", n)
	}
	got := consume(t, brokers, topic, 3)
	if len(got) != 3 {
		t.Fatalf("consumed %d records from %s, want 3", len(got), topic)
	}
	for i, rec := range got {
		if string(rec.Key) != "order-7" || string(rec.Value) != fmt.Sprintf(`{"seq": %d}`, i) {
			t.Fatalf("record %d = key %q value %s, want key order-7 and seq %d in id order", i, rec.Key, rec.Value, i)
		}
	}
}

func TestW3B6_SecondTickDoesNotRepublishSentRows(t *testing.T) {
	db, brokers := deps(t)
	topic := fmt.Sprintf("w3b6-again-%d", time.Now().UnixNano())
	insertRows(t, db, topic, 2)
	r := NewRelay(db, producer(t, brokers))
	if _, err := r.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := consume(t, brokers, topic, 3); len(got) != 2 {
		t.Fatalf("two ticks put %d records on %s, want 2: sent rows must not be published again", len(got), topic)
	}
}

func TestW3B6_RowsLockedByAnotherRelayAreSkipped(t *testing.T) {
	db, brokers := deps(t)
	topic := fmt.Sprintf("w3b6-locked-%d", time.Now().UnixNano())
	ids := insertRows(t, db, topic, 2)
	other, err := db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = other.Rollback() }()
	if _, err := other.ExecContext(t.Context(), `SELECT id FROM outbox WHERE id IN (?, ?) FOR UPDATE`, ids[0], ids[1]); err != nil {
		t.Fatal(err)
	}
	r := NewRelay(db, producer(t, brokers))
	done := make(chan error, 1)
	go func() { _, err := r.Tick(t.Context()); done <- err }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Tick: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Tick waited on rows another transaction holds; the SELECT needs FOR UPDATE SKIP LOCKED")
	}
	if n := unsent(t, db, ids); n != 2 {
		t.Fatalf("%d locked rows were marked sent, want 0", 2-n)
	}
}
