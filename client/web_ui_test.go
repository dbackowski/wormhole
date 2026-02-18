package client

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHandleDashboard(t *testing.T) {
	ui := &WebUI{
		dashboardHTML: "<html><body>test dashboard</body></html>",
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	ui.handleDashboard(w, req)

	resp := w.Result()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	if ct := resp.Header.Get("Content-Type"); ct != "text/html" {
		t.Errorf("Content-Type = %q, want %q", ct, "text/html")
	}

	if body := w.Body.String(); body != ui.dashboardHTML {
		t.Errorf("body = %q, want %q", body, ui.dashboardHTML)
	}
}

func TestHandleStatus(t *testing.T) {
	ui := &WebUI{
		client: &Client{
			tunnelURL: "https://tunnel.example.com",
			domain:    "example.com",
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

	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if result["tunnelURL"] != "https://tunnel.example.com" {
		t.Errorf("tunnelURL = %q, want %q", result["tunnelURL"], "https://tunnel.example.com")
	}

	if result["domain"] != "example.com" {
		t.Errorf("domain = %q, want %q", result["domain"], "example.com")
	}
}

func TestHandleStatusEmptyFields(t *testing.T) {
	ui := &WebUI{
		client: &Client{},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	w := httptest.NewRecorder()

	ui.handleStatus(w, req)

	var result map[string]string
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
		UUID:       "abc-123",
		Timestamp:  time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC),
		Method:     "GET",
		URL:        "/hello",
		StatusCode: 200,
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

func TestBuildDashboardHTML(t *testing.T) {
	ui := &WebUI{}

	html, err := ui.buildDashboardHTML()
	if err != nil {
		t.Fatalf("buildDashboardHTML() error: %v", err)
	}

	if html == "" {
		t.Fatal("buildDashboardHTML() returned empty string")
	}

	if !strings.Contains(html, "<html") {
		t.Error("dashboard HTML does not contain <html tag")
	}

	if strings.Contains(html, "{{CSS}}") {
		t.Error("dashboard HTML still contains {{CSS}} placeholder")
	}

	if strings.Contains(html, "{{JS}}") {
		t.Error("dashboard HTML still contains {{JS}} placeholder")
	}
}

func TestNewWebUI(t *testing.T) {
	client := &Client{}

	ui, err := NewWebUI(client, 8080)
	if err != nil {
		t.Fatalf("NewWebUI() error: %v", err)
	}

	if ui.client != client {
		t.Error("client not set correctly")
	}

	if ui.dashboardHTML == "" {
		t.Error("dashboardHTML is empty")
	}

	if ui.server == nil {
		t.Fatal("server is nil")
	}

	if ui.server.Addr != ":8080" {
		t.Errorf("server.Addr = %q, want %q", ui.server.Addr, ":8080")
	}
}
