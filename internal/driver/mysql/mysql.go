// Package mysql implements the MySQL/MariaDB driver using go-sql-driver/mysql.
package mysql

import (
	"context"
	"database/sql"
	stderrors "errors"
	"fmt"
	"strings"

	gomysql "github.com/go-sql-driver/mysql"
	"github.com/shhac/agent-sql/internal/driver"
	"github.com/shhac/agent-sql/internal/errors"
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
			row[col] = normalizeValue(values[i])
		}
		results = append(results, row)
	}
	if err := rows.Err(); err != nil {
		return nil, classifyError(err)
	}

	return &driver.QueryResult{Columns: columns, Rows: results}, nil
}

func (c *mysqlConn) GetTables(ctx context.Context, includeSystem bool) ([]driver.TableInfo, error) {
	query := `
		SELECT table_name, table_type
		FROM information_schema.tables
		WHERE table_schema = DATABASE()
		  AND table_type IN ('BASE TABLE', 'VIEW')
		ORDER BY table_name`

	rows, err := c.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tables []driver.TableInfo
	for rows.Next() {
		var name, typ string
		if err := rows.Scan(&name, &typ); err != nil {
			return nil, err
		}
		t := "table"
		if typ == "VIEW" {
			t = "view"
		}
		tables = append(tables, driver.TableInfo{Name: name, Type: t})
	}
	return tables, rows.Err()
}

