package common

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigFile_EmptyPath(t *testing.T) {
	cfg, err := LoadConfigFile("")
	if err != nil {
		t.Fatalf("LoadConfigFile(\"\") error = %v", err)
	}
	if cfg.AuthToken != "" {
		t.Errorf("AuthToken = %q, want empty", cfg.AuthToken)
	}
}

func TestLoadConfigFile_NonExistent(t *testing.T) {
	cfg, err := LoadConfigFile("/tmp/nonexistent-wormhole-config")
	if err != nil {
		t.Fatalf("LoadConfigFile() error = %v", err)
	}
	if cfg.AuthToken != "" {
		t.Errorf("AuthToken = %q, want empty", cfg.AuthToken)
	}
}

func TestLoadConfigFile_ValidEnvFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	os.WriteFile(path, []byte("AUTH_TOKEN=my-secret\n"), 0600)

	cfg, err := LoadConfigFile(path)
	if err != nil {
		t.Fatalf("LoadConfigFile() error = %v", err)
	}
	if cfg.AuthToken != "my-secret" {
		t.Errorf("AuthToken = %q, want %q", cfg.AuthToken, "my-secret")
	}
}

func TestLoadConfigFile_WithComments(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	content := "# Wormhole config\nAUTH_TOKEN=token123\n# end\n"
	os.WriteFile(path, []byte(content), 0600)

	cfg, err := LoadConfigFile(path)
	if err != nil {
		t.Fatalf("LoadConfigFile() error = %v", err)
	}
	if cfg.AuthToken != "token123" {
		t.Errorf("AuthToken = %q, want %q", cfg.AuthToken, "token123")
	}
}

func TestLoadConfigFile_WithSpaces(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	os.WriteFile(path, []byte("  AUTH_TOKEN = spaced-token  \n"), 0600)

	cfg, err := LoadConfigFile(path)
	if err != nil {
		t.Fatalf("LoadConfigFile() error = %v", err)
	}
	if cfg.AuthToken != "spaced-token" {
		t.Errorf("AuthToken = %q, want %q", cfg.AuthToken, "spaced-token")
	}
}

func TestLoadConfigFile_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	os.WriteFile(path, []byte(""), 0600)

	cfg, err := LoadConfigFile(path)
	if err != nil {
		t.Fatalf("LoadConfigFile() error = %v", err)
	}
	if cfg.AuthToken != "" {
		t.Errorf("AuthToken = %q, want empty", cfg.AuthToken)
	}
}

func TestLoadConfigFile_BlankLinesOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	os.WriteFile(path, []byte("\n\n\n"), 0600)

	cfg, err := LoadConfigFile(path)
	if err != nil {
		t.Fatalf("LoadConfigFile() error = %v", err)
	}
	if cfg.AuthToken != "" {
		t.Errorf("AuthToken = %q, want empty", cfg.AuthToken)
	}
}

func TestLoadConfigFile_UnknownKeysIgnored(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	os.WriteFile(path, []byte("UNKNOWN_KEY=value\nAUTH_TOKEN=real\n"), 0600)

	cfg, err := LoadConfigFile(path)
	if err != nil {
		t.Fatalf("LoadConfigFile() error = %v", err)
	}
	if cfg.AuthToken != "real" {
		t.Errorf("AuthToken = %q, want %q", cfg.AuthToken, "real")
	}
}

func TestDefaultConfigPath(t *testing.T) {
	path := DefaultConfigPath()
	if path == "" {
		t.Skip("could not determine home directory")
	}

	if filepath.Base(path) != DefaultConfigFile {
		t.Errorf("config file name = %q, want %q", filepath.Base(path), DefaultConfigFile)
	}
}
