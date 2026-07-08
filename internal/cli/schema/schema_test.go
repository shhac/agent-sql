package schema

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

	agentout "github.com/shhac/lib-agent-output"
)

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
	if _, err := db.Exec(`CREATE TABLE orders (id INTEGER PRIMARY KEY, user_id INTEGER REFERENCES users(id))`); err != nil {
		t.Fatalf("create: %v", err)
	}
	return path
}

func testRoot(t *testing.T, dbPath string) *cobra.Command {
	t.Helper()
	root := &cobra.Command{
		Use:           "agent-sql",
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	Register(root, func() SchemaGlobals {
		return SchemaGlobals{Connection: dbPath}
	})
	return root
}

// execute runs root and renders any bubbled error to stderr exactly as the
// production main (libcli.Run) does, then returns it.
func execute(root *cobra.Command) error {
	if err := root.Execute(); err != nil {
		agentout.WriteError(os.Stderr, err)
		return err
	}
	return nil
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

// TestSchemaTablesLists tables from a sqlite database.
func TestSchemaTablesLists(t *testing.T) {
	dbPath := seedSqlite(t)

	stdout, restore := captureStdout(t)

	root := testRoot(t, dbPath)
	root.SetArgs([]string{"schema", "tables"})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	err := root.Execute()
	restore()

	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "users") || !strings.Contains(out, "orders") {
		t.Errorf("expected both tables in output; got: %s", out)
	}
}

// TestSchemaConstraintsInvalidTypeExitsNonZero pins the A1 contract.
func TestSchemaConstraintsInvalidTypeExitsNonZero(t *testing.T) {
	dbPath := seedSqlite(t)

	stderr, restore := captureStderr(t)

	root := testRoot(t, dbPath)
	root.SetArgs([]string{"schema", "constraints", "--type", "invalid"})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	err := execute(root)
	restore()

	if err == nil {
		t.Fatal("expected error for invalid constraint type")
	}
	if !strings.Contains(stderr.String(), "invalid constraint type") {
		t.Errorf("stderr should explain rejection; got: %s", stderr.String())
	}
}

// TestSchemaDescribeTable verifies describe of a known table.
func TestSchemaDescribeTable(t *testing.T) {
	dbPath := seedSqlite(t)

	stdout, restore := captureStdout(t)

	root := testRoot(t, dbPath)
	root.SetArgs([]string{"schema", "describe", "users"})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	err := root.Execute()
	restore()

	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "id") || !strings.Contains(out, "name") {
		t.Errorf("expected id and name columns; got: %s", out)
	}
}
