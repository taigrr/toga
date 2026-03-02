package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/taigrr/toga/internal/config"
)

func TestSetupNetrcGithubToken(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfg := &config.Config{GithubToken: "ghp_testtoken123"}
	if err := setupNetrc(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	netrcPath := filepath.Join(home, ".netrc")
	data, err := os.ReadFile(netrcPath)
	if err != nil {
		t.Fatalf("failed to read .netrc: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "ghp_testtoken123") {
		t.Error("expected github token in .netrc")
	}
	if !strings.Contains(content, "machine github.com") {
		t.Error("expected machine github.com in .netrc")
	}

	info, _ := os.Stat(netrcPath)
	if info.Mode().Perm() != 0o600 {
		t.Errorf("expected 0600 permissions, got %o", info.Mode().Perm())
	}
}

func TestSetupNetrcFromFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	srcPath := filepath.Join(t.TempDir(), "source-netrc")
	netrcContent := "machine example.com login user password pass\n"
	if err := os.WriteFile(srcPath, []byte(netrcContent), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{NETRCPath: srcPath}
	if err := setupNetrc(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(home, ".netrc"))
	if err != nil {
		t.Fatalf("failed to read .netrc: %v", err)
	}
	if string(data) != netrcContent {
		t.Errorf("expected %q, got %q", netrcContent, string(data))
	}
}

func TestSetupNetrcFromMissingFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfg := &config.Config{NETRCPath: "/nonexistent/netrc"}
	if err := setupNetrc(cfg); err == nil {
		t.Error("expected error for missing netrc file")
	}
}

func TestSetupNetrcNoOp(t *testing.T) {
	cfg := &config.Config{}
	if err := setupNetrc(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
