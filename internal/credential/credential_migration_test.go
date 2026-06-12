package credential

import (
	"testing"

	"github.com/shhac/agent-sql/internal/config"
)

func TestKeychainMigrationReadsNewServiceFirst(t *testing.T) {
	store := testKeychain(t)
	store[keychainService]["prod"] = &Credential{Username: "new", Password: "new-pass"}
	store[legacyKeychainService]["prod"] = &Credential{Username: "legacy", Password: "legacy-pass"}
	writeTestIndex(t, "prod")

	cred, err := GetWithMigration("prod", true)
	if err != nil {
		t.Fatal(err)
	}
	if cred.Username != "new" || cred.Password != "new-pass" {
		t.Fatalf("credential = %+v, want new service values", cred)
	}
}

func TestKeychainMigrationRequiresExplicitMigrationForLegacyOnly(t *testing.T) {
	store := testKeychain(t)
	store[legacyKeychainService]["prod"] = &Credential{Username: "legacy", Password: "legacy-pass"}
	writeTestIndex(t, "prod")

	_, err := GetWithMigration("prod", true)
	if err == nil {
		t.Fatal("expected migration error")
	}
	if got := err.Error(); got != `credential "prod" was found under old Keychain service "agent-sql" and must be migrated to "app.paulie.agent-sql"` {
		t.Fatalf("error = %q", got)
	}
}

func TestKeychainMigrationNoMigrateFallsBackSilently(t *testing.T) {
	store := testKeychain(t)
	store[legacyKeychainService]["prod"] = &Credential{Username: "legacy", Password: "legacy-pass"}
	writeTestIndex(t, "prod")

	cred, err := GetWithMigration("prod", false)
	if err != nil {
		t.Fatal(err)
	}
	if cred.Username != "legacy" || cred.Password != "legacy-pass" {
		t.Fatalf("credential = %+v, want legacy service values", cred)
	}
}

func TestKeychainMigrationMovesLegacyCredential(t *testing.T) {
	store := testKeychain(t)
	store[legacyKeychainService]["prod"] = &Credential{Username: "legacy", Password: "legacy-pass", WritePermission: true}
	writeTestIndex(t, "prod")

	migrated, err := MigrateLegacyCredentials()
	if err != nil {
		t.Fatal(err)
	}
	if migrated != 1 {
		t.Fatalf("migrated = %d, want 1", migrated)
	}
	got := store[keychainService]["prod"]
	if got == nil || got.Username != "legacy" || got.Password != "legacy-pass" || !got.WritePermission {
		t.Fatalf("new service credential = %+v, want migrated legacy credential", got)
	}
	if _, ok := store[legacyKeychainService]["prod"]; ok {
		t.Fatal("legacy credential was not deleted")
	}
}

func writeTestIndex(t *testing.T, name string) {
	t.Helper()
	entries := map[string]credentialEntry{
		name: {
			Username:        "__KEYCHAIN__",
			Password:        "__KEYCHAIN__",
			KeychainManaged: true,
		},
	}
	if err := writeIndex(entries); err != nil {
		t.Fatal(err)
	}
}

func testKeychain(t *testing.T) map[string]map[string]*Credential {
	t.Helper()
	previousAvailable := keychainAvailable
	t.Cleanup(func() {
		config.SetConfigDir("")
		config.ClearCache()
		keychainAvailable = previousAvailable
		readKeychainForService = platformReadKeychain
		writeKeychainForService = platformWriteKeychain
		deleteKeychainForService = platformDeleteKeychain
		SetMigrationRequired(true)
	})
	config.SetConfigDir(t.TempDir())
	keychainAvailable = func() bool { return true }
	store := map[string]map[string]*Credential{
		keychainService:       {},
		legacyKeychainService: {},
	}
	readKeychainForService = func(service, name string) *Credential {
		cred := store[service][name]
		if cred == nil {
			return nil
		}
		copied := *cred
		return &copied
	}
	writeKeychainForService = func(service, name string, cred *Credential) error {
		if store[service] == nil {
			store[service] = map[string]*Credential{}
		}
		copied := *cred
		store[service][name] = &copied
		return nil
	}
	deleteKeychainForService = func(service, name string) {
		delete(store[service], name)
	}
	return store
}
