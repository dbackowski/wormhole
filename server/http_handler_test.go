package server

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

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

func TestIsClientRegistration(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		headers http.Header
		want    bool
	}{
		{
			name:    "marked upgrade on any path",
			path:    "/socket",
			headers: http.Header{"Connection": []string{"Upgrade"}, "Upgrade": []string{"websocket"}, common.ClientHeader: []string{"1"}},
			want:    true,
		},
		{
			name:    "marked upgrade with comma-listed connection tokens",
			path:    "/socket",
			headers: http.Header{"Connection": []string{"keep-alive, Upgrade"}, "Upgrade": []string{"WebSocket"}, common.ClientHeader: []string{"1"}},
			want:    true,
		},
		{
			name:    "unmarked upgrade on /ws is tunnel traffic",
			path:    "/ws",
			headers: http.Header{"Connection": []string{"Upgrade"}, "Upgrade": []string{"websocket"}},
			want:    false,
		},
		{
			name:    "unmarked upgrade elsewhere is tunnel traffic",
			path:    "/socket",
			headers: http.Header{"Connection": []string{"Upgrade"}, "Upgrade": []string{"websocket"}},
			want:    false,
		},
		{
			name:    "plain request to /ws is tunnel traffic",
			path:    "/ws",
			headers: http.Header{},
			want:    false,
		},
		{
			name:    "marker alone is not an upgrade",
			path:    "/ws",
			headers: http.Header{common.ClientHeader: []string{"1"}},
			want:    false,
		},
		{
			name:    "upgrade but not websocket",
			path:    "/ws",
			headers: http.Header{"Connection": []string{"Upgrade"}, "Upgrade": []string{"h2c"}},
			want:    false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, tc.path, nil)
			r.Header = tc.headers
			if got := isClientRegistration(r); got != tc.want {
				t.Errorf("isClientRegistration() = %v, want %v", got, tc.want)
			}
		})
	}
}

// A tunneled app may serve plain HTTP at /ws; the control plane must not eat it.
func TestRouteRequest_PlainWSPathIsTunneled(t *testing.T) {
	s := newTestServer(t)
	r := httptest.NewRequest(http.MethodGet, "/ws", nil)
	r.Host = "myapp.wormhole.tools"
	w := httptest.NewRecorder()

	s.routeRequest(w, r)

	if w.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want %d (tunneled, no client connected)", w.Code, http.StatusBadGateway)
	}
}

func TestTunnelRequest_UpgradeWithoutTunnel(t *testing.T) {
	s := newTestServer(t)
	r := httptest.NewRequest(http.MethodGet, "/socket", nil)
	r.Host = "myapp.wormhole.tools"
	r.Header.Set("Connection", "keep-alive, Upgrade")
	r.Header.Set("Upgrade", "websocket")
	w := httptest.NewRecorder()

	s.routeRequest(w, r)

	if w.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadGateway)
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

