package app_test

import (
	"testing"

	"github.com/sodrety/kopdes/internal/app"
)

func TestConfigFromEnvEnablesSecureCookiesForStaging(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://postgres:password@localhost:5432/kopdes?sslmode=disable")
	t.Setenv("JWT_SECRET", "test-secret")
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
	t.Setenv("JWT_SECRET", "test-secret")
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
