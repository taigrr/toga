// Package s3 implements goproxy.Cacher backed by Amazon S3 (or S3-compatible stores).
// Uses the MinIO SDK for S3 compatibility.
// The key layout is Athens-compatible: <module>/@v/<version>.<ext>
package s3

import (
	"bytes"
	"context"
	"io"
	"io/fs"
	"strings"

	"github.com/minio/minio-go/v7"
	miniocreds "github.com/minio/minio-go/v7/pkg/credentials"
)

const (
	defaultRegion = "us-east-1"
	schemeHTTPS   = "https://"
	schemeHTTP    = "http://"

	errNoSuchKey    = "NoSuchKey"
	errNoSuchBucket = "NoSuchBucket"
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
func New(_ context.Context, cfg Config) (*Cacher, error) {
	endpoint, secure := parseEndpoint(cfg)

	var creds *miniocreds.Credentials
	if cfg.Key != "" && cfg.Secret != "" {
		creds = miniocreds.NewStaticV4(cfg.Key, cfg.Secret, cfg.Token)
	} else {
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

func parseEndpoint(cfg Config) (endpoint string, secure bool) {
	secure = true

	if cfg.Endpoint == "" {
		region := cfg.Region
		if region == "" {
			region = defaultRegion
		}
		return "s3." + region + ".amazonaws.com", secure
	}

	endpoint = cfg.Endpoint
	switch {
	case strings.HasPrefix(endpoint, schemeHTTPS):
		endpoint = strings.TrimPrefix(endpoint, schemeHTTPS)
	case strings.HasPrefix(endpoint, schemeHTTP):
		endpoint = strings.TrimPrefix(endpoint, schemeHTTP)
		secure = false
	}

	return endpoint, secure
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
		if errResp.Code == errNoSuchKey || errResp.Code == errNoSuchBucket {
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

// Compile-time interface check.
var _ interface {
	Get(context.Context, string) (io.ReadCloser, error)
	Put(context.Context, string, io.ReadSeeker) error
} = (*Cacher)(nil)
