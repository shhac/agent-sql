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

	q := url.Values{}
	if opts.Database != "" {
		q.Set("database", opts.Database)
	}
	q.Set("app name", "agent-sql")

	u := &url.URL{
		Scheme:   "sqlserver",
		User:     url.UserPassword(opts.Username, opts.Password),
		Host:     fmt.Sprintf("%s:%d", opts.Host, opts.Port),
		RawQuery: q.Encode(),
	}

	db, err := sql.Open("sqlserver", u.String())
	if err != nil {
		return nil, err
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, classifyError(err)
	}

	return &mssqlConn{db: db, readonly: opts.Readonly}, nil
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
