// Package mssql implements the Microsoft SQL Server driver using database/sql.
package mssql

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strings"

	_ "github.com/microsoft/go-mssqldb"
	"github.com/shhac/agent-sql/internal/driver"
	"github.com/shhac/agent-sql/internal/errors"
)

// Opts holds MSSQL connection options.
type Opts struct {
	Host     string
	Port     int
	Database string
	Username string
	Password string
	Readonly bool
	// Options are driver-specific knobs threaded into the sqlserver://
	// URL as query parameters. Pass-through: go-mssqldb is the source of
	// truth for which keys are valid.
	Options map[string]string
}

// MSSQL-specific write commands extend the shared set with EXEC/EXECUTE.
var writeCommands = append(
	append([]string{}, driver.WriteCommands...),
	"EXEC", "EXECUTE",
)

// Connect opens a connection to a MSSQL database.
func Connect(opts Opts) (driver.Connection, error) {
	if opts.Port == 0 {
		opts.Port = 1433
	}

	db, err := sql.Open("sqlserver", buildMssqlURL(opts))
	if err != nil {
		return nil, err
	}

	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, classifyError(err)
	}

	return &mssqlConn{db: db, readonly: opts.Readonly}, nil
}

// buildMssqlURL constructs the sqlserver:// URL handed to go-mssqldb.
//
// Collision policy:
//   - User options are applied first (pass-through to go-mssqldb).
//   - "app name" defaults to "agent-sql" only if the user didn't supply one.
//   - "database" always uses opts.Database -- a user --option database=foo
//     cannot override the connection target.
//
// Other unknown keys pass through verbatim; go-mssqldb decides which
// are valid at connect time.
func buildMssqlURL(opts Opts) string {
	q := url.Values{}
	for k, v := range opts.Options {
		q.Set(k, v)
	}
	if q.Get("app name") == "" {
		q.Set("app name", "agent-sql")
	}
	if opts.Database != "" {
		q.Set("database", opts.Database) // connection target wins
	}
	u := &url.URL{
		Scheme:   "sqlserver",
		User:     url.UserPassword(opts.Username, opts.Password),
		Host:     fmt.Sprintf("%s:%d", opts.Host, opts.Port),
		RawQuery: q.Encode(),
	}
	return u.String()
}

type mssqlConn struct {
	db       *sql.DB
	readonly bool
}

func (c *mssqlConn) QuoteIdent(name string) string {
	var parts []string
	for _, part := range strings.Split(name, ".") {
		escaped := strings.ReplaceAll(part, "]", "]]")
		parts = append(parts, "["+escaped+"]")
	}
	return strings.Join(parts, ".")
}

func (c *mssqlConn) Query(ctx context.Context, sqlStr string, opts driver.QueryOpts) (*driver.QueryResult, error) {
	if c.readonly && !opts.Write {
		if err := guardReadOnly(sqlStr); err != nil {
			return nil, err
		}
	}

	cmd := driver.DetectCommand(sqlStr, writeCommands)
	if cmd != "" && opts.Write {
		result, err := c.db.ExecContext(ctx, sqlStr)
		if err != nil {
			return nil, classifyError(err)
		}
		affected, _ := result.RowsAffected()
		return &driver.QueryResult{
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

func (c *mssqlConn) QueryStream(ctx context.Context, sqlStr string, opts driver.QueryOpts) (*driver.StreamingResult, error) {
	if c.readonly && !opts.Write {
		if err := guardReadOnly(sqlStr); err != nil {
			return nil, err
		}
	}

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

func (c *mssqlConn) Close() error {
	return c.db.Close()
}

// guardReadOnly uses the shared keyword guard plus MSSQL-specific EXEC/EXECUTE.
func guardReadOnly(sqlStr string) error {
	// Use the shared guard first (covers SELECT INTO, FOR UPDATE, etc.)
	if err := driver.GuardReadOnly(sqlStr); err != nil {
		return err
	}

	// Check MSSQL-specific write commands
	cmd := driver.DetectCommand(sqlStr, []string{"EXEC", "EXECUTE"})
	if cmd != "" {
		return errors.New(
			"Statement blocked: "+cmd+" is not allowed in read-only mode.",
			errors.FixableByHuman,
		).WithHint("This connection is read-only. To enable writes, use a credential with writePermission and pass --write. For production safety, grant only the db_datareader role.")
	}

	return nil
}

func splitSchemaTable(name string) (string, string) {
	return driver.SplitSchemaTable(name, "dbo")
}

func mapConstraintType(typ string) driver.ConstraintType {
	if ct := driver.MapConstraintType(typ); ct != "" {
		return ct
	}
	return driver.ConstraintType(strings.ToLower(typ))
}
