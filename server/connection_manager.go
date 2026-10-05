package server

import (
	"crypto/subtle"
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

// AddConnection claims domain for conn. A domain held by a connection with the
// same non-empty session is reclaimed: that connection is returned as stale for
// the caller to close. See common.SessionHeader.
func (cm *ConnectionManager) AddConnection(domain, session string, conn *websocket.Conn) (connection, stale *Connection, err error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	existing, exists := cm.connections[domain]
	if exists && !sameSession(existing.session, session) {
		return nil, nil, fmt.Errorf("domain %s is already taken", domain)
	}

	connection = newConnection(conn, session)
	cm.connections[domain] = connection

	return connection, existing, nil
}

func sameSession(held, offered string) bool {
	return offered != "" && subtle.ConstantTimeCompare([]byte(held), []byte(offered)) == 1
}

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

// RemoveConnection releases domain only if connection still holds it. A stale
// connection replaced by a reconnect must not remove its replacement when its
// read loop finally fails.
func (cm *ConnectionManager) RemoveConnection(domain string, connection *Connection) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cm.connections[domain] == connection {
		delete(cm.connections, domain)
	}
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
