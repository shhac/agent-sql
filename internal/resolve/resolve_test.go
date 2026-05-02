package resolve

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/shhac/agent-sql/internal/config"
	"github.com/shhac/agent-sql/internal/credential"
	"github.com/shhac/agent-sql/internal/driver"
	"github.com/shhac/agent-sql/internal/errors"
)

func TestCheckWritePermission(t *testing.T) {
	tests := []struct {
		name    string
		driver  driver.Driver
		cred    *credential.Credential
		alias   string
		wantErr bool
	}{
		{
			name:    "credential with writePermission=false blocks",
			driver:  driver.DriverPG,
			cred:    &credential.Credential{Username: "u", Password: "p", WritePermission: false},
			alias:   "prod",
			wantErr: true,
		},
		{
			name:    "credential with writePermission=true allows",
			driver:  driver.DriverPG,
			cred:    &credential.Credential{Username: "u", Password: "p", WritePermission: true},
			alias:   "prod",
			wantErr: false,
		},
		{
			name:    "no credential blocks for PG",
			driver:  driver.DriverPG,
			cred:    nil,
			alias:   "prod",
			wantErr: true,
		},
		{
			name:    "no credential blocks for MySQL",
			driver:  driver.DriverMySQL,
			cred:    nil,
			alias:   "prod",
			wantErr: true,
		},
		{
			name:    "no credential blocks for MSSQL",
			driver:  driver.DriverMSSQL,
			cred:    nil,
			alias:   "prod",
			wantErr: true,
		},
		{
			name:    "no credential blocks for Snowflake",
			driver:  driver.DriverSnowflake,
			cred:    nil,
			alias:   "prod",
			wantErr: true,
		},
		{
			name:    "no credential blocks for CockroachDB",
			driver:  driver.DriverCockroachDB,
			cred:    nil,
			alias:   "prod",
			wantErr: true,
		},
		{
			name:    "no credential blocks for MariaDB",
			driver:  driver.DriverMariaDB,
			cred:    nil,
			alias:   "prod",
			wantErr: true,
		},
		{
			name:    "no credential allows for SQLite",
			driver:  driver.DriverSQLite,
			cred:    nil,
			alias:   "local",
			wantErr: false,
		},
		{
			name:    "no credential allows for DuckDB",
			driver:  driver.DriverDuckDB,
			cred:    nil,
			alias:   "analytics",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkWritePermission(tt.driver, tt.cred, tt.alias)
			if (err != nil) != tt.wantErr {
				t.Errorf("checkWritePermission() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestResolveAdHocRejectsWrite confirms that ad-hoc URL connections
// can never be opened in write mode -- write requires a stored
// connection with an explicit credential WritePermission flag.
func TestResolveAdHocRejectsWrite(t *testing.T) {
	config.SetConfigDir(t.TempDir())
	_, err := Resolve(context.Background(), Opts{
		Connection: "postgres://u:p@h/d",
		Write:      true,
	})
	if err == nil {
		t.Fatal("expected rejection; ad-hoc URLs cannot opt into write mode")
	}
	var qerr *errors.QueryError
	if !errors.As(err, &qerr) {
		t.Fatalf("expected *QueryError, got %T", err)
	}
	if qerr.FixableBy != errors.FixableByHuman {
		t.Errorf("FixableBy = %s, want human", qerr.FixableBy)
	}
}

// TestResolveSnowflakeURLRequiresEnvToken confirms ad-hoc snowflake://
// URLs require AGENT_SQL_SNOWFLAKE_TOKEN -- the URL itself has no
// userinfo slot for the PAT.
func TestResolveSnowflakeURLRequiresEnvToken(t *testing.T) {
	config.SetConfigDir(t.TempDir())
	t.Setenv("AGENT_SQL_SNOWFLAKE_TOKEN", "") // explicitly clear
	_, err := Resolve(context.Background(), Opts{
		Connection: "snowflake://acct/MYDB",
	})
	if err == nil {
		t.Fatal("expected rejection without AGENT_SQL_SNOWFLAKE_TOKEN")
	}
	if !strings.Contains(err.Error(), "AGENT_SQL_SNOWFLAKE_TOKEN") {
		t.Errorf("err should mention env var; got %v", err)
	}
}

// TestResolveAdHocUnknownFileExt confirms unknown file extensions are
// rejected rather than silently treated as sqlite (the previous bug:
// `-c ./README.md` would try to open the README as a SQLite database).
// IsFilePath requires the file to exist (it stats), so we create a
// real file with no recognized DB extension.
func TestResolveAdHocUnknownFileExt(t *testing.T) {
	dir := t.TempDir()
	config.SetConfigDir(dir)
	weirdFile := dir + "/weird.txt"
	if err := os.WriteFile(weirdFile, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Resolve(context.Background(), Opts{Connection: weirdFile})
	if err == nil {
		t.Fatal("expected rejection of unknown file extension")
	}
	if !strings.Contains(err.Error(), "Recognized extensions") {
		t.Errorf("err should explain extensions; got %v", err)
	}
}

// TestResolveNoConnection covers the error path when no connection is
// specified and no default is configured.
func TestResolveNoConnection(t *testing.T) {
	config.SetConfigDir(t.TempDir())
	t.Setenv("AGENT_SQL_CONNECTION", "")
	_, err := Resolve(context.Background(), Opts{})
	if err == nil {
		t.Fatal("expected error when no connection specified and no default")
	}
}

// TestResolveFilePathPrefersPathOverURL confirms a stored sqlite
// connection that has both Path and URL set uses Path (URL is the
// fallback for older configs).
func TestResolveFilePathPrefersPathOverURL(t *testing.T) {
	cases := []struct {
		name string
		conn config.Connection
		want string
	}{
		{
			"path wins when set",
			config.Connection{Driver: "sqlite", Path: "/explicit", URL: "sqlite:///fallback"},
			"/explicit",
		},
		{
			"url fallback when path empty",
			config.Connection{Driver: "sqlite", URL: "sqlite:///fallback"},
			"/fallback",
		},
		{
			"empty when both empty",
			config.Connection{Driver: "sqlite"},
			"",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveFilePath(&tc.conn, "sqlite://")
			if got != tc.want {
				t.Errorf("resolveFilePath = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestParseGenericURLLocalhostFallback confirms a URL with no host
// (e.g. postgres:///mydb) gets host="localhost" -- this is necessary
// for backward compatibility with libpq-style URLs.
func TestParseGenericURLLocalhostFallback(t *testing.T) {
	u, err := parseGenericURL("postgres:///mydb")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if u.Host != "localhost" {
		t.Errorf("Host = %q, want localhost (fallback)", u.Host)
	}
	if u.Database != "mydb" {
		t.Errorf("Database = %q, want mydb", u.Database)
	}
}

func TestParseGenericURL(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		wantHost string
		wantPort string
		wantDB   string
		wantUser string
		wantPass string
	}{
		{
			name:     "standard postgres URL",
			url:      "postgres://alice:secret@db.example.com:5432/myapp",
			wantHost: "db.example.com",
			wantPort: "5432",
			wantDB:   "myapp",
			wantUser: "alice",
			wantPass: "secret",
		},
		{
			name:     "missing port",
			url:      "postgres://user:pass@localhost/testdb",
			wantHost: "localhost",
			wantPort: "",
			wantDB:   "testdb",
			wantUser: "user",
			wantPass: "pass",
		},
		{
			name:     "missing host defaults to localhost",
			url:      "postgres:///mydb",
			wantHost: "localhost",
			wantPort: "",
			wantDB:   "mydb",
			wantUser: "",
			wantPass: "",
		},
		{
			name:     "URL-encoded password",
			url:      "postgres://user:p%40ss%23word@host:5432/db",
			wantHost: "host",
			wantPort: "5432",
			wantDB:   "db",
			wantUser: "user",
			wantPass: "p@ss#word",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u, err := parseGenericURL(tt.url)
			if err != nil {
				t.Fatalf("parseGenericURL() error = %v", err)
			}
			if u.Host != tt.wantHost {
				t.Errorf("Host = %q, want %q", u.Host, tt.wantHost)
			}
			if u.Port != tt.wantPort {
				t.Errorf("Port = %q, want %q", u.Port, tt.wantPort)
			}
			if u.Database != tt.wantDB {
				t.Errorf("Database = %q, want %q", u.Database, tt.wantDB)
			}
			if u.Username != tt.wantUser {
				t.Errorf("Username = %q, want %q", u.Username, tt.wantUser)
			}
			if u.Password != tt.wantPass {
				t.Errorf("Password = %q, want %q", u.Password, tt.wantPass)
			}
		})
	}
}

// TestParseGenericURLOptions confirms ad-hoc URL query params land in
// Options for pass-through to drivers.
func TestParseGenericURLOptions(t *testing.T) {
	u, err := parseGenericURL("mysql://u:p@h:3306/db?parseTime=true&tls=skip-verify")
	if err != nil {
		t.Fatalf("parseGenericURL() error = %v", err)
	}
	if u.Options["parseTime"] != "true" {
		t.Errorf("Options[parseTime] = %q, want true", u.Options["parseTime"])
	}
	if u.Options["tls"] != "skip-verify" {
		t.Errorf("Options[tls] = %q, want skip-verify", u.Options["tls"])
	}
}

// TestParseGenericURLMalformedReturnsError confirms a malformed URL no
// longer silently dials localhost (was the previous bug).
func TestParseGenericURLMalformedReturnsError(t *testing.T) {
	_, err := parseGenericURL("postgres://[invalid")
	if err == nil {
		t.Fatal("expected error for malformed URL")
	}
}

func TestParsePort(t *testing.T) {
	tests := []struct {
		name string
		s    string
		def  int
		want int
	}{
		{"valid port", "5432", 3306, 5432},
		{"empty string returns default", "", 3306, 3306},
		{"non-numeric returns default", "abc", 5432, 5432},
		{"zero port returns default", "0", 1433, 1433},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parsePort(tt.s, tt.def)
			if got != tt.want {
				t.Errorf("parsePort(%q, %d) = %d, want %d", tt.s, tt.def, got, tt.want)
			}
		})
	}
}

func TestOrStr(t *testing.T) {
	tests := []struct {
		name string
		val  string
		def  string
		want string
	}{
		{"non-empty returns val", "hello", "default", "hello"},
		{"empty returns default", "", "default", "default"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := orStr(tt.val, tt.def)
			if got != tt.want {
				t.Errorf("orStr(%q, %q) = %q, want %q", tt.val, tt.def, got, tt.want)
			}
		})
	}
}

func TestOrInt(t *testing.T) {
	tests := []struct {
		name string
		val  int
		def  int
		want int
	}{
		{"non-zero returns val", 5432, 3306, 5432},
		{"zero returns default", 0, 3306, 3306},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := orInt(tt.val, tt.def)
			if got != tt.want {
				t.Errorf("orInt(%d, %d) = %d, want %d", tt.val, tt.def, got, tt.want)
			}
		})
	}
}
