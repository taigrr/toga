package main

import (
	"context"
	"fmt"

	"github.com/taigrr/toga/internal/config"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

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
