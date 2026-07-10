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
}

func TestConfigFromEnvAllowsObservabilityOverrides(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://postgres:password@localhost:5432/kopdes?sslmode=disable")
	t.Setenv("JWT_SECRET", "0123456789abcdef0123456789abcdef")
	t.Setenv("SERVICE_NAME", "koperasi")
	t.Setenv("SERVICE_VERSION", "2026.07.10")
	t.Setenv("METRICS_ENABLED", "off")

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
}
