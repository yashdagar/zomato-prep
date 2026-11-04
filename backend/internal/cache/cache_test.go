package cache_test

import (
	"context"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/yashdagar/zomato-prep/backend/internal/cache"
)

func newCache(t *testing.T) *cache.Cache {
	t.Helper()
	addr := os.Getenv("TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("set TEST_REDIS_ADDR=127.0.0.1:6379 to run")
	}
	rdb := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { _ = rdb.Close() })
	return cache.New(rdb, 10*time.Second, time.Second)
}

type menu struct {
	ID    int64    `json:"id"`
	Items []string `json:"items"`
}

func TestW6B1_ConcurrentMissesLoadOnce(t *testing.T) {
	c := newCache(t)
	key := "test:menu:" + time.Now().Format(time.RFC3339Nano)
	var loads atomic.Int32
	load := func(context.Context) (menu, error) {
		loads.Add(1)
		time.Sleep(50 * time.Millisecond)
		return menu{ID: 1, Items: []string{"pizza"}}, nil
	}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var m menu
			if err := cache.GetOrLoad(t.Context(), c, key, &m, load); err != nil || m.ID != 1 {
				t.Errorf("GetOrLoad: %+v %v", m, err)
			}
		}()
	}
	wg.Wait()
	if n := loads.Load(); n != 1 {
		t.Fatalf("loader ran %d times for 20 concurrent misses, want 1 (singleflight)", n)
	}
	var m menu
	if err := cache.GetOrLoad(t.Context(), c, key, &m, load); err != nil || loads.Load() != 1 {
		t.Fatalf("second read should be a cache hit, loads=%d err=%v", loads.Load(), err)
	}
}

func TestW6B1_InvalidateForcesReload(t *testing.T) {
	c := newCache(t)
	key := "test:menu:" + time.Now().Format(time.RFC3339Nano)
	var loads atomic.Int32
	load := func(context.Context) (menu, error) { loads.Add(1); return menu{ID: int64(loads.Load())}, nil }
	var m menu
	_ = cache.GetOrLoad(t.Context(), c, key, &m, load)
	if err := c.Invalidate(t.Context(), key); err != nil {
		t.Fatal(err)
	}
	_ = cache.GetOrLoad(t.Context(), c, key, &m, load)
	if loads.Load() != 2 || m.ID != 2 {
		t.Fatalf("after Invalidate: loads=%d id=%d, want a reload", loads.Load(), m.ID)
	}
}

func TestW6B1_TTLIsJitteredWithinBounds(t *testing.T) {
	addr := os.Getenv("TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("set TEST_REDIS_ADDR=127.0.0.1:6379 to run")
	}
	rdb := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { _ = rdb.Close() })
	c := cache.New(rdb, 100*time.Second, 20*time.Second)
	prefix := "test:jitter:" + time.Now().Format(time.RFC3339Nano)
	seen := map[time.Duration]bool{}
	for i := 0; i < 40; i++ {
		key := prefix + ":" + string(rune('a'+i%26)) + string(rune('a'+i/26))
		var m menu
		if err := cache.GetOrLoad(t.Context(), c, key, &m, func(context.Context) (menu, error) { return menu{ID: 1}, nil }); err != nil {
			t.Fatal(err)
		}
		ttl, err := rdb.PTTL(t.Context(), key).Result()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { rdb.Del(context.Background(), key) })
		if ttl < 99*time.Second || ttl > 121*time.Second {
			t.Fatalf("ttl %v outside 100s plus up to 20s jitter", ttl)
		}
		seen[ttl.Round(time.Second)] = true
	}
	if len(seen) < 3 {
		t.Fatalf("40 keys got only %d distinct TTLs, want jitter", len(seen))
	}
}

func TestW6B1_LoaderErrorIsNotCached(t *testing.T) {
	c := newCache(t)
	key := "test:menu:" + time.Now().Format(time.RFC3339Nano)
	var loads atomic.Int32
	load := func(context.Context) (menu, error) {
		if loads.Add(1) == 1 {
			return menu{}, context.DeadlineExceeded
		}
		return menu{ID: 7}, nil
	}
	var m menu
	if err := cache.GetOrLoad(t.Context(), c, key, &m, load); err == nil {
		t.Fatal("first load failed, GetOrLoad should return the error")
	}
	if err := cache.GetOrLoad(t.Context(), c, key, &m, load); err != nil || m.ID != 7 {
		t.Fatalf("after a failed load the next read should load again: %+v %v", m, err)
	}
}

func TestW6B1_UnreachableRedisFallsBackToTheLoader(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", DialTimeout: 100 * time.Millisecond, ReadTimeout: 100 * time.Millisecond, WriteTimeout: 100 * time.Millisecond, MaxRetries: -1})
	t.Cleanup(func() { _ = rdb.Close() })
	c := cache.New(rdb, 10*time.Second, time.Second)
	var m menu
	err := cache.GetOrLoad(t.Context(), c, "test:menu:down", &m, func(context.Context) (menu, error) { return menu{ID: 9}, nil })
	if err != nil || m.ID != 9 {
		t.Fatalf("with Redis down GetOrLoad = %+v, %v, want the loaded value and no error", m, err)
	}
}

func TestW6B1_ZeroJitterIsAllowed(t *testing.T) {
	addr := os.Getenv("TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("set TEST_REDIS_ADDR=127.0.0.1:6379 to run")
	}
	rdb := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { _ = rdb.Close() })
	c := cache.New(rdb, 5*time.Second, 0)
	var m menu
	key := "test:menu:" + time.Now().Format(time.RFC3339Nano)
	t.Cleanup(func() { rdb.Del(context.Background(), key) })
	if err := cache.GetOrLoad(t.Context(), c, key, &m, func(context.Context) (menu, error) { return menu{ID: 3}, nil }); err != nil || m.ID != 3 {
		t.Fatalf("GetOrLoad with zero jitter: %+v %v", m, err)
	}
}

func TestW6B1_GetOrLoadResultReportsHitOrMiss(t *testing.T) {
	c := newCache(t)
	key := "test:menu:" + time.Now().Format(time.RFC3339Nano)
	load := func(context.Context) (menu, error) { return menu{ID: 5}, nil }
	var m menu
	hit, err := cache.GetOrLoadResult(t.Context(), c, key, &m, load)
	if err != nil || hit {
		t.Fatalf("first read: hit=%v err=%v, want a miss", hit, err)
	}
	hit, err = cache.GetOrLoadResult(t.Context(), c, key, &m, load)
	if err != nil || !hit {
		t.Fatalf("second read: hit=%v err=%v, want a hit", hit, err)
	}
	_ = c.Invalidate(t.Context(), key)
}
