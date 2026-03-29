package mysql

import (
	"context"
	"strings"

	"github.com/shhac/agent-sql/internal/driver"
)

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
