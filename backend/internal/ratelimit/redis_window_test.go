//go:build redis

package ratelimit_test

import (
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/yashdagar/zomato-prep/backend/internal/ratelimit"
)

func redisClient(t *testing.T) *redis.Client {
	t.Helper()
	addr := os.Getenv("TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("set TEST_REDIS_ADDR=127.0.0.1:6379 to run")
	}
	rdb := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { _ = rdb.Close() })
	return rdb
}

func TestW1B7_RedisWindowCountsAcrossInstances(t *testing.T) {
	rdb := redisClient(t)
	key := "test:" + t.Name() + ":" + time.Now().Format(time.RFC3339Nano)
	a := ratelimit.NewRedisFixedWindow(rdb, 3, time.Minute)
	b := ratelimit.NewRedisFixedWindow(rdb, 3, time.Minute)

	got := []bool{a.Allow(key), b.Allow(key), a.Allow(key), b.Allow(key)}
	want := []bool{true, true, true, false}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("call %d = %v, want %v (two instances must share one counter)", i+1, got[i], want[i])
		}
	}
}

func TestW1B7_FailsOpenWhenRedisIsDown(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", DialTimeout: 200 * time.Millisecond})
	defer func() { _ = rdb.Close() }()
	l := ratelimit.NewRedisFixedWindow(rdb, 3, time.Minute)
	start := time.Now()
	for i := 0; i < 3; i++ {
		if !l.Allow("anyone") {
			t.Fatalf("call %d rejected; a dead Redis must fall back to the in-memory window, not reject", i+1)
		}
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("three calls took %s; after the first failure the fallback must not wait on Redis again", time.Since(start))
	}
}
