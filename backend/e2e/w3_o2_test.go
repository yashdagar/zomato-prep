//go:build w3o2

package e2e_test

import (
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
)

func TestW3O2_TwoAPIInstancesPublishEveryRowExactlyOnce(t *testing.T) {
	db := apiDB(t)
	startAPI(t)
	topic := key("w3o2-once")
	for i := range 200 {
		outboxRow(t, db, topic, "k", i)
	}
	got := records(t, topic, 30*time.Second, func(rs []*kgo.Record) bool { return len(rs) >= 200 })
	time.Sleep(2 * time.Second)
	got = records(t, topic, 3*time.Second, nil)
	seen := map[int]int{}
	for _, s := range seqs(t, got) {
		seen[s]++
	}
	for i := range 200 {
		if seen[i] != 1 {
			t.Fatalf("row %d was published %d times by two relays, want exactly once", i, seen[i])
		}
	}
	if n := count(t, db, "SELECT COUNT(*) FROM outbox WHERE topic = ? AND sent_at IS NULL", topic); n != 0 {
		t.Fatalf("%d rows still unsent", n)
	}
}

func TestW3O2_ARelayThatCannotReachKafkaLeavesRowsForTheOthers(t *testing.T) {
	db := apiDB(t)
	startAPI(t, "KAFKA_BROKERS=127.0.0.1:1")
	time.Sleep(2 * time.Second)
	topic := key("w3o2-dead")
	for i := range 50 {
		outboxRow(t, db, topic, "k", i)
	}
	got := records(t, topic, 45*time.Second, func(rs []*kgo.Record) bool { return len(rs) >= 50 })
	if len(got) != 50 {
		t.Fatalf("with one relay unable to reach Kafka, %s got %d of 50 records; a failed publish must leave its rows unsent and unlocked", topic, len(got))
	}
	unsent := func() int {
		return count(t, db, "SELECT COUNT(*) FROM outbox WHERE topic = ? AND sent_at IS NULL", topic)
	}
	if !eventually(t, 10*time.Second, 200*time.Millisecond, func() bool { return unsent() == 0 }) {
		t.Fatalf("%d rows still unsent 10s after all 50 records arrived", unsent())
	}
}
