// Package resolve maps connection aliases, URLs, and file paths to
// concrete driver connections. Separated from the driver package to
// avoid import cycles.
package resolve

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/shhac/agent-sql/internal/config"
	"github.com/shhac/agent-sql/internal/credential"
	"github.com/shhac/agent-sql/internal/driver"
	"github.com/shhac/agent-sql/internal/driver/cockroachdb"
	"github.com/shhac/agent-sql/internal/driver/duckdb"
	"github.com/shhac/agent-sql/internal/driver/mssql"
	"github.com/shhac/agent-sql/internal/driver/mysql"
	"github.com/shhac/agent-sql/internal/driver/pg"
	"github.com/shhac/agent-sql/internal/driver/snowflake"
	"github.com/shhac/agent-sql/internal/driver/sqlite"
	"github.com/shhac/agent-sql/internal/errors"
)

// Opts configures driver resolution.
type Opts struct {
	Connection string
	Write      bool
	Timeout    int
}

// Resolve resolves a connection alias, URL, or file path to a driver.Connection.
func Resolve(ctx context.Context, opts Opts) (driver.Connection, error) {
	alias := resolveAlias(opts.Connection)

	conn, err := resolveAdHoc(ctx, alias, opts.Write)
	if err != nil {
		return nil, err
	}
	if conn != nil {
		return conn, nil
	}

	return resolveFromConfig(ctx, alias, opts.Write)
}

func resolveAlias(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if env := strings.TrimSpace(os.Getenv("AGENT_SQL_CONNECTION")); env != "" {
		return env
	}
	return config.GetDefaultAlias()
}

func resolveAdHoc(ctx context.Context, connStr string, write bool) (driver.Connection, error) {
	if connStr == "" {
		return nil, errors.New(
			fmt.Sprintf("No connection specified and no default configured. Available connections: %s. Tip: -c also accepts file paths (e.g. ./data.db) and connection URLs (e.g. postgres://user:pass@host/db).", listAliases()),
			errors.FixableByAgent,
		)
	}

	if driver.IsConnectionURL(connStr) {
		d := driver.DetectDriverFromURL(connStr)
		if d == "" {
			return nil, nil
		}
		if write {
			return nil, rejectAdHocWrite()
		}
		return connectFromURL(ctx, d, connStr)
	}

	if driver.IsFilePath(connStr) {
		d := driver.DetectDriverFromURL(connStr)
		switch d {
		case driver.DriverDuckDB:
			if write {
				return nil, rejectAdHocWrite()
			}
			return connectDuckDbAdHoc(ctx, connStr)
		case driver.DriverSQLite:
			return connectSqliteAdHoc(ctx, connStr, write)
		default:
			return nil, errors.New(
				fmt.Sprintf("Cannot infer driver for file path %q. Recognized extensions: .db, .sqlite, .sqlite3, .db3 (sqlite); .duckdb (duckdb). Use a connection URL or --driver to disambiguate.", connStr),
				errors.FixableByAgent,
			)
		}
	}

	return nil, nil
}

func resolveFromConfig(ctx context.Context, alias string, write bool) (driver.Connection, error) {
	if alias == "" {
		return nil, errors.New(
			fmt.Sprintf("No connection specified and no default configured. Available connections: %s.", listAliases()),
			errors.FixableByAgent,
		)
	}

	conn := config.GetConnection(alias)
	if conn == nil {
		return nil, errors.NotFound(alias, configAliases())
	}

	d := driver.Driver(conn.Driver)
	if d == "" && conn.URL != "" {
		d = driver.DetectDriverFromURL(conn.URL)
	}
	if d == "" {
		return nil, errors.New("Cannot determine driver type.", errors.FixableByAgent)
	}

	if write {
		cred := credFor(conn)
		if err := checkWritePermission(d, cred, alias); err != nil {
			return nil, err
		}
	}

	return connectFromConfig(ctx, d, conn, !write)
}

func rejectAdHocWrite() *errors.QueryError {
	return errors.New("Write mode is not available for ad-hoc connections.", errors.FixableByHuman)
}

func credFor(conn *config.Connection) *credential.Credential {
	if conn.Credential == "" {
		return nil
	}
	return credential.Get(conn.Credential)
}

var credentialRequired = map[driver.Driver]string{
	driver.DriverPG:          "PostgreSQL",
	driver.DriverCockroachDB: "CockroachDB",
	driver.DriverMySQL:       "MySQL",
	driver.DriverMariaDB:     "MariaDB",
	driver.DriverSnowflake:   "Snowflake",
	driver.DriverMSSQL:       "MSSQL",
}

func checkWritePermission(d driver.Driver, cred *credential.Credential, alias string) error {
	if cred != nil && !cred.WritePermission {
		return errors.New(
			fmt.Sprintf("Write mode requested but credential for connection '%s' has writePermission disabled.", alias),
			errors.FixableByHuman,
		)
	}

	if name, ok := credentialRequired[d]; ok && cred == nil {
		return errors.New(
			fmt.Sprintf("Write mode requested but %s connection '%s' has no credential.", name, alias),
			errors.FixableByHuman,
		)
	}
	return nil
}

