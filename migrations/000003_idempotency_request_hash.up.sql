-- Fingerprint of the request payload (excluding clientId/requestId), so a
-- replayed requestId whose actual parameters differ from the original
-- request (e.g. a different betAmount) is rejected instead of silently
-- returning the stale cached response.
ALTER TABLE idempotency_keys ADD COLUMN request_hash TEXT NOT NULL DEFAULT '';
