package common

import (
	"errors"
	"testing"
)

func TestNewMessageDispatcher(t *testing.T) {
	md := NewMessageDispatcher()

	if md == nil {
		t.Fatal("NewMessageDispatcher() returned nil")
	}

	if md.handlers == nil {
		t.Error("handlers map should be initialized")
	}

	if len(md.handlers) != 0 {
		t.Errorf("handlers map should be empty, got %d entries", len(md.handlers))
	}
}

func TestMessageDispatcher_Register(t *testing.T) {
	md := NewMessageDispatcher()
	called := false

	handler := func(msg *Message) error {
		called = true
		return nil
	}

	md.Register(MessageTypeHTTPRequest, handler)

	if _, exists := md.handlers[MessageTypeHTTPRequest]; !exists {
		t.Error("handler should be registered for MessageTypeHTTPRequest")
	}

	md.handlers[MessageTypeHTTPRequest](&Message{})
	if !called {
		t.Error("registered handler was not the one we provided")
	}
}

func TestMessageDispatcher_Register_Overwrites(t *testing.T) {
	md := NewMessageDispatcher()

	firstCalled := false
	secondCalled := false

	firstHandler := func(msg *Message) error {
		firstCalled = true
		return nil
	}

	secondHandler := func(msg *Message) error {
		secondCalled = true
		return nil
	}

	md.Register(MessageTypeHTTPRequest, firstHandler)
	md.Register(MessageTypeHTTPRequest, secondHandler)

	md.handlers[MessageTypeHTTPRequest](&Message{})

	if firstCalled {
		t.Error("first handler should have been overwritten")
	}

	if !secondCalled {
		t.Error("second handler should have been called")
	}
}

func TestMessageDispatcher_Dispatch(t *testing.T) {
	tests := []struct {
		name        string
		messageType MessageType
		register    bool
		handlerErr  error
		wantErr     bool
		errContains string
	}{
		{
			name:        "successful dispatch",
			messageType: MessageTypeHTTPRequest,
			register:    true,
			handlerErr:  nil,
			wantErr:     false,
		},
		{
			name:        "unknown message type",
			messageType: MessageTypeHTTPRequest,
			register:    false,
			wantErr:     true,
			errContains: "unknown message type",
		},
		{
			name:        "handler returns error",
			messageType: MessageTypeHTTPResponse,
			register:    true,
			handlerErr:  errors.New("handler failed"),
			wantErr:     true,
			errContains: "handler for",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			md := NewMessageDispatcher()

			if tt.register {
				md.Register(tt.messageType, func(msg *Message) error {
					return tt.handlerErr
				})
			}

			msg := &Message{Type: tt.messageType}
			err := md.Dispatch(msg)

			if (err != nil) != tt.wantErr {
				t.Errorf("Dispatch() error = %v, wantErr %v", err, tt.wantErr)
			}

			if tt.wantErr && tt.errContains != "" {
				if err == nil || !contains(err.Error(), tt.errContains) {
					t.Errorf("Dispatch() error = %v, want error containing %q", err, tt.errContains)
				}
			}
		})
	}
}

func TestMessageDispatcher_Dispatch_PassesCorrectMessage(t *testing.T) {
	md := NewMessageDispatcher()

	expectedUUID := "test-uuid-123"
	expectedURL := "/api/test"
	var receivedMsg *Message

	md.Register(MessageTypeHTTPRequest, func(msg *Message) error {
		receivedMsg = msg
		return nil
	})

	msg := &Message{
		Type: MessageTypeHTTPRequest,
		UUID: expectedUUID,
		URL:  expectedURL,
	}

	err := md.Dispatch(msg)
	if err != nil {
		t.Fatalf("Dispatch() unexpected error: %v", err)
	}

	if receivedMsg == nil {
		t.Fatal("handler was not called")
	}

	if receivedMsg.UUID != expectedUUID {
		t.Errorf("received UUID = %q, want %q", receivedMsg.UUID, expectedUUID)
	}

	if receivedMsg.URL != expectedURL {
		t.Errorf("received URL = %q, want %q", receivedMsg.URL, expectedURL)
	}
}

func TestMessageDispatcher_Dispatch_MultipleHandlers(t *testing.T) {
	md := NewMessageDispatcher()

	requestCalled := false
	responseCalled := false

	md.Register(MessageTypeHTTPRequest, func(msg *Message) error {
		requestCalled = true
		return nil
	})

	md.Register(MessageTypeHTTPResponse, func(msg *Message) error {
		responseCalled = true
		return nil
	})

	err := md.Dispatch(&Message{Type: MessageTypeHTTPRequest})
	if err != nil {
		t.Fatalf("Dispatch(HTTPRequest) unexpected error: %v", err)
	}

	if !requestCalled {
		t.Error("request handler should have been called")
	}

	if responseCalled {
		t.Error("response handler should not have been called")
	}

	requestCalled = false

	err = md.Dispatch(&Message{Type: MessageTypeHTTPResponse})
	if err != nil {
		t.Fatalf("Dispatch(HTTPResponse) unexpected error: %v", err)
	}

	if requestCalled {
		t.Error("request handler should not have been called")
	}

	if !responseCalled {
		t.Error("response handler should have been called")
	}
}

func TestMessageDispatcher_Dispatch_ErrorWrapping(t *testing.T) {
	md := NewMessageDispatcher()

	originalErr := errors.New("original error")
	md.Register(MessageTypeHTTPRequest, func(msg *Message) error {
		return originalErr
	})

	err := md.Dispatch(&Message{Type: MessageTypeHTTPRequest})
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if !errors.Is(err, originalErr) {
		t.Errorf("error should wrap original error, got: %v", err)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsAt(s, substr))
}

func containsAt(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
