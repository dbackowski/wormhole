package common

import (
	"log/slog"
	"testing"
)

func TestMapLogLevelToSlogLevel(t *testing.T) {
	tests := []struct {
		name  string
		level LogLevel
		want  slog.Level
	}{
		{"debug level", LevelDebug, slog.LevelDebug},
		{"info level", LevelInfo, slog.LevelInfo},
		{"warn level", LevelWarn, slog.LevelWarn},
		{"error level", LevelError, slog.LevelError},
		{"unknown defaults to info", LogLevel("unknown"), slog.LevelInfo},
		{"empty defaults to info", LogLevel(""), slog.LevelInfo},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mapLogLevelToSlogLevel(tt.level)
			if got != tt.want {
				t.Errorf("mapLogLevelToSlogLevel(%q) = %v, want %v", tt.level, got, tt.want)
			}
		})
	}
}

func TestNewLogger(t *testing.T) {
	tests := []struct {
		name   string
		level  LogLevel
		format string
	}{
		{"text format", LevelInfo, "text"},
		{"json format", LevelInfo, "json"},
		{"empty format defaults to text", LevelInfo, ""},
		{"debug level", LevelDebug, "text"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := NewLogger(tt.level, tt.format)
			if logger == nil {
				t.Fatal("NewLogger() returned nil")
			}
			if logger.Logger == nil {
				t.Error("Logger.Logger is nil")
			}
		})
	}
}

func TestLogger_IsDebug(t *testing.T) {
	tests := []struct {
		name  string
		level LogLevel
		want  bool
	}{
		{"debug level returns true", LevelDebug, true},
		{"info level returns false", LevelInfo, false},
		{"warn level returns false", LevelWarn, false},
		{"error level returns false", LevelError, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := NewLogger(tt.level, "text")
			if got := logger.IsDebug(); got != tt.want {
				t.Errorf("IsDebug() = %v, want %v", got, tt.want)
			}
		})
	}
}
