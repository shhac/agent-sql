package snowflake

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/shhac/agent-sql/internal/driver"
	"github.com/shhac/agent-sql/internal/errors"
)

// -- Read-only guard tests ----------------------------------------------------

func TestValidateReadOnly(t *testing.T) {
	allowed := []struct {
		sql  string
		desc string
	}{
		{"SELECT 1", "simple select"},
		{"  SELECT * FROM t", "leading whitespace"},
		{"select count(*) FROM t", "lowercase select"},
		{"WITH cte AS (SELECT 1) SELECT * FROM cte", "CTE"},
		{"SHOW TABLES", "show"},
		{"DESCRIBE TABLE users", "describe"},
		{"DESC TABLE users", "desc alias"},
		{"EXPLAIN SELECT 1", "explain"},
		{"LIST @stage", "list"},
		{"LS @stage", "ls alias"},
		{"SELECT(1)", "select with paren"},
		{"\n\t SELECT 1", "tabs and newlines"},
	}

	for _, tc := range allowed {
		t.Run("allows "+tc.desc, func(t *testing.T) {
			if err := ValidateReadOnly(tc.sql); err != nil {
				t.Errorf("expected allowed, got error: %v", err)
			}
		})
	}

	blocked := []struct {
		sql      string
		desc     string
		contains string
	}{
		{"INSERT INTO t VALUES(1)", "insert", "INSERT"},
		{"UPDATE t SET x=1", "update", "UPDATE"},
		{"DELETE FROM t", "delete", "DELETE"},
		{"CREATE TABLE t(x INT)", "create", "CREATE"},
		{"ALTER TABLE t ADD COLUMN y INT", "alter", "ALTER"},
		{"DROP TABLE t", "drop", "DROP"},
		{"TRUNCATE TABLE t", "truncate", "TRUNCATE"},
		{"MERGE INTO t USING s", "merge", "MERGE"},
		{"GRANT SELECT ON t TO role", "grant", "GRANT"},
		{"REVOKE SELECT ON t FROM role", "revoke", "REVOKE"},
	}

	for _, tc := range blocked {
		t.Run("blocks "+tc.desc, func(t *testing.T) {
			err := ValidateReadOnly(tc.sql)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tc.contains) {
				t.Errorf("error %q should contain %q", err.Error(), tc.contains)
			}
			var qerr *errors.QueryError
			if errors.As(err, &qerr) {
				if qerr.FixableBy != errors.FixableByHuman {
					t.Errorf("fixableBy = %s, want human", qerr.FixableBy)
				}
			} else {
				t.Error("expected QueryError")
			}
		})
	}
}

