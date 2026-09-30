# Dice Bet

A production-quality Go backend for a dice game where a player bets whether the next roll is `EVEN` or `ODD`. Built for a technical assessment; graded on architecture, concurrency correctness, error handling, and tests rather than feature count.

**Contents**: [Overview](#overview) · [Architecture](#architecture) · [Read freshness under replica lag](#read-freshness-under-replica-lag-wallet-balance-cache) · [Provably fair rolls](#provably-fair-rolls) · [Domain state machine](#domain-state-machine-play) · [WebSocket contract](#websocket-contract) · [HTTP mirror](#http-mirror-for-postman) · [Prerequisites](#prerequisites) · [How to run](#how-to-run) · [Frontend](#frontend) · [How to add a new client](#how-to-add-a-new-client) · [How to test](#how-to-test) · [Assumptions and trade-offs](#assumptions-and-trade-offs) · [What would change for production](#what-would-change-for-production) · [Configuration](#configuration)

![Dice Bet frontend: a connected player's wallet balance, dice, bet controls, and recent rounds](docs/dice-bet.png)

## Overview

- A die (1–6) is rolled server-side using `crypto/rand` behind an injectable `DiceRoller` interface.
- **Win**: the player receives 2x the bet (stake back + equal profit). **Lose**: the bet is forfeited.
- Three operations, exposed over both **WebSocket** (primary contract) and **REST** (for Postman):
  - **Wallet**: `clientId` → current balance.
  - **Play**: `clientId, betAmount, betType` → rolls the die, debits the bet immediately, stores an `OPEN` play with its outcome already computed, returns the roll/result/pending payout.
  - **EndPlay**: `clientId` → credits the pending payout (0 on a loss), closes the play, returns the credited amount and new balance.
- Money is always `int64` minor units (cents) in Go and `BIGINT` in Postgres — never floats.
- Every mutating request carries a client-generated `requestId` (WebSocket) / `Idempotency-Key` header (HTTP): replaying the same one returns the original response instead of re-executing.
- Concurrency safety (same client, multiple simultaneous requests, even across multiple server instances) is enforced with `SELECT ... FOR UPDATE` row locks inside a single DB transaction per operation, backed by database constraints as the last line of defense.

## Architecture

Clean Architecture: dependencies point inward only (`transport → service → domain`). The domain package imports nothing from any other layer. Services depend on `port` interfaces; concrete Postgres/gorilla types are wired only in `cmd/server/main.go`.

```mermaid
flowchart LR
    subgraph Transport
        WS[WebSocket<br/>/ws]
        HTTP[REST<br/>/api/v1/*]
    end
    subgraph Service["Service (use cases)"]
        WalletSvc[WalletService]
        GameSvc[GameService]
    end
    subgraph Domain
        Wallet[Wallet]
        Play[Play]
        Dice[Dice rules]
    end
    subgraph Ports["Port interfaces"]
        Repo[WalletRepository<br/>PlayRepository<br/>TransactionRepository<br/>IdempotencyRepository]
        Tx[TxManager]
        Roller[DiceRoller]
    end
    subgraph Infra["Infrastructure (cmd/server wires these)"]
        PG[(Postgres<br/>pgx/v5)]
        Crypto[crypto/rand]
    end

    WS --> WalletSvc
    WS --> GameSvc
    HTTP --> WalletSvc
    HTTP --> GameSvc
    WalletSvc --> Wallet
    GameSvc --> Wallet
    GameSvc --> Play
    GameSvc --> Dice
    WalletSvc -.depends on.-> Repo
    GameSvc -.depends on.-> Repo
    GameSvc -.depends on.-> Tx
    GameSvc -.depends on.-> Roller
    Repo -.implemented by.-> PG
    Tx -.implemented by.-> PG
    Roller -.implemented by.-> Crypto
```

### Entity-relationship diagram

```mermaid
erDiagram
    CLIENTS ||--|| WALLETS : has
    CLIENTS ||--o{ PLAYS : places
    PLAYS ||--o{ WALLET_TRANSACTIONS : ledgers
    CLIENTS ||--o{ WALLET_TRANSACTIONS : owns
    CLIENTS ||--o{ IDEMPOTENCY_KEYS : "sent requests"

    CLIENTS {
        text id PK
        timestamptz created_at
    }
    WALLETS {
        text client_id PK
        bigint balance
        char3 currency
        bigint version
        timestamptz updated_at
    }
    PLAYS {
        uuid id PK
        text client_id FK
        bigint bet_amount
        text bet_type
        smallint rolled_number
        text result
        bigint payout
        text status
        timestamptz created_at
        timestamptz closed_at
    }
    WALLET_TRANSACTIONS {
        uuid id PK
        text client_id FK
        uuid play_id FK
        text type
        bigint amount
        bigint balance_after
        timestamptz created_at
    }
    IDEMPOTENCY_KEYS {
        text client_id PK
        text request_id PK
        text operation
        text request_hash
        jsonb response_body
        timestamptz created_at
    }
```

Key invariants, enforced by the schema itself (last line of defense, mirrored by service-layer checks first):
- `uq_plays_one_open_per_client`: a **partial unique index** on `plays(client_id) WHERE status = 'OPEN'` — at most one OPEN play per client, database-guaranteed.
- `CHECK (balance >= 0)` on `wallets` — a debit can never take a balance negative.
- `UNIQUE (play_id, type)` on `wallet_transactions` — a play can never be debited or credited twice.
- `PRIMARY KEY (client_id, request_id)` on `idempotency_keys` — a repeated `requestId` can only be inserted once; the loser of a race is safely retried (see "Idempotency race" under [Assumptions and trade-offs](#assumptions-and-trade-offs)).

Two additions beyond the given schema, both justified here:
- A plain (non-unique) `idx_plays_client_id` index on `plays(client_id)`, alongside the partial unique index — the partial index only covers `WHERE status = 'OPEN'` rows, so a client's historical `CLOSED` plays would otherwise require a full table scan to look up.
- A `request_hash` column on `idempotency_keys` (migration `000003`), fingerprinting the request's actual parameters. Without it, replaying a `requestId` with *different* parameters than the original request (e.g. a retried `play.start` with a corrected `betAmount`) would silently return the stale first response instead of being rejected. A mismatch now returns `VALIDATION_ERROR`.

### Folder layout

```
cmd/
  server/        composition root: config, pool, migrations, wiring, graceful shutdown
  migrate/       standalone CLI for `make migrate-up` / `make migrate-down`
internal/
  config/        env-var configuration with defaults + validation
  domain/        entities, dice rules, typed errors — zero external imports
  port/          interfaces the service layer depends on (repos, TxManager, DiceRoller, WalletBalanceCache, FairnessRepository, SeedGenerator)
  service/       Wallet/Play/EndPlay/Fairness use cases, orchestrating ports inside one transaction
  repository/postgres/  pgx/v5 implementations of the port interfaces, plain SQL
  infrastructure/random/ crypto/rand DiceRoller + SeedGenerator implementations
  infrastructure/cache/  in-memory WalletBalanceCache implementation (see "Read freshness under replica lag")
  transport/ws/   WebSocket upgrade, connection lifecycle, message router, controller
  transport/http/ REST mirror for Postman, same services/DTOs
  dto/            wire-format request/response shapes + structural validation
migrations/       embedded SQL migrations (golang-migrate)
test/e2e/         full-stack tests against a real Postgres (testcontainers): happy path + concurrency proofs
postman/          Postman collection
frontend/         React/TanStack Start client (originally scaffolded with Lovable, now a normal
                   part of this monorepo — see "Frontend" below)
```

### SOLID in this codebase

- **S**: `WalletService`/`GameService` each own one use-case group; `Controller`s only translate DTO ⇄ service calls; repositories only persist; `mapError` only maps errors.
- **O**: a new WebSocket message type is added by registering a handler on the `Router` — no existing handler changes.
- **L**: the Postgres repositories and the in-memory test fakes both satisfy the same `port` interfaces and are interchangeable in `GameService`.
- **I**: `WalletRepository`, `PlayRepository`, `TransactionRepository`, `IdempotencyRepository` are separate small interfaces; `DiceRoller` has exactly one method.
- **D**: `WalletService`/`GameService` depend on `port` interfaces, never on `pgx` or `gorilla` types directly; only `cmd/server/main.go` constructs concrete implementations.

## Read freshness under replica lag (wallet balance cache)

Scaling read throughput usually means adding one or more read replicas and routing reads to them, keeping only writes on the primary. That introduces a well-known bug: replicas apply writes asynchronously, so a read routed to a replica immediately after a write can be served before that replica has caught up — the read silently misses its own write. `internal/port/cache.go` (`WalletBalanceCache`) exists specifically to demonstrate the fix for that, correctly scoped:

- **`WalletService.GetBalance` reads cache-aside.** A hit is served without touching the database at all — this is exactly the read that would otherwise be routed to a (possibly lagging) replica at scale. A miss falls through to the database and populates the cache for next time.
- **`GameService.Play`/`EndPlay` write through, but only *after* their transaction commits.** The obvious-looking approach — populate the cache from inside the transaction, right after `wallets.Update(...)` — is a real bug waiting to happen: if a *later* step in the same transaction fails (e.g. the ledger insert), the transaction rolls back, but a cache write from earlier in that transaction wouldn't roll back with it. The cache would end up holding a balance the database never actually had — the identical staleness bug, just relocated from the replica to the cache. So `refreshCache` runs strictly after `runIdempotent`/`WithinTx` returns successfully, re-reading the now-guaranteed-fresh row and writing that through. `TestGameService_Play_RollbackDoesNotPopulateCacheWithUncommittedValue` pins this down directly, and the concurrency e2e tests assert the cache agrees with the database after 20 real goroutines race a real Postgres instance.
- **The locked path never goes anywhere near the cache.** `GetForUpdate` (used by the money-moving code, inside the transaction) always reads the real row under `SELECT ... FOR UPDATE`; the cache is never consulted or trusted there. Caching is only ever applied to the read that doesn't need strict consistency.

What this demo deliberately does **not** do, and why that's fine here but wouldn't be in production:

- **No actual read replica.** There's a single Postgres instance, so a cache miss always falls through to the one source of truth — there's no lagging replica to protect against yet. The pattern is implemented correctly and would immediately start doing real work the moment reads were split across a primary and a replica.
- **Process-local cache (`internal/infrastructure/cache`), not shared.** With more than one server instance, each would have its own cache; an instance that didn't handle a given write wouldn't see its write-through population, and could still serve a stale value from before its own last refresh. A real multi-instance deployment needs a shared cache (Redis, most commonly) behind the same `WalletBalanceCache` interface — the service layer wouldn't change at all.

A worthwhile alternative/complement, for the record: instead of (or alongside) caching, pin a session's own reads to the primary for a short window right after it writes (or use an LSN/version token to detect "has this replica caught up to my last write yet"). That guarantees freshness without a cache at all, at the cost of extra load on the primary for recent writers specifically. Caching wins when reads are heavily skewed toward a small hot set of keys (exactly wallet balances, here); primary-pinning wins when writes and reads are more evenly spread out. Which one's right depends on the actual traffic shape, not on picking a pattern in the abstract.

## Provably fair rolls

The core trust problem in any online dice game: the player has to take the server's word for it that a roll wasn't picked to favor the house after the bet was already known. `PROVABLY_FAIR_ENABLED` (default `true`) switches roll derivation from plain `crypto/rand` to a commit-reveal scheme that removes that trust requirement entirely — fully additively (see "Fully additive" below).

**The scheme.** Each client has a sequence of seed "epochs" (`fairness_seeds`, one row active at a time, enforced the same way as one-open-play-per-client — a partial unique index):

1. A fresh epoch starts with a secret `serverSeed` (32 random bytes, hex-encoded) and a `clientSeed` (16 random bytes by default, or a player-supplied string). The server publishes only `serverSeedHash = SHA256(serverSeed)` — the *commitment* — via `seed.get`, before any bet happens.
2. Every `play.start` while that epoch is active derives its roll as `HMAC-SHA256(serverSeed, "clientSeed:nonce")`, taking the first 4 bytes as a big-endian `uint32` reduced mod 6 (`internal/domain/fairness.go` `ComputeRoll`), where `nonce` starts at 0 and increments once per roll. The response includes `nonce` and `serverSeedHash` (but never the still-secret `serverSeed`) under a `fairness` key.
3. `seed.rotate` retires the active epoch — **revealing `serverSeed`** — and immediately activates a new one. Once revealed, anyone can recompute `SHA256(serverSeed)` and confirm it matches the hash published in step 1, then recompute `ComputeRoll(serverSeed, clientSeed, nonce)` for any past round and confirm it matches the roll they were shown. `seed.history` lists every retired epoch so this works for old rounds too, not just the one just rotated.

**Why this actually proves something, not just obscures it.** SHA-256 is preimage- and collision-resistant: publishing the hash before the bet cryptographically commits the server to that exact seed, without revealing it. If the server tried to swap in a different seed after seeing the bet, the swapped seed's hash wouldn't match what was already published — a player would catch that instantly on verification. This is a guarantee about *not adapting the outcome after the fact*, not a claim that the roll is "more random" than `crypto/rand` — it's a different property entirely (verifiable non-manipulation vs. raw entropy quality), and it's the property that actually matters for player trust.

**Frontend verification is real, not decorative.** `frontend/src/lib/verify-fairness.ts` reimplements `ComputeRoll` independently using the browser's native Web Crypto API (`crypto.subtle`) — no network call back to this server. The "Provably fair" panel's "Verify" button runs entirely client-side: it takes a revealed `serverSeed` plus a round's `clientSeed`/`nonce`, recomputes the roll in the browser, and compares it to what the server showed at play time. The two implementations were cross-checked against the same fixed input vectors (`TestComputeRoll_GoldenVectors` in Go, manually verified byte-for-byte identical in Node's Web Crypto) specifically so a future refactor of either side can't silently drift apart without a test catching it.

**Fully additive — the flag genuinely changes nothing when off.**

- `GameService`'s existing `port.DiceRoller` field and constructor signature are untouched; a new, *optional* `*FairnessService` dependency is threaded through instead (`nil` when the flag is off). `Play` branches on it: `nil` → the exact `s.roller.Roll(ctx)` call that existed before this feature, byte-for-byte; non-nil → `FairnessService.RollFor`, which locks the client's active seed row (`GetActiveForUpdate`, the same `SELECT ... FOR UPDATE` pattern as wallets) inside the *same* transaction as the wallet debit, so the nonce incrementing and the money movement commit or roll back together atomically.
- The migration (`000004_fairness_seeds`) only adds a new table — it does not alter `wallets`, `plays`, or any existing schema, so there's zero risk to data or constraints already in place.
- `seed.get`/`seed.rotate`/`seed.history` are new message types/routes; no existing WebSocket message type or HTTP endpoint changed shape except `play.start`'s response gaining an optional `fairness` object (omitted entirely, not just null, when the flag is off).
- Every existing test in the repo constructs `GameService`/`Controller` with `fairness: nil` and is unmodified in behavior — proving the "off" path really is identical to pre-feature behavior, not just claimed to be. The feature's own tests are additive: `internal/service/fairness_service_test.go`, the `TestGameService_Play_Fairness*` cases, `TestHTTP_*Seed*`, `TestWS_Seed*`/`TestWS_PlayStart_WithFairnessEnabled_*`, and two new Postgres/e2e tests (`test/e2e/fairness_test.go`) — one proving the full real-stack round trip (play, rotate, independently re-verify against real Postgres), one proving under a real 20-goroutine race that the nonce advances by exactly one per successful play, never more.

**Scope note.** `clientSeed` defaults to server-generated but accepts a player-supplied override on `seed.rotate` — letting the player contribute entropy the server couldn't have optimized around when it originally committed to the hash. There's no scheduled auto-rotation (a player/client decides when to rotate); a production deployment might rotate automatically after N rounds or T time, on the same `FairnessRepository` interface, with no service-layer change.

## Domain state machine (Play)

```
        Play (new)
            │
            ▼
        ┌───────┐   EndPlay    ┌────────┐
        │ OPEN  │─────────────▶│ CLOSED │
        └───────┘              └────────┘
            ▲
            │ rejected while OPEN exists: PLAY_ALREADY_IN_PROGRESS
```

- A client has at most 0 or 1 `OPEN` play at any time (DB-enforced).
- `Play` creates a play directly in `OPEN` — the roll, result and payout are already computed; the payout is *pending* until `EndPlay`.
- `EndPlay` transitions `OPEN → CLOSED`, crediting the pending payout exactly once. `CLOSED` is terminal; there is no other transition.

## WebSocket contract

Endpoint: `ws://localhost:8080/ws`. JSON text frames.

**Request envelope**
```json
{ "type": "wallet.get | play.start | play.end | seed.get | seed.rotate | seed.history", "requestId": "uuid-v4", "payload": { } }
```

**Success response**
```json
{ "type": "wallet.get.result | play.start.result | play.end.result | seed.get.result | seed.rotate.result | seed.history.result", "requestId": "same uuid as request", "success": true, "data": { }, "timestamp": "RFC3339" }
```

**Error response**
```json
{ "type": "error", "requestId": "uuid or null if the request could not be parsed", "success": false, "error": { "code": "INSUFFICIENT_BALANCE", "message": "human readable message", "details": { "field": "betAmount" } }, "timestamp": "RFC3339" }
```

### Payloads

| Type | Request payload | Response data |
|---|---|---|
| `wallet.get` | `{ "clientId" }` | `{ "clientId", "balance", "currency" }` |
| `play.start` | `{ "clientId", "betAmount", "betType" }` | `{ "playId", "clientId", "betAmount", "betType", "rolledNumber", "result", "payout", "status", "balance", "fairness"? }` — `fairness: { "serverSeedHash", "clientSeed", "nonce" }` present only when [provably fair](#provably-fair-rolls) is on |
| `play.end` | `{ "clientId" }` | `{ "playId", "clientId", "result", "creditedAmount", "status", "balance" }` |
| `seed.get` | `{ "clientId" }` | `{ "clientId", "serverSeedHash", "clientSeed", "nonce" }` |
| `seed.rotate` | `{ "clientId", "clientSeed"? }` | `{ "retired"?: { "serverSeed", "serverSeedHash", "clientSeed", "finalNonce", "retiredAt" }, "active": { same shape as seed.get } }` |
| `seed.history` | `{ "clientId" }` | `{ "seeds": [ same shape as seed.rotate's "retired", newest first ] }` |

### Error codes (single source of truth: `internal/domain/errors.go`)

| Code | WS | HTTP |
|---|---|---|
| `INVALID_MESSAGE` (malformed JSON / missing envelope fields) | error frame | 400 |
| `UNKNOWN_MESSAGE_TYPE` | error frame | 404 |
| `VALIDATION_ERROR` (missing/invalid payload fields) | error frame | 400 |
| `INVALID_BET_AMOUNT` | error frame | 422 |
| `INVALID_BET_TYPE` | error frame | 422 |
| `CLIENT_NOT_FOUND` | error frame | 404 |
| `INSUFFICIENT_BALANCE` | error frame | 422 |
| `PLAY_ALREADY_IN_PROGRESS` | error frame | 409 |
| `NO_ACTIVE_PLAY` | error frame | 409 |
| `SERVICE_UNAVAILABLE` | error frame | 503 |
| `INTERNAL_ERROR` (never leaks SQL/internal detail) | error frame | 500 |
| `FAIRNESS_DISABLED` (a `seed.*` call while `PROVABLY_FAIR_ENABLED=false`) | error frame | 404 |

## HTTP mirror (for Postman)

Same use cases, same services/DTOs — this also demonstrates the business layer is transport-agnostic.

- `GET  /api/v1/clients` — `{ "clients": ["alice", "bob", ...] }`, every known client ID, alphabetically. Not part of the WebSocket contract; added purely to populate the frontend's player picker (see "Frontend" below). There is no client-creation endpoint anywhere in this project — see [How to add a new client](#how-to-add-a-new-client).
- `GET  /api/v1/clients/{clientId}/wallet`
- `POST /api/v1/plays` — body `{ "clientId", "betAmount", "betType" }`, header `Idempotency-Key` (required)
- `POST /api/v1/plays/end` — body `{ "clientId" }`, header `Idempotency-Key` (required)
- `GET  /api/v1/clients/{clientId}/fairness/seed` — current commitment, see [Provably fair rolls](#provably-fair-rolls)
- `POST /api/v1/clients/{clientId}/fairness/rotate` — body `{ "clientSeed"? }` (optional), reveals the retiring seed. Deliberately **not** gated by `Idempotency-Key`: unlike `/plays`, a rotate is meant to always produce a genuinely new seed, never replay a cached one.
- `GET  /api/v1/clients/{clientId}/fairness/history` — retired seeds, newest first
- `GET  /health` — checks database connectivity via `pool.Ping`

The three `fairness/*` endpoints return `404 FAIRNESS_DISABLED` when `PROVABLY_FAIR_ENABLED=false`.

This mirror sends a permissive `Access-Control-Allow-Origin: *` (see `withCORS` in `internal/transport/http/router.go`), since the frontend fetches `GET /api/v1/clients` directly from its own origin. Safe here because the mirror is already auth-less by design (an explicit assessment-scope simplification, not specific to this endpoint — see "Assumptions and trade-offs"); a production deployment would restrict it to the frontend's actual origin(s).

## Prerequisites

- **Docker + Docker Compose** — the only requirement for the "with Docker" path below; it builds and runs everything, no local Go toolchain needed.
- **Go 1.25+** — only if running the server locally against a Postgres you already have.
- **A local Docker daemon** — only for `make test-integration` (testcontainers spins up a real, disposable Postgres).
- **Node 18+ / npm** — only for the optional `frontend/`.
- **`golangci-lint`** — only for `make lint` (`go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest`).

## How to run

### With Docker (recommended)

```sh
docker compose up --build
```

This starts Postgres, applies migrations, seeds a few clients, and starts the app on `:8080`. Postgres is also published on host port `5433` (not `5432`, to avoid clashing with a locally installed Postgres) if you want to connect with `psql` directly.

Seeded clients (see `migrations/000002_seed_clients.up.sql`): `alice` (1,000.00 EUR), `bob` (500.00 EUR), `carol` (250.00 EUR), `dave` (0.00 EUR — handy for exercising `INSUFFICIENT_BALANCE`).

### Locally against a Postgres you already have

```sh
cp .env.example .env   # adjust DATABASE_URL etc.
make run                # loads .env, applies migrations (RUN_MIGRATIONS=true by default), starts the server
```

## Frontend

`frontend/` is a friendly React/TanStack Start client for this backend (originally scaffolded with [Lovable](https://lovable.dev), now a normal part of this repo — see `frontend/README.md`/`frontend/AGENTS.md` for what it still carries over from that). Gameplay (wallet lookups, `play.start`, `play.end`) goes purely over the WebSocket contract above; the "Player" field is the one exception, a `<select>` populated by fetching `GET /api/v1/clients` from the HTTP mirror when the page loads or the "Game server" field changes (`frontend/src/lib/dice-client.ts`'s `fetchClients`).

```sh
cd frontend
npm i
npm run dev   # http://localhost:5173
```

The dev server defaults to `:5173` specifically so it doesn't collide with the backend's own default `:8080` (see `frontend/vite.config.ts`) — both can run side by side locally. The UI's "Game server" field lets you point it at any backend URL at runtime; its build-time default is `ws://localhost:8080/ws`, overridable via `VITE_DICE_SERVER_URL` (see `frontend/.env.example`) for pointing a deployed build at a deployed backend.

When the backend has [provably fair](#provably-fair-rolls) on, a "Provably fair" panel appears after connecting: the current commitment (`seed.get`), a "Reveal seed & start a new one" button (`seed.rotate`, with an optional field to supply your own client seed), and — once a seed is revealed — a "Verify" button per round. That button calls `frontend/src/lib/verify-fairness.ts`, which recomputes the roll with the browser's own Web Crypto API; it never asks this server to confirm its own answer. If the backend has the feature off, `seed.get` fails with `FAIRNESS_DISABLED` and the panel simply doesn't render — no separate frontend flag to keep in sync.

### How to add a new client

There is no "create client" API, on either transport, anywhere in this project — the player picker only ever shows whoever already exists. To add one:

- **Preferred**: add a migration, following the pattern in `migrations/000002_seed_clients.up.sql` (insert into both `clients` and `wallets` — see the [ER diagram](#entity-relationship-diagram) for why both rows are needed).
- **Quick/local**: insert directly against the database, e.g. `psql postgres://dicebet:dicebet@localhost:5433/dicebet -c "INSERT INTO clients (id) VALUES ('erin'); INSERT INTO wallets (client_id, balance, currency) VALUES ('erin', 10000, 'EUR');"` (host port `5433`, per "How to run" above).

Either way, the new client shows up in the picker on the next page load / "Game server" edit — no restart needed, since `GET /api/v1/clients` reads the table live.

## How to test

```sh
make test-unit         # go test -short -race ./...        — no Docker required
make test-integration   # go test -race ./internal/repository/postgres/... ./test/e2e/...  — requires a local Docker daemon (testcontainers)
make test               # everything, with -race
make cover               # coverage.html
make lint                # golangci-lint run
```

- **Domain** (`internal/domain`): `IsWin` for all 6 faces × both bet types, payout math, `Wallet.Debit`/`Credit` invariants including int64 overflow, `ComputeRoll` against fixed golden vectors (the same values the frontend's independent Web Crypto reimplementation was checked against) — ~98% coverage.
- **Service** (`internal/service`, in-memory fakes + a fake `TxManager` that snapshots/restores state to simulate real rollback): every protection rule, win/loss happy paths, `EndPlay` credits 0 on a loss, calling `EndPlay` twice never double-credits, idempotent replay returns the identical response with zero side effects, the idempotency-conflict retry path, repository failures propagating as `INTERNAL_ERROR`, rollback on a mid-transaction failure, and — for provably fair — seed creation/rotation/history, the revealed seed re-hashing to its originally published commitment, nonce sequencing across plays, and `Play` with fairness disabled being byte-for-byte the pre-feature behavior — 86% coverage.
- **Repository integration** (`internal/repository/postgres`, testcontainers): CRUD, `FOR UPDATE` row-lock blocking behavior for both wallets and fairness seeds (each proven by racing two transactions), the partial unique index rejecting a second OPEN play (and, separately, a second active seed), the balance check constraint, ledger uniqueness, Postgres error → domain error mapping.
- **Concurrency** (`test/e2e`, real Postgres, `-race`): 20 goroutines firing `Play` for the same client — exactly one succeeds, the other 19 get `PLAY_ALREADY_IN_PROGRESS`, and the balance/ledger reflect exactly one debit. Same pattern for `EndPlay` (credited exactly once). A parallel fairness-specific version of this same race additionally asserts the seed's nonce in real Postgres advances by exactly one, never more — proving no phantom increment survives a losing, rolled-back attempt. A separate happy-path e2e test plays twice, rotates, and independently re-derives both rolls from the now-revealed seed via `domain.ComputeRoll`, with no shortcuts back into the service under test.
- **Transport**: WebSocket integration tests via `httptest.Server` + a real `gorilla/websocket` client (full flow, malformed JSON, unknown type, oversized message, connection stays open after a recoverable error, graceful `Shutdown`, the full `seed.get`/`seed.rotate`/`seed.history` flow). HTTP handler tests via `httptest.ResponseRecorder` covering every status code mapping, `GET /api/v1/clients`, the CORS preflight/header behavior described above, and the `fairness/*` endpoints including the `404 FAIRNESS_DISABLED` path when the feature is off.

### Testing the WebSocket endpoint manually

**In Postman**: `New → WebSocket Request`, URL `ws://localhost:8080/ws`, then send these as the message body (Postman's WebSocket tab lets you paste raw text):

```json
{"type":"wallet.get","requestId":"11111111-1111-4111-8111-111111111111","payload":{"clientId":"alice"}}
```
```json
{"type":"play.start","requestId":"22222222-2222-4222-8222-222222222222","payload":{"clientId":"alice","betAmount":500,"betType":"EVEN"}}
```
```json
{"type":"play.end","requestId":"33333333-3333-4333-8333-333333333333","payload":{"clientId":"alice"}}
```
```json
{"type":"seed.get","requestId":"44444444-4444-4444-8444-444444444444","payload":{"clientId":"alice"}}
```
```json
{"type":"seed.rotate","requestId":"55555555-5555-4555-8555-555555555555","payload":{"clientId":"alice"}}
```

**With `websocat`** (`brew install websocat`, optional convenience — not required for anything in this repo):

```sh
websocat ws://localhost:8080/ws
{"type":"wallet.get","requestId":"11111111-1111-4111-8111-111111111111","payload":{"clientId":"alice"}}
```

### Postman collection

`postman/dice-game.postman_collection.json` — import it, set the `baseUrl` and `clientId` collection variables (defaults: `http://localhost:8080`, `alice`), run the **Happy Path** folder (Clients → Wallet → Play → EndPlay → Wallet, with assertions on status/schema/balance math), the **Protections** folder (bet above balance, zero/negative bet, invalid bet type, play while one is open, end play with none open, unknown client, replayed idempotency key), and the **Provably Fair** folder (get the commitment, rotate to reveal it, assert the revealed seed's hash matches what was published, then confirm it shows up in history — tolerant of `PROVABLY_FAIR_ENABLED=false`, where it just checks for a clean `404`). A pre-request script generates a fresh UUID `Idempotency-Key` for every request.

## Assumptions and trade-offs

- **Pessimistic locking vs. the `version` column**: `SELECT ... FOR UPDATE` inside a single transaction is the *sole* concurrency-control mechanism, and it is what makes this safe across multiple stateless server instances — the lock is held by Postgres, not application memory. The `version` column on `wallets` is incremented on every mutation as a belt-and-suspenders audit/optimistic-check guard (the `UPDATE` also matches on the pre-increment version), not the primary mechanism: every mutation path already goes through the row lock, so that extra match should always succeed; it exists as insurance against a hypothetical future code path that mutates the wallet outside a locked transaction.
- **Idempotency race**: two requests with the identical `(clientId, requestId)` racing concurrently are still safe even before either commits, because the wallet row lock already serializes them. The residual race is the `idempotency_keys` insert itself — on a unique-violation the whole transaction (including its business mutation) is rolled back and retried (bounded, 3 attempts with a short backoff), so the retry observes and returns the winner's committed response instead of erroring.
- **`requestId` uniqueness scope**: a `requestId` is expected to be globally unique per client across operation types, matching the given `(client_id, request_id)` primary key. Reusing one across a different operation, or replaying it with different request parameters (e.g. a different `betAmount`), is rejected with `VALIDATION_ERROR` rather than silently replaying a mismatched response — the latter is enforced via a SHA-256 fingerprint of the request parameters stored alongside the cached response (`request_hash`, migration `000003`).
- **Idempotency caching only on success**: the idempotency record is written only after all validation and business checks pass, in the same transaction as the mutation. A rejected request (e.g. `INSUFFICIENT_BALANCE`) is never cached, so retrying the same `requestId` after fixing the input correctly re-executes.
- **Idempotency key retention**: rows in `idempotency_keys` accumulate indefinitely in this implementation. A production deployment needs a scheduled purge (e.g. delete rows older than 72h) — see Production considerations.
- **Module layout**: the Go module lives at the repo root (`github.com/drzbraz/dice-bet`) rather than nesting a `dice-game/` subfolder, since there is exactly one module here and an extra path segment only lengthens imports.
- **WebSocket library**: `gorilla/websocket`. It's the most widely deployed, best-documented option, integrates directly with `net/http`'s `Hijacker` for the upgrade, and its single documented constraint — at most one goroutine may write to a connection at a time — is a deliberate fit here: each connection has exactly one writer goroutine, fed by a buffered channel carrying application messages, pings, and the close frame.
- **Validation**: DTO-level structural validation (required fields) is hand-written (`internal/dto/*.go`), not a third-party validator library, to keep the dependency surface minimal — consistent with "stdlib-first."
- **Auth-less transports**: neither transport authenticates the caller; a client self-asserts `clientId` in the payload. This is an explicit, intentional assessment-scope simplification — see Production considerations.
- **Migrations vs. the connection pool**: `golang-migrate` operates on `database/sql`, while the app's runtime pool is a native `pgxpool.Pool`. `RunMigrations` opens a short-lived `*sql.DB` via `pgx/v5/stdlib` purely for the migration step, then closes it before the long-lived pool is constructed.

## What would change for production

- **Auth**: bind the authenticated session/connection to exactly one `clientId` (e.g. a JWT validated at the WebSocket handshake or in HTTP middleware) instead of trusting a client-supplied field.
- **Rate limiting**: per-client and per-connection limits on both transports.
- **Idempotency key cleanup job**: a scheduled task purging `idempotency_keys` rows past a retention window.
- **Read replicas**: `GetBalance` (a pure read) could be served from a replica; `Play`/`EndPlay` still need the primary for the row lock.
- **Observability**: request tracing (OpenTelemetry), metrics (win rate, error rate by code, p99 latency), structured log correlation IDs threaded through `requestId`.

## Configuration

All via environment variables (see `.env.example` for defaults): `PORT`, `DATABASE_URL`, `DB_MAX_CONNS`, `MIN_BET`, `MAX_BET`, `RUN_MIGRATIONS`, `READ_TIMEOUT`, `WRITE_TIMEOUT`, `WS_PING_INTERVAL`, `WS_MAX_MESSAGE_BYTES`, `WALLET_CACHE_TTL`, `PROVABLY_FAIR_ENABLED`.

`READ_TIMEOUT`/`WRITE_TIMEOUT` govern the plain `http.Server` only (the HTTP mirror's per-request timeouts); they do **not** apply to the WebSocket connection's read deadline, which is instead derived as `2 × WS_PING_INTERVAL` in `cmd/server/main.go`. Reusing the HTTP timeout for WS idle tolerance was a real bug caught during integration testing: with the defaults at the time (`READ_TIMEOUT=15s`, `WS_PING_INTERVAL=30s`), every idle WS connection's read deadline expired before its first keepalive ping could ever arrive, silently killing the connection after ~15s of inactivity (e.g. a player sitting on an open round). Keep this in mind if you ever change `WS_PING_INTERVAL`: the read deadline tracks it automatically, but a *very* long ping interval still means a *very* long tolerance for a genuinely dead connection going undetected.
