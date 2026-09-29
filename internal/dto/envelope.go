// Package dto defines the wire-format request/response shapes for the
// WebSocket and HTTP transports, plus the structural validation that
// belongs to those shapes (required fields). Business-rule validation
// (bet amount limits, bet type domain rules) lives in the service layer.
package dto

import (
	"encoding/json"
	"time"
)

// Envelope is the WebSocket request envelope: every inbound message has
// this shape regardless of its type.
type Envelope struct {
	Type      string          `json:"type"`
	RequestID string          `json:"requestId"`
	Payload   json.RawMessage `json:"payload"`
}

// SuccessResponse is the success response envelope shared by both the
// WebSocket and HTTP (JSON body) transports.
type SuccessResponse struct {
	Type      string    `json:"type"`
	RequestID string    `json:"requestId"`
	Success   bool      `json:"success"`
	Data      any       `json:"data"`
	Timestamp time.Time `json:"timestamp"`
}

// NewSuccessResponse builds a SuccessResponse with the current timestamp.
func NewSuccessResponse(msgType, requestID string, data any) SuccessResponse {
	return SuccessResponse{
		Type:      msgType,
		RequestID: requestID,
		Success:   true,
		Data:      data,
		Timestamp: time.Now().UTC(),
	}
}

// ErrorResponse is the error response envelope shared by both transports.
// RequestID is a pointer because it may be null when the request could
// not be parsed at all.
type ErrorResponse struct {
	Type      string      `json:"type"`
	RequestID *string     `json:"requestId"`
	Success   bool        `json:"success"`
	Error     ErrorDetail `json:"error"`
	Timestamp time.Time   `json:"timestamp"`
}

// NewErrorResponse builds an ErrorResponse with the current timestamp.
func NewErrorResponse(requestID *string, detail ErrorDetail) ErrorResponse {
	return ErrorResponse{
		Type:      "error",
		RequestID: requestID,
		Success:   false,
		Error:     detail,
		Timestamp: time.Now().UTC(),
	}
}
