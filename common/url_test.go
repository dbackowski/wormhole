package common

import (
	"testing"
)

func TestJoinURLPath(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
		urlPath string
		want    string
		wantErr bool
	}{
		{"simple path", "https://example.com", "api", "https://example.com/api", false},
		{"path with leading slash", "https://example.com", "/api", "https://example.com/api", false},
		{"base with trailing slash", "https://example.com/", "api", "https://example.com/api", false},
		{"nested path", "https://example.com/v1", "users/123", "https://example.com/v1/users/123", false},
		{"empty path", "https://example.com", "", "https://example.com", false},
		{"base with port", "https://example.com:8080", "api", "https://example.com:8080/api", false},
		{"base with query", "https://example.com?foo=bar", "api", "https://example.com/api?foo=bar", false},
		{"invalid base URL", "://invalid", "api", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := JoinURLPath(tt.baseURL, tt.urlPath)
			if (err != nil) != tt.wantErr {
				t.Errorf("JoinURLPath() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("JoinURLPath() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildSubdomainURL(t *testing.T) {
	tests := []struct {
		name      string
		scheme    string
		subdomain string
		host      string
		urlPath   string
		want      string
	}{
		{"simple subdomain", "https", "api", "example.com", "", "https://api.example.com"},
		{"with path", "https", "api", "example.com", "/v1/users", "https://api.example.com/v1/users"},
		{"http scheme", "http", "test", "localhost", "/health", "http://test.localhost/health"},
		{"nested subdomain", "https", "staging.api", "example.com", "", "https://staging.api.example.com"},
		{"empty path", "https", "www", "example.com", "", "https://www.example.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildSubdomainURL(tt.scheme, tt.subdomain, tt.host, tt.urlPath)
			if got != tt.want {
				t.Errorf("BuildSubdomainURL() = %q, want %q", got, tt.want)
			}
		})
	}
}
