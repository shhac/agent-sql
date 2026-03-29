package cockroachdb

import (
	"context"
	"os"
	"testing"

	"github.com/shhac/agent-sql/internal/driver"
	"github.com/shhac/agent-sql/internal/errors"
)

func TestDefaults(t *testing.T) {
	if DefaultPort != 26257 {
		t.Errorf("DefaultPort = %d, want 26257", DefaultPort)
	}
	if DefaultDatabase != "defaultdb" {
		t.Errorf("DefaultDatabase = %s, want defaultdb", DefaultDatabase)
	}
}

// Integration tests — require a running CockroachDB instance.
// Set AGENT_SQL_CRDB_TEST_URL to enable (e.g. postgres://root@localhost:26257/defaultdb?sslmode=disable).

func testConn(t *testing.T) driver.Connection {
	t.Helper()
	url := os.Getenv("AGENT_SQL_CRDB_TEST_URL")
	if url == "" {
		t.Skip("requires CockroachDB: set AGENT_SQL_CRDB_TEST_URL")
	}
	ctx := context.Background()
	conn, err := ConnectURL(ctx, url, true)
	if err != nil {
		t.Fatalf("ConnectURL: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func TestIntegrationQuery(t *testing.T) {
	conn := testConn(t)
	ctx := context.Background()

	result, err := conn.Query(ctx, "SELECT 1 AS num", driver.QueryOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Columns) != 1 || result.Columns[0] != "num" {
		t.Errorf("columns = %v, want [num]", result.Columns)
	}
}

func TestIntegrationReadonlyBlocks(t *testing.T) {
	conn := testConn(t)
	ctx := context.Background()

	_, err := conn.Query(ctx, "CREATE TABLE _agent_sql_crdb_test(x INT)", driver.QueryOpts{})
	if err == nil {
		t.Fatal("expected error for CREATE in readonly mode")
	}
	var qerr *errors.QueryError
	if errors.As(err, &qerr) && qerr.FixableBy != errors.FixableByHuman {
		t.Errorf("fixableBy = %s, want human", qerr.FixableBy)
	}
}
