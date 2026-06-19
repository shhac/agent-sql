package errors

import (
	"fmt"
	"strings"
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

// ConnTip is the shared "ad-hoc connections" hint appended to connection
// lookup failures. It's defined once so the unknown-connection and
// no-connection-specified messages stay in sync across this package and the
// resolve package.
const ConnTip = "Tip: -c also accepts file paths (e.g. ./data.db) and connection URLs (e.g. postgres://user:pass@host/db)."

// NotFound creates a "not found" error for connections.
func NotFound(alias string, available []string) *QueryError {
	listing := "(none configured)"
	if len(available) > 0 {
		listing = strings.Join(available, ", ")
	}
	return New(
		fmt.Sprintf("Unknown connection '%s'. Available connections: %s. %s", alias, listing, ConnTip),
		FixableByAgent,
	)
}
