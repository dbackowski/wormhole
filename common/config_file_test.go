package common

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAuthToken_EmptyPath(t *testing.T) {
	token, err := LoadAuthToken("")
	if err != nil {
		t.Fatalf("LoadAuthToken(\"\") error = %v", err)
	}
	if token != "" {
		t.Errorf("AuthToken = %q, want empty", token)
	}
}

func TestLoadAuthToken_NonExistent(t *testing.T) {
	token, err := LoadAuthToken("/tmp/nonexistent-wormhole-config")
	if err != nil {
		t.Fatalf("LoadAuthToken() error = %v", err)
	}
	if token != "" {
		t.Errorf("AuthToken = %q, want empty", token)
	}
}

func TestLoadAuthToken_ValidEnvFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	os.WriteFile(path, []byte("AUTH_TOKEN=my-secret\n"), 0600)

	token, err := LoadAuthToken(path)
	if err != nil {
		t.Fatalf("LoadAuthToken() error = %v", err)
	}
	if token != "my-secret" {
		t.Errorf("AuthToken = %q, want %q", token, "my-secret")
	}
}

func TestLoadAuthToken_WithComments(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	content := "# Wormhole config\nAUTH_TOKEN=token123\n# end\n"
	os.WriteFile(path, []byte(content), 0600)

	token, err := LoadAuthToken(path)
	if err != nil {
		t.Fatalf("LoadAuthToken() error = %v", err)
	}
	if token != "token123" {
		t.Errorf("AuthToken = %q, want %q", token, "token123")
	}
}

func TestLoadAuthToken_WithSpaces(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	os.WriteFile(path, []byte("  AUTH_TOKEN = spaced-token  \n"), 0600)

	token, err := LoadAuthToken(path)
	if err != nil {
		t.Fatalf("LoadAuthToken() error = %v", err)
	}
	if token != "spaced-token" {
		t.Errorf("AuthToken = %q, want %q", token, "spaced-token")
	}
}

func TestLoadAuthToken_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	os.WriteFile(path, []byte(""), 0600)

	token, err := LoadAuthToken(path)
	if err != nil {
		t.Fatalf("LoadAuthToken() error = %v", err)
	}
	if token != "" {
		t.Errorf("AuthToken = %q, want empty", token)
	}
}

func TestLoadAuthToken_BlankLinesOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	os.WriteFile(path, []byte("\n\n\n"), 0600)

	token, err := LoadAuthToken(path)
	if err != nil {
		t.Fatalf("LoadAuthToken() error = %v", err)
	}
	if token != "" {
		t.Errorf("AuthToken = %q, want empty", token)
	}
}

func TestLoadAuthToken_UnknownKeysIgnored(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	os.WriteFile(path, []byte("UNKNOWN_KEY=value\nAUTH_TOKEN=real\n"), 0600)

	token, err := LoadAuthToken(path)
	if err != nil {
		t.Fatalf("LoadAuthToken() error = %v", err)
	}
	if token != "real" {
		t.Errorf("AuthToken = %q, want %q", token, "real")
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
