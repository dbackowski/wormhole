package server

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"testing/iotest"
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

func TestPrepareRequestHeaders_BodyFraming(t *testing.T) {
	s := newTestServer(t)
	for _, tc := range []struct {
		name          string
		contentLength int64
		wantLength    string
		wantEncoding  string
	}{
		{"no body", 0, "", ""},
		{"known length", 5, "5", ""},
		{"chunked", -1, "", "chunked"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/", nil)
			r.Host = "foo.localhost"
			r.ContentLength = tc.contentLength
			r.Header.Set("Content-Length", "999") // whatever the browser claimed
			h := http.Header(s.prepareRequestHeaders(r))

			if got := h.Get("Content-Length"); got != tc.wantLength {
				t.Errorf("Content-Length = %q, want %q", got, tc.wantLength)
			}
			if got := h.Get("Transfer-Encoding"); got != tc.wantEncoding {
				t.Errorf("Transfer-Encoding = %q, want %q", got, tc.wantEncoding)
			}
		})
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

func writeStreamed(s *Server, w *httptest.ResponseRecorder, msg *common.Message, body io.Reader) bool {
	return s.writeStreamedResponse(w, http.NewResponseController(w), msg, body)
}

func TestWriteStreamedResponse(t *testing.T) {
	s := newTestServer(t)
	w := httptest.NewRecorder()
	msg := &common.Message{
		Status:  http.StatusOK,
		Headers: http.Header{"Content-Type": []string{"text/plain"}},
	}

	if !writeStreamed(s, w, msg, strings.NewReader("success")) {
		t.Error("writeStreamedResponse() = false for a complete body")
	}

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if body := w.Body.String(); body != "success" {
		t.Errorf("body = %q, want %q", body, "success")
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/plain" {
		t.Errorf("Content-Type = %q, want %q", ct, "text/plain")
	}
	if !w.Flushed {
		t.Error("response not flushed")
	}
}

// A cut body must abort the response: finishing it normally would end a
// chunked response cleanly, and the browser would take the part for the whole.
func TestWriteStreamedResponse_CutBodyAborts(t *testing.T) {
	s := newTestServer(t)
	w := httptest.NewRecorder()
	body := io.MultiReader(strings.NewReader("part"), iotest.ErrReader(common.ErrStreamReset))

	defer func() {
		if r := recover(); r != http.ErrAbortHandler {
			t.Errorf("recovered %v, want http.ErrAbortHandler", r)
		}
		if got := w.Body.String(); got != "part" {
			t.Errorf("body = %q, want %q", got, "part")
		}
	}()
	writeStreamed(s, w, &common.Message{Status: http.StatusOK}, body)
}

func never() bool { return false }

func bodySentNow() <-chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}

func TestAwaitResponse_MessageReceived(t *testing.T) {
	s := newTestServer(t)
	w := httptest.NewRecorder()
	ch := make(chan *common.Message, 1)
	want := &common.Message{Status: http.StatusOK}
	ch <- want

	if got := s.awaitResponse(context.Background(), w, newConnection(nil, ""), ch, bodySentNow(), never); got != want {
		t.Errorf("awaitResponse() = %v, want %v", got, want)
	}
}

func TestAwaitResponse_ContextCancelled(t *testing.T) {
	s := newTestServer(t)
	w := httptest.NewRecorder()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if s.awaitResponse(ctx, w, newConnection(nil, ""), make(chan *common.Message), bodySentNow(), never) != nil {
		t.Error("awaitResponse() returned a response")
	}
	if w.Code != http.StatusGatewayTimeout {
		t.Errorf("status = %d, want %d", w.Code, http.StatusGatewayTimeout)
	}
}

