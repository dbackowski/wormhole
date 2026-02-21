package server

import (
	"context"
	"fmt"
	"sync"

	"github.com/dbackowski/wormhole/common"
	"github.com/gorilla/websocket"
)

type Connection struct {
	Domain   string
	conn     *websocket.Conn
	requests *PendingRequests
	mu       sync.Mutex
}

type ConnectionManager struct {
	mu          sync.RWMutex
	connections map[string]*Connection
}

func NewConnectionManager() *ConnectionManager {
	return &ConnectionManager{
		connections: make(map[string]*Connection),
	}
}

func (cm *ConnectionManager) AddConnection(domain string, conn *websocket.Conn) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	_, exists := cm.connections[domain]
	if exists {
		return fmt.Errorf("domain %s is already taken", domain)
	}

	cm.connections[domain] = &Connection{
		Domain:   domain,
		conn:     conn,
		requests: NewPendingRequests(),
	}

	return nil
}

func (cm *ConnectionManager) GetConnection(domain string) (*Connection, error) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	connection, exists := cm.connections[domain]

	if !exists {
		return nil, fmt.Errorf("connection for domain %s not found", domain)
	}

	return connection, nil
}

func (cm *ConnectionManager) RemoveConnection(domain string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	delete(cm.connections, domain)
}

func (cm *ConnectionManager) CloseAll() {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	for _, connection := range cm.connections {
		connection.Close()
	}
	clear(cm.connections)
}

func (cm *ConnectionManager) Count() int {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return len(cm.connections)
}

func (c *Connection) RegisterRequest(ctx context.Context, uuid string) (chan *common.Message, context.CancelFunc) {
	return c.requests.Register(ctx, uuid)
}

func (c *Connection) SendMessage(msg *common.Message) error {
	c.mu.Lock()
	defer c.mu.Unlock()
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
