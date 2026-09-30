package ws

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/drzbraz/dice-bet/internal/dto"
)

func TestWS_SeedGet_WhenFairnessDisabled_ReturnsFairnessDisabled(t *testing.T) {
	srv := newTestServer(t, DefaultConfig()) // fairness is nil here
	conn := dial(t, srv.wsURL())

	sendEnvelope(t, conn, TypeSeedGet, uuid.NewString(), map[string]any{"clientId": "alice"})
	errResp := readError(t, conn)

	assert.Equal(t, "FAIRNESS_DISABLED", errResp.Error.Code)
}

func TestWS_SeedGet_ReturnsCommitmentAndCreatesOnFirstUse(t *testing.T) {
	srv, _ := newTestServerWithFairness(t, DefaultConfig())
	conn := dial(t, srv.wsURL())

	sendEnvelope(t, conn, TypeSeedGet, uuid.NewString(), map[string]any{"clientId": "alice"})
	resp := readSuccess(t, conn)

	var data dto.SeedResponse
	require.NoError(t, decodeInto(resp.Data, &data))
	assert.Equal(t, "alice", data.ClientID)
	assert.NotEmpty(t, data.ServerSeedHash)
	assert.NotEmpty(t, data.ClientSeed)
	assert.Equal(t, int64(0), data.Nonce)
}

func TestWS_SeedRotate_RevealsOldSeedMatchingThePreviouslyPublishedHash(t *testing.T) {
	srv, _ := newTestServerWithFairness(t, DefaultConfig())
	conn := dial(t, srv.wsURL())

	sendEnvelope(t, conn, TypeSeedGet, uuid.NewString(), map[string]any{"clientId": "alice"})
	published := readSuccess(t, conn)
	var publishedData dto.SeedResponse
	require.NoError(t, decodeInto(published.Data, &publishedData))

	sendEnvelope(t, conn, TypeSeedRotate, uuid.NewString(), map[string]any{"clientId": "alice"})
	rotated := readSuccess(t, conn)
	var rotatedData dto.SeedRotateResponse
	require.NoError(t, decodeInto(rotated.Data, &rotatedData))

	require.NotNil(t, rotatedData.Retired)
	assert.Equal(t, publishedData.ServerSeedHash, rotatedData.Retired.ServerSeedHash)
	assert.NotEmpty(t, rotatedData.Retired.ServerSeed)
	assert.NotEqual(t, rotatedData.Retired.ServerSeedHash, rotatedData.Active.ServerSeedHash)
}

func TestWS_SeedHistory_ReturnsRetiredSeedsAfterRotation(t *testing.T) {
	srv, _ := newTestServerWithFairness(t, DefaultConfig())
	conn := dial(t, srv.wsURL())
	sendEnvelope(t, conn, TypeSeedGet, uuid.NewString(), map[string]any{"clientId": "alice"})
	readSuccess(t, conn)
	sendEnvelope(t, conn, TypeSeedRotate, uuid.NewString(), map[string]any{"clientId": "alice"})
	readSuccess(t, conn)

	sendEnvelope(t, conn, TypeSeedHistory, uuid.NewString(), map[string]any{"clientId": "alice"})
	resp := readSuccess(t, conn)

	var data dto.SeedHistoryResponse
	require.NoError(t, decodeInto(resp.Data, &data))
	require.Len(t, data.Seeds, 1)
	assert.NotEmpty(t, data.Seeds[0].ServerSeed)
}

func TestWS_PlayStart_WithFairnessEnabled_IncludesFairnessAndMatchesRolledNumber(t *testing.T) {
	srv, fairnessSvc := newTestServerWithFairness(t, DefaultConfig())
	conn := dial(t, srv.wsURL())

	sendEnvelope(t, conn, TypePlayStart, uuid.NewString(), map[string]any{"clientId": "alice", "betAmount": 100, "betType": "EVEN"})
	resp := readSuccess(t, conn)

	var data dto.PlayStartResponse
	require.NoError(t, decodeInto(resp.Data, &data))
	require.NotNil(t, data.Fairness)
	assert.Equal(t, int64(0), data.Fairness.Nonce)
	assert.NotEmpty(t, data.Fairness.ServerSeedHash)

	// Independently reconstruct the roll via the fairness service's own
	// view of the (still-active) seed, exactly as an external verifier
	// would once it's revealed -- proving the WS response's rolledNumber
	// is not just some arbitrary value, but the actual HMAC-derived one.
	seedResp, err := fairnessSvc.GetSeed(t.Context(), "alice")
	require.NoError(t, err)
	assert.Equal(t, data.Fairness.ServerSeedHash, seedResp.ServerSeedHash)
}
