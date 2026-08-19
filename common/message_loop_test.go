package common

import (
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

type wsTestPair struct {
	client  *websocket.Conn
	server  *websocket.Conn
	cleanup func()
}

func newWSTestPair(t *testing.T) *wsTestPair {
	t.Helper()

	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	serverConnCh := make(chan *websocket.Conn, 1)
	upgradeErrCh := make(chan error, 1)

	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			upgradeErrCh <- err
			return
		}
		serverConnCh <- conn
	}))

	wsURL := "ws" + strings.TrimPrefix(httpSrv.URL, "http")
	client, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		httpSrv.Close()
		t.Fatalf("dial websocket: %v", err)
	}

	var server *websocket.Conn
	select {
	case server = <-serverConnCh:
	case err := <-upgradeErrCh:
		client.Close()
		httpSrv.Close()
		t.Fatalf("upgrade: %v", err)
	case <-time.After(2 * time.Second):
		client.Close()
		httpSrv.Close()
		t.Fatal("timed out waiting for server connection")
	}

	cleanup := func() {
		client.Close()
		server.Close()
		httpSrv.Close()
	}

	return &wsTestPair{client: client, server: server, cleanup: cleanup}
}

func waitFor(t *testing.T, ch <-chan struct{}, msg string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", msg)
	}
}

func TestRunMessageLoop_DispatchesMessagesUntilReadError(t *testing.T) {
	pair := newWSTestPair(t)
	defer pair.cleanup()

	dispatcher := NewMessageDispatcher()

	var mu sync.Mutex
	var received []*Message

	dispatcher.Register(MessageTypeHTTPRequest, func(msg *Message) error {
		mu.Lock()
		received = append(received, msg)
		mu.Unlock()
		return nil
	})

	var readErr error
	done := make(chan struct{})

	go func() {
		RunMessageLoop(
			pair.client,
			dispatcher,
			DefaultHeartbeat(),
			func(err error) { readErr = err; close(done) },
			func(msg *Message, err error) { t.Errorf("unexpected dispatch error: %v", err) },
		)
	}()

	messages := []Message{
		{Type: MessageTypeHTTPRequest, UUID: "uuid-1", URL: "/one"},
		{Type: MessageTypeHTTPRequest, UUID: "uuid-2", URL: "/two"},
	}
	for _, m := range messages {
		if err := pair.server.WriteJSON(m); err != nil {
			t.Fatalf("server WriteJSON: %v", err)
		}
	}

	pair.server.Close()
	waitFor(t, done, "loop exit")

	if readErr == nil {
		t.Error("expected onReadErr to be called with a non-nil error")
	}

	mu.Lock()
	defer mu.Unlock()

	if len(received) != len(messages) {
		t.Fatalf("received %d messages, want %d", len(received), len(messages))
	}

	for i, want := range messages {
		if received[i].UUID != want.UUID {
			t.Errorf("message[%d] UUID = %q, want %q", i, received[i].UUID, want.UUID)
		}
		if received[i].URL != want.URL {
			t.Errorf("message[%d] URL = %q, want %q", i, received[i].URL, want.URL)
		}
	}
}

func TestRunMessageLoop_CallsOnReadErrAndReturns(t *testing.T) {
	pair := newWSTestPair(t)
	defer pair.cleanup()

	dispatcher := NewMessageDispatcher()

	var readErr error
	done := make(chan struct{})

	go func() {
		RunMessageLoop(
			pair.client,
			dispatcher,
			DefaultHeartbeat(),
			func(err error) { readErr = err; close(done) },
			func(msg *Message, err error) { t.Errorf("unexpected dispatch error: %v", err) },
		)
	}()

	pair.server.Close()
	waitFor(t, done, "loop exit")

	if readErr == nil {
		t.Fatal("expected onReadErr to be called with a non-nil error")
	}
}