func connectFromURL(ctx context.Context, d driver.Driver, connStr string) (driver.Connection, error) {
	switch d {
	case driver.DriverSQLite:
		return connectSqliteAdHoc(ctx, strings.TrimPrefix(connStr, "sqlite://"), false)
	case driver.DriverDuckDB:
		return connectDuckDbAdHoc(ctx, strings.TrimPrefix(connStr, "duckdb://"))
	case driver.DriverPG:
		return pg.ConnectURL(ctx, connStr, true)
	case driver.DriverCockroachDB:
		return cockroachdb.ConnectURL(ctx, connStr, true)
	case driver.DriverMySQL, driver.DriverMariaDB:
		return connectMysqlLikeURL(d, connStr)
	case driver.DriverSnowflake:
		return connectSnowflakeURL(connStr)
	case driver.DriverMSSQL:
		return connectMssqlURL(connStr)
	}
	return nil, errors.New(fmt.Sprintf("Unsupported driver: %s", d), errors.FixableByAgent)
}

func connectFromConfig(ctx context.Context, d driver.Driver, conn *config.Connection, readonly bool) (driver.Connection, error) {
	cred := credFor(conn)
	switch d {
	case driver.DriverSQLite:
		path := resolveFilePath(conn, "sqlite://")
		if path == "" {
			return nil, errors.New("SQLite connection requires a path.", errors.FixableByAgent)
		}
		return sqlite.Connect(sqlite.Opts{Path: path, Readonly: readonly, Options: conn.Options})
	case driver.DriverDuckDB:
		path := resolveFilePath(conn, "duckdb://")
		if path == "" {
			return nil, errors.New("DuckDB connection requires a path.", errors.FixableByAgent)
		}
		return duckdb.Connect(ctx, duckdb.Opts{Path: path, Readonly: readonly, Options: conn.Options})
	case driver.DriverPG, driver.DriverCockroachDB:
		return connectPgLikeConfig(ctx, d, conn, cred, readonly)
	case driver.DriverMySQL, driver.DriverMariaDB:
		return connectMysqlLikeConfig(d, conn, cred, readonly)
	case driver.DriverSnowflake:
		return connectSnowflakeConfig(conn, cred, readonly)
	case driver.DriverMSSQL:
		return connectMssqlConfig(conn, cred, readonly)
	}
	return nil, errors.New(fmt.Sprintf("Unknown driver '%s'.", d), errors.FixableByAgent)
}

func connectSqliteAdHoc(_ context.Context, path string, write bool) (driver.Connection, error) {
	absP, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if !write {
		if _, err := os.Stat(absP); os.IsNotExist(err) {
			return nil, errors.New(fmt.Sprintf("SQLite database not found: %s", path), errors.FixableByAgent).
				WithHint("Check the file path, or use --write to create a new database.")
		}
	}
	return sqlite.Connect(sqlite.Opts{Path: absP, Readonly: !write, Create: write})
}

