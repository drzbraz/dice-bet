package config_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/drzbraz/dice-bet/internal/config"
)

func TestLoad_Defaults(t *testing.T) {
	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, "8080", cfg.Port)
	assert.Equal(t, int64(100), cfg.Game.MinBet)
	assert.Equal(t, int64(1_000_000), cfg.Game.MaxBet)
	assert.True(t, cfg.RunMigrations)
	assert.Equal(t, 30*time.Second, cfg.WalletCacheTTL)
}

func TestLoad_OverridesFromEnv(t *testing.T) {
	t.Setenv("PORT", "9090")
	t.Setenv("MIN_BET", "50")
	t.Setenv("MAX_BET", "20000")
	t.Setenv("RUN_MIGRATIONS", "false")
	t.Setenv("WALLET_CACHE_TTL", "5s")

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, "9090", cfg.Port)
	assert.Equal(t, int64(50), cfg.Game.MinBet)
	assert.Equal(t, int64(20000), cfg.Game.MaxBet)
	assert.False(t, cfg.RunMigrations)
	assert.Equal(t, 5*time.Second, cfg.WalletCacheTTL)
}

func TestLoad_RejectsInvalidBetLimits(t *testing.T) {
	t.Setenv("MIN_BET", "1000")
	t.Setenv("MAX_BET", "100")

	_, err := config.Load()
	assert.Error(t, err)
}

func TestLoad_RejectsEmptyDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "")

	cfg, err := config.Load()
	require.NoError(t, err) // empty env var falls back to the default, not an error
	assert.NotEmpty(t, cfg.DatabaseURL)
}
