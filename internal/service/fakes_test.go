package service

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/drzbraz/dice-bet/internal/domain"
	"github.com/drzbraz/dice-bet/internal/infrastructure/cache"
	"github.com/drzbraz/dice-bet/internal/port"
)

// fakeStore is the shared in-memory state behind all fake repositories. It
// supports snapshot/restore so fakeTxManager can simulate real transaction
// rollback semantics for service-layer tests.
type fakeStore struct {
	mu          sync.Mutex
	wallets     map[string]domain.Wallet
	openPlays   map[string]domain.Play
	closedPlays map[string][]domain.Play
	ledger      []domain.WalletTransaction
	idempotency map[string]port.IdempotencyRecord
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		wallets:     make(map[string]domain.Wallet),
		openPlays:   make(map[string]domain.Play),
		closedPlays: make(map[string][]domain.Play),
		idempotency: make(map[string]port.IdempotencyRecord),
	}
}

func (s *fakeStore) snapshot() *fakeStore {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := newFakeStore()
	for k, v := range s.wallets {
		cp.wallets[k] = v
	}
	for k, v := range s.openPlays {
		cp.openPlays[k] = v
	}
	for k, v := range s.closedPlays {
		cp.closedPlays[k] = append([]domain.Play(nil), v...)
	}
	cp.ledger = append([]domain.WalletTransaction(nil), s.ledger...)
	for k, v := range s.idempotency {
		cp.idempotency[k] = v
	}
	return cp
}

func (s *fakeStore) restore(from *fakeStore) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.wallets = from.wallets
	s.openPlays = from.openPlays
	s.closedPlays = from.closedPlays
	s.ledger = from.ledger
	s.idempotency = from.idempotency
}

func idemKey(clientID, requestID string) string { return clientID + "|" + requestID }

// fakeTxManager wraps fn with snapshot/restore so a returned error rolls
// back every mutation the closure made, mirroring a real DB transaction.
type fakeTxManager struct {
	store *fakeStore
}

func (t *fakeTxManager) WithinTx(ctx context.Context, fn func(ctx context.Context) error) error {
	before := t.store.snapshot()
	if err := fn(ctx); err != nil {
		t.store.restore(before)
		return err
	}
	return nil
}

type fakeWalletRepository struct{ store *fakeStore }

func (r *fakeWalletRepository) GetForUpdate(ctx context.Context, clientID string) (*domain.Wallet, error) {
	return r.Get(ctx, clientID)
}

func (r *fakeWalletRepository) Get(ctx context.Context, clientID string) (*domain.Wallet, error) {
	r.store.mu.Lock()
	defer r.store.mu.Unlock()
	w, ok := r.store.wallets[clientID]
	if !ok {
		return nil, nil
	}
	cp := w
	return &cp, nil
}

func (r *fakeWalletRepository) Update(ctx context.Context, wallet *domain.Wallet) error {
	if wallet.Balance < 0 {
		return domain.ErrInsufficientBalance()
	}
	r.store.mu.Lock()
	defer r.store.mu.Unlock()
	r.store.wallets[wallet.ClientID] = *wallet
	return nil
}

