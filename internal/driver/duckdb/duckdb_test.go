package duckdb

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/shhac/agent-sql/internal/driver"
	"github.com/shhac/agent-sql/internal/errors"
)

func skipIfNoDuckDB(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("duckdb"); err != nil {
		t.Skip("duckdb CLI not found on PATH, skipping")
	}
}

func setupTestDB(t *testing.T) string {
	t.Helper()
	skipIfNoDuckDB(t)

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.duckdb")

	conn, err := Connect(context.Background(), Opts{Path: dbPath})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	ctx := context.Background()
	for _, sql := range []string{
		"CREATE TABLE users (id INTEGER PRIMARY KEY, name VARCHAR NOT NULL, email VARCHAR, age INTEGER)",
		"INSERT INTO users VALUES (1, 'Alice', 'alice@test.com', 30)",
		"INSERT INTO users VALUES (2, 'Bob', NULL, 25)",
		"INSERT INTO users VALUES (3, 'Charlie', 'charlie@test.com', 35)",
		"CREATE TABLE orders (id INTEGER PRIMARY KEY, user_id INTEGER REFERENCES users(id), amount DOUBLE)",
		"INSERT INTO orders VALUES (1, 1, 99.99)",
		"INSERT INTO orders VALUES (2, 1, 149.50)",
		"INSERT INTO orders VALUES (3, 2, 25.00)",
		"CREATE INDEX idx_orders_user ON orders(user_id)",
		"CREATE VIEW active_users AS SELECT * FROM users WHERE age >= 30",
	} {
		if _, err := conn.Query(ctx, sql, driver.QueryOpts{Write: true}); err != nil {
			t.Fatalf("setup %s: %v", sql, err)
		}
	}
	return dbPath
}

func TestConnect(t *testing.T) {
	skipIfNoDuckDB(t)

	t.Run("connects in-memory", func(t *testing.T) {
		conn, err := Connect(context.Background(), Opts{})
		if err != nil {
			t.Fatal(err)
		}
		conn.Close()
	})

	t.Run("connects to file", func(t *testing.T) {
		dbPath := setupTestDB(t)
		conn, err := Connect(context.Background(), Opts{Path: dbPath, Readonly: true})
		if err != nil {
			t.Fatal(err)
		}
		conn.Close()
	})

	t.Run("rejects non-existent file in readonly", func(t *testing.T) {
		_, err := Connect(context.Background(), Opts{Path: "/tmp/nonexistent-duckdb-test-12345.duckdb", Readonly: true})
		if err == nil {
			t.Fatal("expected error for non-existent file")
		}
	})
}

func TestCLINotFound(t *testing.T) {
	t.Setenv("AGENT_SQL_DUCKDB_PATH", "/nonexistent/duckdb-binary")
	_, err := Connect(context.Background(), Opts{})
	if err == nil {
		t.Fatal("expected error when CLI not found")
	}
	var qerr *errors.QueryError
	if !errors.As(err, &qerr) {
		t.Fatalf("expected QueryError, got %T: %v", err, err)
	}
	if qerr.FixableBy != errors.FixableByHuman {
		t.Errorf("fixableBy = %s, want human", qerr.FixableBy)
	}
}

func TestQuery(t *testing.T) {
	dbPath := setupTestDB(t)
	conn, err := Connect(context.Background(), Opts{Path: dbPath, Readonly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ctx := context.Background()

	t.Run("selects rows", func(t *testing.T) {
		result, err := conn.Query(ctx, "SELECT id, name FROM users ORDER BY id", driver.QueryOpts{})
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Rows) != 3 {
			t.Fatalf("rows = %d, want 3", len(result.Rows))
		}
		// Check columns exist (order may vary due to Go map)
		if len(result.Columns) != 2 {
			t.Errorf("columns = %v, want 2 columns", result.Columns)
		}
	})

	t.Run("preserves NULL values", func(t *testing.T) {
		result, err := conn.Query(ctx, "SELECT email FROM users WHERE id = 2", driver.QueryOpts{})
		if err != nil {
			t.Fatal(err)
		}
		if result.Rows[0]["email"] != nil {
			t.Errorf("email = %v, want nil", result.Rows[0]["email"])
		}
	})

	t.Run("handles empty result set", func(t *testing.T) {
		result, err := conn.Query(ctx, "SELECT * FROM users WHERE id = 999", driver.QueryOpts{})
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Rows) != 0 {
			t.Errorf("rows = %d, want 0", len(result.Rows))
		}
	})

	t.Run("handles aggregations", func(t *testing.T) {
		result, err := conn.Query(ctx, "SELECT COUNT(*) AS cnt FROM users", driver.QueryOpts{})
		if err != nil {
			t.Fatal(err)
		}
		// DuckDB NDJSON returns numbers as float64 via JSON
		cnt, ok := result.Rows[0]["cnt"].(float64)
		if !ok || cnt != 3 {
			t.Errorf("cnt = %v (%T), want 3", result.Rows[0]["cnt"], result.Rows[0]["cnt"])
		}
	})
}

