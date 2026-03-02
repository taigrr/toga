package s3

import (
	"context"
	"io"
	"io/fs"
	"strings"
	"testing"

	"github.com/minio/minio-go/v7"
	tcminio "github.com/testcontainers/testcontainers-go/modules/minio"
)

const (
	testBucket    = "toga-s3-test"
	testAccessKey = "minioadmin"
	testSecretKey = "minioadmin"
)

func newTestCacher(t *testing.T) *Cacher {
	t.Helper()
	ctx := context.Background()

	container, err := tcminio.Run(ctx,
		"minio/minio:latest",
		tcminio.WithUsername(testAccessKey),
		tcminio.WithPassword(testSecretKey),
	)
	if err != nil {
		t.Fatalf("start minio container: %v", err)
	}
	t.Cleanup(func() { container.Terminate(ctx) })

	endpoint, err := container.ConnectionString(ctx)
	if err != nil {
		t.Fatalf("get connection string: %v", err)
	}

	c, err := New(ctx, Config{
		Endpoint:       "http://" + endpoint,
		Key:            testAccessKey,
		Secret:         testSecretKey,
		Bucket:         testBucket,
		Region:         "us-east-1",
		ForcePathStyle: true,
	})
	if err != nil {
		t.Fatalf("new s3 cacher: %v", err)
	}

	c.client.MakeBucket(ctx, testBucket, minio.MakeBucketOptions{Region: "us-east-1"})

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

func TestParseEndpoint(t *testing.T) {
	tests := []struct {
		cfg      Config
		endpoint string
		secure   bool
	}{
		{Config{}, "s3.us-east-1.amazonaws.com", true},
		{Config{Region: "eu-west-1"}, "s3.eu-west-1.amazonaws.com", true},
		{Config{Endpoint: "http://localhost:9000"}, "localhost:9000", false},
		{Config{Endpoint: "https://s3.custom.com"}, "s3.custom.com", true},
		{Config{Endpoint: "minio.local:9000"}, "minio.local:9000", true},
	}
	for _, tt := range tests {
		ep, sec := parseEndpoint(tt.cfg)
		if ep != tt.endpoint || sec != tt.secure {
			t.Errorf("parseEndpoint(%+v) = (%q, %v), want (%q, %v)",
				tt.cfg, ep, sec, tt.endpoint, tt.secure)
		}
	}
}
