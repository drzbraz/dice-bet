// Package domain contains the core business entities and rules for the dice
// game, free of any transport or persistence concerns.
package domain

import "fmt"

// ErrorCode is the single source of truth for the stable error codes
// returned to clients over both the WebSocket and HTTP transports.
type ErrorCode string

const (
	ErrCodeInvalidMessage        ErrorCode = "INVALID_MESSAGE"
	ErrCodeUnknownMessageType    ErrorCode = "UNKNOWN_MESSAGE_TYPE"
	ErrCodeValidationError       ErrorCode = "VALIDATION_ERROR"
	ErrCodeInvalidBetAmount      ErrorCode = "INVALID_BET_AMOUNT"
	ErrCodeInvalidBetType        ErrorCode = "INVALID_BET_TYPE"
	ErrCodeClientNotFound        ErrorCode = "CLIENT_NOT_FOUND"
	ErrCodeInsufficientBalance   ErrorCode = "INSUFFICIENT_BALANCE"
	ErrCodePlayAlreadyInProgress ErrorCode = "PLAY_ALREADY_IN_PROGRESS"
	ErrCodeNoActivePlay          ErrorCode = "NO_ACTIVE_PLAY"
	ErrCodeServiceUnavailable    ErrorCode = "SERVICE_UNAVAILABLE"
	ErrCodeInternal              ErrorCode = "INTERNAL_ERROR"
	ErrCodeFairnessDisabled      ErrorCode = "FAIRNESS_DISABLED"
)

// Error is a typed domain error carrying a stable machine-readable Code in
// addition to a human-readable Message. Transports map Code to a protocol
// specific representation (WS error frame, HTTP status).
type Error struct {
	Code    ErrorCode
	Message string
	Details map[string]any
	cause   error
}

func (e *Error) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.cause)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *Error) Unwrap() error { return e.cause }

func newError(code ErrorCode, message string) *Error {
	return &Error{Code: code, Message: message}
}

func ErrClientNotFound(clientID string) *Error {
	return &Error{
		Code:    ErrCodeClientNotFound,
		Message: "client not found",
		Details: map[string]any{"clientId": clientID},
	}
}

func ErrPlayAlreadyInProgress() *Error {
	return newError(ErrCodePlayAlreadyInProgress, "an open play already exists for this client")
}

func ErrNoActivePlay() *Error {
	return newError(ErrCodeNoActivePlay, "no open play exists for this client")
}

func ErrInsufficientBalance() *Error {
	return newError(ErrCodeInsufficientBalance, "bet amount exceeds available balance")
}

func ErrInvalidBetAmount(reason string) *Error {
	return &Error{
		Code:    ErrCodeInvalidBetAmount,
		Message: reason,
		Details: map[string]any{"field": "betAmount"},
	}
}

func ErrInvalidBetType(got string) *Error {
	return &Error{
		Code:    ErrCodeInvalidBetType,
		Message: "betType must be EVEN or ODD",
		Details: map[string]any{"field": "betType", "value": got},
	}
}

func ErrValidation(field, reason string) *Error {
	return &Error{
		Code:    ErrCodeValidationError,
		Message: reason,
		Details: map[string]any{"field": field},
	}
}

func ErrInvalidMessage(reason string) *Error {
	return newError(ErrCodeInvalidMessage, reason)
}

func ErrUnknownMessageType(msgType string) *Error {
	return &Error{
		Code:    ErrCodeUnknownMessageType,
		Message: "unknown message type",
		Details: map[string]any{"type": msgType},
	}
}

func ErrServiceUnavailable(cause error) *Error {
	return &Error{Code: ErrCodeServiceUnavailable, Message: "service temporarily unavailable", cause: cause}
}

// ErrInternal wraps an unexpected/internal failure. The underlying cause is
// preserved for logging via errors.Unwrap but is never exposed to clients.
func ErrInternal(cause error) *Error {
	return &Error{Code: ErrCodeInternal, Message: "internal error", cause: cause}
}

// ErrFairnessDisabled reports that a provably-fair endpoint was called
// while PROVABLY_FAIR_ENABLED is false. See README "Provably fair rolls".
func ErrFairnessDisabled() *Error {
	return newError(ErrCodeFairnessDisabled, "provably-fair rolls are not enabled on this server")
}
