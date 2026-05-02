// Package duckdb implements the DuckDB driver as a subprocess.
// Each query spawns a fresh `duckdb` CLI process with NDJSON output.
package duckdb

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/shhac/agent-sql/internal/driver"
	"github.com/shhac/agent-sql/internal/errors"
)

// Opts holds DuckDB connection options.
type Opts struct {
	Path     string // empty means in-memory
	Readonly bool
	// Options are applied as `SET key='value';` statements prepended to
	// every query (DuckDB is subprocess-based -- no persistent session).
	// The reserved key "extensions" is comma-separated and translates to
	// `INSTALL <ext>; LOAD <ext>;` per item. DuckDB rejects unknown
	// settings or bad values at execution time.
	Options map[string]string
}

var writeCommands = []string{
	"INSERT", "UPDATE", "DELETE", "CREATE", "DROP",
	"ALTER", "COPY", "TRUNCATE", "MERGE",
}

// Connect verifies the DuckDB CLI is available and the database is accessible.
func Connect(ctx context.Context, opts Opts) (driver.Connection, error) {
	bin := findBin()

	// Verify CLI exists
	if _, err := exec.LookPath(bin); err != nil {
		return nil, errors.New(
			fmt.Sprintf("DuckDB CLI not found (%s). Install with: brew install duckdb", bin),
			errors.FixableByHuman,
		).WithHint("DuckDB requires the duckdb CLI on PATH. Set AGENT_SQL_DUCKDB_PATH to use a custom location.")
	}

	conn := &duckdbConn{
		bin:      bin,
		path:     opts.Path,
		readonly: opts.Readonly,
		prelude:  buildOptionsPrelude(opts.Options),
	}

	// Verify database is accessible
	if _, err := conn.exec(ctx, "SELECT 1"); err != nil {
		return nil, err
	}

	return conn, nil
}

type duckdbConn struct {
	bin      string
	path     string
	readonly bool
	prelude  string // SET / INSTALL+LOAD statements run before every query
}

// Options-prelude rendering (buildOptionsPrelude, writeExtensions,
// writeSetStmt) lives in options.go.

func findBin() string {
	if custom := os.Getenv("AGENT_SQL_DUCKDB_PATH"); custom != "" {
		return custom
	}
	return "duckdb"
}

func (c *duckdbConn) Query(ctx context.Context, sqlStr string, opts driver.QueryOpts) (*driver.QueryResult, error) {
	cmd := driver.DetectCommand(sqlStr, writeCommands)
	if cmd != "" && opts.Write {
		if err := c.execWrite(ctx, sqlStr); err != nil {
			return nil, err
		}
		return &driver.QueryResult{
			Columns:      nil,
			Rows:         nil,
			RowsAffected: 0,
			Command:      cmd,
		}, nil
	}

	rows, err := c.exec(ctx, sqlStr)
	if err != nil {
		return nil, err
	}

	var columns []string
	if len(rows) > 0 {
		columns = orderedKeys(rows[0])
	}

	return &driver.QueryResult{Columns: columns, Rows: rows}, nil
}

// orderedKeys returns map keys. Go maps don't preserve insertion order,
// so we just return all keys.
func orderedKeys(row map[string]any) []string {
	keys := make([]string, 0, len(row))
	for k := range row {
		keys = append(keys, k)
	}
	return keys
}

func (c *duckdbConn) QuoteIdent(name string) string {
	return driver.QuoteIdentDot(name)
}

func (c *duckdbConn) BuildSampleSelect(quotedTable, whereClause string, n int) string {
	return driver.SuffixLimitSelect(quotedTable, whereClause, n)
}

func (c *duckdbConn) Close() error {
	return nil
}
