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

type ConfigFile struct {
	AuthToken string
}

func DefaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, DefaultConfigDir, DefaultConfigFile)
}

func LoadConfigFile(path string) (*ConfigFile, error) {
	if path == "" {
		return &ConfigFile{}, nil
	}

	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &ConfigFile{}, nil
		}
		return nil, fmt.Errorf("reading config file: %w", err)
	}
	defer file.Close()

	cfg := &ConfigFile{}
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

		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)

		switch key {
		case "AUTH_TOKEN":
			cfg.AuthToken = value
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	return cfg, nil
}
