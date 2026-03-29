package driver

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type urlPattern struct {
	re     *regexp.Regexp
	driver Driver
}

var urlPatterns = []urlPattern{
	{regexp.MustCompile(`^postgres(ql)?://`), DriverPG},
	{regexp.MustCompile(`^cockroachdb://`), DriverCockroachDB},
	{regexp.MustCompile(`^mysql://`), DriverMySQL},
	{regexp.MustCompile(`^mariadb://`), DriverMariaDB},
	{regexp.MustCompile(`^sqlite://`), DriverSQLite},
	{regexp.MustCompile(`^duckdb://`), DriverDuckDB},
	{regexp.MustCompile(`^snowflake://`), DriverSnowflake},
	{regexp.MustCompile(`^mssql://`), DriverMSSQL},
	{regexp.MustCompile(`^sqlserver://`), DriverMSSQL},
}

var sqliteExtensions = []string{".sqlite", ".db", ".sqlite3", ".db3"}
var duckdbExtensions = []string{".duckdb"}

// DetectDriverFromURL detects the driver type from a URL or file path.
// Returns empty string if unrecognized.
func DetectDriverFromURL(url string) Driver {
	for _, p := range urlPatterns {
		if p.re.MatchString(url) {
			return p.driver
		}
	}

	lower := strings.ToLower(url)
	for _, ext := range sqliteExtensions {
		if strings.HasSuffix(lower, ext) {
			return DriverSQLite
		}
	}
	for _, ext := range duckdbExtensions {
		if strings.HasSuffix(lower, ext) {
			return DriverDuckDB
		}
	}

	return ""
}

// IsConnectionURL returns true if the value looks like a database URL.
func IsConnectionURL(value string) bool {
	for _, p := range urlPatterns {
		if p.re.MatchString(value) {
			return true
		}
	}
	return false
}

// IsFilePath returns true if the value looks like a database file path.
func IsFilePath(value string) bool {
	lower := strings.ToLower(value)
	for _, ext := range sqliteExtensions {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	for _, ext := range duckdbExtensions {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	if strings.HasPrefix(value, "/") || strings.HasPrefix(value, "./") || strings.HasPrefix(value, "../") {
		abs, err := filepath.Abs(value)
		if err != nil {
			return false
		}
		_, err = os.Stat(abs)
		return err == nil
	}
	return false
}
