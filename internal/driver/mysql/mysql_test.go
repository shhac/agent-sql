package mysql

import (
	"context"
	"fmt"
	"os"
	"testing"

	gomysql "github.com/go-sql-driver/mysql"
	"github.com/shhac/agent-sql/internal/errors"
)

func TestQuoteIdent(t *testing.T) {
	conn := &mysqlConn{}

	tests := []struct {
		input string
		want  string
	}{
		{"table", "`table`"},
		{"my`table", "`my``table`"},
		{"simple", "`simple`"},
		{"", "``"},
	}
	for _, tt := range tests {
		if got := conn.QuoteIdent(tt.input); got != tt.want {
			t.Errorf("QuoteIdent(%q) = %s, want %s", tt.input, got, tt.want)
		}
	}
}

func TestClassifyError(t *testing.T) {
	t.Run("read-only violation (errno 1792)", func(t *testing.T) {
		err := classifyError(&gomysql.MySQLError{Number: 1792, Message: "Cannot execute in read only transaction"})
		var qerr *errors.QueryError
		if !errors.As(err, &qerr) {
			t.Fatal("expected QueryError")
		}
		if qerr.FixableBy != errors.FixableByHuman {
			t.Errorf("fixableBy = %s, want human", qerr.FixableBy)
		}
	})

	t.Run("table not found (errno 1146)", func(t *testing.T) {
		err := classifyError(&gomysql.MySQLError{Number: 1146, Message: "Table 'db.foo' doesn't exist"})
		var qerr *errors.QueryError
		if !errors.As(err, &qerr) {
			t.Fatal("expected QueryError")
		}
		if qerr.FixableBy != errors.FixableByAgent {
			t.Errorf("fixableBy = %s, want agent", qerr.FixableBy)
		}
	})

	t.Run("column not found (errno 1054)", func(t *testing.T) {
		err := classifyError(&gomysql.MySQLError{Number: 1054, Message: "Unknown column 'x'"})
		var qerr *errors.QueryError
		if !errors.As(err, &qerr) {
			t.Fatal("expected QueryError")
		}
		if qerr.FixableBy != errors.FixableByAgent {
			t.Errorf("fixableBy = %s, want agent", qerr.FixableBy)
		}
	})

	t.Run("connection failed (errno 2003)", func(t *testing.T) {
		err := classifyError(&gomysql.MySQLError{Number: 2003, Message: "Can't connect to MySQL server"})
		var qerr *errors.QueryError
		if !errors.As(err, &qerr) {
			t.Fatal("expected QueryError")
		}
		if qerr.FixableBy != errors.FixableByHuman {
			t.Errorf("fixableBy = %s, want human", qerr.FixableBy)
		}
	})

	t.Run("auth failed (errno 1045)", func(t *testing.T) {
		err := classifyError(&gomysql.MySQLError{Number: 1045, Message: "Access denied for user"})
		var qerr *errors.QueryError
		if !errors.As(err, &qerr) {
			t.Fatal("expected QueryError")
		}
		if qerr.FixableBy != errors.FixableByHuman {
			t.Errorf("fixableBy = %s, want human", qerr.FixableBy)
		}
	})

	t.Run("connection refused (message-based)", func(t *testing.T) {
		err := classifyError(fmt.Errorf("dial tcp: connection refused"))
		var qerr *errors.QueryError
		if !errors.As(err, &qerr) {
			t.Fatal("expected QueryError")
		}
		if qerr.FixableBy != errors.FixableByHuman {
			t.Errorf("fixableBy = %s, want human", qerr.FixableBy)
		}
	})

	t.Run("unknown error defaults to agent", func(t *testing.T) {
		err := classifyError(fmt.Errorf("something unexpected"))
		var qerr *errors.QueryError
		if !errors.As(err, &qerr) {
			t.Fatal("expected QueryError")
		}
		if qerr.FixableBy != errors.FixableByAgent {
			t.Errorf("fixableBy = %s, want agent", qerr.FixableBy)
		}
	})
}

func TestNormalizeValue(t *testing.T) {
	t.Run("converts bytes to string", func(t *testing.T) {
		got := normalizeValue([]byte("hello"))
		if s, ok := got.(string); !ok || s != "hello" {
			t.Errorf("normalizeValue([]byte) = %v (%T), want string hello", got, got)
		}
	})

	t.Run("passes through other types", func(t *testing.T) {
		got := normalizeValue(int64(42))
		if n, ok := got.(int64); !ok || n != 42 {
			t.Errorf("normalizeValue(int64) = %v (%T), want int64 42", got, got)
		}
	})

	t.Run("passes through nil", func(t *testing.T) {
		got := normalizeValue(nil)
		if got != nil {
			t.Errorf("normalizeValue(nil) = %v, want nil", got)
		}
	})
}

func TestWriteCommands(t *testing.T) {
	for _, cmd := range []string{"INSERT", "UPDATE", "DELETE", "REPLACE", "CREATE", "ALTER", "DROP", "TRUNCATE"} {
		found := false
		for _, wc := range writeCommands {
			if wc == cmd {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("writeCommands missing %s", cmd)
		}
	}
}

// Integration tests — require a real MySQL instance.
// Set AGENT_SQL_MYSQL_TEST_URL to run (e.g. "root:pass@tcp(localhost:3306)/testdb").
func TestIntegration(t *testing.T) {
	dsn := os.Getenv("AGENT_SQL_MYSQL_TEST_URL")
	if dsn == "" {
		t.Skip("requires MySQL — set AGENT_SQL_MYSQL_TEST_URL")
	}

	conn, err := Connect(Opts{
		Host:     "localhost",
		Port:     3306,
		Database: "test",
		Username: "root",
		Password: "",
		Readonly: true,
		Variant:  "mysql",
	})
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer conn.Close()

	ctx := context.Background()

	t.Run("simple query", func(t *testing.T) {
		result, err := conn.Query(ctx, "SELECT 1 AS val", QueryOpts{})
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Columns) != 1 || result.Columns[0] != "val" {
			t.Errorf("columns = %v, want [val]", result.Columns)
		}
	})
}

// QueryOpts alias for driver.QueryOpts
type QueryOpts = struct{ Write bool }

// Ensure imports are used
var _ = os.Getenv
var _ = context.Background
