package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/drzbraz/dice-bet/internal/domain"
)

func TestIsWin_AllFacesBothBetTypes(t *testing.T) {
	cases := []struct {
		rolled  int
		betType domain.BetType
		want    bool
	}{
		{1, domain.BetTypeEven, false}, {1, domain.BetTypeOdd, true},
		{2, domain.BetTypeEven, true}, {2, domain.BetTypeOdd, false},
		{3, domain.BetTypeEven, false}, {3, domain.BetTypeOdd, true},
		{4, domain.BetTypeEven, true}, {4, domain.BetTypeOdd, false},
		{5, domain.BetTypeEven, false}, {5, domain.BetTypeOdd, true},
		{6, domain.BetTypeEven, true}, {6, domain.BetTypeOdd, false},
	}
	for _, tc := range cases {
		got := domain.IsWin(tc.rolled, tc.betType)
		assert.Equalf(t, tc.want, got, "IsWin(%d, %s)", tc.rolled, tc.betType)
	}
}

func TestIsWin_InvalidBetTypeIsAlwaysLose(t *testing.T) {
	assert.False(t, domain.IsWin(2, "PRIME"))
	assert.False(t, domain.IsWin(3, "PRIME"))
}

func TestResult(t *testing.T) {
	assert.Equal(t, domain.PlayResultWin, domain.Result(4, domain.BetTypeEven))
	assert.Equal(t, domain.PlayResultLose, domain.Result(3, domain.BetTypeEven))
}

func TestPayout(t *testing.T) {
	assert.Equal(t, int64(200), domain.Payout(100, domain.PlayResultWin))
	assert.Equal(t, int64(0), domain.Payout(100, domain.PlayResultLose))
}

func TestBetType_Valid(t *testing.T) {
	assert.True(t, domain.BetTypeEven.Valid())
	assert.True(t, domain.BetTypeOdd.Valid())
	assert.False(t, domain.BetType("PRIME").Valid())
}
