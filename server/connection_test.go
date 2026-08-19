package server

import (
	"context"
	"testing"

	"github.com/dbackowski/wormhole/common"
)

func newTestConnection(t *testing.T) (*Connection, func()) {
	t.Helper()
	ws, cleanup := newTestWSPair(t)
	return newConnection(ws), cleanup
}

func TestConnection_RegisterRequest(t *testing.T) {
	conn, cleanup := newTestConnection(t)
	defer cleanup()

	ch, cancel := conn.RegisterRequest(context.Background(), "uuid-1")
	defer cancel()

	if ch == nil {
		t.Error("expected non-nil channel")
	}
}

func TestConnection_CleanupRequest(t *testing.T) {
	conn, cleanup := newTestConnection(t)
	defer cleanup()

	conn.RegisterRequest(context.Background(), "uuid-1")
	// should not panic
	conn.CleanupRequest("uuid-1")
	// double cleanup should also not panic
	conn.CleanupRequest("uuid-1")
}

func TestConnection_DeliverResponse_Success(t *testing.T) {
	conn, cleanup := newTestConnection(t)
	defer cleanup()

	ch, cancel := conn.RegisterRequest(context.Background(), "uuid-1")
	defer cancel()

	msg := &common.Message{UUID: "uuid-1", Type: common.MessageTypeHTTPResponse}
	if err := conn.DeliverResponse(msg); err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	received := <-ch
	if received.UUID != "uuid-1" {
		t.Errorf("UUID = %q, want %q", received.UUID, "uuid-1")
	}
}

func TestConnection_DeliverResponse_UnknownUUID(t *testing.T) {
	conn, cleanup := newTestConnection(t)
	defer cleanup()

	msg := &common.Message{UUID: "no-such-uuid"}
	if err := conn.DeliverResponse(msg); err == nil {
		t.Error("expected error for unknown UUID, got nil")
	}
}

func TestConnection_SendMessage(t *testing.T) {
	conn, cleanup := newTestConnection(t)
	defer cleanup()

	msg := &common.Message{UUID: "uuid-1", Type: common.MessageTypeHTTPRequest}
	if err := conn.SendMessage(msg); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestConnection_Close(t *testing.T) {
	conn, cleanup := newTestConnection(t)
	defer cleanup()

	if err := conn.Close(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}
