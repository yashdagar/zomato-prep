package ws

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func bcWaitConnected(t *testing.T, hub *Hub, userID int64) {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	for !hub.Connected(userID) {
		select {
		case <-ctx.Done():
			t.Fatalf("user %d did not connect: %v", userID, ctx.Err())
		default:
			runtime.Gosched()
		}
	}
}

func TestBroadcastReachesFiftyConnections(t *testing.T) {
	const connectionCount = 50

	hub := NewHub()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID, err := strconv.ParseInt(r.URL.Query().Get("userID"), 10, 64)
		if err != nil {
			http.Error(w, "invalid user ID", http.StatusBadRequest)
			return
		}

		hub.Serve(w, r, userID)
	}))
	defer server.Close()

	type bcClient struct {
		userID int64
		conn   *websocket.Conn
		msg    []byte
	}

	clients := make([]bcClient, 0, connectionCount)

	for i := 0; i < connectionCount; i++ {
		userID := int64(i + 1)
		msg := []byte(fmt.Sprintf("broadcast-%d", userID))

		url := strings.Replace(server.URL, "http://", "ws://", 1)
		url += fmt.Sprintf("/?userID=%d", userID)

		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		conn, response, err := websocket.Dial(ctx, url, nil)
		cancel()

		if response != nil && response.Body != nil {
			response.Body.Close()
		}

		if err != nil {
			t.Fatalf("dial user %d: %v", userID, err)
		}

		conn.SetReadLimit(1024)

		clients = append(clients, bcClient{
			userID: userID,
			conn:   conn,
			msg:    msg,
		})

		t.Cleanup(func() {
			conn.CloseNow()
		})
	}

	for _, client := range clients {
		bcWaitConnected(t, hub, client.userID)
	}

	for _, client := range clients {
		if !hub.Send(client.userID, client.msg) {
			t.Fatalf("send failed for user %d", client.userID)
		}
	}

	for _, client := range clients {
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		_, received, err := client.conn.Read(ctx)
		cancel()

		if err != nil {
			t.Fatalf("read from user %d: %v", client.userID, err)
		}

		if string(received) != string(client.msg) {
			t.Errorf(
				"user %d received %q, want %q",
				client.userID,
				received,
				client.msg,
			)
		}
	}
}
