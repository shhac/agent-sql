package duckdb

import (
	"context"
	"fmt"
	"strings"

	"github.com/shhac/agent-sql/internal/driver"
)

func (c *duckdbConn) GetTables(ctx context.Context, includeSystem bool) ([]driver.TableInfo, error) {
	query := "SELECT table_name, table_type FROM information_schema.tables WHERE table_schema = 'main' ORDER BY table_name"
	if includeSystem {
		query = "SELECT table_name, table_type FROM information_schema.tables ORDER BY table_name"
	}

	rows, err := c.exec(ctx, query)
	if err != nil {
		return nil, err
	}

	tables := make([]driver.TableInfo, 0, len(rows))
	for _, r := range rows {
		name, _ := r["table_name"].(string)
		tableType, _ := r["table_type"].(string)
		t := "table"
		if tableType == "VIEW" {
			t = "view"
		}
		tables = append(tables, driver.TableInfo{Name: name, Type: t})
	}
	return tables, nil
}

func (c *duckdbConn) DescribeTable(ctx context.Context, table string) ([]driver.ColumnInfo, error) {
	escaped := strings.ReplaceAll(table, "'", "''")
	query := fmt.Sprintf(
		"SELECT column_name, data_type, is_nullable, column_default FROM information_schema.columns WHERE table_schema = 'main' AND table_name = '%s' ORDER BY ordinal_position",
		escaped,
	)

	rows, err := c.exec(ctx, query)
	if err != nil {
		return nil, err
	}

	columns := make([]driver.ColumnInfo, 0, len(rows))
	for _, r := range rows {
		name, _ := r["column_name"].(string)
		dataType, _ := r["data_type"].(string)
		nullable, _ := r["is_nullable"].(string)
		col := driver.ColumnInfo{
			Name:     name,
			Type:     dataType,
			Nullable: nullable == "YES",
		}
		if dflt, ok := r["column_default"]; ok && dflt != nil {
			col.DefaultValue = fmt.Sprintf("%v", dflt)
		}
		columns = append(columns, col)
	}
	return columns, nil
}

func (c *duckdbConn) GetIndexes(ctx context.Context, table string) ([]driver.IndexInfo, error) {
	query := "SELECT index_name, table_name, is_unique, expressions FROM duckdb_indexes() ORDER BY index_name"
	if table != "" {
		escaped := strings.ReplaceAll(table, "'", "''")
		query = fmt.Sprintf(
			"SELECT index_name, table_name, is_unique, expressions FROM duckdb_indexes() WHERE table_name = '%s' ORDER BY index_name",
			escaped,
		)
	}

	rows, err := c.exec(ctx, query)
	if err != nil {
		return nil, err
	}

	indexes := make([]driver.IndexInfo, 0, len(rows))
	for _, r := range rows {
		name, _ := r["index_name"].(string)
		tbl, _ := r["table_name"].(string)
		expr, _ := r["expressions"].(string)
		unique := toBool(r["is_unique"])
		indexes = append(indexes, driver.IndexInfo{
			Name:    name,
			Table:   tbl,
			Columns: parseExpressionList(expr),
			Unique:  unique,
		})
	}
	return indexes, nil
}

func (c *duckdbConn) GetConstraints(ctx context.Context, table string) ([]driver.ConstraintInfo, error) {
	query := "SELECT constraint_type, table_name, constraint_column_names FROM duckdb_constraints() ORDER BY table_name"
	if table != "" {
		escaped := strings.ReplaceAll(table, "'", "''")
		query = fmt.Sprintf(
			"SELECT constraint_type, table_name, constraint_column_names FROM duckdb_constraints() WHERE table_name = '%s' ORDER BY table_name",
			escaped,
		)
	}

	rows, err := c.exec(ctx, query)
	if err != nil {
		return nil, err
	}

	var constraints []driver.ConstraintInfo
	for _, r := range rows {
		constraintType, _ := r["constraint_type"].(string)
		mapped := driver.MapConstraintType(constraintType)
		if mapped == "" {
			continue
		}
		tbl, _ := r["table_name"].(string)
		constraints = append(constraints, driver.ConstraintInfo{
			Name:    fmt.Sprintf("%s_%s", tbl, mapped),
			Table:   tbl,
			Type:    mapped,
			Columns: parseColumnList(r["constraint_column_names"]),
		})
	}
	return constraints, nil
}

func (c *duckdbConn) SearchSchema(ctx context.Context, pattern string) (*driver.SearchResult, error) {
	escaped := strings.ReplaceAll(pattern, "'", "''")

	tableQuery := fmt.Sprintf(
		"SELECT table_name FROM information_schema.tables WHERE table_schema = 'main' AND table_name LIKE '%%%s%%' ORDER BY table_name",
		escaped,
	)
	colQuery := fmt.Sprintf(
		"SELECT table_name, column_name FROM information_schema.columns WHERE table_schema = 'main' AND column_name LIKE '%%%s%%' ORDER BY table_name, column_name",
		escaped,
	)

	tableRows, err := c.exec(ctx, tableQuery)
	if err != nil {
		return nil, err
	}

	colRows, err := c.exec(ctx, colQuery)
	if err != nil {
		return nil, err
	}

	tables := make([]driver.TableInfo, 0, len(tableRows))
	for _, r := range tableRows {
		name, _ := r["table_name"].(string)
		tables = append(tables, driver.TableInfo{Name: name})
	}

	columns := make([]driver.ColumnMatch, 0, len(colRows))
	for _, r := range colRows {
		tbl, _ := r["table_name"].(string)
		col, _ := r["column_name"].(string)
		columns = append(columns, driver.ColumnMatch{Table: tbl, Column: col})
	}

	return &driver.SearchResult{Tables: tables, Columns: columns}, nil
}

// --- shared helpers used by schema methods ---

// parseExpressionList parses DuckDB's expressions format: "[col1, col2]".
func parseExpressionList(expr string) []string {
	if expr == "" {
		return nil
	}
	expr = strings.TrimPrefix(expr, "[")
	expr = strings.TrimSuffix(expr, "]")
	parts := strings.Split(expr, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

// parseColumnList parses DuckDB constraint_column_names which may be a
// JSON array or a bracket string.
func parseColumnList(value any) []string {
	switch v := value.(type) {
	case []any:
		result := make([]string, 0, len(v))
		for _, item := range v {
			result = append(result, fmt.Sprintf("%v", item))
		}
		return result
	case string:
		return parseExpressionList(v)
	default:
		return nil
	}
}

func toBool(v any) bool {
	switch b := v.(type) {
	case bool:
		return b
	case string:
		return b == "true"
	default:
		return false
	}
}
