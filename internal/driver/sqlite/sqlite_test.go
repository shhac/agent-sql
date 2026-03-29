package sqlite

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/shhac/agent-sql/internal/errors"
)

func setupTestDB(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	conn, err := Connect(Opts{Path: dbPath, Create: true})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	ctx := context.Background()
	for _, sql := range []string{
		"CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL, email TEXT, age INTEGER)",
		"INSERT INTO users VALUES (1, 'Alice', 'alice@test.com', 30)",
		"INSERT INTO users VALUES (2, 'Bob', NULL, 25)",
		"INSERT INTO users VALUES (3, 'Charlie', 'charlie@test.com', 35)",
		"CREATE TABLE orders (id INTEGER PRIMARY KEY, user_id INTEGER REFERENCES users(id), amount REAL)",
		"INSERT INTO orders VALUES (1, 1, 99.99)",
		"INSERT INTO orders VALUES (2, 1, 149.50)",
		"INSERT INTO orders VALUES (3, 2, 25.00)",
		"CREATE INDEX idx_orders_user ON orders(user_id)",
		"CREATE VIEW active_users AS SELECT * FROM users WHERE age >= 30",
	} {
		if _, err := conn.Query(ctx, sql, QueryOpts{Write: true}); err != nil {
			t.Fatalf("setup %s: %v", sql, err)
		}
	}
	return dbPath
}

// QueryOpts imported from driver package — use local alias
type QueryOpts = struct{ Write bool }

func TestConnect(t *testing.T) {
	dbPath := setupTestDB(t)

	t.Run("connects to existing db", func(t *testing.T) {
		conn, err := Connect(Opts{Path: dbPath, Readonly: true})
		if err != nil {
			t.Fatal(err)
		}
		conn.Close()
	})

	t.Run("rejects non-existent file in readonly mode", func(t *testing.T) {
		_, err := Connect(Opts{Path: "/tmp/nonexistent-sqlite-test.db", Readonly: true})
		if err == nil {
			t.Fatal("expected error for non-existent file")
		}
	})
}

func TestQuery(t *testing.T) {
	dbPath := setupTestDB(t)
	conn, err := Connect(Opts{Path: dbPath, Readonly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ctx := context.Background()

	t.Run("selects rows with correct columns", func(t *testing.T) {
		result, err := conn.Query(ctx, "SELECT id, name FROM users ORDER BY id", QueryOpts{})
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Columns) != 2 || result.Columns[0] != "id" || result.Columns[1] != "name" {
			t.Errorf("columns = %v, want [id name]", result.Columns)
		}
		if len(result.Rows) != 3 {
			t.Fatalf("rows = %d, want 3", len(result.Rows))
		}
		if result.Rows[0]["name"] != "Alice" {
			t.Errorf("first row name = %v, want Alice", result.Rows[0]["name"])
		}
	})

	t.Run("preserves NULL values", func(t *testing.T) {
		result, err := conn.Query(ctx, "SELECT email FROM users WHERE id = 2", QueryOpts{})
		if err != nil {
			t.Fatal(err)
		}
		if result.Rows[0]["email"] != nil {
			t.Errorf("email = %v, want nil", result.Rows[0]["email"])
		}
	})

	t.Run("handles empty result set", func(t *testing.T) {
		result, err := conn.Query(ctx, "SELECT * FROM users WHERE id = 999", QueryOpts{})
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Rows) != 0 {
			t.Errorf("rows = %d, want 0", len(result.Rows))
		}
	})

	t.Run("handles aggregations", func(t *testing.T) {
		result, err := conn.Query(ctx, "SELECT COUNT(*) AS cnt FROM users", QueryOpts{})
		if err != nil {
			t.Fatal(err)
		}
		cnt, ok := result.Rows[0]["cnt"].(int64)
		if !ok || cnt != 3 {
			t.Errorf("cnt = %v (%T), want 3", result.Rows[0]["cnt"], result.Rows[0]["cnt"])
		}
	})
}

