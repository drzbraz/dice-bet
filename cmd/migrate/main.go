// Command migrate applies or rolls back the embedded SQL migrations
// against DATABASE_URL. It is a thin CLI wrapper used by `make migrate-up`
// / `make migrate-down`; the server itself applies migrations on startup
// via internal/repository/postgres.RunMigrations when RUN_MIGRATIONS=true.
package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/drzbraz/dice-bet/internal/repository/postgres"
)

func main() {
	direction := flag.String("direction", "up", "migration direction: up or down")
	flag.Parse()

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		fmt.Fprintln(os.Stderr, "DATABASE_URL must be set")
		os.Exit(1)
	}

	var err error
	switch *direction {
	case "up":
		err = postgres.RunMigrations(databaseURL)
	case "down":
		err = postgres.RollbackMigrations(databaseURL)
	default:
		fmt.Fprintf(os.Stderr, "unknown -direction %q: must be up or down\n", *direction)
		os.Exit(1)
	}
	if err != nil {
		slog.Error("migration failed", "direction", *direction, "error", err)
		os.Exit(1)
	}
	slog.Info("migration succeeded", "direction", *direction)
}
