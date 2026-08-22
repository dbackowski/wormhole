package server

import (
	"context"
	"sync"
	"time"

	"github.com/dbackowski/wormhole/common"
	"github.com/gorilla/websocket"
)

type Connection struct {
	conn      *websocket.Conn
	requests  *PendingRequests
	mu        sync.Mutex
	ready     bool // guarded by ConnectionManager.mu
	done      chan struct{}
	closeOnce sync.Once
}

func newConnection(conn *websocket.Conn) *Connection {
	return &Connection{
		conn:     conn,
		requests: NewPendingRequests(),
		done:     make(chan struct{}),
	}
}

func (c *Connection) Done() <-chan struct{} {
	return c.done
}

func (c *Connection) RegisterRequest(ctx context.Context, uuid string) (chan *common.Message, context.CancelFunc) {
	return c.requests.Register(ctx, uuid)
}

func (c *Connection) SendMessage(msg *common.Message) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.conn.SetWriteDeadline(time.Now().Add(common.WriteWait)); err != nil {
		return err
	}
	return c.conn.WriteJSON(msg)
}

func (c *Connection) DeliverResponse(msg *common.Message) error {
	return c.requests.Deliver(msg)
}

func (c *Connection) Close() error {
	c.closeOnce.Do(func() { close(c.done) })
	return c.conn.Close()
}
