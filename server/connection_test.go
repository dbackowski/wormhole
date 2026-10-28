package server

import (
	"testing"

	"github.com/dbackowski/wormhole/common"
)

func newTestConnection(t *testing.T) (*Connection, func()) {
	t.Helper()
	ws, cleanup := newTestWSPair(t)
	return newConnection(ws, ""), cleanup
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
