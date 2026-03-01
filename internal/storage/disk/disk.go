// Package disk implements goproxy.Cacher using the local filesystem.
// This is a thin wrapper around goproxy.DirCacher for consistency.
package disk

import (
	"github.com/goproxy/goproxy"
)

// Config holds filesystem storage parameters.
type Config struct {
	RootPath string
}

// New creates a filesystem-backed Cacher.
func New(cfg Config) goproxy.DirCacher {
	return goproxy.DirCacher(cfg.RootPath)
}