// The local app cannot answer before it has the body, so a long upload must
// not use up the time it gets to respond.
func TestAwaitResponse_TimeoutStartsOnceBodyIsSent(t *testing.T) {
	s := newTestServer(t)
	s.responseTimeout = 50 * time.Millisecond
	w := httptest.NewRecorder()
	bodySent := make(chan struct{})
	ch := make(chan *common.Message, 1)

	go func() {
		time.Sleep(4 * s.responseTimeout) // still uploading
		ch <- &common.Message{Status: http.StatusOK}
	}()
	if s.awaitResponse(context.Background(), w, newConnection(nil, ""), ch, bodySent, never) == nil {
		t.Fatalf("timed out during the upload with %d", w.Code)
	}

	close(bodySent)
	start := time.Now()
	if s.awaitResponse(context.Background(), w, newConnection(nil, ""), make(chan *common.Message), bodySent, never) != nil {
		t.Fatal("awaitResponse() returned a response")
	}
	if w.Code != http.StatusGatewayTimeout || time.Since(start) < s.responseTimeout {
		t.Errorf("status = %d after %v, want %d after %v", w.Code, time.Since(start), http.StatusGatewayTimeout, s.responseTimeout)
	}
}

func TestTunnelHTTP_SendFails(t *testing.T) {
	s := newTestServer(t)
	ws, cleanup := newTestWSPair(t)
	ws.Close()
	cleanup()

	conn := newConnection(ws, "")
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Host = "foo.localhost"
	w := httptest.NewRecorder()

	s.tunnelHTTP(w, r, conn, "foo")

	if w.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadGateway)
	}
	if conn.requests.Len() != 0 || conn.bodies.Count() != 0 {
		t.Error("request or stream not cleaned up after send failure")
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

// playClient answers the next request on the tunnel the way a client does:
// headers, then the body on the request's stream. It returns the messages the
// server sent for the request.
func playClient(t *testing.T, s *Server, acceptor *websocket.Conn, domain, body string) <-chan []common.Message {
	t.Helper()
	got := make(chan []common.Message, 1)
	go func() {
		var msgs []common.Message
		for {
			var m common.Message
			if err := acceptor.ReadJSON(&m); err != nil {
				got <- msgs
				return
			}
			msgs = append(msgs, m)
			if m.Type == common.MessageTypeStreamClose {
				break
			}
		}
		conn, _ := s.connManager.GetConnection(domain)
		id := msgs[0].UUID
		conn.requests.Deliver(&common.Message{UUID: id, Status: http.StatusOK}) //nolint:errcheck
		conn.bodies.Deliver(id, []byte(body))
		conn.bodies.Finish(id)
		got <- msgs
	}()
	return got
}

func TestTunnelRequest_Success(t *testing.T) {
	dialer, acceptor, cleanup := newWSPair(t)
	defer cleanup()

	s := newTestServer(t)
	s.connManager.AddConnection("foo", "", dialer) //nolint:errcheck
	s.connManager.ActivateConnection("foo")
	sent := playClient(t, s, acceptor, "foo", "proxied response")

	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("uploaded"))
	r.Host = "foo.localhost"
	w := httptest.NewRecorder()

	s.tunnelRequest(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if body := w.Body.String(); body != "proxied response" {
		t.Errorf("body = %q, want %q", body, "proxied response")
	}

	msgs := <-sent
	if len(msgs) != 3 || msgs[0].Type != common.MessageTypeHTTPRequest || len(msgs[0].Body) != 0 ||
		msgs[1].Type != common.MessageTypeStreamData || string(msgs[1].Body) != "uploaded" {
		t.Errorf("sent %+v, want request headers, then the body as stream_data and stream_close", msgs)
	}
	conn, _ := s.connManager.GetConnection("foo")
	if conn.bodies.Count() != 0 {
		t.Errorf("%d streams left open", conn.bodies.Count())
	}
}

func TestWriteStreamedResponse_InvalidStatus(t *testing.T) {
	for _, status := range []int{0, -1, 99, 1000, 99999} {
		t.Run("status_"+strconv.Itoa(status), func(t *testing.T) {
			w := httptest.NewRecorder()
			msg := &common.Message{
				Type:    common.MessageTypeHTTPResponse,
				Status:  status,
				Headers: map[string][]string{"Content-Length": {"5"}, "Content-Encoding": {"gzip"}},
			}

			if writeStreamed(newTestServer(t), w, msg, strings.NewReader("hello")) {
				t.Error("writeStreamedResponse() = true for an invalid status")
			}

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

func TestWriteStreamedResponse_ValidStatusBoundaries(t *testing.T) {
	for _, status := range []int{200, 404, 599, 999} {
		w := httptest.NewRecorder()

		writeStreamed(newTestServer(t), w, &common.Message{Status: status}, strings.NewReader("ok"))

		if w.Code != status {
			t.Errorf("code = %d, want %d", w.Code, status)
		}
		if body := w.Body.String(); body != "ok" {
			t.Errorf("status %d: body = %q, want %q", status, body, "ok")
		}
	}
}

func TestAwaitResponse_ConnectionClosed(t *testing.T) {
	s := newTestServer(t)
	w := httptest.NewRecorder()
	conn := newConnection(nil, "")
	conn.closeOnce.Do(func() { close(conn.done) })

	done := make(chan time.Duration, 1)
	start := time.Now()
	go func() {
		s.awaitResponse(context.Background(), w, conn, make(chan *common.Message), bodySentNow(), never)
		done <- time.Since(start)
	}()

	select {
	case d := <-done:
		if d > time.Second {
			t.Errorf("took %v, want immediate", d)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("awaitResponse blocked after the tunnel closed")
	}

	if w.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadGateway)
	}
}

// A response delivered just before the tunnel closes must win over the 502.
func TestAwaitResponse_ClosedConnectionPrefersBufferedResponse(t *testing.T) {
	s := newTestServer(t)
	conn := newConnection(nil, "")
	conn.closeOnce.Do(func() { close(conn.done) })

	for range 50 {
		w := httptest.NewRecorder()
		ch := make(chan *common.Message, 1)
		ch <- &common.Message{Status: http.StatusOK}

		if s.awaitResponse(context.Background(), w, conn, ch, bodySentNow(), never) == nil {
			t.Fatalf("status = %d, want the buffered response (lost to close race)", w.Code)
		}
	}
}

// A request in flight when the tunnel drops must fail fast with 502 instead
// of parking until the response timeout expires.
func TestTunnelHTTP_ClientDisconnects(t *testing.T) {
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
		s.tunnelHTTP(w, r, connection, "foo")
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
		t.Fatalf("still waiting after 5s (response timeout is %v)", s.responseTimeout)
	}

	if w.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadGateway)
	}
}

// Even at debug level, a visitor's secrets must not reach the server log: on a
// shared server the operator is not the tunnel owner.
func TestRequestLogging_LeavesOutSecrets(t *testing.T) {
	s := newTestServer(t)
	var logs bytes.Buffer
	s.Logger = slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))

	s.logRequest("myapp", &common.Message{
		UUID:   "req-1",
		Method: "POST",
		URL:    "/reset?token=QUERY-SECRET",
		Headers: map[string][]string{
			"Authorization": {"Bearer HEADER-SECRET"},
			"Cookie":        {"sessionid=COOKIE-SECRET"},
		},
	})

	connection := newConnection(nil, "")
	_, stop := connection.requests.Register("req-1")
	defer stop()
	connection.bodies.Open("req-1", nil) //nolint:errcheck
	handlers := s.newTunnelHandlers("myapp", connection)
	for _, m := range []*common.Message{
		{Type: common.MessageTypeHTTPResponse, UUID: "req-1", Status: 200},
		{Type: common.MessageTypeStreamData, UUID: "req-1", Body: []byte(`{"session":"RESPONSE-SECRET"}`)},
	} {
		if err := handlers.Dispatch(m); err != nil {
			t.Fatalf("Dispatch() error = %v", err)
		}
	}

	out := logs.String()
	for _, secret := range []string{"QUERY-SECRET", "HEADER-SECRET", "COOKIE-SECRET", "RESPONSE-SECRET"} {
		if strings.Contains(out, secret) {
			t.Errorf("log contains %s:\n%s", secret, out)
		}
	}
	for _, want := range []string{"path=/reset", "Authorization", "HTTP response received"} {
		if !strings.Contains(out, want) {
			t.Errorf("log missing %q:\n%s", want, out)
		}
	}
}
