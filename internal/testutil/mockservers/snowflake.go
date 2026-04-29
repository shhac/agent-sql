// Package mockservers provides mock database servers for testing.
// These can be reused across driver tests and integration tests.
package mockservers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
)

// SnowflakeServer is a mock Snowflake SQL REST API v2 server.
type SnowflakeServer struct {
	Server *httptest.Server
	mu     sync.Mutex
	// Queries records all SQL statements received.
	Queries []string
	// CustomHandler allows overriding the default behavior per test.
	CustomHandler func(sql string) (*SnowflakeResponse, int)
}

// SnowflakeResponse represents a Snowflake SQL API response.
type SnowflakeResponse struct {
	ResultSetMetaData *ResultSetMetaData `json:"resultSetMetaData,omitempty"`
	Data              [][]string         `json:"data,omitempty"`
	Code              string             `json:"code,omitempty"`
	Message           string             `json:"message,omitempty"`
	SQLState          string             `json:"sqlState,omitempty"`
}

// ResultSetMetaData describes the result set columns.
type ResultSetMetaData struct {
	NumRows int       `json:"numRows"`
	RowType []RowType `json:"rowType"`
}

// RowType describes a single column.
type RowType struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Nullable bool   `json:"nullable"`
}

// NewSnowflakeServer creates a mock Snowflake server with default table data.
func NewSnowflakeServer() *SnowflakeServer {
	s := &SnowflakeServer{}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v2/statements", s.handleStatements)
	s.Server = httptest.NewServer(mux)
	return s
}

func (s *SnowflakeServer) handleStatements(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Check auth
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(SnowflakeResponse{
			Code:    "390100",
			Message: "Authentication failed",
		})
		return
	}

	var body struct {
		Statement string `json:"statement"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	s.Queries = append(s.Queries, body.Statement)
	handler := s.CustomHandler
	s.mu.Unlock()

	sql := strings.TrimSpace(body.Statement)

	// Custom handler takes priority
	if handler != nil {
		resp, status := handler(sql)
		if resp != nil {
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
	}

	// Default responses based on SQL
	resp := s.defaultResponse(sql)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *SnowflakeServer) defaultResponse(sql string) *SnowflakeResponse {
	upper := strings.ToUpper(strings.TrimSpace(sql))

	// SHOW TABLES
	if strings.HasPrefix(upper, "SHOW TABLES") || strings.HasPrefix(upper, "SELECT") && strings.Contains(upper, "INFORMATION_SCHEMA.TABLES") {
		return &SnowflakeResponse{
			ResultSetMetaData: &ResultSetMetaData{
				NumRows: 2,
				RowType: []RowType{
					{Name: "TABLE_NAME", Type: "text"},
					{Name: "TABLE_TYPE", Type: "text"},
				},
			},
			Data: [][]string{
				{"USERS", "BASE TABLE"},
				{"ORDERS", "BASE TABLE"},
			},
		}
	}

	// DESCRIBE TABLE
	if strings.HasPrefix(upper, "DESCRIBE TABLE") || strings.HasPrefix(upper, "SELECT") && strings.Contains(upper, "INFORMATION_SCHEMA.COLUMNS") {
		return &SnowflakeResponse{
			ResultSetMetaData: &ResultSetMetaData{
				NumRows: 3,
				RowType: []RowType{
					{Name: "COLUMN_NAME", Type: "text"},
					{Name: "DATA_TYPE", Type: "text"},
					{Name: "IS_NULLABLE", Type: "text"},
					{Name: "COLUMN_DEFAULT", Type: "text"},
				},
			},
			Data: [][]string{
				{"ID", "NUMBER", "NO", ""},
				{"NAME", "VARCHAR", "NO", ""},
				{"EMAIL", "VARCHAR", "YES", ""},
			},
		}
	}

	// SELECT 1
	if upper == "SELECT 1" {
		return &SnowflakeResponse{
			ResultSetMetaData: &ResultSetMetaData{
				NumRows: 1,
				RowType: []RowType{{Name: "1", Type: "fixed"}},
			},
			Data: [][]string{{"1"}},
		}
	}

	// Default SELECT
	if strings.HasPrefix(upper, "SELECT") || strings.HasPrefix(upper, "WITH") {
		return &SnowflakeResponse{
			ResultSetMetaData: &ResultSetMetaData{
				NumRows: 2,
				RowType: []RowType{
					{Name: "ID", Type: "fixed"},
					{Name: "NAME", Type: "text"},
				},
			},
			Data: [][]string{
				{"1", "Alice"},
				{"2", "Bob"},
			},
		}
	}

	// Write attempt
	return &SnowflakeResponse{
		Code:    "002003",
		Message: fmt.Sprintf("Statement not allowed: %s", strings.SplitN(upper, " ", 2)[0]),
	}
}

// Close shuts down the mock server.
func (s *SnowflakeServer) Close() {
	s.Server.Close()
}

// URL returns the base URL of the mock server.
func (s *SnowflakeServer) URL() string {
	return s.Server.URL
}
