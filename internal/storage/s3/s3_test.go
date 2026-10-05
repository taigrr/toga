package s3

import (
	"context"
	"io"
	"io/fs"
	"strings"
	"testing"

	"github.com/taigrr/toga/internal/storage/s3test"
)

const testBucket = "toga-s3-test"

func newTestCacher(t *testing.T) *Cacher {
	t.Helper()
	ctx := context.Background()
	srv := s3test.Start(t)

	c, err := New(ctx, Config{
		Endpoint:       srv.Endpoint,
		Key:            s3test.AccessKey,
		Secret:         s3test.SecretKey,
		Bucket:         testBucket,
		Region:         s3test.Region,
		ForcePathStyle: true,
	})
	if err != nil {
		t.Fatalf("new s3 cacher: %v", err)
	}
	if err := c.EnsureBucket(ctx); err != nil {
		t.Fatalf("ensure bucket: %v", err)
	}

	return c
}

func TestPutAndGet(t *testing.T) {
	c := newTestCacher(t)
	ctx := context.Background()

	name := "github.com/example/mod/@v/v1.0.0.info"
	content := `{"Version":"v1.0.0","Time":"2025-01-01T00:00:00Z"}`

	if err := c.Put(ctx, name, strings.NewReader(content)); err != nil {
		t.Fatalf("put: %v", err)
	}

	rc, err := c.Get(ctx, name)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer rc.Close()

	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(data) != content {
		t.Errorf("expected %q, got %q", content, string(data))
	}
}

func TestGetMiss(t *testing.T) {
	c := newTestCacher(t)
	_, err := c.Get(context.Background(), "nonexistent/key")
	if err == nil {
		t.Fatal("expected error for missing key")
	}
	if err != fs.ErrNotExist {
		t.Errorf("expected fs.ErrNotExist, got %v", err)
	}
}

func TestGetMissingBucket(t *testing.T) {
	c := newTestCacher(t)
	c.bucket = "toga-does-not-exist"
	_, err := c.Get(context.Background(), "some/key")
	if err != fs.ErrNotExist {
		t.Errorf("expected fs.ErrNotExist for missing bucket, got %v", err)
	}
}

func TestEnsureBucketIdempotent(t *testing.T) {
	c := newTestCacher(t)
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if err := c.EnsureBucket(ctx); err != nil {
			t.Fatalf("ensure bucket (call %d): %v", i+1, err)
		}
	}
}

func TestOverwrite(t *testing.T) {
	c := newTestCacher(t)
	ctx := context.Background()

	name := "github.com/example/mod/@v/v1.0.0.mod"
	c.Put(ctx, name, strings.NewReader("module v1"))
	c.Put(ctx, name, strings.NewReader("module v2"))

	rc, _ := c.Get(ctx, name)
	defer rc.Close()
	data, _ := io.ReadAll(rc)
	if string(data) != "module v2" {
		t.Errorf("expected v2, got %q", data)
	}
}

func TestLargeFile(t *testing.T) {
	c := newTestCacher(t)
	ctx := context.Background()

	name := "github.com/example/mod/@v/v1.0.0.zip"
	content := strings.Repeat("x", 1024*1024)

	if err := c.Put(ctx, name, strings.NewReader(content)); err != nil {
		t.Fatalf("put large file: %v", err)
	}

	rc, err := c.Get(ctx, name)
	if err != nil {
		t.Fatalf("get large file: %v", err)
	}
	defer rc.Close()

	data, _ := io.ReadAll(rc)
	if len(data) != len(content) {
		t.Errorf("expected %d bytes, got %d", len(content), len(data))
	}
}

func TestNormalizeEndpoint(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"http://localhost:9000", "http://localhost:9000"},
		{"https://s3.custom.com", "https://s3.custom.com"},
		{"minio.local:9000", "https://minio.local:9000"},
	}
	for _, tt := range tests {
		if got := normalizeEndpoint(tt.in); got != tt.want {
			t.Errorf("normalizeEndpoint(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestPathStyleDefaultsForCustomEndpoint(t *testing.T) {
	for _, name := range []string{"AWS_ENDPOINT_URL", "AWS_ENDPOINT_URL_S3", "AWS_PROFILE", "AWS_CONFIG_FILE", "AWS_SHARED_CREDENTIALS_FILE"} {
		t.Setenv(name, "")
	}
	ctx := context.Background()
	tests := []struct {
		cfg  Config
		want bool
	}{
		{Config{}, false},
		{Config{ForcePathStyle: true}, true},
		{Config{Endpoint: "minio.local:9000"}, true},
		{Config{Endpoint: "http://localhost:9000", ForcePathStyle: false}, true},
	}
	for _, tt := range tests {
		tt.cfg.Key, tt.cfg.Secret = "k", "s"
		c, err := New(ctx, tt.cfg)
		if err != nil {
			t.Fatalf("New(%+v): %v", tt.cfg, err)
		}
		if got := c.client.Options().UsePathStyle; got != tt.want {
			t.Errorf("UsePathStyle for %+v = %v, want %v", tt.cfg, got, tt.want)
		}
	}
}

func TestPutFromOffset(t *testing.T) {
	c := newTestCacher(t)
	ctx := context.Background()

	name := "github.com/example/mod/@v/v1.0.0.info"
	rdr := strings.NewReader("HEADERpayload")
	if _, err := rdr.Seek(6, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	if err := c.Put(ctx, name, rdr); err != nil {
		t.Fatalf("put: %v", err)
	}

	rc, err := c.Get(ctx, name)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer rc.Close()
	data, _ := io.ReadAll(rc)
	if string(data) != "payload" {
		t.Errorf("expected %q, got %q", "payload", data)
	}
}
