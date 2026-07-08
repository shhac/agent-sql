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

	"gopkg.in/yaml.v3"
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
		inner, ok := pag["@pagination"].(map[string]any)
		if !ok {
			t.Fatalf("expected @pagination object on last line, got: %s", lines[1])
		}
		// Pin the family snake_case wire keys.
		if inner["has_more"] != true {
			t.Errorf("has_more = %v, want true", inner["has_more"])
		}
		if inner["row_count"] != float64(1) {
			t.Errorf("row_count = %v, want 1", inner["row_count"])
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
	tables := parseNDJSONRecords(t, stdout)
	if len(tables) < 2 {
		t.Errorf("expected at least 2 table records, got %d: %s", len(tables), stdout)
	}
}

// parseNDJSONRecords parses one JSON object per line (the family list shape),
// skipping @-prefixed metadata lines.
func parseNDJSONRecords(t *testing.T, stdout string) []map[string]any {
	t.Helper()
	var records []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(stdout), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("line is not valid JSON: %v\n%s", err, line)
		}
		meta := false
		for k := range rec {
			if strings.HasPrefix(k, "@") {
				meta = true
			}
		}
		if !meta {
			records = append(records, rec)
		}
	}
	return records
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

// TestCLIYAMLMatchesJSON guards that --format yaml produces the same key names
// and pruning as --format json for both a single resource (schema describe) and
// a list (schema tables). The structs carry json tags that yaml.v3 ignores, so
// without a JSON round-trip the YAML keys would diverge (e.g. primarykey vs
// primaryKey, and empty defaultValue would leak instead of being omitted).
func TestCLIYAMLMatchesJSON(t *testing.T) {
	bin := buildBinary(t)
	dbPath := setupTestDB(t)

	cases := []struct {
		name string
		args []string
	}{
		{"single resource (describe)", []string{"schema", "describe", "users"}},
		{"list (tables)", []string{"schema", "tables"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			jsonOut, _ := runCLI(t, bin, append([]string{"-c", dbPath, "-f", "json"}, tc.args...)...)
			yamlOut, _ := runCLI(t, bin, append([]string{"-c", dbPath, "-f", "yaml"}, tc.args...)...)

			var fromJSON, fromYAML any
			if err := json.Unmarshal([]byte(jsonOut), &fromJSON); err != nil {
				t.Fatalf("json output not valid JSON: %v\n%s", err, jsonOut)
			}
			if err := yaml.Unmarshal([]byte(yamlOut), &fromYAML); err != nil {
				t.Fatalf("yaml output not valid YAML: %v\n%s", err, yamlOut)
			}

			// Re-marshal both through JSON so the comparison ignores
			// representation (yaml decodes to map[string]any like json does
			// once normalized) and compares structure and key names.
			normJSON, _ := json.Marshal(fromJSON)
			normYAML, _ := json.Marshal(normalizeYAML(fromYAML))
			if string(normJSON) != string(normYAML) {
				t.Errorf("yaml and json disagree.\njson: %s\nyaml: %s", normJSON, normYAML)
			}
		})
	}
}

// normalizeYAML converts yaml.v3's map[interface{}]interface{} (or
// map[string]interface{}) trees into json-marshalable map[string]any trees.
func normalizeYAML(v any) any {
	switch m := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(m))
		for k, val := range m {
			out[k] = normalizeYAML(val)
		}
		return out
	case map[any]any:
		out := make(map[string]any, len(m))
		for k, val := range m {
			out[k.(string)] = normalizeYAML(val)
		}
		return out
	case []any:
		for i := range m {
			m[i] = normalizeYAML(m[i])
		}
		return m
	default:
		return v
	}
}

// TestCLIUsageErrorsAreStructured guards that cobra's own usage errors —
// unknown command/flag and bad args — are rendered as structured JSON rather
// than failing silently with exit 1 and no output.
func TestCLIUsageErrorsAreStructured(t *testing.T) {
	bin := buildBinary(t)

	cases := []struct {
		name string
		args []string
	}{
		{"unknown command", []string{"bogus-command"}},
		{"unknown flag", []string{"--nonsense"}},
		{"missing required arg", []string{"run"}},
		{"too many args", []string{"run", "SELECT 1", "extra"}},
		{"unknown connection subcommand", []string{"connection", "bogus"}},
		{"unknown schema subcommand", []string{"schema", "bogus"}},
		{"unknown query subcommand", []string{"query", "bogus"}},
		{"unknown credential subcommand", []string{"credential", "bogus"}},
		{"unknown config subcommand", []string{"config", "bogus"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stdout, stderr := runCLI(t, bin, tc.args...)
			if strings.TrimSpace(stderr) == "" {
				t.Fatalf("usage error produced no stderr (silent failure); stdout=%q", stdout)
			}
			var payload map[string]any
			if err := json.Unmarshal([]byte(stderr), &payload); err != nil {
				t.Fatalf("stderr not structured JSON: %v\n%s", err, stderr)
			}
			if payload["error"] == nil || payload["error"] == "" {
				t.Errorf("missing error message: %s", stderr)
			}
			if payload["fixable_by"] == nil {
				t.Errorf("missing fixable_by: %s", stderr)
			}
		})
	}
}

