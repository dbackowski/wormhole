package client

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

)

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}
	os.Stdout = w

	fn()

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
	return buf.String()
}

func TestNewTerminalDisplay(t *testing.T) {
	td := NewTerminalDisplay(10)

	if td.maxLogs != 10 {
		t.Errorf("maxLogs = %d, want 10", td.maxLogs)
	}
}

func TestShowConnectionInfo(t *testing.T) {
	td := NewTerminalDisplay(5)

	output := captureStdout(t, func() {
		td.ShowConnectionInfo("https://myapp.example.com", 4040)
	})

	expectedLines := []string{
		"Connected to server.",
		"Your tunnel is available at: https://myapp.example.com",
		"Web UI is available at: http://localhost:4040",
		"Waiting for incoming HTTP requests...",
	}

	for _, line := range expectedLines {
		if !strings.Contains(output, line) {
			t.Errorf("output missing expected line %q\ngot:\n%s", line, output)
		}
	}
}

func TestShowRequestHistory(t *testing.T) {
	ts := time.Date(2025, 1, 15, 10, 30, 45, 0, time.UTC)

	tests := []struct {
		name     string
		maxLogs  int
		logs     []RequestLog
		expected []string
	}{
		{
			name:    "single request",
			maxLogs: 5,
			logs: []RequestLog{
				{Timestamp: ts, Method: "GET", URL: "/api/users", StatusCode: 200},
			},
			expected: []string{
				"last 5 requests",
				fmt.Sprintf("%s GET /api/users -> 200", FormatTime(ts)),
			},
		},
		{
			name:    "multiple requests",
			maxLogs: 10,
			logs: []RequestLog{
				{Timestamp: ts, Method: "GET", URL: "/", StatusCode: 200},
				{Timestamp: ts, Method: "POST", URL: "/api/data", StatusCode: 201},
				{Timestamp: ts, Method: "DELETE", URL: "/api/items/1", StatusCode: 404},
			},
			expected: []string{
				"last 10 requests",
				"GET / -> 200",
				"POST /api/data -> 201",
				"DELETE /api/items/1 -> 404",
			},
		},
		{
			name:    "empty logs",
			maxLogs: 5,
			logs:    []RequestLog{},
			expected: []string{
				"last 5 requests",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			td := NewTerminalDisplay(tt.maxLogs)

			output := captureStdout(t, func() {
				td.ShowRequestHistory(tt.logs)
			})

			for _, exp := range tt.expected {
				if !strings.Contains(output, exp) {
					t.Errorf("output missing expected string %q\ngot:\n%s", exp, output)
				}
			}
		})
	}
}

func TestTerminalDisplayImplementsDisplayInterface(t *testing.T) {
	var _ Display = (*TerminalDisplay)(nil)
}

func TestFormatTime(t *testing.T) {
	testTime := time.Date(2024, 6, 15, 14, 30, 45, 0, time.UTC)

	got := FormatTime(testTime)
	want := "2024-06-15 14:30:45"

	if got != want {
		t.Errorf("FormatTime() = %q, want %q", got, want)
	}
}

func TestClearTerminal(t *testing.T) {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	ClearTerminal()

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	buf.ReadFrom(r)

	want := "\x1bc"
	if got := buf.String(); got != want {
		t.Errorf("ClearTerminal() wrote %q, want %q", got, want)
	}
}
