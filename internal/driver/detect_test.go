package driver

import (
	"testing"
)

func TestDetectDriverFromURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want Driver
	}{
		{"postgres", "postgres://user:pass@host/db", DriverPG},
		{"postgresql", "postgresql://user:pass@host/db", DriverPG},
		{"cockroachdb", "cockroachdb://user:pass@host/db", DriverCockroachDB},
		{"mysql", "mysql://user:pass@host/db", DriverMySQL},
		{"mariadb", "mariadb://user:pass@host/db", DriverMariaDB},
		{"sqlite", "sqlite:///path/to/db", DriverSQLite},
		{"duckdb", "duckdb:///path/to/db", DriverDuckDB},
		{"snowflake", "snowflake://account/db", DriverSnowflake},
		{"mssql", "mssql://user:pass@host/db", DriverMSSQL},
		{"sqlserver", "sqlserver://user:pass@host/db", DriverMSSQL},

		// File extensions - SQLite
		{"sqlite ext", "data.sqlite", DriverSQLite},
		{"db ext", "data.db", DriverSQLite},
		{"sqlite3 ext", "data.sqlite3", DriverSQLite},
		{"db3 ext", "data.db3", DriverSQLite},

		// File extensions - DuckDB
		{"duckdb ext", "data.duckdb", DriverDuckDB},

		// Case insensitive extensions
		{"DB upper", "data.DB", DriverSQLite},
		{"SQLITE upper", "data.SQLITE", DriverSQLite},
		{"DUCKDB upper", "data.DUCKDB", DriverDuckDB},
		{"mixed case", "data.SqLiTe", DriverSQLite},

		// Unknown
		{"unknown url", "http://example.com", ""},
		{"unknown ext", "data.csv", ""},
		{"empty", "", ""},
		{"plain text", "just-a-name", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetectDriverFromURL(tt.url)
			if got != tt.want {
				t.Errorf("DetectDriverFromURL(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}

func TestIsConnectionURL(t *testing.T) {
	tests := []struct {
		value string
		want  bool
	}{
		{"postgres://host/db", true},
		{"postgresql://host/db", true},
		{"cockroachdb://host/db", true},
		{"mysql://host/db", true},
		{"mariadb://host/db", true},
		{"sqlite:///path", true},
		{"duckdb:///path", true},
		{"snowflake://account/db", true},
		{"mssql://host/db", true},
		{"sqlserver://host/db", true},

		{"http://example.com", false},
		{"data.db", false},
		{"", false},
		{"just-text", false},
	}

	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			got := IsConnectionURL(tt.value)
			if got != tt.want {
				t.Errorf("IsConnectionURL(%q) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

func TestIsFilePathExtensions(t *testing.T) {
	tests := []struct {
		value string
		want  bool
	}{
		{"data.sqlite", true},
		{"data.db", true},
		{"data.sqlite3", true},
		{"data.db3", true},
		{"data.duckdb", true},
		{"DATA.DB", true},
		{"data.DUCKDB", true},

		{"data.csv", false},
		{"data.json", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			got := IsFilePath(tt.value)
			if got != tt.want {
				t.Errorf("IsFilePath(%q) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}
