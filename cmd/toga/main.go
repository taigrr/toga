// Package main provides the toga CLI — a Go module proxy.
package main

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	cloudstorage "cloud.google.com/go/storage"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/charmbracelet/fang"
	"github.com/goproxy/goproxy"
	"github.com/minio/minio-go/v7"
	"github.com/spf13/cobra"
	"github.com/taigrr/log-socket/v2/browser"
	logslog "github.com/taigrr/log-socket/v2/slog"
	"github.com/taigrr/log-socket/v2/ws"
	"github.com/taigrr/toga/internal/config"
	"github.com/taigrr/toga/internal/storage/azureblob"
	"github.com/taigrr/toga/internal/storage/disk"
	"github.com/taigrr/toga/internal/storage/gcs"
	"github.com/taigrr/toga/internal/storage/memory"
	miniocacher "github.com/taigrr/toga/internal/storage/minio"
	s3cacher "github.com/taigrr/toga/internal/storage/s3"
	"github.com/taigrr/toga/internal/web"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

var version = "dev"

func main() {
	var configFile string

	rootCmd := &cobra.Command{
		Use:   "toga",
		Short: "A Go module proxy — drop-in replacement for Athens",
		Long:  "Toga is a Go module proxy powered by goproxy. It supports memory, disk, S3, MinIO, GCS, and Azure Blob storage backends.",
		PersistentPreRunE: func(_ *cobra.Command, _ []string) error {
			return config.Init(configFile)
		},
		RunE: runServe,
	}

	rootCmd.PersistentFlags().StringVar(&configFile, "config", "", "path to config file (toml/yaml/json)")

	if err := fang.Execute(context.Background(), rootCmd, fang.WithVersion(version)); err != nil {
		os.Exit(1)
	}
}

func runServe(cmd *cobra.Command, _ []string) error {
	cfg := config.Load()

	logger := newLogger(cfg)

	ctx, cancel := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Initialize tracing if configured.
	shutdownTracer, err := initTracer(ctx, cfg)
	if err != nil {
		return fmt.Errorf("initialize tracing: %w", err)
	}
	if shutdownTracer != nil {
		defer shutdownTracer(context.Background())
	}

	// Set up .netrc from GitHub token if provided.
	if err := setupNetrc(cfg); err != nil {
		return fmt.Errorf("setup netrc: %w", err)
	}

	cacher, err := newCacher(ctx, cfg)
	if err != nil {
		return fmt.Errorf("initialize storage backend %q: %w", cfg.StorageType, err)
	}
	if closer, ok := cacher.(io.Closer); ok {
		defer closer.Close()
	}

	fetcher := &goproxy.GoFetcher{
		GoBin:            cfg.GoBinary,
		Env:              cfg.GoBinaryEnvVars,
		MaxDirectFetches: cfg.GoGetWorkers,
		TempDir:          os.TempDir(),
	}

	// Propagate Go environment variables so the Go toolchain picks them up.
	goEnvVars := map[string]string{
		"GOMODCACHE": cfg.GoModCache,
		"GOPROXY":    cfg.GoProxy,
		"GOPRIVATE":  cfg.GoPrivate,
		"GONOPROXY":  cfg.GoNoProxy,
		"GOSUMDB":    cfg.GoSumDB,
		"GONOSUMDB":  cfg.GoNoSumDB,
	}
	for k, v := range goEnvVars {
		if v != "" {
			if err := os.Setenv(k, v); err != nil {
				return fmt.Errorf("set %s: %w", k, err)
			}
		}
	}

	proxy := &goproxy.Goproxy{
		ProxiedSumDBs: cfg.ProxiedSumDBs,
		Logger:        logger,
	}

	// Apply network mode.
	switch cfg.NetworkMode {
	case "offline":
		// Cache only — no upstream fetching.
		proxy.Cacher = cacher
	case "strict":
		// Upstream only — no caching.
		proxy.Fetcher = fetcher
	default: // "fallback"
		// Cache first, then upstream.
		proxy.Fetcher = fetcher
		proxy.Cacher = cacher
	}

	handler := buildHandler(proxy, fetcher, cacher, cfg, logger)

	srv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: readHeaderTimeout,
		WriteTimeout:      cfg.Timeout,
		ReadTimeout:       cfg.Timeout,
	}

	ln, err := listen(cfg)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	// Start pprof server if enabled.
	if cfg.EnablePprof {
		go func() {
			pprofSrv := &http.Server{
				Addr:              cfg.PprofPort,
				Handler:           http.DefaultServeMux,
				ReadHeaderTimeout: readHeaderTimeout,
			}
			logger.Info("starting pprof", "address", cfg.PprofPort)
			if err := pprofSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				logger.Error("pprof server failed", "error", err)
			}
		}()
	}

	logger.Info("starting toga",
		"address", ln.Addr().String(),
		"storage", cfg.StorageType,
		"network_mode", cfg.NetworkMode,
		"goget_workers", cfg.GoGetWorkers,
		"protocol_workers", cfg.ProtocolWorkers,
	)

	errCh := make(chan error, 1)
	go func() {
		if cfg.TLSCertFile != "" && cfg.TLSKeyFile != "" {
			errCh <- srv.ServeTLS(ln, cfg.TLSCertFile, cfg.TLSKeyFile)
		} else {
			errCh <- srv.Serve(ln)
		}
	}()

	select {
	case <-ctx.Done():
		logger.Info("shutting down")
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("server: %w", err)
		}
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer shutdownCancel()

	return srv.Shutdown(shutdownCtx)
}

