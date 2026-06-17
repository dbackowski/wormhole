package server

import (
	"context"
	"crypto/subtle"
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/dbackowski/wormhole/common"
)

type Server struct {
	connManager   *ConnectionManager
	Logger        *common.Logger
	requestLogger *common.RequestLogger
	httpServer    *http.Server
	mux           *http.ServeMux
	authToken     string
	host          string
	heartbeat     common.Heartbeat
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
		Logger:        logger,
		requestLogger: common.NewRequestLogger(logger),
		authToken:     cfg.AuthToken,
		host:          cfg.Host,
		heartbeat:     common.DefaultHeartbeat(),
	}

	server.setupRoutes()

	server.httpServer = &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.Port),
		Handler: server.mux,
	}

	return &server, nil
}

func (s *Server) setupRoutes() {
	s.mux.HandleFunc("/ws", s.ServeWebSocket)
	s.mux.HandleFunc("/", s.routeRequest)
}

func (s *Server) routeRequest(w http.ResponseWriter, r *http.Request) {
	if s.isBareHost(r.Host) {
		switch r.URL.Path {
		case "/health":
			s.handleHealth(w, r)
		case "/metrics":
			s.handleMetrics(w, r)
		default:
			http.NotFound(w, r)
		}
		return
	}
	s.ServeHTTP(w, r)
}

func (s *Server) isBareHost(host string) bool {
	h, _, err := net.SplitHostPort(host)
	if err != nil {
		h = host
	}
	if s.host != "" {
		return strings.EqualFold(h, s.host)
	}
	if net.ParseIP(h) != nil {
		return true
	}
	return !strings.Contains(h, ".")
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	s.writeResponse(w, http.StatusOK, []byte("OK"))
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "active_connections: %d\n", s.connManager.Count())
}

func (s *Server) authenticateRequest(r *http.Request) bool {
	if s.authToken == "" {
		return true
	}

	token := r.Header.Get("Authorization")
	if len(token) > 7 && strings.EqualFold(token[:7], "bearer ") {
		token = token[7:]
	}

	return subtle.ConstantTimeCompare([]byte(token), []byte(s.authToken)) == 1
}

func (s *Server) extractDomain(host string) (string, error) {
	parts := strings.SplitN(host, ".", 2)

	if len(parts) < 2 || parts[0] == "" {
		return "", fmt.Errorf("invalid host: %s", host)
	}
	return strings.ToLower(parts[0]), nil
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
