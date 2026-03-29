// Package pg implements the PostgreSQL driver using pgx/v5 directly.
// Also used by CockroachDB (via the cockroachdb wrapper package).
package pg

import (
	"context"
	stderrors "errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/shhac/agent-sql/internal/driver"
	"github.com/shhac/agent-sql/internal/errors"
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
	parts := strings.Split(name, ".")
	for i, p := range parts {
		parts[i] = `"` + strings.ReplaceAll(p, `"`, `""`) + `"`
	}
	return strings.Join(parts, ".")
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

func (c *pgConn) GetTables(ctx context.Context, includeSystem bool) ([]driver.TableInfo, error) {
	query := `
		SELECT table_schema, table_name, table_type
		FROM information_schema.tables
		WHERE table_schema NOT IN ('pg_catalog', 'information_schema')
		ORDER BY table_schema, table_name`
	if includeSystem {
		query = `
			SELECT table_schema, table_name, table_type
			FROM information_schema.tables
			ORDER BY table_schema, table_name`
	}

	rows, err := c.conn.Query(ctx, query)
	if err != nil {
		return nil, classifyError(err)
	}
	defer rows.Close()

	var tables []driver.TableInfo
	for rows.Next() {
		var schema, name, typ string
		if err := rows.Scan(&schema, &name, &typ); err != nil {
			return nil, err
		}
		t := "table"
		if typ == "VIEW" {
			t = "view"
		}
		tables = append(tables, driver.TableInfo{
			Name:   schema + "." + name,
			Schema: schema,
			Type:   t,
		})
	}
	return tables, rows.Err()
}

func (c *pgConn) DescribeTable(ctx context.Context, table string) ([]driver.ColumnInfo, error) {
	schema, tbl := splitSchemaTable(table)

	query := `
		SELECT c.column_name, c.data_type, c.is_nullable, c.column_default,
		       CASE WHEN pk.column_name IS NOT NULL THEN true ELSE false END AS is_pk
		FROM information_schema.columns c
		LEFT JOIN (
			SELECT kcu.column_name
			FROM information_schema.table_constraints tc
			JOIN information_schema.key_column_usage kcu
			  ON tc.constraint_name = kcu.constraint_name
			  AND tc.table_schema = kcu.table_schema
			WHERE tc.constraint_type = 'PRIMARY KEY'
			  AND tc.table_schema = $1
			  AND tc.table_name = $2
		) pk ON pk.column_name = c.column_name
		WHERE c.table_schema = $1 AND c.table_name = $2
		ORDER BY c.ordinal_position`

	rows, err := c.conn.Query(ctx, query, schema, tbl)
	if err != nil {
		return nil, classifyError(err)
	}
	defer rows.Close()

	var columns []driver.ColumnInfo
	for rows.Next() {
		var name, dataType, nullable string
		var defaultVal *string
		var isPK bool
		if err := rows.Scan(&name, &dataType, &nullable, &defaultVal, &isPK); err != nil {
			return nil, err
		}
		col := driver.ColumnInfo{
			Name:       name,
			Type:       dataType,
			Nullable:   nullable == "YES",
			PrimaryKey: isPK,
		}
		if defaultVal != nil {
			col.DefaultValue = *defaultVal
		}
		columns = append(columns, col)
	}
	return columns, rows.Err()
}

func (c *pgConn) GetIndexes(ctx context.Context, table string) ([]driver.IndexInfo, error) {
	query := `
		SELECT schemaname, tablename, indexname,
		       array_to_string(
		           ARRAY(SELECT pg_get_indexdef(i.indexrelid, k + 1, true)
		                 FROM generate_subscripts(i.indkey, 1) AS k
		                 ORDER BY k),
		           ','
		       ) AS columns,
		       i.indisunique
		FROM pg_indexes pgi
		JOIN pg_class c ON c.relname = pgi.indexname
		JOIN pg_index i ON i.indexrelid = c.oid
		WHERE pgi.schemaname NOT IN ('pg_catalog', 'information_schema')`

	args := []any{}
	if table != "" {
		schema, tbl := splitSchemaTable(table)
		query += " AND pgi.schemaname = $1 AND pgi.tablename = $2"
		args = append(args, schema, tbl)
	}
	query += " ORDER BY pgi.schemaname, pgi.tablename, pgi.indexname"

	rows, err := c.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, classifyError(err)
	}
	defer rows.Close()

	var indexes []driver.IndexInfo
	for rows.Next() {
		var schema, tblName, idxName, colStr string
		var unique bool
		if err := rows.Scan(&schema, &tblName, &idxName, &colStr, &unique); err != nil {
			return nil, err
		}
		cols := strings.Split(colStr, ",")
		indexes = append(indexes, driver.IndexInfo{
			Name:    idxName,
			Table:   schema + "." + tblName,
			Schema:  schema,
			Columns: cols,
			Unique:  unique,
		})
	}
	return indexes, rows.Err()
}

