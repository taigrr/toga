package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/goproxy/goproxy"
	"github.com/taigrr/toga/internal/config"
)

func TestListenTCP(t *testing.T) {
	cfg := &config.Config{Port: ":0"}
	ln, err := listen(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer ln.Close()

	if ln.Addr().Network() != "tcp" {
		t.Errorf("expected tcp, got %s", ln.Addr().Network())
	}
}

func TestListenUnixSocket(t *testing.T) {
	// Use /tmp directly to avoid long path from t.TempDir() exceeding Unix socket limit (104 chars on macOS).
	sock := filepath.Join("/tmp", fmt.Sprintf("toga-test-%d.sock", time.Now().UnixNano()))
	t.Cleanup(func() { os.Remove(sock) })

	cfg := &config.Config{UnixSocket: sock}
	ln, err := listen(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer ln.Close()

	if ln.Addr().Network() != "unix" {
		t.Errorf("expected unix, got %s", ln.Addr().Network())
	}
}

func TestListenUnixSocketRemovesStale(t *testing.T) {
	// Use /tmp directly to avoid long path from t.TempDir() exceeding Unix socket limit (104 chars on macOS).
	sock := filepath.Join("/tmp", fmt.Sprintf("toga-test-%d.sock", time.Now().UnixNano()))
	t.Cleanup(func() { os.Remove(sock) })

	// First listen call creates the socket.
	cfg := &config.Config{UnixSocket: sock}
	ln1, err := listen(cfg)
	if err != nil {
		t.Fatalf("first listen: %v", err)
	}
	ln1.Close()

	// Second listen call should remove the stale socket and succeed.
	ln2, err := listen(cfg)
	if err != nil {
		t.Fatalf("second listen (should remove stale): %v", err)
	}
	defer ln2.Close()
}

func TestServerStartAndShutdown(t *testing.T) {
	cfg := &config.Config{
		Port:            ":0",
		StorageType:     "memory",
		NetworkMode:     "offline",
		ShutdownTimeout: 5 * time.Second,
		Timeout:         10 * time.Second,
	}

	cacher, err := newCacher(context.Background(), cfg)
	if err != nil {
		t.Fatalf("newCacher: %v", err)
	}

	proxy := &goproxy.Goproxy{
		Cacher: cacher,
		Logger: slog.Default(),
	}

	handler := buildHandler(proxy, nil, nil, cfg, slog.Default())
	srv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: readHeaderTimeout,
	}

	ln, err := listen(cfg)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	go srv.Serve(ln)

	addr := fmt.Sprintf("http://%s", ln.Addr().String())
	resp, err := http.Get(addr + "/healthz")
	if err != nil {
		t.Fatalf("health check failed: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		t.Errorf("shutdown error: %v", err)
	}
}
