// Command server is the composition root: it loads configuration, applies
// migrations, wires the database pool, repositories, services and
// transports, starts the HTTP+WebSocket server, and shuts it down
// gracefully on SIGINT/SIGTERM. This is the only place concrete pgx and
// gorilla types are constructed; everything else depends on the port
// interfaces.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/drzbraz/dice-bet/internal/config"
	"github.com/drzbraz/dice-bet/internal/infrastructure/cache"
	"github.com/drzbraz/dice-bet/internal/infrastructure/random"
	"github.com/drzbraz/dice-bet/internal/repository/postgres"
	"github.com/drzbraz/dice-bet/internal/service"
	httptransport "github.com/drzbraz/dice-bet/internal/transport/http"
	"github.com/drzbraz/dice-bet/internal/transport/ws"
)

const shutdownTimeout = 15 * time.Second

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	if err := run(logger); err != nil {
		logger.Error("server exited with error", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	if cfg.RunMigrations {
		logger.Info("applying database migrations")
		if err := postgres.RunMigrations(cfg.DatabaseURL); err != nil {
			return fmt.Errorf("run migrations: %w", err)
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := postgres.NewPool(ctx, postgres.PoolConfig{
		DatabaseURL: cfg.DatabaseURL,
		MaxConns:    cfg.DBMaxConns,
	})
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	defer pool.Close()

	walletRepo := postgres.NewWalletRepository(pool)
	playRepo := postgres.NewPlayRepository(pool)
	transactionRepo := postgres.NewTransactionRepository(pool)
	idempotencyRepo := postgres.NewIdempotencyRepository(pool)
	txManager := postgres.NewTxManager(pool)
	roller := random.NewCryptoRoller()
	// Shared by both services below: GameService writes through to it on
	// every committed mutation, WalletService reads from it cache-aside.
	// See internal/port/cache.go for why this exists.
	walletCache := cache.NewMemoryCache(cfg.WalletCacheTTL)

	// nil when PROVABLY_FAIR_ENABLED=false: GameService.Play then rolls via
	// roller exactly as before this feature existed. See README "Provably
	// fair rolls".
	var fairnessSvc *service.FairnessService
	if cfg.ProvablyFairEnabled {
		fairnessRepo := postgres.NewFairnessRepository(pool)
		seedGen := random.NewCryptoSeedGenerator()
		fairnessSvc = service.NewFairnessService(fairnessRepo, seedGen, txManager)
	}

	walletSvc := service.NewWalletService(walletRepo, walletCache)
	gameSvc := service.NewGameService(walletRepo, playRepo, transactionRepo, idempotencyRepo, roller, txManager, walletCache, fairnessSvc, cfg.Game)

	wsRouter := ws.NewRouter()
	ws.NewController(walletSvc, gameSvc, fairnessSvc).RegisterRoutes(wsRouter)
	wsCfg := ws.DefaultConfig()
	wsCfg.MaxMessageBytes = cfg.WSMaxMessageBytes
	wsCfg.PingInterval = cfg.WSPingInterval
	wsCfg.WriteTimeout = cfg.WriteTimeout
	// Deliberately NOT cfg.ReadTimeout: that's an HTTP request timeout, a
	// different concern from a WS connection's idle tolerance. A player
	// sitting on an open round with no traffic is normal; only pings keep
	// the connection alive, so the read deadline must comfortably outlast
	// the ping interval or every idle connection gets killed before its
	// first ping ever arrives.
	wsCfg.ReadTimeout = 2 * cfg.WSPingInterval
	wsServer := ws.NewServer(wsRouter, wsCfg, logger)

	httpController := httptransport.NewController(walletSvc, gameSvc, fairnessSvc)
	httpRouter := httptransport.NewRouter(httpController, pool.Ping)

	mux := http.NewServeMux()
	mux.Handle("/ws", wsServer)
	mux.Handle("/", httpRouter)

	httpServer := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      mux,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
	}

	serverErrCh := make(chan error, 1)
	go func() {
		logger.Info("server starting", "port", cfg.Port)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErrCh <- err
			return
		}
		serverErrCh <- nil
	}()

	select {
	case <-ctx.Done():
		logger.Info("shutdown signal received, draining in-flight requests")
	case err := <-serverErrCh:
		if err != nil {
			return fmt.Errorf("server error: %w", err)
		}
		return nil
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	// httpServer.Shutdown must run first: it closes the listener so no new
	// connections (including new "/ws" upgrades) can be accepted. Only once
	// that happens is wsServer's tracked-connection snapshot final; calling
	// it first would leave a window where a newly-accepted WS connection is
	// covered by neither shutdown path (net/http.Server.Shutdown does not
	// track hijacked connections, so it would never close one either).
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}
	if err := wsServer.Shutdown(shutdownCtx); err != nil {
		logger.Warn("websocket connections did not all close cleanly before the deadline", "error", err)
	}

	logger.Info("server stopped cleanly")
	return nil
}
