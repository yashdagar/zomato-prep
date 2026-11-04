package assign_test

import (
	"fmt"
	"os"
	"sync"
	"sync/atomic"
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
func TestW5B4_ClaimExpiresAfterThirtySeconds(t *testing.T) {
	r, rdb := riders(t)
	orderID := time.Now().UnixNano()
	if ok, err := r.Claim(t.Context(), orderID, 5); err != nil || !ok {
		t.Fatalf("claim: ok=%v err=%v", ok, err)
	}
	ttl, err := rdb.PTTL(t.Context(), fmt.Sprintf("order:%d:claim", orderID)).Result()
	if err != nil {
		t.Fatal(err)
	}
	if ttl <= 25*time.Second || ttl > 30*time.Second {
		t.Fatalf("claim TTL = %s, want just under 30s", ttl)
	}
}

func TestW5B4_ExpiredClaimCanBeTakenByAnotherRider(t *testing.T) {
	r, rdb := riders(t)
	orderID := time.Now().UnixNano()
	if ok, _ := r.Claim(t.Context(), orderID, 5); !ok {
		t.Fatal("first claim failed")
	}
	if err := rdb.PExpire(t.Context(), fmt.Sprintf("order:%d:claim", orderID), 30*time.Millisecond).Err(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(80 * time.Millisecond)
	ok, err := r.Claim(t.Context(), orderID, 6)
	if err != nil || !ok {
		t.Fatalf("claim after expiry: ok=%v err=%v", ok, err)
	}
	by, _ := r.ClaimedBy(t.Context(), orderID)
	if by != 6 {
		t.Fatalf("ClaimedBy = %d, want 6", by)
	}
}

func TestW5B4_OnlyTheHolderCanReleaseAClaim(t *testing.T) {
	r, _ := riders(t)
	orderID := time.Now().UnixNano()
	if ok, _ := r.Claim(t.Context(), orderID, 5); !ok {
		t.Fatal("claim failed")
	}
	released, err := r.Release(t.Context(), orderID, 6)
	if err != nil || released {
		t.Fatalf("release by a stranger: released=%v err=%v, want false", released, err)
	}
	if by, _ := r.ClaimedBy(t.Context(), orderID); by != 5 {
		t.Fatalf("ClaimedBy = %d after a stranger's release, want 5", by)
	}
	released, err = r.Release(t.Context(), orderID, 5)
	if err != nil || !released {
		t.Fatalf("release by the holder: released=%v err=%v, want true", released, err)
	}
	if by, _ := r.ClaimedBy(t.Context(), orderID); by != 0 {
		t.Fatalf("ClaimedBy = %d after release, want 0", by)
	}
}

func TestW5B4_ClaimedByIsZeroWhenNobodyHoldsIt(t *testing.T) {
	r, _ := riders(t)
	by, err := r.ClaimedBy(t.Context(), time.Now().UnixNano())
	if err != nil || by != 0 {
		t.Fatalf("ClaimedBy = %d, %v, want 0 and no error", by, err)
	}
}
func race(t *testing.T, orderID int64, contenders []int64) (winners int, holder int64) {
	t.Helper()
	r, _ := riders(t)
	start := make(chan struct{})
	var won atomic.Int32
	var wg sync.WaitGroup
	for _, id := range contenders {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			ok, err := r.Claim(t.Context(), orderID, id)
			if err != nil {
				t.Errorf("claim by %d: %v", id, err)
				return
			}
			if ok {
				won.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()
	by, err := r.ClaimedBy(t.Context(), orderID)
	if err != nil {
		t.Fatal(err)
	}
	return int(won.Load()), by
}

func TestW5B7_TwoRidersRaceForOneClaimAndExactlyOneWins(t *testing.T) {
	base := time.Now().UnixNano()
	for i := int64(0); i < 100; i++ {
		winners, by := race(t, base+i, []int64{1, 2})
		if winners != 1 {
			t.Fatalf("round %d: %d riders won the claim, want exactly 1", i, winners)
		}
		if by != 1 && by != 2 {
			t.Fatalf("round %d: ClaimedBy = %d, want the winning rider", i, by)
		}
	}
}

func TestW5B7_FiftyRidersRaceForOneClaimAndExactlyOneWins(t *testing.T) {
	ids := make([]int64, 50)
	for i := range ids {
		ids[i] = int64(i + 1)
	}
	winners, by := race(t, time.Now().UnixNano(), ids)
	if winners != 1 {
		t.Fatalf("%d riders won the claim, want exactly 1", winners)
	}
	if by < 1 || by > 50 {
		t.Fatalf("ClaimedBy = %d, want one of the contenders", by)
	}
}
