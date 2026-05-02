package connection

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/shhac/agent-sql/internal/driver"
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

func parseConnectionString(connStr string, driverFlag, host, port, database, path, url, account, warehouse, role, schema *string, options *map[string]string) {
	lower := strings.ToLower(connStr)

	// DuckDB file extensions
	if strings.HasSuffix(lower, ".duckdb") {
		*path = connStr
		return
	}

	// SQLite file extensions
	for _, ext := range []string{".sqlite", ".db", ".sqlite3", ".db3"} {
		if strings.HasSuffix(lower, ext) {
			*path = connStr
			return
		}
	}

	// File path
	if driver.IsFilePath(connStr) {
		*path = connStr
		return
	}

	detected := driver.DetectDriverFromURL(connStr)
	if detected == "" {
		return
	}

	switch detected {
	case driver.DriverSQLite:
		*path = strings.TrimPrefix(connStr, "sqlite://")
	case driver.DriverDuckDB:
		*path = strings.TrimPrefix(connStr, "duckdb://")
	case driver.DriverSnowflake:
		parseSnowflakeURL(connStr, url, account, database, schema, warehouse, role, options)
	default:
		parseGenericURL(connStr, url, host, port, database, options)
	}
}

func parseSnowflakeURL(connStr string, urlOut, account, database, schema, warehouse, role *string, options *map[string]string) {
	*urlOut = connStr
	// snowflake://account/database/schema?warehouse=WH&role=ROLE&query_tag=...
	trimmed := strings.TrimPrefix(connStr, "snowflake://")
	parts := strings.SplitN(trimmed, "?", 2)
	pathParts := strings.Split(parts[0], "/")
	if *account == "" && len(pathParts) > 0 {
		*account = pathParts[0]
	}
	if *database == "" && len(pathParts) > 1 {
		*database = pathParts[1]
	}
	if *schema == "" && len(pathParts) > 2 {
		*schema = pathParts[2]
	}
	if len(parts) > 1 {
		for _, param := range strings.Split(parts[1], "&") {
			kv := strings.SplitN(param, "=", 2)
			if len(kv) != 2 {
				continue
			}
			switch strings.ToLower(kv[0]) {
			case "warehouse":
				if *warehouse == "" {
					*warehouse = kv[1]
				}
			case "role":
				if *role == "" {
					*role = kv[1]
				}
			default:
				if options != nil {
					if *options == nil {
						*options = make(map[string]string)
					}
					if _, exists := (*options)[kv[0]]; !exists {
						(*options)[kv[0]] = kv[1]
					}
				}
			}
		}
	}
}

func parseGenericURL(connStr string, urlOut, host, port, database *string, options *map[string]string) {
	*urlOut = connStr
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
		if *database == "" && dbAndQuery != "" {
			*database = dbAndQuery
		}
		if queryStr != "" && options != nil {
			for _, param := range strings.Split(queryStr, "&") {
				kv := strings.SplitN(param, "=", 2)
				if len(kv) != 2 || kv[0] == "" {
					continue
				}
				if *options == nil {
					*options = make(map[string]string)
				}
				if _, exists := (*options)[kv[0]]; !exists {
					(*options)[kv[0]] = kv[1]
				}
			}
		}
		hostPart = hostPart[:slashIdx]
	}
	colonIdx := strings.LastIndex(hostPart, ":")
	if colonIdx >= 0 {
		if *host == "" {
			*host = hostPart[:colonIdx]
		}
		if *port == "" {
			*port = hostPart[colonIdx+1:]
		}
	} else {
		if *host == "" && hostPart != "" {
			*host = hostPart
		}
	}
}
