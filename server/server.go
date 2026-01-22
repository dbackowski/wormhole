package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/dbackowski/wormhole/common"
)

type Server struct {
	connManager   *ConnectionManager
	dispatcher    *common.MessageDispatcher
	Logger        *common.Logger
	requestLogger *common.RequestLogger
	httpServer    *http.Server
	mux           *http.ServeMux
	debug         bool
}

func NewServer(cfg *Config) (*Server, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	logLvl := common.LevelInfo
	if cfg.Debug {
		logLvl = common.LevelDebug
	}

	logger := common.NewLogger(logLvl, "text")

	server := Server{
		connManager:   NewConnectionManager(),
		mux:           http.NewServeMux(),
		debug:         cfg.Debug,
		Logger:        logger,
		requestLogger: common.NewRequestLogger(logger),
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

func (s *Server) DeliverResponse(message *common.Message) error {
	connection, err := s.connManager.GetConnection(message.Domain)

	if err != nil {
		return err
	}

	if err := connection.DeliverResponse(message); err != nil {
		return fmt.Errorf("failed to deliver HTTP response for %s: %w", message.UUID, err)
	}

	return nil
}

func (s *Server) CleanupRequest(domain string, uuid string) {
	connection, err := s.connManager.GetConnection(domain)

	if err == nil {
		connection.CleanupRequest(uuid)
	}
}

func (s *Server) handleHTTPResponse(msg *common.Message) error {
	s.requestLogger.LogHTTPResponse(msg.Domain, msg.UUID, msg.Status, msg.Body)
	if err := s.DeliverResponse(msg); err != nil {
		return err
	}
	return nil
}

func (s *Server) extractDomain(host string) (string, error) {
	parts := strings.Split(host, ".")

	if parts[0] == "" {
		return "", fmt.Errorf("invalid host: %s", host)
	}
	return parts[0], nil
}

func (s *Server) Start() error {
	s.Logger.Info("Starting WebSocket server", "addr", s.httpServer.Addr)

	if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("server failed: %w", err)
	}

	return nil
}

func (s *Server) Shutdown(ctx context.Context) error {
	s.Logger.Info("Shutting down server gracefully")
	s.connManager.CloseAll()

	if err := s.httpServer.Shutdown(ctx); err != nil {
		return fmt.Errorf("server shutdown failed: %w", err)
	}

	s.Logger.Info("Server stopped")
	return nil
}
