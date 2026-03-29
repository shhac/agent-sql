// Package errors provides error classification for database errors.
package errors

import (
	"fmt"
	"strings"
)

// FixableBy indicates who can fix an error.
type FixableBy string

const (
	FixableByAgent FixableBy = "agent"
	FixableByHuman FixableBy = "human"
	FixableByRetry FixableBy = "retry"
)

// QueryError is a classified database error with context for LLMs.
type QueryError struct {
	Message   string    `json:"error"`
	Hint      string    `json:"hint,omitempty"`
	FixableBy FixableBy `json:"fixable_by"`
	Cause     error     `json:"-"`
}

func (e *QueryError) Error() string { return e.Message }
func (e *QueryError) Unwrap() error { return e.Cause }

// New creates a QueryError.
func New(message string, fixableBy FixableBy) *QueryError {
	return &QueryError{Message: message, FixableBy: fixableBy}
}

// Wrap creates a QueryError wrapping an underlying error.
func Wrap(err error, fixableBy FixableBy) *QueryError {
	return &QueryError{Message: err.Error(), FixableBy: fixableBy, Cause: err}
}

// WithHint adds a hint to a QueryError.
func (e *QueryError) WithHint(hint string) *QueryError {
	e.Hint = hint
	return e
}

// ErrorContext provides context for error classification.
type ErrorContext struct {
	ConnectionAlias      string
	AvailableTables      []string
	AvailableConnections []string
}

// Classifier is a function that attempts to classify a database error.
// Returns nil if the error is not recognized.
type Classifier func(err error, ctx ErrorContext) *QueryError

// Classify attempts to classify an error using the registered classifiers.
// If the error already has a FixableBy field (pre-classified by a driver),
// it is returned as-is.
func Classify(err error, ctx ErrorContext) *QueryError {
	// Pre-classified errors pass through
	var qerr *QueryError
	if As(err, &qerr) {
		return qerr
	}

	for _, c := range classifiers {
		if result := c(err, ctx); result != nil {
			return result
		}
	}

	return Wrap(err, FixableByAgent)
}

// As is a convenience wrapper for errors.As with QueryError.
func As(err error, target **QueryError) bool {
	if err == nil {
		return false
	}
	if qe, ok := err.(*QueryError); ok {
		*target = qe
		return true
	}
	return false
}

// RegisterClassifier adds a classifier to the chain.
func RegisterClassifier(c Classifier) {
	classifiers = append(classifiers, c)
}

var classifiers []Classifier

// NotFound creates a "not found" error for connections.
func NotFound(alias string, available []string) *QueryError {
	listing := "(none configured)"
	if len(available) > 0 {
		listing = strings.Join(available, ", ")
	}
	return New(
		fmt.Sprintf("Unknown connection '%s'. Available connections: %s. Tip: -c also accepts file paths (e.g. ./data.db) and connection URLs (e.g. postgres://user:pass@host/db).", alias, listing),
		FixableByAgent,
	)
}
