package credential

import (
	"fmt"

	agenterrors "github.com/shhac/agent-sql/internal/errors"
)

const migrateCommand = "agent-sql credential --migrate"

var requireKeychainMigration = true

// MigrationRequiredError reports that a credential can only be read from the
// legacy Keychain service name until the user explicitly runs the migration command.
type MigrationRequiredError struct {
	Name string
}

func (e *MigrationRequiredError) Error() string {
	return fmt.Sprintf("credential %q was found under old Keychain service %q and must be migrated to %q", e.Name, legacyKeychainService, keychainService)
}

func (e *MigrationRequiredError) Hint() string {
	return fmt.Sprintf("Run '%s' to migrate stored credentials.", migrateCommand)
}

// SetMigrationRequired controls whether legacy-service credentials should fail
// reads with a migration-required error or be used silently.
func SetMigrationRequired(required bool) {
	requireKeychainMigration = required
}

// GetForRead retrieves a credential using the current process migration policy.
func GetForRead(name string) (*Credential, error) {
	return GetWithMigration(name, requireKeychainMigration)
}

// GetWithMigration reads the current service first, then handles legacy-service
// credentials according to requireMigration.
func GetWithMigration(name string, requireMigration bool) (*Credential, error) {
	entries := readIndex()
	entry, ok := entries[name]
	if !ok {
		return nil, nil
	}

	cred := &Credential{
		Username:        entry.Username,
		Password:        entry.Password,
		WritePermission: entry.WritePermission,
		KeychainManaged: entry.KeychainManaged,
	}
	if !entry.KeychainManaged || !keychainAvailable() {
		return cred, nil
	}

	if kcCred := readKeychainForService(keychainService, name); kcCred != nil {
		return hydrateKeychainCredential(kcCred), nil
	}
	if kcCred := readKeychainForService(legacyKeychainService, name); kcCred != nil {
		if requireMigration {
			return nil, migrationRequiredError(name)
		}
		return hydrateKeychainCredential(kcCred), nil
	}
	return cred, nil
}

// MigrateLegacyCredentials copies legacy-service credentials for every indexed
// Keychain-managed credential to the current service and deletes migrated legacy entries.
func MigrateLegacyCredentials() (int, error) {
	if !keychainAvailable() {
		return 0, nil
	}
	entries := readIndex()
	migrated := 0
	for name, entry := range entries {
		if !entry.KeychainManaged {
			continue
		}
		if readKeychainForService(keychainService, name) != nil {
			continue
		}
		cred := readKeychainForService(legacyKeychainService, name)
		if cred == nil {
			continue
		}
		if err := writeKeychainForService(keychainService, name, cred); err != nil {
			return migrated, err
		}
		deleteKeychainForService(legacyKeychainService, name)
		migrated++
	}
	return migrated, nil
}

func hydrateKeychainCredential(cred *Credential) *Credential {
	cred.KeychainManaged = true
	return cred
}

func migrationRequiredError(name string) error {
	err := &MigrationRequiredError{Name: name}
	return agenterrors.New(err.Error(), agenterrors.FixableByHuman).WithHint(err.Hint())
}
