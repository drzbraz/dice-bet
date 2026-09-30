package dto

import (
	"time"

	"github.com/drzbraz/dice-bet/internal/domain"
	"github.com/drzbraz/dice-bet/internal/service"
)

// SeedGetRequest is the payload for a seed.get request.
type SeedGetRequest struct {
	ClientID string `json:"clientId"`
}

var _ Validator = SeedGetRequest{}

func (r SeedGetRequest) Validate() error {
	if r.ClientID == "" {
		return domain.ErrValidation("clientId", "clientId is required")
	}
	return nil
}

// SeedResponse is the payload for a seed.get.result / the "active" side of
// a seed.rotate.result response: a client's current, safe-to-publish
// commitment. ServerSeed itself is never included while active.
type SeedResponse struct {
	ClientID       string `json:"clientId"`
	ServerSeedHash string `json:"serverSeedHash"`
	ClientSeed     string `json:"clientSeed"`
	Nonce          int64  `json:"nonce"`
}

// NewSeedResponse maps a service.FairnessSeedView to its wire DTO.
func NewSeedResponse(v *service.FairnessSeedView) SeedResponse {
	return SeedResponse{
		ClientID:       v.ClientID,
		ServerSeedHash: v.ServerSeedHash,
		ClientSeed:     v.ClientSeed,
		Nonce:          v.Nonce,
	}
}

// SeedRotateRequest is the payload for a seed.rotate request. ClientSeed
// is optional: a player may supply their own to contribute entropy the
// server could not have chosen around; if omitted, one is generated.
type SeedRotateRequest struct {
	ClientID   string `json:"clientId"`
	ClientSeed string `json:"clientSeed,omitempty"`
}

var _ Validator = SeedRotateRequest{}

func (r SeedRotateRequest) Validate() error {
	if r.ClientID == "" {
		return domain.ErrValidation("clientId", "clientId is required")
	}
	return nil
}

// RetiredSeedResponse is a fully revealed past seed epoch: everything
// needed to independently recompute and verify every roll made under it
// (nonces 0 through FinalNonce-1).
type RetiredSeedResponse struct {
	ClientID       string    `json:"clientId"`
	ServerSeed     string    `json:"serverSeed"`
	ServerSeedHash string    `json:"serverSeedHash"`
	ClientSeed     string    `json:"clientSeed"`
	FinalNonce     int64     `json:"finalNonce"`
	RetiredAt      time.Time `json:"retiredAt"`
}

func newRetiredSeedResponse(v service.RetiredFairnessSeedView) RetiredSeedResponse {
	return RetiredSeedResponse{
		ClientID:       v.ClientID,
		ServerSeed:     v.ServerSeed,
		ServerSeedHash: v.ServerSeedHash,
		ClientSeed:     v.ClientSeed,
		FinalNonce:     v.FinalNonce,
		RetiredAt:      v.RetiredAt,
	}
}

// SeedRotateResponse is the payload for a seed.rotate.result response.
type SeedRotateResponse struct {
	Retired *RetiredSeedResponse `json:"retired,omitempty"`
	Active  SeedResponse         `json:"active"`
}

// NewSeedRotateResponse maps a service.RotateSeedResult to its wire DTO.
func NewSeedRotateResponse(r *service.RotateSeedResult) SeedRotateResponse {
	resp := SeedRotateResponse{
		Active: SeedResponse{
			ClientID:       r.Active.ClientID,
			ServerSeedHash: r.Active.ServerSeedHash,
			ClientSeed:     r.Active.ClientSeed,
			Nonce:          r.Active.Nonce,
		},
	}
	if r.Retired != nil {
		retired := newRetiredSeedResponse(*r.Retired)
		resp.Retired = &retired
	}
	return resp
}

// SeedHistoryResponse is the payload for a seed.history.result response.
type SeedHistoryResponse struct {
	Seeds []RetiredSeedResponse `json:"seeds"`
}

// NewSeedHistoryResponse maps a slice of service.RetiredFairnessSeedView to
// its wire DTO.
func NewSeedHistoryResponse(views []service.RetiredFairnessSeedView) SeedHistoryResponse {
	seeds := make([]RetiredSeedResponse, 0, len(views))
	for _, v := range views {
		seeds = append(seeds, newRetiredSeedResponse(v))
	}
	return SeedHistoryResponse{Seeds: seeds}
}
