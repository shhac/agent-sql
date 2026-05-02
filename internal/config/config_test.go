package config

import (
	"os"
	"path/filepath"
	"testing"
)

func setup(t *testing.T) {
	t.Helper()
	SetConfigDir(t.TempDir())
}

func TestReadReturnsDefaultWhenFileDoesNotExist(t *testing.T) {
	setup(t)
	cfg := Read()

	if cfg == nil {
		t.Fatal("Read() returned nil")
	}
	if len(cfg.Connections) != 0 {
		t.Errorf("expected 0 connections, got %d", len(cfg.Connections))
	}
	if cfg.DefaultConnection != "" {
		t.Errorf("expected empty default, got %q", cfg.DefaultConnection)
	}
}

func TestWriteThenReadRoundTrips(t *testing.T) {
	setup(t)

	original := &Config{
		DefaultConnection: "mydb",
		Connections: map[string]Connection{
			"mydb": {Driver: "pg", Host: "localhost", Port: 5432, Database: "test"},
		},
		Settings: Settings{},
	}
	if err := Write(original); err != nil {
		t.Fatalf("Write() error: %v", err)
	}

	ClearCache()
	cfg := Read()

	if cfg.DefaultConnection != "mydb" {
		t.Errorf("default = %q, want %q", cfg.DefaultConnection, "mydb")
	}
	conn, ok := cfg.Connections["mydb"]
	if !ok {
		t.Fatal("connection 'mydb' not found")
	}
	if conn.Driver != "pg" {
		t.Errorf("driver = %q, want %q", conn.Driver, "pg")
	}
	if conn.Host != "localhost" {
		t.Errorf("host = %q, want %q", conn.Host, "localhost")
	}
	if conn.Port != 5432 {
		t.Errorf("port = %d, want %d", conn.Port, 5432)
	}
}

func TestStoreConnectionFirstBecomesDefault(t *testing.T) {
	setup(t)

	err := StoreConnection("first", Connection{Driver: "sqlite", Path: "/tmp/a.db"})
	if err != nil {
		t.Fatalf("StoreConnection() error: %v", err)
	}

	cfg := Read()
	if cfg.DefaultConnection != "first" {
		t.Errorf("default = %q, want %q", cfg.DefaultConnection, "first")
	}
}

func TestStoreConnectionSubsequentDoesNotChangeDefault(t *testing.T) {
	setup(t)

	StoreConnection("first", Connection{Driver: "sqlite"})
	StoreConnection("second", Connection{Driver: "pg"})

	cfg := Read()
	if cfg.DefaultConnection != "first" {
		t.Errorf("default = %q, want %q", cfg.DefaultConnection, "first")
	}
}

func TestRemoveConnectionReassignsDefault(t *testing.T) {
	setup(t)

	StoreConnection("alpha", Connection{Driver: "pg"})
	StoreConnection("beta", Connection{Driver: "mysql"})

	if err := RemoveConnection("alpha"); err != nil {
		t.Fatalf("RemoveConnection() error: %v", err)
	}

	cfg := Read()
	if _, ok := cfg.Connections["alpha"]; ok {
		t.Error("connection 'alpha' should have been removed")
	}
	// Default should be reassigned to the remaining connection
	if cfg.DefaultConnection == "alpha" {
		t.Error("default should not still be 'alpha'")
	}
	if cfg.DefaultConnection == "" && len(cfg.Connections) > 0 {
		t.Error("default should be reassigned when connections remain")
	}
}

func TestRemoveConnectionErrorForNonExistent(t *testing.T) {
	setup(t)

	err := RemoveConnection("ghost")
	if err == nil {
		t.Fatal("expected error for non-existent alias")
	}
	cnfErr, ok := err.(*ConnectionNotFoundError)
	if !ok {
		t.Fatalf("expected ConnectionNotFoundError, got %T", err)
	}
	if cnfErr.Alias != "ghost" {
		t.Errorf("alias = %q, want %q", cnfErr.Alias, "ghost")
	}
}

func TestSetDefaultWorksForExistingAlias(t *testing.T) {
	setup(t)

	StoreConnection("a", Connection{Driver: "pg"})
	StoreConnection("b", Connection{Driver: "mysql"})

	if err := SetDefault("b"); err != nil {
		t.Fatalf("SetDefault() error: %v", err)
	}

	cfg := Read()
	if cfg.DefaultConnection != "b" {
		t.Errorf("default = %q, want %q", cfg.DefaultConnection, "b")
	}
}

func TestSetDefaultErrorForNonExistentAlias(t *testing.T) {
	setup(t)

	err := SetDefault("nope")
	if err == nil {
		t.Fatal("expected error for non-existent alias")
	}
	if _, ok := err.(*ConnectionNotFoundError); !ok {
		t.Fatalf("expected ConnectionNotFoundError, got %T", err)
	}
}

