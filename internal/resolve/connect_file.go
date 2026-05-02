package resolve

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/shhac/agent-sql/internal/driver"
	"github.com/shhac/agent-sql/internal/driver/duckdb"
	"github.com/shhac/agent-sql/internal/driver/sqlite"
	"github.com/shhac/agent-sql/internal/errors"
)

// connectSqliteAdHoc opens a SQLite database from an ad-hoc file path.
// In read mode the file must exist; in write mode it is created on demand.
func connectSqliteAdHoc(_ context.Context, path string, write bool) (driver.Connection, error) {
	absP, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if !write {
		if _, err := os.Stat(absP); os.IsNotExist(err) {
			return nil, errors.New(fmt.Sprintf("SQLite database not found: %s", path), errors.FixableByAgent).
				WithHint("Check the file path, or use --write to create a new database.")
		}
	}
	return sqlite.Connect(sqlite.Opts{Path: absP, Readonly: !write, Create: write})
}

// connectDuckDbAdHoc opens a DuckDB database from an ad-hoc file path,
// or in-memory mode if path is empty.
func connectDuckDbAdHoc(ctx context.Context, path string) (driver.Connection, error) {
	var dbPath string
	if path != "" {
		p, err := filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		dbPath = p
	}
	return duckdb.Connect(ctx, duckdb.Opts{Path: dbPath, Readonly: true})
}
