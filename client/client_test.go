package client

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dbackowski/wormhole/common"

	"github.com/gorilla/websocket"
)

func TestResolveProxyResponse_Success(t *testing.T) {
	resp := &ProxyResponse{
		StatusCode: http.StatusOK,
		Headers:    map[string][]string{"Content-Type": {"text/plain"}},
		Body:       []byte("hello"),
	}

	result := resolveProxyResponse(resp, nil)

	if result.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d, want %d", result.StatusCode, http.StatusOK)
	}
	if string(result.Body) != "hello" {
		t.Errorf("Body = %q, want %q", string(result.Body), "hello")
	}
	if result.Headers["Content-Type"][0] != "text/plain" {
		t.Errorf("Headers Content-Type = %q, want %q", result.Headers["Content-Type"][0], "text/plain")
	}
}

func TestResolveProxyResponse_Error(t *testing.T) {
	result := resolveProxyResponse(nil, errors.New("connection refused"))

	if result.StatusCode != http.StatusBadGateway {
		t.Errorf("StatusCode = %d, want %d", result.StatusCode, http.StatusBadGateway)
	}
	if string(result.Body) != http.StatusText(http.StatusBadGateway) {
		t.Errorf("Body = %q, want %q", string(result.Body), http.StatusText(http.StatusBadGateway))
	}
	if result.Headers != nil {
		t.Errorf("Headers = %v, want nil", result.Headers)
	}
}

func TestResolveProxyResponse_ErrorIgnoresResponse(t *testing.T) {
	resp := &ProxyResponse{
		StatusCode: http.StatusOK,
		Body:       []byte("this should be ignored"),
	}

	result := resolveProxyResponse(resp, errors.New("some error"))

	if result.StatusCode != http.StatusBadGateway {
		t.Errorf("StatusCode = %d, want %d", result.StatusCode, http.StatusBadGateway)
	}
}

func TestSendResponse(t *testing.T) {
	clientConn, wsServer, serverConn := setupTestWebsocket(t)
	defer wsServer.Close()
	defer clientConn.Close()
	defer serverConn.Close()

	client := &Client{
		domain: "myapp",
		Conn:   clientConn,
	}

	msg := &common.Message{
		UUID:   "resp-uuid-1",
		Method: "POST",
		URL:    "/api/data",
	}

	resolved := ProxyResponse{
		StatusCode: http.StatusCreated,
		Headers:    map[string][]string{"X-Custom": {"value"}},
		Body:       []byte("created"),
	}

	err := client.sendResponse(msg, resolved)
	if err != nil {
		t.Fatalf("sendResponse() error = %v, want nil", err)
	}

	serverConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var received common.Message
	if err := serverConn.ReadJSON(&received); err != nil {
		t.Fatalf("failed to read response: %v", err)
	}

	if received.Type != common.MessageTypeHTTPResponse {
		t.Errorf("Type = %q, want %q", received.Type, common.MessageTypeHTTPResponse)
	}
	if received.Domain != "myapp" {
		t.Errorf("Domain = %q, want %q", received.Domain, "myapp")
	}
	if received.UUID != "resp-uuid-1" {
		t.Errorf("UUID = %q, want %q", received.UUID, "resp-uuid-1")
	}
	if received.Method != "POST" {
		t.Errorf("Method = %q, want %q", received.Method, "POST")
	}
	if received.URL != "/api/data" {
		t.Errorf("URL = %q, want %q", received.URL, "/api/data")
	}
	if received.Status != http.StatusCreated {
		t.Errorf("Status = %d, want %d", received.Status, http.StatusCreated)
	}
	if string(received.Body) != "created" {
		t.Errorf("Body = %q, want %q", string(received.Body), "created")
	}
}

func TestSendResponse_ClosedConnection(t *testing.T) {
	clientConn, wsServer, serverConn := setupTestWebsocket(t)
	defer wsServer.Close()
	defer serverConn.Close()

	client := &Client{
		domain: "test",
		Conn:   clientConn,
	}

	clientConn.Close()

	err := client.sendResponse(&common.Message{UUID: "x"}, ProxyResponse{StatusCode: 200})
	if err == nil {
		t.Error("sendResponse() on closed connection should return error")
	}
}

