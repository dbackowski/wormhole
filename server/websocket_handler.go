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
	conn, domain, err := s.upgradeAndExtractDomain(w, r)

	if err != nil {
		s.requestLogger.LogConnectionError("Websocker upgrade failed", err, map[string]interface{}{
			"remote_addr": r.RemoteAddr,
		})
		return
	}

	err = s.registerClient(conn, domain)

	if err != nil {
		s.requestLogger.LogConnectionError("Client registration failed", err, map[string]interface{}{
			"domain": domain,
		})
		return
	}

	s.requestLogger.LogClientRegistered(domain, r.RemoteAddr)
	s.handleWebSocketConnection(domain, conn)
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
	registered := false

	defer func() {
		if !registered {
			conn.Close()
		}
	}()

	err := s.connManager.AddConnection(domain, conn)

	if err != nil {
		if writeErr := conn.WriteJSON(common.Message{Type: common.MessageTypeDomainTaken}); writeErr != nil {
			return fmt.Errorf("domain %s is already taken, failed to notify client: %w", domain, writeErr)
		}
		return fmt.Errorf("domain %s is already taken", domain)
	}

	if err = conn.WriteJSON(common.Message{Type: common.MessageTypeDomainRegistered}); err != nil {
		s.connManager.RemoveConnection(domain)
		return fmt.Errorf("failed to send registration confirmation: %w", err)
	}

	registered = true
	return nil
}

func (s *Server) handleWebSocketConnection(domain string, conn *websocket.Conn) {
	defer conn.Close()

	for {
		var message common.Message

		err := conn.ReadJSON(&message)

		if err != nil {
			s.requestLogger.LogClientDisconnected(domain, "", "normal closure")
			s.connManager.RemoveConnection(domain)
			return
		}

		err = s.dispatcher.Dispatch(&message)
		if err != nil {
			s.requestLogger.LogDispatchError(message.UUID, err)
		}
	}
}