func TestRunMessageLoop_EnforcesReadLimit(t *testing.T) {
	pair := newWSTestPair(t)
	defer pair.cleanup()

	dispatcher := NewMessageDispatcher()
	dispatcher.Register(MessageTypeHTTPResponse, func(msg *Message) error { return nil })

	var readErr error
	done := make(chan struct{})

	go func() {
		RunMessageLoop(
			pair.client,
			dispatcher,
			DefaultHeartbeat(),
			func(err error) { readErr = err; close(done) },
			func(msg *Message, err error) { t.Errorf("unexpected dispatch error: %v", err) },
		)
	}()

	// Body base64-encodes to ~4/3 its size, so 13 MB yields a frame over the
	// 16 MB limit. Write it from a goroutine: the reader stops draining once the
	// limit trips, so the write blocks on flow control until cleanup() closes
	// the connection — waiting on it inline would deadlock the test.
	go func() {
		pair.server.WriteJSON(Message{
			Type: MessageTypeHTTPResponse,
			UUID: "big",
			Body: make([]byte, 13<<20),
		})
	}()

	waitFor(t, done, "loop exit on read limit")
	if readErr == nil {
		t.Fatal("expected onReadErr when message exceeds read limit")
	}
}

func TestRunMessageLoop_CallsOnDispatchErrAndContinues(t *testing.T) {
	pair := newWSTestPair(t)
	defer pair.cleanup()

	dispatcher := NewMessageDispatcher()

	handlerErr := errors.New("handler boom")
	dispatcher.Register(MessageTypeHTTPResponse, func(msg *Message) error {
		return handlerErr
	})

	var (
		mu           sync.Mutex
		dispatchErrs []error
		dispatchMsgs []*Message
	)
	dispatchSeen := make(chan struct{}, 2)
	done := make(chan struct{})

	go func() {
		RunMessageLoop(
			pair.client,
			dispatcher,
			DefaultHeartbeat(),
			func(err error) { close(done) },
			func(msg *Message, err error) {
				mu.Lock()
				dispatchErrs = append(dispatchErrs, err)
				dispatchMsgs = append(dispatchMsgs, msg)
				mu.Unlock()
				dispatchSeen <- struct{}{}
			},
		)
	}()

	// Unknown type — Dispatch returns "unknown message type" error.
	if err := pair.server.WriteJSON(Message{Type: MessageType("nope"), UUID: "u1"}); err != nil {
		t.Fatalf("server WriteJSON: %v", err)
	}
	// Known type whose handler errors — loop must keep going after either case.
	if err := pair.server.WriteJSON(Message{Type: MessageTypeHTTPResponse, UUID: "u2"}); err != nil {
		t.Fatalf("server WriteJSON: %v", err)
	}

	for i := 0; i < 2; i++ {
		select {
		case <-dispatchSeen:
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for dispatch error %d", i+1)
		}
	}

	pair.server.Close()
	waitFor(t, done, "loop exit")

	mu.Lock()
	defer mu.Unlock()

	if len(dispatchErrs) != 2 {
		t.Fatalf("got %d dispatch errors, want 2", len(dispatchErrs))
	}

	if !strings.Contains(dispatchErrs[0].Error(), "unknown message type") {
		t.Errorf("first error = %v, want it to contain 'unknown message type'", dispatchErrs[0])
	}
	if dispatchMsgs[0].UUID != "u1" {
		t.Errorf("first dispatch msg UUID = %q, want %q", dispatchMsgs[0].UUID, "u1")
	}

	if !errors.Is(dispatchErrs[1], handlerErr) {
		t.Errorf("second error = %v, want it to wrap handlerErr", dispatchErrs[1])
	}
	if dispatchMsgs[1].UUID != "u2" {
		t.Errorf("second dispatch msg UUID = %q, want %q", dispatchMsgs[1].UUID, "u2")
	}
}

func TestRunMessageLoop_SendsPings(t *testing.T) {
	pair := newWSTestPair(t)
	defer pair.cleanup()

	pinged := make(chan struct{}, 1)
	pair.server.SetPingHandler(func(string) error {
		select {
		case pinged <- struct{}{}:
		default:
		}
		return nil
	})

	// Drive the server's reader so incoming ping control frames are processed.
	go func() {
		for {
			if _, _, err := pair.server.ReadMessage(); err != nil {
				return
			}
		}
	}()

	hb := Heartbeat{PongWait: time.Second, PingPeriod: 20 * time.Millisecond, WriteWait: time.Second}
	go RunMessageLoop(pair.client, NewMessageDispatcher(), hb,
		func(error) {}, func(*Message, error) {})

	select {
	case <-pinged:
	case <-time.After(2 * time.Second):
		t.Fatal("expected a ping to be sent within 2s")
	}
}

