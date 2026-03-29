// Package mariadb provides a thin wrapper over the MySQL driver with variant set to "mariadb".
package mariadb

import (
	"github.com/shhac/agent-sql/internal/driver"
	"github.com/shhac/agent-sql/internal/driver/mysql"
)

// Opts holds MariaDB connection options.
type Opts struct {
	Host     string
	Port     int
	Database string
	Username string
	Password string
	Readonly bool
}

// Connect opens a MariaDB connection via the MySQL driver with Variant "mariadb".
func Connect(opts Opts) (driver.Connection, error) {
	return mysql.Connect(mysql.Opts{
		Host:     opts.Host,
		Port:     opts.Port,
		Database: opts.Database,
		Username: opts.Username,
		Password: opts.Password,
		Readonly: opts.Readonly,
		Variant:  "mariadb",
	})
}
