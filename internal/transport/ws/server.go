package ws

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/drzbraz/dice-bet/internal/domain"
	"github.com/drzbraz/dice-bet/internal/dto"
)

// Config controls per-connection behavior.
type Config struct {
	// MaxMessageBytes bounds inbound message size; exceeding it closes the
	// connection per RFC 6455 (rule 10's "oversized messages" guard).
	MaxMessageBytes int64
	// ReadTimeout is the read deadline, reset on every received pong.
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	// PingInterval is how often the server pings an idle connection to
	// keep it alive and detect dead peers.
	PingInterval time.Duration
	// SendBuffer bounds the outbound message queue per connection.
	SendBuffer int
}

// DefaultConfig returns sane production defaults.
func DefaultConfig() Config {
	return Config{
		MaxMessageBytes: 4096,
		ReadTimeout:     60 * time.Second,
		WriteTimeout:    10 * time.Second,
		PingInterval:    30 * time.Second,
		SendBuffer:      16,
	}
}

// Server upgrades HTTP connections to WebSocket and drives each
// connection's lifecycle. It tracks active connections so Shutdown can
// close them cleanly: net/http.Server.Shutdown does not know about
// hijacked connections such as WebSockets and will not wait for or close
// them on its own.
type Server struct {
	router   *Router
	cfg      Config
	upgrader websocket.Upgrader
	logger   *slog.Logger

	mu    sync.Mutex
	conns map[*connection]struct{}
}

// NewServer constructs a Server. logger defaults to slog.Default() if nil.
func NewServer(router *Router, cfg Config, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{
		router: router,
		cfg:    cfg,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			// The WebSocket contract has no browser-origin CSRF concerns
			// (no cookies/session state is used for auth); clients
			// self-assert clientId in the payload. See README assumptions.
			CheckOrigin: func(r *http.Request) bool { return true },
		},
		logger: logger,
		conns:  make(map[*connection]struct{}),
	}
}

// ServeHTTP upgrades the connection and blocks until it closes. Any
// failure here (bad upgrade request) never affects other connections or
// the server process.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.Warn("websocket upgrade failed", "error", err)
		return
	}
	c := newConnection(conn, s.router, s.cfg, s.logger)

	s.mu.Lock()
	s.conns[c] = struct{}{}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.conns, c)
		s.mu.Unlock()
	}()

	c.run(r.Context())
}

// Shutdown closes every currently active WebSocket connection cleanly
// (sending a close frame) and waits for their read/write goroutines to
// finish, up to ctx's deadline.
func (s *Server) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	conns := make([]*connection, 0, len(s.conns))
	for c := range s.conns {
		conns = append(conns, c)
	}
	s.mu.Unlock()

	var wg sync.WaitGroup
	for _, c := range conns {
		wg.Add(1)
		go func(c *connection) {
			defer wg.Done()
			c.closeGracefully()
		}(c)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// connection owns a single WebSocket connection's lifecycle: a read pump
// (this goroutine) and exactly one writer goroutine, communicating via
// sendCh, since gorilla connections do not support concurrent writes.
type connection struct {
	conn    *websocket.Conn
	router  *Router
	cfg     Config
	logger  *slog.Logger
	sendCh  chan []byte
	closeCh chan struct{}
	runDone chan struct{}
}

func newConnection(conn *websocket.Conn, router *Router, cfg Config, logger *slog.Logger) *connection {
	return &connection{
		conn:    conn,
		router:  router,
		cfg:     cfg,
		logger:  logger,
		sendCh:  make(chan []byte, cfg.SendBuffer),
		closeCh: make(chan struct{}),
		runDone: make(chan struct{}),
	}
}

func (c *connection) run(ctx context.Context) {
	defer close(c.runDone)

	done := make(chan struct{})
	go func() {
		c.writePump()
		close(done)
	}()

	c.readPump(ctx)
	close(c.closeCh)
	<-done
}

// closeGracefully sends a close frame and forces the read loop to unblock,
// then waits for the connection's goroutines to finish. WriteControl and
// SetReadDeadline are both safe to call concurrently with the connection's
// own read/write pump goroutines per gorilla/websocket's concurrency
// guarantees.
func (c *connection) closeGracefully() {
	deadline := time.Now().Add(2 * time.Second)
	_ = c.conn.WriteControl(
		websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseGoingAway, "server shutting down"),
		deadline,
	)
	_ = c.conn.SetReadDeadline(time.Now())
	<-c.runDone
}

