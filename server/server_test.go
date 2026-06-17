package server

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	s, err := NewServer(&Config{Port: 9999})
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	return s
}

func newTestServerWithAuth(t *testing.T, token string) *Server {
	t.Helper()
	s, err := NewServer(&Config{Port: 9999, AuthToken: token})
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	return s
}

func TestNewServer_InvalidConfig(t *testing.T) {
	_, err := NewServer(&Config{Port: 0})
	if err == nil {
		t.Error("expected error for invalid config, got nil")
	}
}

func TestNewServer_ValidConfig(t *testing.T) {
	s, err := NewServer(&Config{Port: 8080})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s == nil {
		t.Fatal("expected non-nil server")
	}
	if s.Logger == nil {
		t.Error("expected non-nil Logger")
	}
}

func TestNewServer_DebugMode(t *testing.T) {
	s, err := NewServer(&Config{Port: 8080, Debug: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s == nil {
		t.Fatal("expected non-nil server")
	}
}

func TestExtractDomain(t *testing.T) {
	tests := []struct {
		host       string
		wantDomain string
		wantErr    bool
	}{
		{"myapp.example.com", "myapp", false},
		{"foo.localhost", "foo", false},
		{"MyApp.example.com", "myapp", false},
		{"FOO.localhost:8080", "foo", false},
		{"localhost", "", true},
		{"", "", true},
		{".example.com", "", true},
	}

	s := newTestServer(t)
	for _, tc := range tests {
		t.Run(tc.host, func(t *testing.T) {
			domain, err := s.extractDomain(tc.host)
			if (err != nil) != tc.wantErr {
				t.Errorf("extractDomain(%q) error = %v, wantErr %v", tc.host, err, tc.wantErr)
			}
			if !tc.wantErr && domain != tc.wantDomain {
				t.Errorf("extractDomain(%q) = %q, want %q", tc.host, domain, tc.wantDomain)
			}
		})
	}
}

func TestHandleHealth(t *testing.T) {
	s := newTestServer(t)
	r := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	s.handleHealth(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if body := w.Body.String(); body != "OK" {
		t.Errorf("body = %q, want %q", body, "OK")
	}
}

func TestHandleMetrics_NoConnections(t *testing.T) {
	s := newTestServer(t)
	r := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()

	s.handleMetrics(w, r)

	want := "active_connections: 0\n"
	if body := w.Body.String(); body != want {
		t.Errorf("body = %q, want %q", body, want)
	}
}

func TestHandleMetrics_WithConnections(t *testing.T) {
	ws, cleanup := newTestWSPair(t)
	defer cleanup()

	s := newTestServer(t)
	s.connManager.AddConnection("test", ws) //nolint:errcheck

	r := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()

	s.handleMetrics(w, r)

	want := "active_connections: 1\n"
	if body := w.Body.String(); body != want {
		t.Errorf("body = %q, want %q", body, want)
	}
}

func TestShutdown(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()

	if err := s.Shutdown(ctx); err != nil {
		t.Errorf("Shutdown() error = %v", err)
	}
}

func TestStart_ShutdownClean(t *testing.T) {
	s, err := NewServer(&Config{Port: 19999})
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	errCh := make(chan error, 1)
	go func() { errCh <- s.Start() }()

	time.Sleep(50 * time.Millisecond)

	if err := s.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}

	if err := <-errCh; err != nil {
		t.Errorf("Start() error = %v, want nil", err)
	}
}

func TestStart_PortAlreadyInUse(t *testing.T) {
	l, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("failed to acquire port: %v", err)
	}
	defer l.Close()
	port := l.Addr().(*net.TCPAddr).Port

	s, err := NewServer(&Config{Port: port})
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	if err := s.Start(); err == nil {
		t.Error("expected error when port is already in use, got nil")
	}
}

func TestAuthenticateRequest_NoTokenConfigured(t *testing.T) {
	s := newTestServer(t)
	r := httptest.NewRequest(http.MethodGet, "/ws", nil)

	if !s.authenticateRequest(r) {
		t.Error("expected auth to pass when no token configured")
	}
}

func TestAuthenticateRequest_ValidBearerToken(t *testing.T) {
	s := newTestServerWithAuth(t, "secret123")
	r := httptest.NewRequest(http.MethodGet, "/ws", nil)
	r.Header.Set("Authorization", "Bearer secret123")

	if !s.authenticateRequest(r) {
		t.Error("expected auth to pass with valid token")
	}
}

func TestAuthenticateRequest_CaseInsensitiveBearer(t *testing.T) {
	s := newTestServerWithAuth(t, "secret123")
	r := httptest.NewRequest(http.MethodGet, "/ws", nil)
	r.Header.Set("Authorization", "bearer secret123")

	if !s.authenticateRequest(r) {
		t.Error("expected auth to pass with lowercase bearer prefix")
	}
}

func TestAuthenticateRequest_InvalidToken(t *testing.T) {
	s := newTestServerWithAuth(t, "secret123")
	r := httptest.NewRequest(http.MethodGet, "/ws", nil)
	r.Header.Set("Authorization", "Bearer wrongtoken")

	if s.authenticateRequest(r) {
		t.Error("expected auth to fail with wrong token")
	}
}

func TestAuthenticateRequest_MissingHeader(t *testing.T) {
	s := newTestServerWithAuth(t, "secret123")
	r := httptest.NewRequest(http.MethodGet, "/ws", nil)

	if s.authenticateRequest(r) {
		t.Error("expected auth to fail with missing header")
	}
}

func TestAuthenticateRequest_RawTokenWithoutBearer(t *testing.T) {
	s := newTestServerWithAuth(t, "secret123")
	r := httptest.NewRequest(http.MethodGet, "/ws", nil)
	r.Header.Set("Authorization", "secret123")

	if !s.authenticateRequest(r) {
		t.Error("expected auth to pass with raw token (no Bearer prefix)")
	}
}

func TestSetupRoutes(t *testing.T) {
	s := newTestServer(t)
	srv := httptest.NewServer(s.mux)
	defer srv.Close()

	resp, err := http.Get(fmt.Sprintf("%s/health", srv.URL))
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("/health status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestRouteRequest_HealthOnBareHost(t *testing.T) {
	s := newTestServer(t)
	r := httptest.NewRequest(http.MethodGet, "/health", nil)
	r.Host = "wormhole"
	w := httptest.NewRecorder()

	s.routeRequest(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if body := w.Body.String(); body != "OK" {
		t.Errorf("body = %q, want %q", body, "OK")
	}
}

func TestRouteRequest_MetricsOnBareHost(t *testing.T) {
	s := newTestServer(t)
	r := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	r.Host = "wormhole"
	w := httptest.NewRecorder()

	s.routeRequest(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if body := w.Body.String(); body != "active_connections: 0\n" {
		t.Errorf("unexpected body: %q", body)
	}
}

func TestRouteRequest_HealthOnSubdomainIsTunneled(t *testing.T) {
	s := newTestServer(t)
	r := httptest.NewRequest(http.MethodGet, "/health", nil)
	r.Host = "myapp.wormhole.tools"
	w := httptest.NewRecorder()

	s.routeRequest(w, r)

	if w.Code == http.StatusOK && w.Body.String() == "OK" {
		t.Error("subdomain /health should be tunneled, not answered by server")
	}
	if w.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want %d (no tunnel registered)", w.Code, http.StatusBadGateway)
	}
}

func TestRouteRequest_MetricsOnSubdomainIsTunneled(t *testing.T) {
	s := newTestServer(t)
	r := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	r.Host = "myapp.wormhole.tools"
	w := httptest.NewRecorder()

	s.routeRequest(w, r)

	if w.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want %d (no tunnel registered)", w.Code, http.StatusBadGateway)
	}
}

func TestRouteRequest_UnknownPathOnBareHost(t *testing.T) {
	s := newTestServer(t)
	r := httptest.NewRequest(http.MethodGet, "/nope", nil)
	r.Host = "wormhole"
	w := httptest.NewRecorder()

	s.routeRequest(w, r)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestRouteRequest_ConfiguredHostMatchesBare(t *testing.T) {
	s, err := NewServer(&Config{Port: 9999, Host: "wormhole.tools"})
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	r := httptest.NewRequest(http.MethodGet, "/health", nil)
	r.Host = "wormhole.tools"
	w := httptest.NewRecorder()

	s.routeRequest(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestRouteRequest_ConfiguredHost_SubdomainTunneled(t *testing.T) {
	s, err := NewServer(&Config{Port: 9999, Host: "wormhole.tools"})
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	r := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	r.Host = "damian.wormhole.tools"
	w := httptest.NewRecorder()

	s.routeRequest(w, r)

	if w.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want %d (subdomain /metrics should tunnel)", w.Code, http.StatusBadGateway)
	}
}

func TestRouteRequest_ConfiguredHost_IgnoresPort(t *testing.T) {
	s, err := NewServer(&Config{Port: 9999, Host: "wormhole.tools"})
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	r := httptest.NewRequest(http.MethodGet, "/health", nil)
	r.Host = "wormhole.tools:8080"
	w := httptest.NewRecorder()

	s.routeRequest(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}
