package server

import (
	"cmp"
	"crypto/subtle"
	"fmt"
	"slices"
	"sync"
	"time"

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

// TunnelInfo is a copy of one tunnel's state for the admin page.
type TunnelInfo struct {
	Domain      string
	Email       string
	ClientIP    string
	ConnectedAt time.Time
	Requests    int64
	InFlight    int
	WebSockets  int
}

// Snapshot returns the live tunnels sorted by domain. Copies, so the caller
// renders them without holding the lock.
func (cm *ConnectionManager) Snapshot() []TunnelInfo {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	tunnels := make([]TunnelInfo, 0, len(cm.connections))
	for domain, c := range cm.connections {
		if !c.ready {
			continue
		}
		tunnels = append(tunnels, TunnelInfo{
			Domain:      domain,
			Email:       c.owner.email,
			ClientIP:    c.owner.clientIP,
			ConnectedAt: c.owner.connectedAt,
			Requests:    c.served.Load(),
			InFlight:    c.requests.Len(),
			WebSockets:  c.streams.Count(),
		})
	}
	slices.SortFunc(tunnels, func(a, b TunnelInfo) int { return cmp.Compare(a.Domain, b.Domain) })
	return tunnels
}

// CloseByEmail closes every tunnel opened by email and returns how many. Each
// read loop then fails and releases its domain.
func (cm *ConnectionManager) CloseByEmail(email string) int {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	closed := 0
	for _, c := range cm.connections {
		if c.ready && c.owner.email == email {
			c.Close()
			closed++
		}
	}
	return closed
}

// CloseUnlessOwnedBy closes the tunnel on domain unless email opened it, and
// reports whether it closed one.
func (cm *ConnectionManager) CloseUnlessOwnedBy(domain, email string) bool {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	c, ok := cm.connections[domain]
	if !ok || !c.ready || c.owner.email == email {
		return false
	}
	c.Close()
	return true
}
