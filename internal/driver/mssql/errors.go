package mssql

import (
	"strings"

	"github.com/shhac/agent-sql/internal/errors"
)

func classifyError(err error) error {
	msg := err.Error()

	// Login failed
	if strings.Contains(msg, "Login failed") {
		return errors.New(msg, errors.FixableByHuman).
			WithHint("Login failed. Check the username, password, and database name in your connection config.")
	}

	// Connection refused / network errors
	if strings.Contains(msg, "connection refused") || strings.Contains(msg, "unable to open tcp connection") {
		return errors.New(msg, errors.FixableByHuman).
			WithHint(errors.HintConnectionFailed)
	}

	// Permission denied
	if strings.Contains(msg, "permission") || strings.Contains(msg, "not allowed") {
		return errors.New(msg, errors.FixableByHuman).
			WithHint("Permission denied. Ensure the user has the necessary grants. For read-only, the db_datareader role is recommended.")
	}

	// Object not found
	if strings.Contains(msg, "Invalid object name") {
		return errors.New(msg, errors.FixableByAgent).
			WithHint(errors.HintTableNotFound)
	}

	// Invalid column
	if strings.Contains(msg, "Invalid column name") {
		return errors.New(msg, errors.FixableByAgent).
			WithHint(errors.HintColumnNotFound)
	}

	// Syntax error
	if strings.Contains(msg, "Incorrect syntax") {
		return errors.New(msg, errors.FixableByAgent).
			WithHint("SQL syntax error. MSSQL uses T-SQL syntax which differs from standard SQL in some areas.")
	}

	// Timeout / deadlock
	if strings.Contains(msg, "deadline") || strings.Contains(msg, "context deadline exceeded") {
		return errors.New(msg, errors.FixableByRetry).
			WithHint(errors.HintTimeout)
	}
	if strings.Contains(msg, "deadlock") {
		return errors.New(msg, errors.FixableByRetry).
			WithHint("Transaction deadlock. Retry the query.")
	}

	return errors.Wrap(err, errors.FixableByAgent)
}
