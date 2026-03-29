package pg

import (
	"context"
	"strings"

	"github.com/shhac/agent-sql/internal/driver"
)

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
