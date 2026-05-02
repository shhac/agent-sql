// Package mysql implements the MySQL/MariaDB driver using go-sql-driver/mysql.
package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strings"

	gomysql "github.com/go-sql-driver/mysql"
	"github.com/shhac/agent-sql/internal/driver"
)

// Opts holds MySQL/MariaDB connection options.
type Opts struct {
	Host     string
	Port     int
	Database string
	Username string
	Password string
	Readonly bool
	Variant  string // "mysql" or "mariadb"
	// Options are driver-specific knobs threaded into gomysql.Config via
	// gomysql.ParseDSN. Pass-through: gomysql is the source of truth for
	// which keys are valid (and how each one is typed -- ParseTime is a
	// bool, Loc is a *time.Location, etc.).
	Options map[string]string
}

var writeCommands = append(append([]string{}, driver.WriteCommands...), "REPLACE")

// Connect opens a MySQL or MariaDB connection.
func Connect(opts Opts) (driver.Connection, error) {
	if opts.Port == 0 {
		opts.Port = 3306
	}
	if opts.Variant == "" {
		opts.Variant = "mysql"
	}

	cfg, err := buildMysqlConfig(opts)
	if err != nil {
		return nil, classifyError(err)
	}

	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		return nil, classifyError(err)
	}

	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, classifyError(err)
	}

	if opts.Readonly {
		if _, err := db.Exec("SET SESSION TRANSACTION READ ONLY"); err != nil {
			_ = db.Close()
			return nil, classifyError(err)
		}
	}

	return &mysqlConn{db: db, readonly: opts.Readonly, variant: opts.Variant}, nil
}

// buildMysqlConfig parses opts.Options through gomysql.ParseDSN (which
// validates each key and types every typed field) and overlays the
// connection-target and safety fields on top. Pass-through for unknowns
// via Config.Params; gives free upgrades whenever gomysql adds new
// options.
func buildMysqlConfig(opts Opts) (*gomysql.Config, error) {
	cfg := gomysql.NewConfig()
	if len(opts.Options) > 0 {
		q := url.Values{}
		for k, v := range opts.Options {
			q.Set(k, v)
		}
		parsed, err := gomysql.ParseDSN("/?" + q.Encode())
		if err != nil {
			return nil, err
		}
		cfg = parsed
	}
	cfg.User = opts.Username
	cfg.Passwd = opts.Password
	cfg.Net = "tcp"
	cfg.Addr = fmt.Sprintf("%s:%d", opts.Host, opts.Port)
	cfg.DBName = opts.Database
	cfg.MultiStatements = false // never allow, regardless of user input
	return cfg, nil
}

type mysqlConn struct {
	db       *sql.DB
	readonly bool
	variant  string
}

func (c *mysqlConn) QuoteIdent(name string) string {
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}

func (c *mysqlConn) Query(ctx context.Context, sqlStr string, opts driver.QueryOpts) (*driver.QueryResult, error) {
	cmd := driver.DetectCommand(sqlStr, writeCommands)

	if cmd != "" && opts.Write && !c.readonly {
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

	if c.readonly {
		return c.queryReadonly(ctx, sqlStr)
	}

	return c.queryRows(ctx, sqlStr)
}

func (c *mysqlConn) queryReadonly(ctx context.Context, sqlStr string) (*driver.QueryResult, error) {
	tx, err := c.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, classifyError(err)
	}

	rows, err := tx.QueryContext(ctx, sqlStr)
	if err != nil {
		_ = tx.Rollback()
		return nil, classifyError(err)
	}

	result, err := scanRows(rows)
	_ = rows.Close()
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, classifyError(err)
	}

	return result, nil
}

func (c *mysqlConn) queryRows(ctx context.Context, sqlStr string) (*driver.QueryResult, error) {
	rows, err := c.db.QueryContext(ctx, sqlStr)
	if err != nil {
		return nil, classifyError(err)
	}
	defer func() { _ = rows.Close() }()

	return scanRows(rows)
}

func scanRows(rows *sql.Rows) (*driver.QueryResult, error) {
	return driver.ScanAllRows(rows, driver.NormalizeValue)
}

func (c *mysqlConn) QueryStream(ctx context.Context, sqlStr string, opts driver.QueryOpts) (*driver.StreamingResult, error) {
	cmd := driver.DetectCommand(sqlStr, writeCommands)

	if cmd != "" && opts.Write && !c.readonly {
		result, err := c.db.ExecContext(ctx, sqlStr)
		if err != nil {
			return nil, classifyError(err)
		}
		affected, _ := result.RowsAffected()
		return &driver.StreamingResult{RowsAffected: affected, Command: cmd}, nil
	}

	if c.readonly {
		tx, err := c.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
		if err != nil {
			return nil, classifyError(err)
		}

		rows, err := tx.QueryContext(ctx, sqlStr)
		if err != nil {
			_ = tx.Rollback()
			return nil, classifyError(err)
		}

		iter, err := driver.SQLRowsIterator(rows, driver.NormalizeValue)
		if err != nil {
			_ = tx.Rollback()
			return nil, classifyError(err)
		}

		// Wrap the iterator to commit the transaction on close
		origClose := iter.Close
		wrapped := driver.NewRowIterator(
			iter.Columns(),
			iter.Next,
			iter.Scan,
			iter.Err,
			func() error {
				closeErr := origClose()
				_ = tx.Commit()
				return closeErr
			},
		)
		return &driver.StreamingResult{Iterator: wrapped}, nil
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

func (c *mysqlConn) Close() error {
	return c.db.Close()
}

// normalizeValue delegates to the shared helper.
// Kept as a package-level function for test compatibility.
func normalizeValue(v any) any {
	return driver.NormalizeValue(v)
}
