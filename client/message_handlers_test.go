package client

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/dbackowski/wormhole/common"

	"github.com/gorilla/websocket"
)

type mockDisplay struct {
	mu       sync.Mutex
	rendered bool
	renders  int
	lastLogs []RequestLog
}

func (m *mockDisplay) Render(f Frame) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rendered = true
	m.renders++
	m.lastLogs = f.Logs
}

func (m *mockDisplay) Renders() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.renders
}

// redrawRequested reports whether c asked for a redraw, and takes the request.
func redrawRequested(c *Client) bool {
	select {
	case <-c.redraw:
		return true
	default:
		return false
	}
}

func (m *mockDisplay) Rendered() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.rendered
}

func (m *mockDisplay) LastLogs() []RequestLog {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastLogs
}

func setupTestWebsocket(t *testing.T) (*websocket.Conn, *httptest.Server, *websocket.Conn) {
	t.Helper()

	var serverConn *websocket.Conn
	upgrader := websocket.Upgrader{}
	ready := make(chan struct{})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error
		serverConn, err = upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("failed to upgrade: %v", err)
		}
		close(ready)
		// Keep connection open until test ends
		select {}
	}))

	wsURL := "ws" + server.URL[len("http"):]
	clientConn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		server.Close()
		t.Fatalf("failed to dial: %v", err)
	}

	<-ready
	return clientConn, server, serverConn
}

func newTestClient(t *testing.T, proxyServerURL string) (*Client, *mockDisplay, *httptest.Server, *websocket.Conn) {
	t.Helper()

	clientConn, wsServer, serverConn := setupTestWebsocket(t)
	display := &mockDisplay{}

	client := &Client{
		domain:    "test",
		tunnelURL: "https://test.example.com",
		Conn:      clientConn,
		proxy:     NewLocalProxy(mustParseURL(t, proxyServerURL), "https://test.example.com", 5*time.Second),
		history:   NewRequestHistory(100),
		display:   display,
		WebUIPort: 4040,
		redraw:    make(chan struct{}, 1),
	}

	return client, display, wsServer, serverConn
}

func TestHandleHTTPRequest(t *testing.T) {
	const appDelay = 30 * time.Millisecond
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(appDelay)
		w.Header().Set("X-Response", "test-value")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("proxy response"))
	}))
	defer proxyServer.Close()

	client, _, wsServer, serverConn := newTestClient(t, proxyServer.URL)
	defer wsServer.Close()
	defer client.Conn.Close()
	defer serverConn.Close()

	msg := &common.Message{
		Type:   common.MessageTypeHTTPRequest,
		UUID:   "test-uuid-123",
		Method: "GET",
		URL:    "/api/test",
		Headers: map[string][]string{
			"Host": {"test.example.com"},
		},
	}

	err := client.handleHTTPRequest(msg)
	if err != nil {
		t.Fatalf("handleHTTPRequest() error = %v, want nil", err)
	}

	serverConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var responseMsg common.Message
	if err := serverConn.ReadJSON(&responseMsg); err != nil {
		t.Fatalf("failed to read response message: %v", err)
	}
	if responseMsg.Type != common.MessageTypeHTTPResponse {
		t.Errorf("response Type = %q, want %q", responseMsg.Type, common.MessageTypeHTTPResponse)
	}
	if responseMsg.UUID != "test-uuid-123" {
		t.Errorf("response UUID = %q, want %q", responseMsg.UUID, "test-uuid-123")
	}
	if responseMsg.Status != http.StatusOK {
		t.Errorf("response Status = %d, want %d", responseMsg.Status, http.StatusOK)
	}
	if string(responseMsg.Body) != "proxy response" {
		t.Errorf("response Body = %q, want %q", string(responseMsg.Body), "proxy response")
	}

	logs := client.history.GetRecent(10)
	if len(logs) != 1 {
		t.Fatalf("history length = %d, want 1", len(logs))
	}
	log := logs[0]
	if log.UUID != "test-uuid-123" {
		t.Errorf("log UUID = %q, want %q", log.UUID, "test-uuid-123")
	}
	if log.Method != "GET" {
		t.Errorf("log Method = %q, want %q", log.Method, "GET")
	}
	if log.URL != "/api/test" {
		t.Errorf("log URL = %q, want %q", log.URL, "/api/test")
	}
	if log.StatusCode != http.StatusOK {
		t.Errorf("log StatusCode = %d, want %d", log.StatusCode, http.StatusOK)
	}
	if log.Error != "" {
		t.Errorf("log Error = %q, want empty on success", log.Error)
	}
	if log.Duration < appDelay || log.Duration > 2*time.Second {
		t.Errorf("log Duration = %v, want the app's %v delay", log.Duration, appDelay)
	}

	if !redrawRequested(client) {
		t.Error("expected a redraw to be requested")
	}
}

