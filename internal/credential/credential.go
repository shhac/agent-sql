// Package credential handles reading and writing stored credentials.
// Credentials live at $XDG_CONFIG_HOME/agent-sql/credentials.json.
// On macOS, sensitive values are stored in the system Keychain.
package credential

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/shhac/agent-sql/internal/config"
)

// Credential represents stored authentication credentials.
type Credential struct {
	Username        string `json:"username,omitempty"`
	Password        string `json:"password,omitempty"`
	WritePermission bool   `json:"writePermission"`
	KeychainManaged bool   `json:"keychainManaged,omitempty"`
}

type credentialIndex struct {
	entries map[string]credentialEntry
}

type credentialEntry struct {
	Username        string `json:"username,omitempty"`
	Password        string `json:"password,omitempty"`
	WritePermission bool   `json:"writePermission"`
	KeychainManaged bool   `json:"keychainManaged,omitempty"`
}

func credentialsPath() string {
	return filepath.Join(config.ConfigDir(), "credentials.json")
}

func readIndex() map[string]credentialEntry {
	data, err := os.ReadFile(credentialsPath())
	if err != nil {
		return make(map[string]credentialEntry)
	}
	var entries map[string]credentialEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return make(map[string]credentialEntry)
	}
	return entries
}

func writeIndex(entries map[string]credentialEntry) error {
	dir := config.ConfigDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(credentialsPath(), append(data, '\n'), 0o600)
}

// Get retrieves a credential by name. Returns nil if not found.
func Get(name string) *Credential {
	entries := readIndex()
	entry, ok := entries[name]
	if !ok {
		return nil
	}

	cred := &Credential{
		Username:        entry.Username,
		Password:        entry.Password,
		WritePermission: entry.WritePermission,
		KeychainManaged: entry.KeychainManaged,
	}

	// Try to read from Keychain
	if entry.KeychainManaged && runtime.GOOS == "darwin" {
		if kcCred := readKeychain(name); kcCred != nil {
			cred.Username = kcCred.Username
			cred.Password = kcCred.Password
		}
	}

	return cred
}

// Store saves a credential. On macOS, sensitive values go to Keychain.
func Store(name string, cred Credential) (storage string, err error) {
	entries := readIndex()

	if runtime.GOOS == "darwin" {
		if err := writeKeychain(name, &cred); err == nil {
			entries[name] = credentialEntry{
				Username:        "__KEYCHAIN__",
				Password:        "__KEYCHAIN__",
				WritePermission: cred.WritePermission,
				KeychainManaged: true,
			}
			return "keychain", writeIndex(entries)
		}
	}

	// File fallback
	entries[name] = credentialEntry{
		Username:        cred.Username,
		Password:        cred.Password,
		WritePermission: cred.WritePermission,
	}
	return "file", writeIndex(entries)
}

// Remove deletes a credential.
func Remove(name string) error {
	entries := readIndex()
	entry, ok := entries[name]
	if !ok {
		return &NotFoundError{Name: name}
	}
	if entry.KeychainManaged && runtime.GOOS == "darwin" {
		deleteKeychain(name)
	}
	delete(entries, name)
	return writeIndex(entries)
}

// List returns all credential names.
func List() []string {
	entries := readIndex()
	names := make([]string, 0, len(entries))
	for k := range entries {
		names = append(names, k)
	}
	return names
}

// NotFoundError is returned when a credential doesn't exist.
type NotFoundError struct {
	Name string
}

func (e *NotFoundError) Error() string {
	return "Credential not found: " + e.Name
}

// Keychain helpers (macOS only)

const keychainService = "agent-sql"

func readKeychain(name string) *Credential {
	out, err := exec.Command("security", "find-generic-password",
		"-s", keychainService, "-a", name, "-w").Output()
	if err != nil {
		return nil
	}
	var cred Credential
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(out))), &cred); err != nil {
		return nil
	}
	return &cred
}

func writeKeychain(name string, cred *Credential) error {
	data, err := json.Marshal(cred)
	if err != nil {
		return err
	}
	// Delete existing entry first (ignore errors)
	exec.Command("security", "delete-generic-password",
		"-s", keychainService, "-a", name).Run()
	return exec.Command("security", "add-generic-password",
		"-s", keychainService, "-a", name, "-w", string(data)).Run()
}

func deleteKeychain(name string) {
	exec.Command("security", "delete-generic-password",
		"-s", keychainService, "-a", name).Run()
}
