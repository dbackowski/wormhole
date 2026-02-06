package common

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
)

type logRecord struct {
	Level   slog.Level
	Message string
	Attrs   map[string]any
}

type mockHandler struct {
	mu      sync.Mutex
	records []logRecord
	level   slog.Level
}

func newMockHandler(level slog.Level) *mockHandler {
	return &mockHandler{
		level:   level,
		records: make([]logRecord, 0),
	}
}

func (h *mockHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level
}

func (h *mockHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	attrs := make(map[string]any)
	r.Attrs(func(a slog.Attr) bool {
		attrs[a.Key] = a.Value.Any()
		return true
	})

	h.records = append(h.records, logRecord{
		Level:   r.Level,
		Message: r.Message,
		Attrs:   attrs,
	})
	return nil
}

func (h *mockHandler) WithAttrs(_ []slog.Attr) slog.Handler {
	return h
}

func (h *mockHandler) WithGroup(_ string) slog.Handler {
	return h
}

func (h *mockHandler) getRecords() []logRecord {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]logRecord{}, h.records...)
}

func newTestLogger(level slog.Level) (*Logger, *mockHandler) {
	handler := newMockHandler(level)
	return &Logger{
		Logger: slog.New(handler),
		level:  level,
	}, handler
}

func TestNewRequestLogger(t *testing.T) {
	logger := NewLogger(LevelInfo, "text")
	rl := NewRequestLogger(logger)

	if rl == nil {
		t.Fatal("NewRequestLogger() returned nil")
	}
	if rl.logger != logger {
		t.Error("RequestLogger.logger does not match provided logger")
	}
}

func TestRequestLogger_LogHTTPRequest(t *testing.T) {
	t.Run("logs info level with correct fields", func(t *testing.T) {
		logger, handler := newTestLogger(slog.LevelInfo)
		rl := NewRequestLogger(logger)

		rl.LogHTTPRequest("example.com", "uuid-123", "POST", "https://example.com/api",
			map[string][]string{"Content-Type": {"application/json"}},
			[]byte(`{"key": "value"}`))

		records := handler.getRecords()
		if len(records) != 1 {
			t.Fatalf("expected 1 log record, got %d", len(records))
		}

		rec := records[0]
		if rec.Level != slog.LevelInfo {
			t.Errorf("expected level INFO, got %v", rec.Level)
		}
		if rec.Message != "HTTP request forwarded" {
			t.Errorf("expected message 'HTTP request forwarded', got %q", rec.Message)
		}
		if rec.Attrs["domain"] != "example.com" {
			t.Errorf("expected domain 'example.com', got %v", rec.Attrs["domain"])
		}
		if rec.Attrs["uuid"] != "uuid-123" {
			t.Errorf("expected uuid 'uuid-123', got %v", rec.Attrs["uuid"])
		}
		if rec.Attrs["method"] != "POST" {
			t.Errorf("expected method 'POST', got %v", rec.Attrs["method"])
		}
		if rec.Attrs["url"] != "https://example.com/api" {
			t.Errorf("expected url 'https://example.com/api', got %v", rec.Attrs["url"])
		}
	})

	t.Run("logs debug details when debug level enabled", func(t *testing.T) {
		logger, handler := newTestLogger(slog.LevelDebug)
		rl := NewRequestLogger(logger)

		headers := map[string][]string{"Authorization": {"Bearer token"}}
		body := []byte(`{"data": "test"}`)

		rl.LogHTTPRequest("example.com", "uuid-456", "GET", "/api", headers, body)

		records := handler.getRecords()
		if len(records) != 2 {
			t.Fatalf("expected 2 log records (info + debug), got %d", len(records))
		}

		infoRec := records[0]
		if infoRec.Level != slog.LevelInfo {
			t.Errorf("first record should be INFO, got %v", infoRec.Level)
		}

		debugRec := records[1]
		if debugRec.Level != slog.LevelDebug {
			t.Errorf("second record should be DEBUG, got %v", debugRec.Level)
		}
		if debugRec.Message != "HTTP request details" {
			t.Errorf("expected message 'HTTP request details', got %q", debugRec.Message)
		}
		if debugRec.Attrs["uuid"] != "uuid-456" {
			t.Errorf("expected uuid 'uuid-456', got %v", debugRec.Attrs["uuid"])
		}
		if _, ok := debugRec.Attrs["headers"]; !ok {
			t.Error("expected headers in debug log")
		}
		if _, ok := debugRec.Attrs["body"]; !ok {
			t.Error("expected body in debug log")
		}
	})

	t.Run("does not log debug details when info level", func(t *testing.T) {
		logger, handler := newTestLogger(slog.LevelInfo)
		rl := NewRequestLogger(logger)

		rl.LogHTTPRequest("example.com", "uuid-789", "GET", "/api", nil, nil)

		records := handler.getRecords()
		if len(records) != 1 {
			t.Fatalf("expected 1 log record (info only), got %d", len(records))
		}
	})
}

