package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/goproxy/goproxy"
	"github.com/taigrr/toga/internal/config"
)

func TestHealthHandler(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()

	healthHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if body := w.Body.String(); body != "ok\n" {
		t.Errorf("expected ok, got %q", body)
	}
}

func TestBasicAuthRejectsInvalid(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := basicAuth(inner, "admin", "secret")

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.SetBasicAuth("admin", "wrong")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestBasicAuthAcceptsValid(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := basicAuth(inner, "admin", "secret")

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestBasicAuthNoCredentials(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := basicAuth(inner, "admin", "secret")

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
	if got := w.Header().Get("WWW-Authenticate"); got == "" {
		t.Error("expected WWW-Authenticate header")
	}
}

func TestBasicAuthWrongUser(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := basicAuth(inner, "admin", "secret")

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.SetBasicAuth("notadmin", "secret")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

// --- Validation tests ---

func TestValidateStorageDiskRequiresRootPath(t *testing.T) {
	cfg := &config.Config{StorageType: "disk"}
	if err := validateStorage(cfg); err == nil {
		t.Error("expected error for empty disk root path")
	}
}

func TestValidateStorageDiskValid(t *testing.T) {
	cfg := &config.Config{
		StorageType: "disk",
		Disk:        config.DiskConfig{RootPath: "/tmp/test"},
	}
	if err := validateStorage(cfg); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateStorageS3RequiresBucket(t *testing.T) {
	cfg := &config.Config{StorageType: "s3"}
	if err := validateStorage(cfg); err == nil {
		t.Error("expected error for empty s3 bucket")
	}
}

func TestValidateStorageS3Valid(t *testing.T) {
	cfg := &config.Config{
		StorageType: "s3",
		S3:          config.S3Config{Bucket: "my-bucket"},
	}
	if err := validateStorage(cfg); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateStorageMinioRequiresEndpointAndBucket(t *testing.T) {
	cfg := &config.Config{StorageType: "minio"}
	if err := validateStorage(cfg); err == nil {
		t.Error("expected error for empty minio endpoint/bucket")
	}
}

func TestValidateStorageMinioPartial(t *testing.T) {
	cfg := &config.Config{
		StorageType: "minio",
		Minio:       config.MinioConfig{Endpoint: "localhost:9000"},
	}
	if err := validateStorage(cfg); err == nil {
		t.Error("expected error for missing minio bucket")
	}
}

func TestValidateStorageGCSRequiresBucket(t *testing.T) {
	cfg := &config.Config{StorageType: "gcs"}
	if err := validateStorage(cfg); err == nil {
		t.Error("expected error for empty gcs bucket")
	}
}

func TestValidateStorageAzureRequiresAccountAndContainer(t *testing.T) {
	cfg := &config.Config{StorageType: "azureblob"}
	if err := validateStorage(cfg); err == nil {
		t.Error("expected error for empty azure account/container")
	}
}

func TestValidateStorageAzurePartial(t *testing.T) {
	cfg := &config.Config{
		StorageType: "azureblob",
		AzureBlob:   config.AzureBlobConfig{AccountName: "myacct"},
	}
	if err := validateStorage(cfg); err == nil {
		t.Error("expected error for missing azure container_name")
	}
}

func TestValidateStorageMemory(t *testing.T) {
	cfg := &config.Config{StorageType: "memory"}
	if err := validateStorage(cfg); err != nil {
		t.Errorf("unexpected error for memory storage: %v", err)
	}
}

func TestValidateStorageUnknownType(t *testing.T) {
	cfg := &config.Config{StorageType: "unknown"}
	if err := validateStorage(cfg); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

// --- newCacher tests ---

func TestNewCacherMemory(t *testing.T) {
	cfg := &config.Config{StorageType: "memory"}
	c, err := newCacher(context.Background(), cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c == nil {
		t.Fatal("expected non-nil cacher")
	}
}

func TestNewCacherDisk(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{
		StorageType: "disk",
		Disk:        config.DiskConfig{RootPath: dir},
	}
	c, err := newCacher(context.Background(), cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c == nil {
		t.Fatal("expected non-nil cacher")
	}
}

func TestNewCacherUnknown(t *testing.T) {
	cfg := &config.Config{StorageType: "bogus"}
	_, err := newCacher(context.Background(), cfg)
	if err == nil {
		t.Error("expected error for unknown storage type")
	}
}

func TestNewCacherDiskMissingRootPath(t *testing.T) {
	cfg := &config.Config{StorageType: "disk"}
	_, err := newCacher(context.Background(), cfg)
	if err == nil {
		t.Error("expected validation error")
	}
}

// --- buildHandler tests ---

func TestBuildHandlerHealthz(t *testing.T) {
	proxy := &goproxy.Goproxy{}
	cfg := &config.Config{}
	handler := buildHandler(proxy, cfg, slog.Default())

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestBuildHandlerReadyz(t *testing.T) {
	proxy := &goproxy.Goproxy{}
	cfg := &config.Config{}
	handler := buildHandler(proxy, cfg, slog.Default())

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestBuildHandlerDefaultRobots(t *testing.T) {
	proxy := &goproxy.Goproxy{}
	cfg := &config.Config{}
	handler := buildHandler(proxy, cfg, slog.Default())

	req := httptest.NewRequest(http.MethodGet, "/robots.txt", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/plain" {
		t.Errorf("expected text/plain, got %q", ct)
	}
	if !strings.Contains(w.Body.String(), "Disallow: /") {
		t.Error("expected default disallow-all robots.txt")
	}
}

func TestBuildHandlerCustomRobots(t *testing.T) {
	dir := t.TempDir()
	robotsPath := filepath.Join(dir, "robots.txt")
	if err := os.WriteFile(robotsPath, []byte("User-agent: Googlebot\nAllow: /\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	proxy := &goproxy.Goproxy{}
	cfg := &config.Config{RobotsFile: robotsPath}
	handler := buildHandler(proxy, cfg, slog.Default())

	req := httptest.NewRequest(http.MethodGet, "/robots.txt", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Googlebot") {
		t.Error("expected custom robots.txt content")
	}
}

func TestBuildHandlerCustomRobotsMissing(t *testing.T) {
	proxy := &goproxy.Goproxy{}
	cfg := &config.Config{RobotsFile: "/nonexistent/robots.txt"}
	handler := buildHandler(proxy, cfg, slog.Default())

	req := httptest.NewRequest(http.MethodGet, "/robots.txt", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Disallow: /") {
		t.Error("expected fallback disallow-all")
	}
}

func TestBuildHandlerHomepage(t *testing.T) {
	dir := t.TempDir()
	tmplPath := filepath.Join(dir, "home.html")
	if err := os.WriteFile(tmplPath, []byte("<h1>Welcome to Toga</h1>"), 0o644); err != nil {
		t.Fatal(err)
	}

	proxy := &goproxy.Goproxy{}
	cfg := &config.Config{HomeTemplatePath: tmplPath}
	handler := buildHandler(proxy, cfg, slog.Default())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Welcome to Toga") {
		t.Error("expected homepage content")
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Errorf("expected text/html, got %q", ct)
	}
}

func TestBuildHandlerHomepageOnlyAtRoot(t *testing.T) {
	dir := t.TempDir()
	tmplPath := filepath.Join(dir, "home.html")
	if err := os.WriteFile(tmplPath, []byte("<h1>Home</h1>"), 0o644); err != nil {
		t.Fatal(err)
	}

	proxy := &goproxy.Goproxy{}
	cfg := &config.Config{HomeTemplatePath: tmplPath}
	handler := buildHandler(proxy, cfg, slog.Default())

	req := httptest.NewRequest(http.MethodGet, "/some/module/@v/list", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if strings.Contains(w.Body.String(), "<h1>Home</h1>") {
		t.Error("non-root path should not serve homepage")
	}
}

func TestBuildHandlerHomepageBadTemplate(t *testing.T) {
	proxy := &goproxy.Goproxy{}
	cfg := &config.Config{HomeTemplatePath: "/nonexistent/template.html"}
	handler := buildHandler(proxy, cfg, slog.Default())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code >= 500 {
		t.Errorf("expected non-500, got %d", w.Code)
	}
}

func TestBuildHandlerWithBasicAuth(t *testing.T) {
	proxy := &goproxy.Goproxy{}
	cfg := &config.Config{BasicAuthUser: "user", BasicAuthPass: "pass"}
	handler := buildHandler(proxy, cfg, slog.Default())

	// Healthz is a separate mux route, not behind auth.
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("healthz expected 200, got %d", w.Code)
	}
}

func TestBuildHandlerPathPrefix(t *testing.T) {
	proxy := &goproxy.Goproxy{}
	cfg := &config.Config{PathPrefix: "/proxy"}
	handler := buildHandler(proxy, cfg, slog.Default())

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

// --- maxConcurrency tests ---

func TestMaxConcurrency(t *testing.T) {
	var concurrent atomic.Int32
	var maxSeen atomic.Int32
	limit := 2

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		cur := concurrent.Add(1)
		for {
			old := maxSeen.Load()
			if cur <= old || maxSeen.CompareAndSwap(old, cur) {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
		concurrent.Add(-1)
		w.WriteHeader(http.StatusOK)
	})

	handler := maxConcurrency(inner, limit)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
		}()
	}
	wg.Wait()

	if maxSeen.Load() > int32(limit) {
		t.Errorf("max concurrent %d exceeded limit %d", maxSeen.Load(), limit)
	}
}

// --- Logger tests ---

func TestNewLoggerLevels(t *testing.T) {
	tests := []struct {
		level string
		want  slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"warn", slog.LevelWarn},
		{"error", slog.LevelError},
		{"info", slog.LevelInfo},
		{"", slog.LevelInfo},
		{"unknown", slog.LevelInfo},
	}
	for _, tt := range tests {
		t.Run(tt.level, func(t *testing.T) {
			logger := newLogger(tt.level, "json")
			if logger == nil {
				t.Fatal("expected non-nil logger")
			}
			if !logger.Enabled(context.Background(), tt.want) {
				t.Errorf("logger should be enabled at %v", tt.want)
			}
		})
	}
}

func TestNewLoggerFormats(t *testing.T) {
	for _, format := range []string{"json", "plain", "text", ""} {
		logger := newLogger("info", format)
		if logger == nil {
			t.Errorf("expected non-nil logger for format %q", format)
		}
	}
}

// --- setupNetrc tests ---

func TestSetupNetrcGithubToken(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfg := &config.Config{GithubToken: "ghp_testtoken123"}
	if err := setupNetrc(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	netrcPath := filepath.Join(home, ".netrc")
	data, err := os.ReadFile(netrcPath)
	if err != nil {
		t.Fatalf("failed to read .netrc: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "ghp_testtoken123") {
		t.Error("expected github token in .netrc")
	}
	if !strings.Contains(content, "machine github.com") {
		t.Error("expected machine github.com in .netrc")
	}

	info, _ := os.Stat(netrcPath)
	if info.Mode().Perm() != 0o600 {
		t.Errorf("expected 0600 permissions, got %o", info.Mode().Perm())
	}
}

func TestSetupNetrcFromFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	srcPath := filepath.Join(t.TempDir(), "source-netrc")
	netrcContent := "machine example.com login user password pass\n"
	if err := os.WriteFile(srcPath, []byte(netrcContent), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{NETRCPath: srcPath}
	if err := setupNetrc(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(home, ".netrc"))
	if err != nil {
		t.Fatalf("failed to read .netrc: %v", err)
	}
	if string(data) != netrcContent {
		t.Errorf("expected %q, got %q", netrcContent, string(data))
	}
}

func TestSetupNetrcFromMissingFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfg := &config.Config{NETRCPath: "/nonexistent/netrc"}
	if err := setupNetrc(cfg); err == nil {
		t.Error("expected error for missing netrc file")
	}
}

func TestSetupNetrcNoOp(t *testing.T) {
	cfg := &config.Config{}
	if err := setupNetrc(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- listen tests ---

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
	dir := t.TempDir()
	sock := filepath.Join(dir, "toga.sock")

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
	dir := t.TempDir()
	sock := filepath.Join(dir, "toga.sock")

	if err := os.WriteFile(sock, []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{UnixSocket: sock}
	ln, err := listen(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer ln.Close()
}

// --- initTracer tests ---

func TestInitTracerDisabled(t *testing.T) {
	cfg := &config.Config{TraceExporter: ""}
	shutdown, err := initTracer(context.Background(), cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if shutdown != nil {
		t.Error("expected nil shutdown for disabled tracer")
	}
}

func TestInitTracerUnknownExporter(t *testing.T) {
	cfg := &config.Config{TraceExporter: "zipkin"}
	_, err := initTracer(context.Background(), cfg)
	if err == nil {
		t.Error("expected error for unknown exporter")
	}
}

// --- Memory cacher round-trip ---

func TestMemoryCacherRoundTrip(t *testing.T) {
	cfg := &config.Config{StorageType: "memory"}
	cacher, err := newCacher(context.Background(), cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := context.Background()
	name := "github.com/example/mod/@v/v1.0.0.info"
	content := `{"Version":"v1.0.0","Time":"2025-01-01T00:00:00Z"}`

	if err := cacher.Put(ctx, name, strings.NewReader(content)); err != nil {
		t.Fatalf("put failed: %v", err)
	}

	rc, err := cacher.Get(ctx, name)
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	defer rc.Close()

	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if string(data) != content {
		t.Errorf("expected %q, got %q", content, string(data))
	}
}

func TestMemoryCacherMiss(t *testing.T) {
	cfg := &config.Config{StorageType: "memory"}
	cacher, err := newCacher(context.Background(), cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = cacher.Get(context.Background(), "nonexistent")
	if err == nil {
		t.Error("expected error for cache miss")
	}
}

// --- Disk cacher round-trip ---

func TestDiskCacherRoundTrip(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{
		StorageType: "disk",
		Disk:        config.DiskConfig{RootPath: dir},
	}
	cacher, err := newCacher(context.Background(), cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := context.Background()
	name := "github.com/example/mod/@v/v1.0.0.info"
	content := `{"Version":"v1.0.0"}`

	if err := cacher.Put(ctx, name, strings.NewReader(content)); err != nil {
		t.Fatalf("put failed: %v", err)
	}

	rc, err := cacher.Get(ctx, name)
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	defer rc.Close()

	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if string(data) != content {
		t.Errorf("expected %q, got %q", content, string(data))
	}
}

// --- Full handler stack integration ---

func TestFullHandlerStack(t *testing.T) {
	dir := t.TempDir()

	tmplPath := filepath.Join(dir, "index.html")
	os.WriteFile(tmplPath, []byte("<html><body>Toga Proxy</body></html>"), 0o644)

	robotsPath := filepath.Join(dir, "robots.txt")
	os.WriteFile(robotsPath, []byte("User-agent: *\nAllow: /\n"), 0o644)

	proxy := &goproxy.Goproxy{}
	cfg := &config.Config{
		BasicAuthUser:    "admin",
		BasicAuthPass:    "password",
		HomeTemplatePath: tmplPath,
		RobotsFile:       robotsPath,
		ProtocolWorkers:  5,
	}
	handler := buildHandler(proxy, cfg, slog.Default())

	tests := []struct {
		name   string
		path   string
		auth   bool
		status int
		body   string
	}{
		{"healthz no auth", "/healthz", false, http.StatusOK, "ok"},
		{"readyz no auth", "/readyz", false, http.StatusOK, "ok"},
		{"robots no auth", "/robots.txt", false, http.StatusOK, "Allow: /"},
		{"homepage with auth", "/", true, http.StatusOK, "Toga Proxy"},
		{"homepage without auth", "/", false, http.StatusOK, "Toga Proxy"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			if tt.auth {
				req.SetBasicAuth("admin", "password")
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			if w.Code != tt.status {
				t.Errorf("expected %d, got %d", tt.status, w.Code)
			}
			if !strings.Contains(w.Body.String(), tt.body) {
				t.Errorf("expected body to contain %q, got %q", tt.body, w.Body.String())
			}
		})
	}
}


// --- End-to-end server test ---

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

	handler := buildHandler(proxy, cfg, slog.Default())
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

// --- Homepage template rendering ---

func TestHomepageTemplateExecution(t *testing.T) {
	dir := t.TempDir()
	tmplPath := filepath.Join(dir, "home.html")
	tmpl := `<!DOCTYPE html><html><body><p>Toga v{{printf "1.0"}}</p></body></html>`
	os.WriteFile(tmplPath, []byte(tmpl), 0o644)

	proxy := &goproxy.Goproxy{}
	cfg := &config.Config{HomeTemplatePath: tmplPath}
	handler := buildHandler(proxy, cfg, slog.Default())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if !strings.Contains(w.Body.String(), "Toga v1.0") {
		t.Errorf("expected rendered template, got %q", w.Body.String())
	}
}

// --- Benchmarks ---

func BenchmarkHealthHandler(b *testing.B) {
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		w := httptest.NewRecorder()
		healthHandler(w, req)
	}
}

func BenchmarkBuildHandlerRouting(b *testing.B) {
	proxy := &goproxy.Goproxy{}
	cfg := &config.Config{}
	handler := buildHandler(proxy, cfg, slog.Default())

	paths := []string{"/healthz", "/readyz", "/robots.txt"}

	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodGet, paths[i%len(paths)], nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
	}
}
