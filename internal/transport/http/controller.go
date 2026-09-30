package http

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/drzbraz/dice-bet/internal/domain"
	"github.com/drzbraz/dice-bet/internal/dto"
	"github.com/drzbraz/dice-bet/internal/service"
)

const maxHTTPBodyBytes = 1 << 20 // 1 MiB; generous since this transport is used via Postman, not by untrusted clients directly.

// Controller adapts WalletService/GameService/FairnessService to REST
// handlers, decoding request DTOs and encoding response DTOs. It contains
// no business logic. fairness may be nil (PROVABLY_FAIR_ENABLED=false):
// the fairness/* handlers then report FAIRNESS_DISABLED (404) instead of
// panicking on a nil pointer.
type Controller struct {
	wallet   *service.WalletService
	game     *service.GameService
	fairness *service.FairnessService
}

// NewController constructs a Controller.
func NewController(wallet *service.WalletService, game *service.GameService, fairness *service.FairnessService) *Controller {
	return &Controller{wallet: wallet, game: game, fairness: fairness}
}

// GetWallet implements GET /api/v1/clients/{clientId}/wallet.
func (c *Controller) GetWallet(w http.ResponseWriter, r *http.Request) {
	clientID := r.PathValue("clientId")
	if clientID == "" {
		writeError(w, domain.ErrValidation("clientId", "clientId is required"))
		return
	}

	balance, err := c.wallet.GetBalance(r.Context(), clientID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeSuccess(w, "wallet.get.result", "", dto.NewWalletGetResponse(balance))
}

// ListClients implements GET /api/v1/clients. There is no client-creation
// endpoint: this lists whoever was seeded via migration or inserted
// directly against the database (see README "How to run"), primarily to
// populate a player picker in a UI.
func (c *Controller) ListClients(w http.ResponseWriter, r *http.Request) {
	ids, err := c.wallet.ListClients(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeSuccess(w, "clients.list.result", "", dto.NewClientsListResponse(ids))
}

// PostPlay implements POST /api/v1/plays.
func (c *Controller) PostPlay(w http.ResponseWriter, r *http.Request) {
	requestID, ok := requireIdempotencyKey(w, r)
	if !ok {
		return
	}

	var req dto.PlayStartRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeError(w, err)
		return
	}
	if err := req.Validate(); err != nil {
		writeError(w, err)
		return
	}

	outcome, err := c.game.Play(r.Context(), service.PlayRequest{
		ClientID:  req.ClientID,
		RequestID: requestID,
		BetAmount: req.BetAmount,
		BetType:   domain.BetType(req.BetType),
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeSuccess(w, "play.start.result", requestID, dto.NewPlayStartResponse(outcome))
}

// PostEndPlay implements POST /api/v1/plays/end.
func (c *Controller) PostEndPlay(w http.ResponseWriter, r *http.Request) {
	requestID, ok := requireIdempotencyKey(w, r)
	if !ok {
		return
	}

	var req dto.EndPlayRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeError(w, err)
		return
	}
	if err := req.Validate(); err != nil {
		writeError(w, err)
		return
	}

	outcome, err := c.game.EndPlay(r.Context(), service.EndPlayRequest{
		ClientID:  req.ClientID,
		RequestID: requestID,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeSuccess(w, "play.end.result", requestID, dto.NewEndPlayResponse(outcome))
}

// GetSeed implements GET /api/v1/clients/{clientId}/fairness/seed: the
// client's current, safe-to-publish commitment.
func (c *Controller) GetSeed(w http.ResponseWriter, r *http.Request) {
	if c.fairness == nil {
		writeError(w, domain.ErrFairnessDisabled())
		return
	}
	clientID := r.PathValue("clientId")
	if clientID == "" {
		writeError(w, domain.ErrValidation("clientId", "clientId is required"))
		return
	}

	seed, err := c.fairness.GetSeed(r.Context(), clientID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeSuccess(w, "seed.get.result", "", dto.NewSeedResponse(seed))
}

// PostRotateSeed implements POST /api/v1/clients/{clientId}/fairness/rotate:
// retires the client's active seed (revealing it) and activates a new
// one. Body { "clientSeed": "..." } is optional. Deliberately not gated by
// Idempotency-Key: unlike /plays, a rotate is meant to always produce a
// genuinely new seed, never replay a cached one.
func (c *Controller) PostRotateSeed(w http.ResponseWriter, r *http.Request) {
	if c.fairness == nil {
		writeError(w, domain.ErrFairnessDisabled())
		return
	}
	clientID := r.PathValue("clientId")
	if clientID == "" {
		writeError(w, domain.ErrValidation("clientId", "clientId is required"))
		return
	}

	var body struct {
		ClientSeed string `json:"clientSeed,omitempty"`
	}
	if r.ContentLength != 0 {
		if err := decodeJSONBody(w, r, &body); err != nil {
			writeError(w, err)
			return
		}
	}

	result, err := c.fairness.RotateSeed(r.Context(), clientID, body.ClientSeed)
	if err != nil {
		writeError(w, err)
		return
	}
	writeSuccess(w, "seed.rotate.result", "", dto.NewSeedRotateResponse(result))
}

// GetSeedHistory implements GET /api/v1/clients/{clientId}/fairness/history:
// the client's retired seeds, newest first, each fully revealed and
// re-verifiable.
func (c *Controller) GetSeedHistory(w http.ResponseWriter, r *http.Request) {
	if c.fairness == nil {
		writeError(w, domain.ErrFairnessDisabled())
		return
	}
	clientID := r.PathValue("clientId")
	if clientID == "" {
		writeError(w, domain.ErrValidation("clientId", "clientId is required"))
		return
	}

	history, err := c.fairness.History(r.Context(), clientID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeSuccess(w, "seed.history.result", "", dto.NewSeedHistoryResponse(history))
}

func writeSuccess(w http.ResponseWriter, msgType, requestID string, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(dto.NewSuccessResponse(msgType, requestID, data))
}

func writeError(w http.ResponseWriter, err error) {
	status, detail := mapError(err)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(dto.NewErrorResponse(nil, detail))
}

// requireIdempotencyKey reads and validates the Idempotency-Key header
// shared by every mutating endpoint, writing a VALIDATION_ERROR response and
// returning ok=false if it is missing.
func requireIdempotencyKey(w http.ResponseWriter, r *http.Request) (requestID string, ok bool) {
	requestID = r.Header.Get("Idempotency-Key")
	if requestID == "" {
		writeError(w, domain.ErrValidation("Idempotency-Key", "Idempotency-Key header is required"))
		return "", false
	}
	return requestID, true
}

// decodeJSONBody strictly decodes the request body into target, capping
// its size and rejecting unknown fields, reporting any failure as a
// well-formed INVALID_MESSAGE domain error rather than letting the raw
// JSON/IO error leak to the client.
func decodeJSONBody(w http.ResponseWriter, r *http.Request, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxHTTPBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(target); err != nil {
		return domain.ErrInvalidMessage(fmt.Sprintf("invalid request body: %v", err))
	}
	return nil
}
