// Package snowflake implements the Snowflake driver using the SQL REST API v2.
// Uses net/http (no external dependencies) with PAT (Personal Access Token) auth.
package snowflake

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/shhac/agent-sql/internal/driver"
	"github.com/shhac/agent-sql/internal/errors"
)

// Opts holds Snowflake connection options.
type Opts struct {
	Account   string
	Database  string
	Schema    string
	Warehouse string
	Role      string
	Token     string // PAT secret
	Readonly  bool
	BaseURL   string // override API base URL (for testing with mock servers)
}

// Connect creates a new Snowflake connection via the SQL REST API v2.
func Connect(opts Opts) (driver.Connection, error) {
	if opts.Account == "" {
		return nil, errors.New("Snowflake account is required", errors.FixableByHuman)
	}
	if opts.Token == "" {
		return nil, errors.New("Snowflake PAT token is required", errors.FixableByHuman)
	}

	baseURL := opts.BaseURL
	if baseURL == "" {
		baseURL = "https://" + opts.Account + ".snowflakecomputing.com"
	}
	defaultSchema := opts.Schema
	if defaultSchema == "" {
		defaultSchema = "PUBLIC"
	}

	return &snowflakeConn{
		baseURL:       baseURL,
		token:         opts.Token,
		database:      opts.Database,
		schema:        opts.Schema,
		warehouse:     opts.Warehouse,
		role:          opts.Role,
		readonly:      opts.Readonly,
		defaultSchema: defaultSchema,
		client:        &http.Client{Timeout: 60 * time.Second},
	}, nil
}

type snowflakeConn struct {
	baseURL       string
	token         string
	database      string
	schema        string
	warehouse     string
	role          string
	readonly      bool
	defaultSchema string
	client        *http.Client
}

// -- API types ----------------------------------------------------------------

type statementRequest struct {
	Statement  string             `json:"statement"`
	Timeout    int                `json:"timeout,omitempty"`
	Database   string             `json:"database,omitempty"`
	Schema     string             `json:"schema,omitempty"`
	Warehouse  string             `json:"warehouse,omitempty"`
	Role       string             `json:"role,omitempty"`
	Parameters map[string]string  `json:"parameters,omitempty"`
	Bindings   map[string]binding `json:"bindings,omitempty"`
}

type binding struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

type columnType struct {
	Name      string `json:"name"`
	Type      string `json:"type"`
	Nullable  bool   `json:"nullable"`
	Scale     *int   `json:"scale,omitempty"`
	Precision *int   `json:"precision,omitempty"`
}

type resultMetadata struct {
	NumRows int          `json:"numRows"`
	RowType []columnType `json:"rowType"`
}

type apiResponse struct {
	Code               string          `json:"code"`
	Message            string          `json:"message"`
	SQLState           string          `json:"sqlState"`
	StatementHandle    string          `json:"statementHandle"`
	StatementStatusURL string          `json:"statementStatusUrl"`
	ResultSetMetaData  *resultMetadata `json:"resultSetMetaData,omitempty"`
	Data               [][]*string     `json:"data,omitempty"`
}

func (r *apiResponse) isAsync() bool { return r.Code == "333334" }
func (r *apiResponse) isQuery() bool { return r.ResultSetMetaData != nil && r.Data != nil }

// -- Connection interface -----------------------------------------------------

func (c *snowflakeConn) Close() error { return nil }

func (c *snowflakeConn) QuoteIdent(name string) string {
	return driver.QuoteIdentDot(name)
}

func (c *snowflakeConn) Query(ctx context.Context, sqlStr string, opts driver.QueryOpts) (*driver.QueryResult, error) {
	if c.readonly && !opts.Write {
		if err := validateReadOnly(sqlStr); err != nil {
			return nil, err
		}
	}

	resp, err := c.executeStatement(ctx, sqlStr, nil)
	if err != nil {
		return nil, classifyError(err)
	}

	if resp.ResultSetMetaData == nil {
		return &driver.QueryResult{}, nil
	}

	columns := extractColumns(resp.ResultSetMetaData.RowType)
	rows := parseRows(resp.Data, resp.ResultSetMetaData.RowType)

	return &driver.QueryResult{
		Columns: columns,
		Rows:    rows,
	}, nil
}

