package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	tc "github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// setupTestPool starts an ephemeral Postgres container, applies all
// migrations, and returns a connected pool. It is skipped in -short mode
// (`make test-unit`) so the container-based suite only runs via
// `make test-integration`, which requires a local Docker daemon.
func setupTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping Postgres integration test in short mode")
	}

	ctx := context.Background()
	container, err := tcpostgres.Run(ctx, "postgres:16-alpine",
		tcpostgres.WithDatabase("dicebet"),
		tcpostgres.WithUsername("dicebet"),
		tcpostgres.WithPassword("dicebet"),
		tc.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		t.Fatalf("start postgres container: %v", err)
	}
	t.Cleanup(func() {
		if err := container.Terminate(context.Background()); err != nil {
			t.Logf("terminate postgres container: %v", err)
		}
	})

	connStr, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}

	if err := RunMigrations(connStr); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	pool, err := NewPool(ctx, PoolConfig{DatabaseURL: connStr})
	if err != nil {
		t.Fatalf("create pool: %v", err)
	}
	t.Cleanup(pool.Close)

	return pool
}

// truncateAll resets all tables between tests so each test starts from a
// clean, isolated schema without paying to restart the container.
func truncateAll(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`TRUNCATE idempotency_keys, wallet_transactions, plays, wallets, clients RESTART IDENTITY CASCADE`)
	if err != nil {
		t.Fatalf("truncate tables: %v", err)
	}
}

// seedClient inserts a client with a wallet at the given starting balance.
func seedClient(t *testing.T, pool *pgxpool.Pool, clientID string, balance int64) {
	t.Helper()
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `INSERT INTO clients (id) VALUES ($1)`, clientID); err != nil {
		t.Fatalf("seed client: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO wallets (client_id, balance, currency) VALUES ($1, $2, 'EUR')`, clientID, balance); err != nil {
		t.Fatalf("seed wallet: %v", err)
	}
}
