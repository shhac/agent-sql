package mssql

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/shhac/agent-sql/internal/driver"
	"github.com/shhac/agent-sql/internal/errors"
)

func TestQuoteIdent(t *testing.T) {
	conn := &mssqlConn{}

	tests := []struct {
		input string
		want  string
	}{
		{"users", "[users]"},
		{"dbo.users", "[dbo].[users]"},
		{"my table", "[my table]"},
		{"col]name", "[col]]name]"},
		{"schema.ta]ble", "[schema].[ta]]ble]"},
		{"a.b.c", "[a].[b].[c]"},
	}

	for _, tt := range tests {
		got := conn.QuoteIdent(tt.input)
		if got != tt.want {
			t.Errorf("QuoteIdent(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestGuardReadOnly(t *testing.T) {
	tests := []struct {
		sql     string
		blocked bool
	}{
		{"SELECT * FROM users", false},
		{"  select * from users", false},
		{"WITH cte AS (SELECT 1) SELECT * FROM cte", false},
		{"INSERT INTO users VALUES(1)", true},
		{"  insert into users values(1)", true},
		{"UPDATE users SET name = 'x'", true},
		{"DELETE FROM users", true},
		{"CREATE TABLE t(x INT)", true},
		{"ALTER TABLE t ADD y INT", true},
		{"DROP TABLE t", true},
		{"TRUNCATE TABLE t", true},
		{"MERGE INTO t USING s ON t.id = s.id WHEN MATCHED THEN DELETE", true},
		{"EXEC sp_help", true},
		{"EXECUTE sp_help", true},
		{"  exec sp_help", true},
		{"GRANT SELECT ON t TO u", true},
		{"REVOKE SELECT ON t FROM u", true},
		// SELECT INTO (creates a table)
		{"SELECT * INTO new_table FROM users", true},
	}

	for _, tt := range tests {
		err := guardReadOnly(tt.sql)
		if tt.blocked && err == nil {
			t.Errorf("guardReadOnly(%q): expected error, got nil", tt.sql)
		}
		if !tt.blocked && err != nil {
			t.Errorf("guardReadOnly(%q): unexpected error: %v", tt.sql, err)
		}
		if tt.blocked && err != nil {
			var qerr *errors.QueryError
			if errors.As(err, &qerr) && qerr.FixableBy != errors.FixableByHuman {
				t.Errorf("guardReadOnly(%q): fixableBy = %s, want human", tt.sql, qerr.FixableBy)
			}
		}
	}
}

func TestClassifyError(t *testing.T) {
	tests := []struct {
		msg       string
		fixableBy errors.FixableBy
	}{
		{"Login failed for user 'sa'", errors.FixableByHuman},
		{"connection refused", errors.FixableByHuman},
		{"Invalid object name 'foo'", errors.FixableByAgent},
		{"Invalid column name 'bar'", errors.FixableByAgent},
		{"Incorrect syntax near 'FROM'", errors.FixableByAgent},
		{"context deadline exceeded", errors.FixableByRetry},
		{"deadlock victim", errors.FixableByRetry},
		{"permission denied", errors.FixableByHuman},
		{"some unknown error", errors.FixableByAgent},
	}

	for _, tt := range tests {
		err := classifyError(fmt.Errorf("%s", tt.msg))
		var qerr *errors.QueryError
		if !errors.As(err, &qerr) {
			t.Errorf("classifyError(%q): expected QueryError", tt.msg)
			continue
		}
		if qerr.FixableBy != tt.fixableBy {
			t.Errorf("classifyError(%q): fixableBy = %s, want %s", tt.msg, qerr.FixableBy, tt.fixableBy)
		}
	}
}

func TestSplitSchemaTable(t *testing.T) {
	tests := []struct {
		input      string
		wantSchema string
		wantTable  string
	}{
		{"users", "dbo", "users"},
		{"dbo.users", "dbo", "users"},
		{"sales.orders", "sales", "orders"},
	}

	for _, tt := range tests {
		schema, table := splitSchemaTable(tt.input)
		if schema != tt.wantSchema || table != tt.wantTable {
			t.Errorf("splitSchemaTable(%q) = (%q, %q), want (%q, %q)",
				tt.input, schema, table, tt.wantSchema, tt.wantTable)
		}
	}
}

func TestIsSystemSchema(t *testing.T) {
	if !isSystemSchema("sys") {
		t.Error("sys should be a system schema")
	}
	if !isSystemSchema("INFORMATION_SCHEMA") {
		t.Error("INFORMATION_SCHEMA should be a system schema")
	}
	if isSystemSchema("dbo") {
		t.Error("dbo should not be a system schema")
	}
	if isSystemSchema("myschema") {
		t.Error("myschema should not be a system schema")
	}
}

func TestMapConstraintType(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"PRIMARY KEY", string(ConstraintPrimaryKey)},
		{"FOREIGN KEY", string(ConstraintForeignKey)},
		{"UNIQUE", string(ConstraintUnique)},
		{"CHECK", string(ConstraintCheck)},
	}

	for _, tt := range tests {
		got := mapConstraintType(tt.input)
		if string(got) != tt.want {
			t.Errorf("mapConstraintType(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// Integration tests — skipped unless AGENT_SQL_MSSQL_TEST_URL is set
func TestIntegration(t *testing.T) {
	testURL := os.Getenv("AGENT_SQL_MSSQL_TEST_URL")
	if testURL == "" {
		t.Skip("requires MSSQL: set AGENT_SQL_MSSQL_TEST_URL")
	}

	conn, err := Connect(Opts{
		Host:     "localhost",
		Port:     1433,
		Database: "testdb",
		Username: "sa",
		Password: "test",
		Readonly: true,
	})
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer conn.Close()

	ctx := context.Background()

	t.Run("query returns rows", func(t *testing.T) {
		result, err := conn.Query(ctx, "SELECT 1 AS val", QueryOpts{})
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Rows) != 1 {
			t.Fatalf("rows = %d, want 1", len(result.Rows))
		}
	})

	t.Run("GetTables", func(t *testing.T) {
		tables, err := conn.GetTables(ctx, false)
		if err != nil {
			t.Fatal(err)
		}
		_ = tables // just verify no error
	})
}

// Re-export types for test readability
type QueryOpts = struct{ Write bool }

var (
	ConstraintPrimaryKey = driver.ConstraintPrimaryKey
	ConstraintForeignKey = driver.ConstraintForeignKey
	ConstraintUnique     = driver.ConstraintUnique
	ConstraintCheck      = driver.ConstraintCheck
)
