package server

import (
	"fmt"
	"sync"

	"github.com/gorilla/websocket"
)

type Connection struct {
	Domain   string
	Conn     *websocket.Conn
	Requests *PendingRequests
}

type ConnectionManager struct {
	mu      sync.RWMutex
	clients map[string]*Connection
}

func NewConnectionManager() *ConnectionManager {
	return &ConnectionManager{
		clients: make(map[string]*Connection),
	}
}

func (cm *ConnectionManager) AddConnection(domain string, conn *websocket.Conn) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	_, exists := cm.clients[domain]
	if exists {
		return fmt.Errorf("domain %s is already taken", domain)
	}

	cm.clients[domain] = &Connection{
		Domain:   domain,
		Conn:     conn,
		Requests: NewPendingRequests(),
	}

	return nil
}

func (cm *ConnectionManager) GetConnection(domain string) (*Connection, bool) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	connection, exists := cm.clients[domain]

	return connection, exists
}

func (cm *ConnectionManager) RemoveConnection(domain string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	delete(cm.clients, domain)
}