func TestHandleHTTPRequestProxyError(t *testing.T) {
	client, _, wsServer, serverConn := newTestClient(t, "http://127.0.0.1:1")
	defer wsServer.Close()
	defer client.Conn.Close()
	defer serverConn.Close()

	msg := &common.Message{
		Type:   common.MessageTypeHTTPRequest,
		UUID:   "error-uuid",
		Method: "GET",
		URL:    "/fail",
		Headers: map[string][]string{
			"Host": {"test.example.com"},
		},
	}

	err := client.handleHTTPRequest(msg)
	if err != nil {
		t.Fatalf("handleHTTPRequest() error = %v, want nil (proxy errors return 502)", err)
	}

	serverConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var responseMsg common.Message
	if err := serverConn.ReadJSON(&responseMsg); err != nil {
		t.Fatalf("failed to read response: %v", err)
	}
	if responseMsg.Status != http.StatusBadGateway {
		t.Errorf("response Status = %d, want %d", responseMsg.Status, http.StatusBadGateway)
	}
	if string(responseMsg.Body) != http.StatusText(http.StatusBadGateway) {
		t.Errorf("response Body = %q, want %q", string(responseMsg.Body), http.StatusText(http.StatusBadGateway))
	}

	logs := client.history.GetRecent(10)
	if len(logs) != 1 {
		t.Fatalf("history length = %d, want 1", len(logs))
	}
	if logs[0].StatusCode != http.StatusBadGateway {
		t.Errorf("log StatusCode = %d, want %d", logs[0].StatusCode, http.StatusBadGateway)
	}
	if logs[0].Error == "" {
		t.Error("expected log Error to capture the forward failure reason, got empty")
	}
}

func TestHandleHTTPRequestPreservesRequestDetails(t *testing.T) {
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte("created"))
	}))
	defer proxyServer.Close()

	client, _, wsServer, serverConn := newTestClient(t, proxyServer.URL)
	defer wsServer.Close()
	defer client.Conn.Close()
	defer serverConn.Close()

	reqHeaders := map[string][]string{
		"Host":         {"test.example.com"},
		"Content-Type": {"application/json"},
	}
	reqBody := []byte(`{"name":"test"}`)

	msg := &common.Message{
		Type:    common.MessageTypeHTTPRequest,
		UUID:    "preserve-uuid",
		Method:  "POST",
		URL:     "/api/items",
		Headers: reqHeaders,
		Body:    reqBody,
	}

	err := client.handleHTTPRequest(msg)
	if err != nil {
		t.Fatalf("handleHTTPRequest() error = %v, want nil", err)
	}

	serverConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var responseMsg common.Message
	serverConn.ReadJSON(&responseMsg)

	logs := client.history.GetRecent(10)
	if len(logs) != 1 {
		t.Fatalf("history length = %d, want 1", len(logs))
	}
	log := logs[0]
	if log.Method != "POST" {
		t.Errorf("log Method = %q, want %q", log.Method, "POST")
	}
	if string(log.RequestBody) != `{"name":"test"}` {
		t.Errorf("log RequestBody = %q, want %q", string(log.RequestBody), `{"name":"test"}`)
	}
	if log.RequestHeaders["Host"][0] != "test.example.com" {
		t.Errorf("log RequestHeaders Host = %q, want %q", log.RequestHeaders["Host"][0], "test.example.com")
	}
}

func TestDispatchHTTPRequestSheds503WhenSaturated(t *testing.T) {
	client, _, wsServer, serverConn := newTestClient(t, "http://localhost:1")
	defer wsServer.Close()
	defer client.Conn.Close()
	defer serverConn.Close()

	// Saturate the semaphore so no slots are available.
	client.requestSem = make(chan struct{}, 1)
	client.requestSem <- struct{}{}

	msg := &common.Message{
		Type:    common.MessageTypeHTTPRequest,
		UUID:    "saturated-uuid",
		Method:  "GET",
		URL:     "/busy",
		Headers: map[string][]string{"Host": {"test.example.com"}},
	}

	// dispatchHTTPRequest must not block the read loop when saturated.
	done := make(chan error, 1)
	go func() { done <- client.dispatchHTTPRequest(msg) }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("dispatchHTTPRequest() error = %v, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("dispatchHTTPRequest blocked while saturated; expected immediate 503")
	}

	serverConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var responseMsg common.Message
	if err := serverConn.ReadJSON(&responseMsg); err != nil {
		t.Fatalf("failed to read response: %v", err)
	}
	if responseMsg.UUID != "saturated-uuid" {
		t.Errorf("response UUID = %q, want %q", responseMsg.UUID, "saturated-uuid")
	}
	if responseMsg.Status != http.StatusServiceUnavailable {
		t.Errorf("response Status = %d, want %d", responseMsg.Status, http.StatusServiceUnavailable)
	}
	if string(responseMsg.Body) != http.StatusText(http.StatusServiceUnavailable) {
		t.Errorf("response Body = %q, want %q", string(responseMsg.Body), http.StatusText(http.StatusServiceUnavailable))
	}

	// A shed request never reaches the proxy, so it should not be logged.
	if logs := client.history.GetRecent(10); len(logs) != 0 {
		t.Errorf("history length = %d, want 0 for shed request", len(logs))
	}
}

