// Package s3 implements goproxy.Cacher backed by Amazon S3 (or S3-compatible stores).
// The key layout is Athens-compatible: <module>/@v/<version>.<ext>
package s3

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
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

// Cacher implements goproxy.Cacher using S3.
type Cacher struct {
	client *s3.Client
	bucket string
}

// New creates an S3-backed Cacher.
func New(ctx context.Context, cfg Config) (*Cacher, error) {
	var opts []func(*awsconfig.LoadOptions) error

	if cfg.Region != "" {
		opts = append(opts, awsconfig.WithRegion(cfg.Region))
	}

	if cfg.Key != "" && cfg.Secret != "" {
		opts = append(opts, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.Key, cfg.Secret, cfg.Token),
		))
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, err
	}

	var s3Opts []func(*s3.Options)
	if cfg.Endpoint != "" {
		s3Opts = append(s3Opts, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
			o.UsePathStyle = cfg.ForcePathStyle
		})
	} else if cfg.ForcePathStyle {
		s3Opts = append(s3Opts, func(o *s3.Options) {
			o.UsePathStyle = true
		})
	}

	client := s3.NewFromConfig(awsCfg, s3Opts...)

	return &Cacher{
		client: client,
		bucket: cfg.Bucket,
	}, nil
}

// Get retrieves a cached module file by name.
// Returns fs.ErrNotExist if the key does not exist.
func (c *Cacher) Get(ctx context.Context, name string) (io.ReadCloser, error) {
	out, err := c.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(name),
	})
	if err != nil {
		var nsk *types.NoSuchKey
		var nsb *types.NotFound
		if errors.As(err, &nsk) || errors.As(err, &nsb) {
			return nil, fs.ErrNotExist
		}
		return nil, err
	}
	return out.Body, nil
}

// Put stores a module file in S3.
func (c *Cacher) Put(ctx context.Context, name string, content io.ReadSeeker) error {
	// Read all content to get the size for Content-Length.
	data, err := io.ReadAll(content)
	if err != nil {
		return err
	}

	_, err = c.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(name),
		Body:   bytes.NewReader(data),
	})
	return err
}
