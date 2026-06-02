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

	err = s.registerClient(conn, domain)

	if err != nil {
		s.Logger.Error("Client registration failed", "error", err, "domain", domain)
		return
	}

	s.requestLogger.LogClientConnected(domain, r.RemoteAddr)
	s.handleWebSocketConnection(domain, r.RemoteAddr, conn)
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

func (s *Server) registerClient(conn *websocket.Conn, domain string) error {
	connection, err := s.connManager.AddConnection(domain, conn)
	if err != nil {
		conn.WriteJSON(common.Message{Type: common.MessageTypeDomainTaken})
		conn.Close()
		return fmt.Errorf("registering domain: %w", err)
	}

	if err := connection.SendMessage(&common.Message{Type: common.MessageTypeDomainRegistered}); err != nil {
		s.connManager.RemoveConnection(domain)
		connection.Close()
		return fmt.Errorf("failed to send registration confirmation: %w", err)
	}

	return nil
}

func disconnectReason(err error) string {
	if websocket.IsCloseError(err, websocket.CloseNormalClosure) {
		return "normal closure"
	}
	return "connection error: " + err.Error()
}

func (s *Server) handleWebSocketConnection(domain string, remoteAddr string, conn *websocket.Conn) {
	defer conn.Close()

	common.RunMessageLoop(conn, s.dispatcher,
		func(err error) {
			s.requestLogger.LogClientDisconnected(domain, remoteAddr, disconnectReason(err))
			s.connManager.RemoveConnection(domain)
		},
		func(msg *common.Message, err error) {
			s.Logger.Error("Message dispatch failed", "uuid", msg.UUID, "error", err)
		},
	)
}
