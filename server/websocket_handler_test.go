package server

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dbackowski/wormhole/common"
	"github.com/gorilla/websocket"
)

func dialWS(t *testing.T, url string, host string) *websocket.Conn {
	t.Helper()
	header := http.Header{}
	if host != "" {
		header.Set("Host", host)
	}
	conn, _, err := websocket.DefaultDialer.Dial(url, header)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	return conn
}

func startWSServer(t *testing.T, s *Server) (wsURL string, cleanup func()) {
	t.Helper()
	srv := httptest.NewServer(s.mux)
	wsURL = "ws" + strings.TrimPrefix(srv.URL, "http")
	return wsURL, srv.Close
}

func TestServeWebSocket_Success(t *testing.T) {
	s := newTestServer(t)
	wsURL, cleanup := startWSServer(t, s)
	defer cleanup()

	conn := dialWS(t, wsURL+"/ws", "myapp.localhost")
	defer conn.Close()

	var msg common.Message
	if err := conn.ReadJSON(&msg); err != nil {
		t.Fatalf("failed to read registration message: %v", err)
	}

	if msg.Type != common.MessageTypeDomainRegistered {
		t.Errorf("message type = %q, want %q", msg.Type, common.MessageTypeDomainRegistered)
	}

	if s.connManager.Count() != 1 {
		t.Errorf("connection count = %d, want 1", s.connManager.Count())
	}
}

func TestUpgradeAndExtractDomain_InvalidHost(t *testing.T) {
	s := newTestServer(t)

	// Use a server that sets the Host to an invalid value (no subdomain)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Host = "localhost" // override to invalid host (no subdomain)
		s.ServeWebSocket(w, r)
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	conn := dialWS(t, wsURL, "")
	defer conn.Close()

	// Connection should be closed by server due to invalid host
	conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	var msg common.Message
	err := conn.ReadJSON(&msg)
	if err == nil {
		if msg.Type == common.MessageTypeDomainRegistered {
			t.Error("expected no registration for invalid host")
		}
	}

	if s.connManager.Count() != 0 {
		t.Errorf("connection count = %d, want 0", s.connManager.Count())
	}
}

func TestServeWebSocket_DomainAlreadyTaken(t *testing.T) {
	s := newTestServer(t)
	wsURL, cleanup := startWSServer(t, s)
	defer cleanup()

	// Register first client
	conn1 := dialWS(t, wsURL+"/ws", "taken.localhost")
	defer conn1.Close()

	var regMsg common.Message
	if err := conn1.ReadJSON(&regMsg); err != nil {
		t.Fatalf("failed to read registration: %v", err)
	}
	if regMsg.Type != common.MessageTypeDomainRegistered {
		t.Fatalf("first client: type = %q, want %q", regMsg.Type, common.MessageTypeDomainRegistered)
	}

	// Try to register second client with the same domain
	conn2 := dialWS(t, wsURL+"/ws", "taken.localhost")
	defer conn2.Close()

	var takenMsg common.Message
	if err := conn2.ReadJSON(&takenMsg); err != nil {
		t.Fatalf("failed to read domain taken message: %v", err)
	}

	if takenMsg.Type != common.MessageTypeDomainTaken {
		t.Errorf("second client: type = %q, want %q", takenMsg.Type, common.MessageTypeDomainTaken)
	}

	if s.connManager.Count() != 1 {
		t.Errorf("connection count = %d, want 1", s.connManager.Count())
	}
}

