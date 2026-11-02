package common

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	DefaultConfigDir  = ".wormhole"
	DefaultConfigFile = "config"
)

func DefaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, DefaultConfigDir, DefaultConfigFile)
}

// AuthTokenFromConfigFile returns AUTH_TOKEN from the default config file, or
// "" if there is none. A file that cannot be read is reported on stderr.
func AuthTokenFromConfigFile() string {
	token, err := LoadAuthToken(DefaultConfigPath())
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to load config file: %v\n", err)
	}
	return token
}

// LoadAuthToken reads AUTH_TOKEN from the config file at path. A missing file
// or empty path gives "".
func LoadAuthToken(path string) (string, error) {
	if path == "" {
		return "", nil
	}

	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("reading config file: %w", err)
	}
	defer file.Close()

	token := ""
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}

		if strings.TrimSpace(key) == "AUTH_TOKEN" {
			token = strings.TrimSpace(value)
		}
	}

	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("reading config file: %w", err)
	}

	return token, nil
}

// SaveAuthToken sets AUTH_TOKEN in the config file at path, keeping its other
// lines. The file holds a credential, so it is written owner-only, and via a
// rename so a crash cannot leave it half-written.
func SaveAuthToken(path, token string) error {
	if path == "" {
		return fmt.Errorf("no home directory to save the token in")
	}

	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("reading config file: %w", err)
	}

	var lines []string
	for line := range strings.Lines(string(data)) {
		key, _, _ := strings.Cut(line, "=")
		if strings.TrimSpace(key) != "AUTH_TOKEN" {
			lines = append(lines, strings.TrimRight(line, "\n"))
		}
	}
	lines = append(lines, "AUTH_TOKEN="+token)

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		return fmt.Errorf("writing config file: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("writing config file: %w", err)
	}
	return nil
}
