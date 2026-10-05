package minio

import (
	"context"
	"io"
	"io/fs"
	"strings"
	"testing"

	"github.com/taigrr/toga/internal/storage/s3test"
)

const testBucket = "toga-minio-test"

func newTestCacher(t *testing.T) *Cacher {
	t.Helper()
	ctx := context.Background()
	srv := s3test.Start(t)

	c, err := New(ctx, Config{
		Endpoint: srv.HostPort,
		Key:      s3test.AccessKey,
		Secret:   s3test.SecretKey,
		Bucket:   testBucket,
		Region:   s3test.Region,
	})
	if err != nil {
		t.Fatalf("new minio cacher: %v", err)
	}
	return c
}

func TestPutAndGet(t *testing.T) {
	c := newTestCacher(t)
	ctx := context.Background()

	name := "github.com/example/mod/@v/v1.0.0.info"
	content := `{"Version":"v1.0.0"}`

	if err := c.Put(ctx, name, strings.NewReader(content)); err != nil {
		t.Fatalf("put: %v", err)
	}

	rc, err := c.Get(ctx, name)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer rc.Close()

	data, _ := io.ReadAll(rc)
	if string(data) != content {
		t.Errorf("expected %q, got %q", content, string(data))
	}
}

func TestGetMiss(t *testing.T) {
	c := newTestCacher(t)
	_, err := c.Get(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error")
	}
	if err != fs.ErrNotExist {
		t.Errorf("expected fs.ErrNotExist, got %v", err)
	}
}

func TestOverwrite(t *testing.T) {
	c := newTestCacher(t)
	ctx := context.Background()

	name := "github.com/example/mod/@v/v1.0.0.mod"
	c.Put(ctx, name, strings.NewReader("v1"))
	c.Put(ctx, name, strings.NewReader("v2"))

	rc, _ := c.Get(ctx, name)
	defer rc.Close()
	data, _ := io.ReadAll(rc)
	if string(data) != "v2" {
		t.Errorf("expected v2, got %q", data)
	}
}

func TestLargeFile(t *testing.T) {
	c := newTestCacher(t)
	ctx := context.Background()

	name := "github.com/example/mod/@v/v1.0.0.zip"
	content := strings.Repeat("x", 1024*1024)

	if err := c.Put(ctx, name, strings.NewReader(content)); err != nil {
		t.Fatalf("put: %v", err)
	}

	rc, err := c.Get(ctx, name)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer rc.Close()

	data, _ := io.ReadAll(rc)
	if len(data) != len(content) {
		t.Errorf("expected %d bytes, got %d", len(content), len(data))
	}
}

func TestBucketAutoCreation(t *testing.T) {
	ctx := context.Background()
	srv := s3test.Start(t)

	c, err := New(ctx, Config{
		Endpoint: srv.HostPort,
		Key:      s3test.AccessKey,
		Secret:   s3test.SecretKey,
		Bucket:   "toga-auto-create",
		Region:   s3test.Region,
	})
	if err != nil {
		t.Fatalf("new cacher with auto-create: %v", err)
	}

	if err := c.Put(ctx, "test-key", strings.NewReader("hello")); err != nil {
		t.Fatalf("put after auto-create: %v", err)
	}
}

func TestEndpointURL(t *testing.T) {
	tests := []struct {
		endpoint string
		ssl      bool
		want     string
	}{
		{"host:9000", false, "http://host:9000"},
		{"host:9000", true, "https://host:9000"},
		{"http://host:9000", true, "https://host:9000"},
		{"https://host:9000", false, "http://host:9000"},
	}
	for _, tt := range tests {
		if got := endpointURL(tt.endpoint, tt.ssl); got != tt.want {
			t.Errorf("endpointURL(%q, %v) = %q, want %q", tt.endpoint, tt.ssl, got, tt.want)
		}
	}
}
