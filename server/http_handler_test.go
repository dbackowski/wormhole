package server

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dbackowski/wormhole/common"
	"github.com/gorilla/websocket"
)

func newWSPair(t *testing.T) (dialerConn *websocket.Conn, acceptorConn *websocket.Conn, cleanup func()) {
	t.Helper()
	u := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	ch := make(chan *websocket.Conn, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := u.Upgrade(w, r, nil)
		if err == nil {
			ch <- c
		}
	}))
	url := "ws" + strings.TrimPrefix(srv.URL, "http")
	dialer, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		srv.Close()
		t.Fatalf("dial websocket: %v", err)
	}
	acceptor := <-ch
	return dialer, acceptor, func() { dialer.Close(); acceptor.Close(); srv.Close() }
}

func TestIsWebSocketUpgradeRequest(t *testing.T) {
	tests := []struct {
		name    string
		headers http.Header
		want    bool
	}{
		{
			name:    "ws upgrade",
			headers: http.Header{"Connection": []string{"upgrade"}, "Upgrade": []string{"websocket"}},
			want:    true,
		},
		{
			name:    "ws upgrade case insensitive",
			headers: http.Header{"Connection": []string{"Upgrade"}, "Upgrade": []string{"WebSocket"}},
			want:    true,
		},
		{
			name:    "no upgrade header",
			headers: http.Header{},
			want:    false,
		},
		{
			name:    "upgrade but not websocket",
			headers: http.Header{"Connection": []string{"upgrade"}, "Upgrade": []string{"h2c"}},
			want:    false,
		},
		{
			name:    "websocket but no connection upgrade",
			headers: http.Header{"Upgrade": []string{"websocket"}},
			want:    false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isWebSocketUpgradeRequest(tc.headers); got != tc.want {
				t.Errorf("isWebSocketUpgradeRequest() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestPrepareRequestHeaders(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Host = "foo.localhost"
	r.Header.Set("X-Custom", "value")
	r.Header.Set("Accept", "application/json")

	headers := newTestServer(t).prepareRequestHeaders(r)

	if got := headers["Host"]; len(got) != 1 || got[0] != "foo.localhost" {
		t.Errorf("Host = %v, want [foo.localhost]", got)
	}
	if got := headers["X-Custom"]; len(got) != 1 || got[0] != "value" {
		t.Errorf("X-Custom = %v, want [value]", got)
	}
	if got := headers["Accept"]; len(got) != 1 || got[0] != "application/json" {
		t.Errorf("Accept = %v, want [application/json]", got)
	}
}

func TestPrepareRequestHeaders_StripsHopByHop(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Host = "foo.localhost"
	r.Header.Set("Connection", "keep-alive, X-Hop")
	r.Header.Set("X-Hop", "drop me")
	r.Header.Set("Keep-Alive", "timeout=5")
	r.Header.Set("X-Custom", "keep me")

	headers := newTestServer(t).prepareRequestHeaders(r)

	for _, k := range []string{"Connection", "X-Hop", "Keep-Alive"} {
		if _, ok := headers[http.CanonicalHeaderKey(k)]; ok {
			t.Errorf("hop-by-hop header %q should have been stripped", k)
		}
	}
	if got := headers["X-Custom"]; len(got) != 1 || got[0] != "keep me" {
		t.Errorf("X-Custom = %v, want [keep me]", got)
	}
	if got := headers["Host"]; len(got) != 1 || got[0] != "foo.localhost" {
		t.Errorf("Host = %v, want [foo.localhost]", got)
	}
}

func TestPrepareRequestHeaders_EmptyHeadersExcluded(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header["X-Empty"] = []string{}

	headers := newTestServer(t).prepareRequestHeaders(r)

	if _, ok := headers["X-Empty"]; ok {
		t.Error("expected empty-value header to be excluded")
	}
}

func TestPrepareRequestHeaders_ForwardedFor(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Host = "foo.localhost"
	r.RemoteAddr = "203.0.113.7:54321"

	headers := newTestServer(t).prepareRequestHeaders(r)

	if got := http.Header(headers).Get("X-Forwarded-For"); got != "203.0.113.7" {
		t.Errorf("X-Forwarded-For = %q, want %q", got, "203.0.113.7")
	}
}

func TestPrepareRequestHeaders_ForwardedForAppends(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Host = "foo.localhost"
	r.RemoteAddr = "10.0.0.1:1000"
	r.Header.Set("X-Forwarded-For", "203.0.113.7")

	headers := newTestServer(t).prepareRequestHeaders(r)

	if got := http.Header(headers).Get("X-Forwarded-For"); got != "203.0.113.7, 10.0.0.1" {
		t.Errorf("X-Forwarded-For = %q, want %q", got, "203.0.113.7, 10.0.0.1")
	}
}

func TestPrepareRequestHeaders_ForwardedHost(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Host = "foo.localhost"

	headers := newTestServer(t).prepareRequestHeaders(r)

	if got := http.Header(headers).Get("X-Forwarded-Host"); got != "foo.localhost" {
		t.Errorf("X-Forwarded-Host = %q, want %q", got, "foo.localhost")
	}
}

func TestPrepareRequestHeaders_ForwardedHostPreserved(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Host = "foo.localhost"
	r.Header.Set("X-Forwarded-Host", "public.example.com")

	headers := newTestServer(t).prepareRequestHeaders(r)

	if got := http.Header(headers).Get("X-Forwarded-Host"); got != "public.example.com" {
		t.Errorf("X-Forwarded-Host = %q, want %q (inbound value must be preserved)", got, "public.example.com")
	}
}

func TestPrepareRequestHeaders_ForwardedProtoDefaultsHTTP(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Host = "foo.localhost"

	headers := newTestServer(t).prepareRequestHeaders(r)

	if got := http.Header(headers).Get("X-Forwarded-Proto"); got != "http" {
		t.Errorf("X-Forwarded-Proto = %q, want %q (no -host, no TLS)", got, "http")
	}
}

func TestPrepareRequestHeaders_ForwardedProtoHTTPSWhenHostSet(t *testing.T) {
	s, err := NewServer(&Config{Port: 9999, Host: "wormhole.tools"})
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Host = "foo.wormhole.tools"

	headers := s.prepareRequestHeaders(r)

	if got := http.Header(headers).Get("X-Forwarded-Proto"); got != "https" {
		t.Errorf("X-Forwarded-Proto = %q, want %q (-host set implies HTTPS front)", got, "https")
	}
}

func TestPrepareRequestHeaders_ForwardedProtoPreserved(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Host = "foo.localhost"
	r.Header.Set("X-Forwarded-Proto", "https")

	headers := newTestServer(t).prepareRequestHeaders(r)

	if got := http.Header(headers).Get("X-Forwarded-Proto"); got != "https" {
		t.Errorf("X-Forwarded-Proto = %q, want %q (inbound value must be preserved)", got, "https")
	}
}

func TestBuildRequestMessage_Normal(t *testing.T) {
	s := newTestServer(t)
	body := []byte("hello body")
	r := httptest.NewRequest(http.MethodPost, "/path?q=1", bytes.NewReader(body))
	r.Host = "foo.localhost"
	w := httptest.NewRecorder()

	msg, err := s.buildRequestMessage(w, r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg.Type != common.MessageTypeHTTPRequest {
		t.Errorf("Type = %q, want %q", msg.Type, common.MessageTypeHTTPRequest)
	}
	if msg.Method != http.MethodPost {
		t.Errorf("Method = %q, want %q", msg.Method, http.MethodPost)
	}
	if msg.URL != "/path?q=1" {
		t.Errorf("URL = %q, want %q", msg.URL, "/path?q=1")
	}
	if string(msg.Body) != "hello body" {
		t.Errorf("Body = %q, want %q", msg.Body, "hello body")
	}
	if msg.UUID == "" {
		t.Error("expected non-empty UUID")
	}
}

func TestBuildRequestMessage_WebSocketUpgrade(t *testing.T) {
	s := newTestServer(t)
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Connection", "upgrade")
	r.Header.Set("Upgrade", "websocket")
	w := httptest.NewRecorder()

	_, err := s.buildRequestMessage(w, r)
	if err == nil {
		t.Error("expected error for WebSocket upgrade request, got nil")
	}
}

func TestWriteResponse(t *testing.T) {
	s := newTestServer(t)
	w := httptest.NewRecorder()

	s.writeResponse(w, http.StatusCreated, []byte("created"))

	if w.Code != http.StatusCreated {
		t.Errorf("status = %d, want %d", w.Code, http.StatusCreated)
	}
	if body := w.Body.String(); body != "created" {
		t.Errorf("body = %q, want %q", body, "created")
	}
}

func TestWriteTimeoutResponse(t *testing.T) {
	s := newTestServer(t)
	w := httptest.NewRecorder()

	s.writeTimeoutResponse(w)

	if w.Code != http.StatusRequestTimeout {
		t.Errorf("status = %d, want %d", w.Code, http.StatusRequestTimeout)
	}
	if body := w.Body.String(); body != "Request timeout" {
		t.Errorf("body = %q, want %q", body, "Request timeout")
	}
}

func TestWriteSuccessResponse(t *testing.T) {
	s := newTestServer(t)
	w := httptest.NewRecorder()
	msg := &common.Message{
		Status:  http.StatusOK,
		Body:    []byte("success"),
		Headers: http.Header{"Content-Type": []string{"text/plain"}},
	}

	s.writeSuccessResponse(w, msg)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if body := w.Body.String(); body != "success" {
		t.Errorf("body = %q, want %q", body, "success")
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/plain" {
		t.Errorf("Content-Type = %q, want %q", ct, "text/plain")
	}
}

func TestHandleResponse_MessageReceived(t *testing.T) {
	s := newTestServer(t)
	w := httptest.NewRecorder()
	ch := make(chan *common.Message, 1)
	ch <- &common.Message{Status: http.StatusOK, Body: []byte("ok")}

	s.handleResponse(context.Background(), w, ch)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestHandleResponse_ChannelClosed(t *testing.T) {
	s := newTestServer(t)
	w := httptest.NewRecorder()
	ch := make(chan *common.Message)
	close(ch)

	s.handleResponse(context.Background(), w, ch)

	if w.Code != http.StatusRequestTimeout {
		t.Errorf("status = %d, want %d", w.Code, http.StatusRequestTimeout)
	}
}

func TestHandleResponse_ContextCancelled(t *testing.T) {
	s := newTestServer(t)
	w := httptest.NewRecorder()
	ch := make(chan *common.Message)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	s.handleResponse(ctx, w, ch)

	if w.Code != http.StatusRequestTimeout {
		t.Errorf("status = %d, want %d", w.Code, http.StatusRequestTimeout)
	}
}

func TestHandleResponse_NilMessage(t *testing.T) {
	s := newTestServer(t)
	w := httptest.NewRecorder()
	ch := make(chan *common.Message, 1)
	ch <- nil

	s.handleResponse(context.Background(), w, ch)

	if w.Code != http.StatusRequestTimeout {
		t.Errorf("status = %d, want %d", w.Code, http.StatusRequestTimeout)
	}
}

func TestRegisterAndForwardRequest_SendFails(t *testing.T) {
	s := newTestServer(t)
	ws, cleanup := newTestWSPair(t)
	ws.Close()
	cleanup()

	conn := &Connection{conn: ws, requests: NewPendingRequests()}
	msg := &common.Message{UUID: "u1", Type: common.MessageTypeHTTPRequest}

	_, _, err := s.registerAndForwardRequest(context.Background(), conn, msg)
	if err == nil {
		t.Error("expected error when send fails, got nil")
	}
}

func TestTunnelRequest_InvalidHost(t *testing.T) {
	s := newTestServer(t)
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Host = "localhost"
	w := httptest.NewRecorder()

	s.tunnelRequest(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestTunnelRequest_DomainNotFound(t *testing.T) {
	s := newTestServer(t)
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Host = "unknown.localhost"
	w := httptest.NewRecorder()

	s.tunnelRequest(w, r)

	if w.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadGateway)
	}
}

func TestTunnelRequest_WebSocketUpgradeRejected(t *testing.T) {
	dialer, _, cleanup := newWSPair(t)
	defer cleanup()

	s := newTestServer(t)
	s.connManager.AddConnection("foo", dialer) //nolint:errcheck
	s.connManager.ActivateConnection("foo")

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Host = "foo.localhost"
	r.Header.Set("Connection", "upgrade")
	r.Header.Set("Upgrade", "websocket")
	w := httptest.NewRecorder()

	s.tunnelRequest(w, r)

	if w.Code != http.StatusNotImplemented {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotImplemented)
	}
}

func TestTunnelRequest_BodyTooLarge(t *testing.T) {
	dialer, _, cleanup := newWSPair(t)
	defer cleanup()

	s := newTestServer(t)
	s.connManager.AddConnection("foo", dialer) //nolint:errcheck
	s.connManager.ActivateConnection("foo")

	body := bytes.Repeat([]byte("a"), common.MaxRequestBodySize+1)
	r := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	r.Host = "foo.localhost"
	w := httptest.NewRecorder()

	s.tunnelRequest(w, r)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want %d", w.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestTunnelRequest_Success(t *testing.T) {
	dialer, acceptor, cleanup := newWSPair(t)
	defer cleanup()

	s := newTestServer(t)
	s.connManager.AddConnection("foo", dialer) //nolint:errcheck
	s.connManager.ActivateConnection("foo")

	go func() {
		var req common.Message
		if err := acceptor.ReadJSON(&req); err != nil {
			return
		}
		conn, _ := s.connManager.GetConnection("foo")
		conn.DeliverResponse(&common.Message{ //nolint:errcheck
			UUID:   req.UUID,
			Status: http.StatusOK,
			Body:   []byte("proxied response"),
		})
	}()

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Host = "foo.localhost"
	w := httptest.NewRecorder()

	s.tunnelRequest(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if body := w.Body.String(); body != "proxied response" {
		t.Errorf("body = %q, want %q", body, "proxied response")
	}
}
