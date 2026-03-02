package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/taigrr/toga/internal/config"
)

// setupNetrc creates or copies .netrc for private repo access.
func setupNetrc(cfg *config.Config) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	netrcDest := filepath.Join(home, ".netrc")

	// Copy netrc file if specified.
	if cfg.NETRCPath != "" {
		data, err := os.ReadFile(cfg.NETRCPath)
		if err != nil {
			return fmt.Errorf("read netrc %s: %w", cfg.NETRCPath, err)
		}
		return os.WriteFile(netrcDest, data, 0o600)
	}

	// Generate netrc from GitHub token if specified.
	if cfg.GithubToken != "" {
		content := fmt.Sprintf("machine github.com login %s password x-oauth-basic\n", cfg.GithubToken)
		return os.WriteFile(netrcDest, []byte(content), 0o600)
	}

	return nil
}
