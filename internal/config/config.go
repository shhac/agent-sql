// Package config handles reading and writing the agent-sql configuration file.
// Config lives at $XDG_CONFIG_HOME/agent-sql/config.json (default ~/.config/agent-sql/).
package config

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Connection represents a saved database connection.
type Connection struct {
	Driver     string            `json:"driver"`
	Host       string            `json:"host,omitempty"`
	Port       int               `json:"port,omitempty"`
	Database   string            `json:"database,omitempty"`
	Path       string            `json:"path,omitempty"`
	URL        string            `json:"url,omitempty"`
	Credential string            `json:"credential,omitempty"`
	Account    string            `json:"account,omitempty"`
	Warehouse  string            `json:"warehouse,omitempty"`
	Role       string            `json:"role,omitempty"`
	Schema     string            `json:"schema,omitempty"`
	Options    map[string]string `json:"options,omitempty"`
}

// Settings holds persistent configuration settings.
type Settings struct {
	Defaults   *DefaultsSettings   `json:"defaults,omitempty"`
	Query      *QuerySettings      `json:"query,omitempty"`
	Truncation *TruncationSettings `json:"truncation,omitempty"`
}

// DefaultsSettings holds default output settings.
type DefaultsSettings struct {
	Limit  *int   `json:"limit,omitempty"`
	Format string `json:"format,omitempty"`
}

// QuerySettings holds query execution settings.
type QuerySettings struct {
	Timeout *int `json:"timeout,omitempty"`
	MaxRows *int `json:"maxRows,omitempty"`
}

// TruncationSettings holds string truncation settings.
type TruncationSettings struct {
	MaxLength *int `json:"maxLength,omitempty"`
}

// Config is the top-level configuration structure.
type Config struct {
	DefaultConnection string                `json:"default_connection,omitempty"`
	Connections       map[string]Connection `json:"connections"`
	Settings          Settings              `json:"settings"`
}

var (
	cache     *Config
	cacheMu   sync.Mutex
	configDir string
)

// ConfigDir returns the config directory, creating it if needed.
func ConfigDir() string {
	if configDir != "" {
		return configDir
	}
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "agent-sql")
}

// SetConfigDir overrides the config directory (for testing).
func SetConfigDir(dir string) {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	configDir = dir
	cache = nil
}

func configPath() string {
	return filepath.Join(ConfigDir(), "config.json")
}

// ClearCache forces a re-read on next access.
func ClearCache() {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	cache = nil
}

// Read returns the current configuration.
func Read() *Config {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	if cache != nil {
		return cache
	}

	data, err := os.ReadFile(configPath())
	if err != nil {
		return defaultConfig()
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return defaultConfig()
	}
	if cfg.Connections == nil {
		cfg.Connections = make(map[string]Connection)
	}
	cache = &cfg
	return cache
}

// Write saves the configuration to disk.
func Write(cfg *Config) error {
	cacheMu.Lock()
	cache = nil
	cacheMu.Unlock()

	dir := ConfigDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "config.json"), append(data, '\n'), 0o644)
}

// GetConnection returns a connection by alias, or nil if not found.
func GetConnection(alias string) *Connection {
	cfg := Read()
	conn, ok := cfg.Connections[alias]
	if !ok {
		return nil
	}
	return &conn
}

// GetConnections returns all connections.
func GetConnections() map[string]Connection {
	return Read().Connections
}

// GetDefaultAlias returns the default connection alias, or empty string.
func GetDefaultAlias() string {
	return Read().DefaultConnection
}

// StoreConnection saves a connection. First connection becomes the default.
func StoreConnection(alias string, conn Connection) error {
	cfg := Read()
	cfg.Connections[alias] = conn
	if cfg.DefaultConnection == "" {
		cfg.DefaultConnection = alias
	}
	return Write(cfg)
}

// RemoveConnection removes a connection by alias.
func RemoveConnection(alias string) error {
	cfg := Read()
	if _, ok := cfg.Connections[alias]; !ok {
		return &ConnectionNotFoundError{Alias: alias, Available: connectionAliases(cfg)}
	}
	delete(cfg.Connections, alias)
	if cfg.DefaultConnection == alias {
		cfg.DefaultConnection = ""
		for k := range cfg.Connections {
			cfg.DefaultConnection = k
			break
		}
	}
	return Write(cfg)
}

