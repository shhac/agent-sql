// Package sqlite implements the SQLite driver using modernc.org/sqlite (pure Go).
package sqlite

import (
	"context"
	"database/sql"
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
}

var writeCommands = []string{
	"INSERT", "UPDATE", "DELETE", "REPLACE",
	"CREATE", "ALTER", "DROP", "TRUNCATE",
}

// Connect opens a SQLite database file.
func Connect(opts Opts) (driver.Connection, error) {
	dsn := "file:" + opts.Path
	if opts.Readonly {
		dsn += "?mode=ro"
	} else if opts.Create {
		dsn += "?mode=rwc"
	} else {
		dsn += "?mode=rw"
	}

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}

	// Verify the connection works
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
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
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	var results []map[string]any
	for rows.Next() {
		values := make([]any, len(columns))
		ptrs := make([]any, len(columns))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		row := make(map[string]any, len(columns))
		for i, col := range columns {
			row[col] = driver.NormalizeValue(values[i])
		}
		results = append(results, row)
	}
	if err := rows.Err(); err != nil {
		return nil, classifyError(err)
	}

	return &driver.QueryResult{Columns: columns, Rows: results}, nil
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
