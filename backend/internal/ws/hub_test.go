package ws

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestW5B1_HubBroadcastsToEveryConnectedUser(t *testing.T) {
	hub := NewHub()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.ParseInt(r.URL.Query().Get("user"), 10, 64)
		hub.Serve(w, r, id)
	}))
	defer srv.Close()

	const users = 50
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	clients := make([]*websocket.Conn, users)
	for i := range clients {
		url := "ws" + strings.TrimPrefix(srv.URL, "http") + "?user=" + strconv.Itoa(i)
		c, resp, err := websocket.Dial(ctx, url, nil)
		if err != nil {
			t.Fatalf("dial %d: %v", i, err)
		}
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		defer c.CloseNow() //nolint:errcheck
		clients[i] = c
	}
	deadline := time.Now().Add(2 * time.Second)
	for i := 0; i < users; i++ {
		for !hub.Connected(int64(i)) {
			if time.Now().After(deadline) {
				t.Fatalf("user %d never registered", i)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}

	for i := 0; i < users; i++ {
		if !hub.Send(int64(i), []byte("hello")) {
			t.Fatalf("send to %d failed", i)
		}
	}

	var wg sync.WaitGroup
	errs := make(chan error, users)
	for i, c := range clients {
		wg.Add(1)
		go func(i int, c *websocket.Conn) {
			defer wg.Done()
			_, data, err := c.Read(ctx)
			if err != nil {
				errs <- err
				return
			}
			if string(data) != "hello" {
				errs <- &strconv.NumError{Func: "read", Num: string(data)}
			}
		}(i, c)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
}

func TestW5B1_SendToUnknownUserIsFalse(t *testing.T) {
	if NewHub().Send(999, []byte("x")) {
		t.Fatal("expected false for a user with no connection")
	}
}
