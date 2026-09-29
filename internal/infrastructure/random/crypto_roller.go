// Package random provides the production DiceRoller implementation backed
// by crypto/rand.
package random

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
)

const dieFaces = 6

// CryptoRoller implements port.DiceRoller using crypto/rand.
type CryptoRoller struct{}

// NewCryptoRoller constructs a CryptoRoller.
func NewCryptoRoller() *CryptoRoller {
	return &CryptoRoller{}
}

// Roll returns a cryptographically random integer in [1,6].
func (r *CryptoRoller) Roll(ctx context.Context) (int, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(dieFaces))
	if err != nil {
		return 0, fmt.Errorf("crypto roller: %w", err)
	}
	return int(n.Int64()) + 1, nil
}
