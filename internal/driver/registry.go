package driver

// CredentialKind classifies what authentication shape a driver expects.
type CredentialKind string

const (
	// CredentialNone means the driver does not authenticate
	// (file-backed drivers: sqlite, duckdb).
	CredentialNone CredentialKind = "none"
	// CredentialUserPass requires both username and password.
	CredentialUserPass CredentialKind = "userpass"
	// CredentialToken requires only a password (used as a bearer token /
	// PAT). Currently snowflake.
	CredentialToken CredentialKind = "token"
)

// Info is the canonical metadata for a single driver. Centralizing this
// here means adding a driver is a single Registry entry plus the
// driver-package implementation; downstream consumers (display
// rendering, resolve dispatch, credential validation) read from this
// registry rather than maintaining parallel switches.
type Info struct {
	// Driver is the canonical short name ("pg", "mysql", ...).
	Driver Driver

	// DisplayLabel is the human-readable name used in error messages
	// ("PostgreSQL", "CockroachDB", "MSSQL").
	DisplayLabel string

	// Scheme is the prefix used when building a display URL for this
	// driver (e.g. "postgres" for pg, "sqlserver" — actually "mssql" --
	// for go-mssqldb-style URLs).
	Scheme string

	// DefaultPort is the connect-time default for host:port drivers.
	// Zero for non-host:port drivers (sqlite, duckdb, snowflake).
	DefaultPort int

	// DefaultDB is the database name used when none is configured.
	// Empty when the driver requires the user to specify one (mssql)
	// or when the concept doesn't apply (sqlite, duckdb).
	DefaultDB string

	// HostPort is true for drivers that connect to host:port (pg,
	// cockroachdb, mysql, mariadb, mssql) and false for the rest.
	// Drives display rendering: HostPort drivers get
	// "scheme://host:port/database" URLs.
	HostPort bool

	// Credential describes the authentication shape required for
	// stored connections of this driver.
	Credential CredentialKind
}

// Registry is the single source of truth for per-driver metadata.
// Adding a driver: add one entry here plus the driver-package
// implementation. All other places that switch on driver name
// (config/display, resolve, error messages) read from this map.
var Registry = map[Driver]Info{
	DriverPG: {
		Driver: DriverPG, DisplayLabel: "PostgreSQL",
		Scheme: "postgres", DefaultPort: 5432, DefaultDB: "postgres",
		HostPort: true, Credential: CredentialUserPass,
	},
	DriverCockroachDB: {
		Driver: DriverCockroachDB, DisplayLabel: "CockroachDB",
		Scheme: "cockroachdb", DefaultPort: 26257, DefaultDB: "defaultdb",
		HostPort: true, Credential: CredentialUserPass,
	},
	DriverMySQL: {
		Driver: DriverMySQL, DisplayLabel: "MySQL",
		Scheme: "mysql", DefaultPort: 3306, DefaultDB: "mysql",
		HostPort: true, Credential: CredentialUserPass,
	},
	DriverMariaDB: {
		Driver: DriverMariaDB, DisplayLabel: "MariaDB",
		Scheme: "mariadb", DefaultPort: 3306, DefaultDB: "mysql",
		HostPort: true, Credential: CredentialUserPass,
	},
	DriverMSSQL: {
		Driver: DriverMSSQL, DisplayLabel: "MSSQL",
		Scheme: "mssql", DefaultPort: 1433,
		HostPort: true, Credential: CredentialUserPass,
	},
	DriverSQLite: {
		Driver: DriverSQLite, DisplayLabel: "SQLite",
		Scheme: "sqlite", Credential: CredentialNone,
	},
	DriverDuckDB: {
		Driver: DriverDuckDB, DisplayLabel: "DuckDB",
		Scheme: "duckdb", Credential: CredentialNone,
	},
	DriverSnowflake: {
		Driver: DriverSnowflake, DisplayLabel: "Snowflake",
		Scheme: "snowflake", Credential: CredentialToken,
	},
}

// Lookup returns metadata for d, or zero Info if unknown.
func Lookup(d Driver) Info {
	return Registry[d]
}