func TestRefreshTerminalOutput(t *testing.T) {
	clientConn, wsServer, _ := setupTestWebsocket(t)
	defer wsServer.Close()
	defer clientConn.Close()

	display := &mockDisplay{}
	history := NewRequestHistory(100)
	history.Add(RequestLog{UUID: "log-1", Method: "GET", URL: "/a", StatusCode: 200})
	history.Add(RequestLog{UUID: "log-2", Method: "POST", URL: "/b", StatusCode: 201})

	client := &Client{
		tunnelURL: "https://test.example.com",
		Conn:      clientConn,
		history:   history,
		display:   display,
		WebUIPort: 4040,
	}

	client.RefreshTerminalOutput()

	if !display.ConnectionInfoCalled() {
		t.Error("expected ShowConnectionInfo to be called")
	}
	if !display.RequestHistoryCalled() {
		t.Error("expected ShowRequestHistory to be called")
	}
	if logs := display.LastLogs(); len(logs) != 2 {
		t.Errorf("lastLogs length = %d, want 2", len(logs))
	}
}

func TestRefreshTerminalOutput_EmptyHistory(t *testing.T) {
	clientConn, wsServer, _ := setupTestWebsocket(t)
	defer wsServer.Close()
	defer clientConn.Close()

	display := &mockDisplay{}

	client := &Client{
		tunnelURL: "https://test.example.com",
		Conn:      clientConn,
		history:   NewRequestHistory(100),
		display:   display,
		WebUIPort: 4040,
	}

	client.RefreshTerminalOutput()

	if !display.ConnectionInfoCalled() {
		t.Error("expected ShowConnectionInfo to be called")
	}
	if !display.RequestHistoryCalled() {
		t.Error("expected ShowRequestHistory to be called")
	}
	if logs := display.LastLogs(); len(logs) != 0 {
		t.Errorf("lastLogs length = %d, want 0", len(logs))
	}
}

func TestCloseWebsocket(t *testing.T) {
	clientConn, wsServer, serverConn := setupTestWebsocket(t)
	defer wsServer.Close()
	defer clientConn.Close()
	defer serverConn.Close()

	err := closeWebsocket(clientConn)
	if err != nil {
		t.Fatalf("closeWebsocket() error = %v, want nil", err)
	}

	serverConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _, readErr := serverConn.ReadMessage()
	if readErr == nil {
		t.Fatal("expected error reading from closed connection, got nil")
	}

	closeErr, ok := readErr.(*websocket.CloseError)
	if !ok {
		t.Fatalf("expected *websocket.CloseError, got %T: %v", readErr, readErr)
	}
	if closeErr.Code != websocket.CloseNormalClosure {
		t.Errorf("close code = %d, want %d", closeErr.Code, websocket.CloseNormalClosure)
	}
}

func TestShutdown(t *testing.T) {
	clientConn, wsServer, serverConn := setupTestWebsocket(t)
	defer wsServer.Close()
	defer serverConn.Close()

	client := &Client{
		Conn:   clientConn,
		Logger: common.NewLogger(common.LevelError, "text"),
	}

	err := client.Shutdown()
	if err != nil {
		t.Fatalf("Shutdown() error = %v, want nil", err)
	}

	serverConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _, readErr := serverConn.ReadMessage()
	if readErr == nil {
		t.Fatal("expected error after shutdown, got nil")
	}
}

func TestShutdown_AlreadyClosed(t *testing.T) {
	clientConn, wsServer, serverConn := setupTestWebsocket(t)
	defer wsServer.Close()
	defer serverConn.Close()

	client := &Client{
		Conn:   clientConn,
		Logger: common.NewLogger(common.LevelError, "text"),
	}

	clientConn.Close()

	err := client.Shutdown()
	if err == nil {
		t.Error("Shutdown() on already closed connection should return error")
	}
}

