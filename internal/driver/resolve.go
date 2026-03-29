// Package driver contains the resolve logic for mapping connection aliases,
// URLs, and file paths to concrete driver connections.
package driver

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/shhac/agent-sql/internal/config"
	"github.com/shhac/agent-sql/internal/credential"
	"github.com/shhac/agent-sql/internal/errors"
)

// ResolveOpts configures driver resolution.
type ResolveOpts struct {
	Connection string
	Write      bool
	Timeout    int // milliseconds, 0 = use config default
}

// Resolve resolves a connection alias, URL, or file path to a Connection.
func Resolve(ctx context.Context, opts ResolveOpts) (Connection, error) {
	alias := resolveAlias(opts.Connection)

	// Try ad-hoc (URL or file path) first
	conn, err := resolveAdHoc(ctx, alias, opts.Write)
	if err != nil {
		return nil, err
	}
	if conn != nil {
		return conn, nil
	}

	// Look up in config
	return resolveFromConfig(ctx, alias, opts.Write)
}

func resolveAlias(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if env := strings.TrimSpace(os.Getenv("AGENT_SQL_CONNECTION")); env != "" {
		return env
	}
	if def := config.GetDefaultAlias(); def != "" {
		return def
	}

	return ""
}

func configAliases() []string {
	conns := config.GetConnections()
	aliases := make([]string, 0, len(conns))
	for k := range conns {
		aliases = append(aliases, k)
	}
	return aliases
}

func resolveAdHoc(ctx context.Context, connStr string, write bool) (Connection, error) {
	if connStr == "" {
		return nil, errors.New(
			fmt.Sprintf("No connection specified and no default configured. Available connections: %s. Tip: -c also accepts file paths (e.g. ./data.db) and connection URLs (e.g. postgres://user:pass@host/db).",
				listAliases()),
			errors.FixableByAgent,
		)
	}

	// URL check
	if IsConnectionURL(connStr) {
		driver := DetectDriverFromURL(connStr)
		if driver == "" {
			return nil, nil
		}
		if write {
			return nil, errors.New(
				"Write mode is not available for ad-hoc connections. Set up a named connection with a write-enabled credential to use --write.",
				errors.FixableByHuman,
			)
		}
		return connectFromURL(ctx, driver, connStr)
	}

	// File path check
	if IsFilePath(connStr) {
		driver := DetectDriverFromURL(connStr)
		if driver == DriverDuckDB {
			if write {
				return nil, errors.New(
					"Write mode is not available for ad-hoc connections. Set up a named connection with a write-enabled credential to use --write.",
					errors.FixableByHuman,
				)
			}
			return connectDuckDbAdHoc(ctx, connStr)
		}
		// SQLite file
		return connectSqliteAdHoc(ctx, connStr, write)
	}

	return nil, nil
}

func resolveFromConfig(ctx context.Context, alias string, write bool) (Connection, error) {
	if alias == "" {
		return nil, errors.New(
			fmt.Sprintf("No connection specified and no default configured. Available connections: %s.",
				listAliases()),
			errors.FixableByAgent,
		)
	}

	conn := config.GetConnection(alias)
	if conn == nil {
		return nil, errors.NotFound(alias, configAliases())
	}

	driver := Driver(conn.Driver)
	if driver == "" && conn.URL != "" {
		driver = DetectDriverFromURL(conn.URL)
	}
	if driver == "" {
		return nil, errors.New(
			"Cannot determine driver type. Set the 'driver' field on the connection or use a URL with a recognizable scheme.",
			errors.FixableByAgent,
		)
	}

	// Write permission check
	if write {
		cred := getCredentialForConnection(conn)
		if err := checkWritePermission(driver, cred, alias, write); err != nil {
			return nil, err
		}
	}

	return connectFromConfig(ctx, driver, conn, !write)
}

func getCredentialForConnection(conn *config.Connection) *credential.Credential {
	if conn.Credential == "" {
		return nil
	}
	return credential.Get(conn.Credential)
}

func checkWritePermission(driver Driver, cred *credential.Credential, alias string, write bool) error {
	if !write {
		return nil
	}

	if cred != nil && !cred.WritePermission {
		return errors.New(
			fmt.Sprintf("Write mode requested but credential for connection '%s' has writePermission disabled.", alias),
			errors.FixableByHuman,
		).WithHint("Update the credential with --write to enable write permission.")
	}

	// Network drivers require a credential for writes
	needsCred := driver == DriverPG || driver == DriverCockroachDB ||
		driver == DriverMySQL || driver == DriverMariaDB ||
		driver == DriverSnowflake || driver == DriverMSSQL

	if needsCred && cred == nil {
		names := map[Driver]string{
			DriverPG: "PostgreSQL", DriverCockroachDB: "CockroachDB",
			DriverMySQL: "MySQL", DriverMariaDB: "MariaDB",
			DriverSnowflake: "Snowflake", DriverMSSQL: "MSSQL",
		}
		name := names[driver]
		return errors.New(
			fmt.Sprintf("Write mode requested but %s connection '%s' has no credential. %s requires a credential with writePermission to enable writes.", name, alias, name),
			errors.FixableByHuman,
		).WithHint("Add a credential with: agent-sql credential add <name> --username <user> --password <pass> --write")
	}

	return nil
}

