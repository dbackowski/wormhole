package common

import (
	"bytes"
	"net/http"
	"os"
	"testing"
	"time"
)

func TestValidatePort(t *testing.T) {
	tests := []struct {
		name    string
		port    int
		wantErr bool
	}{
		{"valid min port", 1, false},
		{"valid max port", 65535, false},
		{"valid common port", 8080, false},
		{"invalid zero", 0, true},
		{"invalid negative", -1, true},
		{"invalid too high", 65536, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePort(tt.port)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidatePort(%d) error = %v, wantErr %v", tt.port, err, tt.wantErr)
			}
		})
	}
}

func TestFormatTime(t *testing.T) {
	testTime := time.Date(2024, 6, 15, 14, 30, 45, 0, time.UTC)

	got := FormatTime(testTime)
	want := "2024-06-15 14:30:45"

	if got != want {
		t.Errorf("FormatTime() = %q, want %q", got, want)
	}
}

func TestCopyHTTPHeaders(t *testing.T) {
	src := http.Header{
		"Content-Type": []string{"application/json"},
		"X-Custom":     []string{"value1", "value2"},
		"Host":         []string{"example.com"}, // should be skipped
	}
	dest := make(http.Header)

	CopyHTTPHeaders(src, dest)

	if got := dest.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want %q", got, "application/json")
	}

	if got := dest["X-Custom"]; len(got) != 2 {
		t.Errorf("X-Custom has %d values, want 2", len(got))
	}

	if dest.Get("Host") != "" {
		t.Error("Host header should not be copied")
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
