// Package ws implements the WebSocket transport: it decodes/encodes the
// message envelope defined in internal/dto, dispatches by message type,
// and adapts the service layer's use cases and errors to that envelope.
// It contains no business logic.
package ws

import (
	"context"
	"encoding/json"
)

// Handler processes a decoded request payload and returns the response
// data to embed in the success envelope, or an error.
type Handler func(ctx context.Context, requestID string, payload json.RawMessage) (any, error)

// Router maps a message type to its Handler. New message types are added
// by registering a handler, without modifying existing handlers or the
// server's read loop (open/closed).
type Router struct {
	handlers map[string]Handler
}

// NewRouter constructs an empty Router.
func NewRouter() *Router {
	return &Router{handlers: make(map[string]Handler)}
}

// Register associates msgType with h. Registering the same type twice
// overwrites the previous handler.
func (r *Router) Register(msgType string, h Handler) {
	r.handlers[msgType] = h
}

// Lookup returns the handler registered for msgType, if any.
func (r *Router) Lookup(msgType string) (Handler, bool) {
	h, ok := r.handlers[msgType]
	return h, ok
}

// ResultType returns the "<op>.result" response type used for a known
// request type's success envelope.
func ResultType(requestType string) string {
	return requestType + ".result"
}
