package mysql

import (
	stderrors "errors"
	"strings"

	gomysql "github.com/go-sql-driver/mysql"
	"github.com/shhac/agent-sql/internal/errors"
)

func classifyError(err error) error {
	if err == nil {
		return nil
	}

	// Check for MySQL-specific error numbers
	var mysqlErr *gomysql.MySQLError
	if stderrors.As(err, &mysqlErr) {
		return classifyMySQLError(mysqlErr)
	}

	msg := err.Error()
	return classifyByMessage(msg, err)
}

func classifyMySQLError(e *gomysql.MySQLError) error {
	switch e.Number {
	case 1792: // ER_CANT_EXECUTE_IN_READ_ONLY_TRANSACTION
		return errors.New(e.Message, errors.FixableByHuman).
			WithHint(errors.HintReadOnly)
	case 1146: // ER_NO_SUCH_TABLE
		return errors.New(e.Message, errors.FixableByAgent).
			WithHint(errors.HintTableNotFound)
	case 1054: // ER_BAD_FIELD_ERROR
		return errors.New(e.Message, errors.FixableByAgent).
			WithHint(errors.HintColumnNotFound)
	case 2002, 2003: // CR_CONNECTION_ERROR, CR_CONN_HOST_ERROR
		return errors.New(e.Message, errors.FixableByHuman).
			WithHint(errors.HintConnectionFailed)
	case 1045: // ER_ACCESS_DENIED_ERROR
		return errors.New(e.Message, errors.FixableByHuman).
			WithHint(errors.HintAuthFailed)
	default:
		return errors.Wrap(e, errors.FixableByAgent)
	}
}

func classifyByMessage(msg string, cause error) error {
	switch {
	case strings.Contains(msg, "connection refused"):
		return errors.New(msg, errors.FixableByHuman).
			WithHint(errors.HintConnectionFailed)
	case strings.Contains(msg, "Access denied"):
		return errors.New(msg, errors.FixableByHuman).
			WithHint(errors.HintAuthFailed)
	default:
		return errors.Wrap(cause, errors.FixableByAgent)
	}
}
