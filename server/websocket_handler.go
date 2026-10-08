package server

import (
	"fmt"
	"net/http"

	"github.com/dbackowski/wormhole/common"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// isClientRegistration reports whether an upgrade is a tunnel client claiming a
// domain. Without the marker header any WebSocket a browser opened against a
// tunneled app would register instead, claiming the domain whenever the real
// tunnel happens to be down.
func isClientRegistration(r *http.Request) bool {
	return websocket.IsWebSocketUpgrade(r) && r.Header.Get(common.ClientHeader) != ""
}

func (s *Server) ServeWebSocket(w http.ResponseWriter, r *http.Request) {
	if !s.authenticateRequest(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	conn, domain, err := s.upgradeAndExtractDomain(w, r)

	if err != nil {
		s.Logger.Error("WebSocket upgrade failed", "error", err, "remote_addr", r.RemoteAddr)
		return
	}

	connection, err := s.registerClient(conn, domain, r.Header.Get(common.SessionHeader))

	if err != nil {
		s.Logger.Error("Client registration failed", "error", err, "domain", domain)
		return
	}

	s.requestLogger.LogClientConnected(domain, r.RemoteAddr)
	s.handleWebSocketConnection(domain, r.RemoteAddr, connection)
}

func (s *Server) upgradeAndExtractDomain(w http.ResponseWriter, r *http.Request) (*websocket.Conn, string, error) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return nil, "", err
	}

	domain, err := s.extractDomain(r.Host)

	if err != nil {
		conn.Close()
		return nil, "", err
	}

	return conn, domain, nil
}

func (s *Server) registerClient(conn *websocket.Conn, domain, session string) (*Connection, error) {
	connection, stale, err := s.connManager.AddConnection(domain, session, conn)
	if err != nil {
		conn.WriteJSON(common.Message{Type: common.MessageTypeDomainTaken})
		conn.Close()
		return nil, fmt.Errorf("registering domain: %w", err)
	}

	if stale != nil {
		s.Logger.Info("Client reconnected, replacing stale connection", "domain", domain)
		stale.Close()
	}

	if err := connection.SendMessage(&common.Message{Type: common.MessageTypeDomainRegistered}); err != nil {
		s.connManager.RemoveConnection(domain, connection)
		connection.Close()
		return nil, fmt.Errorf("failed to send registration confirmation: %w", err)
	}

	// Only expose the domain to HTTP forwarding after the confirmation has been
	// written, so the client always reads domain_registered before any request.
	s.connManager.ActivateConnection(domain)

	return connection, nil
}

func disconnectReason(err error) string {
	if websocket.IsCloseError(err, websocket.CloseNormalClosure) {
		return "normal closure"
	}
	return "connection error: " + err.Error()
}

func (s *Server) newTunnelDispatcher(domain string, connection *Connection) *common.MessageDispatcher {
	dispatcher := common.NewMessageDispatcher()

	dispatcher.Register(common.MessageTypeHTTPResponse, func(msg *common.Message) error {
		if err := connection.DeliverResponse(msg); err != nil {
			// The waiter is gone: the request timed out, or the browser hung up.
			// Routine on a public tunnel and not actionable, so it is not
			// reported as a dispatch failure.
			s.Logger.Debug("dropped response with no waiting request",
				"domain", domain, "uuid", msg.UUID, "status", msg.Status, "error", err)
			return nil
		}
		s.requestLogger.LogHTTPResponse(domain, msg.UUID, msg.Status, msg.Body)
		return nil
	})

	dispatcher.Register(common.MessageTypeStreamData, func(msg *common.Message) error {
		connection.streams.Deliver(msg.UUID, msg.Body)
		return nil
	})

	dispatcher.Register(common.MessageTypeStreamClose, func(msg *common.Message) error {
		connection.streams.Finish(msg.UUID)
		return nil
	})

	return dispatcher
}

func (s *Server) handleWebSocketConnection(domain string, remoteAddr string, connection *Connection) {
	conn := connection.conn
	defer connection.Close()

	common.RunMessageLoop(conn, s.newTunnelDispatcher(domain, connection), s.heartbeat,
		func(err error) {
			s.requestLogger.LogClientDisconnected(domain, remoteAddr, disconnectReason(err))
			s.connManager.RemoveConnection(domain, connection)
			connection.Close()
		},
		func(msg *common.Message, err error) {
			s.Logger.Error("Message dispatch failed", "uuid", msg.UUID, "error", err)
		},
	)
}
