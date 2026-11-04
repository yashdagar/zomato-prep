package geo_test

import (
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func spot() (lat, lng float64, base int64) {
	return -60 + rand.Float64()*120, -170 + rand.Float64()*340, time.Now().UnixNano() % 1_000_000_000
}

func TestW5B3_PingGivesTheSeenKeyAShortTTL(t *testing.T) {
	r, rdb := riders(t)
	lat, lng, id := spot()
	if err := r.Ping(t.Context(), id, lat, lng); err != nil {
		t.Fatal(err)
	}
	ttl, err := rdb.TTL(t.Context(), fmt.Sprintf("rider:seen:%d", id)).Result()
	if err != nil {
		t.Fatal(err)
	}
	if ttl <= 0 || ttl > 15*time.Second {
		t.Fatalf("rider:seen TTL = %s, want between 0 and 15s", ttl)
	}
}

func TestW5B3_NearestDropsAndCleansUpRidersWhoWentSilent(t *testing.T) {
	r, rdb := riders(t)
	lat, lng, id := spot()
	if err := r.Ping(t.Context(), id, lat, lng); err != nil {
		t.Fatal(err)
	}
	if err := rdb.Del(t.Context(), fmt.Sprintf("rider:seen:%d", id)).Err(); err != nil {
		t.Fatal(err)
	}
	got, err := r.Nearest(t.Context(), lat, lng, 5, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("Nearest = %v, want no riders because the seen key expired", got)
	}
	if err := rdb.ZScore(t.Context(), "riders:online", fmt.Sprint(id)).Err(); err != redis.Nil {
		t.Fatalf("stale rider is still in riders:online (err=%v)", err)
	}
}

func TestW5B3_NearestHonoursTheRadiusAndTheCount(t *testing.T) {
	r, _ := riders(t)
	lat, lng, base := spot()
	ids := []int64{base + 1, base + 2, base + 3, base + 4}
	offsets := []float64{0.001, 0.004, 0.008, 0.5}
	for i, id := range ids {
		if err := r.Ping(t.Context(), id, lat+offsets[i], lng); err != nil {
			t.Fatal(err)
		}
	}
	got, err := r.Nearest(t.Context(), lat, lng, 5, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != ids[0] || got[1] != ids[1] {
		t.Fatalf("Nearest(count 2) = %v, want %v", got, ids[:2])
	}
	all, err := r.Nearest(t.Context(), lat, lng, 5, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("Nearest(5 km) = %v, want the three riders inside 5 km and not the one 55 km away", all)
	}
}
