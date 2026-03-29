// Package sqlite implements the SQLite driver using modernc.org/sqlite (pure Go).
package sqlite

import (
	"context"
	"database/sql"
	"fmt"
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
			row[col] = normalizeValue(values[i])
		}
		results = append(results, row)
	}
	if err := rows.Err(); err != nil {
		return nil, classifyError(err)
	}

	return &driver.QueryResult{Columns: columns, Rows: results}, nil
}

func (c *sqliteConn) GetTables(ctx context.Context, includeSystem bool) ([]driver.TableInfo, error) {
	query := "SELECT name, type FROM sqlite_master WHERE type IN ('table', 'view') ORDER BY name"
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
		if !includeSystem && strings.HasPrefix(name, "sqlite_") {
			continue
		}
		t := "table"
		if typ == "view" {
			t = "view"
		}
		tables = append(tables, driver.TableInfo{Name: name, Type: t})
	}
	return tables, rows.Err()
}

func (c *sqliteConn) DescribeTable(ctx context.Context, table string) ([]driver.ColumnInfo, error) {
	rows, err := c.db.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%s)", c.QuoteIdent(table)))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var columns []driver.ColumnInfo
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull, pk int
		var dflt *string
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			return nil, err
		}
		col := driver.ColumnInfo{
			Name:       name,
			Type:       typ,
			Nullable:   notnull == 0 && pk == 0,
			PrimaryKey: pk > 0,
		}
		if dflt != nil {
			col.DefaultValue = *dflt
		}
		columns = append(columns, col)
	}
	return columns, rows.Err()
}

func (c *sqliteConn) GetIndexes(ctx context.Context, table string) ([]driver.IndexInfo, error) {
	tables, err := c.tablesToScan(ctx, table)
	if err != nil {
		return nil, err
	}

	var indexes []driver.IndexInfo
	for _, tbl := range tables {
		idxRows, err := c.db.QueryContext(ctx, fmt.Sprintf("PRAGMA index_list(%s)", c.QuoteIdent(tbl)))
		if err != nil {
			return nil, err
		}

		type idxEntry struct {
			name   string
			unique bool
		}
		var entries []idxEntry
		for idxRows.Next() {
			var seq int
			var name, origin string
			var unique, partial int
			if err := idxRows.Scan(&seq, &name, &unique, &origin, &partial); err != nil {
				idxRows.Close()
				return nil, err
			}
			entries = append(entries, idxEntry{name: name, unique: unique == 1})
		}
		idxRows.Close()

		for _, entry := range entries {
			colRows, err := c.db.QueryContext(ctx, fmt.Sprintf("PRAGMA index_info(%s)", c.QuoteIdent(entry.name)))
			if err != nil {
				return nil, err
			}
			var cols []string
			for colRows.Next() {
				var seqno, cid int
				var colName string
				if err := colRows.Scan(&seqno, &cid, &colName); err != nil {
					colRows.Close()
					return nil, err
				}
				cols = append(cols, colName)
			}
			colRows.Close()

			indexes = append(indexes, driver.IndexInfo{
				Name:    entry.name,
				Table:   tbl,
				Columns: cols,
				Unique:  entry.unique,
			})
		}
	}
	return indexes, nil
}

func (c *sqliteConn) GetConstraints(ctx context.Context, table string) ([]driver.ConstraintInfo, error) {
	tables, err := c.tablesToScan(ctx, table)
	if err != nil {
		return nil, err
	}

	var constraints []driver.ConstraintInfo

	for _, tbl := range tables {
		// Primary keys
		pkCols, err := c.primaryKeyCols(ctx, tbl)
		if err != nil {
			return nil, err
		}
		if len(pkCols) > 0 {
			constraints = append(constraints, driver.ConstraintInfo{
				Name:    tbl + "_pkey",
				Table:   tbl,
				Type:    driver.ConstraintPrimaryKey,
				Columns: pkCols,
			})
		}

		// Foreign keys
		fkRows, err := c.db.QueryContext(ctx, fmt.Sprintf("PRAGMA foreign_key_list(%s)", c.QuoteIdent(tbl)))
		if err != nil {
			return nil, err
		}
		for fkRows.Next() {
			var id, seq int
			var refTable, from, to, onUpdate, onDelete, match string
			if err := fkRows.Scan(&id, &seq, &refTable, &from, &to, &onUpdate, &onDelete, &match); err != nil {
				fkRows.Close()
				return nil, err
			}
			constraints = append(constraints, driver.ConstraintInfo{
				Name:              fmt.Sprintf("%s_%s_fkey", tbl, from),
				Table:             tbl,
				Type:              driver.ConstraintForeignKey,
				Columns:           []string{from},
				ReferencedTable:   refTable,
				ReferencedColumns: []string{to},
			})
		}
		fkRows.Close()

		// Unique constraints (from indexes)
		idxRows, err := c.db.QueryContext(ctx, fmt.Sprintf("PRAGMA index_list(%s)", c.QuoteIdent(tbl)))
		if err != nil {
			return nil, err
		}
		type idxEntry struct {
			name   string
			unique int
			origin string
		}
		var entries []idxEntry
		for idxRows.Next() {
			var e idxEntry
			var seq, partial int
			if err := idxRows.Scan(&seq, &e.name, &e.unique, &e.origin, &partial); err != nil {
				idxRows.Close()
				return nil, err
			}
			entries = append(entries, e)
		}
		idxRows.Close()

		for _, e := range entries {
			if e.unique == 0 || e.origin == "pk" {
				continue
			}
			colRows, err := c.db.QueryContext(ctx, fmt.Sprintf("PRAGMA index_info(%s)", c.QuoteIdent(e.name)))
			if err != nil {
				return nil, err
			}
			var cols []string
			for colRows.Next() {
				var seqno, cid int
				var colName string
				if err := colRows.Scan(&seqno, &cid, &colName); err != nil {
					colRows.Close()
					return nil, err
				}
				cols = append(cols, colName)
			}
			colRows.Close()

			constraints = append(constraints, driver.ConstraintInfo{
				Name:    e.name,
				Table:   tbl,
				Type:    driver.ConstraintUnique,
				Columns: cols,
			})
		}
	}
	return constraints, nil
}

