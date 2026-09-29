package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/drzbraz/dice-bet/internal/config"
	"github.com/drzbraz/dice-bet/internal/domain"
	"github.com/drzbraz/dice-bet/internal/port"
)

const (
	operationPlay    = "play.start"
	operationEndPlay = "play.end"

	// idempotencyMaxAttempts bounds retries when a concurrent request for
	// the same (clientID, requestID) is racing to commit its idempotency
	// record first.
	idempotencyMaxAttempts = 3
)

// PlayRequest is the input to the Play use case.
type PlayRequest struct {
	ClientID  string
	RequestID string
	BetAmount int64
	BetType   domain.BetType
}

// PlayOutcome is the transport-agnostic result of a Play. It is also the
// exact shape persisted for idempotent replay.
type PlayOutcome struct {
	PlayID       string            `json:"playId"`
	ClientID     string            `json:"clientId"`
	BetAmount    int64             `json:"betAmount"`
	BetType      domain.BetType    `json:"betType"`
	RolledNumber int               `json:"rolledNumber"`
	Result       domain.PlayResult `json:"result"`
	Payout       int64             `json:"payout"`
	Status       domain.PlayStatus `json:"status"`
	Balance      int64             `json:"balance"`
}

// EndPlayRequest is the input to the EndPlay use case.
type EndPlayRequest struct {
	ClientID  string
	RequestID string
}

// EndPlayOutcome is the transport-agnostic result of an EndPlay.
type EndPlayOutcome struct {
	PlayID         string            `json:"playId"`
	ClientID       string            `json:"clientId"`
	Result         domain.PlayResult `json:"result"`
	CreditedAmount int64             `json:"creditedAmount"`
	Status         domain.PlayStatus `json:"status"`
	Balance        int64             `json:"balance"`
}

// GameService implements the Play and EndPlay use cases.
type GameService struct {
	wallets      port.WalletRepository
	plays        port.PlayRepository
	transactions port.TransactionRepository
	idempotency  port.IdempotencyRepository
	roller       port.DiceRoller
	txManager    port.TxManager
	cfg          config.GameConfig

	now         func() time.Time
	newID       func() string
	idemBackoff func(ctx context.Context, attempt int)
}

// NewGameService constructs a GameService with production defaults for
// clock, ID generation, and idempotency-conflict backoff.
func NewGameService(
	wallets port.WalletRepository,
	plays port.PlayRepository,
	transactions port.TransactionRepository,
	idempotency port.IdempotencyRepository,
	roller port.DiceRoller,
	txManager port.TxManager,
	cfg config.GameConfig,
) *GameService {
	return &GameService{
		wallets:      wallets,
		plays:        plays,
		transactions: transactions,
		idempotency:  idempotency,
		roller:       roller,
		txManager:    txManager,
		cfg:          cfg,
		now:          time.Now,
		newID:        uuid.NewString,
		idemBackoff: func(ctx context.Context, attempt int) {
			t := time.NewTimer(time.Duration(attempt) * 50 * time.Millisecond)
			defer t.Stop()
			select {
			case <-t.C:
			case <-ctx.Done():
			}
		},
	}
}

