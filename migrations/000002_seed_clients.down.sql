-- Dependent rows (plays, wallet_transactions) must be deleted first: both
-- reference clients(id) with no ON DELETE CASCADE, so deleting the seeded
-- clients/wallets directly would fail with a foreign-key violation as soon
-- as any of them has actually played.
DELETE FROM wallet_transactions WHERE client_id IN ('alice', 'bob', 'carol', 'dave');
DELETE FROM plays WHERE client_id IN ('alice', 'bob', 'carol', 'dave');
DELETE FROM wallets WHERE client_id IN ('alice', 'bob', 'carol', 'dave');
DELETE FROM clients WHERE id IN ('alice', 'bob', 'carol', 'dave');
