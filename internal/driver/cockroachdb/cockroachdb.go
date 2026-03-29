// Package cockroachdb implements the CockroachDB driver as a thin wrapper over
// the PostgreSQL driver. CockroachDB uses the PG wire protocol, so the PG driver
// handles all the actual work.
package cockroachdb

import (
	"context"

	"github.com/shhac/agent-sql/internal/driver"
	"github.com/shhac/agent-sql/internal/driver/pg"
)

// DefaultPort is the standard CockroachDB port.
const DefaultPort = 26257

// DefaultDatabase is the default CockroachDB database.
const DefaultDatabase = "defaultdb"

// Opts holds CockroachDB connection options.
type Opts struct {
	Host     string
	Port     int
	Database string
	Username string
	Password string
	Readonly bool
}

// Connect opens a CockroachDB connection via the PG driver.
func Connect(ctx context.Context, opts Opts) (driver.Connection, error) {
	port := opts.Port
	if port == 0 {
		port = DefaultPort
	}
	db := opts.Database
	if db == "" {
		db = DefaultDatabase
	}

	return pg.Connect(ctx, pg.Opts{
		Host:     opts.Host,
		Port:     port,
		Database: db,
		Username: opts.Username,
		Password: opts.Password,
		Readonly: opts.Readonly,
	})
}

// ConnectURL opens a CockroachDB connection from a URL via the PG driver.
func ConnectURL(ctx context.Context, url string, readonly bool) (driver.Connection, error) {
	return pg.ConnectURL(ctx, url, readonly)
}
