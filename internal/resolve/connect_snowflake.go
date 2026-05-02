package resolve

import (
	"os"

	"github.com/shhac/agent-sql/internal/config"
	"github.com/shhac/agent-sql/internal/credential"
	"github.com/shhac/agent-sql/internal/driver"
	"github.com/shhac/agent-sql/internal/driver/snowflake"
	"github.com/shhac/agent-sql/internal/errors"
)

// connectSnowflakeURL connects from an ad-hoc snowflake:// URL. The PAT
// token comes from AGENT_SQL_SNOWFLAKE_TOKEN -- ad-hoc URLs cannot
// embed the token because Snowflake URLs don't have a userinfo slot
// in our grammar.
func connectSnowflakeURL(connStr string) (driver.Connection, error) {
	token := os.Getenv("AGENT_SQL_SNOWFLAKE_TOKEN")
	if token == "" {
		return nil, errors.New("Ad-hoc Snowflake connections require AGENT_SQL_SNOWFLAKE_TOKEN.", errors.FixableByHuman)
	}
	parsed, err := snowflake.ParseURL(connStr)
	if err != nil {
		return nil, err
	}
	return snowflake.Connect(snowflake.Opts{
		Account: parsed.Account, Database: parsed.Database, Schema: parsed.Schema,
		Warehouse: parsed.Warehouse, Role: parsed.Role,
		Token: token, Readonly: true,
		Options: parsed.Options,
	})
}

// connectSnowflakeConfig connects to a stored snowflake connection. The
// PAT is stored as the credential password.
func connectSnowflakeConfig(conn *config.Connection, cred *credential.Credential, readonly bool) (driver.Connection, error) {
	if err := requirePassword(cred, "Snowflake requires a PAT credential."); err != nil {
		return nil, err
	}
	return snowflake.Connect(snowflake.Opts{
		Account: conn.Account, Database: conn.Database, Schema: conn.Schema,
		Warehouse: conn.Warehouse, Role: conn.Role,
		Token: cred.Password, Readonly: readonly,
		Options: conn.Options,
	})
}
