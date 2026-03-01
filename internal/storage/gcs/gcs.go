// Package gcs implements goproxy.Cacher backed by Google Cloud Storage.
package gcs

import (
	"context"
	"errors"
	"io"
	"io/fs"

	"cloud.google.com/go/storage"
	"google.golang.org/api/option"
)

// Config holds GCS connection parameters.
type Config struct {
	Bucket          string
	ProjectID       string
	CredentialsFile string
}

// Cacher implements goproxy.Cacher using GCS.
type Cacher struct {
	client *storage.Client
	bucket string
}

// New creates a GCS-backed Cacher.
func New(ctx context.Context, cfg Config) (*Cacher, error) {
	var opts []option.ClientOption
	if cfg.CredentialsFile != "" {
		opts = append(opts, option.WithAuthCredentialsFile(option.ServiceAccount, cfg.CredentialsFile))
	}

	client, err := storage.NewClient(ctx, opts...)
	if err != nil {
		return nil, err
	}

	return &Cacher{client: client, bucket: cfg.Bucket}, nil
}

// Get retrieves a cached module file.
func (c *Cacher) Get(ctx context.Context, name string) (io.ReadCloser, error) {
	reader, err := c.client.Bucket(c.bucket).Object(name).NewReader(ctx)
	if err != nil {
		if errors.Is(err, storage.ErrObjectNotExist) {
			return nil, fs.ErrNotExist
		}
		return nil, err
	}
	return reader, nil
}

// Put stores a module file in GCS.
func (c *Cacher) Put(ctx context.Context, name string, content io.ReadSeeker) error {
	writer := c.client.Bucket(c.bucket).Object(name).NewWriter(ctx)
	if _, err := io.Copy(writer, content); err != nil {
		writer.Close()
		return err
	}
	return writer.Close()
}

// Close releases GCS client resources.
func (c *Cacher) Close() error {
	return c.client.Close()
}

// Compile-time interface check.
var _ interface {
	Get(context.Context, string) (io.ReadCloser, error)
	Put(context.Context, string, io.ReadSeeker) error
} = (*Cacher)(nil)
