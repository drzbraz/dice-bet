package domain

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"time"
)

// FairnessSeed is one "epoch" of provably-fair rolls for a client: every
// roll while this seed is active is a deterministic function of
// ServerSeed, ClientSeed, and an incrementing Nonce (see ComputeRoll).
// ServerSeed is secret while the seed is active (RetiredAt == nil) -- only
// ServerSeedHash, its commitment, is safe to hand to a client before that.
// Once retired, ServerSeed is revealed and every roll made under it (nonce
// 0 through Nonce-1) becomes independently verifiable by anyone.
type FairnessSeed struct {
	ID             string
	ClientID       string
	ServerSeed     string
	ServerSeedHash string
	ClientSeed     string
	Nonce          int64
	CreatedAt      time.Time
	RetiredAt      *time.Time
}

// IsActive reports whether this seed is still being used to derive rolls
// (as opposed to retired and revealed).
func (s *FairnessSeed) IsActive() bool {
	return s.RetiredAt == nil
}

// dieFaces mirrors infrastructure/random's non-fair CryptoRoller: rolls
// are in [1,6].
const dieFaces = 6

// ComputeRoll deterministically derives a die roll in [1,6] from
// serverSeed, clientSeed, and nonce. It is a pure function with no
// randomness or I/O of its own -- the only randomness in the whole scheme
// is in how ServerSeed/ClientSeed were originally generated (see
// port.SeedGenerator). Anyone who knows all three inputs, including a
// player verifying independently in a browser, recomputes the exact same
// result: HMAC-SHA256(key=serverSeed, message="clientSeed:nonce"), first 4
// bytes as a big-endian uint32, reduced mod 6. The reduction has a
// negligible bias (2^32 mod 6 = 4, i.e. faces 1-4 are about one in four
// billion more likely than 5-6) which is the same trade-off real
// provably-fair implementations accept rather than pay for rejection
// sampling -- see README "Provably fair rolls".
func ComputeRoll(serverSeed, clientSeed string, nonce int64) int {
	mac := hmac.New(sha256.New, []byte(serverSeed))
	fmt.Fprintf(mac, "%s:%d", clientSeed, nonce)
	sum := mac.Sum(nil)
	n := binary.BigEndian.Uint32(sum[:4])
	return int(n%dieFaces) + 1
}

// HashServerSeed returns the public commitment for serverSeed: what gets
// published before any round is played under it, and what a revealed
// serverSeed must hash back to for a player to trust it wasn't swapped
// after the fact.
func HashServerSeed(serverSeed string) string {
	sum := sha256.Sum256([]byte(serverSeed))
	return hex.EncodeToString(sum[:])
}
