//go:build w3o2

package outbox

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
)

func drainTopic(t *testing.T, brokers []string, topic string, quiet time.Duration) []*kgo.Record {
	t.Helper()
	cl, err := kgo.NewClient(kgo.SeedBrokers(brokers...), kgo.ConsumeTopics(topic), kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()))
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	var got []*kgo.Record
	for {
		ctx, cancel := context.WithTimeout(t.Context(), quiet)
		before := len(got)
		cl.PollFetches(ctx).EachRecord(func(r *kgo.Record) { got = append(got, r) })
		cancel()
		if len(got) == before {
			return got
		}
	}
}

func runTwoRelays(t *testing.T, db *sql.DB, brokers []string, ids []int64, batch int) [2]int {
	t.Helper()
	var sent [2]int
	var wg sync.WaitGroup
	deadline := time.Now().Add(30 * time.Second)
	for i := range sent {
		relay := NewRelay(db, producer(t, brokers)).WithBatch(batch)
		wg.Add(1)
		go func() {
			defer wg.Done()
			for unsent(t, db, ids) > 0 && time.Now().Before(deadline) {
				n, err := relay.Tick(t.Context())
				if err != nil {
					t.Errorf("relay %d Tick: %v", i, err)
					return
				}
				sent[i] += n
			}
		}()
	}
	wg.Wait()
	return sent
}

func TestW3O2_TwoRelaysPublishEveryRowExactlyOnce(t *testing.T) {
	db, brokers := deps(t)
	topic := fmt.Sprintf("w3o2-once-%d", time.Now().UnixNano())
	ids := insertRows(t, db, topic, 60)
	runTwoRelays(t, db, brokers, ids, 5)
	if n := unsent(t, db, ids); n != 0 {
		t.Fatalf("%d of 60 rows are still unsent", n)
	}
	seen := map[int]int{}
	for _, rec := range drainTopic(t, brokers, topic, 3*time.Second) {
		var p struct {
			Seq int `json:"seq"`
		}
		if err := json.Unmarshal(rec.Value, &p); err != nil {
			t.Fatal(err)
		}
		seen[p.Seq]++
	}
	for seq := 0; seq < 60; seq++ {
		if seen[seq] != 1 {
			t.Fatalf("seq %d was published %d times, want exactly 1", seq, seen[seq])
		}
	}
}

func TestW3O2_BothRelaysClaimRows(t *testing.T) {
	db, brokers := deps(t)
	topic := fmt.Sprintf("w3o2-share-%d", time.Now().UnixNano())
	ids := insertRows(t, db, topic, 200)
	sent := runTwoRelays(t, db, brokers, ids, 5)
	if sent[0] == 0 || sent[1] == 0 {
		t.Fatalf("relays marked %d and %d rows; with SKIP LOCKED both should get work", sent[0], sent[1])
	}
	if sent[0]+sent[1] < len(ids) {
		t.Fatalf("relays marked %d rows in total, want at least %d", sent[0]+sent[1], len(ids))
	}
}

func TestW3O2_TickClaimsAtMostTheBatchSize(t *testing.T) {
	db, brokers := deps(t)
	topic := fmt.Sprintf("w3o2-batch-%d", time.Now().UnixNano())
	ids := insertRows(t, db, topic, 7)
	n, err := NewRelay(db, producer(t, brokers)).WithBatch(3).Tick(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 || unsent(t, db, ids) != 4 {
		t.Fatalf("Tick marked %d rows and left %d unsent, want 3 and 4", n, unsent(t, db, ids))
	}
}

func TestW3O2_FailedPublishLeavesRowsUnsentAndUnlocked(t *testing.T) {
	db, brokers := deps(t)
	topic := fmt.Sprintf("w3o2-fail-%d", time.Now().UnixNano())
	ids := insertRows(t, db, topic, 3)
	dead, err := kgo.NewClient(kgo.SeedBrokers("127.0.0.1:1"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(dead.Close)
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if _, err := NewRelay(db, dead).Tick(ctx); err == nil {
		t.Fatal("Tick against an unreachable broker returned nil, want an error")
	}
	if n := unsent(t, db, ids); n != 3 {
		t.Fatalf("%d rows unsent after the failed Tick, want all 3", n)
	}
	if n, err := NewRelay(db, producer(t, brokers)).Tick(t.Context()); err != nil || n < 3 {
		t.Fatalf("healthy relay after the failure marked %d rows, err %v; the failed relay must release its row locks", n, err)
	}
}