func TestReadonly(t *testing.T) {
	dbPath := setupTestDB(t)
	conn, err := Connect(Opts{Path: dbPath, Readonly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ctx := context.Background()

	t.Run("blocks INSERT", func(t *testing.T) {
		_, err := conn.Query(ctx, "INSERT INTO users VALUES(99,'Test','t@t.com',20)", QueryOpts{})
		if err == nil {
			t.Fatal("expected error for INSERT in readonly mode")
		}
		var qerr *errors.QueryError
		if errors.As(err, &qerr) && qerr.FixableBy != errors.FixableByHuman {
			t.Errorf("fixableBy = %s, want human", qerr.FixableBy)
		}
	})

	t.Run("blocks CREATE TABLE", func(t *testing.T) {
		_, err := conn.Query(ctx, "CREATE TABLE test(x INT)", QueryOpts{})
		if err == nil {
			t.Fatal("expected error for CREATE in readonly mode")
		}
	})
}

func TestGetTables(t *testing.T) {
	dbPath := setupTestDB(t)
	conn, err := Connect(Opts{Path: dbPath, Readonly: true})
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

	t.Run("excludes system tables by default", func(t *testing.T) {
		tables, err := conn.GetTables(ctx, false)
		if err != nil {
			t.Fatal(err)
		}
		for _, tbl := range tables {
			if tbl.Name == "sqlite_master" || tbl.Name == "sqlite_sequence" {
				t.Errorf("system table %s should be excluded", tbl.Name)
			}
		}
	})
}

func TestDescribeTable(t *testing.T) {
	dbPath := setupTestDB(t)
	conn, err := Connect(Opts{Path: dbPath, Readonly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ctx := context.Background()

	t.Run("describes columns with types and nullability", func(t *testing.T) {
		cols, err := conn.DescribeTable(ctx, "users")
		if err != nil {
			t.Fatal(err)
		}
		if len(cols) != 4 {
			t.Fatalf("columns = %d, want 4", len(cols))
		}

		id := cols[0]
		if id.Name != "id" || id.Type != "INTEGER" || id.Nullable || !id.PrimaryKey {
			t.Errorf("id column = %+v", id)
		}

		email := cols[2]
		if email.Name != "email" || !email.Nullable {
			t.Errorf("email column = %+v", email)
		}
	})
}

func TestGetIndexes(t *testing.T) {
	dbPath := setupTestDB(t)
	conn, err := Connect(Opts{Path: dbPath, Readonly: true})
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
	conn, err := Connect(Opts{Path: dbPath, Readonly: true})
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
			if c.Type == "primary_key" {
				found = true
				if len(c.Columns) != 1 || c.Columns[0] != "id" {
					t.Errorf("pk columns = %v, want [id]", c.Columns)
				}
			}
		}
		if !found {
			t.Error("primary key not found")
		}
	})

	t.Run("finds foreign key", func(t *testing.T) {
		constraints, err := conn.GetConstraints(ctx, "orders")
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, c := range constraints {
			if c.Type == "foreign_key" {
				found = true
				if c.ReferencedTable != "users" {
					t.Errorf("referenced table = %s, want users", c.ReferencedTable)
				}
			}
		}
		if !found {
			t.Error("foreign key not found")
		}
	})
}

func TestSearchSchema(t *testing.T) {
	dbPath := setupTestDB(t)
	conn, err := Connect(Opts{Path: dbPath, Readonly: true})
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
	conn, err := Connect(Opts{Path: filepath.Join(t.TempDir(), "test.db"), Create: true})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	if got := conn.QuoteIdent("table"); got != `"table"` {
		t.Errorf("QuoteIdent(table) = %s, want \"table\"", got)
	}
	if got := conn.QuoteIdent(`my"table`); got != `"my""table"` {
		t.Errorf("QuoteIdent(my\"table) = %s", got)
	}
}

func TestWriteMode(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "write-test.db")

	conn, err := Connect(Opts{Path: dbPath, Create: true})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ctx := context.Background()

	_, err = conn.Query(ctx, "CREATE TABLE t(x INTEGER)", QueryOpts{Write: true})
	if err != nil {
		t.Fatal(err)
	}

	result, err := conn.Query(ctx, "INSERT INTO t VALUES(1),(2),(3)", QueryOpts{Write: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Command != "INSERT" {
		t.Errorf("command = %s, want INSERT", result.Command)
	}
	if result.RowsAffected != 3 {
		t.Errorf("rowsAffected = %d, want 3", result.RowsAffected)
	}
}

// Ensure _ import doesn't cause issues
var _ = os.Getenv