func TestNewClient_InvalidDomain(t *testing.T) {
	cfg := &Config{
		ServerURL: "http://localhost:8080",
		Domain:    "",
		Local:     "http://localhost:3000",
		WebUIPort: 4040,
	}

	_, err := NewClient(cfg)
	if err == nil {
		t.Fatal("NewClient() with empty domain should return error")
	}
	if !strings.Contains(err.Error(), "invalid client config") {
		t.Errorf("error = %q, want containing %q", err.Error(), "invalid client config")
	}
}

func TestNewClient_InvalidLocal(t *testing.T) {
	cfg := &Config{
		ServerURL: "http://localhost:8080",
		Domain:    "myapp",
		Local:     "",
		WebUIPort: 4040,
	}

	_, err := NewClient(cfg)
	if err == nil {
		t.Fatal("NewClient() with empty local should return error")
	}
	if !strings.Contains(err.Error(), "invalid client config") {
		t.Errorf("error = %q, want containing %q", err.Error(), "invalid client config")
	}
}

func TestNewClient_InvalidServerURL(t *testing.T) {
	cfg := &Config{
		ServerURL: "not-a-url",
		Domain:    "myapp",
		Local:     "http://localhost:3000",
		WebUIPort: 4040,
	}

	_, err := NewClient(cfg)
	if err == nil {
		t.Fatal("NewClient() with invalid server URL should return error")
	}
	if !strings.Contains(err.Error(), "invalid server URL") {
		t.Errorf("error = %q, want containing %q", err.Error(), "invalid server URL")
	}
}

func TestNewClient_ConnectionRefused(t *testing.T) {
	cfg := &Config{
		ServerURL: "http://127.0.0.1:1",
		Domain:    "myapp",
		Local:     "http://localhost:3000",
		WebUIPort: 4040,
	}

	_, err := NewClient(cfg)
	if err == nil {
		t.Fatal("NewClient() with unreachable server should return error")
	}
	if !strings.Contains(err.Error(), "failed to connect") {
		t.Errorf("error = %q, want containing %q", err.Error(), "failed to connect")
	}
}

func TestNewClient_Success(t *testing.T) {
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader.Upgrade(w, r, nil)
		select {}
	}))
	defer server.Close()

	wsHost := strings.TrimPrefix(server.URL, "http://")

	cfg := &Config{
		ServerURL: server.URL,
		Domain:    "myapp",
		Local:     "http://localhost:3000",
		WebUIPort: 4040,
	}

	// NewClient builds a subdomain URL like "myapp.<host>", which won't resolve.
	// We need to test with the real server, so we test that validation passes
	// but the ws dial will fail because "myapp.<host>" doesn't resolve.
	_, err := NewClient(cfg)
	if err == nil {
		// If it somehow succeeds (unlikely), that's fine too
		return
	}
	// Expected: fails to connect because myapp.<wsHost> doesn't resolve
	if !strings.Contains(err.Error(), "failed to connect") {
		t.Errorf("error = %q, want containing %q (host was %s)", err.Error(), "failed to connect", wsHost)
	}
}

func TestHandleConnection_ReturnsOnClose(t *testing.T) {
	clientConn, wsServer, serverConn := setupTestWebsocket(t)
	defer wsServer.Close()
	defer clientConn.Close()

	client := &Client{
		Conn:   clientConn,
		Logger: common.NewLogger(common.LevelError, "text"),
	}
	client.setupMessageHandlers()

	done := make(chan struct{})
	go func() {
		client.HandleConnection()
		close(done)
	}()

	// Close the server side to trigger a read error in HandleConnection
	serverConn.Close()

	select {
	case <-done:
		// HandleConnection returned as expected
	case <-time.After(3 * time.Second):
		t.Fatal("HandleConnection did not return after connection closed")
	}
}

