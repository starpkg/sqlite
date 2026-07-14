// Package sqlite provides a Starlark module for SQLite database operations.
package sqlite

import (
	"database/sql"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/1set/starlet"
	"github.com/1set/starlet/dataconv/types"
	"github.com/starpkg/base"
	"github.com/starpkg/base/util"
	"github.com/tursodatabase/libsql-client-go/libsql"
	"go.starlark.net/starlark"
	_ "modernc.org/sqlite"
)

// ============================================================================
// Module Constants and Configuration
// ============================================================================

// Module constants
const (
	// ModuleName defines the expected name for this module when used in Starlark's load() function
	ModuleName = "sqlite"
)

// Default configuration values
const (
	defaultTimeout     = 30.0       // Default per-operation deadline in seconds (0 = no deadline).
	defaultBusyTimeout = 5.0        // Default busy timeout in seconds. SQLite will wait for this duration if the database is locked.
	defaultDatabase    = ":memory:" // Default database path. Use ":memory:" for an in-memory database, or provide a file path.
	defaultForeignKeys = true       // Default for foreign key constraints. Set to true to enable, false to disable. Enabling enforces referential integrity.
	defaultJournalMode = "DELETE"   // Default journal mode. "DELETE" uses a rollback journal and removes journal files on commit.
	//   Other options: "WAL" (Write-Ahead Logging, good for concurrency), "MEMORY", "OFF", "TRUNCATE", "PERSIST".
	//   "DELETE" is often paired with SYNCHRONOUS=FULL for data safety.
	defaultSynchronous = "FULL" // Default synchronous mode. "FULL" ensures all data is written to disk before continuing, providing maximum safety.
	//   Other options: "NORMAL" (safer with WAL, faster than FULL), "OFF" (fastest but less safe).
	defaultCacheSize = -2000 // Default cache size. A negative value (e.g., -2000) instructs SQLite to use its
	//   default page cache size (typically 2000 pages, e.g., 8MB if page size is 4KB).
	//   A positive value sets the cache size in number of pages. 0 means no cache. The TEMP database has a default suggested cache size of 0 pages.
	defaultMaxRows = 0 // Default maximum rows returned by query helpers. 0 means unlimited.
)

// Configuration key constants
const (
	configKeyDatabase    = "database"
	configKeyTimeout     = "timeout"
	configKeyBusyTimeout = "busy_timeout"
	configKeyForeignKeys = "foreign_keys"
	configKeyJournalMode = "journal_mode"
	configKeySynchronous = "synchronous"
	configKeyCacheSize   = "cache_size"
	configKeyMaxRows     = "max_rows"
)

// ============================================================================
// Module Structure and Creation
// ============================================================================

// Module wraps the ConfigurableModule with specific functionality for SQLite operations.
type Module struct {
	cfgMod             *base.ConfigurableModule
	ext                *base.ConfigurableModuleExt
	restrictFileAccess bool
}

// NewModule creates a new module with default configuration.
func NewModule() *Module {
	return newModuleWithOptions(
		genConfigOption(configKeyDatabase, "Path to SQLite database (use :memory: for in-memory)", defaultDatabase),
		genConfigOption(configKeyTimeout, "Per-operation deadline in seconds (0 = no deadline)", defaultTimeout),
		genConfigOption(configKeyBusyTimeout, "Busy timeout in seconds", defaultBusyTimeout),
		genConfigOption(configKeyForeignKeys, "Enable foreign key constraints", defaultForeignKeys),
		genConfigOption(configKeyJournalMode, "Journal mode (WAL, DELETE, TRUNCATE, PERSIST, MEMORY, OFF)", defaultJournalMode),
		genConfigOption(configKeySynchronous, "Synchronous mode (FULL, NORMAL, OFF)", defaultSynchronous),
		genConfigOption(configKeyCacheSize, "Cache size in number of pages", defaultCacheSize),
		genConfigOption(configKeyMaxRows, "Maximum rows returned by query helpers (0 means unlimited)", defaultMaxRows),
	)
}

// NewModuleWithFileAccess creates a new module and optionally restricts file database access.
func NewModuleWithFileAccess(allowed bool) *Module {
	m := NewModule()
	m.restrictFileAccess = !allowed
	return m
}