func (c *snowflakeConn) GetTables(ctx context.Context, includeSystem bool) ([]driver.TableInfo, error) {
	systemFilter := ""
	if !includeSystem {
		systemFilter = "AND TABLE_SCHEMA NOT IN ('INFORMATION_SCHEMA')"
	}
	sqlStr := fmt.Sprintf(`
		SELECT TABLE_SCHEMA, TABLE_NAME, TABLE_TYPE
		FROM INFORMATION_SCHEMA.TABLES
		WHERE TABLE_CATALOG = CURRENT_DATABASE()
			%s
		ORDER BY TABLE_SCHEMA, TABLE_NAME
	`, systemFilter)

	resp, err := c.executeStatement(ctx, sqlStr, nil)
	if err != nil {
		return nil, classifyError(err)
	}

	rows := parseRows(resp.Data, resp.ResultSetMetaData.RowType)
	tables := make([]driver.TableInfo, 0, len(rows))
	for _, row := range rows {
		schema := stringVal(row, "TABLE_SCHEMA")
		name := stringVal(row, "TABLE_NAME")
		tableType := stringVal(row, "TABLE_TYPE")
		typ := "table"
		if tableType == "VIEW" {
			typ = "view"
		}
		tables = append(tables, driver.TableInfo{
			Name:   schema + "." + name,
			Schema: schema,
			Type:   typ,
		})
	}
	return tables, nil
}

func (c *snowflakeConn) DescribeTable(ctx context.Context, table string) ([]driver.ColumnInfo, error) {
	schema, tbl := c.parseTableRef(table)

	sqlStr := `
		SELECT COLUMN_NAME, DATA_TYPE, IS_NULLABLE, COLUMN_DEFAULT, ORDINAL_POSITION
		FROM INFORMATION_SCHEMA.COLUMNS
		WHERE TABLE_CATALOG = CURRENT_DATABASE()
			AND TABLE_SCHEMA = ?
			AND TABLE_NAME = ?
		ORDER BY ORDINAL_POSITION
	`
	bindings := map[string]binding{
		"1": {Type: "TEXT", Value: schema},
		"2": {Type: "TEXT", Value: tbl},
	}

	resp, err := c.executeStatement(ctx, sqlStr, bindings)
	if err != nil {
		return nil, classifyError(err)
	}

	rows := parseRows(resp.Data, resp.ResultSetMetaData.RowType)

	// Fetch PKs to mark columns
	pkCols := c.primaryKeyCols(ctx, schema, tbl)

	columns := make([]driver.ColumnInfo, 0, len(rows))
	for _, row := range rows {
		colName := stringVal(row, "COLUMN_NAME")
		col := driver.ColumnInfo{
			Name:       colName,
			Type:       stringVal(row, "DATA_TYPE"),
			Nullable:   stringVal(row, "IS_NULLABLE") == "YES",
			PrimaryKey: pkCols[colName],
		}
		if v, ok := row["COLUMN_DEFAULT"]; ok && v != nil {
			col.DefaultValue = fmt.Sprint(v)
		}
		columns = append(columns, col)
	}
	return columns, nil
}

func (c *snowflakeConn) GetIndexes(_ context.Context, _ string) ([]driver.IndexInfo, error) {
	// Snowflake uses micro-partitioning, no traditional indexes
	return []driver.IndexInfo{}, nil
}

