package client

import (
	"flag"
	"os"
	"testing"

	"github.com/dbackowski/wormhole/common"
)

func TestParseURL(t *testing.T) {
	tests := []struct {
		name       string
		rawURL     string
		wantHost   string
		wantScheme string
		wantErr    bool
	}{
		{"valid http url", "http://localhost:3000", "localhost:3000", "http", false},
		{"valid https url", "https://example.com", "example.com", "https", false},
		{"valid https url with port", "https://example.com:443", "example.com:443", "https", false},
		{"empty url", "", "", "", true},
		{"unsupported scheme", "ftp://example.com", "", "", true},
		{"missing scheme", "example.com", "", "", true},
		{"missing host", "http://", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed, err := parseURL(tt.rawURL)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseURL(%q) error = %v, wantErr %v", tt.rawURL, err, tt.wantErr)
				return
			}
			if err != nil {
				return
			}
			if parsed.Host != tt.wantHost {
				t.Errorf("parseURL(%q).Host = %q, want %q", tt.rawURL, parsed.Host, tt.wantHost)
			}
			if parsed.Scheme != tt.wantScheme {
				t.Errorf("parseURL(%q).Scheme = %q, want %q", tt.rawURL, parsed.Scheme, tt.wantScheme)
			}
		})
	}
}

func TestValidateAndParseServerURL(t *testing.T) {
	tests := []struct {
		name           string
		rawURL         string
		wantHTTPScheme string
		wantWSScheme   string
		wantHost       string
		wantErr        bool
	}{
		{"http maps to ws", "http://localhost:8080", "http", "ws", "localhost:8080", false},
		{"https maps to wss", "https://example.com", "https", "wss", "example.com", false},
		{"empty url", "", "", "", "", true},
		{"invalid url", "not-a-url", "", "", "", true},
		{"unsupported scheme", "ftp://example.com", "", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := validateAndParseServerURL(tt.rawURL)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateAndParseServerURL(%q) error = %v, wantErr %v", tt.rawURL, err, tt.wantErr)
				return
			}
			if err != nil {
				return
			}
			if cfg.HTTPScheme != tt.wantHTTPScheme {
				t.Errorf("HTTPScheme = %q, want %q", cfg.HTTPScheme, tt.wantHTTPScheme)
			}
			if cfg.WSScheme != tt.wantWSScheme {
				t.Errorf("WSScheme = %q, want %q", cfg.WSScheme, tt.wantWSScheme)
			}
			if cfg.Host != tt.wantHost {
				t.Errorf("Host = %q, want %q", cfg.Host, tt.wantHost)
			}
		})
	}
}

func TestParseFlags(t *testing.T) {
	tests := []struct {
		name          string
		args          []string
		wantServerURL string
		wantDomain    string
		wantLocal     string
		wantWebUIPort int
	}{
		{
			"defaults",
			[]string{"cmd"},
			common.DefaultClientServerURL,
			"",
			"",
			common.DefaultWebUIPort,
		},
		{
			"all flags",
			[]string{"cmd", "-server", "http://localhost:8080", "-domain", "myapp", "-local", "http://localhost:3000", "-webui-port", "5050"},
			"http://localhost:8080",
			"myapp",
			"http://localhost:3000",
			5050,
		},
		{
			"partial flags",
			[]string{"cmd", "-domain", "test", "-local", "http://localhost:9000"},
			common.DefaultClientServerURL,
			"test",
			"http://localhost:9000",
			common.DefaultWebUIPort,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			flag.CommandLine = flag.NewFlagSet(tt.args[0], flag.ContinueOnError)
			os.Args = tt.args

			cfg := ParseFlags()

			if cfg.ServerURL != tt.wantServerURL {
				t.Errorf("ServerURL = %q, want %q", cfg.ServerURL, tt.wantServerURL)
			}
			if cfg.Domain != tt.wantDomain {
				t.Errorf("Domain = %q, want %q", cfg.Domain, tt.wantDomain)
			}
			if cfg.Local != tt.wantLocal {
				t.Errorf("Local = %q, want %q", cfg.Local, tt.wantLocal)
			}
			if cfg.WebUIPort != tt.wantWebUIPort {
				t.Errorf("WebUIPort = %d, want %d", cfg.WebUIPort, tt.wantWebUIPort)
			}
		})
	}
}

func TestValidateClientConfig(t *testing.T) {
	tests := []struct {
		name    string
		domain  string
		local   string
		port    int
		wantErr bool
	}{
		{"valid config", "myapp", "http://localhost:3000", 4040, false},
		{"empty domain", "", "http://localhost:3000", 4040, true},
		{"empty local", "myapp", "", 4040, true},
		{"invalid local url", "myapp", "not-a-url", 4040, true},
		{"invalid port zero", "myapp", "http://localhost:3000", 0, true},
		{"invalid port too high", "myapp", "http://localhost:3000", 65536, true},
		{"invalid port negative", "myapp", "http://localhost:3000", -1, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := validateClientConfig(tt.domain, tt.local, tt.port)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateClientConfig(%q, %q, %d) error = %v, wantErr %v", tt.domain, tt.local, tt.port, err, tt.wantErr)
			}
		})
	}
}
