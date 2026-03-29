// Package snowflake implements the Snowflake driver using the SQL REST API v2.
// Uses net/http (no external dependencies) with PAT (Personal Access Token) auth.
package snowflake

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"

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
}

// readOnlyAllowed lists statement types permitted in read-only mode.
var readOnlyAllowed = []string{
	"SELECT", "WITH", "SHOW", "DESCRIBE", "DESC", "EXPLAIN", "LIST", "LS",
}

// pollIntervals defines the backoff intervals for async polling.
var pollIntervals = []time.Duration{
	500 * time.Millisecond,
	500 * time.Millisecond,
	1 * time.Second,
	1500 * time.Millisecond,
	2 * time.Second,
	4 * time.Second,
	5 * time.Second,
}

const maxRetries = 3

// Connect creates a new Snowflake connection via the SQL REST API v2.
func Connect(opts Opts) (driver.Connection, error) {
	if opts.Account == "" {
		return nil, errors.New("Snowflake account is required", errors.FixableByHuman)
	}
	if opts.Token == "" {
		return nil, errors.New("Snowflake PAT token is required", errors.FixableByHuman)
	}

	baseURL := "https://" + opts.Account + ".snowflakecomputing.com"
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
	Statement  string            `json:"statement"`
	Timeout    int               `json:"timeout,omitempty"`
	Database   string            `json:"database,omitempty"`
	Schema     string            `json:"schema,omitempty"`
	Warehouse  string            `json:"warehouse,omitempty"`
	Role       string            `json:"role,omitempty"`
	Parameters map[string]string `json:"parameters,omitempty"`
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

func (r *apiResponse) isAsync() bool  { return r.Code == "333334" }
func (r *apiResponse) isQuery() bool  { return r.ResultSetMetaData != nil && r.Data != nil }

// -- Connection interface -----------------------------------------------------

func (c *snowflakeConn) Close() error { return nil }

func (c *snowflakeConn) QuoteIdent(name string) string {
	parts := strings.Split(name, ".")
	for i, p := range parts {
		parts[i] = `"` + strings.ReplaceAll(p, `"`, `""`) + `"`
	}
	return strings.Join(parts, ".")
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
	if pkRows, err := c.execShowCommand(ctx, "SHOW PRIMARY KEYS"+inClause); err == nil {
		for _, group := range groupByField(pkRows, "constraint_name") {
			first := group[0]
			sorted := sortByKeySequence(group)
			cols := make([]string, len(sorted))
			for i, r := range sorted {
				cols[i] = stringVal(r, "column_name")
			}
			constraints = append(constraints, driver.ConstraintInfo{
				Name:    stringVal(first, "constraint_name"),
				Table:   stringVal(first, "table_name"),
				Schema:  stringVal(first, "schema_name"),
				Type:    driver.ConstraintPrimaryKey,
				Columns: cols,
			})
		}
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
	if ukRows, err := c.execShowCommand(ctx, "SHOW UNIQUE KEYS"+inClause); err == nil {
		for _, group := range groupByField(ukRows, "constraint_name") {
			first := group[0]
			sorted := sortByKeySequence(group)
			cols := make([]string, len(sorted))
			for i, r := range sorted {
				cols[i] = stringVal(r, "column_name")
			}
			constraints = append(constraints, driver.ConstraintInfo{
				Name:    stringVal(first, "constraint_name"),
				Table:   stringVal(first, "table_name"),
				Schema:  stringVal(first, "schema_name"),
				Type:    driver.ConstraintUnique,
				Columns: cols,
			})
		}
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

// -- HTTP / API ---------------------------------------------------------------

func (c *snowflakeConn) executeStatement(ctx context.Context, sqlStr string, binds map[string]binding) (*apiResponse, error) {
	req := statementRequest{
		Statement: sqlStr,
		Timeout:   45,
		Database:  c.database,
		Schema:    c.schema,
		Warehouse: c.warehouse,
		Role:      c.role,
		Parameters: map[string]string{
			"MULTI_STATEMENT_COUNT": "1",
		},
	}
	if len(binds) > 0 {
		req.Bindings = binds
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := c.baseURL + "/api/v2/statements"
	resp, err := c.doWithRetry(ctx, http.MethodPost, url, body)
	if err != nil {
		return nil, err
	}

	if resp.isAsync() {
		return c.pollForResult(ctx, resp.StatementHandle)
	}

	if resp.ResultSetMetaData == nil && resp.Message != "" && resp.Code != "090001" {
		return nil, &snowflakeAPIError{Code: resp.Code, Msg: resp.Message, SQLState: resp.SQLState}
	}

	return resp, nil
}

func (c *snowflakeConn) pollForResult(ctx context.Context, handle string) (*apiResponse, error) {
	url := c.baseURL + "/api/v2/statements/" + handle

	for attempt := range 100 {
		idx := attempt
		if idx >= len(pollIntervals) {
			idx = len(pollIntervals) - 1
		}
		delay := pollIntervals[idx]

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}

		resp, err := c.doWithRetry(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}

		if resp.isQuery() {
			return resp, nil
		}

		if !resp.isAsync() {
			return nil, &snowflakeAPIError{Code: resp.Code, Msg: resp.Message, SQLState: resp.SQLState}
		}
	}

	return nil, errors.New("Snowflake query timed out after polling", errors.FixableByRetry)
}

func (c *snowflakeConn) doWithRetry(ctx context.Context, method, url string, body []byte) (*apiResponse, error) {
	var lastErr error
	for attempt := range maxRetries + 1 {
		if attempt > 0 {
			// Exponential backoff with jitter-like delay
			backoff := time.Duration(1<<uint(attempt-1)) * time.Second
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
		}

		var bodyReader io.Reader
		if body != nil {
			bodyReader = bytes.NewReader(body)
		}

		httpReq, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
		if err != nil {
			return nil, fmt.Errorf("create request: %w", err)
		}

		httpReq.Header.Set("Authorization", "Bearer "+c.token)
		httpReq.Header.Set("X-Snowflake-Authorization-Token-Type", "PROGRAMMATIC_ACCESS_TOKEN")
		httpReq.Header.Set("Accept", "application/json")
		if body != nil {
			httpReq.Header.Set("Content-Type", "application/json")
		}

		httpResp, err := c.client.Do(httpReq)
		if err != nil {
			lastErr = err
			if isRetryable(0) {
				continue
			}
			return nil, err
		}

		respBody, err := io.ReadAll(httpResp.Body)
		httpResp.Body.Close()
		if err != nil {
			lastErr = err
			continue
		}

		if isRetryable(httpResp.StatusCode) {
			lastErr = fmt.Errorf("HTTP %d", httpResp.StatusCode)
			continue
		}

		var apiResp apiResponse
		if err := json.Unmarshal(respBody, &apiResp); err != nil {
			return nil, fmt.Errorf("unmarshal response: %w (body: %s)", err, truncateBody(respBody))
		}

		return &apiResp, nil
	}

	return nil, fmt.Errorf("request failed after %d retries: %w", maxRetries+1, lastErr)
}

func isRetryable(status int) bool {
	return status == 429 || status == 408 || status >= 500
}

func truncateBody(b []byte) string {
	if len(b) > 200 {
		return string(b[:200]) + "..."
	}
	return string(b)
}

// -- Read-only guard ----------------------------------------------------------

// ValidateReadOnly checks if a SQL statement is allowed in read-only mode.
// Exported for testing.
func ValidateReadOnly(sqlStr string) error {
	return validateReadOnly(sqlStr)
}

func validateReadOnly(sqlStr string) error {
	trimmed := strings.TrimLeftFunc(sqlStr, unicode.IsSpace)
	upper := strings.ToUpper(trimmed)

	for _, keyword := range readOnlyAllowed {
		if strings.HasPrefix(upper, keyword) &&
			(len(trimmed) == len(keyword) || trimmed[len(keyword)] == ' ' ||
				trimmed[len(keyword)] == '\t' || trimmed[len(keyword)] == '\n' ||
				trimmed[len(keyword)] == '(') {
			return nil
		}
	}

	firstWord := upper
	if idx := strings.IndexFunc(upper, unicode.IsSpace); idx > 0 {
		firstWord = upper[:idx]
	}

	return errors.New(
		fmt.Sprintf("Statement type '%s' is not allowed in read-only mode. Allowed: SELECT, SHOW, DESCRIBE, EXPLAIN.", firstWord),
		errors.FixableByHuman,
	).WithHint("To execute write operations, use a connection with a write-enabled credential and pass --write.")
}

// -- Result parsing -----------------------------------------------------------

func extractColumns(rowType []columnType) []string {
	cols := make([]string, len(rowType))
	for i, col := range rowType {
		cols[i] = col.Name
	}
	return cols
}

func parseRows(data [][]*string, rowType []columnType) []map[string]any {
	rows := make([]map[string]any, 0, len(data))
	for _, rawRow := range data {
		row := make(map[string]any, len(rowType))
		for i, col := range rowType {
			if i < len(rawRow) {
				row[col.Name] = parseValue(rawRow[i], col)
			} else {
				row[col.Name] = nil
			}
		}
		rows = append(rows, row)
	}
	return rows
}

// ParseValue converts a raw Snowflake string value to the appropriate Go type.
// Exported for testing.
func ParseValue(raw *string, col columnType) any {
	return parseValue(raw, col)
}

func parseValue(raw *string, col columnType) any {
	if raw == nil {
		return nil
	}
	v := *raw

	switch strings.ToLower(col.Type) {
	case "fixed":
		scale := 0
		if col.Scale != nil {
			scale = *col.Scale
		}
		if scale == 0 {
			n, err := strconv.ParseInt(v, 10, 64)
			if err == nil {
				return n
			}
			return v // keep as string for very large numbers
		}
		f, err := strconv.ParseFloat(v, 64)
		if err == nil {
			return f
		}
		return v

	case "real", "float", "double":
		f, err := strconv.ParseFloat(v, 64)
		if err == nil {
			return f
		}
		return v

	case "boolean":
		return strings.EqualFold(v, "true") || v == "1"

	case "text", "varchar", "char", "string":
		return v

	case "variant", "object", "array", "map":
		var parsed any
		if err := json.Unmarshal([]byte(v), &parsed); err == nil {
			return parsed
		}
		return v

	case "date", "time", "timestamp_ltz", "timestamp_ntz", "timestamp_tz", "binary":
		return v

	default:
		return v
	}
}

// -- Helpers ------------------------------------------------------------------

func (c *snowflakeConn) parseTableRef(table string) (schema, tbl string) {
	if idx := strings.IndexByte(table, '.'); idx >= 0 {
		return table[:idx], table[idx+1:]
	}
	return c.defaultSchema, table
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

// -- Error types and classification -------------------------------------------

type snowflakeAPIError struct {
	Code     string
	Msg      string
	SQLState string
}

func (e *snowflakeAPIError) Error() string {
	if e.SQLState != "" {
		return fmt.Sprintf("Snowflake error %s (SQLState %s): %s", e.Code, e.SQLState, e.Msg)
	}
	return fmt.Sprintf("Snowflake error %s: %s", e.Code, e.Msg)
}

// ClassifyError classifies a Snowflake error. Exported for testing.
func ClassifyError(err error) error {
	return classifyError(err)
}

func classifyError(err error) error {
	if err == nil {
		return nil
	}

	// Already classified
	var qerr *errors.QueryError
	if errors.As(err, &qerr) {
		return qerr
	}

	msg := err.Error()

	var apiErr *snowflakeAPIError
	if asAPIError(err, &apiErr) {
		return classifyAPIError(apiErr)
	}

	// Generic message-based classification
	if strings.Contains(msg, "does not exist or not authorized") {
		return errors.New(msg, errors.FixableByAgent).
			WithHint("Object not found. Use 'schema tables' to see available tables.")
	}
	if strings.Contains(msg, "Authentication") || strings.Contains(msg, "Unauthorized") {
		return errors.New(msg, errors.FixableByHuman).
			WithHint("Authentication failed. Check your Snowflake PAT token.")
	}

	return errors.Wrap(err, errors.FixableByAgent)
}

func classifyAPIError(apiErr *snowflakeAPIError) error {
	msg := apiErr.Error()

	switch {
	case strings.Contains(apiErr.Msg, "does not exist or not authorized"):
		return errors.New(msg, errors.FixableByAgent).
			WithHint("Object not found. Use 'schema tables' to see available tables.")

	case apiErr.Code == "000606" || strings.Contains(apiErr.Msg, "No active warehouse"):
		return errors.New(msg, errors.FixableByHuman).
			WithHint("No active warehouse selected. Set a warehouse in your connection config.")

	case strings.Contains(apiErr.Msg, "Insufficient privileges"):
		return errors.New(msg, errors.FixableByHuman).
			WithHint("Insufficient permissions. Ask your Snowflake admin for the required role/grants.")

	case apiErr.SQLState == "42000" || apiErr.SQLState == "42601":
		return errors.New(msg, errors.FixableByAgent).
			WithHint("SQL syntax error. Check your query syntax.")

	case apiErr.SQLState == "42S02":
		return errors.New(msg, errors.FixableByAgent).
			WithHint("Table not found. Use 'schema tables' to see available tables.")

	case apiErr.SQLState == "42S22":
		return errors.New(msg, errors.FixableByAgent).
			WithHint("Column not found. Use 'schema describe <table>' to see columns.")

	case apiErr.Code == "390318" || strings.Contains(apiErr.Msg, "Authentication token has expired"):
		return errors.New(msg, errors.FixableByHuman).
			WithHint("PAT token has expired. Generate a new token in Snowflake.")

	case strings.Contains(apiErr.Msg, "timeout") || strings.Contains(apiErr.Msg, "Timeout"):
		return errors.New(msg, errors.FixableByRetry).
			WithHint("Query timed out. Try simplifying the query or increasing the timeout.")
	}

	return errors.New(msg, errors.FixableByAgent)
}

func asAPIError(err error, target **snowflakeAPIError) bool {
	if ae, ok := err.(*snowflakeAPIError); ok {
		*target = ae
		return true
	}
	return false
}
