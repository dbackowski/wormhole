package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// buildBinary compiles the client into a temp dir and returns its path.
// The version string is injected via -ldflags so we can assert on it.
func buildBinary(t *testing.T, version string) string {
	t.Helper()

	dir := t.TempDir()
	bin := filepath.Join(dir, "wormhole-client")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}

	cmd := exec.Command("go", "build",
		"-ldflags", "-X main.version="+version,
		"-o", bin,
		".",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go build failed: %v\n%s", err, out)
	}
	return bin
}

func TestMain_VersionFlag(t *testing.T) {
	bin := buildBinary(t, "1.2.3-test")

	out, err := exec.Command(bin, "-version").CombinedOutput()
	if err != nil {
		t.Fatalf("running -version failed: %v\n%s", err, out)
	}

	got := strings.TrimSpace(string(out))
	if got != "1.2.3-test" {
		t.Errorf("version output = %q, want %q", got, "1.2.3-test")
	}
}

func TestMain_MissingDomain_ExitsWithError(t *testing.T) {
	bin := buildBinary(t, "dev")

	// No -domain provided: NewClient must fail validation before any
	// network call, main prints the error and exits 1.
	cmd := exec.Command(bin, "-local", "http://localhost:8080")
	out, err := cmd.CombinedOutput()

	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("expected *exec.ExitError, got %T: %v\noutput: %s", err, err, out)
	}
	if exitErr.ExitCode() != 1 {
		t.Errorf("exit code = %d, want 1", exitErr.ExitCode())
	}

	if !strings.Contains(string(out), "Error creating client") {
		t.Errorf("output missing error prefix; got: %s", out)
	}
	if !strings.Contains(string(out), "domain is required") {
		t.Errorf("output missing validation message; got: %s", out)
	}
}

func TestMain_InvalidLocalURL_ExitsWithError(t *testing.T) {
	bin := buildBinary(t, "dev")

	cmd := exec.Command(bin, "-domain", "myapp", "-local", "not-a-url")
	out, err := cmd.CombinedOutput()

	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("expected *exec.ExitError, got %T: %v\noutput: %s", err, err, out)
	}
	if exitErr.ExitCode() != 1 {
		t.Errorf("exit code = %d, want 1", exitErr.ExitCode())
	}
	if !strings.Contains(string(out), "invalid local URL") {
		t.Errorf("output missing 'invalid local URL'; got: %s", out)
	}
}

// A reader who runs the client without a token gets told the server needs
// one, not just that authentication failed. With a token, the hint would be
// wrong, so it is left out.
func TestMain_UnauthorizedWithoutTokenHints(t *testing.T) {
	bin := buildBinary(t, "dev")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
	}))
	defer srv.Close()
	serverURL := strings.Replace(srv.URL, "127.0.0.1", "localhost", 1)
	hint := "requires a token"

	for _, tt := range []struct {
		name     string
		args     []string
		wantHint bool
	}{
		{"no token", nil, true},
		{"wrong token", []string{"-auth-token=wrong"}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			args := append([]string{"-server=" + serverURL, "-domain=myapp", "-local=http://localhost:3000"}, tt.args...)
			cmd := exec.Command(bin, args...)
			// An empty HOME, so a token in the developer's own config is not read.
			cmd.Env = append(os.Environ(), "HOME="+t.TempDir())
			out, err := cmd.CombinedOutput()
			if _, ok := err.(*exec.ExitError); !ok {
				t.Fatalf("expected exit error, got %v\n%s", err, out)
			}
			if !strings.Contains(string(out), "authentication failed") {
				t.Fatalf("output missing auth error: %s", out)
			}
			if got := strings.Contains(string(out), hint); got != tt.wantHint {
				t.Errorf("hint shown = %v, want %v; output: %s", got, tt.wantHint, out)
			}
		})
	}
}
