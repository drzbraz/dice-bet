package random

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCryptoSeedGenerator_GenerateServerSeed_Returns64HexChars(t *testing.T) {
	g := NewCryptoSeedGenerator()

	seed, err := g.GenerateServerSeed()
	require.NoError(t, err)
	assert.Len(t, seed, 64) // 32 bytes hex-encoded
}

func TestCryptoSeedGenerator_GenerateClientSeed_Returns32HexChars(t *testing.T) {
	g := NewCryptoSeedGenerator()

	seed, err := g.GenerateClientSeed()
	require.NoError(t, err)
	assert.Len(t, seed, 32) // 16 bytes hex-encoded
}

func TestCryptoSeedGenerator_SuccessiveCallsDiffer(t *testing.T) {
	g := NewCryptoSeedGenerator()

	a, err := g.GenerateServerSeed()
	require.NoError(t, err)
	b, err := g.GenerateServerSeed()
	require.NoError(t, err)
	assert.NotEqual(t, a, b)
}
