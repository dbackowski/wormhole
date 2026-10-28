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
