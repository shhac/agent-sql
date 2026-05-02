package resolve

import (
	"github.com/shhac/agent-sql/internal/config"
	"github.com/shhac/agent-sql/internal/credential"
	"github.com/shhac/agent-sql/internal/driver"
	"github.com/shhac/agent-sql/internal/driver/mysql"
)

// connectMysqlLikeURL connects from an ad-hoc URL for mysql or mariadb.
// The two drivers share the gomysql DSN; only the variant string differs.
func connectMysqlLikeURL(d driver.Driver, connStr string) (driver.Connection, error) {
	u, err := parseGenericURL(connStr)
	if err != nil {
		return nil, err
	}
	variant := "mysql"
	if d == driver.DriverMariaDB {
		variant = "mariadb"
	}
	return mysql.Connect(mysql.Opts{
		Host: u.Host, Port: parsePort(u.Port, driver.Lookup(d).DefaultPort), Database: u.Database,
		Username: u.Username, Password: u.Password, Readonly: true, Variant: variant,
		Options: u.Options,
	})
}

// connectMysqlLikeConfig connects to a stored mysql or mariadb connection.
func connectMysqlLikeConfig(d driver.Driver, conn *config.Connection, cred *credential.Credential, readonly bool) (driver.Connection, error) {
	info := driver.Lookup(d)
	variant := "mysql"
	if d == driver.DriverMariaDB {
		variant = "mariadb"
	}
	if err := requireUserPass(cred, info.DisplayLabel); err != nil {
		return nil, err
	}
	return mysql.Connect(mysql.Opts{
		Host: orStr(conn.Host, "localhost"), Port: orInt(conn.Port, info.DefaultPort),
		Database: orStr(conn.Database, info.DefaultDB),
		Username: cred.Username, Password: cred.Password,
		Readonly: readonly, Variant: variant,
		Options: conn.Options,
	})
}
