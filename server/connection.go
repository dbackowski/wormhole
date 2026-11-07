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
	streams   *common.Streams // WebSockets
	bodies    *common.Streams // HTTP request and response bodies
	mu        sync.Mutex
	ready     bool // guarded by ConnectionManager.mu
	done      chan struct{}
	closeOnce sync.Once

	// Written by registerClient before the connection is activated, so only
	// read for ready connections, under ConnectionManager.mu.
	owner tunnelOwner

	// Set by AddConnection, possibly to the stats of the connection this one
	// reconnects. Shown on the admin page.
	stats *tunnelStats
}

// tunnelOwner is who opened a tunnel, shown on the admin page.
type tunnelOwner struct {
	email    string // empty with login disabled
	clientIP string
}

// tunnelStats outlive a single connection: a reconnect from the same client
// process continues them, so a dropped connection does not look like a new
// tunnel.
type tunnelStats struct {
	firstConnected time.Time
	served         atomic.Int64 // requests routed to the tunnel
	reconnects     atomic.Int32
	lastReconnect  atomic.Int64 // unix nanoseconds, 0 if never
}

func newTunnelStats() *tunnelStats {
	return &tunnelStats{firstConnected: time.Now()}
}

func (st *tunnelStats) reconnected() *tunnelStats {
	st.reconnects.Add(1)
	st.lastReconnect.Store(time.Now().UnixNano())
	return st
}

func newConnection(conn *websocket.Conn, session string) *Connection {
	return &Connection{
		conn:     conn,
		session:  session,
		requests: NewPendingRequests(),
		streams:  common.NewStreams(common.MaxStreamsPerTunnel),
		bodies:   common.NewStreams(common.MaxConcurrentRequests),
		done:     make(chan struct{}),
		stats:    newTunnelStats(),
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
		c.bodies.AbortAll()
	})
	return c.conn.Close()
}
