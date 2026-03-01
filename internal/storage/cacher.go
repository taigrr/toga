// Package storage provides goproxy.Cacher implementations for various backends.
package storage

import (
	"context"
	"io"

	"github.com/goproxy/goproxy"
)

// Cacher wraps the goproxy.Cacher interface with a type alias for convenience.
type Cacher = goproxy.Cacher

// NewCacher creates a Cacher from the given backend type and configuration.
// Supported types: disk, s3, minio, gcs, azureblob.
func NewCacher(ctx context.Context, storageType string, opts any) (Cacher, error) {
	// This is wired up in the factory below; each backend implements goproxy.Cacher.
	_ = ctx
	_ = storageType
	_ = opts
	// Placeholder — actual dispatch is in cmd/toga/main.go for now.
	return nil, nil
}

// SizeReadCloser wraps an io.ReadCloser with a known size.
type SizeReadCloser struct {
	io.ReadCloser
	Size int64
}
