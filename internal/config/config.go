// Package config loads application configuration from environment
// variables with sane defaults.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config is the full application configuration.
type Config struct {
	Port          string
	DatabaseURL   string
	DBMaxConns    int32
	RunMigrations bool

	Game GameConfig

	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	WSPingInterval    time.Duration
	WSMaxMessageBytes int64
}

// GameConfig holds the betting limits enforced by the game service. It is
// kept separate from the full Config so the service layer only depends on
// what it actually needs (interface segregation).
type GameConfig struct {
	MinBet int64
	MaxBet int64
}

// Load reads configuration from the environment, applying defaults for
// anything not explicitly set, and validates the result.
func Load() (Config, error) {
	cfg := Config{
		Port:          getEnv("PORT", "8080"),
		DatabaseURL:   getEnv("DATABASE_URL", "postgres://dicebet:dicebet@localhost:5432/dicebet?sslmode=disable"),
		DBMaxConns:    int32(getEnvInt("DB_MAX_CONNS", 10)),
		RunMigrations: getEnvBool("RUN_MIGRATIONS", true),
		Game: GameConfig{
			MinBet: getEnvInt64("MIN_BET", 100),
			MaxBet: getEnvInt64("MAX_BET", 1_000_000),
		},
		ReadTimeout:       getEnvDuration("READ_TIMEOUT", 15*time.Second),
		WriteTimeout:      getEnvDuration("WRITE_TIMEOUT", 15*time.Second),
		WSPingInterval:    getEnvDuration("WS_PING_INTERVAL", 30*time.Second),
		WSMaxMessageBytes: getEnvInt64("WS_MAX_MESSAGE_BYTES", 4096),
	}

	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) validate() error {
	if c.DatabaseURL == "" {
		return fmt.Errorf("DATABASE_URL must not be empty")
	}
	if c.Game.MinBet <= 0 {
		return fmt.Errorf("MIN_BET must be positive")
	}
	if c.Game.MaxBet < c.Game.MinBet {
		return fmt.Errorf("MAX_BET must be >= MIN_BET")
	}
	if c.DBMaxConns <= 0 {
		return fmt.Errorf("DB_MAX_CONNS must be positive")
	}
	return nil
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getEnvInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func getEnvInt64(key string, def int64) int64 {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return def
}

func getEnvBool(key string, def bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return def
}

func getEnvDuration(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}
