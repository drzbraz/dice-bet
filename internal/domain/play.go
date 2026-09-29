package domain

import "time"

// PlayStatus is the lifecycle state of a Play. OPEN -> CLOSED is the only
// transition; CLOSED is terminal.
type PlayStatus string

const (
	PlayStatusOpen   PlayStatus = "OPEN"
	PlayStatusClosed PlayStatus = "CLOSED"
)

// Play is a single bet: the roll and its result/payout are computed and
// stored at creation time (status OPEN), and the payout is credited when
// the play is settled via EndPlay (status CLOSED).
type Play struct {
	ID           string
	ClientID     string
	BetAmount    int64
	BetType      BetType
	RolledNumber int
	Result       PlayResult
	Payout       int64
	Status       PlayStatus
	CreatedAt    time.Time
	ClosedAt     *time.Time
}

// Close transitions the play to CLOSED, recording the settlement time.
func (p *Play) Close(closedAt time.Time) {
	p.Status = PlayStatusClosed
	p.ClosedAt = &closedAt
}
