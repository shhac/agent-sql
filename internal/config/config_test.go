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
