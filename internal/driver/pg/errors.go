package pg

import (
	stderrors "errors"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/shhac/agent-sql/internal/errors"
)

func classifyError(err error) error {
	if err == nil {
		return nil
	}

	var pgErr *pgconn.PgError
	if stderrors.As(err, &pgErr) {
		return classifyPgError(pgErr)
	}

	msg := err.Error()

	// Connection errors
	if strings.Contains(msg, "connect") && (strings.Contains(msg, "refused") || strings.Contains(msg, "timeout")) {
		return errors.New(msg, errors.FixableByHuman).
			WithHint(errors.HintConnectionFailed)
	}
	if strings.Contains(msg, "password authentication failed") {
		return errors.New(msg, errors.FixableByHuman).
			WithHint(errors.HintAuthFailed)
	}

	return errors.Wrap(err, errors.FixableByAgent)
}

func classifyPgError(pgErr *pgconn.PgError) error {
	code := pgErr.Code
	msg := pgErr.Message

	switch code {
	case "42P01": // undefined_table
		return errors.New(msg, errors.FixableByAgent).
			WithHint(errors.HintTableNotFound)
	case "42703": // undefined_column
		return errors.New(msg, errors.FixableByAgent).
			WithHint(errors.HintColumnNotFound)
	case "42601": // syntax_error
		return errors.New(msg, errors.FixableByAgent).
			WithHint("SQL syntax error. Check the query syntax.")
	case "25006": // read_only_sql_transaction
		return errors.New(msg, errors.FixableByHuman).
			WithHint(errors.HintReadOnly)
	case "57014": // query_canceled (timeout)
		return errors.New(msg, errors.FixableByRetry).
			WithHint(errors.HintTimeout)
	case "28P01": // invalid_password
		return errors.New(msg, errors.FixableByHuman).
			WithHint(errors.HintAuthFailed)
	case "28000": // invalid_authorization_specification
		return errors.New(msg, errors.FixableByHuman).
			WithHint("Authorization failed. Check the username and permissions.")
	case "08006", "08001": // connection_failure, sqlclient_unable_to_establish_sqlconnection
		return errors.New(msg, errors.FixableByHuman).
			WithHint(errors.HintConnectionFailed)
	case "3D000": // invalid_catalog_name (database not found)
		return errors.New(msg, errors.FixableByHuman).
			WithHint("Database not found. Check the database name.")
	case "42P07": // duplicate_table
		return errors.New(msg, errors.FixableByAgent).
			WithHint("Table already exists.")
	}

	// Fall back to error class (first two characters of SQLSTATE)
	switch code[:2] {
	case "08": // connection exception
		return errors.New(msg, errors.FixableByHuman).
			WithHint("Connection error. Check the server status and connection details.")
	case "28": // invalid authorization
		return errors.New(msg, errors.FixableByHuman).
			WithHint("Authentication or authorization failed.")
	case "42": // syntax error or access rule violation
		return errors.New(msg, errors.FixableByAgent).
			WithHint("SQL error. Check the query syntax and referenced objects.")
	case "53": // insufficient resources
		return errors.New(msg, errors.FixableByRetry).
			WithHint("Server resource issue. Try again shortly.")
	}

	return errors.New(msg, errors.FixableByAgent)
}
