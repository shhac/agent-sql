package mockservers

import (
	"context"
	"strings"
	"testing"

	"github.com/shhac/agent-sql/internal/driver"
	"github.com/shhac/agent-sql/internal/driver/snowflake"
)

func TestSnowflakeMockServer(t *testing.T) {
	srv := NewSnowflakeServer()
	defer srv.Close()

	// The mock server URL is like http://127.0.0.1:PORT
	// Snowflake driver expects account.snowflakecomputing.com but we can override the base URL
	// For now, test the mock server directly via HTTP and verify the driver's parsing logic separately

	t.Run("mock server responds to SELECT", func(t *testing.T) {
		conn, err := snowflake.Connect(snowflake.Opts{
			Account:  "test",
			Token:    "test-token",
			Readonly: true,
			BaseURL:  srv.URL(), // Use mock server URL
		})
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()

		result, err := conn.Query(context.Background(), "SELECT * FROM users", driver.QueryOpts{})
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Rows) != 2 {
			t.Errorf("expected 2 rows, got %d", len(result.Rows))
		}
		if result.Rows[0]["ID"] != int64(1) && result.Rows[0]["ID"] != "1" {
			t.Errorf("expected ID=1, got %v (%T)", result.Rows[0]["ID"], result.Rows[0]["ID"])
		}
	})

	t.Run("mock server blocks writes", func(t *testing.T) {
		conn, err := snowflake.Connect(snowflake.Opts{
			Account:  "test",
			Token:    "test-token",
			Readonly: true,
			BaseURL:  srv.URL(),
		})
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()

		_, err = conn.Query(context.Background(), "INSERT INTO users VALUES (1, 'Test')", driver.QueryOpts{})
		if err == nil {
			t.Fatal("expected error for INSERT in readonly mode")
		}
	})

	t.Run("mock server records queries", func(t *testing.T) {
		srv.Queries = nil // reset
		conn, err := snowflake.Connect(snowflake.Opts{
			Account:  "test",
			Token:    "test-token",
			Readonly: true,
			BaseURL:  srv.URL(),
		})
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()

		conn.Query(context.Background(), "SELECT 1", driver.QueryOpts{})
		if len(srv.Queries) == 0 {
			t.Error("expected queries to be recorded")
		}
		found := false
		for _, q := range srv.Queries {
			if strings.Contains(q, "SELECT 1") {
				found = true
			}
		}
		if !found {
			t.Errorf("SELECT 1 not found in recorded queries: %v", srv.Queries)
		}
	})

	t.Run("mock server rejects bad auth", func(t *testing.T) {
		conn, err := snowflake.Connect(snowflake.Opts{
			Account:  "test",
			Token:    "", // no token
			Readonly: true,
			BaseURL:  srv.URL(),
		})
		if err != nil {
			// Connect itself might fail — that's fine
			return
		}
		defer conn.Close()

		_, err = conn.Query(context.Background(), "SELECT 1", driver.QueryOpts{})
		if err == nil {
			t.Error("expected auth error with empty token")
		}
	})

	t.Run("custom handler", func(t *testing.T) {
		srv.CustomHandler = func(sql string) (*SnowflakeResponse, int) {
			if strings.Contains(sql, "custom_table") {
				return &SnowflakeResponse{
					ResultSetMetaData: &ResultSetMetaData{
						NumRows: 1,
						RowType: []RowType{{Name: "X", Type: "fixed"}},
					},
					Data: [][]string{{"42"}},
				}, 200
			}
			return nil, 0 // fall through to default
		}
		defer func() { srv.CustomHandler = nil }()

		conn, err := snowflake.Connect(snowflake.Opts{
			Account: "test", Token: "test-token",
			Readonly: true, BaseURL: srv.URL(),
		})
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()

		result, err := conn.Query(context.Background(), "SELECT * FROM custom_table", driver.QueryOpts{})
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Rows) != 1 {
			t.Errorf("expected 1 row, got %d", len(result.Rows))
		}
	})
}