// Play validates and executes a bet: it debits the wallet, rolls the die,
// and stores an OPEN play with its already-computed (but not yet credited)
// outcome.
func (s *GameService) Play(ctx context.Context, req PlayRequest) (*PlayOutcome, error) {
	requestHash := fingerprint(fmt.Sprintf("betAmount=%d;betType=%s", req.BetAmount, req.BetType))

	return runIdempotent(ctx, s.txManager, s.idempotency, s.idemBackoff, req.ClientID, req.RequestID, operationPlay, requestHash,
		func(ctx context.Context) (*PlayOutcome, error) {
			if err := validatePlayRequest(req, s.cfg); err != nil {
				return nil, err
			}

			wallet, err := s.wallets.GetForUpdate(ctx, req.ClientID)
			if err != nil {
				return nil, wrapRepoErr(err)
			}
			if wallet == nil {
				return nil, domain.ErrClientNotFound(req.ClientID)
			}

			openPlay, err := s.plays.GetOpenForUpdate(ctx, req.ClientID)
			if err != nil {
				return nil, wrapRepoErr(err)
			}
			if openPlay != nil {
				return nil, domain.ErrPlayAlreadyInProgress()
			}

			if req.BetAmount > wallet.Balance {
				return nil, domain.ErrInsufficientBalance()
			}

			rolled, err := s.roller.Roll(ctx)
			if err != nil {
				return nil, domain.ErrInternal(err)
			}
			result := domain.Result(rolled, req.BetType)
			payout := domain.Payout(req.BetAmount, result)

			if err := wallet.Debit(req.BetAmount); err != nil {
				return nil, err
			}
			if err := s.wallets.Update(ctx, wallet); err != nil {
				return nil, wrapRepoErr(err)
			}

			play := &domain.Play{
				ID:           s.newID(),
				ClientID:     req.ClientID,
				BetAmount:    req.BetAmount,
				BetType:      req.BetType,
				RolledNumber: rolled,
				Result:       result,
				Payout:       payout,
				Status:       domain.PlayStatusOpen,
				CreatedAt:    s.now(),
			}
			if err := s.plays.Create(ctx, play); err != nil {
				return nil, wrapRepoErr(err)
			}

			if err := s.recordLedgerEntry(ctx, req.ClientID, play.ID, domain.TransactionTypeBetDebit, req.BetAmount, wallet.Balance); err != nil {
				return nil, err
			}

			return &PlayOutcome{
				PlayID:       play.ID,
				ClientID:     req.ClientID,
				BetAmount:    req.BetAmount,
				BetType:      req.BetType,
				RolledNumber: rolled,
				Result:       result,
				Payout:       payout,
				Status:       play.Status,
				Balance:      wallet.Balance,
			}, nil
		},
	)
}

// EndPlay settles the client's OPEN play: it credits the pending payout
// (zero on a loss) and closes the play. Calling it twice never credits
// twice because the second call finds no OPEN play.
func (s *GameService) EndPlay(ctx context.Context, req EndPlayRequest) (*EndPlayOutcome, error) {
	// EndPlayRequest carries no parameters beyond clientID/requestID (both
	// already part of the idempotency key), so there is nothing to
	// fingerprint; the hash is a constant, effectively skipping the check.
	requestHash := fingerprint("")

	return runIdempotent(ctx, s.txManager, s.idempotency, s.idemBackoff, req.ClientID, req.RequestID, operationEndPlay, requestHash,
		func(ctx context.Context) (*EndPlayOutcome, error) {
			wallet, err := s.wallets.GetForUpdate(ctx, req.ClientID)
			if err != nil {
				return nil, wrapRepoErr(err)
			}
			if wallet == nil {
				return nil, domain.ErrClientNotFound(req.ClientID)
			}

			play, err := s.plays.GetOpenForUpdate(ctx, req.ClientID)
			if err != nil {
				return nil, wrapRepoErr(err)
			}
			if play == nil {
				return nil, domain.ErrNoActivePlay()
			}

			if err := wallet.Credit(play.Payout); err != nil {
				return nil, err
			}
			if err := s.wallets.Update(ctx, wallet); err != nil {
				return nil, wrapRepoErr(err)
			}

			play.Close(s.now())
			if err := s.plays.Close(ctx, play); err != nil {
				return nil, wrapRepoErr(err)
			}

			if err := s.recordLedgerEntry(ctx, req.ClientID, play.ID, domain.TransactionTypePayoutCredit, play.Payout, wallet.Balance); err != nil {
				return nil, err
			}

			return &EndPlayOutcome{
				PlayID:         play.ID,
				ClientID:       req.ClientID,
				Result:         play.Result,
				CreditedAmount: play.Payout,
				Status:         play.Status,
				Balance:        wallet.Balance,
			}, nil
		},
	)
}

