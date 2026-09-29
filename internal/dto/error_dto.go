package dto

// ErrorDetail is the machine-readable error payload embedded in an
// ErrorResponse: a stable Code (see domain.ErrorCode), a human-readable
// Message, and optional structured Details (e.g. the offending field).
type ErrorDetail struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}
