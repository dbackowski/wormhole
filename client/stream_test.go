package client

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dbackowski/wormhole/common"
	"github.com/gorilla/websocket"
)

// echoApp is a local app that echoes WebSocket messages. It reports the
// extensions each handshake offered, and the error that ended each socket.
func echoApp(t *testing.T) (app *httptest.Server, extensions chan string, closed chan error) {
	t.Helper()
	extensions = make(chan string, 1)
	closed = make(chan error, 1)
	upgrader := websocket.Upgrader{EnableCompression: true}

	app = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		extensions <- r.Header.Get("Sec-WebSocket-Extensions")
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			mt, data, err := conn.ReadMessage()
			if err != nil {
				closed <- err
				return
			}
			conn.WriteMessage(mt, data) //nolint:errcheck
		}
	}))
	t.Cleanup(app.Close)
	return app, extensions, closed
}

func startStreamClient(t *testing.T, localURL string) (*Client, *websocket.Conn) {
	t.Helper()
	client, _, wsServer, serverConn := newTestClient(t, localURL)
	t.Cleanup(wsServer.Close)
	t.Cleanup(func() { client.Conn.Close() })
	client.Logger = slog.New(slog.DiscardHandler)
	client.setupMessageHandlers()
	go client.HandleConnection()
	return client, serverConn
}

func readFromClient(t *testing.T, serverConn *websocket.Conn) common.Message {
	t.Helper()
	serverConn.SetReadDeadline(time.Now().Add(3 * time.Second))
	var msg common.Message
	if err := serverConn.ReadJSON(&msg); err != nil {
		t.Fatalf("read from client: %v", err)
	}
	return msg
}

func sendUpgrade(t *testing.T, serverConn *websocket.Conn, uuid string) {
	t.Helper()
	err := serverConn.WriteJSON(common.Message{
		Type:   common.MessageTypeUpgradeRequest,
		UUID:   uuid,
		Method: http.MethodGet,
		URL:    "/socket",
		Headers: map[string][]string{
			"Host":                     {"test.example.com"},
			"Connection":               {"Upgrade"},
			"Upgrade":                  {"websocket"},
			"Sec-Websocket-Version":    {"13"},
			"Sec-Websocket-Key":        {"dGhlIHNhbXBsZSBub25jZQ=="},
			"Sec-Websocket-Extensions": {"permessage-deflate; client_max_window_bits"},
		},
	})
	if err != nil {
		t.Fatalf("write upgrade_request: %v", err)
	}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestUpgradeRequest_RelaysAndRecordsMessages(t *testing.T) {
	app, extensions, closed := echoApp(t)
	client, serverConn := startStreamClient(t, app.URL)

	sendUpgrade(t, serverConn, "ws-1")

	resp := readFromClient(t, serverConn)
	if resp.Type != common.MessageTypeHTTPResponse || resp.Status != http.StatusSwitchingProtocols {
		t.Fatalf("response = %s %d, want http_response 101", resp.Type, resp.Status)
	}
	h := http.Header(resp.Headers)
	if h.Get("Sec-WebSocket-Accept") != "s3pPLMBiTxaQ9kYGzzhZRbK+xOo=" || h.Get("Upgrade") != "websocket" {
		t.Errorf("handshake headers = %v, want accept key and Upgrade", h)
	}
	if ext := <-extensions; ext != "" {
		t.Errorf("app was offered extensions %q, want none so frames stay readable", ext)
	}

	sent := wsFrame(true, opText, testMask, []byte("hello"))
	serverConn.WriteJSON(common.Message{Type: common.MessageTypeStreamData, UUID: "ws-1", Body: sent}) //nolint:errcheck

	want := wsFrame(true, opText, nil, []byte("hello"))
	var echoed []byte
	for len(echoed) < len(want) {
		msg := readFromClient(t, serverConn)
		if msg.Type != common.MessageTypeStreamData || msg.UUID != "ws-1" {
			t.Fatalf("message = %+v, want stream_data", msg)
		}
		echoed = append(echoed, msg.Body...)
	}
	if !bytes.Equal(echoed, want) {
		t.Errorf("echoed = %x, want %x", echoed, want)
	}

	var messages []WSMessage
	waitFor(t, "both messages in the history", func() bool {
		messages, _ = client.history.GetMessages("ws-1")
		return len(messages) == 2
	})
	if messages[0].FromApp || messages[0].Type != "text" || string(messages[0].Payload) != "hello" {
		t.Errorf("first message = %+v, want text hello sent to the app", messages[0])
	}
	if !messages[1].FromApp || string(messages[1].Payload) != "hello" {
		t.Errorf("second message = %+v, want text hello from the app", messages[1])
	}
	stream := client.history.GetRecent(1)[0].Stream
	if !stream.Open || stream.BytesToApp != int64(len(sent)) || stream.BytesFromApp != int64(len(want)) || stream.Messages != 2 {
		t.Errorf("stream = %+v, want open with %d bytes in, %d out, 2 messages", stream, len(sent), len(want))
	}

	serverConn.WriteJSON(common.Message{Type: common.MessageTypeStreamClose, UUID: "ws-1"}) //nolint:errcheck

	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		t.Fatal("local socket still open after stream_close")
	}
	if msg := readFromClient(t, serverConn); msg.Type != common.MessageTypeStreamClose {
		t.Errorf("message = %q, want stream_close", msg.Type)
	}
	waitFor(t, "stream marked closed", func() bool {
		s := client.history.GetRecent(1)[0].Stream
		return !s.Open && !s.ClosedAt.IsZero()
	})
}

func TestUpgradeRequest_RejectedHandshakeIsPlainResponse(t *testing.T) {
	app := httptest.NewServer(http.NotFoundHandler())
	defer app.Close()
	client, serverConn := startStreamClient(t, app.URL)

	sendUpgrade(t, serverConn, "ws-404")

	resp := readFromClient(t, serverConn)
	if resp.Status != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.Status)
	}
	waitFor(t, "history entry", func() bool { return len(client.history.GetRecent(1)) == 1 })
	if s := client.history.GetRecent(1)[0].Stream; s != (StreamInfo{}) {
		t.Errorf("stream = %+v, want zero for a rejected handshake", s)
	}
	waitFor(t, "stream released", func() bool { return client.streams.Count() == 0 })
}

func TestUpgradeRequest_TunnelDropClosesLocalSocket(t *testing.T) {
	app, _, closed := echoApp(t)
	_, serverConn := startStreamClient(t, app.URL)

	sendUpgrade(t, serverConn, "ws-drop")
	readFromClient(t, serverConn)

	serverConn.Close()

	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		t.Fatal("local socket still open after the tunnel dropped")
	}
}
