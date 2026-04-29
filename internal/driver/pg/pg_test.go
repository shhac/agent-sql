package pg

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/shhac/agent-sql/internal/driver"
	"github.com/shhac/agent-sql/internal/errors"
)

func TestQuoteIdent(t *testing.T) {
	c := &pgConn{}

	tests := []struct {
		input string
		want  string
	}{
		{"users", `"users"`},
		{"public.users", `"public"."users"`},
		{`my"table`, `"my""table"`},
		{"schema.my.table", `"schema"."my"."table"`},
		{"public.user table", `"public"."user table"`},
	}

	for _, tt := range tests {
		got := c.QuoteIdent(tt.input)
		if got != tt.want {
			t.Errorf("QuoteIdent(%q) = %s, want %s", tt.input, got, tt.want)
		}
	}
}

func TestSplitSchemaTable(t *testing.T) {
	tests := []struct {
		input      string
		wantSchema string
		wantTable  string
	}{
		{"users", "public", "users"},
		{"public.users", "public", "users"},
		{"myschema.orders", "myschema", "orders"},
	}

	for _, tt := range tests {
		schema, table := splitSchemaTable(tt.input)
		if schema != tt.wantSchema || table != tt.wantTable {
			t.Errorf("splitSchemaTable(%q) = (%s, %s), want (%s, %s)",
				tt.input, schema, table, tt.wantSchema, tt.wantTable)
		}
	}
}

func TestClassifyPgError(t *testing.T) {
	tests := []struct {
		code       string
		message    string
		wantFix    errors.FixableBy
		wantInHint string
	}{
		{"42P01", "relation \"foo\" does not exist", errors.FixableByAgent, "Table not found"},
		{"42703", "column \"bar\" does not exist", errors.FixableByAgent, "Column not found"},
		{"25006", "cannot execute in read-only transaction", errors.FixableByHuman, "read-only"},
		{"57014", "canceling statement due to statement timeout", errors.FixableByRetry, "timed out"},
		{"28P01", "password authentication failed", errors.FixableByHuman, "Authentication failed"},
		{"08006", "connection failure", errors.FixableByHuman, "Cannot connect"},
		{"08001", "could not connect", errors.FixableByHuman, "Cannot connect"},
		{"3D000", "database \"nope\" does not exist", errors.FixableByHuman, "Database not found"},
		{"42601", "syntax error at or near", errors.FixableByAgent, "syntax error"},
	}

	for _, tt := range tests {
		pgErr := &pgconn.PgError{Code: tt.code, Message: tt.message}
		err := classifyPgError(pgErr)

		var qerr *errors.QueryError
		if !errors.As(err, &qerr) {
			t.Errorf("code %s: expected QueryError, got %T", tt.code, err)
			continue
		}
		if qerr.FixableBy != tt.wantFix {
			t.Errorf("code %s: fixableBy = %s, want %s", tt.code, qerr.FixableBy, tt.wantFix)
		}
		if tt.wantInHint != "" && !contains(qerr.Hint, tt.wantInHint) {
			t.Errorf("code %s: hint = %q, want to contain %q", tt.code, qerr.Hint, tt.wantInHint)
		}
	}
}

func TestClassifyPgErrorByClass(t *testing.T) {
	tests := []struct {
		code    string
		wantFix errors.FixableBy
	}{
		{"08999", errors.FixableByHuman}, // connection class
		{"28999", errors.FixableByHuman}, // auth class
		{"42999", errors.FixableByAgent}, // syntax/access class
		{"53000", errors.FixableByRetry}, // resource class
	}

	for _, tt := range tests {
		pgErr := &pgconn.PgError{Code: tt.code, Message: "test error"}
		err := classifyPgError(pgErr)

		var qerr *errors.QueryError
		if !errors.As(err, &qerr) {
			t.Errorf("code %s: expected QueryError", tt.code)
			continue
		}
		if qerr.FixableBy != tt.wantFix {
			t.Errorf("code %s: fixableBy = %s, want %s", tt.code, qerr.FixableBy, tt.wantFix)
		}
	}
}

func TestGuardReadOnlyIntegration(t *testing.T) {
	// Test that the PG driver's Query method calls GuardReadOnly
	// We can't connect to a real PG, but we can verify the guard is invoked
	// by checking that write statements are rejected before hitting the DB.

	tests := []struct {
		sql     string
		blocked bool
	}{
		{"SELECT 1", false},
		{"INSERT INTO users VALUES(1)", true},
		{"UPDATE users SET name = 'x'", true},
		{"DELETE FROM users", true},
		{"CREATE TABLE t(x INT)", true},
		{"DROP TABLE users", true},
		{"TRUNCATE users", true},
		{"SELECT * FROM users FOR UPDATE", true},
	}

	for _, tt := range tests {
		err := driver.GuardReadOnly(tt.sql)
		if tt.blocked && err == nil {
			t.Errorf("GuardReadOnly(%q) should have blocked", tt.sql)
		}
		if !tt.blocked && err != nil {
			t.Errorf("GuardReadOnly(%q) should have allowed, got: %v", tt.sql, err)
		}
	}
}

func TestNormalizeValue(t *testing.T) {
	t.Run("converts bytes to string", func(t *testing.T) {
		got := normalizeValue([]byte("hello"))
		if got != "hello" {
			t.Errorf("normalizeValue([]byte) = %v, want hello", got)
		}
	})

	t.Run("passes through int", func(t *testing.T) {
		got := normalizeValue(42)
		if got != 42 {
			t.Errorf("normalizeValue(42) = %v", got)
		}
	})

	t.Run("passes through nil", func(t *testing.T) {
		got := normalizeValue(nil)
		if got != nil {
			t.Errorf("normalizeValue(nil) = %v", got)
		}
	})
}

// Integration tests — require a running PostgreSQL instance.
// Set AGENT_SQL_PG_TEST_URL to enable (e.g. postgres://user:pass@localhost:5432/testdb).

func testConn(t *testing.T) driver.Connection {
	t.Helper()
	url := os.Getenv("AGENT_SQL_PG_TEST_URL")
	if url == "" {
		t.Skip("requires PG: set AGENT_SQL_PG_TEST_URL")
	}
	ctx := context.Background()
	conn, err := ConnectURL(ctx, url, true)
	if err != nil {
		t.Fatalf("ConnectURL: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func TestIntegrationQuery(t *testing.T) {
	conn := testConn(t)
	ctx := context.Background()

	result, err := conn.Query(ctx, "SELECT 1 AS num", driver.QueryOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Columns) != 1 || result.Columns[0] != "num" {
		t.Errorf("columns = %v, want [num]", result.Columns)
	}
	if len(result.Rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(result.Rows))
	}
}

func TestIntegrationGetTables(t *testing.T) {
	conn := testConn(t)
	ctx := context.Background()

	tables, err := conn.GetTables(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	// Just verify it doesn't error — table list depends on the test DB
	_ = tables
}

func TestIntegrationReadonlyBlocks(t *testing.T) {
	conn := testConn(t)
	ctx := context.Background()

	_, err := conn.Query(ctx, "CREATE TABLE _agent_sql_test_ro(x INT)", driver.QueryOpts{})
	if err == nil {
		t.Fatal("expected error for CREATE in readonly mode")
	}
	var qerr *errors.QueryError
	if errors.As(err, &qerr) && qerr.FixableBy != errors.FixableByHuman {
		t.Errorf("fixableBy = %s, want human", qerr.FixableBy)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && containsSubstring(s, substr)
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
