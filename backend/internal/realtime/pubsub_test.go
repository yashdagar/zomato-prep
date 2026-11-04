package realtime

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/redis/go-redis/v9"

	"github.com/yashdagar/zomato-prep/backend/internal/ws"
)

func redisFor(t *testing.T) *redis.Client {
	t.Helper()
	addr := os.Getenv("TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("set TEST_REDIS_ADDR=127.0.0.1:6379 to run")
	}
	rdb := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { _ = rdb.Close() })
	return rdb
}

func serveHub(t *testing.T) (*ws.Hub, string) {
	t.Helper()
	hub := ws.NewHub()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.ParseInt(r.URL.Query().Get("user"), 10, 64)
		hub.Serve(w, r, id)
	}))
	t.Cleanup(srv.Close)
	return hub, "ws" + strings.TrimPrefix(srv.URL, "http")
}

func dial(t *testing.T, base string, user int64) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, resp, err := websocket.Dial(ctx, base+"?user="+strconv.FormatInt(user, 10), nil)
	if err != nil {
		t.Fatalf("dial user %d: %v", user, err)
	}
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	t.Cleanup(func() { _ = c.CloseNow() })
	return c
}

func TestW5B2_PublishedMessageReachesOnlyThatUsersConnection(t *testing.T) {
	rdb := redisFor(t)
	hub, base := serveHub(t)
	user := time.Now().UnixNano() % 1_000_000_000
	target := dial(t, base, user)
	other := dial(t, base, user+1)
	deadline := time.Now().Add(2 * time.Second)
	for !hub.Connected(user) || !hub.Connected(user+1) {
		if time.Now().After(deadline) {
			t.Fatal("connections never registered")
		}
		time.Sleep(5 * time.Millisecond)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Subscribe(ctx, rdb, hub) }()
	t.Cleanup(func() { cancel(); <-done })

	readCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	got := make(chan string, 1)
	go func() {
		_, data, err := target.Read(readCtx)
		if err == nil {
			got <- string(data)
		}
	}()
	payload := `{"order_id":42,"to":"ACCEPTED"}`
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case msg := <-got:
			if msg != payload {
				t.Fatalf("got %q, want %q", msg, payload)
			}
			quiet, quietCancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
			defer quietCancel()
			if _, data, err := other.Read(quiet); err == nil {
				t.Fatalf("user %d received %q meant for user %d", user+1, data, user)
			}
			return
		case <-tick.C:
			rdb.Publish(context.Background(), "user:"+strconv.FormatInt(user, 10), payload)
		case <-readCtx.Done():
			t.Fatal("message was never forwarded to the connection")
		}
	}
}

func TestW5B2_MalformedChannelNamesAreIgnored(t *testing.T) {
	rdb := redisFor(t)
	hub := ws.NewHub()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Subscribe(ctx, rdb, hub) }()
	for i := 0; i < 10; i++ {
		rdb.Publish(context.Background(), "user:not-a-number", "x")
		rdb.Publish(context.Background(), "user:", "x")
		time.Sleep(20 * time.Millisecond)
	}
	select {
	case err := <-done:
		t.Fatalf("Subscribe stopped on a bad channel name: %v", err)
	default:
	}
	cancel()
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("Subscribe returned %v after cancel", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Subscribe did not return after the context was cancelled")
	}
}

func TestW5B2_PublishUsesTheUserChannel(t *testing.T) {
	rdb := redisFor(t)
	user := time.Now().UnixNano() % 1_000_000_000
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sub := rdb.Subscribe(ctx, "user:"+strconv.FormatInt(user, 10))
	defer sub.Close()
	if _, err := sub.Receive(ctx); err != nil {
		t.Fatal(err)
	}
	if err := Publish(ctx, rdb, user, []byte(`{"to":"READY"}`)); err != nil {
		t.Fatal(err)
	}
	msg, err := sub.ReceiveMessage(ctx)
	if err != nil || msg.Payload != `{"to":"READY"}` {
		t.Fatalf("got %v, %v", msg, err)
	}
}
