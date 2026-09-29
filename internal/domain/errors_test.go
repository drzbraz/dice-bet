package domain_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/drzbraz/dice-bet/internal/domain"
)

func TestError_ErrorAndUnwrap(t *testing.T) {
	cause := errors.New("boom")
	err := domain.ErrInternal(cause)
	assert.Contains(t, err.Error(), "INTERNAL_ERROR")
	assert.Contains(t, err.Error(), "boom")
	assert.Equal(t, cause, errors.Unwrap(err))

	simple := domain.ErrNoActivePlay()
	assert.Equal(t, "NO_ACTIVE_PLAY: no open play exists for this client", simple.Error())
	assert.Nil(t, errors.Unwrap(simple))
}

func TestErrorConstructors_SetExpectedCodesAndDetails(t *testing.T) {
	cases := []struct {
		name string
		err  *domain.Error
		code domain.ErrorCode
	}{
		{"ClientNotFound", domain.ErrClientNotFound("alice"), domain.ErrCodeClientNotFound},
		{"PlayAlreadyInProgress", domain.ErrPlayAlreadyInProgress(), domain.ErrCodePlayAlreadyInProgress},
		{"NoActivePlay", domain.ErrNoActivePlay(), domain.ErrCodeNoActivePlay},
		{"InsufficientBalance", domain.ErrInsufficientBalance(), domain.ErrCodeInsufficientBalance},
		{"InvalidBetAmount", domain.ErrInvalidBetAmount("too small"), domain.ErrCodeInvalidBetAmount},
		{"InvalidBetType", domain.ErrInvalidBetType("PRIME"), domain.ErrCodeInvalidBetType},
		{"Validation", domain.ErrValidation("requestId", "missing"), domain.ErrCodeValidationError},
		{"InvalidMessage", domain.ErrInvalidMessage("bad json"), domain.ErrCodeInvalidMessage},
		{"UnknownMessageType", domain.ErrUnknownMessageType("bogus.op"), domain.ErrCodeUnknownMessageType},
		{"ServiceUnavailable", domain.ErrServiceUnavailable(errors.New("db down")), domain.ErrCodeServiceUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.code, tc.err.Code)
			assert.NotEmpty(t, tc.err.Message)
		})
	}

	assert.Equal(t, "alice", domain.ErrClientNotFound("alice").Details["clientId"])
	assert.Equal(t, "betAmount", domain.ErrInvalidBetAmount("x").Details["field"])
	assert.Equal(t, "PRIME", domain.ErrInvalidBetType("PRIME").Details["value"])
	assert.Equal(t, "requestId", domain.ErrValidation("requestId", "x").Details["field"])
	assert.Equal(t, "bogus.op", domain.ErrUnknownMessageType("bogus.op").Details["type"])
}
