package connection

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/shhac/agent-sql/internal/config"
	"github.com/shhac/agent-sql/internal/credential"
	agenterrors "github.com/shhac/agent-sql/internal/errors"
)

// validateCredentialRef returns nil if credName is empty or refers to an
// existing credential entry. Otherwise it returns an error naming the
// available credentials and the recovery command. Used by both add and
// update to reject typos before persisting the connection.
func validateCredentialRef(credName string) error {
	if credName == "" {
		return nil
	}
	// KEYCHAIN-MIGRATION: Surface legacy-service credentials as a hard setup error.
	if cred, err := credential.GetForRead(credName); err != nil {
		return err
	} else if cred != nil {
		return nil
	}
	names := credential.List()
	listing := "(none)"
	if len(names) > 0 {
		listing = strings.Join(names, ", ")
	}
	return fmt.Errorf(
		"credential %q not found. Available: %s. Run: agent-sql credential add <alias> --username <user> --password <pass>",
		credName, listing,
	)
}

// updateInputs collects everything `connection update` reads from the
// user. Only fields whose name appears in the `changed` set passed to
// buildConnectionUpdates are actually applied to the existing
// connection -- the value carried here is meaningless when the flag
// was not explicitly set.
type updateInputs struct {
	Alias        string
	DriverFlag   string
	Host         string
	Port         string
	Database     string
	Path         string
	URL          string
	CredName     string
	OptionFlags  []string
	ClearOptions bool
}

// buildConnectionUpdates applies a set of updates to existing in
// place. Only fields whose name is in `changed` are applied. Returns
// the list of updated field names (for the receipt's "updated" array),
// any warnings the caller should print, and the first error.
//
// `changed["credential"]` controls the URL-update fallback in
// applyURLUpdate -- when --url is set without --credential, the
// existing credential is reused for the embedded-creds rejection check.
func buildConnectionUpdates(existing *config.Connection, in updateInputs, changed map[string]bool) (updated []string, warnings []string, err error) {
	if changed["driver"] {
		existing.Driver = in.DriverFlag
		updated = append(updated, "driver")
	}
	if changed["host"] {
		existing.Host = in.Host
		updated = append(updated, "host")
	}
	if changed["port"] {
		n, atoiErr := strconv.Atoi(in.Port)
		if atoiErr != nil {
			return nil, nil, fmt.Errorf("invalid port: %s", in.Port)
		}
		existing.Port = n
		updated = append(updated, "port")
	}
	if changed["database"] {
		existing.Database = in.Database
		updated = append(updated, "database")
	}
	if changed["url"] {
		w, urlErr := applyURLUpdate(existing, in.URL, in.Alias, in.CredName, changed["credential"])
		if urlErr != nil {
			return nil, nil, urlErr
		}
		if w != "" {
			warnings = append(warnings, w)
		}
		updated = append(updated, "url")
	}
	if changed["path"] {
		abs, absErr := filepath.Abs(in.Path)
		if absErr != nil {
			return nil, nil, absErr
		}
		existing.Path = abs
		updated = append(updated, "path")
	}
	if changed["credential"] {
		existing.Credential = in.CredName
		updated = append(updated, "credential")
	}
	optsChanged, optsErr := applyOptionUpdates(existing, in.ClearOptions, in.OptionFlags)
	if optsErr != nil {
		return nil, nil, optsErr
	}
	if optsChanged {
		updated = append(updated, "options")
	}
	return updated, warnings, nil
}

// applyOptionUpdates mutates existing.Options based on `--clear-options`
// and repeated `--option k=v` flags. Order is documented in the user-facing
// help: clear runs first, then merges. Returns changed=true if either flag
// produced a change so the caller can append "options" to its updated[]
// once (avoids the duplicate-entry bug when both flags are present).
func applyOptionUpdates(existing *config.Connection, clearOptions bool, optionFlags []string) (changed bool, err error) {
	if clearOptions {
		existing.Options = nil
		changed = true
	}
	if len(optionFlags) > 0 {
		optsFromFlags, parseErr := parseOptionFlags(optionFlags)
		if parseErr != nil {
			return false, parseErr
		}
		if existing.Options == nil {
			existing.Options = make(map[string]string)
		}
		for k, v := range optsFromFlags {
			existing.Options[k] = v
		}
		changed = true
	}
	return changed, nil
}

