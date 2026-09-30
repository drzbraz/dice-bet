package dto

import (
	"github.com/drzbraz/dice-bet/internal/domain"
	"github.com/drzbraz/dice-bet/internal/service"
)

// PlayStartRequest is the payload for a play.start request. BetType is
// decoded as a plain string here; validating it against the EVEN/ODD
// domain is a business rule enforced by the service layer
// (INVALID_BET_TYPE), not structural DTO validation.
type PlayStartRequest struct {
	ClientID  string `json:"clientId"`
	BetAmount int64  `json:"betAmount"`
	BetType   string `json:"betType"`
}

var _ Validator = PlayStartRequest{}

func (r PlayStartRequest) Validate() error {
	if r.ClientID == "" {
		return domain.ErrValidation("clientId", "clientId is required")
	}
	return nil
}

// PlayStartResponse is the payload for a play.start.result response.
type PlayStartResponse struct {
	PlayID       string `json:"playId"`
	ClientID     string `json:"clientId"`
	BetAmount    int64  `json:"betAmount"`
	BetType      string `json:"betType"`
	RolledNumber int    `json:"rolledNumber"`
	Result       string `json:"result"`
	Payout       int64  `json:"payout"`
	Status       string `json:"status"`
	Balance      int64  `json:"balance"`

	// Fairness is present only when PROVABLY_FAIR_ENABLED is on. See
	// README "Provably fair rolls".
	Fairness *PlayFairnessResponse `json:"fairness,omitempty"`
}

// PlayFairnessResponse is enough to later verify RolledNumber once the
// seed epoch it was played under is revealed (via seed.rotate): recompute
// the roll from the revealed serverSeed, ClientSeed, and Nonce and confirm
// it matches.
type PlayFairnessResponse struct {
	ServerSeedHash string `json:"serverSeedHash"`
	ClientSeed     string `json:"clientSeed"`
	Nonce          int64  `json:"nonce"`
}

// NewPlayStartResponse maps a service.PlayOutcome to its wire DTO.
func NewPlayStartResponse(o *service.PlayOutcome) PlayStartResponse {
	resp := PlayStartResponse{
		PlayID:       o.PlayID,
		ClientID:     o.ClientID,
		BetAmount:    o.BetAmount,
		BetType:      string(o.BetType),
		RolledNumber: o.RolledNumber,
		Result:       string(o.Result),
		Payout:       o.Payout,
		Status:       string(o.Status),
		Balance:      o.Balance,
	}
	if o.Fairness != nil {
		resp.Fairness = &PlayFairnessResponse{
			ServerSeedHash: o.Fairness.ServerSeedHash,
			ClientSeed:     o.Fairness.ClientSeed,
			Nonce:          o.Fairness.Nonce,
		}
	}
	return resp
}