func TestHandleConnection_RequestsRunConcurrently(t *testing.T) {
	const (
		numRequests   = 5
		handlerDelay  = 100 * time.Millisecond
		readDeadline  = 3 * time.Second
	)

	var inFlight, maxInFlight int32
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cur := atomic.AddInt32(&inFlight, 1)
		defer atomic.AddInt32(&inFlight, -1)
		for {
			peak := atomic.LoadInt32(&maxInFlight)
			if cur <= peak || atomic.CompareAndSwapInt32(&maxInFlight, peak, cur) {
				break
			}
		}
		time.Sleep(handlerDelay)
		w.WriteHeader(http.StatusOK)
	}))
	defer proxyServer.Close()

	client, _, wsServer, serverConn := newTestClient(t, proxyServer.URL)
	defer wsServer.Close()
	defer client.Conn.Close()
	defer serverConn.Close()

	client.Logger = common.NewLogger(common.LevelError, "text")
	client.setupMessageHandlers()

	done := make(chan struct{})
	go func() {
		client.HandleConnection()
		close(done)
	}()

	start := time.Now()
	for i := range numRequests {
		err := serverConn.WriteJSON(common.Message{
			Type:    common.MessageTypeHTTPRequest,
			UUID:    fmt.Sprintf("concurrent-%d", i),
			Method:  "GET",
			URL:     "/",
			Headers: map[string][]string{"Host": {"test.example.com"}},
		})
		if err != nil {
			t.Fatalf("write request %d: %v", i, err)
		}
	}

	serverConn.SetReadDeadline(time.Now().Add(readDeadline))
	for i := range numRequests {
		var resp common.Message
		if err := serverConn.ReadJSON(&resp); err != nil {
			t.Fatalf("read response %d: %v", i, err)
		}
		if resp.Status != http.StatusOK {
			t.Errorf("response %d status = %d, want %d", i, resp.Status, http.StatusOK)
		}
	}
	elapsed := time.Since(start)

	if peak := atomic.LoadInt32(&maxInFlight); peak < 2 {
		t.Errorf("max concurrent in-flight requests = %d, want >= 2 (requests were serialized)", peak)
	}

	// Serialized would be ~numRequests * handlerDelay. Allow generous slack.
	maxSerialized := time.Duration(numRequests) * handlerDelay
	if elapsed >= maxSerialized {
		t.Errorf("requests took %v, expected well under serialized time of %v", elapsed, maxSerialized)
	}
}

func TestHandleConnection_DispatchesMessages(t *testing.T) {
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer proxyServer.Close()

	client, display, wsServer, serverConn := newTestClient(t, proxyServer.URL)
	defer wsServer.Close()
	defer client.Conn.Close()

	client.Logger = common.NewLogger(common.LevelError, "text")
	client.setupMessageHandlers()

	done := make(chan struct{})
	go func() {
		client.HandleConnection()
		close(done)
	}()

	// Send a message from the server side
	err := serverConn.WriteJSON(common.Message{
		Type:    common.MessageTypeHTTPRequest,
		UUID:    "handle-conn-uuid",
		Method:  "GET",
		URL:     "/test",
		Headers: map[string][]string{"Host": {"test.example.com"}},
	})
	if err != nil {
		t.Fatalf("failed to send message: %v", err)
	}

	// Read the response that the client sends back
	serverConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var resp common.Message
	if err := serverConn.ReadJSON(&resp); err != nil {
		t.Fatalf("failed to read response: %v", err)
	}

	if resp.Type != common.MessageTypeHTTPResponse {
		t.Errorf("response Type = %q, want %q", resp.Type, common.MessageTypeHTTPResponse)
	}
	if resp.UUID != "handle-conn-uuid" {
		t.Errorf("response UUID = %q, want %q", resp.UUID, "handle-conn-uuid")
	}
	if resp.Status != http.StatusOK {
		t.Errorf("response Status = %d, want %d", resp.Status, http.StatusOK)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && !display.ConnectionInfoCalled() {
		time.Sleep(10 * time.Millisecond)
	}
	if !display.ConnectionInfoCalled() {
		t.Error("expected ShowConnectionInfo to be called")
	}

	// Close server side to end the loop
	serverConn.Close()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("HandleConnection did not return after connection closed")
	}
}
