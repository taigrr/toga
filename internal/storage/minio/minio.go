// Package minio implements goproxy.Cacher backed by MinIO or any other
// S3-compatible object store reachable at a custom endpoint.
// It is a thin wrapper over the s3 backend that preserves the Athens-style
// MinIO configuration surface and auto-creates the bucket.
// Uses the same key layout as Athens: <module>/@v/<version>.<ext>
package minio

import (
	"context"
	"strings"

	"github.com/taigrr/toga/internal/storage/s3"
)

const (
	schemeHTTPS = "https://"
	schemeHTTP  = "http://"
)

// Config holds MinIO connection parameters.
type Config struct {
	Endpoint  string
	Key       string
	Secret    string
	Bucket    string
	Region    string
	EnableSSL bool
}

// Cacher implements goproxy.Cacher using an S3-compatible store.
type Cacher struct {
	*s3.Cacher
}

// New creates a MinIO-backed Cacher and ensures the bucket exists.
func New(ctx context.Context, cfg Config) (*Cacher, error) {
	inner, err := s3.New(ctx, s3.Config{
		Region:         cfg.Region,
		Key:            cfg.Key,
		Secret:         cfg.Secret,
		Bucket:         cfg.Bucket,
		Endpoint:       endpointURL(cfg.Endpoint, cfg.EnableSSL),
		ForcePathStyle: true,
		Anonymous:      true,
	})
	if err != nil {
		return nil, err
	}

	if err := inner.EnsureBucket(ctx); err != nil {
		return nil, err
	}

	return &Cacher{Cacher: inner}, nil
}

// endpointURL builds the endpoint URL from an Athens-style host:port and the
// SSL flag. A scheme already present on the endpoint is stripped so the flag
// remains authoritative.
func endpointURL(endpoint string, enableSSL bool) string {
	endpoint = strings.TrimPrefix(endpoint, schemeHTTPS)
	endpoint = strings.TrimPrefix(endpoint, schemeHTTP)
	if enableSSL {
		return schemeHTTPS + endpoint
	}
	return schemeHTTP + endpoint
}
