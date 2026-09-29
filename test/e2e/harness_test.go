// Package e2e contains full-stack tests that exercise the service layer
// against a real, ephemeral Postgres instance (via testcontainers),
// including the concurrency guarantees that cannot be proven with fakes.
package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	tc "github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/drzbraz/dice-bet/internal/repository/postgres"
)

func setupTestPool(t *testing.T, maxConns int32) *pgxpool.Pool {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
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

	if err := postgres.RunMigrations(connStr); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	pool, err := postgres.NewPool(ctx, postgres.PoolConfig{DatabaseURL: connStr, MaxConns: maxConns})
	if err != nil {
		t.Fatalf("create pool: %v", err)
	}
	t.Cleanup(pool.Close)

	return pool
}

// truncateAll resets all tables, since the seed migration already inserts
// clients (alice, bob, carol, dave) that would otherwise collide with
// tests seeding their own fixtures under those same ids.
func truncateAll(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`TRUNCATE idempotency_keys, wallet_transactions, plays, wallets, clients RESTART IDENTITY CASCADE`)
	if err != nil {
		t.Fatalf("truncate tables: %v", err)
	}
}

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
