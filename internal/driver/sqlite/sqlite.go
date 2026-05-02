// Package sqlite implements the SQLite driver using modernc.org/sqlite (pure Go).
package sqlite

import (
	"context"
	"database/sql"
	"strings"

	"github.com/shhac/agent-sql/internal/driver"
	_ "modernc.org/sqlite"
)

// Opts holds SQLite connection options.
type Opts struct {
	Path     string
	Readonly bool
	Create   bool
	// Options are driver-specific knobs threaded into the file: DSN as
	// query parameters (e.g. _journal_mode, _busy_timeout). Pass-through
	// to modernc.org/sqlite. The "mode" key is reserved -- we always
	// control read-only vs read-write at the URI level.
	Options map[string]string
}

var writeCommands = append(append([]string{}, driver.WriteCommands...), "REPLACE")

// Connect opens a SQLite database file.
func Connect(opts Opts) (driver.Connection, error) {
	db, err := sql.Open("sqlite", buildSqliteDSN(opts))
	if err != nil {
		return nil, classifyError(err)
	}

	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, classifyError(err)
	}

	return &sqliteConn{db: db, readonly: opts.Readonly}, nil
}

type sqliteConn struct {
	db       *sql.DB
	readonly bool
}

func (c *sqliteConn) QuoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func (c *sqliteConn) BuildSampleSelect(quotedTable, whereClause string, n int) string {
	return driver.SuffixLimitSelect(quotedTable, whereClause, n)
}

func (c *sqliteConn) Query(ctx context.Context, sqlStr string, opts driver.QueryOpts) (*driver.QueryResult, error) {
	cmd := driver.DetectCommand(sqlStr, writeCommands)
	if cmd != "" && opts.Write {
		result, err := c.db.ExecContext(ctx, sqlStr)
		if err != nil {
			return nil, classifyError(err)
		}
		affected, _ := result.RowsAffected()
		return &driver.QueryResult{
			Columns:      nil,
			Rows:         nil,
			RowsAffected: affected,
			Command:      cmd,
		}, nil
	}

	rows, err := c.db.QueryContext(ctx, sqlStr)
	if err != nil {
		return nil, classifyError(err)
	}

	result, err := driver.ScanAllRows(rows, driver.NormalizeValue)
	if err != nil {
		return nil, classifyError(err)
	}
	return result, nil
}

func (c *sqliteConn) QueryStream(ctx context.Context, sqlStr string, opts driver.QueryOpts) (*driver.StreamingResult, error) {
	cmd := driver.DetectCommand(sqlStr, writeCommands)
	if cmd != "" && opts.Write {
		result, err := c.db.ExecContext(ctx, sqlStr)
		if err != nil {
			return nil, classifyError(err)
		}
		affected, _ := result.RowsAffected()
		return &driver.StreamingResult{RowsAffected: affected, Command: cmd}, nil
	}

	rows, err := c.db.QueryContext(ctx, sqlStr)
	if err != nil {
		return nil, classifyError(err)
	}

	iter, err := driver.SQLRowsIterator(rows, driver.NormalizeValue)
	if err != nil {
		return nil, classifyError(err)
	}
	return &driver.StreamingResult{Iterator: iter}, nil
}

func (c *sqliteConn) Close() error {
	return c.db.Close()
}

