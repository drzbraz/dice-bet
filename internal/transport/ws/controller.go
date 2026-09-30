package ws

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/drzbraz/dice-bet/internal/domain"
	"github.com/drzbraz/dice-bet/internal/dto"
	"github.com/drzbraz/dice-bet/internal/service"
)

// Message types understood by the WebSocket transport.
const (
	TypeWalletGet   = "wallet.get"
	TypePlayStart   = "play.start"
	TypePlayEnd     = "play.end"
	TypeSeedGet     = "seed.get"
	TypeSeedRotate  = "seed.rotate"
	TypeSeedHistory = "seed.history"
)

// Controller adapts WalletService/GameService/FairnessService to WS
// message Handlers, decoding request DTOs and encoding response DTOs. It
// contains no business logic of its own. fairness may be nil
// (PROVABLY_FAIR_ENABLED=false): the seed.* handlers then report
// FAIRNESS_DISABLED instead of panicking on a nil pointer.
type Controller struct {
	wallet   *service.WalletService
	game     *service.GameService
	fairness *service.FairnessService
}

// NewController constructs a Controller.
func NewController(wallet *service.WalletService, game *service.GameService, fairness *service.FairnessService) *Controller {
	return &Controller{wallet: wallet, game: game, fairness: fairness}
}

// RegisterRoutes registers every known message type's handler on r.
func (c *Controller) RegisterRoutes(r *Router) {
	r.Register(TypeWalletGet, c.HandleWalletGet)
	r.Register(TypePlayStart, c.HandlePlayStart)
	r.Register(TypePlayEnd, c.HandlePlayEnd)
	r.Register(TypeSeedGet, c.HandleSeedGet)
	r.Register(TypeSeedRotate, c.HandleSeedRotate)
	r.Register(TypeSeedHistory, c.HandleSeedHistory)
}

// HandleWalletGet implements the wallet.get use case.
func (c *Controller) HandleWalletGet(ctx context.Context, requestID string, payload json.RawMessage) (any, error) {
	var req dto.WalletGetRequest
	if err := decodePayload(payload, &req); err != nil {
		return nil, err
	}
	if err := req.Validate(); err != nil {
		return nil, err
	}

	balance, err := c.wallet.GetBalance(ctx, req.ClientID)
	if err != nil {
		return nil, err
	}
	return dto.NewWalletGetResponse(balance), nil
}

// HandlePlayStart implements the play.start use case.
func (c *Controller) HandlePlayStart(ctx context.Context, requestID string, payload json.RawMessage) (any, error) {
	var req dto.PlayStartRequest
	if err := decodePayload(payload, &req); err != nil {
		return nil, err
	}
	if err := req.Validate(); err != nil {
		return nil, err
	}

	outcome, err := c.game.Play(ctx, service.PlayRequest{
		ClientID:  req.ClientID,
		RequestID: requestID,
		BetAmount: req.BetAmount,
		BetType:   domain.BetType(req.BetType),
	})
	if err != nil {
		return nil, err
	}
	return dto.NewPlayStartResponse(outcome), nil
}

// HandlePlayEnd implements the play.end use case.
func (c *Controller) HandlePlayEnd(ctx context.Context, requestID string, payload json.RawMessage) (any, error) {
	var req dto.EndPlayRequest
	if err := decodePayload(payload, &req); err != nil {
		return nil, err
	}
	if err := req.Validate(); err != nil {
		return nil, err
	}

	outcome, err := c.game.EndPlay(ctx, service.EndPlayRequest{
		ClientID:  req.ClientID,
		RequestID: requestID,
	})
	if err != nil {
		return nil, err
	}
	return dto.NewEndPlayResponse(outcome), nil
}

// HandleSeedGet implements the seed.get use case: a client's current,
// safe-to-publish commitment (never the secret serverSeed).
func (c *Controller) HandleSeedGet(ctx context.Context, requestID string, payload json.RawMessage) (any, error) {
	if c.fairness == nil {
		return nil, domain.ErrFairnessDisabled()
	}
	var req dto.SeedGetRequest
	if err := decodePayload(payload, &req); err != nil {
		return nil, err
	}
	if err := req.Validate(); err != nil {
		return nil, err
	}

	seed, err := c.fairness.GetSeed(ctx, req.ClientID)
	if err != nil {
		return nil, err
	}
	return dto.NewSeedResponse(seed), nil
}

// HandleSeedRotate implements the seed.rotate use case: retires the
// client's active seed (revealing it) and activates a new one.
func (c *Controller) HandleSeedRotate(ctx context.Context, requestID string, payload json.RawMessage) (any, error) {
	if c.fairness == nil {
		return nil, domain.ErrFairnessDisabled()
	}
	var req dto.SeedRotateRequest
	if err := decodePayload(payload, &req); err != nil {
		return nil, err
	}
	if err := req.Validate(); err != nil {
		return nil, err
	}

	result, err := c.fairness.RotateSeed(ctx, req.ClientID, req.ClientSeed)
	if err != nil {
		return nil, err
	}
	return dto.NewSeedRotateResponse(result), nil
}

// HandleSeedHistory implements the seed.history use case: a client's
// retired seeds, newest first, each fully revealed and re-verifiable.
func (c *Controller) HandleSeedHistory(ctx context.Context, requestID string, payload json.RawMessage) (any, error) {
	if c.fairness == nil {
		return nil, domain.ErrFairnessDisabled()
	}
	var req dto.SeedGetRequest
	if err := decodePayload(payload, &req); err != nil {
		return nil, err
	}
	if err := req.Validate(); err != nil {
		return nil, err
	}

	history, err := c.fairness.History(ctx, req.ClientID)
	if err != nil {
		return nil, err
	}
	return dto.NewSeedHistoryResponse(history), nil
}

// decodePayload strictly decodes payload into target, rejecting unknown
// fields and reporting any failure as a well-formed INVALID_MESSAGE domain
// error rather than letting the raw JSON error leak to the client.
func decodePayload(payload json.RawMessage, target any) error {
	if len(payload) == 0 {
		return domain.ErrInvalidMessage("payload is required")
	}
	dec := json.NewDecoder(bytes.NewReader(payload))
	dec.DisallowUnknownFields()
	if err := dec.Decode(target); err != nil {
		return domain.ErrInvalidMessage(fmt.Sprintf("invalid payload: %v", err))
	}
	return nil
}
