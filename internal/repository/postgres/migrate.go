package postgres

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	migratepgx "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/drzbraz/dice-bet/migrations"
)

// newMigrator builds a *migrate.Migrate against the embedded SQL
// migrations. golang-migrate operates on database/sql, so this opens a
// short-lived *sql.DB via the pgx stdlib driver purely for migrations; the
// application's long-lived pgxpool.Pool used for normal query traffic is
// constructed separately via NewPool. The returned close func closes that
// underlying *sql.DB and must be called once the migrator is no longer
// needed.
func newMigrator(databaseURL string) (m *migrate.Migrate, closeFn func() error, err error) {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, nil, fmt.Errorf("open migration connection: %w", err)
	}

	driver, err := migratepgx.WithInstance(db, &migratepgx.Config{})
	if err != nil {
		_ = db.Close()
		return nil, nil, fmt.Errorf("create migration driver: %w", err)
	}

	sourceDriver, err := iofs.New(migrations.FS, ".")
	if err != nil {
		_ = db.Close()
		return nil, nil, fmt.Errorf("create migration source: %w", err)
	}

	m, err = migrate.NewWithInstance("iofs", sourceDriver, "pgx5", driver)
	if err != nil {
		_ = db.Close()
		return nil, nil, fmt.Errorf("create migrator: %w", err)
	}

	return m, db.Close, nil
}

// RunMigrations applies all pending embedded migrations against
// databaseURL.
func RunMigrations(databaseURL string) error {
	m, closeFn, err := newMigrator(databaseURL)
	if err != nil {
		return err
	}
	defer func() { _ = closeFn() }()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("apply migrations: %w", err)
	}
	return nil
}

// RollbackMigrations reverts all applied embedded migrations against
// databaseURL. It is used by `make migrate-down` for local development;
// production operates via RunMigrations (forward-only) on startup.
func RollbackMigrations(databaseURL string) error {
	m, closeFn, err := newMigrator(databaseURL)
	if err != nil {
		return err
	}
	defer func() { _ = closeFn() }()

	if err := m.Down(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("rollback migrations: %w", err)
	}
	return nil
}