func (c *pgConn) GetConstraints(ctx context.Context, table string) ([]driver.ConstraintInfo, error) {
	query := `
		SELECT tc.constraint_name, tc.table_schema, tc.table_name,
		       tc.constraint_type,
		       array_agg(DISTINCT kcu.column_name ORDER BY kcu.column_name) AS columns,
		       ccu.table_schema AS ref_schema, ccu.table_name AS ref_table,
		       COALESCE(array_agg(DISTINCT ccu2.column_name ORDER BY ccu2.column_name) FILTER (WHERE ccu2.column_name IS NOT NULL), ARRAY[]::text[]) AS ref_columns,
		       cc.check_clause
		FROM information_schema.table_constraints tc
		JOIN information_schema.key_column_usage kcu
		  ON tc.constraint_name = kcu.constraint_name
		  AND tc.table_schema = kcu.table_schema
		LEFT JOIN information_schema.constraint_column_usage ccu
		  ON tc.constraint_name = ccu.constraint_name
		  AND tc.table_schema = ccu.table_schema
		  AND tc.constraint_type = 'FOREIGN KEY'
		LEFT JOIN information_schema.referential_constraints rc
		  ON tc.constraint_name = rc.constraint_name
		  AND tc.table_schema = rc.constraint_schema
		LEFT JOIN information_schema.key_column_usage ccu2
		  ON rc.unique_constraint_name = ccu2.constraint_name
		  AND rc.unique_constraint_schema = ccu2.constraint_schema
		LEFT JOIN information_schema.check_constraints cc
		  ON tc.constraint_name = cc.constraint_name
		  AND tc.table_schema = cc.constraint_schema
		WHERE tc.table_schema NOT IN ('pg_catalog', 'information_schema')`

	args := []any{}
	if table != "" {
		schema, tbl := splitSchemaTable(table)
		query += " AND tc.table_schema = $1 AND tc.table_name = $2"
		args = append(args, schema, tbl)
	}
	query += ` GROUP BY tc.constraint_name, tc.table_schema, tc.table_name,
		tc.constraint_type, ccu.table_schema, ccu.table_name, cc.check_clause
		ORDER BY tc.table_schema, tc.table_name, tc.constraint_name`

	rows, err := c.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, classifyError(err)
	}
	defer rows.Close()

	var constraints []driver.ConstraintInfo
	for rows.Next() {
		var name, schema, tblName, cType string
		var cols []string
		var refSchema, refTable *string
		var refCols []string
		var checkClause *string
		if err := rows.Scan(&name, &schema, &tblName, &cType, &cols,
			&refSchema, &refTable, &refCols, &checkClause); err != nil {
			return nil, err
		}

		ci := driver.ConstraintInfo{
			Name:    name,
			Table:   schema + "." + tblName,
			Schema:  schema,
			Columns: cols,
		}

		switch cType {
		case "PRIMARY KEY":
			ci.Type = driver.ConstraintPrimaryKey
		case "FOREIGN KEY":
			ci.Type = driver.ConstraintForeignKey
			if refTable != nil {
				refSchemaStr := "public"
				if refSchema != nil {
					refSchemaStr = *refSchema
				}
				ci.ReferencedTable = refSchemaStr + "." + *refTable
				ci.ReferencedColumns = refCols
			}
		case "UNIQUE":
			ci.Type = driver.ConstraintUnique
		case "CHECK":
			ci.Type = driver.ConstraintCheck
			if checkClause != nil {
				ci.Definition = *checkClause
			}
		}

		constraints = append(constraints, ci)
	}
	return constraints, rows.Err()
}

