package config

import (
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/shhac/agent-sql/internal/driver"
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

// driverInfo returns the driver registry entry for c.Driver. Returns
// the zero Info if the stored driver string is unknown -- DisplayURL
// degrades gracefully to "<driver>://" rather than panicking.
func (c Connection) driverInfo() driver.Info {
	return driver.Lookup(driver.Driver(c.Driver))
}

func (c Connection) displayBase() string {
	info := c.driverInfo()
	if info.HostPort {
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
// driver. Reads from the driver registry (single source of truth).
func defaultPort(d string) int {
	return driver.Lookup(driver.Driver(d)).DefaultPort
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
	if c.driverInfo().HostPort {
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
	if c.driverInfo().HostPort {
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