// genConfigOption creates a configuration option with common settings.
// It sets up the name, description, default value, and environment variable.
func genConfigOption[T any](name, description string, defaultValue T) *base.ConfigOption[T] {
	return base.NewNamedConfigOption(ModuleName, name, description, defaultValue)
}

// newModuleWithOptions creates a Module with the given configuration options.
func newModuleWithOptions(
	databaseOpt *base.ConfigOption[string],
	timeoutOpt *base.ConfigOption[float64],
	busyTimeoutOpt *base.ConfigOption[float64],
	foreignKeysOpt *base.ConfigOption[bool],
	journalModeOpt *base.ConfigOption[string],
	synchronousOpt *base.ConfigOption[string],
	cacheSizeOpt *base.ConfigOption[int],
	maxRowsOpt *base.ConfigOption[int],
) *Module {
	cm, _ := base.NewConfigurableModuleWithConfigOptions(
		databaseOpt,
		timeoutOpt,
		busyTimeoutOpt,
		foreignKeysOpt,
		journalModeOpt,
		synchronousOpt,
		cacheSizeOpt,
		maxRowsOpt,
	)
	return &Module{
		cfgMod: cm,
		ext:    cm.Extend(),
	}
}

// ============================================================================
// Module Loading and Starlark Integration
// ============================================================================

// LoadModule returns the Starlark module loader with SQLite-specific functions.
func (m *Module) LoadModule() starlet.ModuleLoader {
	// Prepare methods dictionary
	additionalFuncs := starlark.StringDict{
		"connect":           starlark.NewBuiltin(ModuleName+".connect", m.connect),
		"connect_remote":    starlark.NewBuiltin(ModuleName+".connect_remote", m.connectRemote),
		"register_function": starlark.NewBuiltin(ModuleName+".register_function", registerFunction),
	}

	// Return the module
	return m.cfgMod.LoadModule(ModuleName, additionalFuncs)
}

// ============================================================================
// Database Connection Functions
// ============================================================================

// connect implements the connect function that creates a new database connection.
func (m *Module) connect(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var database string
	var timeout types.FloatOrInt
	var busyTimeout types.FloatOrInt
	var foreignKeys = types.NewNullableBool(false)
	var journalMode string
	var synchronous string
	var cacheSize int

	if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
		"database?", &database,
		"timeout?", &timeout,
		"busy_timeout?", &busyTimeout,
		"foreign_keys?", foreignKeys,
		"journal_mode?", &journalMode,
		"synchronous?", &synchronous,
		"cache_size?", &cacheSize,
	); err != nil {
		return nil, err
	}

	// Use default values from config if not provided
	if database == "" {
		database = m.ext.GetString(configKeyDatabase, defaultDatabase)
	}
	configuredDatabase := m.ext.GetString(configKeyDatabase, defaultDatabase)
	if m.restrictFileAccess && !isInMemoryDSN(database) && database != configuredDatabase {
		return nil, fmt.Errorf("file database access is restricted by the host; only in-memory or the host-configured database is allowed")
	}
	if timeout == 0 {
		timeout = types.FloatOrInt(m.ext.GetFloat(configKeyTimeout, defaultTimeout))
	}
	if busyTimeout == 0 {
		busyTimeout = types.FloatOrInt(m.ext.GetFloat(configKeyBusyTimeout, defaultBusyTimeout))
	}

	// Handle foreignKeys using NullableBool
	var foreignKeysValue bool
	if !foreignKeys.IsNull() {
		foreignKeysValue = bool(foreignKeys.Value().Truth())
	} else {
		foreignKeysValue = m.ext.GetBool(configKeyForeignKeys, defaultForeignKeys)
	}

	if journalMode == "" {
		journalMode = m.ext.GetString(configKeyJournalMode, defaultJournalMode)
	}
	if synchronous == "" {
		synchronous = m.ext.GetString(configKeySynchronous, defaultSynchronous)
	}
	if cacheSize == 0 {
		cacheSize = m.ext.GetInt(configKeyCacheSize, defaultCacheSize)
	}
	maxRows := m.ext.GetInt(configKeyMaxRows, defaultMaxRows)

	// Create a new database connection
	db, err := openDatabase(database, busyTimeout.GoFloat(), foreignKeysValue, journalMode, synchronous, cacheSize)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Create and return the database object. `timeout` now bounds each operation
	// (via a per-query context), replacing its former misuse as the connection's
	// max lifetime.
	return newDatabaseInstance(db, maxRows, m.restrictFileAccess, secondsToDuration(timeout.GoFloat())), nil
}

