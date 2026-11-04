package live_test

import (
	"context"
	"encoding/json"
	"fmt"
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

func TestW5B5_LocationUpdatesAreLimitedToOneEveryTwoSecondsPerRider(t *testing.T) {
	r, _ := riders(t)
	id := time.Now().UnixNano()
	first, err := r.AllowLocation(t.Context(), id)
	if err != nil || !first {
		t.Fatalf("first update: allowed=%v err=%v", first, err)
	}
	second, err := r.AllowLocation(t.Context(), id)
	if err != nil || second {
		t.Fatalf("second update straight after: allowed=%v err=%v, want false", second, err)
	}
	other, err := r.AllowLocation(t.Context(), id+1)
	if err != nil || !other {
		t.Fatalf("another rider: allowed=%v err=%v, want true", other, err)
	}
	time.Sleep(2100 * time.Millisecond)
	again, err := r.AllowLocation(t.Context(), id)
	if err != nil || !again {
		t.Fatalf("after two seconds: allowed=%v err=%v, want true", again, err)
	}
}

func TestW5B5_PublishLocationSendsLatLngOnTheOrderChannel(t *testing.T) {
	r, rdb := riders(t)
	orderID := time.Now().UnixNano()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sub := rdb.Subscribe(ctx, fmt.Sprintf("order:%d:loc", orderID))
	defer sub.Close()
	if _, err := sub.Receive(ctx); err != nil {
		t.Fatal(err)
	}
	if err := r.PublishLocation(ctx, orderID, 28.4601, 77.0302); err != nil {
		t.Fatal(err)
	}
	msg, err := sub.ReceiveMessage(ctx)
	if err != nil {
		t.Fatalf("no message on order:%d:loc: %v", orderID, err)
	}
	var got struct{ Lat, Lng float64 }
	if err := json.Unmarshal([]byte(msg.Payload), &got); err != nil {
		t.Fatalf("payload %q is not JSON: %v", msg.Payload, err)
	}
	if got.Lat != 28.4601 || got.Lng != 77.0302 {
		t.Fatalf("payload %q, want lat 28.4601 and lng 77.0302", msg.Payload)
	}
}
