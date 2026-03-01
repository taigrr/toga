// Package s3 implements goproxy.Cacher backed by Amazon S3 (or S3-compatible stores).
// Uses the MinIO SDK for S3 compatibility.
// The key layout is Athens-compatible: <module>/@v/<version>.<ext>
package s3

import (
	"bytes"
	"context"
	"io"
	"io/fs"

	"github.com/minio/minio-go/v7"
	miniocreds "github.com/minio/minio-go/v7/pkg/credentials"
)

// Config holds S3 connection parameters.
type Config struct {
	Region         string
	Key            string
	Secret         string
	Token          string
	Bucket         string
	Endpoint       string
	ForcePathStyle bool
}

// Cacher implements goproxy.Cacher using S3 via the MinIO SDK.
type Cacher struct {
	client *minio.Client
	bucket string
}

// New creates an S3-backed Cacher using the MinIO SDK.
func New(ctx context.Context, cfg Config) (*Cacher, error) {
	endpoint := cfg.Endpoint
	secure := true

	// Default to AWS S3 endpoint if none specified.
	if endpoint == "" {
		region := cfg.Region
		if region == "" {
			region = "us-east-1"
		}
		endpoint = "s3." + region + ".amazonaws.com"
	} else {
		// Strip scheme if present and detect SSL.
		if len(endpoint) > 8 && endpoint[:8] == "https://" {
			endpoint = endpoint[8:]
		} else if len(endpoint) > 7 && endpoint[:7] == "http://" {
			endpoint = endpoint[7:]
			secure = false
		}
	}

	var creds *miniocreds.Credentials
	if cfg.Key != "" && cfg.Secret != "" {
		creds = miniocreds.NewStaticV4(cfg.Key, cfg.Secret, cfg.Token)
	} else {
		// Fall back to IAM/environment credentials (EC2, ECS, etc.)
		creds = miniocreds.NewIAM("")
	}

	client, err := minio.New(endpoint, &minio.Options{
		Creds:  creds,
		Secure: secure,
		Region: cfg.Region,
	})
	if err != nil {
		return nil, err
	}

	return &Cacher{client: client, bucket: cfg.Bucket}, nil
}

// Get retrieves a cached module file by name.
// Returns fs.ErrNotExist if the key does not exist.
func (c *Cacher) Get(ctx context.Context, name string) (io.ReadCloser, error) {
	obj, err := c.client.GetObject(ctx, c.bucket, name, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	if _, err := obj.Stat(); err != nil {
		obj.Close()
		errResp := minio.ToErrorResponse(err)
		if errResp.Code == "NoSuchKey" || errResp.Code == "NoSuchBucket" {
			return nil, fs.ErrNotExist
		}
		return nil, err
	}
	return obj, nil
}

// Put stores a module file in S3.
func (c *Cacher) Put(ctx context.Context, name string, content io.ReadSeeker) error {
	data, err := io.ReadAll(content)
	if err != nil {
		return err
	}
	_, err = c.client.PutObject(ctx, c.bucket, name, bytes.NewReader(data), int64(len(data)), minio.PutObjectOptions{})
	return err
}
