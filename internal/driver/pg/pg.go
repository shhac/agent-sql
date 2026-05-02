// Package pg implements the PostgreSQL driver using pgx/v5 directly.
// Also used by CockroachDB (via the cockroachdb wrapper package).
package pg

import (
	"context"
	"fmt"
	"net/url"
	"strconv"

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
	// Options are driver-specific knobs threaded into the pgx connection
	// URL as query parameters. Pass-through: pgx is the source of truth
	// for which keys are valid.
	Options map[string]string
}

// DefaultPort is the standard PostgreSQL port.
const DefaultPort = 5432

var writeCommands = driver.WriteCommands

// Connect opens a PostgreSQL connection using pgx directly.
func Connect(ctx context.Context, opts Opts) (driver.Connection, error) {
	if opts.Port == 0 {
		opts.Port = DefaultPort
	}

	conn, err := pgx.Connect(ctx, buildPgURL(opts))
	if err != nil {
		return nil, classifyError(err)
	}

	if opts.Readonly {
		if _, err := conn.Exec(ctx, "SET default_transaction_read_only = on"); err != nil {
			_ = conn.Close(ctx)
			return nil, classifyError(err)
		}
	}

	return &pgConn{conn: conn, readonly: opts.Readonly}, nil
}

// buildPgURL renders an Opts into a postgres:// URL. sslmode defaults to
// "prefer" but is overridden if the caller supplies sslmode in Options.
func buildPgURL(opts Opts) string {
	u := &url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(opts.Username, opts.Password),
		Host:   opts.Host + ":" + strconv.Itoa(opts.Port),
		Path:   "/" + opts.Database,
	}
	q := u.Query()
	if _, ok := opts.Options["sslmode"]; !ok {
		q.Set("sslmode", "prefer")
	}
	for k, v := range opts.Options {
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// ConnectURL opens a PostgreSQL connection from a connection URL.
func ConnectURL(ctx context.Context, url string, readonly bool) (driver.Connection, error) {
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		return nil, classifyError(err)
	}

	if readonly {
		if _, err := conn.Exec(ctx, "SET default_transaction_read_only = on"); err != nil {
			_ = conn.Close(ctx)
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
			_, _ = c.conn.Exec(ctx, "ROLLBACK")
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
			_, _ = c.conn.Exec(ctx, "ROLLBACK")
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
				_, _ = conn.Exec(ctx, "ROLLBACK")
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
