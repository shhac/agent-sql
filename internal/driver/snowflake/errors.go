package snowflake

import (
	"fmt"
	"strings"

	"github.com/shhac/agent-sql/internal/errors"
)

type snowflakeAPIError struct {
	Code     string
	Msg      string
	SQLState string
}

func (e *snowflakeAPIError) Error() string {
	if e.SQLState != "" {
		return fmt.Sprintf("Snowflake error %s (SQLState %s): %s", e.Code, e.SQLState, e.Msg)
	}
	return fmt.Sprintf("Snowflake error %s: %s", e.Code, e.Msg)
}

// ClassifyError classifies a Snowflake error. Exported for testing.
func ClassifyError(err error) error {
	return classifyError(err)
}

func classifyError(err error) error {
	if err == nil {
		return nil
	}

	// Already classified
	var qerr *errors.QueryError
	if errors.As(err, &qerr) {
		return qerr
	}

	msg := err.Error()

	var apiErr *snowflakeAPIError
	if asAPIError(err, &apiErr) {
		return classifyAPIError(apiErr)
	}

	// Generic message-based classification
	if strings.Contains(msg, "does not exist or not authorized") {
		return errors.New(msg, errors.FixableByAgent).
			WithHint(errors.HintTableNotFound)
	}
	if strings.Contains(msg, "Authentication") || strings.Contains(msg, "Unauthorized") {
		return errors.New(msg, errors.FixableByHuman).
			WithHint(errors.HintAuthFailed)
	}

	return errors.Wrap(err, errors.FixableByAgent)
}

func classifyAPIError(apiErr *snowflakeAPIError) error {
	msg := apiErr.Error()

	switch {
	case strings.Contains(apiErr.Msg, "does not exist or not authorized"):
		return errors.New(msg, errors.FixableByAgent).
			WithHint(errors.HintTableNotFound)

	case apiErr.Code == "000606" || strings.Contains(apiErr.Msg, "No active warehouse"):
		return errors.New(msg, errors.FixableByHuman).
			WithHint("No active warehouse selected. Set a warehouse in your connection config.")

	case strings.Contains(apiErr.Msg, "Insufficient privileges"):
		return errors.New(msg, errors.FixableByHuman).
			WithHint("Insufficient permissions. Ask your Snowflake admin for the required role/grants.")

	case apiErr.SQLState == "42000" || apiErr.SQLState == "42601":
		return errors.New(msg, errors.FixableByAgent).
			WithHint("SQL syntax error. Check your query syntax.")

	case apiErr.SQLState == "42S02":
		return errors.New(msg, errors.FixableByAgent).
			WithHint(errors.HintTableNotFound)

	case apiErr.SQLState == "42S22":
		return errors.New(msg, errors.FixableByAgent).
			WithHint(errors.HintColumnNotFound)

	case apiErr.Code == "390318" || strings.Contains(apiErr.Msg, "Authentication token has expired"):
		return errors.New(msg, errors.FixableByHuman).
			WithHint("PAT token has expired. Generate a new token in Snowflake.")

	case strings.Contains(apiErr.Msg, "timeout") || strings.Contains(apiErr.Msg, "Timeout"):
		return errors.New(msg, errors.FixableByRetry).
			WithHint(errors.HintTimeout)
	}

	return errors.New(msg, errors.FixableByAgent)
}

func asAPIError(err error, target **snowflakeAPIError) bool {
	if ae, ok := err.(*snowflakeAPIError); ok {
		*target = ae
		return true
	}
	return false
}
