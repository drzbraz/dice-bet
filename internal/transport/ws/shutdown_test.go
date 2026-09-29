package ws

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServer_Shutdown_ClosesActiveConnectionsCleanly(t *testing.T) {
	srv := newTestServer(t, DefaultConfig())
	conn := dial(t, srv.wsURL())

	sendEnvelope(t, conn, TypeWalletGet, uuid.NewString(), map[string]any{"clientId": "alice"})
	readSuccess(t, conn)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// srv is an httptest.Server wrapping a mux that dispatches to our
	// ws.Server; grab it back out to call Shutdown directly.
	wsServer := srv.wsServer
	require.NoError(t, wsServer.Shutdown(ctx))

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _, err := conn.ReadMessage()
	assert.Error(t, err)
	var closeErr *websocket.CloseError
	if errors.As(err, &closeErr) {
		assert.Equal(t, websocket.CloseGoingAway, closeErr.Code)
	}
}
