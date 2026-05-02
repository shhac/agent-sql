package query

import (
	"bytes"
	"database/sql"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	_ "modernc.org/sqlite"

	"github.com/shhac/agent-sql/internal/cli/shared"
)

// seedSqlite creates a tempdir with a small sqlite database and returns
// the absolute path. The DB has a `users` table with two rows.
func seedSqlite(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "data.db")
	db, err := sql.Open("sqlite", "file:"+path+"?mode=rwc")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT)`); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO users VALUES (1, 'alice'), (2, 'bob')`); err != nil {
		t.Fatalf("insert: %v", err)
	}
	return path
}

// testRoot returns a fresh cobra root with the query commands. The
// global flags (like --connection) are bound to a single GlobalFlags
// struct that the tests can mutate.
func testRoot(t *testing.T, g *shared.GlobalFlags) *cobra.Command {
	t.Helper()
	root := &cobra.Command{
		Use:           "agent-sql",
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	Register(root, func() *shared.GlobalFlags { return g })
	return root
}

func captureStdout(t *testing.T) (*bytes.Buffer, func()) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	prev := os.Stdout
	os.Stdout = w
	buf := &bytes.Buffer{}
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(buf, r)
		close(done)
	}()
	return buf, func() {
		_ = w.Close()
		<-done
		os.Stdout = prev
		_ = r.Close()
	}
}

func captureStderr(t *testing.T) (*bytes.Buffer, func()) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	prev := os.Stderr
	os.Stderr = w
	buf := &bytes.Buffer{}
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(buf, r)
		close(done)
	}()
	return buf, func() {
		_ = w.Close()
		<-done
		os.Stderr = prev
		_ = r.Close()
	}
}

// TestQueryRunSucceeds verifies a basic SELECT through the CLI.
func TestQueryRunSucceeds(t *testing.T) {
	dbPath := seedSqlite(t)
	g := &shared.GlobalFlags{Connection: dbPath}

	stdout, restore := captureStdout(t)

	root := testRoot(t, g)
	root.SetArgs([]string{"query", "run", "SELECT * FROM users ORDER BY id"})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	err := root.Execute()
	restore()

	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "alice") || !strings.Contains(out, "bob") {
		t.Errorf("expected both rows in output; got: %s", out)
	}
}

// TestQueryRunBadSQLExitsNonZero pins the A1 contract: a bad query
// hard-exits.
func TestQueryRunBadSQLExitsNonZero(t *testing.T) {
	dbPath := seedSqlite(t)
	g := &shared.GlobalFlags{Connection: dbPath}

	stderr, restore := captureStderr(t)

	root := testRoot(t, g)
	root.SetArgs([]string{"query", "run", "SELECT * FROM no_such_table"})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	err := root.Execute()
	restore()

	if err == nil {
		t.Fatal("expected non-nil error from bad SQL")
	}
	if !strings.Contains(stderr.String(), `"error"`) {
		t.Errorf("stderr should be JSON error; got: %s", stderr.String())
	}
}

// TestQueryExplainAnalyzeRejectsWriteSQL pins the analyze safety: an
// EXPLAIN ANALYZE on a write query is rejected because ANALYZE runs the
// query.
func TestQueryExplainAnalyzeRejectsWriteSQL(t *testing.T) {
	dbPath := seedSqlite(t)
	g := &shared.GlobalFlags{Connection: dbPath}

	stderr, restore := captureStderr(t)

	root := testRoot(t, g)
	root.SetArgs([]string{"query", "explain", "DELETE FROM users", "--analyze"})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	err := root.Execute()
	restore()

	if err == nil {
		t.Fatal("expected rejection")
	}
	if !strings.Contains(stderr.String(), "EXPLAIN ANALYZE is not allowed") {
		t.Errorf("stderr should explain rejection; got: %s", stderr.String())
	}
}

// TestQuerySampleAppliesLimit verifies --limit on sample.
func TestQuerySampleAppliesLimit(t *testing.T) {
	dbPath := seedSqlite(t)
	g := &shared.GlobalFlags{Connection: dbPath}

	stdout, restore := captureStdout(t)

	root := testRoot(t, g)
	root.SetArgs([]string{"query", "sample", "users", "--limit", "1"})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	err := root.Execute()
	restore()

	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	out := stdout.String()
	// Should have one row only.
	if strings.Count(out, "alice")+strings.Count(out, "bob") != 1 {
		t.Errorf("expected exactly one user in output; got: %s", out)
	}
}

// TestQueryCountReturnsCount verifies count on the seeded table.
func TestQueryCountReturnsCount(t *testing.T) {
	dbPath := seedSqlite(t)
	g := &shared.GlobalFlags{Connection: dbPath}

	stdout, restore := captureStdout(t)

	root := testRoot(t, g)
	root.SetArgs([]string{"query", "count", "users"})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	err := root.Execute()
	restore()

	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(stdout.String(), `"count": 2`) {
		t.Errorf("expected count: 2; got: %s", stdout.String())
	}
}
