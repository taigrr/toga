package main

import (
	"context"
	"log/slog"
	"os"
	"strings"

	logslog "github.com/taigrr/log-socket/v2/slog"
	"github.com/taigrr/toga/internal/config"
)

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

// newGoproxyLogger returns a logger for goproxy that downgrades expected probe errors.
func newGoproxyLogger(base *slog.Logger) *slog.Logger {
	return slog.New(&goproxyHandler{inner: base.Handler()})
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

// goproxyHandler wraps a handler to downgrade expected goproxy errors to debug level.
// Go's module resolution probes subpaths before finding the actual module root,
// which causes "no matching versions" errors that are normal behavior.
type goproxyHandler struct {
	inner slog.Handler
}

func (g *goproxyHandler) Enabled(ctx context.Context, level slog.Level) bool {
	// Always enable to allow downgrading errors to debug.
	if level == slog.LevelError {
		return g.inner.Enabled(ctx, slog.LevelDebug)
	}
	return g.inner.Enabled(ctx, level)
}

func (g *goproxyHandler) Handle(ctx context.Context, r slog.Record) error {
	if r.Level == slog.LevelError && isExpectedProbeError(r) {
		r = slog.NewRecord(r.Time, slog.LevelDebug, r.Message, r.PC)
	}
	return g.inner.Handle(ctx, r)
}

func (g *goproxyHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &goproxyHandler{inner: g.inner.WithAttrs(attrs)}
}

func (g *goproxyHandler) WithGroup(name string) slog.Handler {
	return &goproxyHandler{inner: g.inner.WithGroup(name)}
}

// isExpectedProbeError returns true for errors that are expected during normal
// Go module resolution (e.g., probing subpaths before finding module root).
func isExpectedProbeError(r slog.Record) bool {
	// Check message and error attribute for expected patterns.
	if strings.Contains(r.Message, "no matching versions") {
		return true
	}
	var found bool
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == "error" {
			errStr := a.Value.String()
			if strings.Contains(errStr, "no matching versions") {
				found = true
				return false
			}
		}
		return true
	})
	return found
}
