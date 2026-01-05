package common

import (
	"log/slog"
	"os"
)

type Logger struct {
	*slog.Logger
	level slog.Level
}

type LogLevel string

const (
	LevelDebug LogLevel = "debug"
	LevelInfo  LogLevel = "info"
	LevelWarn  LogLevel = "warn"
	LevelError LogLevel = "error"
)

func mapLogLevelToSlogLevel(level LogLevel) slog.Level {
	switch level {
	case LevelDebug:
		return slog.LevelDebug
	case LevelInfo:
		return slog.LevelInfo
	case LevelWarn:
		return slog.LevelWarn
	case LevelError:
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func NewLogger(lvl LogLevel, format string) *Logger {
	level := mapLogLevelToSlogLevel(lvl)
	opts := &slog.HandlerOptions{
		Level:     level,
		AddSource: level == slog.LevelDebug,
	}

	var handler slog.Handler
	var output = os.Stderr

	if format == "" {
		format = "text"
	}

	if format == "json" {
		handler = slog.NewJSONHandler(output, opts)
	} else {
		handler = slog.NewTextHandler(output, opts)
	}

	return &Logger{
		Logger: slog.New(handler),
		level:  level,
	}
}

func (l *Logger) IsDebug() bool {
	return l.level <= slog.LevelDebug
}
