// Package minio implements goproxy.Cacher backed by MinIO.
// Uses the same key layout as Athens: <module>/@v/<version>.<ext>
package minio

import (
	"bytes"
	"context"
	"io"
	"io/fs"

	"github.com/minio/minio-go/v7"
	miniocreds "github.com/minio/minio-go/v7/pkg/credentials"
)

const errNoSuchKey = "NoSuchKey"

// Config holds MinIO connection parameters.
type Config struct {
	Endpoint  string
	Key       string
	Secret    string
	Bucket    string
	Region    string
	EnableSSL bool
}

// Cacher implements goproxy.Cacher using MinIO.
type Cacher struct {
	client *minio.Client
	bucket string
}

// New creates a MinIO-backed Cacher.
func New(ctx context.Context, cfg Config) (*Cacher, error) {
	client, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  miniocreds.NewStaticV4(cfg.Key, cfg.Secret, ""),
		Secure: cfg.EnableSSL,
		Region: cfg.Region,
	})
	if err != nil {
		return nil, err
	}

	// Ensure bucket exists. MakeBucket is idempotent if the bucket already
	// exists and is owned by the same account; the error is safe to ignore
	// in that case (avoids a TOCTOU race with BucketExists).
	if err := client.MakeBucket(ctx, cfg.Bucket, minio.MakeBucketOptions{Region: cfg.Region}); err != nil {
		// Ignore "bucket already owned by you" errors.
		exists, checkErr := client.BucketExists(ctx, cfg.Bucket)
		if checkErr != nil || !exists {
			return nil, err
		}
	}

	return &Cacher{client: client, bucket: cfg.Bucket}, nil
}

// Get retrieves a cached module file.
func (c *Cacher) Get(ctx context.Context, name string) (io.ReadCloser, error) {
	obj, err := c.client.GetObject(ctx, c.bucket, name, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	if _, err := obj.Stat(); err != nil {
		obj.Close()
		errResp := minio.ToErrorResponse(err)
		if errResp.Code == errNoSuchKey {
			return nil, fs.ErrNotExist
		}
		return nil, err
	}
	return obj, nil
}

// Put stores a module file in MinIO.
func (c *Cacher) Put(ctx context.Context, name string, content io.ReadSeeker) error {
	data, err := io.ReadAll(content)
	if err != nil {
		return err
	}
	_, err = c.client.PutObject(ctx, c.bucket, name, bytes.NewReader(data), int64(len(data)), minio.PutObjectOptions{})
	return err
}
