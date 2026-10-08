package server

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dbackowski/wormhole/common"
	"github.com/gorilla/websocket"
)

// startTunnel runs a server with a fake client registered for foo.localhost.
func startTunnel(t *testing.T) (*Server, *httptest.Server, *websocket.Conn) {
	t.Helper()
	s := newTestServer(t)
	srv := httptest.NewServer(s.mux)
	t.Cleanup(srv.Close)

	tunnel := dialWS(t, "ws"+strings.TrimPrefix(srv.URL, "http"), "foo.localhost")
	t.Cleanup(func() { tunnel.Close() })
	if msg := readTunnel(t, tunnel); msg.Type != common.MessageTypeDomainRegistered {
		t.Fatalf("first message = %q, want domain_registered", msg.Type)
	}
	return s, srv, tunnel
}

func readTunnel(t *testing.T, tunnel *websocket.Conn) common.Message {
	t.Helper()
	tunnel.SetReadDeadline(time.Now().Add(3 * time.Second))
	var msg common.Message
	if err := tunnel.ReadJSON(&msg); err != nil {
		t.Fatalf("read from tunnel: %v", err)
	}
	return msg
}

// dialBrowser sends a WebSocket handshake over a raw connection, so the test
// sees exactly the bytes the server relays.
func dialBrowser(t *testing.T, srv *httptest.Server) (net.Conn, *bufio.Reader) {
	t.Helper()
	conn, err := net.Dial("tcp", srv.Listener.Addr().String())
	if err != nil {
		t.Fatalf("dial server: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	fmt.Fprint(conn, "GET /socket?room=1 HTTP/1.1\r\n"+
		"Host: foo.localhost\r\n"+
		"Connection: keep-alive, Upgrade\r\n"+
		"Upgrade: websocket\r\n"+
		"Sec-WebSocket-Version: 13\r\n"+
		"Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n\r\n")
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	return conn, bufio.NewReader(conn)
}

const testAccept = "s3pPLMBiTxaQ9kYGzzhZRbK+xOo="

func acceptUpgrade(t *testing.T, tunnel *websocket.Conn, uuid string) {
	t.Helper()
	err := tunnel.WriteJSON(common.Message{
		Type:   common.MessageTypeHTTPResponse,
		UUID:   uuid,
		Status: http.StatusSwitchingProtocols,
		Headers: map[string][]string{
			"Upgrade":              {"websocket"},
			"Connection":           {"Upgrade"},
			"Sec-Websocket-Accept": {testAccept},
		},
	})
	if err != nil {
		t.Fatalf("write 101: %v", err)
	}
}

func readSwitch(t *testing.T, br *bufio.Reader) {
	t.Helper()
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		t.Fatalf("read handshake response: %v", err)
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("status = %d, want 101", resp.StatusCode)
	}
	if got := resp.Header.Get("Sec-WebSocket-Accept"); got != testAccept {
		t.Errorf("Sec-WebSocket-Accept = %q, want %q", got, testAccept)
	}
	if resp.ContentLength > 0 || resp.Header.Get("Content-Length") != "" {
		t.Errorf("101 carries Content-Length %q", resp.Header.Get("Content-Length"))
	}
}

// openSocket completes a handshake that the fake client accepts.
func openSocket(t *testing.T, srv *httptest.Server, tunnel *websocket.Conn) (net.Conn, *bufio.Reader, string) {
	t.Helper()
	conn, br := dialBrowser(t, srv)
	req := readTunnel(t, tunnel)
	if req.Type != common.MessageTypeUpgradeRequest {
		t.Fatalf("message type = %q, want upgrade_request", req.Type)
	}
	acceptUpgrade(t, tunnel, req.UUID)
	readSwitch(t, br)
	return conn, br, req.UUID
}