func TestDataTypes(t *testing.T) {
	skipIfNoDuckDB(t)
	conn, err := Connect(context.Background(), Opts{})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ctx := context.Background()

	t.Run("boolean", func(t *testing.T) {
		result, err := conn.Query(ctx, "SELECT true AS t, false AS f", driver.QueryOpts{})
		if err != nil {
			t.Fatal(err)
		}
		// DuckDB jsonlines may serialize booleans as JSON booleans or "true"/"false" strings
		// depending on version. Accept both.
		tVal := result.Rows[0]["t"]
		fVal := result.Rows[0]["f"]
		if tVal != true && tVal != "true" {
			t.Errorf("true = %v (%T)", tVal, tVal)
		}
		if fVal != false && fVal != "false" {
			t.Errorf("false = %v (%T)", fVal, fVal)
		}
	})

	t.Run("integer and bigint", func(t *testing.T) {
		result, err := conn.Query(ctx, "SELECT 42::INTEGER AS i, 9999999999::BIGINT AS b", driver.QueryOpts{})
		if err != nil {
			t.Fatal(err)
		}
		// JSON numbers parse as float64 in Go
		if v, ok := result.Rows[0]["i"].(float64); !ok || v != 42 {
			t.Errorf("integer = %v (%T)", result.Rows[0]["i"], result.Rows[0]["i"])
		}
		if v, ok := result.Rows[0]["b"].(float64); !ok || v != 9999999999 {
			t.Errorf("bigint = %v (%T)", result.Rows[0]["b"], result.Rows[0]["b"])
		}
	})

	t.Run("float", func(t *testing.T) {
		result, err := conn.Query(ctx, "SELECT 3.14::DOUBLE AS f", driver.QueryOpts{})
		if err != nil {
			t.Fatal(err)
		}
		if v, ok := result.Rows[0]["f"].(float64); !ok || v != 3.14 {
			t.Errorf("float = %v (%T)", result.Rows[0]["f"], result.Rows[0]["f"])
		}
	})

	t.Run("decimal as string", func(t *testing.T) {
		result, err := conn.Query(ctx, "SELECT 123.45::DECIMAL(10,2) AS d", driver.QueryOpts{})
		if err != nil {
			t.Fatal(err)
		}
		// DuckDB serializes DECIMAL as a number in jsonlines
		v := result.Rows[0]["d"]
		if v == nil {
			t.Fatal("decimal value is nil")
		}
	})

	t.Run("date and time", func(t *testing.T) {
		result, err := conn.Query(ctx, "SELECT DATE '2024-01-15' AS d, TIME '13:30:00' AS t", driver.QueryOpts{})
		if err != nil {
			t.Fatal(err)
		}
		if result.Rows[0]["d"] == nil {
			t.Error("date is nil")
		}
		if result.Rows[0]["t"] == nil {
			t.Error("time is nil")
		}
	})

	t.Run("array", func(t *testing.T) {
		result, err := conn.Query(ctx, "SELECT [1, 2, 3] AS arr", driver.QueryOpts{})
		if err != nil {
			t.Fatal(err)
		}
		arr, ok := result.Rows[0]["arr"].([]any)
		if !ok {
			t.Fatalf("array = %v (%T), want []any", result.Rows[0]["arr"], result.Rows[0]["arr"])
		}
		if len(arr) != 3 {
			t.Errorf("array length = %d, want 3", len(arr))
		}
	})

	t.Run("NULL", func(t *testing.T) {
		result, err := conn.Query(ctx, "SELECT NULL AS n", driver.QueryOpts{})
		if err != nil {
			t.Fatal(err)
		}
		if result.Rows[0]["n"] != nil {
			t.Errorf("NULL = %v, want nil", result.Rows[0]["n"])
		}
	})
}

