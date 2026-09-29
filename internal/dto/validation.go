package dto

// Validator is implemented by request DTOs that need structural validation
// (e.g. required fields) beyond what JSON decoding already enforces.
// Business-rule validation (bet limits, bet type) is the service layer's
// responsibility, not the DTO's.
type Validator interface {
	Validate() error
}
