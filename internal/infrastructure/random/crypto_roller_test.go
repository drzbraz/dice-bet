package random_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/drzbraz/dice-bet/internal/infrastructure/random"
)

func TestCryptoRoller_RollAlwaysInRange(t *testing.T) {
	r := random.NewCryptoRoller()
	seen := make(map[int]bool)
	for i := 0; i < 1000; i++ {
		v, err := r.Roll(context.Background())
		require.NoError(t, err)
		assert.GreaterOrEqual(t, v, 1)
		assert.LessOrEqual(t, v, 6)
		seen[v] = true
	}
	assert.Len(t, seen, 6, "expected all six faces to appear across 1000 rolls")
}
