package main

import (
	"context"
	"log/slog"
	"testing"

	"github.com/taigrr/toga/internal/config"
)

func TestNewLoggerLevels(t *testing.T) {
	tests := []struct {
		level string
		want  slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"warn", slog.LevelWarn},
		{"error", slog.LevelError},
		{"info", slog.LevelInfo},
		{"", slog.LevelInfo},
		{"unknown", slog.LevelInfo},
	}
	for _, tt := range tests {
		t.Run(tt.level, func(t *testing.T) {
			logger := newLogger(&config.Config{LogLevel: tt.level, LogFormat: "json"})
			if logger == nil {
				t.Fatal("expected non-nil logger")
			}
			if !logger.Enabled(context.Background(), tt.want) {
				t.Errorf("logger should be enabled at %v", tt.want)
			}
		})
	}
}

func TestNewLoggerFormats(t *testing.T) {
	for _, format := range []string{"json", "plain", "text", ""} {
		logger := newLogger(&config.Config{LogLevel: "info", LogFormat: format})
		if logger == nil {
			t.Errorf("expected non-nil logger for format %q", format)
		}
	}
}
