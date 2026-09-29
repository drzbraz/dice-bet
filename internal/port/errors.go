package port

import "errors"

// ErrIdempotencyConflict is returned by IdempotencyRepository.Save when a
// concurrent request for the same (clientID, requestID) committed its
// record first. The caller should retry the idempotency lookup to obtain
// the winner's stored response rather than treat this as a failure.
var ErrIdempotencyConflict = errors.New("idempotency key conflict")
