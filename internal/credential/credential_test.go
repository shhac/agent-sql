package credential

import (
	"os"
	"strings"
	"testing"

	"github.com/shhac/agent-sql/internal/config"
)

// TestStore_Headless_FileFallback exercises the real credential-WRITE path
// non-interactively. Setting the per-CLI keychain opt-out (derived by
// lib-agent-cli from the "app.paulie.agent-sql" service) makes the keychain
// backend report unavailable, so Store deterministically takes the 0600 file
// fallback on every platform — including darwin, where it would otherwise reach
// the `security` CLI and its GUI prompt. This is the connection-credential write
// path that previously could only be tested by chance on non-macOS runners.
func TestStore_Headless_FileFallback(t *testing.T) {
	t.Setenv("AGENT_SQL_NO_KEYCHAIN", "1")
	dir := t.TempDir()
	config.SetConfigDir(dir)
	t.Cleanup(func() { config.SetConfigDir("") })

	cred := Credential{Username: "alice", Password: "s3cr3t", WritePermission: true}
	storage, err := Store("headless-write", cred)
	if err != nil {
		t.Fatalf("Store() error = %v", err)
	}
	if storage != "file" {
		t.Fatalf("storage = %q, want \"file\" (keychain opt-out should force the file path)", storage)
	}

	info, err := os.Stat(credentialsPath())
	if err != nil {
		t.Fatalf("credentials file not written: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("credentials mode = %o, want 0600", mode)
	}

	// File fallback must store the secret in the file itself, not the keychain.
	data, err := os.ReadFile(credentialsPath())
	if err != nil {
		t.Fatalf("ReadFile(credentialsPath) error = %v", err)
	}
	if strings.Contains(string(data), "__KEYCHAIN__") {
		t.Fatalf("credentials index used keychain sentinels despite opt-out: %s", data)
	}

	got := Get("headless-write")
	if got == nil {
		t.Fatal("Get() returned nil after file-fallback Store")
	}
	if got.Username != "alice" || got.Password != "s3cr3t" {
		t.Errorf("round-trip = %q/%q, want alice/s3cr3t", got.Username, got.Password)
	}
	if !got.WritePermission {
		t.Error("WritePermission not round-tripped")
	}

	if err := Remove("headless-write"); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if got := Get("headless-write"); got != nil {
		t.Errorf("credential still present after Remove: %+v", got)
	}
}

func TestStoreAndGetRoundTrip(t *testing.T) {
	config.SetConfigDir(t.TempDir())

	cred := Credential{
		Username:        "alice",
		Password:        "secret123",
		WritePermission: false,
	}

	storage, err := Store("test-cred", cred)
	if err != nil {
		t.Fatalf("Store() error = %v", err)
	}
	// In CI or non-macOS, storage may be "file"; on macOS it may be "keychain"
	if storage != "file" && storage != "keychain" {
		t.Errorf("storage = %q, want file or keychain", storage)
	}

	got := Get("test-cred")
	if got == nil {
		t.Fatal("Get() returned nil")
	}
	if got.Username != "alice" {
		t.Errorf("Username = %q, want alice", got.Username)
	}
	if got.Password != "secret123" {
		t.Errorf("Password = %q, want secret123", got.Password)
	}
	if got.WritePermission != false {
		t.Errorf("WritePermission = %v, want false", got.WritePermission)
	}
}

func TestWritePermissionPreservation(t *testing.T) {
	config.SetConfigDir(t.TempDir())

	cred := Credential{
		Username:        "bob",
		Password:        "pass",
		WritePermission: true,
	}
	if _, err := Store("write-cred", cred); err != nil {
		t.Fatalf("Store() error = %v", err)
	}

	got := Get("write-cred")
	if got == nil {
		t.Fatal("Get() returned nil")
	}
	if !got.WritePermission {
		t.Error("WritePermission should be true, got false")
	}
}

func TestGetNotFound(t *testing.T) {
	config.SetConfigDir(t.TempDir())

	got := Get("nonexistent")
	if got != nil {
		t.Errorf("Get(nonexistent) = %v, want nil", got)
	}
}

func TestRemoveExisting(t *testing.T) {
	config.SetConfigDir(t.TempDir())

	if _, err := Store("to-remove", Credential{Username: "u", Password: "p"}); err != nil {
		t.Fatal(err)
	}

	if err := Remove("to-remove"); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}

	if got := Get("to-remove"); got != nil {
		t.Error("credential should be removed")
	}
}

func TestRemoveNonExisting(t *testing.T) {
	config.SetConfigDir(t.TempDir())

	err := Remove("does-not-exist")
	if err == nil {
		t.Fatal("Remove(nonexistent) should return error")
	}

	nfe, ok := err.(*NotFoundError)
	if !ok {
		t.Fatalf("error type = %T, want *NotFoundError", err)
	}
	if nfe.Name != "does-not-exist" {
		t.Errorf("NotFoundError.Name = %q, want does-not-exist", nfe.Name)
	}
}

func TestList(t *testing.T) {
	config.SetConfigDir(t.TempDir())

	// Empty initially
	names := List()
	if len(names) != 0 {
		t.Errorf("List() = %v, want empty", names)
	}

	Store("cred-a", Credential{Username: "a", Password: "pa"})
	Store("cred-b", Credential{Username: "b", Password: "pb"})

	names = List()
	if len(names) != 2 {
		t.Fatalf("List() len = %d, want 2", len(names))
	}

	found := map[string]bool{}
	for _, n := range names {
		found[n] = true
	}
	if !found["cred-a"] || !found["cred-b"] {
		t.Errorf("List() = %v, want [cred-a cred-b]", names)
	}
}

func TestNotFoundErrorMessage(t *testing.T) {
	err := &NotFoundError{Name: "missing"}
	want := "Credential not found: missing"
	if err.Error() != want {
		t.Errorf("Error() = %q, want %q", err.Error(), want)
	}
}