func connectFromURL(ctx context.Context, driver Driver, connStr string) (Connection, error) {
	switch driver {
	case DriverSQLite:
		path := strings.TrimPrefix(connStr, "sqlite://")
		return connectSqliteAdHoc(ctx, path, false)

	case DriverDuckDB:
		path := strings.TrimPrefix(connStr, "duckdb://")
		return connectDuckDbAdHoc(ctx, path)

	case DriverPG, DriverCockroachDB:
		return connectPgLikeFromURL(ctx, driver, connStr)

	case DriverMySQL, DriverMariaDB:
		return connectMysqlLikeFromURL(ctx, driver, connStr)

	case DriverSnowflake:
		return connectSnowflakeFromURL(ctx, connStr)

	case DriverMSSQL:
		return connectMssqlFromURL(ctx, connStr)
	}
	return nil, errors.New(fmt.Sprintf("Unsupported driver: %s", driver), errors.FixableByAgent)
}

func connectFromConfig(ctx context.Context, driver Driver, conn *config.Connection, readonly bool) (Connection, error) {
	cred := getCredentialForConnection(conn)

	switch driver {
	case DriverSQLite:
		path := conn.Path
		if path == "" && conn.URL != "" {
			path = strings.TrimPrefix(conn.URL, "sqlite://")
		}
		if path == "" {
			return nil, errors.New(
				fmt.Sprintf("SQLite connection requires a path. Set 'path' on the connection or use a sqlite:// URL."),
				errors.FixableByAgent,
			)
		}
		return connectSqliteFile(ctx, path, readonly)

	case DriverDuckDB:
		path := conn.Path
		if path == "" && conn.URL != "" {
			path = strings.TrimPrefix(conn.URL, "duckdb://")
		}
		if path == "" {
			return nil, errors.New("DuckDB connection requires a path.", errors.FixableByAgent)
		}
		return connectDuckDbFile(ctx, path, readonly)

	case DriverPG, DriverCockroachDB:
		return connectPgLikeFromConfig(ctx, driver, conn, cred, readonly)

	case DriverMySQL, DriverMariaDB:
		return connectMysqlLikeFromConfig(ctx, driver, conn, cred, readonly)

	case DriverSnowflake:
		return connectSnowflakeFromConfig(ctx, conn, cred, readonly)

	case DriverMSSQL:
		return connectMssqlFromConfig(ctx, conn, cred, readonly)
	}

	return nil, errors.New(
		fmt.Sprintf("Unknown driver '%s'. Supported drivers: pg, cockroachdb, sqlite, duckdb, mysql, mariadb, snowflake, mssql.", driver),
		errors.FixableByAgent,
	)
}

func listAliases() string {
	aliases := configAliases()
	if len(aliases) == 0 {
		return "(none configured)"
	}
	return strings.Join(aliases, ", ")
}

// Helper to parse URL into host/port/database/user/password
func parseURL(connStr string) (host string, port string, database string, user string, password string, err error) {
	u, err := url.Parse(connStr)
	if err != nil {
		return "", "", "", "", "", err
	}
	host = u.Hostname()
	if host == "" {
		host = "localhost"
	}
	port = u.Port()
	database = strings.TrimPrefix(u.Path, "/")
	user = u.User.Username()
	password, _ = u.User.Password()
	return
}

// Stub connection functions — these will be populated by each driver package
// via init() or by the resolve layer importing them.
// For now, return "not implemented" errors. The actual implementations will
// be wired in when each driver agent completes.

func connectSqliteFile(ctx context.Context, path string, readonly bool) (Connection, error) {
	// Import cycle prevention: this calls into the sqlite package
	// This will be wired up properly when we have the full CLI
	return nil, fmt.Errorf("SQLite connection not wired up yet")
}

func connectSqliteAdHoc(ctx context.Context, path string, write bool) (Connection, error) {
	return nil, fmt.Errorf("SQLite ad-hoc not wired up yet")
}

func connectDuckDbAdHoc(ctx context.Context, path string) (Connection, error) {
	return nil, fmt.Errorf("DuckDB ad-hoc not wired up yet")
}

func connectDuckDbFile(ctx context.Context, path string, readonly bool) (Connection, error) {
	return nil, fmt.Errorf("DuckDB file not wired up yet")
}

func connectPgLikeFromURL(ctx context.Context, driver Driver, connStr string) (Connection, error) {
	return nil, fmt.Errorf("PG-like URL not wired up yet")
}

func connectPgLikeFromConfig(ctx context.Context, driver Driver, conn *config.Connection, cred *credential.Credential, readonly bool) (Connection, error) {
	return nil, fmt.Errorf("PG-like config not wired up yet")
}

func connectMysqlLikeFromURL(ctx context.Context, driver Driver, connStr string) (Connection, error) {
	return nil, fmt.Errorf("MySQL-like URL not wired up yet")
}

func connectMysqlLikeFromConfig(ctx context.Context, driver Driver, conn *config.Connection, cred *credential.Credential, readonly bool) (Connection, error) {
	return nil, fmt.Errorf("MySQL-like config not wired up yet")
}

func connectSnowflakeFromURL(ctx context.Context, connStr string) (Connection, error) {
	return nil, fmt.Errorf("Snowflake URL not wired up yet")
}

func connectSnowflakeFromConfig(ctx context.Context, conn *config.Connection, cred *credential.Credential, readonly bool) (Connection, error) {
	return nil, fmt.Errorf("Snowflake config not wired up yet")
}

func connectMssqlFromURL(ctx context.Context, connStr string) (Connection, error) {
	return nil, fmt.Errorf("MSSQL URL not wired up yet")
}

func connectMssqlFromConfig(ctx context.Context, conn *config.Connection, cred *credential.Credential, readonly bool) (Connection, error) {
	return nil, fmt.Errorf("MSSQL config not wired up yet")
}

// filepath.Abs wrapper for convenience
var absPath = filepath.Abs
