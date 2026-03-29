// Package mssql implements the Microsoft SQL Server driver using database/sql.
package mssql

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strings"

	"github.com/shhac/agent-sql/internal/driver"
	"github.com/shhac/agent-sql/internal/errors"
	_ "github.com/microsoft/go-mssqldb"
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
			row[col] = normalizeValue(values[i])
		}
		results = append(results, row)
	}
	if err := rows.Err(); err != nil {
		return nil, classifyError(err)
	}

	return &driver.QueryResult{Columns: columns, Rows: results}, nil
}

func (c *mssqlConn) GetTables(ctx context.Context, includeSystem bool) ([]driver.TableInfo, error) {
	query := `SELECT TABLE_NAME, TABLE_SCHEMA, TABLE_TYPE
		FROM INFORMATION_SCHEMA.TABLES
		WHERE TABLE_TYPE IN ('BASE TABLE', 'VIEW')
		ORDER BY TABLE_SCHEMA, TABLE_NAME`

	rows, err := c.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tables []driver.TableInfo
	for rows.Next() {
		var name, schema, typ string
		if err := rows.Scan(&name, &schema, &typ); err != nil {
			return nil, err
		}
		if !includeSystem && isSystemSchema(schema) {
			continue
		}
		t := "table"
		if typ == "VIEW" {
			t = "view"
		}
		tables = append(tables, driver.TableInfo{
			Name:   name,
			Schema: schema,
			Type:   t,
		})
	}
	return tables, rows.Err()
}

func (c *mssqlConn) DescribeTable(ctx context.Context, table string) ([]driver.ColumnInfo, error) {
	schema, tbl := splitSchemaTable(table)

	query := `SELECT c.COLUMN_NAME, c.DATA_TYPE, c.IS_NULLABLE, c.COLUMN_DEFAULT,
		CASE WHEN pk.COLUMN_NAME IS NOT NULL THEN 1 ELSE 0 END AS is_pk
		FROM INFORMATION_SCHEMA.COLUMNS c
		LEFT JOIN (
			SELECT kcu.COLUMN_NAME, kcu.TABLE_NAME, kcu.TABLE_SCHEMA
			FROM INFORMATION_SCHEMA.TABLE_CONSTRAINTS tc
			JOIN INFORMATION_SCHEMA.KEY_COLUMN_USAGE kcu
				ON tc.CONSTRAINT_NAME = kcu.CONSTRAINT_NAME
				AND tc.TABLE_SCHEMA = kcu.TABLE_SCHEMA
			WHERE tc.CONSTRAINT_TYPE = 'PRIMARY KEY'
		) pk ON pk.COLUMN_NAME = c.COLUMN_NAME
			AND pk.TABLE_NAME = c.TABLE_NAME
			AND pk.TABLE_SCHEMA = c.TABLE_SCHEMA
		WHERE c.TABLE_NAME = @p1 AND c.TABLE_SCHEMA = @p2
		ORDER BY c.ORDINAL_POSITION`

	rows, err := c.db.QueryContext(ctx, query, tbl, schema)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var columns []driver.ColumnInfo
	for rows.Next() {
		var name, dataType, nullable string
		var defaultVal *string
		var isPK int
		if err := rows.Scan(&name, &dataType, &nullable, &defaultVal, &isPK); err != nil {
			return nil, err
		}
		col := driver.ColumnInfo{
			Name:       name,
			Type:       dataType,
			Nullable:   nullable == "YES",
			PrimaryKey: isPK == 1,
		}
		if defaultVal != nil {
			col.DefaultValue = *defaultVal
		}
		columns = append(columns, col)
	}
	return columns, rows.Err()
}

