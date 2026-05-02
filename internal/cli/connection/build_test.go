package connection

import (
	"strings"
	"testing"

	"github.com/shhac/agent-sql/internal/config"
)

func TestBuildConnectionFromAddArgsURLParsing(t *testing.T) {
	conn, warnings, err := buildConnectionFromAddArgs(addInputs{
		Alias:      "x",
		ConnString: "postgres://h.example.com:5432/db?sslmode=require",
		CredName:   "cred",
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("no warnings expected, got %v", warnings)
	}
	if conn.Driver != "pg" {
		t.Errorf("Driver = %q, want pg", conn.Driver)
	}
	if conn.Host != "h.example.com" || conn.Port != 5432 || conn.Database != "db" {
		t.Errorf("h/p/db = %q/%d/%q", conn.Host, conn.Port, conn.Database)
	}
	if conn.Options["sslmode"] != "require" {
		t.Errorf("sslmode option lost: %v", conn.Options)
	}
}

func TestBuildConnectionFromAddArgsFlagsBeatConnString(t *testing.T) {
	// Explicit --host overrides the host parsed from the connection string.
	conn, _, err := buildConnectionFromAddArgs(addInputs{
		Alias:      "x",
		ConnString: "postgres://from.url/db",
		Host:       "from.flag",
		CredName:   "cred",
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if conn.Host != "from.flag" {
		t.Errorf("Host = %q, want from.flag (explicit flag wins)", conn.Host)
	}
}

func TestBuildConnectionFromAddArgsOptionFlagBeatsURLOption(t *testing.T) {
	conn, _, err := buildConnectionFromAddArgs(addInputs{
		Alias:       "x",
		ConnString:  "postgres://h/d?sslmode=require",
		OptionFlags: []string{"sslmode=verify-full"},
		CredName:    "cred",
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if conn.Options["sslmode"] != "verify-full" {
		t.Errorf("--option should win over URL ?sslmode= ; got %v", conn.Options)
	}
}

func TestBuildConnectionFromAddArgsRejectsEmbeddedCreds(t *testing.T) {
	_, _, err := buildConnectionFromAddArgs(addInputs{
		Alias:      "x",
		ConnString: "postgres://u:secret@h/d",
	})
	if err == nil {
		t.Fatal("expected rejection")
	}
	if !strings.Contains(err.Error(), "embedded credentials") {
		t.Errorf("err = %v, want embedded credentials message", err)
	}
}

func TestBuildConnectionFromAddArgsStripsCredsWithCredentialFlag(t *testing.T) {
	conn, warnings, err := buildConnectionFromAddArgs(addInputs{
		Alias:      "x",
		ConnString: "postgres://leakuser:leaksecret@h/d",
		CredName:   "cred",
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if strings.Contains(conn.URL, "leakuser") || strings.Contains(conn.URL, "leaksecret") {
		t.Errorf("URL still contains creds: %q", conn.URL)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "stripped") {
		t.Errorf("expected one stripped-creds warning, got %v", warnings)
	}
}

func TestBuildConnectionFromAddArgsInvalidPort(t *testing.T) {
	_, _, err := buildConnectionFromAddArgs(addInputs{
		Alias:      "x",
		DriverFlag: "pg",
		Port:       "not-a-number",
		Host:       "h",
		Database:   "d",
		CredName:   "cred",
	})
	if err == nil || !strings.Contains(err.Error(), "invalid port") {
		t.Errorf("err = %v, want invalid port", err)
	}
}

func TestBuildConnectionUpdatesAppliesOnlyChangedFields(t *testing.T) {
	existing := &config.Connection{
		Driver: "pg", Host: "old.host", Port: 5432, Database: "old-db",
	}
	in := updateInputs{
		Alias: "x",
		Host:  "new.host",
		// Port/Database not set; should not change because not in `changed`.
	}
	changed := map[string]bool{"host": true}

	updated, warnings, err := buildConnectionUpdates(existing, in, changed)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("expected no warnings, got %v", warnings)
	}
	if existing.Host != "new.host" {
		t.Errorf("Host = %q, want new.host", existing.Host)
	}
	if existing.Port != 5432 || existing.Database != "old-db" {
		t.Errorf("untouched fields mutated: %+v", existing)
	}
	if len(updated) != 1 || updated[0] != "host" {
		t.Errorf("updated = %v, want [host]", updated)
	}
}

func TestBuildConnectionUpdatesInvalidPort(t *testing.T) {
	existing := &config.Connection{Driver: "pg"}
	_, _, err := buildConnectionUpdates(existing, updateInputs{Alias: "x", Port: "not-a-number"}, map[string]bool{"port": true})
	if err == nil || !strings.Contains(err.Error(), "invalid port") {
		t.Errorf("err = %v, want invalid port", err)
	}
}

func TestBuildConnectionUpdatesClearOptionsThenSet(t *testing.T) {
	existing := &config.Connection{
		Driver:  "pg",
		Options: map[string]string{"old_key": "x"},
	}
	in := updateInputs{
		Alias:        "x",
		ClearOptions: true,
		OptionFlags:  []string{"new_key=v"},
	}
	updated, _, err := buildConnectionUpdates(existing, in, map[string]bool{})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if existing.Options["old_key"] != "" {
		t.Errorf("clear-options should remove old_key; got %v", existing.Options)
	}
	if existing.Options["new_key"] != "v" {
		t.Errorf("--option should add new_key after clear; got %v", existing.Options)
	}
	if len(updated) != 1 || updated[0] != "options" {
		t.Errorf("updated = %v, want [options] (only one entry even with clear+set)", updated)
	}
}

// TestOptionsURLBridge pins the seam between add-time URL parsing
// (cli/connection's buildConnectionFromAddArgs) and connect-time DSN
// building (each driver's TestBuildXxxURL/Config). Together they
// guarantee that `?sslmode=require` on the command line ends up in
// the driver's URL/DSN under the same key. A rename in either layer
// breaks this test.
func TestOptionsURLBridge(t *testing.T) {
	cases := []struct {
		name      string
		url       string
		wantKey   string
		wantValue string
	}{
		{"pg sslmode", "postgres://h:5432/d?sslmode=require", "sslmode", "require"},
		{"pg application_name", "postgres://h:5432/d?application_name=agent-sql", "application_name", "agent-sql"},
		{"mysql parseTime", "mysql://h:3306/d?parseTime=true", "parseTime", "true"},
		{"mysql tls", "mysql://h:3306/d?tls=skip-verify", "tls", "skip-verify"},
		{"mssql encrypt", "mssql://h:1433/d?encrypt=true", "encrypt", "true"},
		// sqlite/duckdb file paths don't carry options in their URL
		// form -- options come via --option flags. Tested separately.
		{"snowflake query_tag", "snowflake://acct/DB?query_tag=foo", "query_tag", "foo"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			conn, _, err := buildConnectionFromAddArgs(addInputs{
				Alias:      "x",
				ConnString: tc.url,
				CredName:   "cred",
			})
			if err != nil {
				t.Fatalf("err: %v", err)
			}
			if conn.Options[tc.wantKey] != tc.wantValue {
				t.Errorf("Options[%q] = %q, want %q (URL = %q, all opts = %v)",
					tc.wantKey, conn.Options[tc.wantKey], tc.wantValue, tc.url, conn.Options)
			}
		})
	}
}

func TestBuildConnectionFromAddArgsBadOptionFlag(t *testing.T) {
	_, _, err := buildConnectionFromAddArgs(addInputs{
		Alias:       "x",
		OptionFlags: []string{"missing-equals"},
		Host:        "h",
		CredName:    "cred",
	})
	if err == nil {
		t.Fatal("expected error for malformed --option flag")
	}
}