// writePump is the connection's single writer goroutine: every outbound
// application message and every ping frame flows through it.
func (c *connection) writePump() {
	ticker := time.NewTicker(c.cfg.PingInterval)
	defer func() {
		ticker.Stop()
		_ = c.conn.Close()
	}()

	for {
		// Give sendCh priority over closeCh: without this, a message
		// enqueued just before the connection starts closing (e.g. an
		// oversized-message error reported by readPump right before it
		// returns) races the plain select below against closeCh being
		// closed, and select's pseudo-random tie-breaking can drop it.
		select {
		case msg, ok := <-c.sendCh:
			if !ok {
				return
			}
			_ = c.conn.SetWriteDeadline(time.Now().Add(c.cfg.WriteTimeout))
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
			continue
		default:
		}

		select {
		case msg, ok := <-c.sendCh:
			if !ok {
				return
			}
			_ = c.conn.SetWriteDeadline(time.Now().Add(c.cfg.WriteTimeout))
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(c.cfg.WriteTimeout))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		case <-c.closeCh:
			// Drain any messages already buffered (e.g. a final error
			// response) before exiting, rather than dropping them.
			for {
				select {
				case msg, ok := <-c.sendCh:
					if !ok {
						return
					}
					_ = c.conn.SetWriteDeadline(time.Now().Add(c.cfg.WriteTimeout))
					if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
						return
					}
				default:
					return
				}
			}
		}
	}
}

// readPump reads and dispatches inbound messages until the connection
// closes (client disconnect, protocol error, or the size limit being
// exceeded). A read error here only ever ends this one connection; it
// never propagates to other connections or the server.
func (c *connection) readPump(ctx context.Context) {
	c.conn.SetReadLimit(c.cfg.MaxMessageBytes)
	_ = c.conn.SetReadDeadline(time.Now().Add(c.cfg.ReadTimeout))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(c.cfg.ReadTimeout))
	})

	for {
		_, raw, err := c.conn.ReadMessage()
		if err != nil {
			if errors.Is(err, websocket.ErrReadLimit) {
				c.trySendError(nil, domain.ErrInvalidMessage("message exceeds the maximum allowed size"))
			}
			return
		}
		c.handleMessage(ctx, raw)
	}
}

// handleMessage decodes and dispatches a single inbound message. A panic
// anywhere in a handler is recovered here so it can never crash the
// server or even this connection: the read loop simply continues with the
// next message (rule 10).
func (c *connection) handleMessage(ctx context.Context, raw []byte) {
	defer func() {
		if r := recover(); r != nil {
			c.logger.Error("recovered panic handling websocket message", "panic", r)
			c.trySendError(nil, domain.ErrInternal(fmt.Errorf("panic: %v", r)))
		}
	}()

	var env dto.Envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		c.trySendError(nil, domain.ErrInvalidMessage("malformed JSON"))
		return
	}

	var requestIDPtr *string
	if env.RequestID != "" {
		id := env.RequestID
		requestIDPtr = &id
	}
	if env.RequestID == "" {
		c.trySendError(nil, domain.ErrInvalidMessage("requestId is required"))
		return
	}
	if env.Type == "" {
		c.trySendError(requestIDPtr, domain.ErrInvalidMessage("type is required"))
		return
	}

	handler, ok := c.router.Lookup(env.Type)
	if !ok {
		c.trySendError(requestIDPtr, domain.ErrUnknownMessageType(env.Type))
		return
	}

	data, err := handler(ctx, env.RequestID, env.Payload)
	if err != nil {
		c.trySendError(requestIDPtr, err)
		return
	}

	c.trySendSuccess(ResultType(env.Type), env.RequestID, data)
}

func (c *connection) trySendSuccess(msgType, requestID string, data any) {
	body, err := json.Marshal(dto.NewSuccessResponse(msgType, requestID, data))
	if err != nil {
		c.logger.Error("marshal success response", "error", err)
		return
	}
	c.enqueue(body)
}

func (c *connection) trySendError(requestID *string, err error) {
	body, marshalErr := json.Marshal(dto.NewErrorResponse(requestID, mapError(err)))
	if marshalErr != nil {
		c.logger.Error("marshal error response", "error", marshalErr)
		return
	}
	c.enqueue(body)
}

// enqueue hands body to the writer goroutine, without blocking forever if
// the connection is already closing.
func (c *connection) enqueue(body []byte) {
	select {
	case c.sendCh <- body:
	case <-c.closeCh:
	}
}