func (c *mssqlConn) GetIndexes(ctx context.Context, table string) ([]driver.IndexInfo, error) {
	query := `SELECT i.name AS index_name,
		s.name AS schema_name,
		t.name AS table_name,
		i.is_unique,
		c.name AS column_name,
		ic.key_ordinal
		FROM sys.indexes i
		JOIN sys.tables t ON i.object_id = t.object_id
		JOIN sys.schemas s ON t.schema_id = s.schema_id
		JOIN sys.index_columns ic ON i.object_id = ic.object_id AND i.index_id = ic.index_id
		JOIN sys.columns c ON ic.object_id = c.object_id AND ic.column_id = c.column_id
		WHERE i.name IS NOT NULL AND ic.key_ordinal > 0`

	args := []any{}
	if table != "" {
		schema, tbl := splitSchemaTable(table)
		query += " AND t.name = @p1 AND s.name = @p2"
		args = append(args, tbl, schema)
	}

	query += " ORDER BY s.name, t.name, i.name, ic.key_ordinal"

	rows, err := c.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type indexKey struct {
		name   string
		schema string
		table  string
		unique bool
	}
	indexMap := make(map[string]*indexKey)
	indexCols := make(map[string][]string)
	var order []string

	for rows.Next() {
		var idxName, schemaName, tableName, colName string
		var isUnique bool
		var keyOrdinal int
		if err := rows.Scan(&idxName, &schemaName, &tableName, &isUnique, &colName, &keyOrdinal); err != nil {
			return nil, err
		}
		key := schemaName + "." + idxName
		if _, exists := indexMap[key]; !exists {
			indexMap[key] = &indexKey{name: idxName, schema: schemaName, table: tableName, unique: isUnique}
			order = append(order, key)
		}
		indexCols[key] = append(indexCols[key], colName)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var indexes []driver.IndexInfo
	for _, key := range order {
		ik := indexMap[key]
		indexes = append(indexes, driver.IndexInfo{
			Name:    ik.name,
			Table:   ik.table,
			Schema:  ik.schema,
			Columns: indexCols[key],
			Unique:  ik.unique,
		})
	}
	return indexes, nil
}

func (c *mssqlConn) GetConstraints(ctx context.Context, table string) ([]driver.ConstraintInfo, error) {
	query := `SELECT tc.CONSTRAINT_NAME, tc.TABLE_SCHEMA, tc.TABLE_NAME, tc.CONSTRAINT_TYPE,
		kcu.COLUMN_NAME, kcu.ORDINAL_POSITION
		FROM INFORMATION_SCHEMA.TABLE_CONSTRAINTS tc
		JOIN INFORMATION_SCHEMA.KEY_COLUMN_USAGE kcu
			ON tc.CONSTRAINT_NAME = kcu.CONSTRAINT_NAME
			AND tc.TABLE_SCHEMA = kcu.TABLE_SCHEMA
		WHERE tc.CONSTRAINT_TYPE IN ('PRIMARY KEY', 'FOREIGN KEY', 'UNIQUE')`

	args := []any{}
	if table != "" {
		schema, tbl := splitSchemaTable(table)
		query += " AND tc.TABLE_NAME = @p1 AND tc.TABLE_SCHEMA = @p2"
		args = append(args, tbl, schema)
	}

	query += " ORDER BY tc.TABLE_SCHEMA, tc.TABLE_NAME, tc.CONSTRAINT_NAME, kcu.ORDINAL_POSITION"

	rows, err := c.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	constraintMap := make(map[string]*constraintKey)
	constraintCols := make(map[string][]string)
	var order []string

	for rows.Next() {
		var name, schema, tableName, typ, colName string
		var ordinal int
		if err := rows.Scan(&name, &schema, &tableName, &typ, &colName, &ordinal); err != nil {
			return nil, err
		}
		key := schema + "." + name
		if _, exists := constraintMap[key]; !exists {
			constraintMap[key] = &constraintKey{
				name:   name,
				schema: schema,
				table:  tableName,
				typ:    mapConstraintType(typ),
			}
			order = append(order, key)
		}
		constraintCols[key] = append(constraintCols[key], colName)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Fetch foreign key references separately
	fkRefs := make(map[string]fkRef)
	if hasForeignKeys(constraintMap) {
		refs, err := c.fetchFKReferences(ctx, table)
		if err != nil {
			return nil, err
		}
		fkRefs = refs
	}

	var constraints []driver.ConstraintInfo
	for _, key := range order {
		ck := constraintMap[key]
		ci := driver.ConstraintInfo{
			Name:    ck.name,
			Table:   ck.table,
			Schema:  ck.schema,
			Type:    ck.typ,
			Columns: constraintCols[key],
		}
		if ck.typ == driver.ConstraintForeignKey {
			if ref, ok := fkRefs[ck.name]; ok {
				ci.ReferencedTable = ref.table
				ci.ReferencedColumns = ref.columns
			}
		}
		constraints = append(constraints, ci)
	}

	// Check constraints via sys.check_constraints
	checkQuery := `SELECT cc.name, s.name AS schema_name, t.name AS table_name, cc.definition
		FROM sys.check_constraints cc
		JOIN sys.tables t ON cc.parent_object_id = t.object_id
		JOIN sys.schemas s ON t.schema_id = s.schema_id
		WHERE 1=1`

	checkArgs := []any{}
	if table != "" {
		schema, tbl := splitSchemaTable(table)
		checkQuery += " AND t.name = @p1 AND s.name = @p2"
		checkArgs = append(checkArgs, tbl, schema)
	}

	checkRows, err := c.db.QueryContext(ctx, checkQuery, checkArgs...)
	if err != nil {
		return constraints, nil // non-fatal
	}
	defer checkRows.Close()

	for checkRows.Next() {
		var name, schema, tableName, definition string
		if err := checkRows.Scan(&name, &schema, &tableName, &definition); err != nil {
			continue
		}
		constraints = append(constraints, driver.ConstraintInfo{
			Name:       name,
			Table:      tableName,
			Schema:     schema,
			Type:       driver.ConstraintCheck,
			Definition: definition,
		})
	}

	return constraints, nil
}

func (c *mssqlConn) SearchSchema(ctx context.Context, pattern string) (*driver.SearchResult, error) {
	likePattern := "%" + pattern + "%"

	tableQuery := `SELECT TABLE_NAME, TABLE_SCHEMA
		FROM INFORMATION_SCHEMA.TABLES
		WHERE TABLE_TYPE IN ('BASE TABLE', 'VIEW')
		AND TABLE_NAME LIKE @p1
		ORDER BY TABLE_SCHEMA, TABLE_NAME`

	tableRows, err := c.db.QueryContext(ctx, tableQuery, likePattern)
	if err != nil {
		return nil, err
	}
	defer tableRows.Close()

	var tables []driver.TableInfo
	for tableRows.Next() {
		var name, schema string
		if err := tableRows.Scan(&name, &schema); err != nil {
			return nil, err
		}
		if !isSystemSchema(schema) {
			tables = append(tables, driver.TableInfo{Name: name, Schema: schema})
		}
	}

	colQuery := `SELECT c.TABLE_NAME, c.TABLE_SCHEMA, c.COLUMN_NAME
		FROM INFORMATION_SCHEMA.COLUMNS c
		JOIN INFORMATION_SCHEMA.TABLES t
			ON c.TABLE_NAME = t.TABLE_NAME AND c.TABLE_SCHEMA = t.TABLE_SCHEMA
		WHERE t.TABLE_TYPE IN ('BASE TABLE', 'VIEW')
		AND c.COLUMN_NAME LIKE @p1
		ORDER BY c.TABLE_SCHEMA, c.TABLE_NAME, c.COLUMN_NAME`

	colRows, err := c.db.QueryContext(ctx, colQuery, likePattern)
	if err != nil {
		return nil, err
	}
	defer colRows.Close()

	var columns []driver.ColumnMatch
	for colRows.Next() {
		var tableName, schema, colName string
		if err := colRows.Scan(&tableName, &schema, &colName); err != nil {
			return nil, err
		}
		if !isSystemSchema(schema) {
			columns = append(columns, driver.ColumnMatch{
				Table:  schema + "." + tableName,
				Column: colName,
			})
		}
	}

	return &driver.SearchResult{Tables: tables, Columns: columns}, nil
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

type constraintKey struct {
	name   string
	schema string
	table  string
	typ    driver.ConstraintType
}

type fkRef struct {
	table   string
	columns []string
}

func (c *mssqlConn) fetchFKReferences(ctx context.Context, table string) (map[string]fkRef, error) {
	query := `SELECT fk.name AS constraint_name,
		rt.name AS referenced_table,
		rc.name AS referenced_column
		FROM sys.foreign_keys fk
		JOIN sys.foreign_key_columns fkc ON fk.object_id = fkc.constraint_object_id
		JOIN sys.tables rt ON fkc.referenced_object_id = rt.object_id
		JOIN sys.columns rc ON fkc.referenced_object_id = rc.object_id AND fkc.referenced_column_id = rc.column_id
		JOIN sys.tables t ON fk.parent_object_id = t.object_id
		JOIN sys.schemas s ON t.schema_id = s.schema_id
		WHERE 1=1`

	args := []any{}
	if table != "" {
		schema, tbl := splitSchemaTable(table)
		query += " AND t.name = @p1 AND s.name = @p2"
		args = append(args, tbl, schema)
	}

	query += " ORDER BY fk.name, fkc.constraint_column_id"

	rows, err := c.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	refs := make(map[string]fkRef)
	for rows.Next() {
		var constraintName, refTable, refCol string
		if err := rows.Scan(&constraintName, &refTable, &refCol); err != nil {
			return nil, err
		}
		ref := refs[constraintName]
		ref.table = refTable
		ref.columns = append(ref.columns, refCol)
		refs[constraintName] = ref
	}
	return refs, rows.Err()
}

func splitSchemaTable(name string) (string, string) {
	parts := strings.SplitN(name, ".", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "dbo", parts[0]
}

func isSystemSchema(schema string) bool {
	switch schema {
	case "sys", "INFORMATION_SCHEMA", "guest", "db_owner", "db_accessadmin",
		"db_securityadmin", "db_ddladmin", "db_backupoperator", "db_datareader",
		"db_datawriter", "db_denydatareader", "db_denydatawriter":
		return true
	}
	return false
}

func mapConstraintType(typ string) driver.ConstraintType {
	switch typ {
	case "PRIMARY KEY":
		return driver.ConstraintPrimaryKey
	case "FOREIGN KEY":
		return driver.ConstraintForeignKey
	case "UNIQUE":
		return driver.ConstraintUnique
	case "CHECK":
		return driver.ConstraintCheck
	default:
		return driver.ConstraintType(strings.ToLower(typ))
	}
}

func hasForeignKeys(m map[string]*constraintKey) bool {
	for _, v := range m {
		if v.typ == driver.ConstraintForeignKey {
			return true
		}
	}
	return false
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
	msg := err.Error()

	// Login failed
	if strings.Contains(msg, "Login failed") {
		return errors.New(msg, errors.FixableByHuman).
			WithHint("Login failed. Check the username, password, and database name in your connection config.")
	}

	// Connection refused / network errors
	if strings.Contains(msg, "connection refused") || strings.Contains(msg, "unable to open tcp connection") {
		return errors.New(msg, errors.FixableByHuman).
			WithHint("Cannot connect to MSSQL server. Verify the host, port, and that the server is running.")
	}

	// Permission denied
	if strings.Contains(msg, "permission") || strings.Contains(msg, "not allowed") {
		return errors.New(msg, errors.FixableByHuman).
			WithHint("Permission denied. Ensure the user has the necessary grants. For read-only, the db_datareader role is recommended.")
	}

	// Object not found
	if strings.Contains(msg, "Invalid object name") {
		return errors.New(msg, errors.FixableByAgent).
			WithHint("Table or view not found. Use 'schema tables' to see available tables. MSSQL uses schema-qualified names (e.g., dbo.users).")
	}

	// Invalid column
	if strings.Contains(msg, "Invalid column name") {
		return errors.New(msg, errors.FixableByAgent).
			WithHint("Column not found. Use 'schema describe <table>' to see available columns.")
	}

	// Syntax error
	if strings.Contains(msg, "Incorrect syntax") {
		return errors.New(msg, errors.FixableByAgent).
			WithHint("SQL syntax error. MSSQL uses T-SQL syntax which differs from standard SQL in some areas.")
	}

	// Timeout / deadlock
	if strings.Contains(msg, "deadline") || strings.Contains(msg, "context deadline exceeded") {
		return errors.New(msg, errors.FixableByRetry).
			WithHint("Query timed out. Try simplifying the query or increasing the timeout.")
	}
	if strings.Contains(msg, "deadlock") {
		return errors.New(msg, errors.FixableByRetry).
			WithHint("Transaction deadlock. Retry the query.")
	}

	return errors.Wrap(err, errors.FixableByAgent)
}
