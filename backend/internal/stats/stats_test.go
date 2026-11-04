package stats_test

import (
	"context"
	"errors"
	"math/rand"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/yashdagar/zomato-prep/backend/internal/stats"
)

func newStats(t *testing.T) (*stats.Stats, *redis.Client, time.Time) {
	t.Helper()
	addr := os.Getenv("TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("set TEST_REDIS_ADDR=127.0.0.1:6379 to run")
	}
	rdb := redis.NewClient(&redis.Options{Addr: addr})
	now := time.Date(2040, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(rand.Intn(500000)) * time.Minute)
	t.Cleanup(func() {
		for i := 0; i < 40; i++ {
			rdb.Del(context.Background(), stats.Key(now.Add(-time.Duration(i)*time.Minute)))
		}
		_ = rdb.Close()
	})
	return stats.New(rdb), rdb, now
}

func TestW6B7_KeyIsOneBucketPerUTCMinute(t *testing.T) {
	at := time.Date(2026, 11, 4, 7, 5, 59, 0, time.FixedZone("IST", 5*3600+1800))
	if got := stats.Key(at); got != "orders:202611040135" {
		t.Fatalf("Key = %q, want orders:202611040135", got)
	}
	if stats.Key(at) != stats.Key(at.Add(-59*time.Second).Add(time.Second)) {
		t.Fatal("two instants in the same minute must share a key")
	}
}

func TestW6B7_OrdersInTheSameMinuteAreSummed(t *testing.T) {
	s, _, now := newStats(t)
	for i := 0; i < 3; i++ {
		if err := s.Record(t.Context(), now); err != nil {
			t.Fatal(err)
		}
	}
	n, err := s.Orders(t.Context(), now, 5)
	if err != nil || n != 3 {
		t.Fatalf("Orders = %d, %v, want 3", n, err)
	}
}

func TestW6B7_WindowCoversTheLastMinutesIncludingTheCurrentOne(t *testing.T) {
	s, _, now := newStats(t)
	for _, ago := range []time.Duration{0, 4 * time.Minute, 5 * time.Minute, 20 * time.Minute} {
		if err := s.Record(t.Context(), now.Add(-ago)); err != nil {
			t.Fatal(err)
		}
	}
	n, err := s.Orders(t.Context(), now, 5)
	if err != nil || n != 2 {
		t.Fatalf("last 5 minutes = %d, %v, want 2 (now and 4 minutes ago)", n, err)
	}
	n, err = s.Orders(t.Context(), now, 30)
	if err != nil || n != 4 {
		t.Fatalf("last 30 minutes = %d, %v, want 4", n, err)
	}
}

func TestW6B7_MissingBucketsCountAsZero(t *testing.T) {
	s, _, now := newStats(t)
	n, err := s.Orders(t.Context(), now, 30)
	if err != nil || n != 0 {
		t.Fatalf("empty window = %d, %v, want 0", n, err)
	}
}

func TestW6B7_BucketExpiresAfterThirtyOneMinutes(t *testing.T) {
	s, rdb, now := newStats(t)
	if err := s.Record(t.Context(), now); err != nil {
		t.Fatal(err)
	}
	ttl, err := rdb.TTL(t.Context(), stats.Key(now)).Result()
	if err != nil {
		t.Fatal(err)
	}
	if ttl <= 30*time.Minute || ttl > 31*time.Minute {
		t.Fatalf("TTL %v, want just over 30 minutes and at most 31", ttl)
	}
}

func TestW6B7_WindowOutsideOneToThirtyIsRejected(t *testing.T) {
	s, _, now := newStats(t)
	for _, m := range []int{0, -1, 31, 1000} {
		if _, err := s.Orders(t.Context(), now, m); !errors.Is(err, stats.ErrBadWindow) {
			t.Fatalf("minutes=%d: err %v, want ErrBadWindow", m, err)
		}
	}
}
