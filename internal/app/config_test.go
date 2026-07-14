package app_test

import (
	"testing"

	"github.com/sodrety/kopdes/internal/app"
)

func TestConfigFromEnvEnablesSecureCookiesForStaging(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://postgres:password@localhost:5432/kopdes?sslmode=disable")
	t.Setenv("JWT_SECRET", "0123456789abcdef0123456789abcdef")
	t.Setenv("APP_ENV", "staging")

	cfg, err := app.ConfigFromEnv()
	if err != nil {
		t.Fatalf("ConfigFromEnv returned error: %v", err)
	}

	if !cfg.CookieSecure {
		t.Fatal("expected staging environment to enable secure cookies")
	}
}

func TestConfigFromEnvAllowsCookieSecureOverride(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://postgres:password@localhost:5432/kopdes?sslmode=disable")
	t.Setenv("JWT_SECRET", "0123456789abcdef0123456789abcdef")
	t.Setenv("APP_ENV", "staging")
	t.Setenv("COOKIE_SECURE", "false")

	cfg, err := app.ConfigFromEnv()
	if err != nil {
		t.Fatalf("ConfigFromEnv returned error: %v", err)
	}

	if cfg.CookieSecure {
		t.Fatal("expected COOKIE_SECURE=false to disable secure cookies")
	}
}

func TestConfigFromEnvRejectsWeakJWTSecret(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://postgres:password@localhost:5432/kopdes?sslmode=disable")
	t.Setenv("JWT_SECRET", "short-secret")

	if _, err := app.ConfigFromEnv(); err == nil {
		t.Fatal("expected weak JWT secret to be rejected")
	}
}

func TestConfigFromEnvSetsHTTPTimeoutDefaults(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://postgres:password@localhost:5432/kopdes?sslmode=disable")
	t.Setenv("JWT_SECRET", "0123456789abcdef0123456789abcdef")

	cfg, err := app.ConfigFromEnv()
	if err != nil {
		t.Fatalf("ConfigFromEnv returned error: %v", err)
	}

	if cfg.ReadTimeout == 0 || cfg.WriteTimeout == 0 || cfg.IdleTimeout == 0 {
		t.Fatalf("expected timeout defaults to be set, got read=%s write=%s idle=%s", cfg.ReadTimeout, cfg.WriteTimeout, cfg.IdleTimeout)
	}
}

func TestConfigFromEnvLoadsMemberBackedOfficerBootstrap(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://postgres:password@localhost:5432/kopdes?sslmode=disable")
	t.Setenv("JWT_SECRET", "0123456789abcdef0123456789abcdef")
	t.Setenv("KETUA_UTAMA_MEMBER_ID", "member-ketua")
	t.Setenv("LEGACY_OFFICER_MEMBER_MAPPINGS", `{"legacy@coop.test":"member-manager"}`)

	cfg, err := app.ConfigFromEnv()
	if err != nil {
		t.Fatalf("ConfigFromEnv returned error: %v", err)
	}
	if cfg.KetuaUtamaMemberID != "member-ketua" || cfg.LegacyOfficerMemberMappings["legacy@coop.test"] != "member-manager" {
		t.Fatalf("unexpected Officer bootstrap config: %+v", cfg)
	}
}

func TestConfigFromEnvRejectsInvalidLegacyOfficerMapping(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://postgres:password@localhost:5432/kopdes?sslmode=disable")
	t.Setenv("JWT_SECRET", "0123456789abcdef0123456789abcdef")
	t.Setenv("LEGACY_OFFICER_MEMBER_MAPPINGS", `null`)

	if _, err := app.ConfigFromEnv(); err == nil {
		t.Fatal("expected null legacy Officer mapping to be rejected")
	}
}

func TestConfigFromEnvSetsObservabilityDefaults(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://postgres:password@localhost:5432/kopdes?sslmode=disable")
	t.Setenv("JWT_SECRET", "0123456789abcdef0123456789abcdef")

	cfg, err := app.ConfigFromEnv()
	if err != nil {
		t.Fatalf("ConfigFromEnv returned error: %v", err)
	}

	if cfg.ServiceName != "kopdes" || cfg.ServiceVersion != "development" {
		t.Fatalf("unexpected service metadata: name=%q version=%q", cfg.ServiceName, cfg.ServiceVersion)
	}
	if !cfg.MetricsEnabled {
		t.Fatal("expected metrics to be enabled by default")
	}
	if cfg.TracingEnabled {
		t.Fatal("expected tracing to be disabled by default")
	}
	if cfg.TracingExporter != "stdout" {
		t.Fatalf("expected stdout tracing exporter default, got %q", cfg.TracingExporter)
	}
}

func TestConfigFromEnvAllowsObservabilityOverrides(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://postgres:password@localhost:5432/kopdes?sslmode=disable")
	t.Setenv("JWT_SECRET", "0123456789abcdef0123456789abcdef")
	t.Setenv("SERVICE_NAME", "koperasi")
	t.Setenv("SERVICE_VERSION", "2026.07.10")
	t.Setenv("METRICS_ENABLED", "off")
	t.Setenv("TRACING_ENABLED", "true")
	t.Setenv("TRACING_EXPORTER", "otlp")
	t.Setenv("TRACING_ENDPOINT", "tempo.internal:4317")
	t.Setenv("TRACING_INSECURE", "true")

	cfg, err := app.ConfigFromEnv()
	if err != nil {
		t.Fatalf("ConfigFromEnv returned error: %v", err)
	}

	if cfg.ServiceName != "koperasi" || cfg.ServiceVersion != "2026.07.10" {
		t.Fatalf("unexpected service metadata: name=%q version=%q", cfg.ServiceName, cfg.ServiceVersion)
	}
	if cfg.MetricsEnabled {
		t.Fatal("expected METRICS_ENABLED=off to disable metrics")
	}
	if !cfg.TracingEnabled {
		t.Fatal("expected TRACING_ENABLED=true to enable tracing")
	}
	if cfg.TracingExporter != "otlp" || cfg.TracingEndpoint != "tempo.internal:4317" || !cfg.TracingInsecure {
		t.Fatalf("unexpected tracing config: exporter=%q endpoint=%q insecure=%v", cfg.TracingExporter, cfg.TracingEndpoint, cfg.TracingInsecure)
	}
}

func TestConfigureTracingRejectsUnsupportedExporter(t *testing.T) {
	_, err := app.ConfigureTracing(t.Context(), app.Config{
		TracingEnabled:  true,
		TracingExporter: "unknown",
	})
	if err == nil {
		t.Fatal("expected unsupported tracing exporter to be rejected")
	}
}

func TestOpenDatabaseUsesInstrumentedDriverWhenTracingEnabled(t *testing.T) {
	db, err := app.OpenDatabase(app.Config{
		DatabaseDriver: "sqlite",
		DatabaseURL:    ":memory:",
		TracingEnabled: true,
	})
	if err != nil {
		t.Fatalf("open traced database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := db.PingContext(t.Context()); err != nil {
		t.Fatalf("ping traced database: %v", err)
	}
}
