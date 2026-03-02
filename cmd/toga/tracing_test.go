package main

import (
	"context"
	"testing"

	"github.com/taigrr/toga/internal/config"
)

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
