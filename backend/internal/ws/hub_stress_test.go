package ws

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestW5B7_EveryConnectionReceivesEveryBroadcastInOrder(t *testing.T) {
	hub, base := serveHub(t)
	const users, messages = 50, 20
	conns := make([]*websocket.Conn, users)
	for i := 0; i < users; i++ {
		conns[i] = dial(t, base, int64(i))
	}
	waitFor(t, "all connections to register", 3*time.Second, func() bool { return hub.Connected(users - 1) })

	var senders sync.WaitGroup
	for i := 0; i < users; i++ {
		senders.Add(1)
		go func() {
			defer senders.Done()
			for m := 0; m < messages; m++ {
				if !hub.Send(int64(i), []byte(fmt.Sprintf("m%d", m))) {
					t.Errorf("send %d to user %d was dropped", m, i)
				}
			}
		}()
	}
	senders.Wait()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var readers sync.WaitGroup
	for i, c := range conns {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for m := 0; m < messages; m++ {
				_, data, err := c.Read(ctx)
				if err != nil {
					t.Errorf("user %d message %d: %v", i, m, err)
					return
				}
				if want := fmt.Sprintf("m%d", m); string(data) != want {
					t.Errorf("user %d got %q, want %q", i, data, want)
					return
				}
			}
		}()
	}
	readers.Wait()
}
