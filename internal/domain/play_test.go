package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/drzbraz/dice-bet/internal/domain"
)

func TestPlay_Close(t *testing.T) {
	p := &domain.Play{Status: domain.PlayStatusOpen}
	closedAt := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	p.Close(closedAt)

	assert.Equal(t, domain.PlayStatusClosed, p.Status)
	require.NotNil(t, p.ClosedAt)
	assert.True(t, closedAt.Equal(*p.ClosedAt))
}
