package main

import (
	"crypto/subtle"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"os"
	"os/exec"

	cloudstorage "cloud.google.com/go/storage"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/goproxy/goproxy"
	"github.com/minio/minio-go/v7"
	"github.com/taigrr/log-socket/v2/browser"
	"github.com/taigrr/log-socket/v2/ws"
	"github.com/taigrr/toga/internal/config"
	"github.com/taigrr/toga/internal/web"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

func buildHandler(proxy *goproxy.Goproxy, fetcher *goproxy.GoFetcher, cacher goproxy.Cacher, cfg *config.Config, logger *slog.Logger) http.Handler {
	var handler http.Handler = proxy

	// Apply protocol worker semaphore.
	if cfg.ProtocolWorkers > 0 {
		handler = maxConcurrency(handler, cfg.ProtocolWorkers)
	}

	if cfg.PathPrefix != "" {
		handler = http.StripPrefix(cfg.PathPrefix, handler)
	}
	if cfg.BasicAuthUser != "" {
		handler = basicAuth(handler, cfg.BasicAuthUser, cfg.BasicAuthPass)
	}

	// Wrap with OpenTelemetry if tracing is enabled.
	if cfg.TraceExporter != "" {
		handler = otelhttp.NewHandler(handler, "toga")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthHandler)
	mux.HandleFunc("/readyz", readyHandler(cfg.GoBinary))

	// Robots.txt
	if cfg.RobotsFile != "" {
		mux.HandleFunc("/robots.txt", robotsHandler(cfg.RobotsFile, logger))
	} else {
		mux.HandleFunc("/robots.txt", defaultRobotsHandler)
	}

	// Log-socket viewer + WebSocket
	logPath := cfg.LogSocketPath
	if logPath == "" {
		logPath = "/logs"
	}
	mux.HandleFunc(logPath+"/", browser.LogSocketViewHandler)
	mux.HandleFunc(logPath+"/ws", ws.LogSocketHandler)
	mux.HandleFunc("/api/namespaces", ws.NamespacesHandler)

	// Web UI for module browsing — pick the right lister for the backend.
	lister := newLister(cacher, cfg)

	uiHandler := &web.Handler{
		Lister:  lister,
		Fetcher: fetcher,
		Cacher:  cacher,
		Logger:  logger,
		Prefix:  "/-/ui",
	}
	mux.Handle("/-/ui/", uiHandler)
	mux.Handle("/-/ui", http.RedirectHandler("/-/ui/", http.StatusMovedPermanently))

	// Homepage
	if cfg.HomeTemplatePath != "" {
		mux.HandleFunc("/", homeOrProxy(cfg.HomeTemplatePath, handler, logger))
	} else {
		mux.Handle("/", handler)
	}

	return mux
}

func newLister(cacher goproxy.Cacher, cfg *config.Config) web.Lister {
	type minioBackend interface {
		MinioClient() *minio.Client
		BucketName() string
	}
	type gcsBackend interface {
		StorageClient() *cloudstorage.Client
		BucketName() string
	}
	type azureBackend interface {
		AzblobClient() *azblob.Client
		ContainerName() string
	}

	switch b := cacher.(type) {
	case minioBackend:
		return &web.ObjectStoreLister{
			Client: b.MinioClient(),
			Bucket: b.BucketName(),
		}
	case gcsBackend:
		return &web.GCSLister{
			Client: b.StorageClient(),
			Bucket: b.BucketName(),
		}
	case azureBackend:
		return &web.AzureLister{
			Client:    b.AzblobClient(),
			Container: b.ContainerName(),
		}
	default:
		return &web.DiskLister{Root: cfg.Disk.RootPath}
	}
}

func healthHandler(w http.ResponseWriter, _ *http.Request) {
	fmt.Fprintln(w, "ok")
}

func defaultRobotsHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	fmt.Fprintln(w, "User-agent: *\nDisallow: /")
}

func robotsHandler(path string, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		data, err := os.ReadFile(path)
		if err != nil {
			logger.Warn("failed to read robots.txt", "path", path, "error", err)
			w.Header().Set("Content-Type", "text/plain")
			fmt.Fprintln(w, "User-agent: *\nDisallow: /")
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		w.Write(data)
	}
}

func homeOrProxy(tmplPath string, proxyHandler http.Handler, logger *slog.Logger) http.HandlerFunc {
	// Parse template once at startup rather than per-request.
	tmpl, parseErr := template.ParseFiles(tmplPath)
	if parseErr != nil {
		logger.Warn("failed to parse home template", "path", tmplPath, "error", parseErr)
	}

	return func(w http.ResponseWriter, r *http.Request) {
		// Only serve homepage at exact root path.
		if r.URL.Path != "/" {
			proxyHandler.ServeHTTP(w, r)
			return
		}
		if tmpl == nil {
			proxyHandler.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := tmpl.Execute(w, nil); err != nil {
			logger.Error("failed to render home template", "error", err)
		}
	}
}

// maxConcurrency limits concurrent requests via a semaphore.
func maxConcurrency(next http.Handler, n int) http.Handler {
	sem := make(chan struct{}, n)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case sem <- struct{}{}:
			defer func() { <-sem }()
			next.ServeHTTP(w, r)
		case <-r.Context().Done():
			http.Error(w, "service unavailable", http.StatusServiceUnavailable)
		}
	})
}

func basicAuth(next http.Handler, user, pass string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		userMatch := subtle.ConstantTimeCompare([]byte(u), []byte(user)) == 1
		passMatch := subtle.ConstantTimeCompare([]byte(p), []byte(pass)) == 1
		if !ok || !userMatch || !passMatch {
			w.Header().Set("WWW-Authenticate", `Basic realm="toga"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func readyHandler(goBin string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		if goBin == "" {
			goBin = "go"
		}
		if _, err := exec.LookPath(goBin); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprintf(w, "go binary not found: %s\n", goBin)
			return
		}
		fmt.Fprintln(w, "ok")
	}
}
