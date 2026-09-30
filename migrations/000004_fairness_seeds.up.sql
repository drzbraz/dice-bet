-- Provably-fair rolls: each client has a sequence of "seed epochs". While
-- an epoch is active, every roll is HMAC-SHA256(server_seed, client_seed:
-- nonce) -- deterministic and, once server_seed is revealed at rotation,
-- independently reproducible by anyone. See README "Provably fair rolls".
CREATE TABLE fairness_seeds (
    id                UUID PRIMARY KEY,
    client_id         TEXT NOT NULL REFERENCES clients(id),
    server_seed       TEXT NOT NULL,
    server_seed_hash  TEXT NOT NULL,
    client_seed       TEXT NOT NULL,
    nonce             BIGINT NOT NULL DEFAULT 0 CHECK (nonce >= 0),
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    retired_at        TIMESTAMPTZ
);

-- At most one active (not yet retired) seed per client -- the same
-- partial-unique-index pattern as uq_plays_one_open_per_client.
CREATE UNIQUE INDEX uq_fairness_seeds_one_active_per_client
    ON fairness_seeds (client_id) WHERE retired_at IS NULL;

CREATE INDEX idx_fairness_seeds_client_id ON fairness_seeds (client_id);
