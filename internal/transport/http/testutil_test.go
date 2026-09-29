package http

import (
	"context"
	"net/http"
	"sync"

	"github.com/drzbraz/dice-bet/internal/config"
	"github.com/drzbraz/dice-bet/internal/domain"
	"github.com/drzbraz/dice-bet/internal/port"
	"github.com/drzbraz/dice-bet/internal/service"
)

// Minimal non-transactional in-memory fakes, mirroring
// internal/transport/ws's test fakes, sufficient to exercise HTTP status
// code mapping end to end.

type memStore struct {
	mu          sync.Mutex
	wallets     map[string]domain.Wallet
	openPlays   map[string]domain.Play
	idempotency map[string]port.IdempotencyRecord
}

func newMemStore() *memStore {
	return &memStore{
		wallets:     make(map[string]domain.Wallet),
		openPlays:   make(map[string]domain.Play),
		idempotency: make(map[string]port.IdempotencyRecord),
	}
}

type memTxManager struct{}

func (memTxManager) WithinTx(ctx context.Context, fn func(ctx context.Context) error) error {
	return fn(ctx)
}

type memWalletRepo struct{ store *memStore }

func (r *memWalletRepo) GetForUpdate(ctx context.Context, clientID string) (*domain.Wallet, error) {
	return r.Get(ctx, clientID)
}

func (r *memWalletRepo) Get(ctx context.Context, clientID string) (*domain.Wallet, error) {
	r.store.mu.Lock()
	defer r.store.mu.Unlock()
	w, ok := r.store.wallets[clientID]
	if !ok {
		return nil, nil
	}
	cp := w
	return &cp, nil
}

func (r *memWalletRepo) Update(ctx context.Context, wallet *domain.Wallet) error {
	r.store.mu.Lock()
	defer r.store.mu.Unlock()
	r.store.wallets[wallet.ClientID] = *wallet
	return nil
}

type memPlayRepo struct{ store *memStore }

func (r *memPlayRepo) Create(ctx context.Context, play *domain.Play) error {
	r.store.mu.Lock()
	defer r.store.mu.Unlock()
	if _, exists := r.store.openPlays[play.ClientID]; exists {
		return domain.ErrPlayAlreadyInProgress()
	}
	r.store.openPlays[play.ClientID] = *play
	return nil
}

func (r *memPlayRepo) GetOpenForUpdate(ctx context.Context, clientID string) (*domain.Play, error) {
	r.store.mu.Lock()
	defer r.store.mu.Unlock()
	p, ok := r.store.openPlays[clientID]
	if !ok {
		return nil, nil
	}
	cp := p
	return &cp, nil
}

func (r *memPlayRepo) Close(ctx context.Context, play *domain.Play) error {
	r.store.mu.Lock()
	defer r.store.mu.Unlock()
	if _, ok := r.store.openPlays[play.ClientID]; !ok {
		return domain.ErrNoActivePlay()
	}
	delete(r.store.openPlays, play.ClientID)
	return nil
}

type memTxRepo struct{}

func (memTxRepo) Create(ctx context.Context, tx *domain.WalletTransaction) error { return nil }

type memIdemRepo struct{ store *memStore }

func (r *memIdemRepo) Get(ctx context.Context, clientID, requestID string) (*port.IdempotencyRecord, error) {
	r.store.mu.Lock()
	defer r.store.mu.Unlock()
	rec, ok := r.store.idempotency[clientID+"|"+requestID]
	if !ok {
		return nil, nil
	}
	cp := rec
	return &cp, nil
}

func (r *memIdemRepo) Save(ctx context.Context, record *port.IdempotencyRecord) error {
	r.store.mu.Lock()
	defer r.store.mu.Unlock()
	r.store.idempotency[record.ClientID+"|"+record.RequestID] = *record
	return nil
}

type fixedRoller struct{ value int }

func (r fixedRoller) Roll(ctx context.Context) (int, error) { return r.value, nil }

func newTestRouter(pingErr error) http.Handler {
	store := newMemStore()
	store.wallets["alice"] = domain.Wallet{ClientID: "alice", Balance: 1000, Currency: "EUR"}

	walletSvc := service.NewWalletService(&memWalletRepo{store: store})
	gameSvc := service.NewGameService(
		&memWalletRepo{store: store},
		&memPlayRepo{store: store},
		memTxRepo{},
		&memIdemRepo{store: store},
		fixedRoller{value: 4}, // even
		memTxManager{},
		config.GameConfig{MinBet: 1, MaxBet: 100000},
	)

	controller := NewController(walletSvc, gameSvc)
	return NewRouter(controller, func(ctx context.Context) error { return pingErr })
}
