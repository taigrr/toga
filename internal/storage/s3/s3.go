// Package s3 implements goproxy.Cacher backed by Amazon S3 (or S3-compatible stores).
// Uses the AWS SDK for Go v2.
// The key layout is Athens-compatible: <module>/@v/<version>.<ext>
package s3

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

const (
	defaultRegion = "us-east-1"
	schemeHTTPS   = "https://"
	schemeHTTP    = "http://"

	errNoSuchKey    = "NoSuchKey"
	errNoSuchBucket = "NoSuchBucket"
	errNotFound     = "NotFound"
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
	// Anonymous disables the AWS default credential chain when Key and
	// Secret are empty, sending unsigned requests instead. Used by the
	// minio backend, where probing IMDS/ECS/SSO is never wanted.
	Anonymous bool
}

// Cacher implements goproxy.Cacher using S3.
type Cacher struct {
	client *s3.Client
	bucket string
	region string
}

// New creates an S3-backed Cacher.
// When Key and Secret are empty, the default AWS credential chain is used
// (environment, shared config, IMDS, IRSA, Pod Identity, etc.).
func New(ctx context.Context, cfg Config) (*Cacher, error) {
	region := cfg.Region
	if region == "" {
		region = defaultRegion
	}

	loadOpts := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(region),
	}
	switch {
	case cfg.Key != "" && cfg.Secret != "":
		loadOpts = append(loadOpts, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.Key, cfg.Secret, cfg.Token),
		))
	case cfg.Anonymous:
		loadOpts = append(loadOpts, awsconfig.WithCredentialsProvider(aws.AnonymousCredentials{}))
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, loadOpts...)
	if err != nil {
		return nil, err
	}

	endpoint := normalizeEndpoint(cfg.Endpoint)
	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if endpoint != "" {
			o.BaseEndpoint = aws.String(endpoint)
			// Third-party S3 implementations often reject the SDK's default
			// CRC-based request checksums; only send them when required.
			o.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
			o.ResponseChecksumValidation = aws.ResponseChecksumValidationWhenRequired
		}
		// Virtual-hosted addressing only works reliably on AWS itself; every
		// custom endpoint is addressed path-style, as the MinIO SDK did.
		o.UsePathStyle = cfg.ForcePathStyle || endpoint != ""
	})

	return &Cacher{client: client, bucket: cfg.Bucket, region: region}, nil
}

// normalizeEndpoint returns a full URL for a custom endpoint, defaulting to
// https:// when no scheme is given. Empty input yields empty output so the
// SDK's regional AWS endpoint is used.
func normalizeEndpoint(endpoint string) string {
	if endpoint == "" {
		return ""
	}
	if hasScheme(endpoint) {
		return endpoint
	}
	return schemeHTTPS + endpoint
}

// hasScheme reports whether endpoint already carries an http(s) scheme.
func hasScheme(endpoint string) bool {
	return strings.HasPrefix(endpoint, schemeHTTPS) || strings.HasPrefix(endpoint, schemeHTTP)
}

// Get retrieves a cached module file by name.
// Returns fs.ErrNotExist if the key does not exist.
func (c *Cacher) Get(ctx context.Context, name string) (io.ReadCloser, error) {
	out, err := c.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(name),
	})
	if err != nil {
		if isNoSuchKey(err) {
			return nil, fs.ErrNotExist
		}
		return nil, err
	}
	return out.Body, nil
}

// Put stores a module file in S3, uploading from the reader's current
// position to its end.
func (c *Cacher) Put(ctx context.Context, name string, content io.ReadSeeker) error {
	start, err := content.Seek(0, io.SeekCurrent)
	if err != nil {
		return err
	}
	end, err := content.Seek(0, io.SeekEnd)
	if err != nil {
		return err
	}
	if _, err := content.Seek(start, io.SeekStart); err != nil {
		return err
	}
	_, err = c.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(c.bucket),
		Key:           aws.String(name),
		Body:          content,
		ContentLength: aws.Int64(end - start),
	})
	return err
}

// EnsureBucket creates the bucket if it does not already exist.
func (c *Cacher) EnsureBucket(ctx context.Context) error {
	err := c.headBucket(ctx)
	if err == nil {
		return nil
	}
	if !IsNotFound(err) {
		return err
	}
	in := &s3.CreateBucketInput{Bucket: aws.String(c.bucket)}
	// S3 rejects an explicit us-east-1 location constraint; omit it there.
	if c.region != defaultRegion {
		in.CreateBucketConfiguration = &types.CreateBucketConfiguration{
			LocationConstraint: types.BucketLocationConstraint(c.region),
		}
	}
	_, err = c.client.CreateBucket(ctx, in)
	if err == nil {
		return nil
	}
	if _, ok := errors.AsType[*types.BucketAlreadyOwnedByYou](err); ok {
		return nil
	}
	// BucketAlreadyExists may be a concurrent create by us or a name owned
	// by someone else; only accept it if the bucket is reachable now.
	if _, ok := errors.AsType[*types.BucketAlreadyExists](err); ok {
		return c.headBucket(ctx)
	}
	return err
}

func (c *Cacher) headBucket(ctx context.Context) error {
	_, err := c.client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(c.bucket)})
	return err
}

// isNoSuchKey reports whether err is S3's NoSuchKey or NoSuchBucket, the
// only errors GetObject returns for a key that does not exist.
func isNoSuchKey(err error) bool {
	if _, ok := errors.AsType[*types.NoSuchKey](err); ok {
		return true
	}
	if _, ok := errors.AsType[*types.NoSuchBucket](err); ok {
		return true
	}
	if apiErr, ok := errors.AsType[smithy.APIError](err); ok {
		code := apiErr.ErrorCode()
		return code == errNoSuchKey || code == errNoSuchBucket
	}
	return false
}

// IsNotFound reports whether err is an S3 "does not exist" error for a key
// or bucket, including the bare 404 NotFound returned by Head requests.
func IsNotFound(err error) bool {
	if isNoSuchKey(err) {
		return true
	}
	if _, ok := errors.AsType[*types.NotFound](err); ok {
		return true
	}
	if apiErr, ok := errors.AsType[smithy.APIError](err); ok {
		return apiErr.ErrorCode() == errNotFound
	}
	return false
}

// Compile-time interface check.
var _ interface {
	Get(context.Context, string) (io.ReadCloser, error)
	Put(context.Context, string, io.ReadSeeker) error
} = (*Cacher)(nil)

// S3Client returns the underlying S3 client for direct API access.
func (c *Cacher) S3Client() *s3.Client { return c.client }

// BucketName returns the S3 bucket name.
func (c *Cacher) BucketName() string { return c.bucket }
