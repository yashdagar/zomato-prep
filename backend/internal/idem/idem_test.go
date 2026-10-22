package idem_test

import (
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/yashdagar/zomato-prep/backend/internal/idem"
)

func store(t *testing.T) *idem.Store {
	t.Helper()
	addr := os.Getenv("TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("set TEST_REDIS_ADDR=127.0.0.1:6379 to run")
	}
	rdb := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { _ = rdb.Close() })
	return idem.New(rdb)
}

func TestW3B4_ClaimIsExclusiveThenResolvesToOrderID(t *testing.T) {
	s := store(t)
	key := t.Name() + time.Now().Format(time.RFC3339Nano)

	_, existed, err := s.Claim(t.Context(), 1, key)
	if err != nil || existed {
		t.Fatalf("first claim: existed=%v err=%v, want a fresh claim", existed, err)
	}
	_, existed, err = s.Claim(t.Context(), 1, key)
	if err == nil && !existed {
		t.Fatal("second claim with the same key must report it exists or is in flight")
	}
	if err := s.Done(t.Context(), 1, key, "order-7"); err != nil {
		t.Fatal(err)
	}
	id, existed, err := s.Claim(t.Context(), 1, key)
	if err != nil || !existed || id != "order-7" {
		t.Fatalf("after Done: id=%q existed=%v err=%v, want order-7", id, existed, err)
	}
}

func TestW3B4_ReleaseLetsTheKeyBeClaimedAgain(t *testing.T) {
	s := store(t)
	key := t.Name() + time.Now().Format(time.RFC3339Nano)
	if _, _, err := s.Claim(t.Context(), 2, key); err != nil {
		t.Fatal(err)
	}
	if err := s.Release(t.Context(), 2, key); err != nil {
		t.Fatal(err)
	}
	if _, existed, err := s.Claim(t.Context(), 2, key); err != nil || existed {
		t.Fatalf("after Release: existed=%v err=%v, want a fresh claim", existed, err)
	}
}

func TestW3B4_KeysAreScopedPerUser(t *testing.T) {
	s := store(t)
	key := t.Name() + time.Now().Format(time.RFC3339Nano)
	if _, _, err := s.Claim(t.Context(), 1, key); err != nil {
		t.Fatal(err)
	}
	if _, existed, err := s.Claim(t.Context(), 2, key); err != nil || existed {
		t.Fatalf("another user with the same key: existed=%v err=%v, want a fresh claim", existed, err)
	}
}
