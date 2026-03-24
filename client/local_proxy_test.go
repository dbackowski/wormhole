package client

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func mustParseURL(t *testing.T, rawURL string) url.URL {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("failed to parse URL %q: %v", rawURL, err)
	}
	return *u
}

func TestNewLocalProxy(t *testing.T) {
	lp := NewLocalProxy(mustParseURL(t, "http://localhost:3000"), "https://tunnel.example.com", 5*time.Second)

	if lp.baseURL.String() != "http://localhost:3000" {
		t.Errorf("baseURL = %q, want %q", lp.baseURL.String(), "http://localhost:3000")
	}
	if lp.tunnelURL != "https://tunnel.example.com" {
		t.Errorf("tunnelURL = %q, want %q", lp.tunnelURL, "https://tunnel.example.com")
	}
	if lp.httpClient == nil {
		t.Fatal("httpClient is nil")
	}
	if lp.httpClient.Timeout != 5*time.Second {
		t.Errorf("Timeout = %v, want %v", lp.httpClient.Timeout, 5*time.Second)
	}
}

func TestForward(t *testing.T) {
	tests := []struct {
		name           string
		handler        http.HandlerFunc
		request        ProxyRequest
		wantStatusCode int
		wantBody       string
		wantErr        bool
		errContains    string
	}{
		{
			name: "successful GET request",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Custom", "test-value")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("hello"))
			},
			request: ProxyRequest{
				Method:  "GET",
				URL:     "/path",
				Headers: map[string][]string{"Host": {"example.com"}},
			},
			wantStatusCode: http.StatusOK,
			wantBody:       "hello",
		},
		{
			name: "successful POST with body",
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" {
					t.Errorf("method = %q, want POST", r.Method)
				}
				buf := make([]byte, 1024)
				n, _ := r.Body.Read(buf)
				w.WriteHeader(http.StatusCreated)
				w.Write(buf[:n])
			},
			request: ProxyRequest{
				Method:  "POST",
				URL:     "/submit",
				Headers: map[string][]string{"Host": {"example.com"}},
				Body:    []byte("request-body"),
			},
			wantStatusCode: http.StatusCreated,
			wantBody:       "request-body",
		},
		{
			name:    "missing Host header",
			handler: func(w http.ResponseWriter, r *http.Request) {},
			request: ProxyRequest{
				Method:  "GET",
				URL:     "/path",
				Headers: map[string][]string{},
			},
			wantErr:     true,
			errContains: "missing required Host header",
		},
		{
			name:    "nil headers",
			handler: func(w http.ResponseWriter, r *http.Request) {},
			request: ProxyRequest{
				Method:  "GET",
				URL:     "/path",
				Headers: nil,
			},
			wantErr:     true,
			errContains: "missing required Host header",
		},
		{
			name:    "empty Host header slice",
			handler: func(w http.ResponseWriter, r *http.Request) {},
			request: ProxyRequest{
				Method:  "GET",
				URL:     "/path",
				Headers: map[string][]string{"Host": {}},
			},
			wantErr:     true,
			errContains: "missing required Host header",
		},
		{
			name: "headers are forwarded",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Got-Auth", r.Header.Get("Authorization"))
				w.Header().Set("X-Got-Content-Type", r.Header.Get("Content-Type"))
				w.WriteHeader(http.StatusOK)
			},
			request: ProxyRequest{
				Method: "GET",
				URL:    "/path",
				Headers: map[string][]string{
					"Host":          {"example.com"},
					"Authorization": {"Bearer token123"},
					"Content-Type":  {"application/json"},
				},
			},
			wantStatusCode: http.StatusOK,
		},
		{
			name: "Host header is set on request",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Got-Host", r.Host)
				w.WriteHeader(http.StatusOK)
			},
			request: ProxyRequest{
				Method:  "GET",
				URL:     "/path",
				Headers: map[string][]string{"Host": {"custom.example.com"}},
			},
			wantStatusCode: http.StatusOK,
		},
		{
			name: "DELETE method",
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "DELETE" {
					t.Errorf("method = %q, want DELETE", r.Method)
				}
				w.WriteHeader(http.StatusNoContent)
			},
			request: ProxyRequest{
				Method:  "DELETE",
				URL:     "/resource/1",
				Headers: map[string][]string{"Host": {"example.com"}},
			},
			wantStatusCode: http.StatusNoContent,
		},
		{
			name: "PUT method",
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "PUT" {
					t.Errorf("method = %q, want PUT", r.Method)
				}
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("updated"))
			},
			request: ProxyRequest{
				Method:  "PUT",
				URL:     "/resource/1",
				Headers: map[string][]string{"Host": {"example.com"}},
				Body:    []byte("update-data"),
			},
			wantStatusCode: http.StatusOK,
			wantBody:       "updated",
		},
		{
			name: "server returns 500",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte("internal error"))
			},
			request: ProxyRequest{
				Method:  "GET",
				URL:     "/fail",
				Headers: map[string][]string{"Host": {"example.com"}},
			},
			wantStatusCode: http.StatusInternalServerError,
			wantBody:       "internal error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			lp := NewLocalProxy(mustParseURL(t, server.URL), "https://tunnel.example.com", 5*time.Second)
			resp, err := lp.Forward(tt.request)

			if (err != nil) != tt.wantErr {
				t.Fatalf("Forward() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				if tt.errContains != "" && err != nil {
					if got := err.Error(); !contains(got, tt.errContains) {
						t.Errorf("error = %q, want containing %q", got, tt.errContains)
					}
				}
				return
			}
			if resp.StatusCode != tt.wantStatusCode {
				t.Errorf("StatusCode = %d, want %d", resp.StatusCode, tt.wantStatusCode)
			}
			if tt.wantBody != "" && string(resp.Body) != tt.wantBody {
				t.Errorf("Body = %q, want %q", string(resp.Body), tt.wantBody)
			}
		})
	}
}

