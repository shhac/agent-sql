package sqlite

import (
	stderrors "errors"
	"strings"

	"github.com/shhac/agent-sql/internal/errors"
)

func classifyError(err error) error {
	if err == nil {
		return nil
	}

	// Already classified -- pass through unchanged so re-wrapping
	// doesn't lose the original FixableBy classification.
	var qerr *errors.QueryError
	if stderrors.As(err, &qerr) {
		return qerr
	}

	msg := err.Error()
	if strings.Contains(msg, "attempt to write a readonly database") {
		return errors.New(msg, errors.FixableByHuman).
			WithHint(errors.HintReadOnly)
	}
	if strings.Contains(msg, "database is locked") {
		return errors.New(msg, errors.FixableByRetry).
			WithHint("The database is locked by another process. Try again shortly.")
	}
	if strings.Contains(msg, "no such table") {
		return errors.New(msg, errors.FixableByAgent).
			WithHint(errors.HintTableNotFound)
	}
	if strings.Contains(msg, "no such column") {
		return errors.New(msg, errors.FixableByAgent).
			WithHint(errors.HintColumnNotFound)
	}
	return errors.Wrap(err, errors.FixableByAgent)
}
