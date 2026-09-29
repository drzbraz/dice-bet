package domain_test

import (
	"errors"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/drzbraz/dice-bet/internal/domain"
)

func TestWallet_Debit_Success(t *testing.T) {
	w := &domain.Wallet{ClientID: "alice", Balance: 1000}
	err := w.Debit(400)
	require.NoError(t, err)
	assert.Equal(t, int64(600), w.Balance)
	assert.Equal(t, int64(1), w.Version)
}

func TestWallet_Debit_RejectsAmountExceedingBalance(t *testing.T) {
	w := &domain.Wallet{ClientID: "alice", Balance: 100}
	err := w.Debit(200)
	var domainErr *domain.Error
	require.True(t, errors.As(err, &domainErr))
	assert.Equal(t, domain.ErrCodeInsufficientBalance, domainErr.Code)
	assert.Equal(t, int64(100), w.Balance, "balance must be unchanged on rejection")
}

func TestWallet_Debit_RejectsNonPositiveAmount(t *testing.T) {
	for _, amount := range []int64{0, -50} {
		w := &domain.Wallet{ClientID: "alice", Balance: 100}
		err := w.Debit(amount)
		var domainErr *domain.Error
		require.True(t, errors.As(err, &domainErr))
		assert.Equal(t, domain.ErrCodeInternal, domainErr.Code)
	}
}

func TestWallet_Credit_Success(t *testing.T) {
	w := &domain.Wallet{ClientID: "alice", Balance: 500}
	err := w.Credit(1000)
	require.NoError(t, err)
	assert.Equal(t, int64(1500), w.Balance)
	assert.Equal(t, int64(1), w.Version)
}

func TestWallet_Credit_AllowsZero(t *testing.T) {
	w := &domain.Wallet{ClientID: "alice", Balance: 500}
	err := w.Credit(0)
	require.NoError(t, err)
	assert.Equal(t, int64(500), w.Balance)
}

func TestWallet_Credit_RejectsNegativeAmount(t *testing.T) {
	w := &domain.Wallet{ClientID: "alice", Balance: 500}
	err := w.Credit(-1)
	var domainErr *domain.Error
	require.True(t, errors.As(err, &domainErr))
	assert.Equal(t, domain.ErrCodeInternal, domainErr.Code)
}

func TestWallet_Credit_GuardsAgainstOverflow(t *testing.T) {
	w := &domain.Wallet{ClientID: "alice", Balance: math.MaxInt64 - 10}
	err := w.Credit(20)
	var domainErr *domain.Error
	require.True(t, errors.As(err, &domainErr))
	assert.Equal(t, domain.ErrCodeInternal, domainErr.Code)
	assert.Equal(t, int64(math.MaxInt64-10), w.Balance, "balance must be unchanged on overflow rejection")
}

func TestWallet_Credit_AtOverflowBoundarySucceeds(t *testing.T) {
	w := &domain.Wallet{ClientID: "alice", Balance: math.MaxInt64 - 10}
	err := w.Credit(10)
	require.NoError(t, err)
	assert.Equal(t, int64(math.MaxInt64), w.Balance)
}
