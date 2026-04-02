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
