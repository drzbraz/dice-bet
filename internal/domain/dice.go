package domain

// BetType is the player's wager on the parity of the next roll.
type BetType string

const (
	BetTypeEven BetType = "EVEN"
	BetTypeOdd  BetType = "ODD"
)

// Valid reports whether b is one of the known bet types.
func (b BetType) Valid() bool {
	return b == BetTypeEven || b == BetTypeOdd
}

// PlayResult is the outcome of a settled roll.
type PlayResult string

const (
	PlayResultWin  PlayResult = "WIN"
	PlayResultLose PlayResult = "LOSE"
)

// PayoutMultiplier is applied to the bet amount on a win: the player
// receives the bet back plus an equal amount of profit.
const PayoutMultiplier = 2

// IsWin reports whether a die roll in [1,6] satisfies the given bet type.
func IsWin(rolled int, betType BetType) bool {
	isEven := rolled%2 == 0
	switch betType {
	case BetTypeEven:
		return isEven
	case BetTypeOdd:
		return !isEven
	default:
		return false
	}
}

// Result derives the WIN/LOSE outcome for a roll against a bet type.
func Result(rolled int, betType BetType) PlayResult {
	if IsWin(rolled, betType) {
		return PlayResultWin
	}
	return PlayResultLose
}

// Payout returns the amount owed to the player for the given bet amount and
// result: 2x the bet on a win, 0 on a loss.
func Payout(betAmount int64, result PlayResult) int64 {
	if result == PlayResultWin {
		return betAmount * PayoutMultiplier
	}
	return 0
}
