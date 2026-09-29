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
	TypeWalletGet = "wallet.get"
	TypePlayStart = "play.start"
	TypePlayEnd   = "play.end"
)

// Controller adapts WalletService/GameService to WS message Handlers,
// decoding request DTOs and encoding response DTOs. It contains no
// business logic of its own.
type Controller struct {
	wallet *service.WalletService
	game   *service.GameService
}

// NewController constructs a Controller.
func NewController(wallet *service.WalletService, game *service.GameService) *Controller {
	return &Controller{wallet: wallet, game: game}
}

// RegisterRoutes registers every known message type's handler on r.
func (c *Controller) RegisterRoutes(r *Router) {
	r.Register(TypeWalletGet, c.HandleWalletGet)
	r.Register(TypePlayStart, c.HandlePlayStart)
	r.Register(TypePlayEnd, c.HandlePlayEnd)
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
