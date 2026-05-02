package connection

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/shhac/agent-sql/internal/driver"
	"github.com/shhac/agent-sql/internal/driver/snowflake"
	agenterrors "github.com/shhac/agent-sql/internal/errors"
)

// parseOptionFlags converts repeated "key=value" CLI flag values into a map.
// Values may legitimately contain '='; only the first '=' separates the key.
// Empty keys, missing '=', and duplicate keys (last wins, with a stable
// final state) all return a clear error.
func parseOptionFlags(flags []string) (map[string]string, error) {
	if len(flags) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(flags))
	for _, raw := range flags {
		eq := strings.IndexByte(raw, '=')
		if eq <= 0 {
			return nil, fmt.Errorf("--option %q must be key=value", raw)
		}
		out[raw[:eq]] = raw[eq+1:]
	}
	return out, nil
}

// rejectEmbeddedCreds is the shared "secrets must not land in plaintext
// config" gate for `connection add` and `connection update --url`.
//
// Returns (cleaned, warning, nil) when:
//   - the URL has no embedded user:pass@ (warning is empty), OR
//   - the URL has embedded creds AND a credential reference is present
//     (effectiveCred != ""), in which case the userinfo is stripped and
//     a warning string is returned for the caller to print.
//
// Returns (raw, "", err) when the URL has embedded creds and no credential
// is available -- the error is FixableByHuman and names the user so the
// caller can show a recovery command.
//
// `field` is included in the error message ("connection string" for add,
// "--url" for update). `effectiveCred` is the credential name that will
// actually be associated with the connection after the operation
// completes -- which for update may come from existing.Credential rather
// than the --credential flag.
func rejectEmbeddedCreds(rawURL, alias, effectiveCred, field string) (cleaned, warning string, err error) {
	cleaned, hadCreds, embeddedUser := stripURLCredentials(rawURL)
	if !hadCreds {
		return cleaned, "", nil
	}
	if effectiveCred == "" {
		return rawURL, "", agenterrors.New(fmt.Sprintf(
			"%s contains embedded credentials (user %q). Config is plaintext on disk -- credentials must live in the OS keychain. "+
				"Run: agent-sql credential add %s --username %s --password <pass>; "+
				"then re-run with --credential %s",
			field, embeddedUser, alias, embeddedUser, alias,
		), agenterrors.FixableByHuman)
	}
	return cleaned, fmt.Sprintf("warning: stripped embedded credentials from %s; using --credential %s\n", field, effectiveCred), nil
}

// stripURLCredentials returns the URL with any embedded user:pass@ removed.
// hadCreds reports whether userinfo was present; user reports the username
// component (if any) so callers can produce a helpful error message. Inputs
// that don't parse as URLs (file paths, malformed strings) are returned
// unchanged with hadCreds=false.
func stripURLCredentials(connStr string) (cleaned string, hadCreds bool, user string) {
	if connStr == "" {
		return "", false, ""
	}
	u, err := url.Parse(connStr)
	if err != nil || u.User == nil {
		return connStr, false, ""
	}
	user = u.User.Username()
	u.User = nil
	return u.String(), true, user
}

func resolveDriver(driverFlag, url, path string) string {
	if driverFlag != "" {
		return driverFlag
	}
	if url != "" {
		detected := driver.DetectDriverFromURL(url)
		if detected != "" {
			return string(detected)
		}
	}
	if path != "" {
		detected := driver.DetectDriverFromURL(path)
		if detected != "" {
			return string(detected)
		}
		return string(driver.DriverSQLite)
	}
	return ""
}

// parsedConnString holds everything extractable from a positional
// connection-string argument. All fields are independent zero-value
// defaults; the caller decides how to merge with explicit flag values
// (typically: explicit flag wins on conflict).
//
// This type is CLI-internal: it exists to feed `connection add`'s
// flag-merge step, and includes fields like Driver and Path that are
// CLI/config concepts rather than runtime URL grammar. The runtime URL
// parser for snowflake (the only driver with non-host:port URL grammar)
// lives in its driver package as `snowflake.ParseURL`. They look
// similar but serve different layers: parsedConnString is for storage
// merging at add time; snowflake.ParseURL is for connect-time URL
// interpretation (used by both `connection add` here and the ad-hoc
// connectSnowflakeURL in resolve.go). The seam is intentional.
type parsedConnString struct {
	Driver    string
	Host      string
	Port      string
	Database  string
	Path      string
	URL       string
	Account   string
	Warehouse string
	Role      string
	Schema    string
	Options   map[string]string
}

func parseConnectionString(connStr string) parsedConnString {
	lower := strings.ToLower(connStr)

	// File-extension shortcuts: .duckdb, .sqlite, .db, .sqlite3, .db3
	for _, ext := range []string{".duckdb", ".sqlite3", ".sqlite", ".db3", ".db"} {
		if strings.HasSuffix(lower, ext) {
			return parsedConnString{Path: connStr}
		}
	}

	// Plain file path with no recognized extension
	if driver.IsFilePath(connStr) {
		return parsedConnString{Path: connStr}
	}

	detected := driver.DetectDriverFromURL(connStr)
	if detected == "" {
		return parsedConnString{}
	}

	switch detected {
	case driver.DriverSQLite:
		return parsedConnString{Path: strings.TrimPrefix(connStr, "sqlite://")}
	case driver.DriverDuckDB:
		return parsedConnString{Path: strings.TrimPrefix(connStr, "duckdb://")}
	case driver.DriverSnowflake:
		return parseSnowflakeURL(connStr)
	default:
		return parseGenericURL(connStr)
	}
}

func parseSnowflakeURL(connStr string) parsedConnString {
	p := parsedConnString{URL: connStr}
	parsed, err := snowflake.ParseURL(connStr)
	if err != nil {
		// Malformed URL: fall back to URL-only storage. The driver will
		// surface the parse error at connect time.
		return p
	}
	p.Account = parsed.Account
	p.Database = parsed.Database
	p.Schema = parsed.Schema
	p.Warehouse = parsed.Warehouse
	p.Role = parsed.Role
	p.Options = parsed.Options
	return p
}

func parseGenericURL(connStr string) parsedConnString {
	p := parsedConnString{URL: connStr}
	u, err := url.Parse(connStr)
	if err != nil {
		// Malformed URL: leave host/port/db/options empty, keep raw URL
		// stored. The driver will surface the parse error at connect time.
		return p
	}
	p.Host = u.Hostname()
	p.Port = u.Port()
	p.Database = strings.TrimPrefix(u.Path, "/")
	if q := u.Query(); len(q) > 0 {
		p.Options = make(map[string]string, len(q))
		for k, vs := range q {
			if len(vs) > 0 {
				p.Options[k] = vs[0]
			}
		}
	}
	return p
}
