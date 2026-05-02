package sqlite

import (
	"net/url"
)

// buildSqliteDSN renders Opts into a `file:path?mode=...&_pragma=...` DSN.
// User-supplied "mode" is dropped -- read-only enforcement is non-negotiable.
// All other Options keys pass through verbatim; modernc.org/sqlite is
// the source of truth for which PRAGMAs and URI parameters are valid.
func buildSqliteDSN(opts Opts) string {
	q := url.Values{}
	for k, v := range opts.Options {
		if k == "mode" {
			continue
		}
		q.Set(k, v)
	}
	switch {
	case opts.Readonly:
		q.Set("mode", "ro")
	case opts.Create:
		q.Set("mode", "rwc")
	default:
		q.Set("mode", "rw")
	}
	return "file:" + opts.Path + "?" + q.Encode()
}
