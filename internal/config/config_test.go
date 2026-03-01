package config

import (
	"fmt"
	"os"
	"os/exec"
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

func TestTogaEnvVarsOverrideDefaults(t *testing.T) {
	// Subprocess so jety's global default manager reads env at import time.
	if os.Getenv("TEST_TOGA_ENV") == "1" {
		if err := Init(""); err != nil {
			fmt.Fprintf(os.Stderr, "Init: %v", err)
			os.Exit(1)
		}
		cfg := Load()

		checks := []struct {
			name, got, want string
		}{
			{"port", cfg.Port, ":9999"},
			{"storage_type", cfg.StorageType, "s3"},
			{"log_level", cfg.LogLevel, "debug"},
		}
		for _, c := range checks {
			if c.got != c.want {
				fmt.Fprintf(os.Stderr, "%s: got %q, want %q\n", c.name, c.got, c.want)
				os.Exit(1)
			}
		}
		os.Exit(0)
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestTogaEnvVarsOverrideDefaults$")
	cmd.Env = append(os.Environ(),
		"TEST_TOGA_ENV=1",
		"TOGA_PORT=:9999",
		"TOGA_STORAGE_TYPE=s3",
		"TOGA_LOG_LEVEL=debug",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("subprocess failed: %v\n%s", err, out)
	}
}