func (c *pgConn) SearchSchema(ctx context.Context, pattern string) (*driver.SearchResult, error) {
	likePattern := "%" + strings.ToLower(pattern) + "%"

	// Search tables
	tableRows, err := c.conn.Query(ctx, `
		SELECT table_schema, table_name
		FROM information_schema.tables
		WHERE table_schema NOT IN ('pg_catalog', 'information_schema')
		  AND LOWER(table_name) LIKE $1
		ORDER BY table_schema, table_name`, likePattern)
	if err != nil {
		return nil, classifyError(err)
	}
	defer tableRows.Close()

	var tables []driver.TableInfo
	for tableRows.Next() {
		var schema, name string
		if err := tableRows.Scan(&schema, &name); err != nil {
			return nil, err
		}
		tables = append(tables, driver.TableInfo{
			Name:   schema + "." + name,
			Schema: schema,
		})
	}

	// Search columns
	colRows, err := c.conn.Query(ctx, `
		SELECT table_schema || '.' || table_name, column_name
		FROM information_schema.columns
		WHERE table_schema NOT IN ('pg_catalog', 'information_schema')
		  AND LOWER(column_name) LIKE $1
		ORDER BY table_schema, table_name, ordinal_position`, likePattern)
	if err != nil {
		return nil, classifyError(err)
	}
	defer colRows.Close()

	var columns []driver.ColumnMatch
	for colRows.Next() {
		var tbl, col string
		if err := colRows.Scan(&tbl, &col); err != nil {
			return nil, err
		}
		columns = append(columns, driver.ColumnMatch{Table: tbl, Column: col})
	}

	return &driver.SearchResult{Tables: tables, Columns: columns}, nil
}

func (c *pgConn) Close() error {
	return c.conn.Close(context.Background())
}

// splitSchemaTable splits "schema.table" into parts, defaulting to "public".
func splitSchemaTable(name string) (string, string) {
	if idx := strings.IndexByte(name, '.'); idx >= 0 {
		return name[:idx], name[idx+1:]
	}
	return "public", name
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

func classifyError(err error) error {
	if err == nil {
		return nil
	}

	var pgErr *pgconn.PgError
	if stderrors.As(err, &pgErr) {
		return classifyPgError(pgErr)
	}

	msg := err.Error()

	// Connection errors
	if strings.Contains(msg, "connect") && (strings.Contains(msg, "refused") || strings.Contains(msg, "timeout")) {
		return errors.New(msg, errors.FixableByHuman).
			WithHint("Cannot connect to PostgreSQL. Check that the server is running and the connection details are correct.")
	}
	if strings.Contains(msg, "password authentication failed") {
		return errors.New(msg, errors.FixableByHuman).
			WithHint("Authentication failed. Check the username and password.")
	}

	return errors.Wrap(err, errors.FixableByAgent)
}

func classifyPgError(pgErr *pgconn.PgError) error {
	code := pgErr.Code
	msg := pgErr.Message

	switch code {
	case "42P01": // undefined_table
		return errors.New(msg, errors.FixableByAgent).
			WithHint("Table not found. Use 'schema tables' to see available tables.")
	case "42703": // undefined_column
		return errors.New(msg, errors.FixableByAgent).
			WithHint("Column not found. Use 'schema describe <table>' to see available columns.")
	case "42601": // syntax_error
		return errors.New(msg, errors.FixableByAgent).
			WithHint("SQL syntax error. Check the query syntax.")
	case "25006": // read_only_sql_transaction
		return errors.New(msg, errors.FixableByHuman).
			WithHint("This connection is read-only. To enable writes, use a credential with writePermission and pass --write.")
	case "57014": // query_canceled (timeout)
		return errors.New(msg, errors.FixableByRetry).
			WithHint("Query timed out. Try a simpler query or increase the timeout with --timeout.")
	case "28P01": // invalid_password
		return errors.New(msg, errors.FixableByHuman).
			WithHint("Authentication failed. Check the username and password.")
	case "28000": // invalid_authorization_specification
		return errors.New(msg, errors.FixableByHuman).
			WithHint("Authorization failed. Check the username and permissions.")
	case "08006", "08001": // connection_failure, sqlclient_unable_to_establish_sqlconnection
		return errors.New(msg, errors.FixableByHuman).
			WithHint("Cannot connect to PostgreSQL. Check that the server is running and the connection details are correct.")
	case "3D000": // invalid_catalog_name (database not found)
		return errors.New(msg, errors.FixableByHuman).
			WithHint("Database not found. Check the database name.")
	case "42P07": // duplicate_table
		return errors.New(msg, errors.FixableByAgent).
			WithHint("Table already exists.")
	}

	// Fall back to error class (first two characters of SQLSTATE)
	switch code[:2] {
	case "08": // connection exception
		return errors.New(msg, errors.FixableByHuman).
			WithHint("Connection error. Check the server status and connection details.")
	case "28": // invalid authorization
		return errors.New(msg, errors.FixableByHuman).
			WithHint("Authentication or authorization failed.")
	case "42": // syntax error or access rule violation
		return errors.New(msg, errors.FixableByAgent).
			WithHint("SQL error. Check the query syntax and referenced objects.")
	case "53": // insufficient resources
		return errors.New(msg, errors.FixableByRetry).
			WithHint("Server resource issue. Try again shortly.")
	}

	return errors.New(msg, errors.FixableByAgent)
}
