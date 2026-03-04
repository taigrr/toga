// Package main provides the toga CLI — a Go module proxy.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/charmbracelet/fang"
	"github.com/goproxy/goproxy"
	"github.com/spf13/cobra"
	"github.com/taigrr/toga/internal/config"
)

var version = "dev"

const readHeaderTimeout = 5 * time.Second

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
	if err := setGoEnv(cfg); err != nil {
		return err
	}

	proxy := &goproxy.Goproxy{
		ProxiedSumDBs: cfg.ProxiedSumDBs,
		Logger:        newGoproxyLogger(logger),
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
		go runPprof(cfg, logger)
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

func setGoEnv(cfg *config.Config) error {
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
	return nil
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

func runPprof(cfg *config.Config, logger interface{ Info(string, ...any) }) {
	pprofSrv := &http.Server{
		Addr:              cfg.PprofPort,
		Handler:           http.DefaultServeMux,
		ReadHeaderTimeout: readHeaderTimeout,
	}
	logger.Info("starting pprof", "address", cfg.PprofPort)
	if err := pprofSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Info("pprof server failed", "error", err)
	}
}
