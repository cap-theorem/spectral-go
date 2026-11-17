package spectral

import (
	"errors"
	"fmt"
	"net/http"
)

var (
	ErrInvalidRegistration = errors.New("invalid registration")
	ErrAlreadyRegistered   = errors.New("already registered")
	ErrServiceNotFound     = errors.New("service not found")
	ErrLeaseExpired        = errors.New("lease expired")
	ErrSidecarTimeout      = errors.New("sidecar timed out")
	ErrSidecarUnavailable  = errors.New("sidecar unavailable")
)

type APIError struct {
	Op         string
	StatusCode int
	Message    string
	Err        error
}

func (e *APIError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("spectral %s: http %d", e.Op, e.StatusCode)
	}
	return fmt.Sprintf("spectral %s: http %d: %s", e.Op, e.StatusCode, e.Message)
}

func (e *APIError) Unwrap() error {
	return e.Err
}

func errorKind(op string, status int) error {
	switch {
	case status == http.StatusGatewayTimeout:
		return ErrSidecarTimeout
	case op == opRegister && status == http.StatusBadRequest:
		return ErrInvalidRegistration
	case op == opRegister && status == http.StatusConflict:
		return ErrAlreadyRegistered
	case op == opRenew && status == http.StatusNotFound:
		return ErrLeaseExpired
	case op == opLookup && status == http.StatusNotFound:
		return ErrServiceNotFound
	}
	return nil
}
