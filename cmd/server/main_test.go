package main

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// buildBinary compiles the server into a temp dir and returns its path.
// The version string is injected via -ldflags so we can assert on it.
func buildBinary(t *testing.T, version string) string {
	t.Helper()

	dir := t.TempDir()
	bin := filepath.Join(dir, "wormhole-server")
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

	// main() clears the screen first, so the version is embedded rather than
	// being the whole output; assert on containment.
	if !strings.Contains(string(out), "1.2.3-test") {
		t.Errorf("version output = %q, want it to contain %q", out, "1.2.3-test")
	}
}

func TestMain_InvalidPort_ExitsWithError(t *testing.T) {
	bin := buildBinary(t, "dev")

	// Port above the valid range: NewServer fails config validation before
	// binding anything, main prints the error and exits 1.
	cmd := exec.Command(bin, "-port", "99999")
	out, err := cmd.CombinedOutput()

	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("expected *exec.ExitError, got %T: %v\noutput: %s", err, err, out)
	}
	if exitErr.ExitCode() != 1 {
		t.Errorf("exit code = %d, want 1", exitErr.ExitCode())
	}

	if !strings.Contains(string(out), "Failed to create server") {
		t.Errorf("output missing error prefix; got: %s", out)
	}
	if !strings.Contains(string(out), "invalid port") {
		t.Errorf("output missing validation message; got: %s", out)
	}
}