const readHeaderTimeout = 5 * time.Second

func parseSlogLevel(level string) slog.Level {
	switch level {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func newLogger(cfg *config.Config) *slog.Logger {
	logLevel := parseSlogLevel(cfg.LogLevel)
	opts := &slog.HandlerOptions{Level: logLevel}

	var stderrHandler slog.Handler
	if cfg.LogFormat == "plain" || cfg.LogFormat == "text" {
		stderrHandler = slog.NewTextHandler(os.Stderr, opts)
	} else {
		stderrHandler = slog.NewJSONHandler(os.Stderr, opts)
	}

	// Fan out to both stderr and log-socket.
	lsHandler := logslog.NewHandler(
		logslog.WithNamespace("toga"),
		logslog.WithLevel(logLevel),
	)
	return slog.New(&multiHandler{handlers: []slog.Handler{stderrHandler, lsHandler}})
}

// multiHandler fans out slog records to multiple handlers.
type multiHandler struct {
	handlers []slog.Handler
}

func (m *multiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, h := range m.handlers {
		if h.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (m *multiHandler) Handle(ctx context.Context, r slog.Record) error {
	var firstErr error
	for _, h := range m.handlers {
		if h.Enabled(ctx, r.Level) {
			if err := h.Handle(ctx, r); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

func (m *multiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	handlers := make([]slog.Handler, len(m.handlers))
	for i, h := range m.handlers {
		handlers[i] = h.WithAttrs(attrs)
	}
	return &multiHandler{handlers: handlers}
}

func (m *multiHandler) WithGroup(name string) slog.Handler {
	handlers := make([]slog.Handler, len(m.handlers))
	for i, h := range m.handlers {
		handlers[i] = h.WithGroup(name)
	}
	return &multiHandler{handlers: handlers}
}

func listen(cfg *config.Config) (net.Listener, error) {
	if cfg.UnixSocket != "" {
		// Remove stale socket from a previous crash.
		if _, err := os.Stat(cfg.UnixSocket); err == nil {
			os.Remove(cfg.UnixSocket)
		}
		return net.Listen("unix", cfg.UnixSocket)
	}
	return net.Listen("tcp", cfg.Port)
}

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
	mux.HandleFunc("/readyz", healthHandler)

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

	// Web UI for module browsing — pick the right lister for the backend.
	var lister web.Lister
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
		lister = &web.ObjectStoreLister{
			Client: b.MinioClient(),
			Bucket: b.BucketName(),
		}
	case gcsBackend:
		lister = &web.GCSLister{
			Client: b.StorageClient(),
			Bucket: b.BucketName(),
		}
	case azureBackend:
		lister = &web.AzureLister{
			Client:    b.AzblobClient(),
			Container: b.ContainerName(),
		}
	default:
		lister = &web.DiskLister{Root: cfg.Disk.RootPath}
	}

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

func newCacher(ctx context.Context, cfg *config.Config) (goproxy.Cacher, error) {
	if err := validateStorage(cfg); err != nil {
		return nil, err
	}

	switch cfg.StorageType {
	case "memory":
		return memory.New(), nil
	case "disk":
		return disk.New(disk.Config{RootPath: cfg.Disk.RootPath}), nil
	case "s3":
		return s3cacher.New(ctx, s3cacher.Config{
			Region:         cfg.S3.Region,
			Key:            cfg.S3.Key,
			Secret:         cfg.S3.Secret,
			Token:          cfg.S3.Token,
			Bucket:         cfg.S3.Bucket,
			Endpoint:       cfg.S3.Endpoint,
			ForcePathStyle: cfg.S3.ForcePathStyle,
		})
	case "minio":
		return miniocacher.New(ctx, miniocacher.Config{
			Endpoint:  cfg.Minio.Endpoint,
			Key:       cfg.Minio.Key,
			Secret:    cfg.Minio.Secret,
			Bucket:    cfg.Minio.Bucket,
			Region:    cfg.Minio.Region,
			EnableSSL: cfg.Minio.EnableSSL,
		})
	case "gcs":
		return gcs.New(ctx, gcs.Config{
			Bucket:          cfg.GCS.Bucket,
			ProjectID:       cfg.GCS.ProjectID,
			CredentialsFile: cfg.GCS.CredentialsFile,
		})
	case "azureblob":
		return azureblob.New(ctx, azureblob.Config{
			AccountName:   cfg.AzureBlob.AccountName,
			AccountKey:    cfg.AzureBlob.AccountKey,
			ContainerName: cfg.AzureBlob.ContainerName,
		})
	default:
		return nil, fmt.Errorf("unknown storage type: %s", cfg.StorageType)
	}
}

func validateStorage(cfg *config.Config) error {
	switch cfg.StorageType {
	case "memory":
		// No validation needed.
	case "disk":
		if cfg.Disk.RootPath == "" {
			return fmt.Errorf("disk storage requires root_path")
		}
	case "s3":
		if cfg.S3.Bucket == "" {
			return fmt.Errorf("s3 storage requires bucket")
		}
	case "minio":
		if cfg.Minio.Endpoint == "" || cfg.Minio.Bucket == "" {
			return fmt.Errorf("minio storage requires endpoint and bucket")
		}
	case "gcs":
		if cfg.GCS.Bucket == "" {
			return fmt.Errorf("gcs storage requires bucket")
		}
	case "azureblob":
		if cfg.AzureBlob.AccountName == "" || cfg.AzureBlob.ContainerName == "" {
			return fmt.Errorf("azureblob storage requires account_name and container_name")
		}
	default:
		return fmt.Errorf("unknown storage type: %s", cfg.StorageType)
	}
	return nil
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

// setupNetrc creates or copies .netrc for private repo access.
func setupNetrc(cfg *config.Config) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	netrcDest := filepath.Join(home, ".netrc")

	// Copy netrc file if specified.
	if cfg.NETRCPath != "" {
		data, err := os.ReadFile(cfg.NETRCPath)
		if err != nil {
			return fmt.Errorf("read netrc %s: %w", cfg.NETRCPath, err)
		}
		return os.WriteFile(netrcDest, data, 0o600)
	}

	// Generate netrc from GitHub token if specified.
	if cfg.GithubToken != "" {
		content := fmt.Sprintf("machine github.com login %s password x-oauth-basic\n", cfg.GithubToken)
		return os.WriteFile(netrcDest, []byte(content), 0o600)
	}

	return nil
}

// initTracer sets up OpenTelemetry tracing. Returns a shutdown function.
func initTracer(ctx context.Context, cfg *config.Config) (func(context.Context) error, error) {
	if cfg.TraceExporter == "" {
		return nil, nil
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String("toga"),
			semconv.ServiceVersionKey.String(version),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("create resource: %w", err)
	}

	var exporter sdktrace.SpanExporter

	switch cfg.TraceExporter {
	case "otlp", "jaeger":
		opts := []otlptracehttp.Option{}
		if cfg.TraceEndpoint != "" {
			opts = append(opts, otlptracehttp.WithEndpoint(cfg.TraceEndpoint))
		}
		exp, err := otlptracehttp.New(ctx, opts...)
		if err != nil {
			return nil, fmt.Errorf("create OTLP exporter: %w", err)
		}
		exporter = exp
	default:
		return nil, fmt.Errorf("unknown trace exporter: %s", cfg.TraceExporter)
	}

	sampler := sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.TraceSampleRate))

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sampler),
	)

	otel.SetTracerProvider(tp)

	return tp.Shutdown, nil
}