// secondsToDuration converts a fractional-seconds timeout into a time.Duration.
// A non-positive value means "no per-operation deadline".
func secondsToDuration(sec float64) time.Duration {
	if sec <= 0 {
		return 0
	}
	return time.Duration(sec * float64(time.Second))
}

// connectRemote opens a connection to a remote libSQL server — a self-hosted
// sqld or Turso Cloud — over the pure-Go libSQL client (no cgo). The returned
// object exposes the same query/exec/table API as a local connection, because
// the libSQL remote client is a standard database/sql driver.
//
// Cancellation caveat: queries (Ping/Query/Exec) over the HTTP(S)/Turso client
// honour the operation context, but the pinned libSQL driver on Go 1.20 does not
// thread a context into COMMIT/ROLLBACK, nor into a ws://wss:// connection dial
// (a fixed background dial). So a commit/rollback, or a reconnect over ws/wss,
// can still outlast the per-operation deadline / thread cancellation until the
// driver's own timeout. The documented HTTP(S)/Turso paths are the unaffected
// common case.
func (m *Module) connectRemote(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var dbURL, authToken string
	if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
		"url", &dbURL,
		"auth_token?", &authToken,
	); err != nil {
		return nil, err
	}
	if dbURL == "" {
		return nil, fmt.Errorf("connect_remote: url is required")
	}

	var opts []libsql.Option
	if authToken != "" {
		opts = append(opts, libsql.WithAuthToken(authToken))
	}
	connector, err := libsql.NewConnector(dbURL, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create remote connector: %w", err)
	}

	db := sql.OpenDB(connector)
	opTimeout := secondsToDuration(m.ext.GetFloat(configKeyTimeout, defaultTimeout))
	// A remote connect/query is a network round-trip: bound the Ping (and, below,
	// every query) with a context so an unreachable or slow host cannot block the
	// host goroutine indefinitely and so a cancelled script thread aborts it.
	pingCtx, cancel := util.OpContext(thread, opTimeout)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to connect to remote database: %w", err)
	}

	maxRows := m.ext.GetInt(configKeyMaxRows, defaultMaxRows)
	return newDatabaseInstance(db, maxRows, m.restrictFileAccess, opTimeout), nil
}

// isInMemoryDSN reports whether a SQLite DSN refers to an in-memory (or private
// temporary) database rather than a real file on disk. The check is deliberately
// strict: a loose substring match on ":memory:" / "mode=memory" would let a file
// DSN such as "file:/etc/passwd?x=:memory:" slip past the file-access restriction.
func isInMemoryDSN(s string) bool {
	// Empty = private temporary database; ":memory:" (optionally with query
	// parameters) = the in-memory database.
	if s == "" || s == ":memory:" || strings.HasPrefix(s, ":memory:?") {
		return true
	}
	// A DSN without the "file:" scheme is a literal filename to SQLite — even one
	// containing "?mode=memory" or ":memory:" is a real on-disk file, so it must
	// NOT be treated as in-memory (that was a file-access bypass).
	if !strings.HasPrefix(s, "file:") {
		return false
	}
	u, err := url.Parse(s)
	if err != nil {
		return false
	}
	// The path is in Opaque for "file::memory:" / "file:name" and in Path for
	// "file:/abs". An empty path is a private temporary database.
	path := u.Opaque
	if path == "" {
		path = u.Path
	}
	if path == "" || path == ":memory:" {
		return true
	}
	// mode=memory (last value wins, per SQLite) makes even a named database
	// in-memory; a real query parameter only, never a substring.
	modes := u.Query()["mode"]
	return len(modes) > 0 && modes[len(modes)-1] == "memory"
}

