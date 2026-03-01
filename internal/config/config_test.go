package config

import (
	"testing"
)

func TestDefaults(t *testing.T) {
	if err := Init(""); err != nil {
		t.Fatalf("Init: %v", err)
	}
	cfg := Load()

	tests := []struct {
		name string
		got  string
		want string
	}{
		{"port", cfg.Port, ":3000"},
		{"storage_type", cfg.StorageType, "disk"},
		{"network_mode", cfg.NetworkMode, "fallback"},
		{"log_level", cfg.LogLevel, "info"},
		{"go_binary", cfg.GoBinary, "go"},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s: got %q, want %q", tt.name, tt.got, tt.want)
		}
	}
	if cfg.Disk.RootPath == "" {
		t.Error("expected non-empty default disk root path")
	}
}
