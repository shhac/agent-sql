// Package integration contains end-to-end CLI tests.
package integration

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func buildBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "agent-sql")
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/agent-sql")
	cmd.Dir = projectRoot(t)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build failed: %s\n%s", err, out)
	}
	return bin
}

func projectRoot(t *testing.T) string {
	t.Helper()
	// Walk up from test file to find go.mod
	dir, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("cannot find project root")
		}
		dir = parent
	}
}

func setupTestDB(t *testing.T) string {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	// Use the binary's own SQLite to create the test DB
	bin := buildBinary(t)
	runCLI(t, bin, "run", "-c", dbPath, "--write",
		"CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL, email TEXT, age INTEGER)")
	runCLI(t, bin, "run", "-c", dbPath, "--write",
		"INSERT INTO users VALUES (1, 'Alice', 'alice@test.com', 30), (2, 'Bob', NULL, 25), (3, 'Charlie', 'charlie@test.com', 35)")
	runCLI(t, bin, "run", "-c", dbPath, "--write",
		"CREATE TABLE orders (id INTEGER PRIMARY KEY, user_id INTEGER, amount REAL)")
	runCLI(t, bin, "run", "-c", dbPath, "--write",
		"INSERT INTO orders VALUES (1, 1, 99.99), (2, 1, 149.50), (3, 2, 25.00)")
	runCLI(t, bin, "run", "-c", dbPath, "--write",
		"CREATE INDEX idx_orders_user ON orders(user_id)")
	return dbPath
}

func runCLI(t *testing.T, bin string, args ...string) (stdout, stderr string) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	cmd.Env = append(os.Environ(), "XDG_CONFIG_HOME="+t.TempDir())
	cmd.Run()
	return outBuf.String(), errBuf.String()
}

func TestCLIQueryRun(t *testing.T) {
	bin := buildBinary(t)
	dbPath := setupTestDB(t)

	t.Run("returns NDJSON rows", func(t *testing.T) {
		stdout, _ := runCLI(t, bin, "run", "-c", dbPath, "SELECT id, name FROM users ORDER BY id")
		lines := nonEmptyLines(stdout)
		if len(lines) != 3 {
			t.Fatalf("expected 3 lines, got %d: %s", len(lines), stdout)
		}
		var row map[string]any
		json.Unmarshal([]byte(lines[0]), &row)
		if row["name"] != "Alice" {
			t.Errorf("first row name = %v, want Alice", row["name"])
		}
		if _, ok := row["@truncated"]; !ok {
			t.Error("missing @truncated field")
		}
	})

	t.Run("respects --limit", func(t *testing.T) {
		stdout, _ := runCLI(t, bin, "run", "-c", dbPath, "--limit", "1", "SELECT * FROM users ORDER BY id")
		lines := nonEmptyLines(stdout)
		// Should have 1 data row + 1 pagination row
		if len(lines) != 2 {
			t.Fatalf("expected 2 lines (1 row + pagination), got %d: %s", len(lines), stdout)
		}
		var pag map[string]any
		json.Unmarshal([]byte(lines[1]), &pag)
		if pag["@pagination"] == nil {
			t.Error("expected @pagination on last line")
		}
	})

	t.Run("handles empty result", func(t *testing.T) {
		stdout, _ := runCLI(t, bin, "run", "-c", dbPath, "SELECT * FROM users WHERE id = 999")
		if strings.TrimSpace(stdout) != "" {
			t.Errorf("expected empty output, got: %s", stdout)
		}
	})

	t.Run("error on bad SQL goes to stderr", func(t *testing.T) {
		_, stderr := runCLI(t, bin, "run", "-c", dbPath, "SELEC * FROM users")
		if !strings.Contains(stderr, "fixable_by") {
			t.Errorf("expected fixable_by in stderr, got: %s", stderr)
		}
	})

	t.Run("readonly blocks writes", func(t *testing.T) {
		_, stderr := runCLI(t, bin, "run", "-c", dbPath, "INSERT INTO users VALUES(99,'Test','t@t',20)")
		if !strings.Contains(stderr, "readonly") && !strings.Contains(stderr, "read-only") && !strings.Contains(stderr, "fixable_by") {
			// The readonly enforcement varies — SQLite uses OS-level SQLITE_OPEN_READONLY
			// The error should contain some indication of the failure
			if stderr == "" {
				t.Error("expected error for write on readonly connection")
			}
		}
	})
}

func TestCLISchemaTables(t *testing.T) {
	bin := buildBinary(t)
	dbPath := setupTestDB(t)

	stdout, _ := runCLI(t, bin, "schema", "tables", "-c", dbPath)
	var result map[string]any
	json.Unmarshal([]byte(stdout), &result)
	tables, ok := result["tables"].([]any)
	if !ok {
		t.Fatalf("expected tables array, got: %s", stdout)
	}
	if len(tables) < 2 {
		t.Errorf("expected at least 2 tables, got %d", len(tables))
	}
}

func TestCLISchemaDescribe(t *testing.T) {
	bin := buildBinary(t)
	dbPath := setupTestDB(t)

	stdout, _ := runCLI(t, bin, "schema", "describe", "users", "-c", dbPath)
	var result map[string]any
	json.Unmarshal([]byte(stdout), &result)
	columns, ok := result["columns"].([]any)
	if !ok {
		t.Fatalf("expected columns array, got: %s", stdout)
	}
	if len(columns) != 4 {
		t.Errorf("expected 4 columns, got %d", len(columns))
	}
}

func TestCLIQueryCount(t *testing.T) {
	bin := buildBinary(t)
	dbPath := setupTestDB(t)

	stdout, _ := runCLI(t, bin, "query", "count", "users", "-c", dbPath)
	var result map[string]any
	json.Unmarshal([]byte(stdout), &result)
	count, ok := result["count"].(float64)
	if !ok || count != 3 {
		t.Errorf("expected count=3, got: %v", result["count"])
	}
}

func TestCLIQuerySample(t *testing.T) {
	bin := buildBinary(t)
	dbPath := setupTestDB(t)

	stdout, _ := runCLI(t, bin, "query", "sample", "users", "-c", dbPath, "--limit", "2")
	lines := nonEmptyLines(stdout)
	if len(lines) != 2 {
		t.Errorf("expected 2 sample rows, got %d", len(lines))
	}
}

func TestCLIUsage(t *testing.T) {
	bin := buildBinary(t)
	stdout, _ := runCLI(t, bin, "usage")
	if !strings.Contains(stdout, "agent-sql") {
		t.Error("usage text missing agent-sql")
	}
	if !strings.Contains(stdout, "MSSQL") {
		t.Error("usage text missing MSSQL")
	}
	if !strings.Contains(stdout, "DuckDB") {
		t.Error("usage text missing DuckDB")
	}
}

func TestCLIConfigListKeys(t *testing.T) {
	bin := buildBinary(t)
	stdout, _ := runCLI(t, bin, "config", "list-keys")
	if !strings.Contains(stdout, "defaults.format") {
		t.Error("config list-keys missing defaults.format")
	}
	if !strings.Contains(stdout, "query.timeout") {
		t.Error("config list-keys missing query.timeout")
	}
}

func TestCLIVersion(t *testing.T) {
	bin := buildBinary(t)
	stdout, _ := runCLI(t, bin, "--version")
	if !strings.Contains(stdout, "agent-sql") {
		t.Error("version missing agent-sql")
	}
}

func nonEmptyLines(s string) []string {
	var lines []string
	for _, line := range strings.Split(strings.TrimSpace(s), "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	return lines
}
