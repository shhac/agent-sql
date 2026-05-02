package mysql

import (
	"fmt"
	"net/url"

	gomysql "github.com/go-sql-driver/mysql"
)

// buildMysqlConfig parses opts.Options through gomysql.ParseDSN (which
// validates each key and types every typed field) and overlays the
// connection-target and safety fields on top. Pass-through for unknowns
// via Config.Params; gives free upgrades whenever gomysql adds new
// options.
func buildMysqlConfig(opts Opts) (*gomysql.Config, error) {
	cfg := gomysql.NewConfig()
	if len(opts.Options) > 0 {
		q := url.Values{}
		for k, v := range opts.Options {
			q.Set(k, v)
		}
		parsed, err := gomysql.ParseDSN("/?" + q.Encode())
		if err != nil {
			return nil, err
		}
		cfg = parsed
	}
	cfg.User = opts.Username
	cfg.Passwd = opts.Password
	cfg.Net = "tcp"
	cfg.Addr = fmt.Sprintf("%s:%d", opts.Host, opts.Port)
	cfg.DBName = opts.Database
	cfg.MultiStatements = false // never allow, regardless of user input
	return cfg, nil
}
