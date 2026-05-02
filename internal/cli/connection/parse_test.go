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
			var options map[string]string
			parseGenericURL(tt.connStr, &url, &host, &port, &database, &options)

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
			var options map[string]string
			parseSnowflakeURL(tt.connStr, &url, &account, &database, &schema, &warehouse, &role, &options)

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

func TestParseOptionFlags(t *testing.T) {
	cases := []struct {
		name    string
		in      []string
		want    map[string]string
		wantErr bool
	}{
		{"empty", nil, nil, false},
		{"single", []string{"sslmode=require"}, map[string]string{"sslmode": "require"}, false},
		{"multiple", []string{"a=1", "b=2"}, map[string]string{"a": "1", "b": "2"}, false},
		{"value with equals", []string{"options=-csearch_path=public"}, map[string]string{"options": "-csearch_path=public"}, false},
		{"missing equals errors", []string{"sslmode"}, nil, true},
		{"empty key errors", []string{"=value"}, nil, true},
		{"last duplicate wins", []string{"a=1", "a=2"}, map[string]string{"a": "2"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseOptionFlags(tc.in)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tc.wantErr)
			}
			if !mapsEqual(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestParseGenericURLOptions(t *testing.T) {
	var url, host, port, database string
	var options map[string]string
	parseGenericURL("postgres://h:5432/db?sslmode=require&application_name=foo", &url, &host, &port, &database, &options)
	want := map[string]string{"sslmode": "require", "application_name": "foo"}
	if !mapsEqual(options, want) {
		t.Errorf("options = %v, want %v", options, want)
	}
}

func TestParseSnowflakeURLOptions(t *testing.T) {
	var url, account, database, schema, warehouse, role string
	var options map[string]string
	parseSnowflakeURL("snowflake://acct/DB/PUBLIC?warehouse=WH&role=ANALYST&query_tag=foo&timezone=UTC", &url, &account, &database, &schema, &warehouse, &role, &options)
	if warehouse != "WH" || role != "ANALYST" {
		t.Errorf("warehouse/role not lifted to first-class fields: %s/%s", warehouse, role)
	}
	want := map[string]string{"query_tag": "foo", "timezone": "UTC"}
	if !mapsEqual(options, want) {
		t.Errorf("options = %v, want %v (warehouse/role should not be in options)", options, want)
	}
}

func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func TestStripURLCredentials(t *testing.T) {
	cases := []struct {
		name     string
		in       string
		want     string
		hadCreds bool
		user     string
	}{
		{"pg with user:pass", "postgres://user:secret@host:5432/db", "postgres://host:5432/db", true, "user"},
		{"pg with user only", "postgres://user@host:5432/db", "postgres://host:5432/db", true, "user"},
		{"pg without creds unchanged", "postgres://host:5432/db", "postgres://host:5432/db", false, ""},
		{"mssql with creds", "mssql://sa:p%40ss@sqlhost/proddb", "mssql://sqlhost/proddb", true, "sa"},
		{"snowflake URL has no userinfo", "snowflake://acct/DB?warehouse=WH", "snowflake://acct/DB?warehouse=WH", false, ""},
		{"empty input", "", "", false, ""},
		{"file path is left alone", "/tmp/data.db", "/tmp/data.db", false, ""},
		{"preserves query string", "postgres://u:p@h:5432/db?sslmode=require", "postgres://h:5432/db?sslmode=require", true, "u"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, had, user := stripURLCredentials(tc.in)
			if got != tc.want {
				t.Errorf("cleaned = %q, want %q", got, tc.want)
			}
			if had != tc.hadCreds {
				t.Errorf("hadCreds = %v, want %v", had, tc.hadCreds)
			}
			if user != tc.user {
				t.Errorf("user = %q, want %q", user, tc.user)
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