func TestRequestLogger_LogHTTPResponse(t *testing.T) {
	t.Run("logs info level with correct fields", func(t *testing.T) {
		logger, handler := newTestLogger(slog.LevelInfo)
		rl := NewRequestLogger(logger)

		rl.LogHTTPResponse("example.com", "resp-uuid", 200, []byte(`{"ok": true}`))

		records := handler.getRecords()
		if len(records) != 1 {
			t.Fatalf("expected 1 log record, got %d", len(records))
		}

		rec := records[0]
		if rec.Level != slog.LevelInfo {
			t.Errorf("expected level INFO, got %v", rec.Level)
		}
		if rec.Message != "HTTP response received" {
			t.Errorf("expected message 'HTTP response received', got %q", rec.Message)
		}
		if rec.Attrs["domain"] != "example.com" {
			t.Errorf("expected domain 'example.com', got %v", rec.Attrs["domain"])
		}
		if rec.Attrs["uuid"] != "resp-uuid" {
			t.Errorf("expected uuid 'resp-uuid', got %v", rec.Attrs["uuid"])
		}
		if rec.Attrs["status"] != int64(200) {
			t.Errorf("expected status 200, got %v", rec.Attrs["status"])
		}
	})

	t.Run("logs debug details when debug level enabled", func(t *testing.T) {
		logger, handler := newTestLogger(slog.LevelDebug)
		rl := NewRequestLogger(logger)

		body := []byte(`{"error": "not found"}`)
		rl.LogHTTPResponse("example.com", "resp-uuid-2", 404, body)

		records := handler.getRecords()
		if len(records) != 2 {
			t.Fatalf("expected 2 log records, got %d", len(records))
		}

		debugRec := records[1]
		if debugRec.Level != slog.LevelDebug {
			t.Errorf("second record should be DEBUG, got %v", debugRec.Level)
		}
		if debugRec.Message != "HTTP response details" {
			t.Errorf("expected message 'HTTP response details', got %q", debugRec.Message)
		}
		if _, ok := debugRec.Attrs["body"]; !ok {
			t.Error("expected body in debug log")
		}
	})

	t.Run("does not log debug details when info level", func(t *testing.T) {
		logger, handler := newTestLogger(slog.LevelInfo)
		rl := NewRequestLogger(logger)

		rl.LogHTTPResponse("example.com", "resp-uuid-3", 500, []byte("error"))

		records := handler.getRecords()
		if len(records) != 1 {
			t.Fatalf("expected 1 log record, got %d", len(records))
		}
	})
}

func TestRequestLogger_LogRequestError(t *testing.T) {
	logger, handler := newTestLogger(slog.LevelInfo)
	rl := NewRequestLogger(logger)

	testErr := errors.New("connection refused")
	rl.LogRequestError("connect", testErr)

	records := handler.getRecords()
	if len(records) != 1 {
		t.Fatalf("expected 1 log record, got %d", len(records))
	}

	rec := records[0]
	if rec.Level != slog.LevelError {
		t.Errorf("expected level ERROR, got %v", rec.Level)
	}
	if rec.Message != "Request operation failed" {
		t.Errorf("expected message 'Request operation failed', got %q", rec.Message)
	}
	if rec.Attrs["operation"] != "connect" {
		t.Errorf("expected operation 'connect', got %v", rec.Attrs["operation"])
	}
	if rec.Attrs["error"] != testErr {
		t.Errorf("expected error %v, got %v", testErr, rec.Attrs["error"])
	}
}

func TestRequestLogger_LogConnectionError(t *testing.T) {
	t.Run("logs error with basic fields", func(t *testing.T) {
		logger, handler := newTestLogger(slog.LevelInfo)
		rl := NewRequestLogger(logger)

		testErr := errors.New("network unreachable")
		rl.LogConnectionError("Connection failed", testErr)

		records := handler.getRecords()
		if len(records) != 1 {
			t.Fatalf("expected 1 log record, got %d", len(records))
		}

		rec := records[0]
		if rec.Level != slog.LevelError {
			t.Errorf("expected level ERROR, got %v", rec.Level)
		}
		if rec.Message != "Connection failed" {
			t.Errorf("expected message 'Connection failed', got %q", rec.Message)
		}
		if rec.Attrs["error"] != testErr {
			t.Errorf("expected error %v, got %v", testErr, rec.Attrs["error"])
		}
	})

	t.Run("logs error with additional fields", func(t *testing.T) {
		logger, handler := newTestLogger(slog.LevelInfo)
		rl := NewRequestLogger(logger)

		testErr := errors.New("handshake failed")
		rl.LogConnectionError("WebSocket error", testErr, "remote_addr", "192.168.1.1", "attempt", 3)

		records := handler.getRecords()
		if len(records) != 1 {
			t.Fatalf("expected 1 log record, got %d", len(records))
		}

		rec := records[0]
		if rec.Attrs["remote_addr"] != "192.168.1.1" {
			t.Errorf("expected remote_addr '192.168.1.1', got %v", rec.Attrs["remote_addr"])
		}
		if rec.Attrs["attempt"] != int64(3) {
			t.Errorf("expected attempt 3, got %v", rec.Attrs["attempt"])
		}
	})
}

