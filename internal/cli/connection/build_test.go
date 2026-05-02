package connection

import (
	"strings"
	"testing"
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
