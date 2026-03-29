package duckdb

import (
	"strings"

	"github.com/shhac/agent-sql/internal/errors"
)

func classifyError(message string) error {
	firstLine := message
	if idx := strings.Index(message, "\n"); idx >= 0 {
		firstLine = message[:idx]
	}

	if strings.Contains(firstLine, "Catalog Error") {
		hint := errors.HintTableNotFound
		if strings.Contains(message, "Did you mean") {
			for _, line := range strings.Split(message, "\n") {
				if strings.Contains(line, "Did you mean") {
					hint = strings.TrimSpace(line)
					break
				}
			}
		}
		return errors.New(firstLine, errors.FixableByAgent).WithHint(hint)
	}

	if strings.Contains(firstLine, "Parser Error") {
		return errors.New(firstLine, errors.FixableByAgent)
	}

	if strings.Contains(firstLine, "read-only mode") || strings.Contains(firstLine, "Permission Error") {
		return errors.New(firstLine, errors.FixableByHuman).
			WithHint(errors.HintReadOnly)
	}

	if strings.Contains(firstLine, "IO Error") {
		return errors.New(firstLine, errors.FixableByAgent)
	}

	return errors.New(firstLine, errors.FixableByAgent)
}