func TestValidateReadOnlyEdgeCases(t *testing.T) {
	t.Run("does not allow SELECTIVE as SELECT", func(t *testing.T) {
		err := ValidateReadOnly("SELECTIVE_INSERT foo")
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("empty string is blocked", func(t *testing.T) {
		err := ValidateReadOnly("")
		if err == nil {
			t.Fatal("expected error for empty SQL")
		}
	})
}

// -- QuoteIdent tests ---------------------------------------------------------

func TestQuoteIdent(t *testing.T) {
	conn := &snowflakeConn{defaultSchema: "PUBLIC"}

	tests := []struct {
		input string
		want  string
	}{
		{"table", `"table"`},
		{`my"table`, `"my""table"`},
		{"schema.table", `"schema"."table"`},
		{`s"a.t"b`, `"s""a"."t""b"`},
		{"a.b.c", `"a"."b"."c"`},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := conn.QuoteIdent(tc.input)
			if got != tc.want {
				t.Errorf("QuoteIdent(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

// -- Result parsing tests -----------------------------------------------------

func TestParseValue(t *testing.T) {
	s := func(v string) *string { return &v }
	scale0 := 0
	scale2 := 2

	tests := []struct {
		desc string
		raw  *string
		col  columnType
		want any
	}{
		{"null value", nil, columnType{Name: "x", Type: "text"}, nil},
		{"text", s("hello"), columnType{Name: "x", Type: "text"}, "hello"},
		{"varchar", s("world"), columnType{Name: "x", Type: "varchar"}, "world"},
		{"integer (fixed scale=0)", s("42"), columnType{Name: "x", Type: "fixed", Scale: &scale0}, int64(42)},
		{"decimal (fixed scale=2)", s("3.14"), columnType{Name: "x", Type: "fixed", Scale: &scale2}, 3.14},
		{"float", s("2.718"), columnType{Name: "x", Type: "float"}, 2.718},
		{"real", s("1.5"), columnType{Name: "x", Type: "real"}, 1.5},
		{"double", s("9.99"), columnType{Name: "x", Type: "double"}, 9.99},
		{"boolean true", s("true"), columnType{Name: "x", Type: "boolean"}, true},
		{"boolean false", s("false"), columnType{Name: "x", Type: "boolean"}, false},
		{"boolean 1", s("1"), columnType{Name: "x", Type: "boolean"}, true},
		{"boolean 0", s("0"), columnType{Name: "x", Type: "boolean"}, false},
		{"date", s("2024-01-15"), columnType{Name: "x", Type: "date"}, "2024-01-15"},
		{"timestamp_ntz", s("2024-01-15 10:30:00"), columnType{Name: "x", Type: "timestamp_ntz"}, "2024-01-15 10:30:00"},
		{"variant json", s(`{"key":"val"}`), columnType{Name: "x", Type: "variant"}, map[string]any{"key": "val"}},
		{"array json", s(`[1,2,3]`), columnType{Name: "x", Type: "array"}, []any{float64(1), float64(2), float64(3)}},
		{"variant invalid json", s("not-json"), columnType{Name: "x", Type: "variant"}, "not-json"},
		{"binary hex", s("DEADBEEF"), columnType{Name: "x", Type: "binary"}, "DEADBEEF"},
		{"unknown type", s("abc"), columnType{Name: "x", Type: "geography"}, "abc"},
		{"large integer stays string", s("9999999999999999999"), columnType{Name: "x", Type: "fixed", Scale: &scale0}, "9999999999999999999"},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			got := ParseValue(tc.raw, tc.col)
			if fmt.Sprintf("%v", got) != fmt.Sprintf("%v", tc.want) {
				t.Errorf("ParseValue = %v (%T), want %v (%T)", got, got, tc.want, tc.want)
			}
		})
	}
}

func TestParseRows(t *testing.T) {
	s := func(v string) *string { return &v }
	scale0 := 0

	rowType := []columnType{
		{Name: "ID", Type: "fixed", Scale: &scale0},
		{Name: "NAME", Type: "text"},
		{Name: "ACTIVE", Type: "boolean"},
	}

	data := [][]*string{
		{s("1"), s("Alice"), s("true")},
		{s("2"), s("Bob"), s("false")},
		{s("3"), nil, s("true")},
	}

	rows := parseRows(data, rowType)

	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3", len(rows))
	}

	if rows[0]["ID"] != int64(1) {
		t.Errorf("row[0].ID = %v, want 1", rows[0]["ID"])
	}
	if rows[0]["NAME"] != "Alice" {
		t.Errorf("row[0].NAME = %v, want Alice", rows[0]["NAME"])
	}
	if rows[0]["ACTIVE"] != true {
		t.Errorf("row[0].ACTIVE = %v, want true", rows[0]["ACTIVE"])
	}
	if rows[2]["NAME"] != nil {
		t.Errorf("row[2].NAME = %v, want nil", rows[2]["NAME"])
	}
}

func TestExtractColumns(t *testing.T) {
	rowType := []columnType{
		{Name: "COL_A"},
		{Name: "COL_B"},
		{Name: "COL_C"},
	}
	cols := extractColumns(rowType)
	if len(cols) != 3 || cols[0] != "COL_A" || cols[1] != "COL_B" || cols[2] != "COL_C" {
		t.Errorf("extractColumns = %v, want [COL_A COL_B COL_C]", cols)
	}
}

// -- Error classification tests -----------------------------------------------

func TestClassifyError(t *testing.T) {
	t.Run("already classified passes through", func(t *testing.T) {
		orig := errors.New("already done", errors.FixableByRetry)
		got := ClassifyError(orig)
		var qerr *errors.QueryError
		if !errors.As(got, &qerr) || qerr.FixableBy != errors.FixableByRetry {
			t.Errorf("expected pass-through, got %v", got)
		}
	})

	t.Run("API error: not found", func(t *testing.T) {
		apiErr := &snowflakeAPIError{Code: "002003", Msg: "Object 'MISSING' does not exist or not authorized"}
		got := ClassifyError(apiErr)
		var qerr *errors.QueryError
		if !errors.As(got, &qerr) || qerr.FixableBy != errors.FixableByAgent {
			t.Errorf("expected agent-fixable, got %v", got)
		}
	})

	t.Run("API error: no warehouse", func(t *testing.T) {
		apiErr := &snowflakeAPIError{Code: "000606", Msg: "No active warehouse selected"}
		got := ClassifyError(apiErr)
		var qerr *errors.QueryError
		if !errors.As(got, &qerr) || qerr.FixableBy != errors.FixableByHuman {
			t.Errorf("expected human-fixable, got %v", got)
		}
	})

	t.Run("API error: insufficient privileges", func(t *testing.T) {
		apiErr := &snowflakeAPIError{Code: "003001", Msg: "Insufficient privileges to operate on schema"}
		got := ClassifyError(apiErr)
		var qerr *errors.QueryError
		if !errors.As(got, &qerr) || qerr.FixableBy != errors.FixableByHuman {
			t.Errorf("expected human-fixable, got %v", got)
		}
	})

	t.Run("API error: syntax error", func(t *testing.T) {
		apiErr := &snowflakeAPIError{Code: "001003", Msg: "SQL compilation error", SQLState: "42000"}
		got := ClassifyError(apiErr)
		var qerr *errors.QueryError
		if !errors.As(got, &qerr) || qerr.FixableBy != errors.FixableByAgent {
			t.Errorf("expected agent-fixable, got %v", got)
		}
	})

	t.Run("API error: expired token", func(t *testing.T) {
		apiErr := &snowflakeAPIError{Code: "390318", Msg: "Authentication token has expired"}
		got := ClassifyError(apiErr)
		var qerr *errors.QueryError
		if !errors.As(got, &qerr) || qerr.FixableBy != errors.FixableByHuman {
			t.Errorf("expected human-fixable, got %v", got)
		}
	})

	t.Run("API error: timeout", func(t *testing.T) {
		apiErr := &snowflakeAPIError{Code: "000900", Msg: "Query timeout exceeded"}
		got := ClassifyError(apiErr)
		var qerr *errors.QueryError
		if !errors.As(got, &qerr) || qerr.FixableBy != errors.FixableByRetry {
			t.Errorf("expected retry, got %v", got)
		}
	})

	t.Run("generic error falls through", func(t *testing.T) {
		got := ClassifyError(fmt.Errorf("something went wrong"))
		var qerr *errors.QueryError
		if !errors.As(got, &qerr) || qerr.FixableBy != errors.FixableByAgent {
			t.Errorf("expected agent-fixable, got %v", got)
		}
	})

	t.Run("nil error returns nil", func(t *testing.T) {
		got := ClassifyError(nil)
		if got != nil {
			t.Errorf("expected nil, got %v", got)
		}
	})
}

// -- Connect validation tests -------------------------------------------------

func TestConnectValidation(t *testing.T) {
	t.Run("rejects empty account", func(t *testing.T) {
		_, err := Connect(Opts{Token: "test-token"})
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("rejects empty token", func(t *testing.T) {
		_, err := Connect(Opts{Account: "test-account"})
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("succeeds with valid opts", func(t *testing.T) {
		conn, err := Connect(Opts{Account: "test", Token: "tok"})
		if err != nil {
			t.Fatal(err)
		}
		conn.Close()
	})
}

// -- parseTableRef tests ------------------------------------------------------

func TestParseTableRef(t *testing.T) {
	conn := &snowflakeConn{defaultSchema: "PUBLIC"}

	tests := []struct {
		input      string
		wantSchema string
		wantTable  string
	}{
		{"USERS", "PUBLIC", "USERS"},
		{"PUBLIC.USERS", "PUBLIC", "USERS"},
		{"MY_SCHEMA.MY_TABLE", "MY_SCHEMA", "MY_TABLE"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			schema, tbl := conn.parseTableRef(tc.input)
			if schema != tc.wantSchema || tbl != tc.wantTable {
				t.Errorf("parseTableRef(%q) = (%q, %q), want (%q, %q)",
					tc.input, schema, tbl, tc.wantSchema, tc.wantTable)
			}
		})
	}
}

// -- Helper function tests ----------------------------------------------------

func TestStringVal(t *testing.T) {
	row := map[string]any{
		"lower_name": "value1",
		"UPPER_NAME": "value2",
	}

	if got := stringVal(row, "lower_name"); got != "value1" {
		t.Errorf("stringVal lowercase = %q, want value1", got)
	}
	if got := stringVal(row, "UPPER_NAME"); got != "value2" {
		t.Errorf("stringVal uppercase = %q, want value2", got)
	}
	if got := stringVal(row, "missing"); got != "" {
		t.Errorf("stringVal missing = %q, want empty", got)
	}
}

func TestGroupByField(t *testing.T) {
	rows := []map[string]any{
		{"name": "a", "group": "g1"},
		{"name": "b", "group": "g2"},
		{"name": "c", "group": "g1"},
	}

	groups := groupByField(rows, "group")
	if len(groups) != 2 {
		t.Fatalf("groups = %d, want 2", len(groups))
	}
	if len(groups[0]) != 2 {
		t.Errorf("g1 size = %d, want 2", len(groups[0]))
	}
	if len(groups[1]) != 1 {
		t.Errorf("g2 size = %d, want 1", len(groups[1]))
	}
}

func TestSortByKeySequence(t *testing.T) {
	rows := []map[string]any{
		{"column_name": "c", "key_sequence": "3"},
		{"column_name": "a", "key_sequence": "1"},
		{"column_name": "b", "key_sequence": "2"},
	}

	sorted := sortByKeySequence(rows)
	if stringVal(sorted[0], "column_name") != "a" {
		t.Errorf("first = %v, want a", sorted[0])
	}
	if stringVal(sorted[2], "column_name") != "c" {
		t.Errorf("third = %v, want c", sorted[2])
	}
}

// -- Mock HTTP server tests ---------------------------------------------------

func TestQueryWithMockServer(t *testing.T) {
	scale0 := 0

	successResp := apiResponse{
		Code:            "090001",
		Message:         "Statement executed successfully.",
		StatementHandle: "test-handle-123",
		SQLState:        "00000",
		ResultSetMetaData: &resultMetadata{
			NumRows: 2,
			RowType: []columnType{
				{Name: "ID", Type: "fixed", Scale: &scale0},
				{Name: "NAME", Type: "text"},
			},
		},
		Data: [][]*string{
			{strPtr("1"), strPtr("Alice")},
			{strPtr("2"), strPtr("Bob")},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify auth header
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		// Verify MULTI_STATEMENT_COUNT
		var req statementRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if req.Parameters["MULTI_STATEMENT_COUNT"] != "1" {
			http.Error(w, "MULTI_STATEMENT_COUNT must be 1", http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(successResp)
	}))
	defer server.Close()

	conn := &snowflakeConn{
		baseURL:       server.URL,
		token:         "test-token",
		database:      "TESTDB",
		schema:        "PUBLIC",
		defaultSchema: "PUBLIC",
		client:        server.Client(),
	}

	ctx := context.Background()
	result, err := conn.Query(ctx, "SELECT ID, NAME FROM USERS", driver.QueryOpts{})
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Columns) != 2 || result.Columns[0] != "ID" || result.Columns[1] != "NAME" {
		t.Errorf("columns = %v, want [ID NAME]", result.Columns)
	}
	if len(result.Rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(result.Rows))
	}
	if result.Rows[0]["ID"] != int64(1) {
		t.Errorf("row[0].ID = %v (%T), want 1", result.Rows[0]["ID"], result.Rows[0]["ID"])
	}
	if result.Rows[0]["NAME"] != "Alice" {
		t.Errorf("row[0].NAME = %v, want Alice", result.Rows[0]["NAME"])
	}
}

func TestQueryReadonlyBlocksWrite(t *testing.T) {
	conn := &snowflakeConn{
		baseURL:       "https://unused",
		token:         "tok",
		readonly:      true,
		defaultSchema: "PUBLIC",
		client:        &http.Client{},
	}

	ctx := context.Background()
	_, err := conn.Query(ctx, "INSERT INTO t VALUES(1)", driver.QueryOpts{})
	if err == nil {
		t.Fatal("expected error for INSERT in readonly mode")
	}
	var qerr *errors.QueryError
	if !errors.As(err, &qerr) || qerr.FixableBy != errors.FixableByHuman {
		t.Errorf("expected human-fixable, got %v", err)
	}
}

func TestAsyncPolling(t *testing.T) {
	scale0 := 0
	callCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")

		if r.Method == http.MethodPost {
			// Initial POST returns async response
			json.NewEncoder(w).Encode(apiResponse{
				Code:               "333334",
				Message:            "Statement in progress.",
				StatementHandle:    "async-handle",
				StatementStatusURL: "/api/v2/statements/async-handle",
			})
			return
		}

		// GET poll: return result on second poll
		if callCount >= 4 {
			json.NewEncoder(w).Encode(apiResponse{
				Code:            "090001",
				Message:         "Statement executed successfully.",
				StatementHandle: "async-handle",
				SQLState:        "00000",
				ResultSetMetaData: &resultMetadata{
					NumRows: 1,
					RowType: []columnType{
						{Name: "RESULT", Type: "fixed", Scale: &scale0},
					},
				},
				Data: [][]*string{
					{strPtr("42")},
				},
			})
		} else {
			json.NewEncoder(w).Encode(apiResponse{
				Code:            "333334",
				Message:         "Still running.",
				StatementHandle: "async-handle",
			})
		}
	}))
	defer server.Close()

	conn := &snowflakeConn{
		baseURL:       server.URL,
		token:         "test-token",
		defaultSchema: "PUBLIC",
		client:        server.Client(),
	}

	// Override poll intervals for fast testing
	origIntervals := pollIntervals
	pollIntervals = []time.Duration{10 * time.Millisecond}
	defer func() { pollIntervals = origIntervals }()

	ctx := context.Background()
	result, err := conn.Query(ctx, "SELECT 42 AS RESULT", driver.QueryOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Rows) != 1 || result.Rows[0]["RESULT"] != int64(42) {
		t.Errorf("result = %v, want [{RESULT: 42}]", result.Rows)
	}
}

func TestGetIndexesReturnsEmpty(t *testing.T) {
	conn := &snowflakeConn{defaultSchema: "PUBLIC"}
	indexes, err := conn.GetIndexes(context.Background(), "any_table")
	if err != nil {
		t.Fatal(err)
	}
	if len(indexes) != 0 {
		t.Errorf("indexes = %v, want empty", indexes)
	}
}

func TestAPIErrorFormatting(t *testing.T) {
	t.Run("with sqlstate", func(t *testing.T) {
		err := &snowflakeAPIError{Code: "001003", Msg: "SQL compilation error", SQLState: "42000"}
		got := err.Error()
		if !strings.Contains(got, "001003") || !strings.Contains(got, "42000") || !strings.Contains(got, "SQL compilation error") {
			t.Errorf("error = %q, should contain code, sqlstate, and message", got)
		}
	})

	t.Run("without sqlstate", func(t *testing.T) {
		err := &snowflakeAPIError{Code: "000606", Msg: "No active warehouse"}
		got := err.Error()
		if strings.Contains(got, "SQLState") {
			t.Errorf("error = %q, should not contain SQLState", got)
		}
	})
}

// -- Integration test (skipped unless env var set) ----------------------------

func TestSnowflakeIntegration(t *testing.T) {
	token := os.Getenv("AGENT_SQL_SNOWFLAKE_TEST_TOKEN")
	if token == "" {
		t.Skip("requires Snowflake: set AGENT_SQL_SNOWFLAKE_TEST_TOKEN")
	}

	account := os.Getenv("AGENT_SQL_SNOWFLAKE_TEST_ACCOUNT")
	if account == "" {
		t.Skip("requires AGENT_SQL_SNOWFLAKE_TEST_ACCOUNT")
	}

	conn, err := Connect(Opts{
		Account:   account,
		Token:     token,
		Database:  os.Getenv("AGENT_SQL_SNOWFLAKE_TEST_DATABASE"),
		Schema:    os.Getenv("AGENT_SQL_SNOWFLAKE_TEST_SCHEMA"),
		Warehouse: os.Getenv("AGENT_SQL_SNOWFLAKE_TEST_WAREHOUSE"),
		Readonly:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	ctx := context.Background()

	t.Run("simple query", func(t *testing.T) {
		result, err := conn.Query(ctx, "SELECT 1 AS val", driver.QueryOpts{})
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Rows) != 1 {
			t.Fatalf("rows = %d, want 1", len(result.Rows))
		}
	})

	t.Run("get tables", func(t *testing.T) {
		tables, err := conn.GetTables(ctx, false)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("found %d tables", len(tables))
	})

	t.Run("readonly blocks writes", func(t *testing.T) {
		_, err := conn.Query(ctx, "CREATE TABLE test_should_fail(x INT)", driver.QueryOpts{})
		if err == nil {
			t.Fatal("expected readonly error")
		}
	})
}

func strPtr(s string) *string { return &s }
