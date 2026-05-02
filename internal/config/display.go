package config

import (
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// DisplayURL builds a human-readable connection URL from config fields.
// Never includes credentials -- only the connection target. Render-time only:
// it backfills empty host/port/database from c.URL and applies the per-driver
// default port so the listing reflects what would actually be used at connect
// time. Stored Options are appended as `?key=value&...` (alphabetized) for
// URL-form drivers. Storage is not modified.
func (c Connection) DisplayURL() string {
	base := c.displayBase()
	if c.Driver == "duckdb" {
		// duckdb has no URL form; never append a query string.
		return base
	}
	if q := optionsQueryString(c.Options); q != "" {
		return base + "?" + q
	}
	return base
}

// hostPortDriverInfo holds per-driver display info for host:port-style
// drivers (pg, cockroachdb, mysql, mariadb, mssql). The scheme is what
// appears before :// in display URLs (note pg → "postgres"). DefaultPort
// mirrors the connect-time default applied in resolve.connectFromConfig.
type hostPortDriverInfo struct {
	Scheme      string
	DefaultPort int
}

// hostPortDrivers is the single source of truth for which drivers use
// host:port wire format and what their display scheme + default port are.
// Adding a new host:port driver: add one entry here. Adding a non-host
// driver (file, account, etc.) requires a new arm in displayBase below.
var hostPortDrivers = map[string]hostPortDriverInfo{
	"pg":          {Scheme: "postgres", DefaultPort: 5432},
	"cockroachdb": {Scheme: "cockroachdb", DefaultPort: 26257},
	"mysql":       {Scheme: "mysql", DefaultPort: 3306},
	"mariadb":     {Scheme: "mariadb", DefaultPort: 3306},
	"mssql":       {Scheme: "mssql", DefaultPort: 1433},
}

func (c Connection) displayBase() string {
	if info, ok := hostPortDrivers[c.Driver]; ok {
		host, port, db := effectiveHostPortDB(c, c.Driver)
		return hostPortDBURL(info.Scheme, host, port, db)
	}
	switch c.Driver {
	case "sqlite":
		if c.Path != "" {
			return "sqlite://" + c.Path
		}
		return "sqlite://"
	case "duckdb":
		if c.Path != "" {
			return "duckdb://" + c.Path
		}
		return "duckdb://"
	case "snowflake":
		u := "snowflake://"
		if c.Account != "" {
			u += c.Account
		}
		if c.Database != "" {
			u += "/" + c.Database
		}
		if c.Schema != "" {
			u += "/" + c.Schema
		}
		return u
	default:
		return c.Driver + "://"
	}
}

// optionsQueryString renders an options map as a deterministically-ordered
// `key=value&...` string (URL-encoded). Empty map → "".
func optionsQueryString(opts map[string]string) string {
	if len(opts) == 0 {
		return ""
	}
	keys := make([]string, 0, len(opts))
	for k := range opts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	v := url.Values{}
	for _, k := range keys {
		v.Set(k, opts[k])
	}
	return v.Encode()
}

// defaultPort returns the connect-time default port for a host:port-style
// driver. Reads from the hostPortDrivers registry (single source of truth).
func defaultPort(driver string) int {
	if info, ok := hostPortDrivers[driver]; ok {
		return info.DefaultPort
	}
	return 0
}

// parseURLFallback extracts host/port/database from a URL string, returning
// zero values when the URL is empty or unparseable. Used as a fallback for
// stored connections that have only a URL field populated.
func parseURLFallback(rawURL string) (host string, port int, db string) {
	if rawURL == "" {
		return "", 0, ""
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", 0, ""
	}
	host = u.Hostname()
	if p := u.Port(); p != "" {
		if n, parseErr := strconv.Atoi(p); parseErr == nil {
			port = n
		}
	}
	db = strings.TrimPrefix(u.Path, "/")
	return
}

// effectiveHostPortDB resolves host/port/database for display in three
// composable steps: stored fields → URL fallback → driver default port.
// All in-memory; never written back.
func effectiveHostPortDB(c Connection, driver string) (string, int, string) {
	host, port, db := c.Host, c.Port, c.Database
	if host == "" {
		fbHost, fbPort, fbDB := parseURLFallback(c.URL)
		host = fbHost
		if port == 0 {
			port = fbPort
		}
		if db == "" {
			db = fbDB
		}
	}
	if port == 0 {
		port = defaultPort(driver)
	}
	return host, port, db
}

// EffectiveHost returns the connection's host for display, derived the same
// way as DisplayURL: stored Host, then parsed from URL. For drivers where
// "host" doesn't apply (sqlite, duckdb), returns "". For snowflake, returns
// the account identifier.
func (c Connection) EffectiveHost() string {
	if _, ok := hostPortDrivers[c.Driver]; ok {
		host, _, _ := effectiveHostPortDB(c, c.Driver)
		return host
	}
	if c.Driver == "snowflake" {
		return c.Account
	}
	return ""
}

// EffectivePort returns the connect-time port for host:port drivers: stored
// Port, then parsed from URL, then the per-driver default. Returns 0 for
// drivers without a port (sqlite, duckdb, snowflake).
func (c Connection) EffectivePort() int {
	if _, ok := hostPortDrivers[c.Driver]; ok {
		_, port, _ := effectiveHostPortDB(c, c.Driver)
		return port
	}
	return 0
}

func hostPortDBURL(scheme, host string, port int, database string) string {
	u := scheme + "://"
	if host != "" {
		u += host
	}
	if port != 0 {
		u += ":" + strconv.Itoa(port)
	}
	if database != "" {
		u += "/" + database
	}
	return u
}
