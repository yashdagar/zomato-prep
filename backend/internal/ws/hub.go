package ws

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
)

type Hub struct {
	mu    sync.Mutex
	conns map[int64]*conn
}

type conn struct {
	ws     *websocket.Conn
	send   chan []byte
	ctx    context.Context
	cancel context.CancelFunc
}

func NewHub() *Hub {
	return &Hub{
		conns: make(map[int64]*conn),
	}
}

func (h *Hub) Serve(w http.ResponseWriter, r *http.Request, userID int64) {
	wsConn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())

	c := &conn{
		ws:     wsConn,
		send:   make(chan []byte, 32),
		ctx:    ctx,
		cancel: cancel,
	}

	h.mu.Lock()
	old := h.conns[userID]
	h.conns[userID] = c
	h.mu.Unlock()

	if old != nil {
		old.cancel()
		_ = old.ws.Close(websocket.StatusGoingAway, "connection replaced")
	}

	readDone := make(chan struct{})
	writeDone := make(chan struct{})

	// The reader exits when the connection closes or its context is cancelled.
	go func() {
		defer close(readDone)
		readCtx := wsConn.CloseRead(ctx)
		<-readCtx.Done()
	}()

	// The writer exits when the send channel closes or its context is cancelled.
	go func() {
		defer close(writeDone)

		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-c.send:
				if !ok {
					return
				}

				writeCtx, writeCancel := context.WithTimeout(ctx, 5*time.Second)
				err := wsConn.Write(writeCtx, websocket.MessageText, msg)
				writeCancel()

				if err != nil {
					cancel()
					_ = wsConn.Close(websocket.StatusGoingAway, "write failed")
					return
				}
			}
		}
	}()

	<-readDone

	cancel()
	_ = wsConn.Close(websocket.StatusGoingAway, "connection closed")

	h.mu.Lock()
	if h.conns[userID] == c {
		delete(h.conns, userID)
	}
	close(c.send)
	h.mu.Unlock()

	<-writeDone
}

func (h *Hub) Connected(userID int64) bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	_, ok := h.conns[userID]
	return ok
}

func (h *Hub) Send(userID int64, msg []byte) bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	c, ok := h.conns[userID]
	if !ok {
		return false
	}

	select {
	case c.send <- msg:
		return true
	default:
		slog.Warn(
			"dropping WebSocket message because send buffer is full",
			"user_id", userID,
		)
		return false
	}
}