func TestReadonly(t *testing.T) {
	dbPath := setupTestDB(t)
	conn, err := Connect(context.Background(), Opts{Path: dbPath, Readonly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ctx := context.Background()

	t.Run("blocks INSERT in readonly", func(t *testing.T) {
		_, err := conn.Query(ctx, "INSERT INTO users VALUES(99,'Test','t@t.com',20)", driver.QueryOpts{})
		if err == nil {
			t.Fatal("expected error for INSERT in readonly mode")
		}
	})

	t.Run("blocks CREATE TABLE in readonly", func(t *testing.T) {
		_, err := conn.Query(ctx, "CREATE TABLE test(x INT)", driver.QueryOpts{})
		if err == nil {
			t.Fatal("expected error for CREATE in readonly mode")
		}
	})
}

func TestWriteMode(t *testing.T) {
	skipIfNoDuckDB(t)
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "write-test.duckdb")

	conn, err := Connect(context.Background(), Opts{Path: dbPath})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ctx := context.Background()

	_, err = conn.Query(ctx, "CREATE TABLE t(x INTEGER)", driver.QueryOpts{Write: true})
	if err != nil {
		t.Fatal(err)
	}

	result, err := conn.Query(ctx, "INSERT INTO t VALUES(1),(2),(3)", driver.QueryOpts{Write: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Command != "INSERT" {
		t.Errorf("command = %s, want INSERT", result.Command)
	}
	// DuckDB jsonlines doesn't report rowsAffected
	if result.RowsAffected != 0 {
		t.Errorf("rowsAffected = %d, want 0", result.RowsAffected)
	}

	// Verify data was written
	sel, err := conn.Query(ctx, "SELECT COUNT(*) AS cnt FROM t", driver.QueryOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if cnt, ok := sel.Rows[0]["cnt"].(float64); !ok || cnt != 3 {
		t.Errorf("count = %v, want 3", sel.Rows[0]["cnt"])
	}
}

func TestGetTables(t *testing.T) {
	dbPath := setupTestDB(t)
	conn, err := Connect(context.Background(), Opts{Path: dbPath, Readonly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ctx := context.Background()

	t.Run("lists user tables and views", func(t *testing.T) {
		tables, err := conn.GetTables(ctx, false)
		if err != nil {
			t.Fatal(err)
		}
		names := make(map[string]string)
		for _, tbl := range tables {
			names[tbl.Name] = tbl.Type
		}
		if _, ok := names["users"]; !ok {
			t.Error("missing users table")
		}
		if names["active_users"] != "view" {
			t.Error("active_users should be a view")
		}
	})

	t.Run("includes system tables when requested", func(t *testing.T) {
		tables, err := conn.GetTables(ctx, true)
		if err != nil {
			t.Fatal(err)
		}
		if len(tables) == 0 {
			t.Error("expected tables")
		}
	})
}

func TestDescribeTable(t *testing.T) {
	dbPath := setupTestDB(t)
	conn, err := Connect(context.Background(), Opts{Path: dbPath, Readonly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ctx := context.Background()

	cols, err := conn.DescribeTable(ctx, "users")
	if err != nil {
		t.Fatal(err)
	}
	if len(cols) != 4 {
		t.Fatalf("columns = %d, want 4", len(cols))
	}

	// Find columns by name
	colMap := make(map[string]driver.ColumnInfo)
	for _, c := range cols {
		colMap[c.Name] = c
	}

	name := colMap["name"]
	if name.Nullable {
		t.Error("name should not be nullable")
	}

	email := colMap["email"]
	if !email.Nullable {
		t.Error("email should be nullable")
	}
}

func TestGetIndexes(t *testing.T) {
	dbPath := setupTestDB(t)
	conn, err := Connect(context.Background(), Opts{Path: dbPath, Readonly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ctx := context.Background()

	t.Run("lists indexes", func(t *testing.T) {
		indexes, err := conn.GetIndexes(ctx, "")
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, idx := range indexes {
			if idx.Name == "idx_orders_user" {
				found = true
				if len(idx.Columns) != 1 || idx.Columns[0] != "user_id" {
					t.Errorf("columns = %v, want [user_id]", idx.Columns)
				}
			}
		}
		if !found {
			t.Error("idx_orders_user not found")
		}
	})

	t.Run("filters by table", func(t *testing.T) {
		indexes, err := conn.GetIndexes(ctx, "orders")
		if err != nil {
			t.Fatal(err)
		}
		for _, idx := range indexes {
			if idx.Table != "orders" {
				t.Errorf("index %s has table %s, want orders", idx.Name, idx.Table)
			}
		}
	})
}

func TestGetConstraints(t *testing.T) {
	dbPath := setupTestDB(t)
	conn, err := Connect(context.Background(), Opts{Path: dbPath, Readonly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ctx := context.Background()

	t.Run("finds primary key", func(t *testing.T) {
		constraints, err := conn.GetConstraints(ctx, "users")
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, c := range constraints {
			if c.Type == driver.ConstraintPrimaryKey {
				found = true
			}
		}
		if !found {
			t.Error("primary key not found")
		}
	})

	t.Run("finds constraints for all tables", func(t *testing.T) {
		constraints, err := conn.GetConstraints(ctx, "")
		if err != nil {
			t.Fatal(err)
		}
		if len(constraints) == 0 {
			t.Error("expected some constraints")
		}
	})
}

func TestSearchSchema(t *testing.T) {
	dbPath := setupTestDB(t)
	conn, err := Connect(context.Background(), Opts{Path: dbPath, Readonly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ctx := context.Background()

	t.Run("finds tables by name", func(t *testing.T) {
		result, err := conn.SearchSchema(ctx, "user")
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, tbl := range result.Tables {
			if tbl.Name == "users" {
				found = true
			}
		}
		if !found {
			t.Error("users table not found in search")
		}
	})

	t.Run("finds columns by name", func(t *testing.T) {
		result, err := conn.SearchSchema(ctx, "email")
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, col := range result.Columns {
			if col.Column == "email" && col.Table == "users" {
				found = true
			}
		}
		if !found {
			t.Error("email column not found in search")
		}
	})
}

func TestQuoteIdent(t *testing.T) {
	conn := &duckdbConn{}

	if got := conn.QuoteIdent("table"); got != `"table"` {
		t.Errorf("QuoteIdent(table) = %s, want \"table\"", got)
	}
	if got := conn.QuoteIdent(`my"table`); got != `"my""table"` {
		t.Errorf(`QuoteIdent(my"table) = %s`, got)
	}
	if got := conn.QuoteIdent("schema.table"); got != `"schema"."table"` {
		t.Errorf("QuoteIdent(schema.table) = %s", got)
	}
}

func TestParseNDJSON(t *testing.T) {
	t.Run("parses multiple rows", func(t *testing.T) {
		input := `{"id":1,"name":"Alice"}
{"id":2,"name":"Bob"}
`
		rows, err := parseNDJSON(input)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 2 {
			t.Fatalf("rows = %d, want 2", len(rows))
		}
		if rows[0]["name"] != "Alice" {
			t.Errorf("first name = %v", rows[0]["name"])
		}
	})

	t.Run("skips empty lines", func(t *testing.T) {
		input := `{"id":1}

{"id":2}

`
		rows, err := parseNDJSON(input)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 2 {
			t.Errorf("rows = %d, want 2", len(rows))
		}
	})

	t.Run("skips DuckDB empty result quirk", func(t *testing.T) {
		input := "{\n"
		rows, err := parseNDJSON(input)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 0 {
			t.Errorf("rows = %d, want 0", len(rows))
		}
	})

	t.Run("handles empty stdout", func(t *testing.T) {
		rows, err := parseNDJSON("")
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 0 {
			t.Errorf("rows = %d, want 0", len(rows))
		}
	})

	t.Run("handles embedded newlines in strings", func(t *testing.T) {
		// JSON with escaped newlines (valid single-line JSON)
		input := `{"text":"line1\nline2"}
`
		rows, err := parseNDJSON(input)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 1 {
			t.Fatalf("rows = %d, want 1", len(rows))
		}
		if rows[0]["text"] != "line1\nline2" {
			t.Errorf("text = %v", rows[0]["text"])
		}
	})

	t.Run("handles embedded quotes", func(t *testing.T) {
		input := `{"text":"he said \"hello\""}
`
		rows, err := parseNDJSON(input)
		if err != nil {
			t.Fatal(err)
		}
		if rows[0]["text"] != `he said "hello"` {
			t.Errorf("text = %v", rows[0]["text"])
		}
	})

	t.Run("handles unicode", func(t *testing.T) {
		input := `{"name":"日本語テスト"}
`
		rows, err := parseNDJSON(input)
		if err != nil {
			t.Fatal(err)
		}
		if rows[0]["name"] != "日本語テスト" {
			t.Errorf("name = %v", rows[0]["name"])
		}
	})

	t.Run("errors on malformed JSON", func(t *testing.T) {
		input := `not valid json`
		_, err := parseNDJSON(input)
		if err == nil {
			t.Fatal("expected error for malformed JSON")
		}
		var qerr *errors.QueryError
		if !errors.As(err, &qerr) {
			t.Fatalf("expected QueryError, got %T", err)
		}
		if qerr.FixableBy != errors.FixableByAgent {
			t.Errorf("fixableBy = %s, want agent", qerr.FixableBy)
		}
	})
}

func TestErrorClassification(t *testing.T) {
	tests := []struct {
		message   string
		fixableBy errors.FixableBy
	}{
		{"Catalog Error: Table with name 'foo' does not exist", errors.FixableByAgent},
		{"Parser Error: syntax error at end of input", errors.FixableByAgent},
		{"Not allowed in read-only mode", errors.FixableByHuman},
		{"Permission Error: cannot write", errors.FixableByHuman},
		{"IO Error: could not open file", errors.FixableByAgent},
		{"Unknown error", errors.FixableByAgent},
	}

	for _, tt := range tests {
		t.Run(tt.message, func(t *testing.T) {
			err := classifyError(tt.message)
			var qerr *errors.QueryError
			if !errors.As(err, &qerr) {
				t.Fatalf("expected QueryError, got %T", err)
			}
			if qerr.FixableBy != tt.fixableBy {
				t.Errorf("fixableBy = %s, want %s", qerr.FixableBy, tt.fixableBy)
			}
		})
	}
}

func TestFileQueries(t *testing.T) {
	skipIfNoDuckDB(t)

	t.Run("queries CSV file", func(t *testing.T) {
		dir := t.TempDir()
		csvPath := filepath.Join(dir, "data.csv")
		if err := os.WriteFile(csvPath, []byte("id,name\n1,Alice\n2,Bob\n"), 0644); err != nil {
			t.Fatal(err)
		}

		conn, err := Connect(context.Background(), Opts{})
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()

		result, err := conn.Query(context.Background(),
			fmt.Sprintf("SELECT * FROM read_csv_auto('%s')", csvPath),
			driver.QueryOpts{})
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Rows) != 2 {
			t.Errorf("rows = %d, want 2", len(result.Rows))
		}
	})

	t.Run("queries JSON file", func(t *testing.T) {
		dir := t.TempDir()
		jsonPath := filepath.Join(dir, "data.json")
		if err := os.WriteFile(jsonPath, []byte(`[{"id":1,"name":"Alice"},{"id":2,"name":"Bob"}]`), 0644); err != nil {
			t.Fatal(err)
		}

		conn, err := Connect(context.Background(), Opts{})
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()

		result, err := conn.Query(context.Background(),
			fmt.Sprintf("SELECT * FROM read_json_auto('%s')", jsonPath),
			driver.QueryOpts{})
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Rows) != 2 {
			t.Errorf("rows = %d, want 2", len(result.Rows))
		}
	})
}

func TestParseExpressionList(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"[col1, col2]", 2},
		{"[single]", 1},
		{"", 0},
		{"[a, b, c]", 3},
	}
	for _, tt := range tests {
		result := parseExpressionList(tt.input)
		if len(result) != tt.want {
			t.Errorf("parseExpressionList(%q) = %v (len %d), want len %d", tt.input, result, len(result), tt.want)
		}
	}
}