func (c *snowflakeConn) GetConstraints(ctx context.Context, table string) ([]driver.ConstraintInfo, error) {
	var constraints []driver.ConstraintInfo

	var inClause string
	if table != "" {
		schema, tbl := c.parseTableRef(table)
		inClause = " IN " + c.QuoteIdent(schema+"."+tbl)
	} else {
		inClause = " IN SCHEMA " + c.QuoteIdent(c.defaultSchema)
	}

	// Primary keys
	if pks, err := c.fetchConstraints(ctx, inClause, "SHOW PRIMARY KEYS", "constraint_name", "column_name", driver.ConstraintPrimaryKey); err == nil {
		constraints = append(constraints, pks...)
	}

	// Foreign keys
	if fkRows, err := c.execShowCommand(ctx, "SHOW IMPORTED KEYS"+inClause); err == nil {
		for _, group := range groupByField(fkRows, "fk_constraint_name") {
			first := group[0]
			sorted := sortByKeySequence(group)
			fkCols := make([]string, len(sorted))
			pkCols := make([]string, len(sorted))
			for i, r := range sorted {
				fkCols[i] = stringVal(r, "fk_column_name")
				pkCols[i] = stringVal(r, "pk_column_name")
			}
			constraints = append(constraints, driver.ConstraintInfo{
				Name:              stringVal(first, "fk_constraint_name"),
				Table:             stringVal(first, "fk_table_name"),
				Schema:            stringVal(first, "fk_schema_name"),
				Type:              driver.ConstraintForeignKey,
				Columns:           fkCols,
				ReferencedTable:   stringVal(first, "pk_table_name"),
				ReferencedColumns: pkCols,
			})
		}
	}

	// Unique keys
	if uks, err := c.fetchConstraints(ctx, inClause, "SHOW UNIQUE KEYS", "constraint_name", "column_name", driver.ConstraintUnique); err == nil {
		constraints = append(constraints, uks...)
	}

	return constraints, nil
}

func (c *snowflakeConn) SearchSchema(ctx context.Context, pattern string) (*driver.SearchResult, error) {
	ilike := "%" + pattern + "%"
	ilikeBinding := map[string]binding{
		"1": {Type: "TEXT", Value: ilike},
	}

	// Search tables
	tableResp, err := c.executeStatement(ctx, `
		SELECT TABLE_SCHEMA, TABLE_NAME
		FROM INFORMATION_SCHEMA.TABLES
		WHERE TABLE_CATALOG = CURRENT_DATABASE()
			AND TABLE_SCHEMA NOT IN ('INFORMATION_SCHEMA')
			AND TABLE_NAME ILIKE ?
		ORDER BY TABLE_SCHEMA, TABLE_NAME
	`, ilikeBinding)
	if err != nil {
		return nil, classifyError(err)
	}

	tableRows := parseRows(tableResp.Data, tableResp.ResultSetMetaData.RowType)
	tables := make([]driver.TableInfo, 0, len(tableRows))
	for _, row := range tableRows {
		schema := stringVal(row, "TABLE_SCHEMA")
		name := stringVal(row, "TABLE_NAME")
		tables = append(tables, driver.TableInfo{
			Name:   schema + "." + name,
			Schema: schema,
		})
	}

	// Search columns
	colResp, err := c.executeStatement(ctx, `
		SELECT TABLE_SCHEMA, TABLE_NAME, COLUMN_NAME
		FROM INFORMATION_SCHEMA.COLUMNS
		WHERE TABLE_CATALOG = CURRENT_DATABASE()
			AND TABLE_SCHEMA NOT IN ('INFORMATION_SCHEMA')
			AND COLUMN_NAME ILIKE ?
		ORDER BY TABLE_SCHEMA, TABLE_NAME, COLUMN_NAME
	`, ilikeBinding)
	if err != nil {
		return nil, classifyError(err)
	}

	colRows := parseRows(colResp.Data, colResp.ResultSetMetaData.RowType)
	columns := make([]driver.ColumnMatch, 0, len(colRows))
	for _, row := range colRows {
		schema := stringVal(row, "TABLE_SCHEMA")
		tbl := stringVal(row, "TABLE_NAME")
		columns = append(columns, driver.ColumnMatch{
			Table:  schema + "." + tbl,
			Column: stringVal(row, "COLUMN_NAME"),
		})
	}

	return &driver.SearchResult{Tables: tables, Columns: columns}, nil
}

