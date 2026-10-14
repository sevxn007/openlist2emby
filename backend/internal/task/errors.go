package task

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
)

var (
	ErrNotFound        = errors.New("task record not found")
	ErrConflict        = errors.New("task state conflict")
	ErrTypeUnavailable = errors.New("task type is unavailable")
	ErrNotManual       = errors.New("task type does not support manual runs")
	ErrNotSchedulable  = errors.New("task type does not support schedules")
	ErrNotCancelable   = errors.New("task type cannot be canceled")
	ErrInvalidSchedule = errors.New("invalid task schedule")
	ErrRunActive       = errors.New("running task run cannot be deleted")
	ErrRunCanceled     = errors.New("task run canceled")
	ErrManagerStopping = errors.New("task manager is stopping")
)

type PublicError struct {
	Code      string
	Message   string
	Status    int
	Retryable bool
	Cause     error
}

func (e *PublicError) Error() string {
	return e.Message
}

func (e *PublicError) Unwrap() error {
	return e.Cause
}

func (e *PublicError) StatusCode() int {
	return e.Status
}

func (e *PublicError) PublicMessage() string {
	return e.Message
}

func NewPublicError(code, message string, status int, retryable bool, cause error) error {
	return &PublicError{
		Code:      code,
		Message:   message,
		Status:    status,
		Retryable: retryable,
		Cause:     cause,
	}
}

func ErrorDetails(err error) (code, message string, status int, retryable bool) {
	if err == nil {
		return "", "", 0, false
	}
	var publicErr *PublicError
	if errors.As(err, &publicErr) {
		return publicErr.Code, publicErr.Message, normalizeErrorStatus(publicErr.Status), publicErr.Retryable
	}
	var messageErr interface{ PublicMessage() string }
	if errors.As(err, &messageErr) && messageErr.PublicMessage() != "" {
		message = messageErr.PublicMessage()
	} else {
		message = err.Error()
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, ErrRunCanceled) {
		return "canceled", "task run canceled", http.StatusConflict, false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout", message, http.StatusGatewayTimeout, true
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return "network_error", message, http.StatusBadGateway, true
	}
	var statusErr interface{ StatusCode() int }
	if errors.As(err, &statusErr) {
		status = normalizeErrorStatus(statusErr.StatusCode())
		return fmt.Sprintf("upstream_%d", status), message, status,
			status == http.StatusRequestTimeout || status == http.StatusTooManyRequests || status >= 500
	}
	return "task_failed", message, http.StatusInternalServerError, false
}

func normalizeErrorStatus(status int) int {
	if status < 400 || status > 599 {
		return http.StatusInternalServerError
	}
	return status
}