func TestForwardRedirectNotFollowed(t *testing.T) {
	redirectServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/redirected", http.StatusFound)
	}))
	defer redirectServer.Close()

	lp := NewLocalProxy(mustParseURL(t, redirectServer.URL), "https://tunnel.example.com", 5*time.Second)
	resp, err := lp.Forward(ProxyRequest{
		Method:  "GET",
		URL:     "/original",
		Headers: map[string][]string{"Host": {"example.com"}},
	})

	if err != nil {
		t.Fatalf("Forward() unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusFound {
		t.Errorf("StatusCode = %d, want %d (redirect should not be followed)", resp.StatusCode, http.StatusFound)
	}
	if loc := http.Header(resp.Headers).Get("Location"); loc != "/redirected" {
		t.Errorf("Location header = %q, want %q", loc, "/redirected")
	}
}

func TestForwardRequestToUnreachableServer(t *testing.T) {
	lp := NewLocalProxy(mustParseURL(t, "http://127.0.0.1:1"), "https://tunnel.example.com", 1*time.Second)
	_, err := lp.Forward(ProxyRequest{
		Method:  "GET",
		URL:     "/path",
		Headers: map[string][]string{"Host": {"example.com"}},
	})

	if err == nil {
		t.Fatal("Forward() expected error for unreachable server, got nil")
	}
	if !contains(err.Error(), "request failed") {
		t.Errorf("error = %q, want containing %q", err.Error(), "request failed")
	}
}

func TestForwardResponseHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Custom-Header", "custom-value")
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer server.Close()

	lp := NewLocalProxy(mustParseURL(t, server.URL), "https://tunnel.example.com", 5*time.Second)
	resp, err := lp.Forward(ProxyRequest{
		Method:  "GET",
		URL:     "/",
		Headers: map[string][]string{"Host": {"example.com"}},
	})

	if err != nil {
		t.Fatalf("Forward() unexpected error: %v", err)
	}
	headers := http.Header(resp.Headers)
	if got := headers.Get("X-Custom-Header"); got != "custom-value" {
		t.Errorf("X-Custom-Header = %q, want %q", got, "custom-value")
	}
	if got := headers.Get("Content-Type"); got != "text/plain" {
		t.Errorf("Content-Type = %q, want %q", got, "text/plain")
	}
}

func TestForwardURLPath(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	lp := NewLocalProxy(mustParseURL(t, server.URL), "https://tunnel.example.com", 5*time.Second)
	_, err := lp.Forward(ProxyRequest{
		Method:  "GET",
		URL:     "/api/v1/users",
		Headers: map[string][]string{"Host": {"example.com"}},
	})

	if err != nil {
		t.Fatalf("Forward() unexpected error: %v", err)
	}
	if gotPath != "/api/v1/users" {
		t.Errorf("request path = %q, want %q", gotPath, "/api/v1/users")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