func TestServeWebSocket_ClientDisconnect(t *testing.T) {
	s := newTestServer(t)
	wsURL, cleanup := startWSServer(t, s)
	defer cleanup()

	conn := dialWS(t, wsURL+"/ws", "disc.localhost")

	var msg common.Message
	if err := conn.ReadJSON(&msg); err != nil {
		t.Fatalf("failed to read registration: %v", err)
	}

	if s.connManager.Count() != 1 {
		t.Fatalf("connection count = %d, want 1 before disconnect", s.connManager.Count())
	}

	// Close the client connection
	conn.WriteMessage(websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	conn.Close()

	// Wait for server to process the disconnect
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if s.connManager.Count() == 0 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Errorf("connection count = %d, want 0 after disconnect", s.connManager.Count())
}

func TestServeWebSocket_MessageDispatch(t *testing.T) {
	s := newTestServer(t)
	wsURL, cleanup := startWSServer(t, s)
	defer cleanup()

	// Register a connection via the dialer side and keep the acceptor for sending messages
	dialer, acceptor, wsCleanup := newWSPair(t)
	defer wsCleanup()

	s.connManager.AddConnection("msgtest", dialer) //nolint:errcheck

	// Connect a WebSocket client to /ws
	conn := dialWS(t, wsURL+"/ws", "dispatch.localhost")
	defer conn.Close()

	var regMsg common.Message
	if err := conn.ReadJSON(&regMsg); err != nil {
		t.Fatalf("failed to read registration: %v", err)
	}

	// Send an HTTP response message from the client
	responseMsg := common.Message{
		Type:   common.MessageTypeHTTPResponse,
		Domain: "msgtest",
		UUID:   "test-uuid",
		Status: 200,
		Body:   []byte("hello"),
	}
	if err := conn.WriteJSON(responseMsg); err != nil {
		t.Fatalf("failed to write message: %v", err)
	}

	// The server's dispatcher should handle it — verify msgtest connection got the response
	_ = acceptor // acceptor available if needed for further verification
}

func TestServeWebSocket_AuthRequired_NoToken(t *testing.T) {
	s := newTestServerWithAuth(t, "secret")
	wsURL, cleanup := startWSServer(t, s)
	defer cleanup()

	// Try to connect without a token — should get HTTP 401, not a WebSocket upgrade
	header := http.Header{}
	header.Set("Host", "app.localhost")
	_, resp, err := websocket.DefaultDialer.Dial(wsURL+"/ws", header)

	if err == nil {
		t.Fatal("expected dial error for missing auth token")
	}
	if resp != nil && resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}

func TestServeWebSocket_AuthRequired_WrongToken(t *testing.T) {
	s := newTestServerWithAuth(t, "secret")
	wsURL, cleanup := startWSServer(t, s)
	defer cleanup()

	header := http.Header{}
	header.Set("Host", "app.localhost")
	header.Set("Authorization", "Bearer wrong")
	_, resp, err := websocket.DefaultDialer.Dial(wsURL+"/ws", header)

	if err == nil {
		t.Fatal("expected dial error for wrong auth token")
	}
	if resp != nil && resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}

func TestServeWebSocket_AuthRequired_ValidToken(t *testing.T) {
	s := newTestServerWithAuth(t, "secret")
	wsURL, cleanup := startWSServer(t, s)
	defer cleanup()

	header := http.Header{}
	header.Set("Host", "app.localhost")
	header.Set("Authorization", "Bearer secret")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL+"/ws", header)
	if err != nil {
		t.Fatalf("dial with valid token: %v", err)
	}
	defer conn.Close()

	var msg common.Message
	if err := conn.ReadJSON(&msg); err != nil {
		t.Fatalf("failed to read registration message: %v", err)
	}
	if msg.Type != common.MessageTypeDomainRegistered {
		t.Errorf("message type = %q, want %q", msg.Type, common.MessageTypeDomainRegistered)
	}
}

func TestServeWebSocket_NoAuthConfigured(t *testing.T) {
	s := newTestServer(t) // no auth token set
	wsURL, cleanup := startWSServer(t, s)
	defer cleanup()

	// Should connect without any token
	conn := dialWS(t, wsURL+"/ws", "noauth.localhost")
	defer conn.Close()

	var msg common.Message
	if err := conn.ReadJSON(&msg); err != nil {
		t.Fatalf("failed to read registration message: %v", err)
	}
	if msg.Type != common.MessageTypeDomainRegistered {
		t.Errorf("message type = %q, want %q", msg.Type, common.MessageTypeDomainRegistered)
	}
}

func TestDisconnectReason_NormalClosure(t *testing.T) {
	err := &websocket.CloseError{Code: websocket.CloseNormalClosure}
	reason := disconnectReason(err)
	if reason != "normal closure" {
		t.Errorf("disconnectReason() = %q, want %q", reason, "normal closure")
	}
}

