package port

import "context"

// DiceRoller produces a single die roll in [1,6].
type DiceRoller interface {
	Roll(ctx context.Context) (int, error)
}
