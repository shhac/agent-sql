package errors

// Standard hint strings used across drivers for consistent error messages.
const (
	HintTableNotFound    = "Table not found. Use 'schema tables' to see available tables."
	HintColumnNotFound   = "Column not found. Use 'schema describe <table>' to see available columns."
	HintReadOnly         = "This connection is read-only. To enable writes, use a credential with writePermission and pass --write."
	HintConnectionFailed = "Cannot connect. Check that the host and port are correct and the server is running."
	HintAuthFailed       = "Authentication failed. Check the username and password."
	HintTimeout          = "Query timed out. Increase with --timeout <ms> or 'config set query.timeout <ms>'."
)
