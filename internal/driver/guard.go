package driver

import (
	"strings"

	"github.com/shhac/agent-sql/internal/errors"
)

// GuardReadOnly validates that a SQL statement is read-only using keyword matching.
// Used by PG, CockroachDB, and MSSQL drivers as defense-in-depth alongside
// server-side enforcement (BEGIN READ ONLY, db_datareader role, etc.).
func GuardReadOnly(sql string) error {
	cmd := DetectCommand(sql, WriteCommands)
	if cmd != "" {
		return errors.New(
			"Statement blocked: "+cmd+" is not allowed in read-only mode.",
			errors.FixableByHuman,
		).WithHint("This connection is read-only. To enable writes, use a credential with writePermission and pass --write.")
	}

	upper := strings.ToUpper(strings.TrimSpace(sql))

	// Block SELECT INTO (creates a table)
	if strings.Contains(upper, "INTO") && strings.HasPrefix(upper, "SELECT") {
		// Check for SELECT ... INTO ... FROM pattern (not INSERT INTO)
		selectIdx := 0
		intoIdx := strings.Index(upper, "INTO")
		fromIdx := strings.Index(upper, "FROM")
		if intoIdx > selectIdx && (fromIdx < 0 || intoIdx < fromIdx) {
			return errors.New(
				"Statement blocked: SELECT INTO is not allowed in read-only mode.",
				errors.FixableByHuman,
			).WithHint("SELECT INTO creates a new table. Use a regular SELECT instead.")
		}
	}

	// Block SELECT ... FOR UPDATE/SHARE
	if strings.HasPrefix(upper, "SELECT") || strings.HasPrefix(upper, "WITH") {
		if strings.Contains(upper, "FOR UPDATE") || strings.Contains(upper, "FOR SHARE") || strings.Contains(upper, "FOR NO KEY UPDATE") {
			return errors.New(
				"Statement blocked: FOR UPDATE/SHARE is not allowed in read-only mode.",
				errors.FixableByHuman,
			).WithHint("Locking clauses require write access. Remove the FOR UPDATE/SHARE clause.")
		}
	}

	return nil
}
