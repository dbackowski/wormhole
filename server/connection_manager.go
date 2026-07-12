package server

import (
	"fmt"
	"sync"

	"github.com/gorilla/websocket"
)

type ConnectionManager struct {
	mu          sync.RWMutex
	connections map[string]*Connection
}

func NewConnectionManager() *ConnectionManager {
	return &ConnectionManager{
		connections: make(map[string]*Connection),
	}
}

func (cm *ConnectionManager) AddConnection(domain string, conn *websocket.Conn) (*Connection, error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	_, exists := cm.connections[domain]
	if exists {
		return nil, fmt.Errorf("domain %s is already taken", domain)
	}

	connection := &Connection{
		conn:     conn,
		requests: NewPendingRequests(),
	}
	cm.connections[domain] = connection

	return connection, nil
}

// ActivateConnection marks a reserved connection as ready to serve HTTP
// requests. Until it is called, GetConnection treats the domain as absent, so
// the registration confirmation can be delivered before any forwarded request.
func (cm *ConnectionManager) ActivateConnection(domain string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if connection, exists := cm.connections[domain]; exists {
		connection.ready = true
	}
}

func (cm *ConnectionManager) GetConnection(domain string) (*Connection, error) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	connection, exists := cm.connections[domain]

	if !exists || !connection.ready {
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