// TestCLIUnknownSubcommandExitsNonZero guards the per-group unknown-command
// handler: an unknown subcommand under a domain group must exit non-zero (it
// previously printed cobra usage text and exited 0).
func TestCLIUnknownSubcommandExitsNonZero(t *testing.T) {
	bin := buildBinary(t)

	for _, group := range []string{"connection", "schema", "query", "credential", "config"} {
		t.Run(group, func(t *testing.T) {
			cmd := exec.Command(bin, group, "bogus")
			cmd.Env = append(os.Environ(), "XDG_CONFIG_HOME="+t.TempDir())
			err := cmd.Run()
			if err == nil {
				t.Fatalf("%s bogus: expected non-zero exit, got 0", group)
			}
		})
	}
}

// TestCLIFormatScoping pins the per-command --format allow-listing that
// replaced the old global csv-aware validator: csv is valid only on query
// commands, sql only on schema dump, and everything else gets a structured
// fixable_by:agent rejection.
func TestCLIFormatScoping(t *testing.T) {
	bin := buildBinary(t)
	dbPath := setupTestDB(t)

	t.Run("csv accepted on query run", func(t *testing.T) {
		stdout, stderr := runCLI(t, bin, "run", "-c", dbPath, "--format", "csv", "SELECT id, name FROM users ORDER BY id LIMIT 1")
		lines := nonEmptyLines(stdout)
		if len(lines) != 2 || lines[0] != "id,name" {
			t.Fatalf("expected CSV header + row, got stdout: %q stderr: %q", stdout, stderr)
		}
	})

	t.Run("csv rejected on schema tables", func(t *testing.T) {
		stdout, stderr := runCLI(t, bin, "schema", "tables", "-c", dbPath, "--format", "csv")
		if strings.TrimSpace(stdout) != "" {
			t.Errorf("expected no stdout, got: %s", stdout)
		}
		assertStructuredFormatError(t, stderr, "csv")
	})

	t.Run("sql accepted on schema dump", func(t *testing.T) {
		stdout, _ := runCLI(t, bin, "schema", "dump", "-c", dbPath, "--format", "sql")
		if !strings.Contains(stdout, "CREATE TABLE") {
			t.Errorf("expected CREATE TABLE statements, got: %s", stdout)
		}
	})

	t.Run("sql rejected on query run", func(t *testing.T) {
		_, stderr := runCLI(t, bin, "run", "-c", dbPath, "--format", "sql", "SELECT 1")
		assertStructuredFormatError(t, stderr, "sql")
	})
}

func assertStructuredFormatError(t *testing.T, stderr, format string) {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(stderr)), &payload); err != nil {
		t.Fatalf("stderr is not one JSON error line: %v\n%s", err, stderr)
	}
	msg, _ := payload["error"].(string)
	if !strings.Contains(msg, "unknown format") || !strings.Contains(msg, format) {
		t.Errorf("error should reject format %q, got: %s", format, msg)
	}
	if payload["fixable_by"] != "agent" {
		t.Errorf("fixable_by = %v, want agent", payload["fixable_by"])
	}
}

// TestCLIWriteReceipt pins the write-path wire contract: a --write mutation
// returns one JSON line {"result":"ok","rows_affected":N,"command":...} with
// the family snake_case key (not the old rowsAffected).
func TestCLIWriteReceipt(t *testing.T) {
	bin := buildBinary(t)
	dbPath := setupTestDB(t)

	stdout, stderr := runCLI(t, bin, "run", "-c", dbPath, "--write", "UPDATE users SET age = age + 1 WHERE age IS NOT NULL")
	var receipt map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &receipt); err != nil {
		t.Fatalf("write receipt is not one JSON line: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}
	if receipt["result"] != "ok" {
		t.Errorf("result = %v, want ok", receipt["result"])
	}
	if receipt["rows_affected"] != float64(3) {
		t.Errorf("rows_affected = %v, want 3", receipt["rows_affected"])
	}
	if _, ok := receipt["rowsAffected"]; ok {
		t.Error("receipt must not contain the old camelCase rowsAffected key")
	}
	if receipt["command"] == nil {
		t.Error("receipt should include the command")
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
