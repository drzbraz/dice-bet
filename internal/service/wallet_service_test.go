package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/drzbraz/dice-bet/internal/domain"
)

func TestWalletService_GetBalance_ReturnsBalance(t *testing.T) {
	h := newTestHarness(100, 10000)
	h.seedWallet("alice", 12345)

	out, err := h.wallet.GetBalance(context.Background(), "alice")
	require.NoError(t, err)
	assert.Equal(t, "alice", out.ClientID)
	assert.Equal(t, int64(12345), out.Balance)
	assert.Equal(t, "EUR", out.Currency)
}

func TestWalletService_GetBalance_RejectsUnknownClient(t *testing.T) {
	h := newTestHarness(100, 10000)

	_, err := h.wallet.GetBalance(context.Background(), "ghost")
	requireDomainErr(t, err, domain.ErrCodeClientNotFound)
}

func TestWalletService_GetBalance_RepositoryErrorPropagatesAsInternal(t *testing.T) {
	svc := NewWalletService(&failingWalletRepository{err: errors.New("db down")})

	_, err := svc.GetBalance(context.Background(), "alice")
	requireDomainErr(t, err, domain.ErrCodeInternal)
}
