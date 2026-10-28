package client

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDashboardServesFiles(t *testing.T) {
	ui := NewWebUI(&Client{}, 0)

	for path, want := range map[string]string{
		"/":              `<script src="dashboard.js">`,
		"/dashboard.css": ".app-header",
		"/dashboard.js":  "function",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Host = "localhost"
		w := httptest.NewRecorder()
		ui.server.Handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), want) {
			t.Errorf("GET %s = %d, body missing %q", path, w.Code, want)
		}
	}
}

func TestHandleStatus(t *testing.T) {
	history := NewRequestHistory(1)
	history.Add(RequestLog{UUID: "a"})
	history.Add(RequestLog{UUID: "b"})

	ui := &WebUI{
		client: &Client{
			tunnelURL: "https://tunnel.example.com",
			domain:    "example.com",
			history:   history,
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	w := httptest.NewRecorder()

	ui.handleStatus(w, req)

	resp := w.Result()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if result["tunnelURL"] != "https://tunnel.example.com" {
		t.Errorf("tunnelURL = %q, want %q", result["tunnelURL"], "https://tunnel.example.com")
	}

	if result["domain"] != "example.com" {
		t.Errorf("domain = %q, want %q", result["domain"], "example.com")
	}

	// Counts past the one entry the history keeps.
	if result["totalRequests"] != float64(2) {
		t.Errorf("totalRequests = %v, want 2", result["totalRequests"])
	}
}

func TestHandleStatusEmptyFields(t *testing.T) {
	ui := &WebUI{
		client: &Client{history: NewRequestHistory(1)},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	w := httptest.NewRecorder()

	ui.handleStatus(w, req)

	var result map[string]any
	if err := json.NewDecoder(w.Result().Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if result["tunnelURL"] != "" {
		t.Errorf("tunnelURL = %q, want empty string", result["tunnelURL"])
	}

	if result["domain"] != "" {
		t.Errorf("domain = %q, want empty string", result["domain"])
	}
}

func TestHandleRequests(t *testing.T) {
	history := NewRequestHistory(100)
	history.Add(RequestLog{
		UUID:           "abc-123",
		Timestamp:      time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC),
		Method:         "GET",
		URL:            "/hello",
		StatusCode:     200,
		RequestHeaders: map[string][]string{"Accept": {"*/*"}},
		ResponseBody:   []byte("hello body"),
	})
	history.Add(RequestLog{
		UUID:       "def-456",
		Timestamp:  time.Date(2025, 1, 1, 12, 1, 0, 0, time.UTC),
		Method:     "POST",
		URL:        "/submit",
		StatusCode: 201,
	})

	ui := &WebUI{
		client: &Client{
			history: history,
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/requests", nil)
	w := httptest.NewRecorder()

	ui.handleRequests(w, req)

	resp := w.Result()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}
	if total := resp.Header.Get("X-Total-Requests"); total != "2" {
		t.Errorf("X-Total-Requests = %q, want %q", total, "2")
	}

	var logs []RequestLog
	if err := json.NewDecoder(resp.Body).Decode(&logs); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(logs) != 2 {
		t.Fatalf("got %d logs, want 2", len(logs))
	}

	if logs[0].UUID != "abc-123" {
		t.Errorf("logs[0].UUID = %q, want %q", logs[0].UUID, "abc-123")
	}

	if logs[1].UUID != "def-456" {
		t.Errorf("logs[1].UUID = %q, want %q", logs[1].UUID, "def-456")
	}

	// Headers and bodies are served per request, see TestHandleRequest.
	if logs[0].RequestHeaders != nil || logs[0].ResponseBody != nil {
		t.Errorf("list entry = %+v, want no headers or bodies", logs[0])
	}
	if logs[0].ResponseBodySize != len("hello body") {
		t.Errorf("ResponseBodySize = %d, want %d", logs[0].ResponseBodySize, len("hello body"))
	}
	if full, _ := history.Get("abc-123"); string(full.ResponseBody) != "hello body" {
		t.Errorf("history lost the body after listing: %q", full.ResponseBody)
	}
}

func TestHandleRequest(t *testing.T) {
	history := NewRequestHistory(100)
	history.Add(RequestLog{
		UUID:           "abc-123",
		Method:         "POST",
		URL:            "/hook",
		StatusCode:     200,
		RequestHeaders: map[string][]string{"Content-Type": {"application/json"}},
		RequestBody:    []byte(`{"a":1}`),
		ResponseBody:   []byte("ok"),
	})

	ui := NewWebUI(&Client{history: history}, 0)
	get := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Host = "localhost"
		w := httptest.NewRecorder()
		ui.server.Handler.ServeHTTP(w, req)
		return w
	}

	w := get("/api/requests/abc-123")
	var log RequestLog
	if err := json.NewDecoder(w.Body).Decode(&log); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	if log.UUID != "abc-123" || string(log.RequestBody) != `{"a":1}` || string(log.ResponseBody) != "ok" ||
		log.RequestHeaders["Content-Type"][0] != "application/json" {
		t.Errorf("request = %+v, want the full record", log)
	}

	if w := get("/api/requests/unknown"); w.Code != http.StatusNotFound {
		t.Errorf("unknown id status = %d, want 404", w.Code)
	}
}

func TestHandleRequestsDelete(t *testing.T) {
	history := NewRequestHistory(100)
	history.Add(RequestLog{UUID: "abc-123", Method: "GET", URL: "/hello", StatusCode: 200})

	ui := &WebUI{
		client: &Client{
			history: history,
			display: &mockDisplay{},
		},
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/requests", nil)
	w := httptest.NewRecorder()

	ui.handleRequests(w, req)

	if w.Result().StatusCode != http.StatusNoContent {
		t.Errorf("status = %d, want %d", w.Result().StatusCode, http.StatusNoContent)
	}

	if logs := history.GetRecent(100); len(logs) != 0 {
		t.Errorf("got %d logs after DELETE, want 0", len(logs))
	}
}

func TestHandleRequestsEmpty(t *testing.T) {
	history := NewRequestHistory(100)

	ui := &WebUI{
		client: &Client{
			history: history,
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/requests", nil)
	w := httptest.NewRecorder()

	ui.handleRequests(w, req)

	var logs []RequestLog
	if err := json.NewDecoder(w.Result().Body).Decode(&logs); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(logs) != 0 {
		t.Errorf("got %d logs, want 0", len(logs))
	}
}

func TestNewWebUI(t *testing.T) {
	client := &Client{}

	ui := NewWebUI(client, 8080)

	if ui.client != client {
		t.Error("client not set correctly")
	}

	if ui.server == nil {
		t.Fatal("server is nil")
	}

	if ui.server.Addr != "127.0.0.1:8080" {
		t.Errorf("server.Addr = %q, want %q", ui.server.Addr, "127.0.0.1:8080")
	}
}

func TestLocalHostOnly(t *testing.T) {
	handler := localHostOnly(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	tests := []struct {
		host string
		want int
	}{
		{"localhost:4040", http.StatusOK},
		{"127.0.0.1:4040", http.StatusOK},
		{"localhost", http.StatusOK},
		{"evil.example:4040", http.StatusForbidden},
		{"localhost.evil.example:4040", http.StatusForbidden},
		{"", http.StatusForbidden},
	}

	for _, tt := range tests {
		req := httptest.NewRequest(http.MethodGet, "/api/requests", nil)
		req.Host = tt.host
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		if w.Code != tt.want {
			t.Errorf("Host %q: status = %d, want %d", tt.host, w.Code, tt.want)
		}
	}
}

func TestWebUIStart_PortInUse(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	ui := NewWebUI(&Client{}, ln.Addr().(*net.TCPAddr).Port)

	if err := ui.Start(); err == nil {
		ui.Shutdown(context.Background())
		t.Fatal("Start() on a port in use returned nil, want error")
	}
}

// Messages are served per socket, never in the list the dashboard polls.
func TestHandleMessages(t *testing.T) {
	history := NewRequestHistory(100)
	history.Add(RequestLog{
		UUID:       "ws-1",
		Method:     "GET",
		URL:        "/socket",
		StatusCode: 101,
		Stream:     StreamInfo{Open: true, Messages: 1},
		Messages:   []WSMessage{{Type: "text", Size: 2, Payload: []byte("hi")}},
	})

	ui := NewWebUI(&Client{history: history}, 0)
	get := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Host = "localhost"
		w := httptest.NewRecorder()
		ui.server.Handler.ServeHTTP(w, req)
		return w
	}

	w := get("/api/requests/ws-1/messages")
	var messages []WSMessage
	if err := json.NewDecoder(w.Body).Decode(&messages); err != nil {
		t.Fatalf("decode messages: %v", err)
	}
	if len(messages) != 1 || string(messages[0].Payload) != "hi" {
		t.Errorf("messages = %+v, want one %q", messages, "hi")
	}

	if w := get("/api/requests/unknown/messages"); w.Code != http.StatusNotFound {
		t.Errorf("unknown id status = %d, want 404", w.Code)
	}

	list := get("/api/requests").Body.String()
	if strings.Contains(list, `"Messages":[`) || !strings.Contains(list, `"Stream":{"Open":true`) {
		t.Errorf("list = %s, want Stream without the messages", list)
	}
}
