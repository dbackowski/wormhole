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

func TestConnection_RegisterRequest_CancelIsIdempotent(t *testing.T) {
	conn, cleanup := newTestConnection(t)
	defer cleanup()

	ch, cancel := conn.RegisterRequest(context.Background(), "uuid-1")

	cancel()
	// A second cancel must not panic on the already-closed channel.
	cancel()

	if _, ok := <-ch; ok {
		t.Error("expected response channel to be closed after cancel")
	}
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
