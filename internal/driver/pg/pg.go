// Package pg implements the PostgreSQL driver using pgx/v5 directly.
// Also used by CockroachDB (via the cockroachdb wrapper package).
package pg

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/shhac/agent-sql/internal/driver"
)

// Opts holds PostgreSQL connection options.
type Opts struct {
	Host     string
	Port     int
	Database string
	Username string
	Password string
	Readonly bool
}

// DefaultPort is the standard PostgreSQL port.
const DefaultPort = 5432

var writeCommands = driver.WriteCommands

// Connect opens a PostgreSQL connection using pgx directly.
func Connect(ctx context.Context, opts Opts) (driver.Connection, error) {
	if opts.Port == 0 {
		opts.Port = DefaultPort
	}

	connStr := fmt.Sprintf(
		"host=%s port=%d dbname=%s user=%s password=%s sslmode=prefer",
		opts.Host, opts.Port, opts.Database, opts.Username, opts.Password,
	)

	conn, err := pgx.Connect(ctx, connStr)
	if err != nil {
		return nil, classifyError(err)
	}

	if opts.Readonly {
		if _, err := conn.Exec(ctx, "SET default_transaction_read_only = on"); err != nil {
			conn.Close(ctx)
			return nil, classifyError(err)
		}
	}

	return &pgConn{conn: conn, readonly: opts.Readonly}, nil
}

// ConnectURL opens a PostgreSQL connection from a connection URL.
func ConnectURL(ctx context.Context, url string, readonly bool) (driver.Connection, error) {
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		return nil, classifyError(err)
	}

	if readonly {
		if _, err := conn.Exec(ctx, "SET default_transaction_read_only = on"); err != nil {
			conn.Close(ctx)
			return nil, classifyError(err)
		}
	}

	return &pgConn{conn: conn, readonly: readonly}, nil
}

type pgConn struct {
	conn     *pgx.Conn
	readonly bool
}

func (c *pgConn) QuoteIdent(name string) string {
	return driver.QuoteIdentDot(name)
}

func (c *pgConn) Query(ctx context.Context, sqlStr string, opts driver.QueryOpts) (*driver.QueryResult, error) {
	if c.readonly {
		if err := driver.GuardReadOnly(sqlStr); err != nil {
			return nil, err
		}
	}

	cmd := driver.DetectCommand(sqlStr, writeCommands)
	if cmd != "" && opts.Write {
		tag, err := c.conn.Exec(ctx, sqlStr)
		if err != nil {
			return nil, classifyError(err)
		}
		return &driver.QueryResult{
			RowsAffected: tag.RowsAffected(),
			Command:      cmd,
		}, nil
	}

	// Use BEGIN READ ONLY for defense in depth on read-only connections
	if c.readonly {
		if _, err := c.conn.Exec(ctx, "BEGIN READ ONLY"); err != nil {
			return nil, classifyError(err)
		}
		defer func() {
			// Always rollback — we're only reading
			c.conn.Exec(ctx, "ROLLBACK")
		}()
	}

	rows, err := c.conn.Query(ctx, sqlStr)
	if err != nil {
		return nil, classifyError(err)
	}
	defer rows.Close()

	columns := fieldNames(rows.FieldDescriptions())

	var results []map[string]any
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			return nil, classifyError(err)
		}
		row := make(map[string]any, len(columns))
		for i, col := range columns {
			row[col] = normalizeValue(values[i])
		}
		results = append(results, row)
	}
	if err := rows.Err(); err != nil {
		return nil, classifyError(err)
	}

	return &driver.QueryResult{Columns: columns, Rows: results}, nil
}

func (c *pgConn) QueryStream(ctx context.Context, sqlStr string, opts driver.QueryOpts) (*driver.StreamingResult, error) {
	if c.readonly {
		if err := driver.GuardReadOnly(sqlStr); err != nil {
			return nil, err
		}
	}

	cmd := driver.DetectCommand(sqlStr, writeCommands)
	if cmd != "" && opts.Write {
		tag, err := c.conn.Exec(ctx, sqlStr)
		if err != nil {
			return nil, classifyError(err)
		}
		return &driver.StreamingResult{
			RowsAffected: tag.RowsAffected(),
			Command:      cmd,
		}, nil
	}

	if c.readonly {
		if _, err := c.conn.Exec(ctx, "BEGIN READ ONLY"); err != nil {
			return nil, classifyError(err)
		}
	}

	rows, err := c.conn.Query(ctx, sqlStr)
	if err != nil {
		if c.readonly {
			c.conn.Exec(ctx, "ROLLBACK")
		}
		return nil, classifyError(err)
	}

	columns := fieldNames(rows.FieldDescriptions())
	needsRollback := c.readonly
	conn := c.conn

	iter := driver.NewRowIterator(
		columns,
		func() bool { return rows.Next() },
		func() (map[string]any, error) {
			values, err := rows.Values()
			if err != nil {
				return nil, classifyError(err)
			}
			row := make(map[string]any, len(columns))
			for i, col := range columns {
				row[col] = normalizeValue(values[i])
			}
			return row, nil
		},
		func() error { return rows.Err() },
		func() error {
			rows.Close()
			if needsRollback {
				conn.Exec(ctx, "ROLLBACK")
			}
			return nil
		},
	)

	return &driver.StreamingResult{Iterator: iter}, nil
}

func (c *pgConn) Close() error {
	return c.conn.Close(context.Background())
}

// splitSchemaTable splits "schema.table" into parts, defaulting to "public".
func splitSchemaTable(name string) (string, string) {
	return driver.SplitSchemaTable(name, "public")
}

func fieldNames(fds []pgconn.FieldDescription) []string {
	names := make([]string, len(fds))
	for i, fd := range fds {
		names[i] = fd.Name
	}
	return names
}

func normalizeValue(v any) any {
	switch val := v.(type) {
	case []byte:
		return string(val)
	case [16]byte:
		// UUID from pgx comes as [16]byte
		return fmt.Sprintf("%x-%x-%x-%x-%x", val[0:4], val[4:6], val[6:8], val[8:10], val[10:16])
	default:
		return val
	}
}
