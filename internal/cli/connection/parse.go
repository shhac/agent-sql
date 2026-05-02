package connection

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/shhac/agent-sql/internal/driver"
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
	// snowflake://account/database/schema?warehouse=WH&role=ROLE&query_tag=...
	trimmed := strings.TrimPrefix(connStr, "snowflake://")
	parts := strings.SplitN(trimmed, "?", 2)
	pathParts := strings.Split(parts[0], "/")
	if len(pathParts) > 0 {
		p.Account = pathParts[0]
	}
	if len(pathParts) > 1 {
		p.Database = pathParts[1]
	}
	if len(pathParts) > 2 {
		p.Schema = pathParts[2]
	}
	if len(parts) > 1 {
		for _, param := range strings.Split(parts[1], "&") {
			kv := strings.SplitN(param, "=", 2)
			if len(kv) != 2 {
				continue
			}
			switch strings.ToLower(kv[0]) {
			case "warehouse":
				p.Warehouse = kv[1]
			case "role":
				p.Role = kv[1]
			default:
				if p.Options == nil {
					p.Options = make(map[string]string)
				}
				p.Options[kv[0]] = kv[1]
			}
		}
	}
	return p
}

func parseGenericURL(connStr string) parsedConnString {
	p := parsedConnString{URL: connStr}
	// Strip scheme: [user:pass@]host[:port]/database
	trimmed := connStr
	for _, prefix := range []string{"postgres://", "postgresql://", "cockroachdb://", "mysql://", "mariadb://", "mssql://", "sqlserver://"} {
		trimmed = strings.TrimPrefix(trimmed, prefix)
	}
	atIdx := strings.LastIndex(trimmed, "@")
	hostPart := trimmed
	if atIdx >= 0 {
		hostPart = trimmed[atIdx+1:]
	}
	slashIdx := strings.Index(hostPart, "/")
	if slashIdx >= 0 {
		dbAndQuery := hostPart[slashIdx+1:]
		var queryStr string
		if qIdx := strings.Index(dbAndQuery, "?"); qIdx >= 0 {
			queryStr = dbAndQuery[qIdx+1:]
			dbAndQuery = dbAndQuery[:qIdx]
		}
		if dbAndQuery != "" {
			p.Database = dbAndQuery
		}
		if queryStr != "" {
			for _, param := range strings.Split(queryStr, "&") {
				kv := strings.SplitN(param, "=", 2)
				if len(kv) != 2 || kv[0] == "" {
					continue
				}
				if p.Options == nil {
					p.Options = make(map[string]string)
				}
				p.Options[kv[0]] = kv[1]
			}
		}
		hostPart = hostPart[:slashIdx]
	}
	colonIdx := strings.LastIndex(hostPart, ":")
	if colonIdx >= 0 {
		p.Host = hostPart[:colonIdx]
		p.Port = hostPart[colonIdx+1:]
	} else if hostPart != "" {
		p.Host = hostPart
	}
	return p
}