// -- Helpers ------------------------------------------------------------------

// fetchConstraints extracts simple (non-FK) constraints from a SHOW command.
// The nameField and colField identify which row fields hold the constraint name
// and column name respectively.
func (c *snowflakeConn) fetchConstraints(ctx context.Context, inClause, showCmd, nameField, colField string, cType driver.ConstraintType) ([]driver.ConstraintInfo, error) {
	rows, err := c.execShowCommand(ctx, showCmd+inClause)
	if err != nil {
		return nil, err
	}
	var result []driver.ConstraintInfo
	for _, group := range groupByField(rows, nameField) {
		first := group[0]
		sorted := sortByKeySequence(group)
		cols := make([]string, len(sorted))
		for i, r := range sorted {
			cols[i] = stringVal(r, colField)
		}
		result = append(result, driver.ConstraintInfo{
			Name:    stringVal(first, nameField),
			Table:   stringVal(first, "table_name"),
			Schema:  stringVal(first, "schema_name"),
			Type:    cType,
			Columns: cols,
		})
	}
	return result, nil
}

func (c *snowflakeConn) parseTableRef(table string) (schema, tbl string) {
	return driver.SplitSchemaTable(table, c.defaultSchema)
}

func (c *snowflakeConn) primaryKeyCols(ctx context.Context, schema, table string) map[string]bool {
	rows, err := c.execShowCommand(ctx, "SHOW PRIMARY KEYS IN "+c.QuoteIdent(schema+"."+table))
	if err != nil {
		return nil
	}
	result := make(map[string]bool)
	for _, row := range rows {
		result[stringVal(row, "column_name")] = true
	}
	return result
}

func (c *snowflakeConn) execShowCommand(ctx context.Context, sqlStr string) ([]map[string]any, error) {
	resp, err := c.executeStatement(ctx, sqlStr, nil)
	if err != nil {
		return nil, err
	}
	if resp.ResultSetMetaData == nil {
		return nil, fmt.Errorf("no result metadata")
	}
	return parseRows(resp.Data, resp.ResultSetMetaData.RowType), nil
}

// stringVal extracts a string value from a row, trying both cases (Snowflake
// SHOW commands may return column names in varying case).
func stringVal(row map[string]any, name string) string {
	if v, ok := row[name]; ok && v != nil {
		return fmt.Sprint(v)
	}
	if v, ok := row[strings.ToUpper(name)]; ok && v != nil {
		return fmt.Sprint(v)
	}
	if v, ok := row[strings.ToLower(name)]; ok && v != nil {
		return fmt.Sprint(v)
	}
	return ""
}

// groupByField groups rows by a field value, returning groups in order of first appearance.
func groupByField(rows []map[string]any, field string) [][]map[string]any {
	orderMap := make(map[string]int)
	groups := make(map[string][]map[string]any)
	var keys []string

	for _, row := range rows {
		key := stringVal(row, field)
		if _, seen := orderMap[key]; !seen {
			orderMap[key] = len(keys)
			keys = append(keys, key)
		}
		groups[key] = append(groups[key], row)
	}

	result := make([][]map[string]any, len(keys))
	for i, key := range keys {
		result[i] = groups[key]
	}
	return result
}

func sortByKeySequence(rows []map[string]any) []map[string]any {
	sorted := make([]map[string]any, len(rows))
	copy(sorted, rows)
	// Simple insertion sort (constraint groups are small)
	for i := 1; i < len(sorted); i++ {
		tmp := sorted[i]
		key := keySeqNum(tmp)
		j := i
		for j > 0 && keySeqNum(sorted[j-1]) > key {
			sorted[j] = sorted[j-1]
			j--
		}
		sorted[j] = tmp
	}
	return sorted
}

func keySeqNum(row map[string]any) int {
	s := stringVal(row, "key_sequence")
	n, _ := strconv.Atoi(s)
	return n
}