func (c *sqliteConn) SearchSchema(ctx context.Context, pattern string) (*driver.SearchResult, error) {
	likePattern := "%" + pattern + "%"

	// Search tables
	tableRows, err := c.db.QueryContext(ctx,
		"SELECT name FROM sqlite_master WHERE type IN ('table', 'view') AND name NOT LIKE 'sqlite_%' AND name LIKE ? COLLATE NOCASE",
		likePattern)
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

	// Search columns
	allTables, err := c.userTableNames(ctx)
	if err != nil {
		return nil, err
	}

	var columns []driver.ColumnMatch
	lowerPattern := strings.ToLower(pattern)
	for _, tbl := range allTables {
		colRows, err := c.db.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%s)", c.QuoteIdent(tbl)))
		if err != nil {
			return nil, err
		}
		for colRows.Next() {
			var cid int
			var name, typ string
			var notnull, pk int
			var dflt *string
			if err := colRows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
				colRows.Close()
				return nil, err
			}
			if strings.Contains(strings.ToLower(name), lowerPattern) {
				columns = append(columns, driver.ColumnMatch{Table: tbl, Column: name})
			}
		}
		colRows.Close()
	}

	return &driver.SearchResult{Tables: tables, Columns: columns}, nil
}

func (c *sqliteConn) Close() error {
	return c.db.Close()
}

func (c *sqliteConn) tablesToScan(ctx context.Context, table string) ([]string, error) {
	if table != "" {
		return []string{table}, nil
	}
	return c.userTableNames(ctx)
}

func (c *sqliteConn) userTableNames(ctx context.Context) ([]string, error) {
	rows, err := c.db.QueryContext(ctx,
		"SELECT name FROM sqlite_master WHERE type IN ('table', 'view') AND name NOT LIKE 'sqlite_%' ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

func (c *sqliteConn) primaryKeyCols(ctx context.Context, table string) ([]string, error) {
	rows, err := c.db.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%s)", c.QuoteIdent(table)))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type pkCol struct {
		name string
		pk   int
	}
	var pkCols []pkCol
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull, pk int
		var dflt *string
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			return nil, err
		}
		if pk > 0 {
			pkCols = append(pkCols, pkCol{name: name, pk: pk})
		}
	}

	// Sort by pk order
	result := make([]string, len(pkCols))
	for _, p := range pkCols {
		if p.pk-1 < len(result) {
			result[p.pk-1] = p.name
		}
	}
	// Filter empty entries
	var filtered []string
	for _, name := range result {
		if name != "" {
			filtered = append(filtered, name)
		}
	}
	return filtered, nil
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
	if strings.Contains(msg, "attempt to write a readonly database") {
		return errors.New(msg, errors.FixableByHuman).
			WithHint("This database is opened read-only. To enable writes, use a credential with writePermission and pass --write.")
	}
	if strings.Contains(msg, "database is locked") {
		return errors.New(msg, errors.FixableByRetry).
			WithHint("The database is locked by another process. Try again shortly.")
	}
	if strings.Contains(msg, "no such table") {
		return errors.New(msg, errors.FixableByAgent).
			WithHint("Table not found. Use 'schema tables' to see available tables.")
	}
	if strings.Contains(msg, "no such column") {
		return errors.New(msg, errors.FixableByAgent).
			WithHint("Column not found. Use 'schema describe <table>' to see available columns.")
	}
	return errors.Wrap(err, errors.FixableByAgent)
}
