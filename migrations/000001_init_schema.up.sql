CREATE TABLE clients (
    id          TEXT PRIMARY KEY,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE wallets (
    client_id   TEXT PRIMARY KEY REFERENCES clients(id),
    balance     BIGINT NOT NULL CHECK (balance >= 0),
    currency    CHAR(3) NOT NULL DEFAULT 'EUR',
    version     BIGINT NOT NULL DEFAULT 0,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE plays (
    id             UUID PRIMARY KEY,
    client_id      TEXT NOT NULL REFERENCES clients(id),
    bet_amount     BIGINT NOT NULL CHECK (bet_amount > 0),
    bet_type       TEXT NOT NULL CHECK (bet_type IN ('EVEN', 'ODD')),
    rolled_number  SMALLINT NOT NULL CHECK (rolled_number BETWEEN 1 AND 6),
    result         TEXT NOT NULL CHECK (result IN ('WIN', 'LOSE')),
    payout         BIGINT NOT NULL CHECK (payout >= 0),
    status         TEXT NOT NULL CHECK (status IN ('OPEN', 'CLOSED')),
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    closed_at      TIMESTAMPTZ
);

-- At most one OPEN play per client, enforced by the database.
CREATE UNIQUE INDEX uq_plays_one_open_per_client ON plays (client_id) WHERE status = 'OPEN';

CREATE INDEX idx_plays_client_id ON plays (client_id);

-- Append-only ledger for auditability: every balance change has a row.
CREATE TABLE wallet_transactions (
    id             UUID PRIMARY KEY,
    client_id      TEXT NOT NULL REFERENCES clients(id),
    play_id        UUID NOT NULL REFERENCES plays(id),
    type           TEXT NOT NULL CHECK (type IN ('BET_DEBIT', 'PAYOUT_CREDIT')),
    amount         BIGINT NOT NULL CHECK (amount >= 0),
    balance_after  BIGINT NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (play_id, type)
);

CREATE TABLE idempotency_keys (
    client_id      TEXT NOT NULL,
    request_id     TEXT NOT NULL,
    operation      TEXT NOT NULL,
    response_body  JSONB NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (client_id, request_id)
);
