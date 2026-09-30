package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestComputeRoll_GoldenVectors locks in the exact algorithm: the frontend
// independently reimplements this same computation (via Web Crypto) to
// verify a revealed seed in the browser, so a silent change here would
// break every already-shown "verify" result without any test catching it
// unless the expected outputs are pinned like this.
func TestComputeRoll_GoldenVectors(t *testing.T) {
	const serverSeed = "test-server-seed"
	const clientSeed = "test-client-seed"

	want := map[int64]int{0: 1, 1: 6, 2: 6, 3: 4, 4: 1}
	for nonce, expected := range want {
		assert.Equal(t, expected, ComputeRoll(serverSeed, clientSeed, nonce), "nonce=%d", nonce)
	}
}

func TestComputeRoll_IsDeterministic(t *testing.T) {
	a := ComputeRoll("seed-a", "client-a", 7)
	b := ComputeRoll("seed-a", "client-a", 7)
	assert.Equal(t, a, b)
}

func TestComputeRoll_DifferentNonceUsuallyDiffersAndAlwaysInRange(t *testing.T) {
	seen := make(map[int]bool)
	for nonce := int64(0); nonce < 50; nonce++ {
		roll := ComputeRoll("seed", "client", nonce)
		assert.GreaterOrEqual(t, roll, 1)
		assert.LessOrEqual(t, roll, 6)
		seen[roll] = true
	}
	assert.Greater(t, len(seen), 1, "50 different nonces should not all collide on the same face")
}

func TestComputeRoll_DifferentServerSeedChangesResult(t *testing.T) {
	a := ComputeRoll("seed-a", "client", 0)
	b := ComputeRoll("seed-b", "client", 0)
	assert.NotEqual(t, a, b, "different server seeds should (overwhelmingly likely) produce different rolls")
}

func TestHashServerSeed_MatchesPlainSHA256Hex(t *testing.T) {
	sum := sha256.Sum256([]byte("my-secret-seed"))
	assert.Equal(t, hex.EncodeToString(sum[:]), HashServerSeed("my-secret-seed"))
}

func TestHashServerSeed_DifferentSeedsHaveDifferentHashes(t *testing.T) {
	assert.NotEqual(t, HashServerSeed("seed-a"), HashServerSeed("seed-b"))
}

func TestFairnessSeed_IsActive(t *testing.T) {
	active := &FairnessSeed{}
	assert.True(t, active.IsActive())

	retired := &FairnessSeed{}
	now := time.Now()
	retired.RetiredAt = &now
	assert.False(t, retired.IsActive())
}