func TestGetSettingAndUpdateSettingWithDottedKeys(t *testing.T) {
	setup(t)

	if err := UpdateSetting("query.timeout", float64(60)); err != nil {
		t.Fatalf("UpdateSetting() error: %v", err)
	}

	val := GetSetting("query.timeout")
	if val != float64(60) {
		t.Errorf("GetSetting(query.timeout) = %v, want 60", val)
	}

	// Nested key that doesn't exist
	val = GetSetting("query.nonexistent")
	if val != nil {
		t.Errorf("expected nil for missing key, got %v", val)
	}

	// Top-level missing
	val = GetSetting("missing.deep.path")
	if val != nil {
		t.Errorf("expected nil for missing deep path, got %v", val)
	}
}

func TestResetSettingsClearsAll(t *testing.T) {
	setup(t)

	UpdateSetting("query.timeout", float64(30))
	UpdateSetting("truncation.maxLength", float64(100))

	if err := ResetSettings(); err != nil {
		t.Fatalf("ResetSettings() error: %v", err)
	}

	val := GetSetting("query.timeout")
	if val != nil {
		t.Errorf("expected nil after reset, got %v", val)
	}
}

func TestSetConfigDirIsolatesTests(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()

	SetConfigDir(dir1)
	StoreConnection("one", Connection{Driver: "pg"})

	SetConfigDir(dir2)
	cfg := Read()
	if len(cfg.Connections) != 0 {
		t.Errorf("expected 0 connections in dir2, got %d", len(cfg.Connections))
	}

	SetConfigDir(dir1)
	cfg = Read()
	if len(cfg.Connections) != 1 {
		t.Errorf("expected 1 connection in dir1, got %d", len(cfg.Connections))
	}
}