func connectDuckDbAdHoc(ctx context.Context, path string) (driver.Connection, error) {
	var dbPath string
	if path != "" {
		p, err := filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		dbPath = p
	}
	return duckdb.Connect(ctx, duckdb.Opts{Path: dbPath, Readonly: true})
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

func connectPgLikeConfig(ctx context.Context, d driver.Driver, conn *config.Connection, cred *credential.Credential, readonly bool) (driver.Connection, error) {
	label := "PostgreSQL"
	if d == driver.DriverCockroachDB {
		label = "CockroachDB"
	}
	if err := requireUserPass(cred, label); err != nil {
		return nil, err
	}
	defaultPort, defaultDB := 5432, "postgres"
	if d == driver.DriverCockroachDB {
		defaultPort, defaultDB = 26257, "defaultdb"
	}
	return pg.Connect(ctx, pg.Opts{
		Host: orStr(conn.Host, "localhost"), Port: orInt(conn.Port, defaultPort),
		Database: orStr(conn.Database, defaultDB),
		Username: cred.Username, Password: cred.Password, Readonly: readonly,
		Options: conn.Options,
	})
}

func connectMysqlLikeURL(d driver.Driver, connStr string) (driver.Connection, error) {
	u, err := parseGenericURL(connStr)
	if err != nil {
		return nil, err
	}
	variant := "mysql"
	if d == driver.DriverMariaDB {
		variant = "mariadb"
	}
	return mysql.Connect(mysql.Opts{
		Host: u.Host, Port: parsePort(u.Port, 3306), Database: u.Database,
		Username: u.Username, Password: u.Password, Readonly: true, Variant: variant,
		Options: u.Options,
	})
}

func connectMysqlLikeConfig(d driver.Driver, conn *config.Connection, cred *credential.Credential, readonly bool) (driver.Connection, error) {
	label, variant := "MySQL", "mysql"
	if d == driver.DriverMariaDB {
		label, variant = "MariaDB", "mariadb"
	}
	if err := requireUserPass(cred, label); err != nil {
		return nil, err
	}
	return mysql.Connect(mysql.Opts{
		Host: orStr(conn.Host, "localhost"), Port: orInt(conn.Port, 3306),
		Database: orStr(conn.Database, "mysql"),
		Username: cred.Username, Password: cred.Password,
		Readonly: readonly, Variant: variant,
		Options: conn.Options,
	})
}

func connectSnowflakeURL(connStr string) (driver.Connection, error) {
	token := os.Getenv("AGENT_SQL_SNOWFLAKE_TOKEN")
	if token == "" {
		return nil, errors.New("Ad-hoc Snowflake connections require AGENT_SQL_SNOWFLAKE_TOKEN.", errors.FixableByHuman)
	}
	parsed, err := snowflake.ParseURL(connStr)
	if err != nil {
		return nil, err
	}
	return snowflake.Connect(snowflake.Opts{
		Account: parsed.Account, Database: parsed.Database, Schema: parsed.Schema,
		Warehouse: parsed.Warehouse, Role: parsed.Role,
		Token: token, Readonly: true,
		Options: parsed.Options,
	})
}

func connectSnowflakeConfig(conn *config.Connection, cred *credential.Credential, readonly bool) (driver.Connection, error) {
	if err := requirePassword(cred, "Snowflake requires a PAT credential."); err != nil {
		return nil, err
	}
	return snowflake.Connect(snowflake.Opts{
		Account: conn.Account, Database: conn.Database, Schema: conn.Schema,
		Warehouse: conn.Warehouse, Role: conn.Role,
		Token: cred.Password, Readonly: readonly,
		Options: conn.Options,
	})
}

func connectMssqlURL(connStr string) (driver.Connection, error) {
	u, err := parseGenericURL(connStr)
	if err != nil {
		return nil, err
	}
	return mssql.Connect(mssql.Opts{
		Host: u.Host, Port: parsePort(u.Port, 1433), Database: u.Database,
		Username: u.Username, Password: u.Password, Readonly: true,
		Options: u.Options,
	})
}

func connectMssqlConfig(conn *config.Connection, cred *credential.Credential, readonly bool) (driver.Connection, error) {
	if err := requireUserPass(cred, "MSSQL"); err != nil {
		return nil, err
	}
	return mssql.Connect(mssql.Opts{
		Host: orStr(conn.Host, "localhost"), Port: orInt(conn.Port, 1433),
		Database: conn.Database, Username: cred.Username, Password: cred.Password,
		Readonly: readonly,
		Options:  conn.Options,
	})
}

func listAliases() string {
	a := configAliases()
	if len(a) == 0 {
		return "(none configured)"
	}
	return strings.Join(a, ", ")
}

func configAliases() []string {
	conns := config.GetConnections()
	out := make([]string, 0, len(conns))
	for k := range conns {
		out = append(out, k)
	}
	return out
}

// genericURL is the structured form of a host:port:database connection
// URL (postgres, mysql, mariadb, mssql, sqlserver). Userinfo is preserved
// because ad-hoc URL connections legitimately carry credentials -- only
// stored connections reject them. Options carries any query-string
// parameters for pass-through to the driver.
type genericURL struct {
	Host     string
	Port     string
	Database string
	Username string
	Password string
	Options  map[string]string
}

// parseGenericURL parses a host-style connection URL. Returns a
// FixableByHuman error if the URL is malformed -- the previous
// behavior of silently swallowing parse errors and falling back to
// localhost:default-port was a footgun.
func parseGenericURL(connStr string) (genericURL, error) {
	u, err := url.Parse(connStr)
	if err != nil {
		return genericURL{}, errors.New(
			fmt.Sprintf("Invalid connection URL %q: %v", connStr, err),
			errors.FixableByHuman,
		)
	}
	out := genericURL{
		Host:     u.Hostname(),
		Port:     u.Port(),
		Database: strings.TrimPrefix(u.Path, "/"),
		Username: u.User.Username(),
	}
	if out.Host == "" {
		out.Host = "localhost"
	}
	out.Password, _ = u.User.Password()
	if q := u.Query(); len(q) > 0 {
		out.Options = make(map[string]string, len(q))
		for k, vs := range q {
			if len(vs) > 0 {
				out.Options[k] = vs[0]
			}
		}
	}
	return out, nil
}

func parsePort(s string, def int) int {
	if s == "" {
		return def
	}
	var p int
	_, _ = fmt.Sscanf(s, "%d", &p)
	if p == 0 {
		return def
	}
	return p
}

func resolveFilePath(conn *config.Connection, urlPrefix string) string {
	if conn.Path != "" {
		return conn.Path
	}
	if conn.URL != "" {
		return strings.TrimPrefix(conn.URL, urlPrefix)
	}
	return ""
}

func orStr(val, def string) string {
	if val == "" {
		return def
	}
	return val
}

func orInt(val, def int) int {
	if val == 0 {
		return def
	}
	return val
}
