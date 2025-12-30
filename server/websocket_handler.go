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
		fmt.Printf("WebSocket upgrade or extracting domain failed: %v\n", err)
		return
	}

	err = s.registerClient(conn, domain)

	if err != nil {
		fmt.Printf("Client registration failed for domain %s: %v\n", domain, err)
		return
	}

	fmt.Printf("Registered connection for domain: %s\n", domain)
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
	err := s.connManager.AddConnection(domain, conn)

	if err != nil {
		if writeErr := conn.WriteJSON(common.Message{Type: common.MessageTypeDomainTaken}); writeErr != nil {
			conn.Close()
			return fmt.Errorf("domain %s already taken, failed to notify client: %w", domain, writeErr)
		}
		conn.Close()
		return fmt.Errorf("domain %s already taken", domain)
	}

	if err = conn.WriteJSON(common.Message{Type: common.MessageTypeDomainRegistered}); err != nil {
		s.connManager.RemoveConnection(domain)
		conn.Close()
		return fmt.Errorf("failed to send registration confirmation: %w", err)
	}

	return nil
}

func (s *Server) handleWebSocketConnection(domain string, conn *websocket.Conn) {
	defer conn.Close()

	for {
		var message common.Message

		err := conn.ReadJSON(&message)

		if err != nil {
			fmt.Printf("Closing connection for domain: %s\n", domain)
			s.connManager.RemoveConnection(domain)
			return
		}

		err = s.dispatcher.Dispatch(&message)
		if err != nil {
			fmt.Printf("Failed to dispatch message: %v\n", err)
		}
	}
}
