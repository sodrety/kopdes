package app

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"time"
)

type Config struct {
	Address                     string
	DatabaseDriver              string
	DatabaseURL                 string
	JWTSecret                   string
	KetuaUtamaMemberID          string
	KetuaUtamaEmail             string
	KetuaUtamaPassword          string
	LegacyOfficerMemberMappings map[string]string
	AppEnv                      string
	ServiceName                 string
	ServiceVersion              string
	MetricsEnabled              bool
	TracingEnabled              bool
	TracingExporter             string
	TracingEndpoint             string
	TracingInsecure             bool
	CookieSecure                bool
	ReadTimeout                 time.Duration
	WriteTimeout                time.Duration
	IdleTimeout                 time.Duration
}

func ConfigFromEnv() (Config, error) {
	appEnv := envOrDefault("APP_ENV", "development")
	cfg := Config{
		Address:            envOrDefault("APP_ADDRESS", ":8080"),
		DatabaseDriver:     envOrDefault("DATABASE_DRIVER", "pgx"),
		DatabaseURL:        os.Getenv("DATABASE_URL"),
		JWTSecret:          os.Getenv("JWT_SECRET"),
		KetuaUtamaMemberID: os.Getenv("KETUA_UTAMA_MEMBER_ID"),
		KetuaUtamaEmail:    os.Getenv("KETUA_UTAMA_EMAIL"),
		KetuaUtamaPassword: os.Getenv("KETUA_UTAMA_PASSWORD"),
		AppEnv:             appEnv,
		ServiceName:        envOrDefault("SERVICE_NAME", "kopdes"),
		ServiceVersion:     envOrDefault("SERVICE_VERSION", "development"),
		MetricsEnabled:     !isFalsey(os.Getenv("METRICS_ENABLED")),
		TracingEnabled:     isTruthy(os.Getenv("TRACING_ENABLED")),
		TracingExporter: strings.ToLower(strings.TrimSpace(envOrDefault(
			"TRACING_EXPORTER",
			envOrDefault("OTEL_TRACES_EXPORTER", "stdout"),
		))),
		TracingEndpoint: envOrDefault("TRACING_ENDPOINT", os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")),
		TracingInsecure: isTruthy(envOrDefault("TRACING_INSECURE", os.Getenv("OTEL_EXPORTER_OTLP_INSECURE"))),
		CookieSecure:    cookieSecureFromEnv(appEnv),
		ReadTimeout:     10 * time.Second,
		WriteTimeout:    15 * time.Second,
		IdleTimeout:     60 * time.Second,
	}
	if cfg.DatabaseURL == "" {
		return Config{}, errors.New("DATABASE_URL is required")
	}
	if cfg.JWTSecret == "" {
		return Config{}, errors.New("JWT_SECRET is required")
	}
	if len(cfg.JWTSecret) < 32 {
		return Config{}, errors.New("JWT_SECRET must be at least 32 characters")
	}
	if raw := strings.TrimSpace(os.Getenv("LEGACY_OFFICER_MEMBER_MAPPINGS")); raw != "" {
		if err := json.Unmarshal([]byte(raw), &cfg.LegacyOfficerMemberMappings); err != nil || cfg.LegacyOfficerMemberMappings == nil {
			return Config{}, errors.New("LEGACY_OFFICER_MEMBER_MAPPINGS must be a JSON object of user IDs or emails to Member IDs")
		}
	}
	return cfg, nil
}

func envOrDefault(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func cookieSecureFromEnv(appEnv string) bool {
	value, ok := os.LookupEnv("COOKIE_SECURE")
	if ok {
		return isTruthy(value)
	}
	return appEnv == "staging" || appEnv == "production"
}

func isTruthy(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}

func isFalsey(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "0", "false", "no", "n", "off":
		return true
	default:
		return false
	}
}
