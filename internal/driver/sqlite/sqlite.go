// Package sqlite implements the SQLite driver using modernc.org/sqlite (pure Go).
package sqlite

import (
	"context"
	"database/sql"
	"net/url"
	"strings"

	"github.com/shhac/agent-sql/internal/driver"
	"github.com/shhac/agent-sql/internal/errors"
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

// buildSqliteDSN renders Opts into a `file:path?mode=...&_pragma=...` DSN.
// User-supplied "mode" is dropped -- read-only enforcement is non-negotiable.
func buildSqliteDSN(opts Opts) string {
	q := url.Values{}
	for k, v := range opts.Options {
		if k == "mode" {
			continue
		}
		q.Set(k, v)
	}
	switch {
	case opts.Readonly:
		q.Set("mode", "ro")
	case opts.Create:
		q.Set("mode", "rwc")
	default:
		q.Set("mode", "rw")
	}
	return "file:" + opts.Path + "?" + q.Encode()
}

type sqliteConn struct {
	db       *sql.DB
	readonly bool
}

func (c *sqliteConn) QuoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
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

func classifyError(err error) error {
	msg := err.Error()
	if strings.Contains(msg, "attempt to write a readonly database") {
		return errors.New(msg, errors.FixableByHuman).
			WithHint(errors.HintReadOnly)
	}
	if strings.Contains(msg, "database is locked") {
		return errors.New(msg, errors.FixableByRetry).
			WithHint("The database is locked by another process. Try again shortly.")
	}
	if strings.Contains(msg, "no such table") {
		return errors.New(msg, errors.FixableByAgent).
			WithHint(errors.HintTableNotFound)
	}
	if strings.Contains(msg, "no such column") {
		return errors.New(msg, errors.FixableByAgent).
			WithHint(errors.HintColumnNotFound)
	}
	return errors.Wrap(err, errors.FixableByAgent)
}
