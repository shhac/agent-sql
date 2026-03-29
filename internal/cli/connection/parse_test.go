package connection

import (
	"testing"
)

func TestParseGenericURL(t *testing.T) {
	tests := []struct {
		name     string
		connStr  string
		wantHost string
		wantPort string
		wantDB   string
	}{
		{
			name:     "standard postgres URL",
			connStr:  "postgres://user:pass@db.example.com:5432/myapp",
			wantHost: "db.example.com",
			wantPort: "5432",
			wantDB:   "myapp",
		},
		{
			name:     "URL with port",
			connStr:  "mysql://user:pass@localhost:3306/testdb",
			wantHost: "localhost",
			wantPort: "3306",
			wantDB:   "testdb",
		},
		{
			name:     "missing port",
			connStr:  "postgres://user:pass@myhost/mydb",
			wantHost: "myhost",
			wantPort: "",
			wantDB:   "mydb",
		},
		{
			name:     "URL-encoded password",
			connStr:  "postgres://user:p%40ss%23word@host:5432/db",
			wantHost: "host",
			wantPort: "5432",
			wantDB:   "db",
		},
		{
			name:     "mssql URL",
			connStr:  "mssql://sa:pass@sqlserver.local:1433/proddb",
			wantHost: "sqlserver.local",
			wantPort: "1433",
			wantDB:   "proddb",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var url, host, port, database string
			parseGenericURL(tt.connStr, &url, &host, &port, &database)

			if url != tt.connStr {
				t.Errorf("url = %q, want %q", url, tt.connStr)
			}
			if host != tt.wantHost {
				t.Errorf("host = %q, want %q", host, tt.wantHost)
			}
			if port != tt.wantPort {
				t.Errorf("port = %q, want %q", port, tt.wantPort)
			}
			if database != tt.wantDB {
				t.Errorf("database = %q, want %q", database, tt.wantDB)
			}
		})
	}
}

func TestParseSnowflakeURL(t *testing.T) {
	tests := []struct {
		name          string
		connStr       string
		wantAccount   string
		wantDB        string
		wantSchema    string
		wantWarehouse string
		wantRole      string
	}{
		{
			name:          "full URL with warehouse and role",
			connStr:       "snowflake://org-acct/MYDB/PUBLIC?warehouse=COMPUTE_WH&role=ANALYST",
			wantAccount:   "org-acct",
			wantDB:        "MYDB",
			wantSchema:    "PUBLIC",
			wantWarehouse: "COMPUTE_WH",
			wantRole:      "ANALYST",
		},
		{
			name:        "minimal URL",
			connStr:     "snowflake://myaccount/DB",
			wantAccount: "myaccount",
			wantDB:      "DB",
			wantSchema:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var url, account, database, schema, warehouse, role string
			parseSnowflakeURL(tt.connStr, &url, &account, &database, &schema, &warehouse, &role)

			if url != tt.connStr {
				t.Errorf("url = %q, want %q", url, tt.connStr)
			}
			if account != tt.wantAccount {
				t.Errorf("account = %q, want %q", account, tt.wantAccount)
			}
			if database != tt.wantDB {
				t.Errorf("database = %q, want %q", database, tt.wantDB)
			}
			if schema != tt.wantSchema {
				t.Errorf("schema = %q, want %q", schema, tt.wantSchema)
			}
			if warehouse != tt.wantWarehouse {
				t.Errorf("warehouse = %q, want %q", warehouse, tt.wantWarehouse)
			}
			if role != tt.wantRole {
				t.Errorf("role = %q, want %q", role, tt.wantRole)
			}
		})
	}
}

func TestResolveDriver(t *testing.T) {
	tests := []struct {
		name       string
		driverFlag string
		url        string
		path       string
		want       string
	}{
		{"from --driver flag", "pg", "", "", "pg"},
		{"from postgres URL", "", "postgres://host/db", "", "pg"},
		{"from mysql URL", "", "mysql://host/db", "", "mysql"},
		{"from mssql URL", "", "mssql://host/db", "", "mssql"},
		{"from .db file extension", "", "", "data.db", "sqlite"},
		{"from .duckdb file extension", "", "", "data.duckdb", "duckdb"},
		{"flag overrides URL", "mysql", "postgres://host/db", "", "mysql"},
		{"unknown path defaults to sqlite", "", "", "myfile.xyz", "sqlite"},
		{"empty everything", "", "", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveDriver(tt.driverFlag, tt.url, tt.path)
			if got != tt.want {
				t.Errorf("resolveDriver(%q, %q, %q) = %q, want %q",
					tt.driverFlag, tt.url, tt.path, got, tt.want)
			}
		})
	}
}