func TestRequestLogger_LogLocalRequest(t *testing.T) {
	logger, handler := newTestLogger(slog.LevelInfo)
	rl := NewRequestLogger(logger)

	rl.LogLocalRequest("GET", "/health", 200)

	records := handler.getRecords()
	if len(records) != 1 {
		t.Fatalf("expected 1 log record, got %d", len(records))
	}

	rec := records[0]
	if rec.Level != slog.LevelInfo {
		t.Errorf("expected level INFO, got %v", rec.Level)
	}
	if rec.Message != "Local request processed" {
		t.Errorf("expected message 'Local request processed', got %q", rec.Message)
	}
	if rec.Attrs["method"] != "GET" {
		t.Errorf("expected method 'GET', got %v", rec.Attrs["method"])
	}
	if rec.Attrs["url"] != "/health" {
		t.Errorf("expected url '/health', got %v", rec.Attrs["url"])
	}
	if rec.Attrs["status"] != int64(200) {
		t.Errorf("expected status 200, got %v", rec.Attrs["status"])
	}
}

func TestRequestLogger_LogClientConnected(t *testing.T) {
	logger, handler := newTestLogger(slog.LevelInfo)
	rl := NewRequestLogger(logger)

	rl.LogClientConnected("app.example.com", "192.168.1.100:54321")

	records := handler.getRecords()
	if len(records) != 1 {
		t.Fatalf("expected 1 log record, got %d", len(records))
	}

	rec := records[0]
	if rec.Level != slog.LevelInfo {
		t.Errorf("expected level INFO, got %v", rec.Level)
	}
	if rec.Message != "Client connected" {
		t.Errorf("expected message 'Client connected', got %q", rec.Message)
	}
	if rec.Attrs["domain"] != "app.example.com" {
		t.Errorf("expected domain 'app.example.com', got %v", rec.Attrs["domain"])
	}
	if rec.Attrs["remote_addr"] != "192.168.1.100:54321" {
		t.Errorf("expected remote_addr '192.168.1.100:54321', got %v", rec.Attrs["remote_addr"])
	}
}

func TestRequestLogger_LogClientDisconnected(t *testing.T) {
	logger, handler := newTestLogger(slog.LevelInfo)
	rl := NewRequestLogger(logger)

	rl.LogClientDisconnected("app.example.com", "192.168.1.100:54321", "client closed connection")

	records := handler.getRecords()
	if len(records) != 1 {
		t.Fatalf("expected 1 log record, got %d", len(records))
	}

	rec := records[0]
	if rec.Level != slog.LevelInfo {
		t.Errorf("expected level INFO, got %v", rec.Level)
	}
	if rec.Message != "Client disconnected" {
		t.Errorf("expected message 'Client disconnected', got %q", rec.Message)
	}
	if rec.Attrs["domain"] != "app.example.com" {
		t.Errorf("expected domain 'app.example.com', got %v", rec.Attrs["domain"])
	}
	if rec.Attrs["remote_addr"] != "192.168.1.100:54321" {
		t.Errorf("expected remote_addr '192.168.1.100:54321', got %v", rec.Attrs["remote_addr"])
	}
	if rec.Attrs["reason"] != "client closed connection" {
		t.Errorf("expected reason 'client closed connection', got %v", rec.Attrs["reason"])
	}
}

func TestRequestLogger_LogDispatchError(t *testing.T) {
	logger, handler := newTestLogger(slog.LevelInfo)
	rl := NewRequestLogger(logger)

	testErr := errors.New("dispatch timeout")
	rl.LogDispatchError("msg-uuid-123", testErr)

	records := handler.getRecords()
	if len(records) != 1 {
		t.Fatalf("expected 1 log record, got %d", len(records))
	}

	rec := records[0]
	if rec.Level != slog.LevelError {
		t.Errorf("expected level ERROR, got %v", rec.Level)
	}
	if rec.Message != "Message dispatch failed" {
		t.Errorf("expected message 'Message dispatch failed', got %q", rec.Message)
	}
	if rec.Attrs["uuid"] != "msg-uuid-123" {
		t.Errorf("expected uuid 'msg-uuid-123', got %v", rec.Attrs["uuid"])
	}
	if rec.Attrs["error"] != testErr {
		t.Errorf("expected error %v, got %v", testErr, rec.Attrs["error"])
	}
}
