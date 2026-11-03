package server

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/dbackowski/wormhole/common"
	"github.com/gorilla/websocket"
)

type Connection struct {
	conn      *websocket.Conn
	session   string
	requests  *PendingRequests
	streams   *common.Streams
	mu        sync.Mutex
	ready     bool // guarded by ConnectionManager.mu
	done      chan struct{}
	closeOnce sync.Once

	// Written by registerClient before the connection is activated, so only
	// read for ready connections, under ConnectionManager.mu.
	owner tunnelOwner

	served atomic.Int64 // requests routed to this tunnel
}

// tunnelOwner is who opened a tunnel, shown on the admin page.
type tunnelOwner struct {
	email       string // empty with login disabled
	clientIP    string
	connectedAt time.Time
}

func newConnection(conn *websocket.Conn, session string) *Connection {
	return &Connection{
		conn:     conn,
		session:  session,
		requests: NewPendingRequests(),
		streams:  common.NewStreams(),
		done:     make(chan struct{}),
	}
}

func (c *Connection) Done() <-chan struct{} {
	return c.done
}

func (c *Connection) SendMessage(msg *common.Message) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return common.WriteJSON(c.conn, msg)
}

func (c *Connection) Close() error {
	c.closeOnce.Do(func() {
		close(c.done)
		c.streams.AbortAll()
	})
	return c.conn.Close()
}
