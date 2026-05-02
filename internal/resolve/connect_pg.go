package resolve

import (
	"context"

	"github.com/shhac/agent-sql/internal/config"
	"github.com/shhac/agent-sql/internal/credential"
	"github.com/shhac/agent-sql/internal/driver"
	"github.com/shhac/agent-sql/internal/driver/pg"
)

// connectPgLikeConfig connects to a stored pg or cockroachdb connection.
// pgx accepts both as URL form, so the two drivers share one path.
func connectPgLikeConfig(ctx context.Context, d driver.Driver, conn *config.Connection, cred *credential.Credential, readonly bool) (driver.Connection, error) {
	info := driver.Lookup(d)
	if err := requireUserPass(cred, info.DisplayLabel); err != nil {
		return nil, err
	}
	return pg.Connect(ctx, pg.Opts{
		Host: orStr(conn.Host, "localhost"), Port: orInt(conn.Port, info.DefaultPort),
		Database: orStr(conn.Database, info.DefaultDB),
		Username: cred.Username, Password: cred.Password, Readonly: readonly,
		Options: conn.Options,
	})
}
