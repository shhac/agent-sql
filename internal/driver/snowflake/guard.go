package snowflake

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/shhac/agent-sql/internal/errors"
)

// readOnlyAllowed lists statement types permitted in read-only mode.
var readOnlyAllowed = []string{
	"SELECT", "WITH", "SHOW", "DESCRIBE", "DESC", "EXPLAIN", "LIST", "LS",
}

// ValidateReadOnly checks if a SQL statement is allowed in read-only mode.
// Exported for testing.
func ValidateReadOnly(sqlStr string) error {
	return validateReadOnly(sqlStr)
}

func validateReadOnly(sqlStr string) error {
	trimmed := strings.TrimLeftFunc(sqlStr, unicode.IsSpace)
	upper := strings.ToUpper(trimmed)

	for _, keyword := range readOnlyAllowed {
		if strings.HasPrefix(upper, keyword) &&
			(len(trimmed) == len(keyword) || trimmed[len(keyword)] == ' ' ||
				trimmed[len(keyword)] == '\t' || trimmed[len(keyword)] == '\n' ||
				trimmed[len(keyword)] == '(') {
			return nil
		}
	}

	firstWord := upper
	if idx := strings.IndexFunc(upper, unicode.IsSpace); idx > 0 {
		firstWord = upper[:idx]
	}

	return errors.New(
		fmt.Sprintf("Statement type '%s' is not allowed in read-only mode. Allowed: SELECT, SHOW, DESCRIBE, EXPLAIN.", firstWord),
		errors.FixableByHuman,
	).WithHint("To execute write operations, use a connection with a write-enabled credential and pass --write.")
}
