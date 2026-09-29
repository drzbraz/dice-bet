package http

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func doRequest(t *testing.T, router http.Handler, method, path string, headers map[string]string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var bodyReader *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		require.NoError(t, err)
		bodyReader = bytes.NewReader(b)
	} else {
		bodyReader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, bodyReader)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func decodeBody(t *testing.T, rec *httptest.ResponseRecorder, target any) {
	t.Helper()
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), target))
}

func TestHTTP_GetWallet_Success(t *testing.T) {
	router := newTestRouter(nil)

	rec := doRequest(t, router, http.MethodGet, "/api/v1/clients/alice/wallet", nil, nil)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp struct {
		Success bool
		Data    struct {
			Balance int64
		}
	}
	decodeBody(t, rec, &resp)
	assert.True(t, resp.Success)
	assert.Equal(t, int64(1000), resp.Data.Balance)
}

func TestHTTP_GetWallet_UnknownClientReturns404(t *testing.T) {
	router := newTestRouter(nil)

	rec := doRequest(t, router, http.MethodGet, "/api/v1/clients/ghost/wallet", nil, nil)

	assert.Equal(t, http.StatusNotFound, rec.Code)
	var resp struct {
		Error struct{ Code string }
	}
	decodeBody(t, rec, &resp)
	assert.Equal(t, "CLIENT_NOT_FOUND", resp.Error.Code)
}

func TestHTTP_PostPlay_Success(t *testing.T) {
	router := newTestRouter(nil)

	rec := doRequest(t, router, http.MethodPost, "/api/v1/plays",
		map[string]string{"Idempotency-Key": uuid.NewString(), "Content-Type": "application/json"},
		map[string]any{"clientId": "alice", "betAmount": 100, "betType": "EVEN"},
	)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp struct {
		Success bool
		Data    struct {
			Result  string
			Balance int64
		}
	}
	decodeBody(t, rec, &resp)
	assert.True(t, resp.Success)
	assert.Equal(t, "WIN", resp.Data.Result)
	assert.Equal(t, int64(900), resp.Data.Balance)
}

func TestHTTP_PostPlay_MissingIdempotencyKeyReturns400(t *testing.T) {
	router := newTestRouter(nil)

	rec := doRequest(t, router, http.MethodPost, "/api/v1/plays", nil,
		map[string]any{"clientId": "alice", "betAmount": 100, "betType": "EVEN"},
	)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	var resp struct {
		Error struct{ Code string }
	}
	decodeBody(t, rec, &resp)
	assert.Equal(t, "VALIDATION_ERROR", resp.Error.Code)
}

func TestHTTP_PostPlay_BetExceedingBalanceReturns422(t *testing.T) {
	router := newTestRouter(nil)

	rec := doRequest(t, router, http.MethodPost, "/api/v1/plays",
		map[string]string{"Idempotency-Key": uuid.NewString()},
		map[string]any{"clientId": "alice", "betAmount": 50000, "betType": "EVEN"},
	)

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	var resp struct {
		Error struct{ Code string }
	}
	decodeBody(t, rec, &resp)
	assert.Equal(t, "INSUFFICIENT_BALANCE", resp.Error.Code)
}

func TestHTTP_PostPlay_ZeroBetReturns422(t *testing.T) {
	router := newTestRouter(nil)

	rec := doRequest(t, router, http.MethodPost, "/api/v1/plays",
		map[string]string{"Idempotency-Key": uuid.NewString()},
		map[string]any{"clientId": "alice", "betAmount": 0, "betType": "EVEN"},
	)

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	var resp struct {
		Error struct{ Code string }
	}
	decodeBody(t, rec, &resp)
	assert.Equal(t, "INVALID_BET_AMOUNT", resp.Error.Code)
}

func TestHTTP_PostPlay_InvalidBetTypeReturns422(t *testing.T) {
	router := newTestRouter(nil)

	rec := doRequest(t, router, http.MethodPost, "/api/v1/plays",
		map[string]string{"Idempotency-Key": uuid.NewString()},
		map[string]any{"clientId": "alice", "betAmount": 100, "betType": "PRIME"},
	)

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	var resp struct {
		Error struct{ Code string }
	}
	decodeBody(t, rec, &resp)
	assert.Equal(t, "INVALID_BET_TYPE", resp.Error.Code)
}

