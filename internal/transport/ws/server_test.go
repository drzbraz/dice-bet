package ws

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/drzbraz/dice-bet/internal/dto"
)

func dial(t *testing.T, url string) *websocket.Conn {
	t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	require.NoError(t, err)
	t.Cleanup(func() { conn.Close() })
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	return conn
}

func sendRaw(t *testing.T, conn *websocket.Conn, raw string) {
	t.Helper()
	require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte(raw)))
}

func sendEnvelope(t *testing.T, conn *websocket.Conn, msgType, requestID string, payload any) {
	t.Helper()
	body, err := json.Marshal(payload)
	require.NoError(t, err)
	env := dto.Envelope{Type: msgType, RequestID: requestID, Payload: body}
	require.NoError(t, conn.WriteJSON(env))
}

func readSuccess(t *testing.T, conn *websocket.Conn) dto.SuccessResponse {
	t.Helper()
	var resp dto.SuccessResponse
	require.NoError(t, conn.ReadJSON(&resp))
	require.True(t, resp.Success, "expected a success response, got %+v", resp)
	return resp
}

func readError(t *testing.T, conn *websocket.Conn) dto.ErrorResponse {
	t.Helper()
	var resp dto.ErrorResponse
	require.NoError(t, conn.ReadJSON(&resp))
	require.False(t, resp.Success, "expected an error response, got %+v", resp)
	require.Equal(t, "error", resp.Type)
	return resp
}

func TestWS_FullFlow_WalletPlayEndPlayWallet(t *testing.T) {
	srv := newTestServer(t, DefaultConfig())
	conn := dial(t, srv.wsURL())

	sendEnvelope(t, conn, TypeWalletGet, uuid.NewString(), map[string]any{"clientId": "alice"})
	walletResp := readSuccess(t, conn)
	assert.Equal(t, "wallet.get.result", walletResp.Type)

	var walletData dto.WalletGetResponse
	require.NoError(t, decodeInto(walletResp.Data, &walletData))
	assert.Equal(t, int64(1000), walletData.Balance)

	playReqID := uuid.NewString()
	sendEnvelope(t, conn, TypePlayStart, playReqID, map[string]any{"clientId": "alice", "betAmount": 100, "betType": "EVEN"})
	playResp := readSuccess(t, conn)
	assert.Equal(t, "play.start.result", playResp.Type)
	assert.Equal(t, playReqID, playResp.RequestID)

	var playData dto.PlayStartResponse
	require.NoError(t, decodeInto(playResp.Data, &playData))
	assert.Equal(t, "WIN", playData.Result) // roller is fixed to 4 (even)
	assert.Equal(t, int64(200), playData.Payout)
	assert.Equal(t, int64(900), playData.Balance)
	assert.Equal(t, "OPEN", playData.Status)

	sendEnvelope(t, conn, TypePlayEnd, uuid.NewString(), map[string]any{"clientId": "alice"})
	endResp := readSuccess(t, conn)
	assert.Equal(t, "play.end.result", endResp.Type)

	var endData dto.EndPlayResponse
	require.NoError(t, decodeInto(endResp.Data, &endData))
	assert.Equal(t, "CLOSED", endData.Status)
	assert.Equal(t, int64(200), endData.CreditedAmount)
	assert.Equal(t, int64(1100), endData.Balance)
}

func TestWS_MalformedJSON_ReturnsInvalidMessageAndKeepsConnectionOpen(t *testing.T) {
	srv := newTestServer(t, DefaultConfig())
	conn := dial(t, srv.wsURL())

	sendRaw(t, conn, `{not valid json`)
	errResp := readError(t, conn)
	assert.Equal(t, "INVALID_MESSAGE", errResp.Error.Code)
	assert.Nil(t, errResp.RequestID)

	// The connection must still be usable afterwards.
	sendEnvelope(t, conn, TypeWalletGet, uuid.NewString(), map[string]any{"clientId": "alice"})
	readSuccess(t, conn)
}

