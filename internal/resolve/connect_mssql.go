package resolve

import (
	"github.com/shhac/agent-sql/internal/config"
	"github.com/shhac/agent-sql/internal/credential"
	"github.com/shhac/agent-sql/internal/driver"
	"github.com/shhac/agent-sql/internal/driver/mssql"
)

// connectMssqlURL connects from an ad-hoc sqlserver:// or mssql:// URL.
func connectMssqlURL(connStr string) (driver.Connection, error) {
	u, err := parseGenericURL(connStr)
	if err != nil {
		return nil, err
	}
	return mssql.Connect(mssql.Opts{
		Host: u.Host, Port: parsePort(u.Port, driver.Lookup(driver.DriverMSSQL).DefaultPort), Database: u.Database,
		Username: u.Username, Password: u.Password, Readonly: true,
		Options: u.Options,
	})
}

// connectMssqlConfig connects to a stored mssql connection.
func connectMssqlConfig(conn *config.Connection, cred *credential.Credential, readonly bool) (driver.Connection, error) {
	info := driver.Lookup(driver.DriverMSSQL)
	if err := requireUserPass(cred, info.DisplayLabel); err != nil {
		return nil, err
	}
	return mssql.Connect(mssql.Opts{
		Host: orStr(conn.Host, "localhost"), Port: orInt(conn.Port, info.DefaultPort),
		Database: conn.Database, Username: cred.Username, Password: cred.Password,
		Readonly: readonly,
		Options:  conn.Options,
	})
}
