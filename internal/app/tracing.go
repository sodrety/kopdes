package app

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/XSAM/otelsql"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.38.0"
)

func OpenDatabase(cfg Config) (*sql.DB, error) {
	driverName := cfg.DatabaseDriver
	if cfg.TracingEnabled {
		tracedDriverName, err := instrumentedDatabaseDriver(cfg)
		if err != nil {
			return nil, err
		}
		driverName = tracedDriverName
	}
	return sql.Open(driverName, cfg.DatabaseURL)
}

func instrumentedDatabaseDriver(cfg Config) (string, error) {
	return otelsql.Register(cfg.DatabaseDriver, otelsql.WithAttributes(
		attribute.String("db.system.name", databaseSystemName(cfg.DatabaseDriver)),
	))
}

func ConfigureTracing(ctx context.Context, cfg Config) (func(context.Context) error, error) {
	if !cfg.TracingEnabled {
		return func(context.Context) error { return nil }, nil
	}

	exporter, err := traceExporter(ctx, cfg)
	if err != nil {
		return nil, err
	}

	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName(cfg.observabilityServiceName()),
			semconv.ServiceVersion(cfg.observabilityServiceVersion()),
			semconv.DeploymentEnvironmentName(cfg.AppEnv),
		)),
	)
	otel.SetTracerProvider(tracerProvider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return tracerProvider.Shutdown, nil
}

func traceExporter(ctx context.Context, cfg Config) (sdktrace.SpanExporter, error) {
	switch strings.ToLower(strings.TrimSpace(cfg.TracingExporter)) {
	case "", "stdout", "console":
		return stdouttrace.New(stdouttrace.WithPrettyPrint())
	case "otlp", "otlp-grpc", "grpc":
		options := []otlptracegrpc.Option{}
		if cfg.TracingEndpoint != "" {
			options = append(options, otlptracegrpc.WithEndpoint(strings.TrimPrefix(strings.TrimPrefix(cfg.TracingEndpoint, "https://"), "http://")))
		}
		if cfg.TracingInsecure {
			options = append(options, otlptracegrpc.WithInsecure())
		}
		return otlptracegrpc.New(ctx, options...)
	default:
		return nil, fmt.Errorf("unsupported tracing exporter %q", cfg.TracingExporter)
	}
}

func (cfg Config) observabilityServiceName() string {
	if cfg.ServiceName != "" {
		return cfg.ServiceName
	}
	return "kopdes"
}

func (cfg Config) observabilityServiceVersion() string {
	if cfg.ServiceVersion != "" {
		return cfg.ServiceVersion
	}
	return "development"
}

func databaseSystemName(driverName string) string {
	switch strings.ToLower(strings.TrimSpace(driverName)) {
	case "pgx", "postgres", "postgresql":
		return "postgresql"
	case "sqlite", "sqlite3":
		return "sqlite"
	default:
		return driverName
	}
}
