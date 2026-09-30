package ws

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sort"
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

func (r *memWalletRepo) ListClientIDs(ctx context.Context) ([]string, error) {
	r.store.mu.Lock()
	defer r.store.mu.Unlock()
	ids := make([]string, 0, len(r.store.wallets))
	for id := range r.store.wallets {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids, nil
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

// fakeFairnessRepository is a minimal in-memory port.FairnessRepository
// for the fairness/* transport tests -- see newTestServerWithFairness.
type fakeFairnessRepository struct {
	mu      sync.Mutex
	active  map[string]domain.FairnessSeed
	retired map[string][]domain.FairnessSeed
}

func newFakeFairnessRepository() *fakeFairnessRepository {
	return &fakeFairnessRepository{active: map[string]domain.FairnessSeed{}, retired: map[string][]domain.FairnessSeed{}}
}

func (r *fakeFairnessRepository) GetActiveForUpdate(ctx context.Context, clientID string) (*domain.FairnessSeed, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.active[clientID]
	if !ok {
		return nil, nil
	}
	cp := s
	return &cp, nil
}

func (r *fakeFairnessRepository) Create(ctx context.Context, seed *domain.FairnessSeed) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.active[seed.ClientID] = *seed
	return nil
}

func (r *fakeFairnessRepository) IncrementNonce(ctx context.Context, seed *domain.FairnessSeed) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.active[seed.ClientID]
	s.Nonce = seed.Nonce
	r.active[seed.ClientID] = s
	return nil
}

func (r *fakeFairnessRepository) Retire(ctx context.Context, clientID string) (*domain.FairnessSeed, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.active[clientID]
	if !ok {
		return nil, nil
	}
	now := time.Now()
	s.RetiredAt = &now
	delete(r.active, clientID)
	r.retired[clientID] = append([]domain.FairnessSeed{s}, r.retired[clientID]...)
	cp := s
	return &cp, nil
}

func (r *fakeFairnessRepository) ListRetired(ctx context.Context, clientID string) ([]domain.FairnessSeed, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]domain.FairnessSeed(nil), r.retired[clientID]...), nil
}

type fakeSeedGenerator struct{ n int }

func (g *fakeSeedGenerator) GenerateServerSeed() (string, error) {
	g.n++
	return fmt.Sprintf("fake-server-seed-%d", g.n), nil
}

func (g *fakeSeedGenerator) GenerateClientSeed() (string, error) {
	g.n++
	return fmt.Sprintf("fake-client-seed-%d", g.n), nil
}

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
	return newTestServerWith(t, cfg, nil)
}

// newTestServerWithFairness is newTestServer with PROVABLY_FAIR_ENABLED
// effectively on, for the seed.get/seed.rotate/seed.history tests. Kept
// separate from newTestServer so the existing fixedRoller-based suite
// keeps its deterministic (always-even) rolls: enabling fairness there
// would silently switch those rolls over to the HMAC-derived ones instead,
// breaking unrelated assertions on rolledNumber/result.
func newTestServerWithFairness(t *testing.T, cfg Config) (*testServer, *service.FairnessService) {
	t.Helper()
	fairnessSvc := service.NewFairnessService(newFakeFairnessRepository(), &fakeSeedGenerator{}, memTxManager{})
	return newTestServerWith(t, cfg, fairnessSvc), fairnessSvc
}

func newTestServerWith(t *testing.T, cfg Config, fairnessSvc *service.FairnessService) *testServer {
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
		fairnessSvc,
		config.GameConfig{MinBet: 1, MaxBet: 100000},
	)

	controller := NewController(walletSvc, gameSvc, fairnessSvc)
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
