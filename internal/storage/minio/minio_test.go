package minio

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

const (
	testBucket    = "toga-minio-test"
	testAccessKey = "minioadmin"
	testSecretKey = "minioadmin"
)

var testEndpoint string

func TestMain(m *testing.M) {
	containerName := "toga-minio-test-minio"
	exec.Command("docker", "rm", "-f", containerName).Run()

	cmd := exec.Command("docker", "run", "-d",
		"--name", containerName,
		"-p", "0:9000",
		"-e", "MINIO_ROOT_USER="+testAccessKey,
		"-e", "MINIO_ROOT_PASSWORD="+testSecretKey,
		"minio/minio", "server", "/data",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Fprintf(os.Stderr, "skipping MinIO tests: docker unavailable: %s\n", out)
		os.Exit(0)
	}
	containerID := strings.TrimSpace(string(out))

	portOut, err := exec.Command("docker", "port", containerName, "9000/tcp").Output()
	if err != nil {
		exec.Command("docker", "rm", "-f", containerID).Run()
		fmt.Fprintf(os.Stderr, "failed to get port: %v\n", err)
		os.Exit(1)
	}
	lines := strings.Split(strings.TrimSpace(string(portOut)), "\n")
	testEndpoint = strings.TrimSpace(lines[0])

	ready := false
	for i := 0; i < 30; i++ {
		checkCmd := exec.Command("docker", "exec", containerName, "mc", "ready", "local")
		if checkCmd.Run() == nil {
			ready = true
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if !ready {
		time.Sleep(2 * time.Second)
	}

	code := m.Run()
	exec.Command("docker", "rm", "-f", containerID).Run()
	os.Exit(code)
}

func newTestCacher(t *testing.T) *Cacher {
	t.Helper()
	c, err := New(context.Background(), Config{
		Endpoint: testEndpoint,
		Key:      testAccessKey,
		Secret:   testSecretKey,
		Bucket:   testBucket,
		Region:   "us-east-1",
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
	// New() should auto-create the bucket — verify by creating a second
	// cacher pointing at a fresh bucket name.
	freshBucket := "toga-minio-auto-create"
	c, err := New(context.Background(), Config{
		Endpoint: testEndpoint,
		Key:      testAccessKey,
		Secret:   testSecretKey,
		Bucket:   freshBucket,
		Region:   "us-east-1",
	})
	if err != nil {
		t.Fatalf("new cacher with auto-create: %v", err)
	}

	ctx := context.Background()
	if err := c.Put(ctx, "test-key", strings.NewReader("hello")); err != nil {
		t.Fatalf("put after auto-create: %v", err)
	}
}