// SetDefault sets the default connection alias.
func SetDefault(alias string) error {
	cfg := Read()
	if _, ok := cfg.Connections[alias]; !ok {
		return &ConnectionNotFoundError{Alias: alias, Available: connectionAliases(cfg)}
	}
	cfg.DefaultConnection = alias
	return Write(cfg)
}

// GetSetting reads a dotted config key (e.g. "query.timeout").
func GetSetting(key string) any {
	cfg := Read()
	parts := strings.Split(key, ".")
	var current any = settingsToMap(cfg.Settings)
	for _, part := range parts {
		m, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = m[part]
	}
	return current
}

// UpdateSetting writes a dotted config key.
func UpdateSetting(key string, value any) error {
	cfg := Read()
	parts := strings.Split(key, ".")
	m := settingsToMap(cfg.Settings)
	parent := m
	for _, part := range parts[:len(parts)-1] {
		child, ok := parent[part].(map[string]any)
		if !ok {
			child = make(map[string]any)
			parent[part] = child
		}
		parent = child
	}
	parent[parts[len(parts)-1]] = value
	cfg.Settings = mapToSettings(m)
	return Write(cfg)
}

// ResetSettings clears all settings to defaults.
func ResetSettings() error {
	cfg := Read()
	cfg.Settings = Settings{}
	return Write(cfg)
}

// ConnectionNotFoundError is returned when a connection alias doesn't exist.
type ConnectionNotFoundError struct {
	Alias     string
	Available []string
}

func (e *ConnectionNotFoundError) Error() string {
	listing := "(none)"
	if len(e.Available) > 0 {
		listing = strings.Join(e.Available, ", ")
	}
	return "Unknown connection: \"" + e.Alias + "\". Valid: " + listing
}

func defaultConfig() *Config {
	return &Config{
		Connections: make(map[string]Connection),
		Settings:    Settings{},
	}
}

func connectionAliases(cfg *Config) []string {
	aliases := make([]string, 0, len(cfg.Connections))
	for k := range cfg.Connections {
		aliases = append(aliases, k)
	}
	return aliases
}

func settingsToMap(s Settings) map[string]any {
	data, _ := json.Marshal(s)
	var m map[string]any
	_ = json.Unmarshal(data, &m)
	if m == nil {
		m = make(map[string]any)
	}
	return m
}

func mapToSettings(m map[string]any) Settings {
	data, _ := json.Marshal(m)
	var s Settings
	_ = json.Unmarshal(data, &s)
	return s
}

// DisplayURL builds a human-readable connection URL from config fields.
// Never includes credentials -- only the connection target. Render-time only:
// it backfills empty host/port/database from c.URL and applies the per-driver
// default port so the listing reflects what would actually be used at connect
// time. Stored Options are appended as `?key=value&...` (alphabetized) for
// URL-form drivers. Storage is not modified.
func (c Connection) DisplayURL() string {
	base := c.displayBase()
	if c.Driver == "duckdb" {
		// duckdb has no URL form; never append a query string.
		return base
	}
	if q := optionsQueryString(c.Options); q != "" {
		return base + "?" + q
	}
	return base
}

// hostPortDriverInfo holds per-driver display info for host:port-style
// drivers (pg, cockroachdb, mysql, mariadb, mssql). The scheme is what
// appears before :// in display URLs (note pg → "postgres"). DefaultPort
// mirrors the connect-time default applied in resolve.connectFromConfig.
type hostPortDriverInfo struct {
	Scheme      string
	DefaultPort int
}

// hostPortDrivers is the single source of truth for which drivers use
// host:port wire format and what their display scheme + default port are.
// Adding a new host:port driver: add one entry here. Adding a non-host
// driver (file, account, etc.) requires a new arm in displayBase below.
var hostPortDrivers = map[string]hostPortDriverInfo{
	"pg":          {Scheme: "postgres", DefaultPort: 5432},
	"cockroachdb": {Scheme: "cockroachdb", DefaultPort: 26257},
	"mysql":       {Scheme: "mysql", DefaultPort: 3306},
	"mariadb":     {Scheme: "mariadb", DefaultPort: 3306},
	"mssql":       {Scheme: "mssql", DefaultPort: 1433},
}