func waitForStreams(t *testing.T, s *Server, want int) {
	t.Helper()
	connection, err := s.connManager.GetConnection("foo")
	if err != nil {
		t.Fatalf("GetConnection() error = %v", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for connection.streams.Count() != want {
		if time.Now().After(deadline) {
			t.Fatalf("open streams = %d, want %d", connection.streams.Count(), want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestTunnelUpgrade_RelaysBothDirections(t *testing.T) {
	s, srv, tunnel := startTunnel(t)
	conn, br := dialBrowser(t, srv)

	req := readTunnel(t, tunnel)
	if req.Type != common.MessageTypeUpgradeRequest {
		t.Fatalf("message type = %q, want upgrade_request", req.Type)
	}
	if req.URL != "/socket?room=1" || req.Method != http.MethodGet {
		t.Errorf("request = %s %s, want GET /socket?room=1", req.Method, req.URL)
	}
	h := http.Header(req.Headers)
	if h.Get("Connection") != "Upgrade" || h.Get("Upgrade") != "websocket" {
		t.Errorf("Connection = %q, Upgrade = %q, want restored handshake headers", h.Get("Connection"), h.Get("Upgrade"))
	}
	if h.Get("Sec-WebSocket-Key") == "" || h.Get("X-Forwarded-Host") != "foo.localhost" {
		t.Errorf("headers = %v, want key and forwarded host", h)
	}

	acceptUpgrade(t, tunnel, req.UUID)
	// Sent before the browser has read the 101, so it must wait in the queue.
	tunnel.WriteJSON(common.Message{Type: common.MessageTypeStreamData, UUID: req.UUID, Body: []byte("from app")}) //nolint:errcheck

	readSwitch(t, br)
	got := make([]byte, len("from app"))
	if _, err := io.ReadFull(br, got); err != nil || string(got) != "from app" {
		t.Fatalf("browser read = %q, %v; want %q", got, err, "from app")
	}

	if _, err := conn.Write([]byte("from browser")); err != nil {
		t.Fatalf("browser write: %v", err)
	}
	var relayed []byte
	for len(relayed) < len("from browser") {
		msg := readTunnel(t, tunnel)
		if msg.Type != common.MessageTypeStreamData || msg.UUID != req.UUID {
			t.Fatalf("message = %+v, want stream_data", msg)
		}
		relayed = append(relayed, msg.Body...)
	}
	if string(relayed) != "from browser" {
		t.Errorf("relayed = %q, want %q", relayed, "from browser")
	}

	tunnel.WriteJSON(common.Message{Type: common.MessageTypeStreamClose, UUID: req.UUID}) //nolint:errcheck
	if _, err := br.ReadByte(); err != io.EOF {
		t.Errorf("browser read after stream_close = %v, want EOF", err)
	}
	if msg := readTunnel(t, tunnel); msg.Type != common.MessageTypeStreamClose {
		t.Errorf("message = %q, want stream_close", msg.Type)
	}
	waitForStreams(t, s, 0)
}

func TestTunnelUpgrade_BrowserCloseNotifiesClient(t *testing.T) {
	s, srv, tunnel := startTunnel(t)
	conn, _, id := openSocket(t, srv, tunnel)

	conn.Close()

	msg := readTunnel(t, tunnel)
	if msg.Type != common.MessageTypeStreamClose || msg.UUID != id {
		t.Errorf("message = %+v, want stream_close for %s", msg, id)
	}
	waitForStreams(t, s, 0)
}

func TestTunnelUpgrade_TunnelDropClosesSocket(t *testing.T) {
	_, srv, tunnel := startTunnel(t)
	_, br, _ := openSocket(t, srv, tunnel)

	tunnel.Close()

	if _, err := br.ReadByte(); err == nil {
		t.Error("browser socket still open after the tunnel dropped")
	}
}

func TestTunnelUpgrade_RejectedHandshakePassesThrough(t *testing.T) {
	_, srv, tunnel := startTunnel(t)
	_, br := dialBrowser(t, srv)

	req := readTunnel(t, tunnel)
	tunnel.WriteJSON(common.Message{ //nolint:errcheck
		Type:   common.MessageTypeHTTPResponse,
		UUID:   req.UUID,
		Status: http.StatusForbidden,
		Body:   []byte("nope"),
	})

	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusForbidden || string(body) != "nope" {
		t.Errorf("response = %d %q, want 403 %q", resp.StatusCode, body, "nope")
	}
}

func TestTunnelUpgrade_TooManyStreams(t *testing.T) {
	s, srv, _ := startTunnel(t)
	connection, _ := s.connManager.GetConnection("foo")
	for i := range common.MaxStreamsPerTunnel {
		connection.streams.Open(fmt.Sprint("busy-", i)) //nolint:errcheck
	}

	_, br := dialBrowser(t, srv)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", resp.StatusCode)
	}
}
