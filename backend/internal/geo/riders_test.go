package geo_test

import (
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/yashdagar/zomato-prep/backend/internal/geo"
)

func riders(t *testing.T) (*geo.Riders, *redis.Client) {
	t.Helper()
	addr := os.Getenv("TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("set TEST_REDIS_ADDR=127.0.0.1:6379 to run")
	}
	rdb := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { _ = rdb.Close() })
	return geo.NewRiders(rdb), rdb
}

func TestW5B3_NearestReturnsClosestFirstAndDropsOfflineRiders(t *testing.T) {
	r, _ := riders(t)
	base := time.Now().UnixNano() % 1_000_000
	near, far, gone := base+1, base+2, base+3
	if err := r.Ping(t.Context(), near, 28.4600, 77.0300); err != nil {
		t.Fatal(err)
	}
	if err := r.Ping(t.Context(), far, 28.4800, 77.0500); err != nil {
		t.Fatal(err)
	}
	if err := r.Ping(t.Context(), gone, 28.4601, 77.0301); err != nil {
		t.Fatal(err)
	}
	if err := r.Offline(t.Context(), gone); err != nil {
		t.Fatal(err)
	}
	got, err := r.Nearest(t.Context(), 28.46, 77.03, 5, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) < 2 || got[0] != near {
		t.Fatalf("Nearest = %v, want %d first", got, near)
	}
	for _, id := range got {
		if id == gone {
			t.Fatalf("Nearest = %v still contains the offline rider %d", got, gone)
		}
	}
}

func TestW5B4_ClaimGoesToExactlyOneRider(t *testing.T) {
	r, _ := riders(t)
	orderID := time.Now().UnixNano()
	okA, err := r.Claim(t.Context(), orderID, 11)
	if err != nil || !okA {
		t.Fatalf("first claim: ok=%v err=%v", okA, err)
	}
	okB, err := r.Claim(t.Context(), orderID, 12)
	if err != nil || okB {
		t.Fatalf("second claim: ok=%v err=%v, want false", okB, err)
	}
	by, err := r.ClaimedBy(t.Context(), orderID)
	if err != nil || by != 11 {
		t.Fatalf("ClaimedBy = %d, %v, want 11", by, err)
	}
}
