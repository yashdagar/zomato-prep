package geo_test

import (
	"os"
	"testing"

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
	lat, lng, base := spot()
	near, far, gone := base+1, base+2, base+3
	if err := r.Ping(t.Context(), near, lat, lng); err != nil {
		t.Fatal(err)
	}
	if err := r.Ping(t.Context(), far, lat+0.02, lng+0.02); err != nil {
		t.Fatal(err)
	}
	if err := r.Ping(t.Context(), gone, lat+0.0001, lng+0.0001); err != nil {
		t.Fatal(err)
	}
	if err := r.Offline(t.Context(), gone); err != nil {
		t.Fatal(err)
	}
	got, err := r.Nearest(t.Context(), lat, lng, 5, 5)
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
