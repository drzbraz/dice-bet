package ws

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/drzbraz/dice-bet/internal/config"
	"github.com/drzbraz/dice-bet/internal/domain"
	"github.com/drzbraz/dice-bet/internal/infrastructure/cache"
	"github.com/drzbraz/dice-bet/internal/port"
	"github.com/drzbraz/dice-bet/internal/service"
)

// The fakes below are a minimal, non-transactional in-memory backing store
// sufficient to exercise the WebSocket transport end to end. Transactional
// rollback semantics are already covered by internal/service's own fakes;
// these transport tests only need a working service to dispatch against.

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

// fixedRoller always returns the same, pre-programmed roll.
type fixedRoller struct{ value int }

func (r fixedRoller) Roll(ctx context.Context) (int, error) { return r.value, nil }

// testServer bundles an httptest.Server exposing /ws and the underlying
// store, so tests can seed data and assert on it after exchanging messages.
type testServer struct {
	*httptest.Server
	store    *memStore
	wsServer *Server
}

// wsURL returns the ws:// URL for the /ws endpoint.
func (s *testServer) wsURL() string {
	return "ws" + strings.TrimPrefix(s.URL, "http") + "/ws"
}

func newTestServer(t *testing.T, cfg Config) *testServer {
	t.Helper()
	store := newMemStore()
	store.wallets["alice"] = domain.Wallet{ClientID: "alice", Balance: 1000, Currency: "EUR"}

	walletCache := cache.NewMemoryCache(time.Minute)
	walletSvc := service.NewWalletService(&memWalletRepo{store: store}, walletCache)
	gameSvc := service.NewGameService(
		&memWalletRepo{store: store},
		&memPlayRepo{store: store},
		memTxRepo{},
		&memIdemRepo{store: store},
		fixedRoller{value: 4}, // even
		memTxManager{},
		walletCache,
		config.GameConfig{MinBet: 1, MaxBet: 100000},
	)

	controller := NewController(walletSvc, gameSvc)
	router := NewRouter()
	controller.RegisterRoutes(router)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	wsServer := NewServer(router, cfg, logger)

	mux := http.NewServeMux()
	mux.Handle("/ws", wsServer)
	httpServer := httptest.NewServer(mux)
	t.Cleanup(httpServer.Close)

	return &testServer{Server: httpServer, store: store, wsServer: wsServer}
}
