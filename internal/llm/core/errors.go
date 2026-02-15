package core

import "errors"

var (
	// ErrInvalidRequest indicates missing or malformed provider request input.
	ErrInvalidRequest = errors.New("invalid llm request")
	// ErrMissingAPIKey indicates missing provider API key.
	ErrMissingAPIKey = errors.New("missing api key")
)