// applyURLUpdate sets existing.URL to the cleaned form of rawURL,
// rejecting embedded credentials when no effective credential reference
// is available. credChanged should be true iff the caller's --credential
// flag was explicitly set; when false, existing.Credential is used as
// the effective credential for the rejection check. Returns any warning
// the caller should print.
func applyURLUpdate(existing *config.Connection, rawURL, alias, credName string, credChanged bool) (warning string, err error) {
	effectiveCred := credName
	if !credChanged {
		effectiveCred = existing.Credential
	}
	cleanedURL, w, err := rejectEmbeddedCreds(rawURL, alias, effectiveCred, "--url")
	if err != nil {
		return "", err
	}
	existing.URL = cleanedURL
	return w, nil
}

// addInputs collects everything `connection add` reads from the user
// before any side effects. Fields mirror the command's positional and
// flag arguments. The Alias is required; ConnString is the optional
// positional connection string.
type addInputs struct {
	Alias       string
	ConnString  string
	DriverFlag  string
	Host        string
	Port        string
	Database    string
	Path        string
	URL         string
	Account     string
	Warehouse   string
	Role        string
	Schema      string
	CredName    string
	OptionFlags []string
}

// buildConnectionFromAddArgs runs the side-effect-free portion of
// `connection add`: parse the positional connection string (if any),
// merge --option flags (flag wins on conflict), strip embedded
// credentials (or reject if no --credential is available), resolve
// driver, normalize path to absolute, parse port.
//
// Returns the populated config.Connection ready to store, plus any
// warnings the caller should emit on stderr (e.g. "stripped embedded
// credentials").
//
// Errors are FixableByHuman (rejection of embedded creds) or
// FixableByAgent (everything else: bad --option, unresolvable driver,
// invalid port, path resolution).
func buildConnectionFromAddArgs(in addInputs) (config.Connection, []string, error) {
	driverFlag, host, port, database, path, url := in.DriverFlag, in.Host, in.Port, in.Database, in.Path, in.URL
	account, warehouse, role, schema := in.Account, in.Warehouse, in.Role, in.Schema

	var options map[string]string
	if in.ConnString != "" {
		parsed := parseConnectionString(in.ConnString)
		// Explicit flag wins over connection-string parse on conflict.
		if driverFlag == "" {
			driverFlag = parsed.Driver
		}
		if host == "" {
			host = parsed.Host
		}
		if port == "" {
			port = parsed.Port
		}
		if database == "" {
			database = parsed.Database
		}
		if path == "" {
			path = parsed.Path
		}
		if url == "" {
			url = parsed.URL
		}
		if account == "" {
			account = parsed.Account
		}
		if warehouse == "" {
			warehouse = parsed.Warehouse
		}
		if role == "" {
			role = parsed.Role
		}
		if schema == "" {
			schema = parsed.Schema
		}
		options = parsed.Options
	}

	optsFromFlags, err := parseOptionFlags(in.OptionFlags)
	if err != nil {
		return config.Connection{}, nil, err
	}
	for k, v := range optsFromFlags {
		if options == nil {
			options = make(map[string]string)
		}
		options[k] = v
	}

	cleanedURL, warning, err := rejectEmbeddedCreds(url, in.Alias, in.CredName, "connection string")
	if err != nil {
		return config.Connection{}, nil, err
	}
	url = cleanedURL
	var warnings []string
	if warning != "" {
		warnings = append(warnings, warning)
	}

	resolvedDriver := resolveDriver(driverFlag, url, path)
	if resolvedDriver == "" {
		return config.Connection{}, warnings, agenterrors.New(
			"cannot determine driver. Use --driver pg|cockroachdb|sqlite|duckdb|mysql|mariadb|snowflake|mssql, a connection URL, or a file path for SQLite",
			agenterrors.FixableByAgent,
		)
	}

	absPath := path
	if absPath != "" {
		a, err := filepath.Abs(absPath)
		if err != nil {
			return config.Connection{}, warnings, err
		}
		absPath = a
	}

	portNum := 0
	if port != "" {
		n, err := strconv.Atoi(port)
		if err != nil {
			return config.Connection{}, warnings, agenterrors.New(
				fmt.Sprintf("invalid port: %s", port),
				agenterrors.FixableByAgent,
			)
		}
		portNum = n
	}

	return config.Connection{
		Driver:     resolvedDriver,
		Host:       host,
		Port:       portNum,
		Database:   database,
		Path:       absPath,
		URL:        url,
		Credential: in.CredName,
		Account:    account,
		Warehouse:  warehouse,
		Role:       role,
		Schema:     schema,
		Options:    options,
	}, warnings, nil
}
