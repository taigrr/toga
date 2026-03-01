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

func TestAthensOverridesTakePrecedence(t *testing.T) {
	// Run in a subprocess to avoid jety global state leaking.
	if os.Getenv("TEST_SUBPROCESS") == "1" {
		os.Setenv("TOGA_PORT", ":4000")
		os.Setenv("ATHENS_PORT", ":5000")
		os.Setenv("ATHENS_STORAGE_TYPE", "s3")

		if err := Init(""); err != nil {
			fmt.Fprintf(os.Stderr, "Init: %v", err)
			os.Exit(1)
		}
		cfg := Load()

		if cfg.Port != ":5000" {
			fmt.Fprintf(os.Stderr, "port: got %q, want :5000", cfg.Port)
			os.Exit(1)
		}
		if cfg.StorageType != "s3" {
			fmt.Fprintf(os.Stderr, "storage: got %q, want s3", cfg.StorageType)
			os.Exit(1)
		}
		os.Exit(0)
	}

	cmd := testSubprocess(t)
	cmd.Env = append(os.Environ(), "TEST_SUBPROCESS=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("subprocess failed: %v\n%s", err, out)
	}
}

func testSubprocess(t *testing.T) *exec.Cmd {
	t.Helper()
	return exec.Command(os.Args[0], "-test.run=^TestAthensOverridesTakePrecedence$")
}