func TestHTTP_PostPlay_UnknownClientReturns404(t *testing.T) {
	router := newTestRouter(nil)

	rec := doRequest(t, router, http.MethodPost, "/api/v1/plays",
		map[string]string{"Idempotency-Key": uuid.NewString()},
		map[string]any{"clientId": "ghost", "betAmount": 100, "betType": "EVEN"},
	)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHTTP_PostPlay_WhileAlreadyOpenReturns409(t *testing.T) {
	router := newTestRouter(nil)

	doRequest(t, router, http.MethodPost, "/api/v1/plays",
		map[string]string{"Idempotency-Key": uuid.NewString()},
		map[string]any{"clientId": "alice", "betAmount": 100, "betType": "EVEN"},
	)

	rec := doRequest(t, router, http.MethodPost, "/api/v1/plays",
		map[string]string{"Idempotency-Key": uuid.NewString()},
		map[string]any{"clientId": "alice", "betAmount": 100, "betType": "EVEN"},
	)

	assert.Equal(t, http.StatusConflict, rec.Code)
	var resp struct {
		Error struct{ Code string }
	}
	decodeBody(t, rec, &resp)
	assert.Equal(t, "PLAY_ALREADY_IN_PROGRESS", resp.Error.Code)
}

func TestHTTP_PostPlay_ReplayedIdempotencyKeyReturnsSameResponse(t *testing.T) {
	router := newTestRouter(nil)
	key := uuid.NewString()

	first := doRequest(t, router, http.MethodPost, "/api/v1/plays",
		map[string]string{"Idempotency-Key": key},
		map[string]any{"clientId": "alice", "betAmount": 100, "betType": "EVEN"},
	)
	second := doRequest(t, router, http.MethodPost, "/api/v1/plays",
		map[string]string{"Idempotency-Key": key},
		map[string]any{"clientId": "alice", "betAmount": 100, "betType": "EVEN"},
	)

	// The envelope timestamp reflects when each response was written, but
	// the replayed business data (the "data" field) must be identical,
	// proving the second call did not re-execute the play.
	var firstResp, secondResp struct{ Data json.RawMessage }
	decodeBody(t, first, &firstResp)
	decodeBody(t, second, &secondResp)
	assert.JSONEq(t, string(firstResp.Data), string(secondResp.Data))
}

func TestHTTP_PostEndPlay_Success(t *testing.T) {
	router := newTestRouter(nil)

	doRequest(t, router, http.MethodPost, "/api/v1/plays",
		map[string]string{"Idempotency-Key": uuid.NewString()},
		map[string]any{"clientId": "alice", "betAmount": 100, "betType": "EVEN"},
	)

	rec := doRequest(t, router, http.MethodPost, "/api/v1/plays/end",
		map[string]string{"Idempotency-Key": uuid.NewString()},
		map[string]any{"clientId": "alice"},
	)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp struct {
		Data struct {
			Status  string
			Balance int64
		}
	}
	decodeBody(t, rec, &resp)
	assert.Equal(t, "CLOSED", resp.Data.Status)
	assert.Equal(t, int64(1100), resp.Data.Balance)
}

func TestHTTP_PostEndPlay_NoOpenPlayReturns409(t *testing.T) {
	router := newTestRouter(nil)

	rec := doRequest(t, router, http.MethodPost, "/api/v1/plays/end",
		map[string]string{"Idempotency-Key": uuid.NewString()},
		map[string]any{"clientId": "alice"},
	)

	assert.Equal(t, http.StatusConflict, rec.Code)
	var resp struct {
		Error struct{ Code string }
	}
	decodeBody(t, rec, &resp)
	assert.Equal(t, "NO_ACTIVE_PLAY", resp.Error.Code)
}

func TestHTTP_MalformedJSONReturns400(t *testing.T) {
	router := newTestRouter(nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/plays", bytes.NewReader([]byte(`{not valid`)))
	req.Header.Set("Idempotency-Key", uuid.NewString())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	var resp struct {
		Error struct{ Code string }
	}
	decodeBody(t, rec, &resp)
	assert.Equal(t, "INVALID_MESSAGE", resp.Error.Code)
}

func TestHTTP_Health_Ok(t *testing.T) {
	router := newTestRouter(nil)

	rec := doRequest(t, router, http.MethodGet, "/health", nil, nil)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestHTTP_Health_Unavailable(t *testing.T) {
	router := newTestRouter(assert.AnError)

	rec := doRequest(t, router, http.MethodGet, "/health", nil, nil)

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}
