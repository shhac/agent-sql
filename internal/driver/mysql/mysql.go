// Package mysql implements the MySQL/MariaDB driver using go-sql-driver/mysql.
package mysql

import (
	"context"
	"database/sql"
	"fmt"
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
}

var writeCommands = []string{
	"INSERT", "UPDATE", "DELETE", "REPLACE",
	"CREATE", "ALTER", "DROP", "TRUNCATE",
}

// Connect opens a MySQL or MariaDB connection.
func Connect(opts Opts) (driver.Connection, error) {
	if opts.Port == 0 {
		opts.Port = 3306
	}
	if opts.Variant == "" {
		opts.Variant = "mysql"
	}

	cfg := gomysql.NewConfig()
	cfg.User = opts.Username
	cfg.Passwd = opts.Password
	cfg.Net = "tcp"
	cfg.Addr = fmt.Sprintf("%s:%d", opts.Host, opts.Port)
	cfg.DBName = opts.Database
	cfg.MultiStatements = false

	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		return nil, classifyError(err)
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, classifyError(err)
	}

	if opts.Readonly {
		if _, err := db.Exec("SET SESSION TRANSACTION READ ONLY"); err != nil {
			db.Close()
			return nil, classifyError(err)
		}
	}

	return &mysqlConn{db: db, readonly: opts.Readonly, variant: opts.Variant}, nil
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
		tx.Rollback()
		return nil, classifyError(err)
	}

	result, err := scanRows(rows)
	rows.Close()
	if err != nil {
		tx.Rollback()
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
	defer rows.Close()

	return scanRows(rows)
}

func scanRows(rows *sql.Rows) (*driver.QueryResult, error) {
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
			tx.Rollback()
			return nil, classifyError(err)
		}

		iter, err := driver.SQLRowsIterator(rows, driver.NormalizeValue)
		if err != nil {
			tx.Rollback()
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
				tx.Commit()
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
