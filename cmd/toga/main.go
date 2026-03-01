// Package main provides the toga CLI — a Go module proxy.
package main

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/charmbracelet/fang"
	"github.com/goproxy/goproxy"
	"github.com/spf13/cobra"
	"github.com/taigrr/toga/internal/config"
	"github.com/taigrr/toga/internal/storage/azureblob"
	"github.com/taigrr/toga/internal/storage/disk"
	"github.com/taigrr/toga/internal/storage/gcs"
	miniocacher "github.com/taigrr/toga/internal/storage/minio"
	s3cacher "github.com/taigrr/toga/internal/storage/s3"
)

var version = "dev"

func main() {
	var configFile string

	rootCmd := &cobra.Command{
		Use:   "toga",
		Short: "A Go module proxy — drop-in replacement for Athens",
		Long:  "Toga is a Go module proxy powered by goproxy. It supports disk, S3, MinIO, GCS, and Azure Blob storage backends.",
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

	logger := newLogger(cfg.LogLevel)

	ctx, cancel := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	cacher, err := newCacher(ctx, cfg)
	if err != nil {
		return fmt.Errorf("initialize storage backend %q: %w", cfg.StorageType, err)
	}
	if closer, ok := cacher.(io.Closer); ok {
		defer closer.Close()
	}

	fetcher := &goproxy.GoFetcher{
		GoBin:   cfg.GoBinary,
		TempDir: os.TempDir(),
	}

	// TODO: implement NetworkMode (strict/offline/fallback) to control
	// whether toga serves from cache only, upstream only, or falls back.

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
		Fetcher:       fetcher,
		Cacher:        cacher,
		ProxiedSumDBs: cfg.ProxiedSumDBs,
		Logger:        logger,
	}

	handler := buildHandler(proxy, cfg)

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

	logger.Info("starting toga", "address", ln.Addr().String(), "storage", cfg.StorageType)

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

func newLogger(level string) *slog.Logger {
	var logLevel slog.Level
	switch level {
	case "debug":
		logLevel = slog.LevelDebug
	case "warn":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	default:
		logLevel = slog.LevelInfo
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel}))
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

func buildHandler(proxy *goproxy.Goproxy, cfg *config.Config) http.Handler {
	var handler http.Handler = proxy
	if cfg.PathPrefix != "" {
		handler = http.StripPrefix(cfg.PathPrefix, handler)
	}
	if cfg.BasicAuthUser != "" {
		handler = basicAuth(handler, cfg.BasicAuthUser, cfg.BasicAuthPass)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthHandler)
	mux.HandleFunc("/readyz", healthHandler)
	mux.Handle("/", handler)

	return mux
}

func healthHandler(w http.ResponseWriter, _ *http.Request) {
	fmt.Fprintln(w, "ok")
}

func newCacher(ctx context.Context, cfg *config.Config) (goproxy.Cacher, error) {
	if err := validateStorage(cfg); err != nil {
		return nil, err
	}

	switch cfg.StorageType {
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