func TestRunMessageLoop_ExitsWhenPeerStopsResponding(t *testing.T) {
	pair := newWSTestPair(t)
	defer pair.cleanup()

	// The server side never reads, so it never auto-pongs; the client's read
	// deadline must fire and terminate the loop.
	hb := Heartbeat{PongWait: 150 * time.Millisecond, PingPeriod: 40 * time.Millisecond, WriteWait: time.Second}

	var readErr error
	done := make(chan struct{})
	go RunMessageLoop(pair.client, NewMessageDispatcher(), hb,
		func(err error) { readErr = err; close(done) },
		func(*Message, error) {})

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("expected loop to exit after missed pongs")
	}
	if readErr == nil {
		t.Fatal("expected a non-nil read error after missed heartbeat")
	}
}

func TestRunMessageLoop_OnReadErrReceivesUnderlyingError(t *testing.T) {
	pair := newWSTestPair(t)
	defer pair.cleanup()

	dispatcher := NewMessageDispatcher()

	var readErr error
	done := make(chan struct{})

	go func() {
		RunMessageLoop(
			pair.client,
			dispatcher,
			DefaultHeartbeat(),
			func(err error) { readErr = err; close(done) },
			func(msg *Message, err error) { t.Errorf("unexpected dispatch error: %v", err) },
		)
	}()

	// Abnormal close from the server side surfaces as a CloseError on the client.
	if err := pair.server.WriteMessage(
		websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseAbnormalClosure, "bye"),
	); err != nil {
		t.Fatalf("server WriteMessage(close): %v", err)
	}
	pair.server.Close()

	waitFor(t, done, "loop exit")

	if readErr == nil {
		t.Fatal("expected onReadErr to be called with a non-nil error")
	}
}

func TestPingLoop_ClosesConnectionOnWriteError(t *testing.T) {
	pair := newWSTestPair(t)
	defer pair.cleanup()

	// Shut down only the write half, so pings fail with a plain network error
	// while the socket itself is still open and readable.
	tcpConn, ok := pair.server.UnderlyingConn().(*net.TCPConn)
	if !ok {
		t.Fatalf("underlying conn is %T, want *net.TCPConn", pair.server.UnderlyingConn())
	}
	if err := tcpConn.CloseWrite(); err != nil {
		t.Fatalf("closing write half: %v", err)
	}

	hb := Heartbeat{PingPeriod: 10 * time.Millisecond, WriteWait: time.Second}
	done := make(chan struct{})
	defer close(done)

	returned := make(chan struct{})
	go func() {
		pingLoop(pair.server, hb, done)
		close(returned)
	}()

	waitFor(t, returned, "pingLoop to give up on a dead connection")

	// Giving up is not enough: the socket has to be closed so the read loop
	// fails now instead of waiting out PongWait.
	// Errors here already mean the socket is gone; the deadline is only a guard
	// against blocking forever if it is not.
	_ = pair.server.SetReadDeadline(time.Now().Add(2 * time.Second))

	_, _, err := pair.server.ReadMessage()
	if err == nil {
		t.Fatal("ReadMessage() succeeded, want a closed-connection error")
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		t.Errorf("ReadMessage() blocked until the deadline (%v), want pingLoop to have closed the connection", err)
	}
}

func TestPingLoop_SurvivesWriteTimeout(t *testing.T) {
	pair := newWSTestPair(t)
	defer pair.cleanup()

	// A negative WriteWait puts every ping deadline in the past, so WriteControl
	// reports a timeout without ever touching the socket - the same error a busy
	// write lock produces.
	hb := Heartbeat{PingPeriod: 10 * time.Millisecond, WriteWait: -time.Second}
	done := make(chan struct{})

	returned := make(chan struct{})
	go func() {
		pingLoop(pair.client, hb, done)
		close(returned)
	}()

	select {
	case <-returned:
		t.Fatal("pingLoop returned after a write timeout, want it to keep pinging")
	case <-time.After(100 * time.Millisecond):
	}

	// The connection must still be usable once the write lock frees up.
	if err := pair.client.WriteMessage(websocket.TextMessage, []byte("still here")); err != nil {
		t.Fatalf("writing after a ping timeout: %v", err)
	}

	close(done)
	waitFor(t, returned, "pingLoop to stop when done is closed")
}