func TestPrepareRequestHeaders_ForwardedHostOverwritten(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Host = "foo.localhost"
	r.Header.Set("X-Forwarded-Host", "evil.example.com")

	headers := newTestServer(t).prepareRequestHeaders(r)

	if got := http.Header(headers)["X-Forwarded-Host"]; len(got) != 1 || got[0] != "foo.localhost" {
		t.Errorf("X-Forwarded-Host = %q, want [foo.localhost] (inbound value must not be trusted)", got)
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

	if w.Code != http.StatusGatewayTimeout {
		t.Errorf("status = %d, want %d", w.Code, http.StatusGatewayTimeout)
	}
	if body := w.Body.String(); body != "Gateway timeout" {
		t.Errorf("body = %q, want %q", body, "Gateway timeout")
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

	s.handleResponse(context.Background(), w, newConnection(nil, ""), ch)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestHandleResponse_ChannelClosed(t *testing.T) {
	s := newTestServer(t)
	w := httptest.NewRecorder()
	ch := make(chan *common.Message)
	close(ch)

	s.handleResponse(context.Background(), w, newConnection(nil, ""), ch)

	if w.Code != http.StatusGatewayTimeout {
		t.Errorf("status = %d, want %d", w.Code, http.StatusGatewayTimeout)
	}
}

func TestHandleResponse_ContextCancelled(t *testing.T) {
	s := newTestServer(t)
	w := httptest.NewRecorder()
	ch := make(chan *common.Message)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	s.handleResponse(ctx, w, newConnection(nil, ""), ch)

	if w.Code != http.StatusGatewayTimeout {
		t.Errorf("status = %d, want %d", w.Code, http.StatusGatewayTimeout)
	}
}

func TestHandleResponse_NilMessage(t *testing.T) {
	s := newTestServer(t)
	w := httptest.NewRecorder()
	ch := make(chan *common.Message, 1)
	ch <- nil

	s.handleResponse(context.Background(), w, newConnection(nil, ""), ch)

	if w.Code != http.StatusGatewayTimeout {
		t.Errorf("status = %d, want %d", w.Code, http.StatusGatewayTimeout)
	}
}

func TestForwardAndWaitForResponse_SendFails(t *testing.T) {
	s := newTestServer(t)
	ws, cleanup := newTestWSPair(t)
	ws.Close()
	cleanup()

	conn := newConnection(ws, "")
	msg := &common.Message{UUID: "u1", Type: common.MessageTypeHTTPRequest}
	w := httptest.NewRecorder()

	s.forwardAndWaitForResponse(context.Background(), w, conn, msg, "foo")

	if w.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadGateway)
	}
	if _, exists := conn.requests.pending["u1"]; exists {
		t.Error("pending request not cleaned up after send failure")
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

func TestTunnelRequest_BodyTooLarge(t *testing.T) {
	dialer, _, cleanup := newWSPair(t)
	defer cleanup()

	s := newTestServer(t)
	s.connManager.AddConnection("foo", "", dialer) //nolint:errcheck
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
	s.connManager.AddConnection("foo", "", dialer) //nolint:errcheck
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

func TestWriteSuccessResponse_InvalidStatus(t *testing.T) {
	for _, status := range []int{0, -1, 99, 1000, 99999} {
		t.Run("status_"+strconv.Itoa(status), func(t *testing.T) {
			w := httptest.NewRecorder()
			msg := &common.Message{
				Type:    common.MessageTypeHTTPResponse,
				Status:  status,
				Headers: map[string][]string{"Content-Length": {"5"}, "Content-Encoding": {"gzip"}},
				Body:    []byte("hello"),
			}

			newTestServer(t).writeSuccessResponse(w, msg)

			if w.Code != http.StatusBadGateway {
				t.Errorf("code = %d, want %d", w.Code, http.StatusBadGateway)
			}
			if got := w.Header().Get("Content-Encoding"); got != "" {
				t.Errorf("stale upstream header kept: Content-Encoding = %q", got)
			}
			if got := w.Header().Get("Content-Length"); got != "" {
				t.Errorf("stale upstream header kept: Content-Length = %q", got)
			}
			if body := w.Body.String(); body != "upstream returned invalid status" {
				t.Errorf("body = %q", body)
			}
		})
	}
}

func TestWriteSuccessResponse_ValidStatusBoundaries(t *testing.T) {
	for _, status := range []int{100, 200, 404, 599, 999} {
		w := httptest.NewRecorder()
		msg := &common.Message{Status: status, Body: []byte("ok")}

		newTestServer(t).writeSuccessResponse(w, msg)

		if w.Code != status {
			t.Errorf("code = %d, want %d", w.Code, status)
		}
		if body := w.Body.String(); body != "ok" {
			t.Errorf("status %d: body = %q, want %q", status, body, "ok")
		}
	}
}

func TestHandleResponse_ConnectionClosed(t *testing.T) {
	s := newTestServer(t)
	w := httptest.NewRecorder()
	conn := newConnection(nil, "")
	conn.closeOnce.Do(func() { close(conn.done) })

	done := make(chan time.Duration, 1)
	start := time.Now()
	go func() {
		s.handleResponse(context.Background(), w, conn, make(chan *common.Message))
		done <- time.Since(start)
	}()

	select {
	case d := <-done:
		if d > time.Second {
			t.Errorf("took %v, want immediate", d)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("handleResponse blocked after the tunnel closed")
	}

	if w.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadGateway)
	}
}

// A response delivered just before the tunnel closes must win over the 502.
func TestHandleResponse_ClosedConnectionPrefersBufferedResponse(t *testing.T) {
	s := newTestServer(t)
	conn := newConnection(nil, "")
	conn.closeOnce.Do(func() { close(conn.done) })

	for range 50 {
		w := httptest.NewRecorder()
		ch := make(chan *common.Message, 1)
		ch <- &common.Message{Status: http.StatusOK, Body: []byte("ok")}

		s.handleResponse(context.Background(), w, conn, ch)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d (buffered response lost to close race)", w.Code, http.StatusOK)
		}
	}
}

// End-to-end: a request in flight when the tunnel drops must fail fast with 502
// instead of parking until RequestTimeoutBuffer expires.
func TestForwardAndWaitForResponse_ClientDisconnects(t *testing.T) {
	dialer, acceptor, cleanup := newWSPair(t)
	defer cleanup()

	s := newTestServer(t)
	connection, _, err := s.connManager.AddConnection("foo", "", dialer)
	if err != nil {
		t.Fatalf("AddConnection() error = %v", err)
	}
	s.connManager.ActivateConnection("foo")

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Host = "foo.example.com"
	w := httptest.NewRecorder()

	done := make(chan time.Duration, 1)
	start := time.Now()
	go func() {
		msg := &common.Message{Type: common.MessageTypeHTTPRequest, UUID: "uuid-1", Method: http.MethodGet, URL: "/"}
		s.forwardAndWaitForResponse(r.Context(), w, connection, msg, "foo")
		done <- time.Since(start)
	}()

	time.Sleep(50 * time.Millisecond)

	// What the disconnect callback in handleWebSocketConnection does.
	acceptor.Close()
	s.connManager.RemoveConnection("foo", connection)
	connection.Close()

	select {
	case d := <-done:
		if d > 2*time.Second {
			t.Errorf("responded after %v, want immediate", d)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("still waiting after 5s (RequestTimeoutBuffer is %v)", common.RequestTimeoutBuffer)
	}

	if w.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadGateway)
	}
}
