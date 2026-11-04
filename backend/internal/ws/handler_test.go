package ws

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func verifyOnly(token string) (int64, error) {
	if token == "good" {
		return 77, nil
	}
	return 0, errors.New("bad token")
}

func TestW5B1_HandlerRejectsAMissingOrInvalidToken(t *testing.T) {
	srv := httptest.NewServer(Handler(NewHub(), verifyOnly))
	defer srv.Close()
	for _, path := range []string{"/", "/?token=", "/?token=bad"} {
		res, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		_ = res.Body.Close()
		if res.StatusCode != http.StatusUnauthorized {
			t.Fatalf("GET %s = %d, want 401", path, res.StatusCode)
		}
	}
}

func TestW5B1_HandlerRegistersTheUserFromTheToken(t *testing.T) {
	hub := NewHub()
	srv := httptest.NewServer(Handler(hub, verifyOnly))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, resp, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/?token=good", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	defer c.CloseNow() //nolint:errcheck
	waitFor(t, "user 77 to register", 2*time.Second, func() bool { return hub.Connected(77) })
}