func TestDisplayURLAppliesDefaultPort(t *testing.T) {
	cases := []struct {
		name string
		conn Connection
		want string
	}{
		{
			name: "pg with no port falls back to 5432",
			conn: Connection{Driver: "pg", Host: "db.example.com", Database: "myapp"},
			want: "postgres://db.example.com:5432/myapp",
		},
		{
			name: "pg with explicit port wins over default",
			conn: Connection{Driver: "pg", Host: "db.example.com", Port: 6543, Database: "myapp"},
			want: "postgres://db.example.com:6543/myapp",
		},
		{
			name: "cockroachdb default port 26257",
			conn: Connection{Driver: "cockroachdb", Host: "crdb.example.com", Database: "defaultdb"},
			want: "cockroachdb://crdb.example.com:26257/defaultdb",
		},
		{
			name: "mysql default port 3306",
			conn: Connection{Driver: "mysql", Host: "mysql.example.com", Database: "app"},
			want: "mysql://mysql.example.com:3306/app",
		},
		{
			name: "mariadb default port 3306",
			conn: Connection{Driver: "mariadb", Host: "maria.example.com", Database: "app"},
			want: "mariadb://maria.example.com:3306/app",
		},
		{
			name: "mssql default port 1433",
			conn: Connection{Driver: "mssql", Host: "sql.example.com", Database: "rep"},
			want: "mssql://sql.example.com:1433/rep",
		},
		{
			name: "host backfilled from URL when not stored",
			conn: Connection{Driver: "pg", URL: "postgres://parsed.example.com:7000/parsedb"},
			want: "postgres://parsed.example.com:7000/parsedb",
		},
		{
			name: "host backfilled from URL, default port applied when URL omits port",
			conn: Connection{Driver: "pg", URL: "postgres://parsed.example.com/parsedb"},
			want: "postgres://parsed.example.com:5432/parsedb",
		},
		{
			name: "snowflake builds from account, no port logic",
			conn: Connection{Driver: "snowflake", Account: "org-acct", Database: "DB", Schema: "PUBLIC"},
			want: "snowflake://org-acct/DB/PUBLIC",
		},
		{
			name: "sqlite path",
			conn: Connection{Driver: "sqlite", Path: "/tmp/x.db"},
			want: "sqlite:///tmp/x.db",
		},
		{
			name: "IPv6 host literal preserved",
			conn: Connection{Driver: "pg", Host: "::1", Port: 5432, Database: "d"},
			want: "postgres://::1:5432/d",
		},
		{
			name: "IPv6 from URL backfill",
			conn: Connection{Driver: "pg", URL: "postgres://[::1]:6543/d"},
			want: "postgres://::1:6543/d",
		},
		{
			name: "malformed URL falls back to base without panicking",
			conn: Connection{Driver: "pg", URL: "postgres://[invalid"},
			want: "postgres://:5432",
		},
		{
			name: "stored URL with embedded user:pass leaks no creds",
			conn: Connection{Driver: "pg", URL: "postgres://leakuser:leaksecret@h.example.com:5432/d"},
			want: "postgres://h.example.com:5432/d",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.conn.DisplayURL(); got != tc.want {
				t.Errorf("DisplayURL() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestEffectiveHost(t *testing.T) {
	cases := []struct {
		name string
		conn Connection
		want string
	}{
		{"pg stored host", Connection{Driver: "pg", Host: "db.example.com"}, "db.example.com"},
		{"pg backfilled from URL", Connection{Driver: "pg", URL: "postgres://parsed.example.com/db"}, "parsed.example.com"},
		{"snowflake returns account", Connection{Driver: "snowflake", Account: "org-acct"}, "org-acct"},
		{"sqlite has no host", Connection{Driver: "sqlite", Path: "/tmp/x.db"}, ""},
		{"duckdb has no host", Connection{Driver: "duckdb", Path: "/tmp/x.duckdb"}, ""},
		{"mssql stored host", Connection{Driver: "mssql", Host: "sql.example.com"}, "sql.example.com"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.conn.EffectiveHost(); got != tc.want {
				t.Errorf("EffectiveHost() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestOptionsRoundTrip(t *testing.T) {
	setup(t)

	original := &Config{
		Connections: map[string]Connection{
			"prod": {
				Driver:   "pg",
				Host:     "h",
				Database: "d",
				Options:  map[string]string{"sslmode": "require", "application_name": "agent-sql"},
			},
		},
		Settings: Settings{},
	}
	if err := Write(original); err != nil {
		t.Fatalf("Write() error: %v", err)
	}
	ClearCache()
	cfg := Read()
	got := cfg.Connections["prod"].Options
	if got["sslmode"] != "require" || got["application_name"] != "agent-sql" {
		t.Errorf("options round-trip lost data: %v", got)
	}
}

func TestDisplayURLAppendsOptions(t *testing.T) {
	cases := []struct {
		name string
		conn Connection
		want string
	}{
		{
			"pg with options alphabetized",
			Connection{Driver: "pg", Host: "h", Database: "d", Options: map[string]string{"sslmode": "require", "application_name": "foo"}},
			"postgres://h:5432/d?application_name=foo&sslmode=require",
		},
		{
			"empty options omits query string",
			Connection{Driver: "pg", Host: "h", Database: "d"},
			"postgres://h:5432/d",
		},
		{
			"duckdb never appends query string",
			Connection{Driver: "duckdb", Path: "/tmp/x.duckdb", Options: map[string]string{"memory_limit": "4GB"}},
			"duckdb:///tmp/x.duckdb",
		},
		{
			"snowflake with options",
			Connection{Driver: "snowflake", Account: "acct", Database: "DB", Options: map[string]string{"query_tag": "agent-sql"}},
			"snowflake://acct/DB?query_tag=agent-sql",
		},
		{
			"sqlite with PRAGMAs",
			Connection{Driver: "sqlite", Path: "/tmp/x.db", Options: map[string]string{"_journal_mode": "wal"}},
			"sqlite:///tmp/x.db?_journal_mode=wal",
		},
		{
			"value with reserved char gets URL-encoded",
			Connection{Driver: "pg", Host: "h", Database: "d", Options: map[string]string{"options": "-csearch_path=public"}},
			"postgres://h:5432/d?options=-csearch_path%3Dpublic",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.conn.DisplayURL(); got != tc.want {
				t.Errorf("DisplayURL() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestEffectivePort(t *testing.T) {
	cases := []struct {
		name string
		conn Connection
		want int
	}{
		{"pg stored port wins", Connection{Driver: "pg", Host: "h", Port: 6543}, 6543},
		{"pg default 5432", Connection{Driver: "pg", Host: "h"}, 5432},
		{"pg port from URL", Connection{Driver: "pg", URL: "postgres://h:7000/d"}, 7000},
		{"pg URL no port falls back to default", Connection{Driver: "pg", URL: "postgres://h/d"}, 5432},
		{"cockroachdb default 26257", Connection{Driver: "cockroachdb", Host: "h"}, 26257},
		{"mysql default 3306", Connection{Driver: "mysql", Host: "h"}, 3306},
		{"mariadb default 3306", Connection{Driver: "mariadb", Host: "h"}, 3306},
		{"mssql default 1433", Connection{Driver: "mssql", Host: "h"}, 1433},
		{"sqlite has no port", Connection{Driver: "sqlite", Path: "/tmp/x.db"}, 0},
		{"duckdb has no port", Connection{Driver: "duckdb"}, 0},
		{"snowflake has no port", Connection{Driver: "snowflake", Account: "a"}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.conn.EffectivePort(); got != tc.want {
				t.Errorf("EffectivePort() = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestCorruptJSONReturnsDefaultConfig(t *testing.T) {
	dir := t.TempDir()
	SetConfigDir(dir)

	// Write corrupt JSON
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "config.json"), []byte("{invalid json!!!"), 0o644)

	ClearCache()
	cfg := Read()
	if cfg == nil {
		t.Fatal("Read() returned nil for corrupt JSON")
	}
	if len(cfg.Connections) != 0 {
		t.Errorf("expected 0 connections for corrupt JSON, got %d", len(cfg.Connections))
	}
}