// recordLedgerEntry appends a single append-only ledger row for a balance
// change on clientID's wallet caused by playID, shared by Play (BET_DEBIT)
// and EndPlay (PAYOUT_CREDIT).
func (s *GameService) recordLedgerEntry(ctx context.Context, clientID, playID string, txType domain.TransactionType, amount, balanceAfter int64) error {
	entry := &domain.WalletTransaction{
		ID:           s.newID(),
		ClientID:     clientID,
		PlayID:       playID,
		Type:         txType,
		Amount:       amount,
		BalanceAfter: balanceAfter,
		CreatedAt:    s.now(),
	}
	if err := s.transactions.Create(ctx, entry); err != nil {
		return wrapRepoErr(err)
	}
	return nil
}

func validatePlayRequest(req PlayRequest, cfg config.GameConfig) error {
	if req.BetAmount <= 0 {
		return domain.ErrInvalidBetAmount("betAmount must be greater than zero")
	}
	if req.BetAmount < cfg.MinBet || req.BetAmount > cfg.MaxBet {
		return domain.ErrInvalidBetAmount(fmt.Sprintf("betAmount must be between %d and %d", cfg.MinBet, cfg.MaxBet))
	}
	if !req.BetType.Valid() {
		return domain.ErrInvalidBetType(string(req.BetType))
	}
	return nil
}

// fingerprint deterministically hashes s, used to detect a replayed
// requestId whose actual request parameters differ from the original
// request.
func fingerprint(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// wrapRepoErr normalizes a repository error for propagation to callers: a
// *domain.Error (a defense-in-depth constraint mapping) passes through
// unchanged, anything else is treated as an unexpected internal failure.
func wrapRepoErr(err error) error {
	if err == nil {
		return nil
	}
	var domainErr *domain.Error
	if errors.As(err, &domainErr) {
		return domainErr
	}
	return domain.ErrInternal(err)
}

// runIdempotent executes fn inside a single database transaction that also
// owns the idempotency check-and-store, so a repeated requestId for the
// same client always returns the original response without re-executing
// side effects, and the idempotency record is committed atomically with
// the business change (protection rule 9).
//
// If a concurrent request for the same (clientID, requestID) commits its
// idempotency record first, this attempt's transaction (including its
// business-logic mutations) is rolled back and the whole operation is
// retried, so the retry observes and returns the winner's stored response.
func runIdempotent[T any](
	ctx context.Context,
	txManager port.TxManager,
	idem port.IdempotencyRepository,
	backoff func(ctx context.Context, attempt int),
	clientID, requestID, operation, requestHash string,
	fn func(ctx context.Context) (*T, error),
) (*T, error) {
	var lastErr error
	for attempt := 1; attempt <= idempotencyMaxAttempts; attempt++ {
		var result *T
		conflict := false

		txErr := txManager.WithinTx(ctx, func(ctx context.Context) error {
			record, err := idem.Get(ctx, clientID, requestID)
			if err != nil {
				return domain.ErrInternal(err)
			}
			if record != nil {
				if record.Operation != operation {
					return domain.ErrValidation("requestId", "requestId was already used for a different operation")
				}
				if record.RequestHash != requestHash {
					return domain.ErrValidation("requestId", "requestId was reused with different request parameters")
				}
				var cached T
				if err := json.Unmarshal(record.ResponseBody, &cached); err != nil {
					return domain.ErrInternal(err)
				}
				result = &cached
				return nil
			}

			out, err := fn(ctx)
			if err != nil {
				return err
			}

			body, err := json.Marshal(out)
			if err != nil {
				return domain.ErrInternal(err)
			}
			if err := idem.Save(ctx, &port.IdempotencyRecord{
				ClientID:     clientID,
				RequestID:    requestID,
				Operation:    operation,
				RequestHash:  requestHash,
				ResponseBody: body,
			}); err != nil {
				if errors.Is(err, port.ErrIdempotencyConflict) {
					conflict = true
					return err
				}
				return domain.ErrInternal(err)
			}

			result = out
			return nil
		})

		if conflict {
			lastErr = txErr
			backoff(ctx, attempt)
			continue
		}
		if txErr != nil {
			return nil, txErr
		}
		return result, nil
	}
	if lastErr != nil {
		return nil, domain.ErrInternal(fmt.Errorf("idempotency conflict retry limit exceeded: %w", lastErr))
	}
	return nil, domain.ErrInternal(errors.New("idempotency conflict retry limit exceeded"))
}
