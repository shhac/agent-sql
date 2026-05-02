package pg

import (
	"net/url"
	"strconv"
)

// buildPgURL renders an Opts into a postgres:// URL. sslmode defaults to
// "prefer" but is overridden if the caller supplies sslmode in Options.
// All other Options keys pass through verbatim; pgx is the source of
// truth for which keys are valid.
func buildPgURL(opts Opts) string {
	u := &url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(opts.Username, opts.Password),
		Host:   opts.Host + ":" + strconv.Itoa(opts.Port),
		Path:   "/" + opts.Database,
	}
	q := u.Query()
	if _, ok := opts.Options["sslmode"]; !ok {
		q.Set("sslmode", "prefer")
	}
	for k, v := range opts.Options {
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()
	return u.String()
}
