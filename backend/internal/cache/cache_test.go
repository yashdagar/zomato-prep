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