func (c *mysqlConn) DescribeTable(ctx context.Context, table string) ([]driver.ColumnInfo, error) {
	query := `
		SELECT
			c.COLUMN_NAME,
			c.COLUMN_TYPE,
			c.IS_NULLABLE,
			c.COLUMN_DEFAULT,
			c.COLUMN_KEY
		FROM information_schema.columns c
		WHERE c.TABLE_SCHEMA = DATABASE()
		  AND c.TABLE_NAME = ?
		ORDER BY c.ORDINAL_POSITION`

	rows, err := c.db.QueryContext(ctx, query, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var columns []driver.ColumnInfo
	for rows.Next() {
		var name, colType, nullable, colKey string
		var dflt *string
		if err := rows.Scan(&name, &colType, &nullable, &dflt, &colKey); err != nil {
			return nil, err
		}
		col := driver.ColumnInfo{
			Name:       name,
			Type:       colType,
			Nullable:   nullable == "YES",
			PrimaryKey: colKey == "PRI",
		}
		if dflt != nil {
			col.DefaultValue = *dflt
		}
		columns = append(columns, col)
	}
	return columns, rows.Err()
}

func (c *mysqlConn) GetIndexes(ctx context.Context, table string) ([]driver.IndexInfo, error) {
	query := `
		SELECT
			INDEX_NAME,
			TABLE_NAME,
			GROUP_CONCAT(COLUMN_NAME ORDER BY SEQ_IN_INDEX) AS idx_columns,
			NOT NON_UNIQUE AS is_unique
		FROM information_schema.statistics
		WHERE TABLE_SCHEMA = DATABASE()`

	var args []any
	if table != "" {
		query += " AND TABLE_NAME = ?"
		args = append(args, table)
	}
	query += `
		GROUP BY TABLE_NAME, INDEX_NAME, NON_UNIQUE
		ORDER BY TABLE_NAME, INDEX_NAME`

	rows, err := c.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var indexes []driver.IndexInfo
	for rows.Next() {
		var name, tableName, cols string
		var unique bool
		if err := rows.Scan(&name, &tableName, &cols, &unique); err != nil {
			return nil, err
		}
		indexes = append(indexes, driver.IndexInfo{
			Name:    name,
			Table:   tableName,
			Columns: strings.Split(cols, ","),
			Unique:  unique,
		})
	}
	return indexes, rows.Err()
}

func (c *mysqlConn) GetConstraints(ctx context.Context, table string) ([]driver.ConstraintInfo, error) {
	query := `
		SELECT
			tc.CONSTRAINT_NAME,
			tc.TABLE_NAME,
			tc.CONSTRAINT_TYPE,
			GROUP_CONCAT(kcu.COLUMN_NAME ORDER BY kcu.ORDINAL_POSITION) AS cols,
			kcu.REFERENCED_TABLE_NAME,
			GROUP_CONCAT(kcu.REFERENCED_COLUMN_NAME ORDER BY kcu.ORDINAL_POSITION) AS ref_cols
		FROM information_schema.table_constraints tc
		JOIN information_schema.key_column_usage kcu
		  ON kcu.CONSTRAINT_NAME = tc.CONSTRAINT_NAME
		  AND kcu.TABLE_SCHEMA = tc.TABLE_SCHEMA
		  AND kcu.TABLE_NAME = tc.TABLE_NAME
		WHERE tc.TABLE_SCHEMA = DATABASE()`

	var args []any
	if table != "" {
		query += " AND tc.TABLE_NAME = ?"
		args = append(args, table)
	}
	query += `
		GROUP BY tc.CONSTRAINT_NAME, tc.TABLE_NAME, tc.CONSTRAINT_TYPE,
		         kcu.REFERENCED_TABLE_NAME
		ORDER BY tc.TABLE_NAME, tc.CONSTRAINT_NAME`

	rows, err := c.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	constraintTypeMap := map[string]driver.ConstraintType{
		"PRIMARY KEY": driver.ConstraintPrimaryKey,
		"FOREIGN KEY": driver.ConstraintForeignKey,
		"UNIQUE":      driver.ConstraintUnique,
		"CHECK":       driver.ConstraintCheck,
	}

	var constraints []driver.ConstraintInfo
	for rows.Next() {
		var name, tableName, cType, cols string
		var refTable, refCols *string
		if err := rows.Scan(&name, &tableName, &cType, &cols, &refTable, &refCols); err != nil {
			return nil, err
		}
		ct, ok := constraintTypeMap[cType]
		if !ok {
			ct = driver.ConstraintCheck
		}
		ci := driver.ConstraintInfo{
			Name:    name,
			Table:   tableName,
			Type:    ct,
			Columns: strings.Split(cols, ","),
		}
		if cType == "FOREIGN KEY" && refTable != nil {
			ci.ReferencedTable = *refTable
			if refCols != nil {
				ci.ReferencedColumns = strings.Split(*refCols, ",")
			}
		}
		constraints = append(constraints, ci)
	}
	return constraints, rows.Err()
}

func (c *mysqlConn) SearchSchema(ctx context.Context, pattern string) (*driver.SearchResult, error) {
	likePattern := "%" + pattern + "%"

	tableRows, err := c.db.QueryContext(ctx,
		`SELECT TABLE_NAME
		 FROM information_schema.tables
		 WHERE TABLE_SCHEMA = DATABASE()
		   AND TABLE_NAME LIKE ?
		 ORDER BY TABLE_NAME`, likePattern)
	if err != nil {
		return nil, err
	}
	defer tableRows.Close()

	var tables []driver.TableInfo
	for tableRows.Next() {
		var name string
		if err := tableRows.Scan(&name); err != nil {
			return nil, err
		}
		tables = append(tables, driver.TableInfo{Name: name})
	}

	colRows, err := c.db.QueryContext(ctx,
		`SELECT TABLE_NAME, COLUMN_NAME
		 FROM information_schema.columns
		 WHERE TABLE_SCHEMA = DATABASE()
		   AND COLUMN_NAME LIKE ?
		 ORDER BY TABLE_NAME, COLUMN_NAME`, likePattern)
	if err != nil {
		return nil, err
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

func (c *mysqlConn) Close() error {
	return c.db.Close()
}

func normalizeValue(v any) any {
	switch val := v.(type) {
	case []byte:
		return string(val)
	default:
		return val
	}
}

func classifyError(err error) error {
	if err == nil {
		return nil
	}

	// Check for MySQL-specific error numbers
	var mysqlErr *gomysql.MySQLError
	if stderrors.As(err, &mysqlErr) {
		return classifyMySQLError(mysqlErr)
	}

	msg := err.Error()
	return classifyByMessage(msg, err)
}

func classifyMySQLError(e *gomysql.MySQLError) error {
	switch e.Number {
	case 1792: // ER_CANT_EXECUTE_IN_READ_ONLY_TRANSACTION
		return errors.New(e.Message, errors.FixableByHuman).
			WithHint("This connection is read-only. To enable writes, use a credential with writePermission and pass --write.")
	case 1146: // ER_NO_SUCH_TABLE
		return errors.New(e.Message, errors.FixableByAgent).
			WithHint("Table not found. Use 'schema tables' to see available tables.")
	case 1054: // ER_BAD_FIELD_ERROR
		return errors.New(e.Message, errors.FixableByAgent).
			WithHint("Column not found. Use 'schema describe <table>' to see available columns.")
	case 2002, 2003: // CR_CONNECTION_ERROR, CR_CONN_HOST_ERROR
		return errors.New(e.Message, errors.FixableByHuman).
			WithHint("Cannot connect to the database server. Check that the host and port are correct and the server is running.")
	case 1045: // ER_ACCESS_DENIED_ERROR
		return errors.New(e.Message, errors.FixableByHuman).
			WithHint("Authentication failed. Check the username and password.")
	default:
		return errors.Wrap(e, errors.FixableByAgent)
	}
}

func classifyByMessage(msg string, cause error) error {
	switch {
	case strings.Contains(msg, "connection refused"):
		return errors.New(msg, errors.FixableByHuman).
			WithHint("Cannot connect to the database server. Check that the host and port are correct and the server is running.")
	case strings.Contains(msg, "Access denied"):
		return errors.New(msg, errors.FixableByHuman).
			WithHint("Authentication failed. Check the username and password.")
	default:
		return errors.Wrap(cause, errors.FixableByAgent)
	}
}