func (c Connection) displayBase() string {
	if info, ok := hostPortDrivers[c.Driver]; ok {
		host, port, db := effectiveHostPortDB(c, c.Driver)
		return hostPortDBURL(info.Scheme, host, port, db)
	}
	switch c.Driver {
	case "sqlite":
		if c.Path != "" {
			return "sqlite://" + c.Path
		}
		return "sqlite://"
	case "duckdb":
		if c.Path != "" {
			return "duckdb://" + c.Path
		}
		return "duckdb://"
	case "snowflake":
		u := "snowflake://"
		if c.Account != "" {
			u += c.Account
		}
		if c.Database != "" {
			u += "/" + c.Database
		}
		if c.Schema != "" {
			u += "/" + c.Schema
		}
		return u
	default:
		return c.Driver + "://"
	}
}

// optionsQueryString renders an options map as a deterministically-ordered
// `key=value&...` string (URL-encoded). Empty map → "".
func optionsQueryString(opts map[string]string) string {
	if len(opts) == 0 {
		return ""
	}
	keys := make([]string, 0, len(opts))
	for k := range opts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	v := url.Values{}
	for _, k := range keys {
		v.Set(k, opts[k])
	}
	return v.Encode()
}

// defaultPort returns the connect-time default port for a host:port-style
// driver. Reads from the hostPortDrivers registry (single source of truth).
func defaultPort(driver string) int {
	if info, ok := hostPortDrivers[driver]; ok {
		return info.DefaultPort
	}
	return 0
}

// parseURLFallback extracts host/port/database from a URL string, returning
// zero values when the URL is empty or unparseable. Used as a fallback for
// stored connections that have only a URL field populated.
func parseURLFallback(rawURL string) (host string, port int, db string) {
	if rawURL == "" {
		return "", 0, ""
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", 0, ""
	}
	host = u.Hostname()
	if p := u.Port(); p != "" {
		if n, parseErr := strconv.Atoi(p); parseErr == nil {
			port = n
		}
	}
	db = strings.TrimPrefix(u.Path, "/")
	return
}

// effectiveHostPortDB resolves host/port/database for display in three
// composable steps: stored fields → URL fallback → driver default port.
// All in-memory; never written back.
func effectiveHostPortDB(c Connection, driver string) (string, int, string) {
	host, port, db := c.Host, c.Port, c.Database
	if host == "" {
		fbHost, fbPort, fbDB := parseURLFallback(c.URL)
		host = fbHost
		if port == 0 {
			port = fbPort
		}
		if db == "" {
			db = fbDB
		}
	}
	if port == 0 {
		port = defaultPort(driver)
	}
	return host, port, db
}

// EffectiveHost returns the connection's host for display, derived the same
// way as DisplayURL: stored Host, then parsed from URL. For drivers where
// "host" doesn't apply (sqlite, duckdb), returns "". For snowflake, returns
// the account identifier.
func (c Connection) EffectiveHost() string {
	if _, ok := hostPortDrivers[c.Driver]; ok {
		host, _, _ := effectiveHostPortDB(c, c.Driver)
		return host
	}
	if c.Driver == "snowflake" {
		return c.Account
	}
	return ""
}

// EffectivePort returns the connect-time port for host:port drivers: stored
// Port, then parsed from URL, then the per-driver default. Returns 0 for
// drivers without a port (sqlite, duckdb, snowflake).
func (c Connection) EffectivePort() int {
	if _, ok := hostPortDrivers[c.Driver]; ok {
		_, port, _ := effectiveHostPortDB(c, c.Driver)
		return port
	}
	return 0
}

func hostPortDBURL(scheme, host string, port int, database string) string {
	u := scheme + "://"
	if host != "" {
		u += host
	}
	if port != 0 {
		u += ":" + strconv.Itoa(port)
	}
	if database != "" {
		u += "/" + database
	}
	return u
}
