// Package snowflake implements the Snowflake driver using the SQL REST API v2.
// Uses net/http (no external dependencies) with PAT (Personal Access Token) auth.
package snowflake

import (
	"context"
	"net/http"
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
	// Options are session parameters threaded into every statement's
	// `parameters` field on the REST v2 endpoint (e.g. QUERY_TAG,
	// TIMEZONE, STATEMENT_TIMEOUT_IN_SECONDS). Pass-through: Snowflake
	// rejects unknown parameters at execution time. The MULTI_STATEMENT_COUNT
	// safety value is always forced to 1; user input cannot override it.
	Options map[string]string
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
		options:       opts.Options,
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
	options       map[string]string
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

func (c *snowflakeConn) BuildSampleSelect(quotedTable, whereClause string, n int) string {
	return driver.SuffixLimitSelect(quotedTable, whereClause, n)
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

