package server

import (
	"flag"
	"os"
	"testing"

	"github.com/dbackowski/wormhole/common"
)

func resetFlags() {
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
}

func TestParseFlags_Defaults(t *testing.T) {
	resetFlags()
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	os.Args = []string{"cmd"}

	cfg := ParseFlags("test")

	if cfg.Port != common.DefaultServerPort {
		t.Errorf("Port = %d, want %d", cfg.Port, common.DefaultServerPort)
	}
	if cfg.Debug != false {
		t.Errorf("Debug = %v, want false", cfg.Debug)
	}
}

func TestParseFlags_CustomPort(t *testing.T) {
	resetFlags()
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	os.Args = []string{"cmd", "-port", "9090"}

	cfg := ParseFlags("test")

	if cfg.Port != 9090 {
		t.Errorf("Port = %d, want 9090", cfg.Port)
	}
}

func TestParseFlags_DebugEnabled(t *testing.T) {
	resetFlags()
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	os.Args = []string{"cmd", "-debug"}

	cfg := ParseFlags("test")

	if !cfg.Debug {
		t.Error("Debug = false, want true")
	}
}

func TestParseFlags_AllFlags(t *testing.T) {
	resetFlags()
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	os.Args = []string{"cmd", "-port", "1234", "-debug"}

	cfg := ParseFlags("test")

	if cfg.Port != 1234 {
		t.Errorf("Port = %d, want 1234", cfg.Port)
	}
	if !cfg.Debug {
		t.Error("Debug = false, want true")
	}
}

func TestParseFlags_EnvFallback(t *testing.T) {
	resetFlags()
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	os.Args = []string{"cmd"}
	t.Setenv("AUTH_TOKEN", "env-token")
	t.Setenv("HOST", "wormhole.example")

	cfg := ParseFlags("test")

	if cfg.AuthToken != "env-token" {
		t.Errorf("AuthToken = %q, want %q", cfg.AuthToken, "env-token")
	}
	if cfg.Host != "wormhole.example" {
		t.Errorf("Host = %q, want %q", cfg.Host, "wormhole.example")
	}
}

func TestParseFlags_FlagOverridesEnv(t *testing.T) {
	resetFlags()
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	os.Args = []string{"cmd", "-auth-token", "flag-token", "-host", "flag.example"}
	t.Setenv("AUTH_TOKEN", "env-token")
	t.Setenv("HOST", "env.example")

	cfg := ParseFlags("test")

	if cfg.AuthToken != "flag-token" {
		t.Errorf("AuthToken = %q, want %q (flag should win over env)", cfg.AuthToken, "flag-token")
	}
	if cfg.Host != "flag.example" {
		t.Errorf("Host = %q, want %q (flag should win over env)", cfg.Host, "flag.example")
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		port    int
		wantErr bool
	}{
		{"valid min port", common.MinValidPort, false},
		{"valid max port", common.MaxValidPort, false},
		{"valid default port", common.DefaultServerPort, false},
		{"invalid zero", 0, true},
		{"invalid negative", -1, true},
		{"invalid too high", common.MaxValidPort + 1, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{Port: tt.port}
			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
