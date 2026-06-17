package main

import (
	"database/sql"
	"log"
	"net/http"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/sodrety/kopdes/internal/app"
	_ "modernc.org/sqlite"
)

func main() {
	cfg, err := app.ConfigFromEnv()
	if err != nil {
		log.Fatal(err)
	}

	db, err := sql.Open(cfg.DatabaseDriver, cfg.DatabaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	if err := app.Migrate(db); err != nil {
		log.Fatal(err)
	}
	if err := app.EnsureAdminUser(db, cfg.AdminEmail, cfg.AdminPassword); err != nil {
		log.Fatal(err)
	}

	log.Printf("listening on %s", cfg.Address)
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
