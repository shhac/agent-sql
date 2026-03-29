package integration

import (
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

type dockerDriver struct {
	name    string
	connURL string
	port    string
}

var dockerDrivers = []dockerDriver{
	{"pg", "postgres://test:test@localhost:15432/testdb?sslmode=disable", "15432"},
	{"mysql", "mysql://root:test@localhost:13306/testdb", "13306"},
	{"mariadb", "mariadb://root:test@localhost:13307/testdb", "13307"},
	{"mssql", "mssql://SA:TestPass123!@localhost:11433/testdb", "11433"},
}

func isPortOpen(port string) bool {
	conn, err := net.DialTimeout("tcp", "localhost:"+port, 500*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func TestDockerQueryRun(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping docker integration tests in short mode")
	}
	bin := buildBinary(t)

	for _, drv := range dockerDrivers {
		t.Run(drv.name, func(t *testing.T) {
			if !isPortOpen(drv.port) {
				t.Skipf("%s container not available on port %s", drv.name, drv.port)
			}

			t.Run("returns NDJSON rows", func(t *testing.T) {
				stdout, stderr := runCLI(t, bin, "run", "-c", drv.connURL, "SELECT id, name FROM users ORDER BY id")
				if stderr != "" && strings.Contains(stderr, "error") {
					t.Fatalf("unexpected error: %s", stderr)
				}
				lines := nonEmptyLines(stdout)
				if len(lines) < 3 {
					t.Fatalf("expected at least 3 lines, got %d: %s", len(lines), stdout)
				}
				var row map[string]any
				json.Unmarshal([]byte(lines[0]), &row)
				if row["name"] != "Alice" {
					t.Errorf("first row name = %v, want Alice", row["name"])
				}
			})

			t.Run("respects --limit", func(t *testing.T) {
				stdout, _ := runCLI(t, bin, "run", "-c", drv.connURL, "--limit", "2", "SELECT * FROM users ORDER BY id")
				lines := nonEmptyLines(stdout)
				if len(lines) != 3 {
					t.Fatalf("expected 3 lines (2 rows + pagination), got %d: %s", len(lines), stdout)
				}
				var pag map[string]any
				json.Unmarshal([]byte(lines[2]), &pag)
				if pag["@pagination"] == nil {
					t.Error("expected @pagination on last line")
				}
			})

			t.Run("handles NULL values", func(t *testing.T) {
				stdout, _ := runCLI(t, bin, "run", "-c", drv.connURL, "SELECT email FROM users WHERE name = 'Bob'")
				lines := nonEmptyLines(stdout)
				if len(lines) < 1 {
					t.Fatal("expected at least 1 row")
				}
				var row map[string]any
				json.Unmarshal([]byte(lines[0]), &row)
				if row["email"] != nil {
					t.Errorf("expected null email, got %v", row["email"])
				}
			})

			t.Run("handles empty result", func(t *testing.T) {
				stdout, _ := runCLI(t, bin, "run", "-c", drv.connURL, "SELECT * FROM users WHERE id = 999")
				if strings.TrimSpace(stdout) != "" {
					t.Errorf("expected empty output, got: %s", stdout)
				}
			})

			t.Run("readonly blocks writes", func(t *testing.T) {
				_, stderr := runCLI(t, bin, "run", "-c", drv.connURL, "INSERT INTO users (name) VALUES ('Hacker')")
				if stderr == "" {
					t.Error("expected error for write on readonly connection")
				}
			})

			t.Run("handles unicode data", func(t *testing.T) {
				stdout, _ := runCLI(t, bin, "run", "-c", drv.connURL,
					fmt.Sprintf("SELECT name, bio FROM users WHERE name = '%s'", "Héloïse"))
				lines := nonEmptyLines(stdout)
				if len(lines) < 1 {
					t.Fatal("expected at least 1 row")
				}
				var row map[string]any
				json.Unmarshal([]byte(lines[0]), &row)
				if !strings.Contains(fmt.Sprint(row["bio"]), "日本語") {
					t.Errorf("expected unicode bio, got %v", row["bio"])
				}
			})

			t.Run("error on bad SQL", func(t *testing.T) {
				_, stderr := runCLI(t, bin, "run", "-c", drv.connURL, "SELEC * FROM users")
				if !strings.Contains(stderr, "fixable_by") {
					t.Errorf("expected fixable_by in stderr, got: %s", stderr)
				}
			})
		})
	}
}

func TestDockerSchemaTables(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping docker integration tests in short mode")
	}
	bin := buildBinary(t)

	for _, drv := range dockerDrivers {
		t.Run(drv.name, func(t *testing.T) {
			if !isPortOpen(drv.port) {
				t.Skipf("%s container not available on port %s", drv.name, drv.port)
			}

			stdout, _ := runCLI(t, bin, "schema", "tables", "-c", drv.connURL)
			var result map[string]any
			json.Unmarshal([]byte(stdout), &result)
			tables, ok := result["tables"].([]any)
			if !ok {
				t.Fatalf("expected tables array, got: %s", stdout)
			}
			if len(tables) < 2 {
				t.Errorf("expected at least 2 tables, got %d", len(tables))
			}
		})
	}
}

func TestDockerSchemaDescribe(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping docker integration tests in short mode")
	}
	bin := buildBinary(t)

	for _, drv := range dockerDrivers {
		t.Run(drv.name, func(t *testing.T) {
			if !isPortOpen(drv.port) {
				t.Skipf("%s container not available on port %s", drv.name, drv.port)
			}

			stdout, _ := runCLI(t, bin, "schema", "describe", "users", "-c", drv.connURL)
			var result map[string]any
			json.Unmarshal([]byte(stdout), &result)
			columns, ok := result["columns"].([]any)
			if !ok {
				t.Fatalf("expected columns array, got: %s", stdout)
			}
			if len(columns) < 4 {
				t.Errorf("expected at least 4 columns, got %d", len(columns))
			}
		})
	}
}

func TestDockerQueryCount(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping docker integration tests in short mode")
	}
	bin := buildBinary(t)

	for _, drv := range dockerDrivers {
		t.Run(drv.name, func(t *testing.T) {
			if !isPortOpen(drv.port) {
				t.Skipf("%s container not available on port %s", drv.name, drv.port)
			}

			stdout, _ := runCLI(t, bin, "query", "count", "users", "-c", drv.connURL)
			var result map[string]any
			json.Unmarshal([]byte(stdout), &result)
			count, ok := result["count"].(float64)
			if !ok || count != 5 {
				t.Errorf("expected count=5, got: %v", result["count"])
			}
		})
	}
}

func TestDockerQuerySample(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping docker integration tests in short mode")
	}
	bin := buildBinary(t)

	for _, drv := range dockerDrivers {
		t.Run(drv.name, func(t *testing.T) {
			if !isPortOpen(drv.port) {
				t.Skipf("%s container not available on port %s", drv.name, drv.port)
			}

			stdout, _ := runCLI(t, bin, "query", "sample", "users", "-c", drv.connURL, "--limit", "3")
			lines := nonEmptyLines(stdout)
			if len(lines) != 3 {
				t.Errorf("expected 3 sample rows, got %d: %s", len(lines), stdout)
			}
		})
	}
}
