package common

import (
	"net/http"
	"testing"
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

func TestRemoveHopByHopHeaders(t *testing.T) {
	tests := []struct {
		name    string
		headers http.Header
		removed []string
		kept    []string
	}{
		{
			name: "fixed hop-by-hop set",
			headers: http.Header{
				"Connection":          []string{"keep-alive"},
				"Keep-Alive":          []string{"timeout=5"},
				"Proxy-Authenticate":  []string{"Basic"},
				"Proxy-Authorization": []string{"Basic abc"},
				"Te":                  []string{"trailers"},
				"Trailer":             []string{"Expires"},
				"Transfer-Encoding":   []string{"chunked"},
				"Upgrade":             []string{"h2c"},
				"Content-Type":        []string{"application/json"},
			},
			removed: []string{"Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization", "Te", "Trailer", "Transfer-Encoding", "Upgrade"},
			kept:    []string{"Content-Type"},
		},
		{
			name: "headers named in Connection are removed",
			headers: http.Header{
				"Connection": []string{"keep-alive, X-Hop-One", "X-Hop-Two"},
				"X-Hop-One":  []string{"drop me"},
				"X-Hop-Two":  []string{"drop me too"},
				"X-Keep":     []string{"stay"},
			},
			removed: []string{"Connection", "X-Hop-One", "X-Hop-Two"},
			kept:    []string{"X-Keep"},
		},
		{
			name:    "no hop-by-hop headers",
			headers: http.Header{"Content-Type": []string{"text/plain"}, "X-Custom": []string{"v"}},
			removed: nil,
			kept:    []string{"Content-Type", "X-Custom"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			RemoveHopByHopHeaders(tt.headers)

			for _, k := range tt.removed {
				if _, ok := tt.headers[http.CanonicalHeaderKey(k)]; ok {
					t.Errorf("header %q should have been removed", k)
				}
			}
			for _, k := range tt.kept {
				if tt.headers.Get(k) == "" {
					t.Errorf("header %q should have been kept", k)
				}
			}
		})
	}
}

