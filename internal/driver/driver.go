// Package driver defines the shared interface that all database drivers implement.
package driver

import (
	"context"
	"strings"
)

// Driver identifies a database driver type.
type Driver string

const (
	DriverPG          Driver = "pg"
	DriverCockroachDB Driver = "cockroachdb"
	DriverMySQL       Driver = "mysql"
	DriverMariaDB     Driver = "mariadb"
	DriverSQLite      Driver = "sqlite"
	DriverDuckDB      Driver = "duckdb"
	DriverSnowflake   Driver = "snowflake"
	DriverMSSQL       Driver = "mssql"
)

// AllDrivers lists all supported driver names for error messages and help text.
var AllDrivers = []Driver{
	DriverPG, DriverCockroachDB, DriverSQLite, DriverDuckDB,
	DriverMySQL, DriverMariaDB, DriverSnowflake, DriverMSSQL,
}

// QueryOpts controls query execution behavior.
type QueryOpts struct {
	Write bool
}

// QueryResult holds the result of a SQL query.
type QueryResult struct {
	Columns      []string
	Rows         []map[string]any
	RowsAffected int64
	Command      string // e.g. "INSERT", empty for SELECT
}

// TableInfo describes a database table or view.
type TableInfo struct {
	Name   string `json:"name"`
	Schema string `json:"schema,omitempty"`
	Type   string `json:"type,omitempty"` // "table" or "view"
}

// ColumnInfo describes a table column.
type ColumnInfo struct {
	Name         string `json:"name"`
	Type         string `json:"type"`
	Nullable     bool   `json:"nullable"`
	DefaultValue string `json:"defaultValue,omitempty"`
	PrimaryKey   bool   `json:"primaryKey,omitempty"`
}

// IndexInfo describes a database index.
type IndexInfo struct {
	Name    string   `json:"name"`
	Table   string   `json:"table"`
	Schema  string   `json:"schema,omitempty"`
	Columns []string `json:"columns"`
	Unique  bool     `json:"unique"`
}

// ConstraintType classifies a constraint.
type ConstraintType string

const (
	ConstraintPrimaryKey ConstraintType = "primary_key"
	ConstraintForeignKey ConstraintType = "foreign_key"
	ConstraintUnique     ConstraintType = "unique"
	ConstraintCheck      ConstraintType = "check"
)

// ConstraintInfo describes a database constraint.
type ConstraintInfo struct {
	Name              string         `json:"name"`
	Table             string         `json:"table"`
	Schema            string         `json:"schema,omitempty"`
	Type              ConstraintType `json:"type"`
	Columns           []string       `json:"columns"`
	ReferencedTable   string         `json:"referencedTable,omitempty"`
	ReferencedColumns []string       `json:"referencedColumns,omitempty"`
	Definition        string         `json:"definition,omitempty"`
}

// SearchResult holds schema search results.
type SearchResult struct {
	Tables  []TableInfo   `json:"tables"`
	Columns []ColumnMatch `json:"columns"`
}

// ColumnMatch is a column that matched a search pattern.
type ColumnMatch struct {
	Table  string `json:"table"`
	Column string `json:"column"`
}

// Connection is the core driver interface. Every database driver implements this.
// All methods that perform I/O take context.Context for timeout and cancellation.
type Connection interface {
	// Query executes a SQL statement and returns the result.
	Query(ctx context.Context, sql string, opts QueryOpts) (*QueryResult, error)

	// GetTables lists tables and views. When includeSystem is false, system tables are excluded.
	GetTables(ctx context.Context, includeSystem bool) ([]TableInfo, error)

	// DescribeTable returns column information for a table.
	DescribeTable(ctx context.Context, table string) ([]ColumnInfo, error)

	// GetIndexes returns indexes. If table is empty, returns indexes for all tables.
	GetIndexes(ctx context.Context, table string) ([]IndexInfo, error)

	// GetConstraints returns constraints. If table is empty, returns all constraints.
	GetConstraints(ctx context.Context, table string) ([]ConstraintInfo, error)

	// SearchSchema searches table and column names by pattern.
	SearchSchema(ctx context.Context, pattern string) (*SearchResult, error)

	// QuoteIdent quotes an identifier for safe use in SQL.
	QuoteIdent(name string) string

	// Close releases the connection resources.
	Close() error
}

// RowIterator streams query results row by row.
// Follows the Go scanner pattern: call Next() in a loop, then check Err().
type RowIterator struct {
	columns []string
	scanFn  func() (map[string]any, error)
	nextFn  func() bool
	errFn   func() error
	closeFn func() error
}

// NewRowIterator creates a RowIterator from function callbacks.
func NewRowIterator(columns []string, next func() bool, scan func() (map[string]any, error), err func() error, close func() error) *RowIterator {
	return &RowIterator{columns: columns, nextFn: next, scanFn: scan, errFn: err, closeFn: close}
}

// Columns returns the column names.
func (r *RowIterator) Columns() []string { return r.columns }

// Next advances to the next row. Returns false when done.
func (r *RowIterator) Next() bool { return r.nextFn() }

// Scan returns the current row as a map.
func (r *RowIterator) Scan() (map[string]any, error) { return r.scanFn() }

// Err returns any error from iteration.
func (r *RowIterator) Err() error { return r.errFn() }

// Close releases the underlying resources.
func (r *RowIterator) Close() error { return r.closeFn() }

// StreamingResult holds either a streaming iterator or a write result.
type StreamingResult struct {
	Iterator     *RowIterator // nil for write results
	RowsAffected int64
	Command      string
}

// StreamingQuerier is an optional interface for drivers that support streaming.
// The CLI prefers this over Query() for read operations.
type StreamingQuerier interface {
	QueryStream(ctx context.Context, sql string, opts QueryOpts) (*StreamingResult, error)
}

// SliceIterator wraps a pre-collected slice as a RowIterator (for non-streaming drivers).
func SliceIterator(columns []string, rows []map[string]any) *RowIterator {
	idx := -1
	return NewRowIterator(
		columns,
		func() bool { idx++; return idx < len(rows) },
		func() (map[string]any, error) { return rows[idx], nil },
		func() error { return nil },
		func() error { return nil },
	)
}

// Collect drains a RowIterator into a QueryResult.
func Collect(iter *RowIterator) (*QueryResult, error) {
	defer iter.Close()
	var rows []map[string]any
	for iter.Next() {
		row, err := iter.Scan()
		if err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	if err := iter.Err(); err != nil {
		return nil, err
	}
	return &QueryResult{Columns: iter.Columns(), Rows: rows}, nil
}

// DetectCommand checks if SQL starts with a known write command.
// Returns the matched command (e.g. "INSERT") or empty string.
func DetectCommand(sql string, commands []string) string {
	trimmed := strings.TrimSpace(sql)
	if trimmed == "" {
		return ""
	}
	end := strings.IndexAny(trimmed, " \t\n\r(")
	firstWord := trimmed
	if end >= 0 {
		firstWord = trimmed[:end]
	}
	firstWord = strings.ToUpper(firstWord)
	for _, cmd := range commands {
		if firstWord == cmd {
			return cmd
		}
	}
	return ""
}

// WriteCommands is the shared set of SQL write commands used by keyword guards.
var WriteCommands = []string{
	"INSERT", "UPDATE", "DELETE", "CREATE", "ALTER", "DROP",
	"TRUNCATE", "MERGE", "GRANT", "REVOKE",
}