// openDatabase creates a new local SQLite database connection. The connection
// PRAGMAs are set through the DSN (modernc's _pragma= parameter) so they apply
// to EVERY physical connection the pool opens; applying them once with db.Exec
// only configures a single borrowed connection, leaving every later connection
// on SQLite's defaults (notably foreign_keys OFF, so constraint-violating writes
// silently succeed). An in-memory or private-temporary database is additionally
// pinned to one never-recycled connection, because its schema and data live only
// inside that connection — a second (empty) connection or a recycled one would
// otherwise make earlier data vanish ("no such table").
func openDatabase(connStr string, busyTimeout float64, foreignKeys bool, journalMode, synchronous string, cacheSize int) (*sql.DB, error) {
	if !pragmaIdentifier(journalMode) {
		return nil, fmt.Errorf("invalid journal_mode %q", journalMode)
	}
	if !pragmaIdentifier(synchronous) {
		return nil, fmt.Errorf("invalid synchronous %q", synchronous)
	}
	settings := pragmaSettings(busyTimeout, foreignKeys, journalMode, synchronous, cacheSize)

	memory := isInMemoryDSN(connStr)
	dsn := connStr
	if !memory {
		// A file database is served by a POOL of connections; carry the PRAGMAs in
		// the DSN (modernc _pragma=) so every connection the pool opens is
		// configured identically. Applying them once with db.Exec would leave
		// later connections on SQLite's defaults (notably foreign_keys OFF).
		dsn = appendPragmas(connStr, settings)
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if memory {
		// An in-memory / private-temporary database lives inside a single
		// connection: pin the pool to one never-recycled connection so its schema
		// and data don't vanish when a second (empty) connection opens or the first
		// is retired. With exactly one connection, db.Exec configures it directly
		// (and sidesteps the DSN quirk that an empty connStr would be misparsed).
		db.SetMaxOpenConns(1)
		db.SetConnMaxLifetime(0)
		db.SetConnMaxIdleTime(0)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	if memory {
		for _, stmt := range pragmaStatements(settings) {
			if _, err := db.Exec(stmt); err != nil {
				db.Close()
				return nil, fmt.Errorf("failed to apply %s: %w", stmt, err)
			}
		}
	}
	return db, nil
}

// pragmaIdentifier reports whether v is a bare alphanumeric PRAGMA keyword/value,
// so it is safe to place into a PRAGMA statement or the DSN _pragma= expression
// without risking injection (a value like "WAL); DROP …" is rejected).
func pragmaIdentifier(v string) bool {
	if v == "" {
		return false
	}
	for _, r := range v {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

// pragmaSettings returns the ordered (name, value) connection PRAGMAs. journal_mode
// and synchronous must already be validated by pragmaIdentifier; the rest are
// integer-formatted.
func pragmaSettings(busyTimeout float64, foreignKeys bool, journalMode, synchronous string, cacheSize int) [][2]string {
	return [][2]string{
		{"busy_timeout", strconv.Itoa(int(busyTimeout * 1000))},
		{"foreign_keys", strconv.Itoa(boolToInt(foreignKeys))},
		{"journal_mode", journalMode},
		{"synchronous", synchronous},
		{"cache_size", strconv.Itoa(cacheSize)},
	}
}

// appendPragmas builds the DSN carrying the PRAGMAs as modernc _pragma=
// parameters so every pooled (file) connection is configured identically.
func appendPragmas(connStr string, settings [][2]string) string {
	parts := make([]string, len(settings))
	for i, s := range settings {
		parts[i] = "_pragma=" + s[0] + "(" + s[1] + ")"
	}
	sep := "?"
	if strings.Contains(connStr, "?") {
		sep = "&"
	}
	return connStr + sep + strings.Join(parts, "&")
}

// pragmaStatements returns the PRAGMA statements for the single-connection
// (in-memory) path.
func pragmaStatements(settings [][2]string) []string {
	stmts := make([]string, len(settings))
	for i, s := range settings {
		stmts[i] = "PRAGMA " + s[0] + " = " + s[1]
	}
	return stmts
}