func TestSetupMessageHandlers(t *testing.T) {
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer proxyServer.Close()

	client, _, wsServer, serverConn := newTestClient(t, proxyServer.URL)
	defer wsServer.Close()
	defer client.Conn.Close()
	defer serverConn.Close()

	client.setupMessageHandlers()

	if client.handlers == nil {
		t.Fatal("handlers are nil after setupMessageHandlers")
	}

	t.Run("http_request is registered", func(t *testing.T) {
		err := client.handlers.Dispatch(&common.Message{
			Type:    common.MessageTypeHTTPRequest,
			UUID:    "setup-test",
			Method:  "GET",
			URL:     "/test",
			Headers: map[string][]string{"Host": {"test.example.com"}},
		})
		if err != nil {
			t.Errorf("dispatch http_request failed: %v", err)
		}
		serverConn.SetReadDeadline(time.Now().Add(2 * time.Second))
		var resp common.Message
		serverConn.ReadJSON(&resp)
	})

	err := client.handlers.Dispatch(&common.Message{Type: "unknown_type"})
	if err == nil {
		t.Error("expected error for unknown message type, got nil")
	}
	if !contains(err.Error(), "unknown message type") {
		t.Errorf("error = %q, want containing %q", err.Error(), "unknown message type")
	}
}

// frozenDisplay stands in for a terminal that has stopped reading its output:
// Render blocks until released.
type frozenDisplay struct {
	entered chan struct{}
	release chan struct{}
}

func (d frozenDisplay) Render(Frame) {
	select {
	case d.entered <- struct{}{}:
	default:
	}
	<-d.release
}

// A stuck terminal must not hold up the tunnel: if request handling waited on
// the display, each request would keep its slot and, after
// MaxConcurrentRequests of them, every further request would get a 503.
func TestRequestsCompleteWhileTerminalIsFrozen(t *testing.T) {
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	}))
	defer proxyServer.Close()

	client, _, wsServer, serverConn := newTestClient(t, proxyServer.URL)
	defer wsServer.Close()
	defer client.Conn.Close()
	defer serverConn.Close()

	display := frozenDisplay{entered: make(chan struct{}, 1), release: make(chan struct{})}
	defer close(display.release)
	client.display = display
	client.requestSem = make(chan struct{}, MaxConcurrentRequests)

	go client.RunDisplay()
	client.requestRedraw()
	select {
	case <-display.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("display loop never started a frame")
	}

	for i := range 3 * MaxConcurrentRequests {
		msg := &common.Message{
			Type:    common.MessageTypeHTTPRequest,
			UUID:    fmt.Sprintf("frozen-%d", i),
			Method:  "GET",
			URL:     "/",
			Headers: map[string][]string{"Host": {"test.example.com"}},
		}
		if err := client.dispatchHTTPRequest(msg); err != nil {
			t.Fatalf("request %d: dispatchHTTPRequest() error = %v", i, err)
		}

		serverConn.SetReadDeadline(time.Now().Add(2 * time.Second))
		var resp common.Message
		if err := serverConn.ReadJSON(&resp); err != nil {
			t.Fatalf("request %d: failed to read response: %v", i, err)
		}
		if resp.Status != http.StatusOK {
			t.Fatalf("request %d: status = %d, want %d", i, resp.Status, http.StatusOK)
		}
	}

	if got := client.history.Total(); got != 3*MaxConcurrentRequests {
		t.Errorf("history total = %d, want %d", got, 3*MaxConcurrentRequests)
	}
}

// Reconnecting reports its progress through setStatus, which must not wait for
// a frame stuck on a frozen terminal either.
func TestSetStatusWhileTerminalIsFrozen(t *testing.T) {
	display := frozenDisplay{entered: make(chan struct{}, 1), release: make(chan struct{})}
	defer close(display.release)
	c := &Client{history: NewRequestHistory(10), display: display, redraw: make(chan struct{}, 1)}

	go c.RunDisplay()
	c.requestRedraw()
	select {
	case <-display.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("display loop never started a frame")
	}

	done := make(chan struct{})
	go func() {
		c.setStatus("reconnecting... (attempt 1)")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("setStatus blocked on the frozen terminal")
	}
}
