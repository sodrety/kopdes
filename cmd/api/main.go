package main

import (
	"context"
	"log"
	"log/slog"
	"net/http"
	"os"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/sodrety/kopdes/internal/app"
	_ "modernc.org/sqlite"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	cfg, err := app.ConfigFromEnv()
	if err != nil {
		log.Fatal(err)
	}

	tracingShutdown, err := app.ConfigureTracing(context.Background(), cfg)
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := tracingShutdown(ctx); err != nil {
			slog.Error("tracing_shutdown_failed", "error", err)
		}
	}()

	db, err := app.OpenDatabase(cfg)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	if err := app.PrepareLegacyOfficerMappings(db, cfg.LegacyOfficerMemberMappings); err != nil {
		log.Fatal(err)
	}
	if err := app.Migrate(db); err != nil {
		log.Fatal(err)
	}
	if err := app.EnsureKetuaUtamaUser(db, cfg.KetuaUtamaMemberID, cfg.KetuaUtamaEmail, cfg.KetuaUtamaPassword); err != nil {
		log.Fatal(err)
	}

	slog.Info("server_starting", "address", cfg.Address, "service", cfg.ServiceName, "version", cfg.ServiceVersion)
	server := &http.Server{
		Addr:         cfg.Address,
		Handler:      app.NewServer(cfg, db),
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
		IdleTimeout:  cfg.IdleTimeout,
	}
	if err := server.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}
