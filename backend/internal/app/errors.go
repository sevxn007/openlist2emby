package app

import (
	"errors"
	"net/http"
	"strings"

	"github.com/sevxn007/openlist2emby/backend/internal/domain"
	"github.com/sevxn007/openlist2emby/backend/internal/metadata"
)

type requestError struct {
	status  int
	message string
	err     error
}

func (e *requestError) Error() string {
	return e.message
}

func (e *requestError) Unwrap() error {
	return e.err
}

func (e *requestError) StatusCode() int {
	return e.status
}

func (e *requestError) PublicMessage() string {
	return e.message
}

type mediaError struct {
	err    error
	secret string
}

func (e *mediaError) Error() string {
	return e.err.Error()
}

func (e *mediaError) Unwrap() error {
	return e.err
}

func (e *mediaError) StatusCode() int {
	return StatusCode(e.err, http.StatusBadGateway)
}

func (e *mediaError) PublicMessage() string {
	message := PublicMessage(e.err)
	if e.secret != "" {
		message = strings.ReplaceAll(message, e.secret, "[redacted]")
	}
	return message
}

func wrapMediaError(err error, secret string) error {
	if err == nil {
		return nil
	}
	return &mediaError{err: err, secret: secret}
}

func badRequest(message string) error {
	return &requestError{status: http.StatusBadRequest, message: message}
}

func unauthorized(message string) error {
	return &requestError{status: http.StatusUnauthorized, message: message}
}

func unsupportedMediaType(message string) error {
	return &requestError{status: http.StatusUnsupportedMediaType, message: message, err: domain.ErrUnsupportedAudio}
}

func unprocessable(message string) error {
	return &requestError{status: http.StatusUnprocessableEntity, message: message}
}

func unprocessableWithCause(message string, err error) error {
	return &requestError{status: http.StatusUnprocessableEntity, message: message, err: err}
}

type statusError interface {
	StatusCode() int
}

type publicMessageError interface {
	PublicMessage() string
}

func StatusCode(err error, fallback int) int {
	if err == nil {
		return fallback
	}
	var statusErr statusError
	if errors.As(err, &statusErr) && statusErr.StatusCode() > 0 {
		return statusErr.StatusCode()
	}
	if errors.Is(err, metadata.ErrInvalidSource) || errors.Is(err, metadata.ErrNotConfigured) || errors.Is(err, metadata.ErrNoMatch) {
		return http.StatusBadRequest
	}
	if errors.Is(err, metadata.ErrAuthentication) || errors.Is(err, metadata.ErrUnavailable) {
		return http.StatusBadGateway
	}
	if errors.Is(err, metadata.ErrRateLimited) {
		return http.StatusTooManyRequests
	}
	if errors.Is(err, domain.ErrAlreadySetup) {
		return http.StatusConflict
	}
	if errors.Is(err, domain.ErrNotSetup) {
		return http.StatusPreconditionRequired
	}
	if errors.Is(err, domain.ErrNotFound) {
		return http.StatusNotFound
	}
	if errors.Is(err, domain.ErrTMDBKeyNotConfigured) {
		return http.StatusBadRequest
	}
	if errors.Is(err, domain.ErrUnsupportedAudio) {
		return http.StatusUnsupportedMediaType
	}
	return fallback
}

func PublicMessage(err error) string {
	if err == nil {
		return ""
	}
	var messageErr publicMessageError
	if errors.As(err, &messageErr) && messageErr.PublicMessage() != "" {
		return messageErr.PublicMessage()
	}
	return err.Error()
}