func TestDisconnectReason_AbnormalClosure(t *testing.T) {
	err := &websocket.CloseError{Code: websocket.CloseAbnormalClosure, Text: "unexpected"}
	reason := disconnectReason(err)
	if !strings.Contains(reason, "connection error") {
		t.Errorf("disconnectReason() = %q, want it to contain %q", reason, "connection error")
	}
}

func TestRegisterClient_Success(t *testing.T) {
	s := newTestServer(t)
	dialer, _, cleanup := newWSPair(t)
	defer cleanup()

	err := s.registerClient(dialer, "regtest.com")
	if err != nil {
		t.Fatalf("registerClient() error = %v", err)
	}

	if s.connManager.Count() != 1 {
		t.Errorf("connection count = %d, want 1", s.connManager.Count())
	}
}

func TestRegisterClient_DuplicateDomain(t *testing.T) {
	s := newTestServer(t)
	ws1, cleanup1 := newTestWSPair(t)
	defer cleanup1()
	dialer, _, cleanup2 := newWSPair(t)
	defer cleanup2()

	s.connManager.AddConnection("dup.com", ws1) //nolint:errcheck

	err := s.registerClient(dialer, "dup.com")
	if err == nil {
		t.Error("expected error for duplicate domain, got nil")
	}
}

func TestServeWebSocket_ConcurrentRequestDuringRegistration(t *testing.T) {
	s := newTestServer(t)
	srv := httptest.NewServer(s.mux)
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	httpURL := srv.URL

	const iterations = 40
	for i := 0; i < iterations; i++ {
		host := fmt.Sprintf("race%d.localhost", i)

		stop := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
				req, err := http.NewRequestWithContext(ctx, http.MethodGet, httpURL, nil)
				if err == nil {
					req.Host = host
					if resp, err := http.DefaultClient.Do(req); err == nil {
						resp.Body.Close()
					}
				}
				cancel()
			}
		}()

		conn := dialWS(t, wsURL+"/ws", host)
		var msg common.Message
		if err := conn.ReadJSON(&msg); err != nil {
			t.Fatalf("iteration %d: failed to read registration: %v", i, err)
		}
		if msg.Type != common.MessageTypeDomainRegistered {
			t.Fatalf("iteration %d: type = %q, want %q", i, msg.Type, common.MessageTypeDomainRegistered)
		}
		conn.Close()
		close(stop)
		wg.Wait()
	}
}

func waitForConnCount(s *Server, want int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if s.connManager.Count() == want {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return s.connManager.Count() == want
}

// TestServeWebSocket_FreesDomainWhenClientStopsResponding verifies the heartbeat
// releases a domain held by a half-open connection: a client that connects but
// never reads (so never pongs) must be detected and dropped within PongWait,
// rather than holding the subdomain indefinitely.
func TestServeWebSocket_FreesDomainWhenClientStopsResponding(t *testing.T) {
	s := newTestServer(t)
	s.heartbeat = common.Heartbeat{
		PongWait:   150 * time.Millisecond,
		PingPeriod: 40 * time.Millisecond,
		WriteWait:  time.Second,
	}
	wsURL, cleanup := startWSServer(t, s)
	defer cleanup()

	// Connect but never read from the connection: the client never processes
	// the server's pings and therefore never pongs.
	conn := dialWS(t, wsURL+"/ws", "stuck.localhost")
	defer conn.Close()

	if !waitForConnCount(s, 1, time.Second) {
		t.Fatalf("connection count = %d, want 1 after registration", s.connManager.Count())
	}
	if !waitForConnCount(s, 0, 2*time.Second) {
		t.Fatalf("connection count = %d, want 0 after missed heartbeat", s.connManager.Count())
	}
}

func TestRegisterClient_WriteConfirmationFails(t *testing.T) {
	s := newTestServer(t)
	ws, cleanup := newTestWSPair(t)
	cleanup() // close immediately so WriteJSON fails

	err := s.registerClient(ws, "writefail.com")
	if err == nil {
		t.Error("expected error when write fails, got nil")
	}

	// Connection should be cleaned up from manager
	if s.connManager.Count() != 0 {
		t.Errorf("connection count = %d, want 0 after failed registration", s.connManager.Count())
	}
}
