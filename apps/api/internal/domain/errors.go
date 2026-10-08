// Package domain holds IdeaVault's core entities, enums and invariants.
// It has no dependencies on storage, transport or AI providers.
package domain

import (
	"errors"
	"fmt"
)

// ErrorKind classifies domain errors so transports can map them to status codes.
type ErrorKind string

const (
	KindNotFound     ErrorKind = "not_found"
	KindInvalid      ErrorKind = "invalid"
	KindConflict     ErrorKind = "conflict"
	KindForbidden    ErrorKind = "forbidden"
	KindUnauthorized ErrorKind = "unauthorized"
	KindImmutable    ErrorKind = "immutable"
	KindRateLimited  ErrorKind = "rate_limited"
	KindUnavailable  ErrorKind = "unavailable"
)

// Error is a classified, user-presentable domain error.
type Error struct {
	Kind    ErrorKind `json:"kind"`
	Message string    `json:"message"`
	Field   string    `json:"field,omitempty"`
}

func (e *Error) Error() string {
	if e.Field != "" {
		return fmt.Sprintf("%s: %s (%s)", e.Kind, e.Message, e.Field)
	}
	return fmt.Sprintf("%s: %s", e.Kind, e.Message)
}

// NotFound returns a not-found error for an entity name.
func NotFound(entity string) error {
	return &Error{Kind: KindNotFound, Message: entity + " not found"}
}

// Invalid returns a validation error for a field.
func Invalid(field, msg string) error {
	return &Error{Kind: KindInvalid, Message: msg, Field: field}
}

// Conflict returns a conflict error.
func Conflict(msg string) error { return &Error{Kind: KindConflict, Message: msg} }

// Forbidden returns a forbidden error.
func Forbidden(msg string) error { return &Error{Kind: KindForbidden, Message: msg} }

// Unauthorized returns an authentication error.
func Unauthorized(msg string) error { return &Error{Kind: KindUnauthorized, Message: msg} }

// Immutable returns an error for attempts to rewrite history.
func Immutable(msg string) error { return &Error{Kind: KindImmutable, Message: msg} }

// RateLimited returns a rate-limit error.
func RateLimited(msg string) error { return &Error{Kind: KindRateLimited, Message: msg} }

// Unavailable returns an error for a dependency that is not configured or reachable.
func Unavailable(msg string) error { return &Error{Kind: KindUnavailable, Message: msg} }

// KindOf returns the ErrorKind of err, or "" if err is not a domain error.
func KindOf(err error) ErrorKind {
	var de *Error
	if errors.As(err, &de) {
		return de.Kind
	}
	return ""
}

// IsNotFound reports whether err is a not-found domain error.
func IsNotFound(err error) bool { return KindOf(err) == KindNotFound }
