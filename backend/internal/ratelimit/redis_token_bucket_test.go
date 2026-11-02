//go:build redis && w1o4

package ratelimit_test

import (
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/yashdagar/zomato-prep/backend/internal/ratelimit"
)

func uniqueKey(t *testing.T) string {
	t.Helper()
	return "test:" + t.Name() + ":" + time.Now().Format(time.RFC3339Nano)
}

func TestW1O4_BurstThenReject(t *testing.T) {
	rdb := redisClient(t)
	l := ratelimit.NewRedisTokenBucket(rdb, 3, 0)
	key := uniqueKey(t)
	for i, want := range []bool{true, true, true, false} {
		if got := l.Allow(key); got != want {
			t.Fatalf("request %d: Allow() = %v, want %v", i, got, want)
		}
	}
}

func TestW1O4_SharedAcrossInstances(t *testing.T) {
	rdb := redisClient(t)
	a := ratelimit.NewRedisTokenBucket(rdb, 4, 0)
	b := ratelimit.NewRedisTokenBucket(rdb, 4, 0)
	key := uniqueKey(t)
	for i := 0; i < 4; i++ {
		l := a
		if i%2 == 1 {
			l = b
		}
		if !l.Allow(key) {
			t.Fatalf("request %d must pass", i)
		}
	}
	if a.Allow(key) || b.Allow(key) {
		t.Fatal("the fifth request must fail on both instances")
	}
}

func TestW1O4_RefillsOverTime(t *testing.T) {
	rdb := redisClient(t)
	l := ratelimit.NewRedisTokenBucket(rdb, 1, 5)
	key := uniqueKey(t)
	if !l.Allow(key) {
		t.Fatal("first request must pass")
	}
	if l.Allow(key) {
		t.Fatal("immediate second request must fail")
	}
	time.Sleep(400 * time.Millisecond)
	if !l.Allow(key) {
		t.Fatal("after 400ms at 5 tokens per second the bucket must have refilled")
	}
}

func TestW1O4_SurvivesScriptFlush(t *testing.T) {
	rdb := redisClient(t)
	l := ratelimit.NewRedisTokenBucket(rdb, 2, 0)
	key := uniqueKey(t)
	if !l.Allow(key) {
		t.Fatal("first request must pass")
	}
	if err := rdb.ScriptFlush(t.Context()).Err(); err != nil {
		t.Fatal(err)
	}
	if !l.Allow(key) {
		t.Fatal("the limiter must reload its script after SCRIPT FLUSH")
	}
}

func TestW1O4_FailsOpenWhenRedisIsDown(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"})
	t.Cleanup(func() { _ = rdb.Close() })
	l := ratelimit.NewRedisTokenBucket(rdb, 1, 0)
	start := time.Now()
	for i := 0; i < 3; i++ {
		if !l.Allow("any") {
			t.Fatalf("request %d must pass while Redis is down", i)
		}
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("took %v, the Redis calls must time out quickly", time.Since(start))
	}
}
