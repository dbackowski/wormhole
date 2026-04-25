package common

import (
	"errors"
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

func TestRunMessageLoop_CallsOnDispatchErrAndContinues(t *testing.T) {
	pair := newWSTestPair(t)
	defer pair.cleanup()

	dispatcher := NewMessageDispatcher()

	handlerErr := errors.New("handler boom")
	dispatcher.Register(MessageTypeHTTPResponse, func(msg *Message) error {
		return handlerErr
	})

	var (
		mu            sync.Mutex
		dispatchErrs  []error
		dispatchMsgs  []*Message
	)
	dispatchSeen := make(chan struct{}, 2)
	done := make(chan struct{})

	go func() {
		RunMessageLoop(
			pair.client,
			dispatcher,
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
