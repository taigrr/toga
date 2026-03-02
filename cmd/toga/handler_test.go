package main

import (
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

func TestBuildHandlerHealthz(t *testing.T) {
	proxy := &goproxy.Goproxy{}
	cfg := &config.Config{}
	handler := buildHandler(proxy, nil, nil, cfg, slog.Default())

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
	handler := buildHandler(proxy, nil, nil, cfg, slog.Default())

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
	handler := buildHandler(proxy, nil, nil, cfg, slog.Default())

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
	handler := buildHandler(proxy, nil, nil, cfg, slog.Default())

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
	handler := buildHandler(proxy, nil, nil, cfg, slog.Default())

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
	handler := buildHandler(proxy, nil, nil, cfg, slog.Default())

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
	handler := buildHandler(proxy, nil, nil, cfg, slog.Default())

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
	handler := buildHandler(proxy, nil, nil, cfg, slog.Default())

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
	handler := buildHandler(proxy, nil, nil, cfg, slog.Default())

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
	handler := buildHandler(proxy, nil, nil, cfg, slog.Default())

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

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
	handler := buildHandler(proxy, nil, nil, cfg, slog.Default())

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

func TestHomepageTemplateExecution(t *testing.T) {
	dir := t.TempDir()
	tmplPath := filepath.Join(dir, "home.html")
	tmpl := `<!DOCTYPE html><html><body><p>Toga v{{printf "1.0"}}</p></body></html>`
	os.WriteFile(tmplPath, []byte(tmpl), 0o644)

	proxy := &goproxy.Goproxy{}
	cfg := &config.Config{HomeTemplatePath: tmplPath}
	handler := buildHandler(proxy, nil, nil, cfg, slog.Default())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if !strings.Contains(w.Body.String(), "Toga v1.0") {
		t.Errorf("expected rendered template, got %q", w.Body.String())
	}
}

func BenchmarkHealthHandler(b *testing.B) {
	for b.Loop() {
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		w := httptest.NewRecorder()
		healthHandler(w, req)
	}
}

func BenchmarkBuildHandlerRouting(b *testing.B) {
	proxy := &goproxy.Goproxy{}
	cfg := &config.Config{}
	handler := buildHandler(proxy, nil, nil, cfg, slog.Default())

	paths := []string{"/healthz", "/readyz", "/robots.txt"}

	for b.Loop() {
		req := httptest.NewRequest(http.MethodGet, paths[b.N%len(paths)], nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
	}
}
