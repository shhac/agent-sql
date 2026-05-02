package resolve

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/shhac/agent-sql/internal/errors"
)

// genericURL is the structured form of a host:port:database connection
// URL (postgres, mysql, mariadb, mssql, sqlserver). Userinfo is preserved
// because ad-hoc URL connections legitimately carry credentials -- only
// stored connections reject them. Options carries any query-string
// parameters for pass-through to the driver.
type genericURL struct {
	Host     string
	Port     string
	Database string
	Username string
	Password string
	Options  map[string]string
}

// parseGenericURL parses a host-style connection URL. Returns a
// FixableByHuman error if the URL is malformed -- the previous
// behavior of silently swallowing parse errors and falling back to
// localhost:default-port was a footgun.
func parseGenericURL(connStr string) (genericURL, error) {
	u, err := url.Parse(connStr)
	if err != nil {
		return genericURL{}, errors.New(
			fmt.Sprintf("Invalid connection URL %q: %v", connStr, err),
			errors.FixableByHuman,
		)
	}
	out := genericURL{
		Host:     u.Hostname(),
		Port:     u.Port(),
		Database: strings.TrimPrefix(u.Path, "/"),
		Username: u.User.Username(),
	}
	if out.Host == "" {
		out.Host = "localhost"
	}
	out.Password, _ = u.User.Password()
	if q := u.Query(); len(q) > 0 {
		out.Options = make(map[string]string, len(q))
		for k, vs := range q {
			if len(vs) > 0 {
				out.Options[k] = vs[0]
			}
		}
	}
	return out, nil
}

// parsePort returns the integer form of s, or def if s is empty or
// cannot be parsed.
func parsePort(s string, def int) int {
	if s == "" {
		return def
	}
	var p int
	_, _ = fmt.Sscanf(s, "%d", &p)
	if p == 0 {
		return def
	}
	return p
}
