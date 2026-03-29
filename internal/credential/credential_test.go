package credential

import (
	"testing"

	"github.com/shhac/agent-sql/internal/config"
)

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
