package server

import (
	"context"
	"sync"
	"time"

	"github.com/dbackowski/wormhole/common"
	"github.com/gorilla/websocket"
)

type Connection struct {
	conn     *websocket.Conn
	requests *PendingRequests
	mu       sync.Mutex
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

func (c *Connection) CleanupRequest(uuid string) {
	c.requests.Cleanup(uuid)
}

func (c *Connection) DeliverResponse(msg *common.Message) error {
	return c.requests.Deliver(msg)
}

func (c *Connection) Close() error {
	return c.conn.Close()
}
