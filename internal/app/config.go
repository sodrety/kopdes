package app

import (
	"errors"
	"os"
	"strings"
)

type Config struct {
	Address        string
	DatabaseDriver string
	DatabaseURL    string
	JWTSecret      string
	AdminEmail     string
	AdminPassword  string
	AppEnv         string
	CookieSecure   bool
}

func ConfigFromEnv() (Config, error) {
	appEnv := envOrDefault("APP_ENV", "development")
	cfg := Config{
		Address:        envOrDefault("APP_ADDRESS", ":8080"),
		DatabaseDriver: envOrDefault("DATABASE_DRIVER", "pgx"),
		DatabaseURL:    os.Getenv("DATABASE_URL"),
		JWTSecret:      os.Getenv("JWT_SECRET"),
		AdminEmail:     os.Getenv("ADMIN_EMAIL"),
		AdminPassword:  os.Getenv("ADMIN_PASSWORD"),
		AppEnv:         appEnv,
		CookieSecure:   cookieSecureFromEnv(appEnv),
	}
	if cfg.DatabaseURL == "" {
		return Config{}, errors.New("DATABASE_URL is required")
	}
	if cfg.JWTSecret == "" {
		return Config{}, errors.New("JWT_SECRET is required")
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
