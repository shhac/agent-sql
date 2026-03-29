package driver

import (
	"testing"
)

func TestGuardReadOnlyAllowsReadStatements(t *testing.T) {
	allowed := []string{
		"SELECT * FROM users",
		"select id from orders",
		"EXPLAIN SELECT * FROM users",
		"explain analyze select 1",
		"SHOW TABLES",
		"show databases",
		"WITH cte AS (SELECT 1) SELECT * FROM cte",
		"  SELECT 1",         // leading whitespace
		"\t\nSELECT 1",      // leading tabs/newlines
		"SELECT 1 FROM dual", // simple select
	}

	for _, sql := range allowed {
		t.Run(sql, func(t *testing.T) {
			if err := GuardReadOnly(sql); err != nil {
				t.Errorf("GuardReadOnly(%q) should allow, got error: %v", sql, err)
			}
		})
	}
}

func TestGuardReadOnlyBlocksWriteStatements(t *testing.T) {
	blocked := []struct {
		name string
		sql  string
	}{
		{"INSERT", "INSERT INTO users (name) VALUES ('alice')"},
		{"UPDATE", "UPDATE users SET name = 'bob'"},
		{"DELETE", "DELETE FROM users WHERE id = 1"},
		{"CREATE", "CREATE TABLE test (id INT)"},
		{"DROP", "DROP TABLE users"},
		{"ALTER", "ALTER TABLE users ADD COLUMN age INT"},
		{"TRUNCATE", "TRUNCATE TABLE users"},
		{"MERGE", "MERGE INTO t1 USING t2 ON t1.id = t2.id"},
		{"GRANT", "GRANT SELECT ON users TO reader"},
		{"REVOKE", "REVOKE SELECT ON users FROM reader"},
	}

	for _, tt := range blocked {
		t.Run(tt.name, func(t *testing.T) {
			err := GuardReadOnly(tt.sql)
			if err == nil {
				t.Errorf("GuardReadOnly(%q) should block, got nil", tt.sql)
			}
		})
	}
}

func TestGuardReadOnlyBlocksSelectInto(t *testing.T) {
	err := GuardReadOnly("SELECT * INTO new_table FROM users")
	if err == nil {
		t.Fatal("SELECT INTO should be blocked")
	}
}

func TestGuardReadOnlyBlocksForUpdate(t *testing.T) {
	tests := []string{
		"SELECT * FROM users FOR UPDATE",
		"SELECT * FROM users FOR SHARE",
		"SELECT * FROM users FOR NO KEY UPDATE",
		"WITH cte AS (SELECT 1) SELECT * FROM cte FOR UPDATE",
	}

	for _, sql := range tests {
		t.Run(sql, func(t *testing.T) {
			err := GuardReadOnly(sql)
			if err == nil {
				t.Errorf("GuardReadOnly(%q) should block FOR UPDATE/SHARE", sql)
			}
		})
	}
}

func TestGuardReadOnlyCaseInsensitive(t *testing.T) {
	tests := []struct {
		sql     string
		blocked bool
	}{
		{"insert into users values (1)", true},
		{"INSERT INTO users values (1)", true},
		{"Insert Into users values (1)", true},
		{"select 1", false},
		{"SELECT 1", false},
		{"Select 1", false},
	}

	for _, tt := range tests {
		t.Run(tt.sql, func(t *testing.T) {
			err := GuardReadOnly(tt.sql)
			if tt.blocked && err == nil {
				t.Errorf("expected %q to be blocked", tt.sql)
			}
			if !tt.blocked && err != nil {
				t.Errorf("expected %q to be allowed, got: %v", tt.sql, err)
			}
		})
	}
}

// TestGuardReadOnlyBypassVectors documents known limitations of the keyword guard.
// The guard checks only the first word of the statement. Multi-statement and CTE
// bypasses are mitigated by server-side enforcement:
//   - PG/CockroachDB: BEGIN READ ONLY per query
//   - MySQL/MariaDB: START TRANSACTION READ ONLY per query
//   - SQLite: SQLITE_OPEN_READONLY at OS level
//   - DuckDB: -readonly CLI flag at engine level
//   - MSSQL: db_datareader role recommended for production
//   - Snowflake: client-side allowlist + MULTI_STATEMENT_COUNT=1
func TestGuardReadOnlyBypassVectors(t *testing.T) {
	t.Run("SQL comment before SELECT passes guard", func(t *testing.T) {
		// The guard sees "--" as the first word (not a write command), so it allows.
		// This is correct: the actual statement after the comment is a SELECT.
		err := GuardReadOnly("-- DROP TABLE\nSELECT 1")
		if err != nil {
			t.Errorf("comment before SELECT should be allowed, got: %v", err)
		}
	})

	t.Run("multi-statement via semicolon not caught by guard", func(t *testing.T) {
		// The guard only checks the first word ("SELECT"), so the second statement
		// is not caught. Server-side enforcement (BEGIN READ ONLY, etc.) prevents
		// the write from executing.
		err := GuardReadOnly("SELECT 1; DROP TABLE x")
		if err != nil {
			t.Errorf("guard only checks first word, got: %v", err)
		}
	})

	t.Run("CTE-wrapped write not caught by guard", func(t *testing.T) {
		// First word is "WITH" (allowed), so the DELETE inside the CTE is not
		// caught by the keyword guard. Server-side enforcement prevents execution.
		err := GuardReadOnly("WITH x AS (DELETE FROM t RETURNING *) SELECT * FROM x")
		if err != nil {
			t.Errorf("guard only checks first word, got: %v", err)
		}
	})
}

func TestGuardReadOnlyWhitespaceHandling(t *testing.T) {
	tests := []struct {
		sql     string
		blocked bool
	}{
		{"  SELECT 1", false},
		{"\tSELECT 1", false},
		{"\n\nSELECT 1", false},
		{"  INSERT INTO t VALUES (1)", true},
		{"\tDELETE FROM t", true},
	}

	for _, tt := range tests {
		t.Run(tt.sql, func(t *testing.T) {
			err := GuardReadOnly(tt.sql)
			if tt.blocked && err == nil {
				t.Errorf("expected blocked for %q", tt.sql)
			}
			if !tt.blocked && err != nil {
				t.Errorf("expected allowed for %q, got: %v", tt.sql, err)
			}
		})
	}
}
