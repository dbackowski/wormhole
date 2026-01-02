package server

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/dbackowski/wormhole/common"
)

type Server struct {
	connManager *ConnectionManager
	dispatcher  *common.MessageDispatcher
	httpServer  *http.Server
	mux         *http.ServeMux
	debug       bool
}

func NewServer(cfg *Config) (*Server, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	server := Server{
		connManager: NewConnectionManager(),
		mux:         http.NewServeMux(),
		debug:       cfg.Debug,
	}

	server.setupMessageHandlers()
	server.setupRoutes()

	server.httpServer = &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.Port),
		Handler: server.mux,
	}

	return &server, nil
}

func (s *Server) setupRoutes() {
	s.mux.HandleFunc("/ws", s.ServeWebSocket)
	s.mux.HandleFunc("/", s.ServeHTTP)
	s.mux.HandleFunc("/health", s.handleHealth)
	s.mux.HandleFunc("/metrics", s.handleMetrics)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "active_connections: %d\n", s.connManager.Count())
}

func (s *Server) setupMessageHandlers() {
	s.dispatcher = common.NewMessageDispatcher()
	s.dispatcher.Register(common.MessageTypeHTTPResponse, s.handleHTTPResponse)
}

func (s *Server) DeliverResponse(message *common.Message) {
	connection, exists := s.connManager.GetConnection(message.Domain)

	if exists {
		connection.Requests.Deliver(message)
	}
}

func (s *Server) CleanupRequest(domain string, uuid string) {
	connection, exists := s.connManager.GetConnection(domain)

	if exists {
		connection.Requests.Cleanup(uuid)
	}
}

func (s *Server) handleHTTPResponse(msg *common.Message) {
	if s.debug {
		fmt.Println("Received HTTP response from client:")
		common.PrettyPrintMessage(*msg)
	} else {
		fmt.Printf("%s Received HTTP response %d for UUID: %s\n", common.FormatTime(time.Now()), msg.Status, msg.UUID)
	}
	s.DeliverResponse(msg)
}

func (s *Server) Start() error {
	fmt.Printf("WebSocket server starting on %s\n", s.httpServer.Addr)

	if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("server failed: %w", err)
	}

	return nil
}

func (s *Server) Shutdown(ctx context.Context) error {
	fmt.Println("Shutting down server gracefully...")

	s.connManager.CloseAll()

	if err := s.httpServer.Shutdown(ctx); err != nil {
		return fmt.Errorf("server shutdown failed: %w", err)
	}

	fmt.Println("Server stopped")
	return nil
}
