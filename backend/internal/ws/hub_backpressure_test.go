package ws

import (
	"context"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func serveHub(t *testing.T) (*Hub, string) {
	t.Helper()
	hub := NewHub()
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

func waitFor(t *testing.T, what string, within time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(within)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("%s did not happen within %s", what, within)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestW5B1_SlowClientNeverBlocksSend(t *testing.T) {
	hub, base := serveHub(t)
	dial(t, base, 1)
	waitFor(t, "user 1 to register", 2*time.Second, func() bool { return hub.Connected(1) })

	msg := make([]byte, 256<<10)
	start := time.Now()
	dropped := 0
	for i := 0; i < 400; i++ {
		if !hub.Send(1, msg) {
			dropped++
		}
	}
	if took := time.Since(start); took > 2*time.Second {
		t.Fatalf("400 sends to a client that never reads took %s, Send must not block", took)
	}
	if dropped == 0 {
		t.Fatal("no message was dropped, the send buffer must be bounded")
	}
}

func TestW5B1_ConnectionIsRemovedWhenTheClientCloses(t *testing.T) {
	hub, base := serveHub(t)
	c := dial(t, base, 7)
	waitFor(t, "user 7 to register", 2*time.Second, func() bool { return hub.Connected(7) })
	_ = c.Close(websocket.StatusNormalClosure, "bye")
	waitFor(t, "user 7 to be removed", 3*time.Second, func() bool { return !hub.Connected(7) })
	if hub.Send(7, []byte("x")) {
		t.Fatal("Send to a removed connection returned true")
	}
}

func TestW5B1_SendersAndDisconnectsRaceSafely(t *testing.T) {
	hub, base := serveHub(t)
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ctx.Err() == nil {
				for id := int64(0); id < 10; id++ {
					hub.Send(id, []byte("ping"))
				}
			}
		}()
	}
	for round := 0; round < 5; round++ {
		conns := make([]*websocket.Conn, 10)
		for i := range conns {
			conns[i] = dial(t, base, int64(i))
		}
		time.Sleep(20 * time.Millisecond)
		for _, c := range conns {
			_ = c.CloseNow()
		}
	}
	cancel()
	wg.Wait()
}

func TestW5B1_GoroutinesExitAfterConnectionsClose(t *testing.T) {
	hub, base := serveHub(t)
	time.Sleep(100 * time.Millisecond)
	before := runtime.NumGoroutine()
	conns := make([]*websocket.Conn, 20)
	for i := range conns {
		conns[i] = dial(t, base, int64(100+i))
	}
	waitFor(t, "all 20 to register", 3*time.Second, func() bool { return hub.Connected(119) })
	for _, c := range conns {
		_ = c.CloseNow()
	}
	waitFor(t, "goroutines to exit", 5*time.Second, func() bool { return runtime.NumGoroutine() <= before+4 })
}

func TestW5B1_NewConnectionForTheSameUserReplacesTheOldOne(t *testing.T) {
	hub, base := serveHub(t)
	first := dial(t, base, 5)
	waitFor(t, "first connection to register", 2*time.Second, func() bool { return hub.Connected(5) })
	second := dial(t, base, 5)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, _, err := first.Read(ctx); err == nil {
		t.Fatal("the first connection was still open and readable after the second one replaced it")
	}
	time.Sleep(100 * time.Millisecond)
	if !hub.Connected(5) {
		t.Fatal("closing the replaced connection removed the new one from the hub")
	}
	if !hub.Send(5, []byte("to-second")) {
		t.Fatal("Send to the new connection failed")
	}
	_, data, err := second.Read(ctx)
	if err != nil || string(data) != "to-second" {
		t.Fatalf("second connection read %q, %v", data, err)
	}
}

func TestW5B1_ReplacedConnectionReleasesItsGoroutines(t *testing.T) {
	hub, base := serveHub(t)
	time.Sleep(100 * time.Millisecond)
	before := runtime.NumGoroutine()
	for round := 0; round < 10; round++ {
		c := dial(t, base, 9)
		waitFor(t, "user 9 to register", 2*time.Second, func() bool { return hub.Connected(9) })
		_ = c
	}
	last := dial(t, base, 9)
	_ = last.CloseNow()
	waitFor(t, "user 9 to be removed", 3*time.Second, func() bool { return !hub.Connected(9) })
	waitFor(t, "goroutines to exit", 5*time.Second, func() bool { return runtime.NumGoroutine() <= before+4 })
}