func (r *fakeWalletRepository) ListClientIDs(ctx context.Context) ([]string, error) {
	r.store.mu.Lock()
	defer r.store.mu.Unlock()
	ids := make([]string, 0, len(r.store.wallets))
	for id := range r.store.wallets {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids, nil
}

type fakePlayRepository struct{ store *fakeStore }

func (r *fakePlayRepository) Create(ctx context.Context, play *domain.Play) error {
	r.store.mu.Lock()
	defer r.store.mu.Unlock()
	if _, exists := r.store.openPlays[play.ClientID]; exists {
		return domain.ErrPlayAlreadyInProgress()
	}
	r.store.openPlays[play.ClientID] = *play
	return nil
}

func (r *fakePlayRepository) GetOpenForUpdate(ctx context.Context, clientID string) (*domain.Play, error) {
	r.store.mu.Lock()
	defer r.store.mu.Unlock()
	p, ok := r.store.openPlays[clientID]
	if !ok {
		return nil, nil
	}
	cp := p
	return &cp, nil
}

func (r *fakePlayRepository) Close(ctx context.Context, play *domain.Play) error {
	r.store.mu.Lock()
	defer r.store.mu.Unlock()
	if _, ok := r.store.openPlays[play.ClientID]; !ok {
		return domain.ErrNoActivePlay()
	}
	delete(r.store.openPlays, play.ClientID)
	r.store.closedPlays[play.ClientID] = append(r.store.closedPlays[play.ClientID], *play)
	return nil
}

type fakeTransactionRepository struct{ store *fakeStore }

func (r *fakeTransactionRepository) Create(ctx context.Context, tx *domain.WalletTransaction) error {
	r.store.mu.Lock()
	defer r.store.mu.Unlock()
	for _, existing := range r.store.ledger {
		if existing.PlayID == tx.PlayID && existing.Type == tx.Type {
			return domain.ErrInternal(errors.New("duplicate ledger entry"))
		}
	}
	r.store.ledger = append(r.store.ledger, *tx)
	return nil
}

// fakeIdempotencyRepository additionally supports injecting a one-shot
// conflict for a given (clientID, requestID) to exercise the
// idempotency-race retry path. conflictOnce lives outside fakeStore so it
// survives a tx rollback (it models a competing transaction's timing, not
// this transaction's own state).
type fakeIdempotencyRepository struct {
	store        *fakeStore
	mu           sync.Mutex
	conflictOnce map[string]bool
}

func (r *fakeIdempotencyRepository) Get(ctx context.Context, clientID, requestID string) (*port.IdempotencyRecord, error) {
	r.store.mu.Lock()
	defer r.store.mu.Unlock()
	rec, ok := r.store.idempotency[idemKey(clientID, requestID)]
	if !ok {
		return nil, nil
	}
	cp := rec
	return &cp, nil
}

func (r *fakeIdempotencyRepository) Save(ctx context.Context, record *port.IdempotencyRecord) error {
	key := idemKey(record.ClientID, record.RequestID)

	r.mu.Lock()
	if r.conflictOnce != nil && r.conflictOnce[key] {
		delete(r.conflictOnce, key)
		r.mu.Unlock()
		return port.ErrIdempotencyConflict
	}
	r.mu.Unlock()

	r.store.mu.Lock()
	defer r.store.mu.Unlock()
	if _, exists := r.store.idempotency[key]; exists {
		return port.ErrIdempotencyConflict
	}
	r.store.idempotency[key] = *record
	return nil
}

func (r *fakeIdempotencyRepository) simulateConflictOnce(clientID, requestID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.conflictOnce == nil {
		r.conflictOnce = make(map[string]bool)
	}
	r.conflictOnce[idemKey(clientID, requestID)] = true
}

// fakeDiceRoller returns a fixed, pre-programmed sequence of rolls.
type fakeDiceRoller struct {
	rolls []int
	i     int
}

func (r *fakeDiceRoller) Roll(ctx context.Context) (int, error) {
	if r.i >= len(r.rolls) {
		return 0, errors.New("fake dice roller exhausted")
	}
	v := r.rolls[r.i]
	r.i++
	return v, nil
}

// fakeFailingRepository-style helper: a wallet repository that always
// errors, to test that generic repository failures propagate as
// INTERNAL_ERROR.
type failingWalletRepository struct {
	err error
}

func (r *failingWalletRepository) GetForUpdate(ctx context.Context, clientID string) (*domain.Wallet, error) {
	return nil, r.err
}
func (r *failingWalletRepository) Get(ctx context.Context, clientID string) (*domain.Wallet, error) {
	return nil, r.err
}
func (r *failingWalletRepository) Update(ctx context.Context, wallet *domain.Wallet) error {
	return r.err
}
func (r *failingWalletRepository) ListClientIDs(ctx context.Context) ([]string, error) {
	return nil, r.err
}

// erroringIdempotencyRepository always fails, to test that idempotency
// repository errors propagate as INTERNAL_ERROR.
type erroringIdempotencyRepository struct{ err error }

func (r *erroringIdempotencyRepository) Get(ctx context.Context, clientID, requestID string) (*port.IdempotencyRecord, error) {
	return nil, r.err
}
func (r *erroringIdempotencyRepository) Save(ctx context.Context, record *port.IdempotencyRecord) error {
	return r.err
}

// updateFailingWalletRepository wraps a fakeWalletRepository but always
// fails Update, to test rollback/error propagation for repository failures
// that occur after successful reads.
type updateFailingWalletRepository struct {
	*fakeWalletRepository
	err error
}

func (r *updateFailingWalletRepository) Update(ctx context.Context, wallet *domain.Wallet) error {
	return r.err
}

// closeFailingPlayRepository wraps a fakePlayRepository but always fails
// Close.
type closeFailingPlayRepository struct {
	*fakePlayRepository
	err error
}

func (r *closeFailingPlayRepository) Close(ctx context.Context, play *domain.Play) error {
	return r.err
}

// testHarness bundles a full set of fakes wired to a GameService and
// WalletService for use across test cases.
type testHarness struct {
	store       *fakeStore
	wallets     *fakeWalletRepository
	plays       *fakePlayRepository
	txs         *fakeTransactionRepository
	idempotency *fakeIdempotencyRepository
	roller      *fakeDiceRoller
	txManager   *fakeTxManager
	cache       *cache.MemoryCache
	game        *GameService
	wallet      *WalletService
}

func newTestHarness(minBet, maxBet int64, rolls ...int) *testHarness {
	store := newFakeStore()
	h := &testHarness{
		store:       store,
		wallets:     &fakeWalletRepository{store: store},
		plays:       &fakePlayRepository{store: store},
		txs:         &fakeTransactionRepository{store: store},
		idempotency: &fakeIdempotencyRepository{store: store},
		roller:      &fakeDiceRoller{rolls: rolls},
		txManager:   &fakeTxManager{store: store},
		cache:       cache.NewMemoryCache(time.Minute),
	}
	h.game = NewGameService(h.wallets, h.plays, h.txs, h.idempotency, h.roller, h.txManager, h.cache, gameConfigFor(minBet, maxBet))
	h.game.now = fixedClock
	h.game.idemBackoff = func(context.Context, int) {}
	h.wallet = NewWalletService(h.wallets, h.cache)
	return h
}

//nolint:unparam // clientID is kept for reuse by future multi-client tests; every current test happens to use "alice".
func (h *testHarness) seedWallet(clientID string, balance int64) {
	h.store.mu.Lock()
	defer h.store.mu.Unlock()
	h.store.wallets[clientID] = domain.Wallet{ClientID: clientID, Balance: balance, Currency: "EUR"}
}
