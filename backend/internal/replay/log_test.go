//go:build w5o1

package replay_test

import (
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/yashdagar/zomato-prep/backend/internal/replay"
)

func newLog(t *testing.T, maxLen int64, ttl time.Duration) (*replay.Log, *redis.Client, int64) {
	t.Helper()
	addr := os.Getenv("TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("set TEST_REDIS_ADDR=127.0.0.1:6379 to run")
	}
	rdb := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { _ = rdb.Close() })
	user := time.Now().UnixNano() % 1_000_000_000
	return replay.NewLog(rdb, maxLen, ttl), rdb, user
}

func TestW5O1_AppendReturnsIncreasingIDsStartingAtOne(t *testing.T) {
	l, _, user := newLog(t, 100, time.Minute)
	for want := int64(1); want <= 3; want++ {
		got, err := l.Append(t.Context(), user, []byte("x"))
		if err != nil || got != want {
			t.Fatalf("Append = %d, %v, want %d", got, err, want)
		}
	}
}

func TestW5O1_SinceReturnsOnlyNewerEntriesInOrder(t *testing.T) {
	l, _, user := newLog(t, 100, time.Minute)
	for _, p := range []string{"a", "b", "c", "d"} {
		if _, err := l.Append(t.Context(), user, []byte(p)); err != nil {
			t.Fatal(err)
		}
	}
	got, err := l.Since(t.Context(), user, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != 3 || string(got[0].Payload) != "c" || got[1].ID != 4 || string(got[1].Payload) != "d" {
		t.Fatalf("Since(2) = %+v, want entries 3 and 4", got)
	}
}

func TestW5O1_SinceZeroReturnsEverythingKept(t *testing.T) {
	l, _, user := newLog(t, 100, time.Minute)
	for _, p := range []string{"a", "b"} {
		if _, err := l.Append(t.Context(), user, []byte(p)); err != nil {
			t.Fatal(err)
		}
	}
	got, err := l.Since(t.Context(), user, 0)
	if err != nil || len(got) != 2 {
		t.Fatalf("Since(0) = %+v, %v, want 2 entries", got, err)
	}
}

func TestW5O1_SinceForAnUnknownUserIsEmpty(t *testing.T) {
	l, _, user := newLog(t, 100, time.Minute)
	got, err := l.Since(t.Context(), user, 0)
	if err != nil || len(got) != 0 {
		t.Fatalf("Since = %+v, %v, want nothing", got, err)
	}
}

func TestW5O1_OnlyTheNewestEntriesAreKept(t *testing.T) {
	l, _, user := newLog(t, 3, time.Minute)
	for i := 1; i <= 5; i++ {
		if _, err := l.Append(t.Context(), user, []byte(fmt.Sprint(i))); err != nil {
			t.Fatal(err)
		}
	}
	got, err := l.Since(t.Context(), user, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].ID != 3 || got[2].ID != 5 {
		t.Fatalf("Since(0) = %+v, want entries 3 to 5", got)
	}
}

func TestW5O1_EveryKeyExpires(t *testing.T) {
	l, rdb, user := newLog(t, 100, 90*time.Second)
	if _, err := l.Append(t.Context(), user, []byte("x")); err != nil {
		t.Fatal(err)
	}
	keys, err := rdb.Keys(t.Context(), fmt.Sprintf("*%d*", user)).Result()
	if err != nil || len(keys) == 0 {
		t.Fatalf("keys for user %d = %v, %v", user, keys, err)
	}
	for _, k := range keys {
		ttl, err := rdb.TTL(t.Context(), k).Result()
		if err != nil || ttl <= 0 || ttl > 90*time.Second {
			t.Fatalf("TTL of %s = %s, %v, want between 0 and 90s", k, ttl, err)
		}
	}
}

func TestW5O1_ConcurrentAppendsGetUniqueIDs(t *testing.T) {
	l, _, user := newLog(t, 1000, time.Minute)
	const n = 50
	ids := make(chan int64, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id, err := l.Append(t.Context(), user, []byte("x"))
			if err != nil {
				t.Error(err)
				return
			}
			ids <- id
		}()
	}
	wg.Wait()
	close(ids)
	seen := map[int64]bool{}
	for id := range ids {
		if seen[id] {
			t.Fatalf("id %d was handed out twice", id)
		}
		seen[id] = true
	}
	if len(seen) != n {
		t.Fatalf("got %d distinct ids, want %d", len(seen), n)
	}
}

func TestW5O1_UsersHaveSeparateLogs(t *testing.T) {
	l, _, user := newLog(t, 100, time.Minute)
	if _, err := l.Append(t.Context(), user, []byte("mine")); err != nil {
		t.Fatal(err)
	}
	got, err := l.Since(t.Context(), user+1, 0)
	if err != nil || len(got) != 0 {
		t.Fatalf("another user sees %+v, %v", got, err)
	}
	id, err := l.Append(t.Context(), user+1, []byte("theirs"))
	if err != nil || id != 1 {
		t.Fatalf("first id for another user = %d, %v, want 1", id, err)
	}
}