func TestWS_UnknownMessageType_ReturnsErrorAndKeepsConnectionOpen(t *testing.T) {
	srv := newTestServer(t, DefaultConfig())
	conn := dial(t, srv.wsURL())

	reqID := uuid.NewString()
	sendEnvelope(t, conn, "bogus.operation", reqID, map[string]any{})
	errResp := readError(t, conn)
	assert.Equal(t, "UNKNOWN_MESSAGE_TYPE", errResp.Error.Code)
	require.NotNil(t, errResp.RequestID)
	assert.Equal(t, reqID, *errResp.RequestID)

	sendEnvelope(t, conn, TypeWalletGet, uuid.NewString(), map[string]any{"clientId": "alice"})
	readSuccess(t, conn)
}

func TestWS_MissingRequestID_ReturnsInvalidMessage(t *testing.T) {
	srv := newTestServer(t, DefaultConfig())
	conn := dial(t, srv.wsURL())

	sendRaw(t, conn, `{"type":"wallet.get","payload":{"clientId":"alice"}}`)
	errResp := readError(t, conn)
	assert.Equal(t, "INVALID_MESSAGE", errResp.Error.Code)
	assert.Nil(t, errResp.RequestID)
}

func TestWS_MissingClientId_ReturnsValidationError(t *testing.T) {
	srv := newTestServer(t, DefaultConfig())
	conn := dial(t, srv.wsURL())

	sendEnvelope(t, conn, TypeWalletGet, uuid.NewString(), map[string]any{})
	errResp := readError(t, conn)
	assert.Equal(t, "VALIDATION_ERROR", errResp.Error.Code)
}

func TestWS_UnknownClient_ReturnsClientNotFound(t *testing.T) {
	srv := newTestServer(t, DefaultConfig())
	conn := dial(t, srv.wsURL())

	sendEnvelope(t, conn, TypeWalletGet, uuid.NewString(), map[string]any{"clientId": "ghost"})
	errResp := readError(t, conn)
	assert.Equal(t, "CLIENT_NOT_FOUND", errResp.Error.Code)
}

func TestWS_OversizedMessage_ClosesConnectionWithoutCrashingServer(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxMessageBytes = 200 // comfortably above a normal small request, well below the oversized one
	srv := newTestServer(t, cfg)
	conn := dial(t, srv.wsURL())

	oversized := make(map[string]any)
	oversized["clientId"] = "alice"
	oversized["padding"] = string(make([]byte, 500))
	sendEnvelope(t, conn, TypeWalletGet, uuid.NewString(), oversized)

	// The connection is expected to close (RFC 6455): further reads must
	// eventually fail rather than hang or the process crashing.
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, _, err := conn.ReadMessage()
	assert.Error(t, err)

	// The server itself must still be serving other connections.
	other := dial(t, srv.wsURL())
	sendEnvelope(t, other, TypeWalletGet, uuid.NewString(), map[string]any{"clientId": "alice"})
	readSuccess(t, other)
}

func TestWS_IdempotentReplay_ReturnsSameResponse(t *testing.T) {
	srv := newTestServer(t, DefaultConfig())
	conn := dial(t, srv.wsURL())

	reqID := uuid.NewString()
	sendEnvelope(t, conn, TypePlayStart, reqID, map[string]any{"clientId": "alice", "betAmount": 100, "betType": "EVEN"})
	first := readSuccess(t, conn)

	sendEnvelope(t, conn, TypePlayStart, reqID, map[string]any{"clientId": "alice", "betAmount": 100, "betType": "EVEN"})
	second := readSuccess(t, conn)

	assert.Equal(t, first.Data, second.Data)
}

// decodeInto round-trips v (typically an `any` holding a decoded
// map[string]any from JSON) through JSON into target, since dto.Data is
// typed `any` on the wire.
func decodeInto(v any, target any) error {
	body, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, target)
}
