package resolve

import (
	"testing"

	"github.com/shhac/agent-sql/internal/credential"
	"github.com/shhac/agent-sql/internal/driver"
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

func TestParseURL(t *testing.T) {
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
			host, port, db, user, pass, err := parseURL(tt.url)
			if err != nil {
				t.Fatalf("parseURL() error = %v", err)
			}
			if host != tt.wantHost {
				t.Errorf("host = %q, want %q", host, tt.wantHost)
			}
			if port != tt.wantPort {
				t.Errorf("port = %q, want %q", port, tt.wantPort)
			}
			if db != tt.wantDB {
				t.Errorf("database = %q, want %q", db, tt.wantDB)
			}
			if user != tt.wantUser {
				t.Errorf("user = %q, want %q", user, tt.wantUser)
			}
			if pass != tt.wantPass {
				t.Errorf("password = %q, want %q", pass, tt.wantPass)
			}
		})
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
