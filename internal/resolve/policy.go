package resolve

import (
	"fmt"

	"github.com/shhac/agent-sql/internal/config"
	"github.com/shhac/agent-sql/internal/credential"
	"github.com/shhac/agent-sql/internal/driver"
	"github.com/shhac/agent-sql/internal/errors"
)

// rejectAdHocWrite returns the canonical "ad-hoc connections cannot be
// used in write mode" error. Stored connections are the only path that
// can opt into writes, gated by an explicit credential WritePermission.
func rejectAdHocWrite() *errors.QueryError {
	return errors.New("Write mode is not available for ad-hoc connections.", errors.FixableByHuman)
}

// credFor looks up the credential referenced by a stored connection,
// or nil if the connection has no credential reference.
func credFor(conn *config.Connection) (*credential.Credential, error) {
	if conn.Credential == "" {
		return nil, nil
	}
	// KEYCHAIN-MIGRATION: Surface legacy-service credentials as a hard setup error.
	return credential.GetForRead(conn.Credential)
}

// checkWritePermission gates write-mode access. The credential must
// exist (for drivers that authenticate) and have WritePermission set.
func checkWritePermission(d driver.Driver, cred *credential.Credential, alias string) error {
	if cred != nil && !cred.WritePermission {
		return errors.New(
			fmt.Sprintf("Write mode requested but credential for connection '%s' has writePermission disabled.", alias),
			errors.FixableByHuman,
		)
	}

	info := driver.Lookup(d)
	if info.Credential != driver.CredentialNone && cred == nil {
		return errors.New(
			fmt.Sprintf("Write mode requested but %s connection '%s' has no credential.", info.DisplayLabel, alias),
			errors.FixableByHuman,
		)
	}
	return nil
}

// requireUserPass returns a FixableByHuman error if cred is missing
// either username or password. Used by host:port drivers that auth via
// user/pass (pg, cockroachdb, mysql, mariadb, mssql).
func requireUserPass(cred *credential.Credential, label string) error {
	if cred == nil || cred.Username == "" || cred.Password == "" {
		return errors.New(label+" requires a credential.", errors.FixableByHuman)
	}
	return nil
}

// requirePassword returns a FixableByHuman error if cred has no
// password component. Used by token-only drivers (snowflake PAT). The
// caller supplies the full message because token wording varies.
func requirePassword(cred *credential.Credential, message string) error {
	if cred == nil || cred.Password == "" {
		return errors.New(message, errors.FixableByHuman)
	}
	return nil
}
