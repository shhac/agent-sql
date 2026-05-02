package mssql

import (
	"fmt"
	"net/url"
)

// buildMssqlURL constructs the sqlserver:// URL handed to go-mssqldb.
//
// Collision policy: user options win on conflict, matching the
// pass-through philosophy of every other driver in this codebase. A
// user `--option database=other` overrides opts.Database; this is a
// feature -- it lets a stored connection target a different database
// for specific runs without creating a new alias. A user
// `--option "app name=my-app"` overrides the agent-sql default.
//
// Unknown keys pass through verbatim; go-mssqldb decides which are
// valid at connect time.
func buildMssqlURL(opts Opts) string {
	q := url.Values{}
	if opts.Database != "" {
		q.Set("database", opts.Database)
	}
	q.Set("app name", "agent-sql")
	for k, v := range opts.Options {
		q.Set(k, v) // applied last: user wins on conflict
	}
	u := &url.URL{
		Scheme:   "sqlserver",
		User:     url.UserPassword(opts.Username, opts.Password),
		Host:     fmt.Sprintf("%s:%d", opts.Host, opts.Port),
		RawQuery: q.Encode(),
	}
	return u.String()
}
