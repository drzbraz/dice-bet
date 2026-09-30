package random

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// CryptoSeedGenerator implements port.SeedGenerator using crypto/rand.
type CryptoSeedGenerator struct{}

// NewCryptoSeedGenerator constructs a CryptoSeedGenerator.
func NewCryptoSeedGenerator() *CryptoSeedGenerator {
	return &CryptoSeedGenerator{}
}

// GenerateServerSeed returns a 32-byte cryptographically random value,
// hex-encoded, kept secret until its epoch is retired.
func (g *CryptoSeedGenerator) GenerateServerSeed() (string, error) {
	return randomHex(32)
}

// GenerateClientSeed returns a 16-byte cryptographically random value,
// hex-encoded, used as the default when a player doesn't supply their own.
func (g *CryptoSeedGenerator) GenerateClientSeed() (string, error) {
	return randomHex(16)
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate random seed: %w", err)
	}
	return hex.EncodeToString(b), nil
}
