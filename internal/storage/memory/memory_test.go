package memory

import (
	"context"
	"io"
	"io/fs"
	"strings"
	"sync"
	"testing"
)

func TestPutAndGet(t *testing.T) {
	c := New()
	ctx := context.Background()

	if err := c.Put(ctx, "mod/@v/v1.0.0.info", strings.NewReader(`{"Version":"v1.0.0"}`)); err != nil {
		t.Fatalf("put: %v", err)
	}

	rc, err := c.Get(ctx, "mod/@v/v1.0.0.info")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer rc.Close()

	data, _ := io.ReadAll(rc)
	if string(data) != `{"Version":"v1.0.0"}` {
		t.Errorf("got %q", data)
	}
}

func TestGetMiss(t *testing.T) {
	c := New()
	_, err := c.Get(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error")
	}
	if err != fs.ErrNotExist {
		t.Errorf("expected fs.ErrNotExist, got %v", err)
	}
}

func TestOverwrite(t *testing.T) {
	c := New()
	ctx := context.Background()

	c.Put(ctx, "key", strings.NewReader("v1"))
	c.Put(ctx, "key", strings.NewReader("v2"))

	rc, _ := c.Get(ctx, "key")
	defer rc.Close()
	data, _ := io.ReadAll(rc)
	if string(data) != "v2" {
		t.Errorf("expected v2, got %q", data)
	}
}

func TestReadCloserMetadata(t *testing.T) {
	c := New()
	ctx := context.Background()

	c.Put(ctx, "key", strings.NewReader("hello"))

	rc, _ := c.Get(ctx, "key")
	defer rc.Close()

	r := rc.(*readCloser)
	if r.Size() != 5 {
		t.Errorf("expected size 5, got %d", r.Size())
	}
	if r.LastModified().IsZero() {
		t.Error("expected non-zero LastModified")
	}
	if err := r.Close(); err != nil {
		t.Errorf("close error: %v", err)
	}
}

func TestConcurrentAccess(t *testing.T) {
	c := New()
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := "key"
			c.Put(ctx, key, strings.NewReader("data"))
			if rc, err := c.Get(ctx, key); err == nil {
				rc.Close()
			}
		}(i)
	}
	wg.Wait()
}
