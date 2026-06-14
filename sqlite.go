// Package sqlite provides a Starlark module for SQLite database operations.
package sqlite

import (
	"database/sql"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/1set/starlet"
	"github.com/1set/starlet/dataconv/types"
	"github.com/starpkg/base"
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
	defaultTimeout     = 30.0       // Default connection timeout in seconds.
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
		genConfigOption(configKeyTimeout, "Connection timeout in seconds", defaultTimeout),
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
	return base.NewConfigOption(defaultValue).
		WithName(name).
		WithDescription(description).
		WithEnvVar(strings.ToUpper(ModuleName + "_" + name))
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
	db, err := openDatabase(database, timeout.GoFloat(), busyTimeout.GoFloat(), foreignKeysValue, journalMode, synchronous, cacheSize)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Create and return the database object
	return newDatabaseInstance(db, maxRows, m.restrictFileAccess), nil
}

// connectRemote opens a connection to a remote libSQL server — a self-hosted
// sqld or Turso Cloud — over the pure-Go libSQL client (no cgo). The returned
// object exposes the same query/exec/table API as a local connection, because
// the libSQL remote client is a standard database/sql driver.
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
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to connect to remote database: %w", err)
	}

	maxRows := m.ext.GetInt(configKeyMaxRows, defaultMaxRows)
	return newDatabaseInstance(db, maxRows, m.restrictFileAccess), nil
}

// isInMemoryDSN reports whether a SQLite DSN refers to an in-memory (or private
// temporary) database rather than a real file on disk. The check is deliberately
// strict: a loose substring match on ":memory:" / "mode=memory" would let a file
// DSN such as "file:/etc/passwd?x=:memory:" slip past the file-access restriction.
func isInMemoryDSN(s string) bool {
	switch {
	case s == "", s == ":memory:":
		// Empty = private temporary database; ":memory:" = the in-memory database.
		return true
	case strings.HasPrefix(s, "file::memory:"):
		// Named in-memory database, e.g. "file::memory:?cache=shared".
		return true
	}
	// Honour "mode=memory" only as a real query parameter, not as a substring.
	if i := strings.IndexByte(s, '?'); i >= 0 {
		if q, err := url.ParseQuery(s[i+1:]); err == nil && q.Get("mode") == "memory" {
			return true
		}
	}
	return false
}

// openDatabase creates a new SQLite database connection with the given options.
func openDatabase(connStr string, timeout, busyTimeout float64, foreignKeys bool, journalMode, synchronous string, cacheSize int) (*sql.DB, error) {
	// Open database connection
	db, err := sql.Open("sqlite", connStr)
	if err != nil {
		return nil, err
	}

	// Verify database connection with Ping
	// This will catch database file accessibility issues early
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}

	// Configure connection options
	pragmas := []string{
		fmt.Sprintf("PRAGMA busy_timeout = %d", int(busyTimeout*1000)),
		fmt.Sprintf("PRAGMA journal_mode = %s", journalMode),
		fmt.Sprintf("PRAGMA synchronous = %s", synchronous),
		fmt.Sprintf("PRAGMA cache_size = %d", cacheSize),
		fmt.Sprintf("PRAGMA foreign_keys = %d", boolToInt(foreignKeys)),
	}

	// Execute all pragmas
	for _, pragma := range pragmas {
		_, err = db.Exec(pragma)
		if err != nil {
			db.Close()
			return nil, fmt.Errorf("failed to execute %s: %w", pragma, err)
		}
	}

	// Set timeout
	db.SetConnMaxLifetime(time.Duration(timeout * float64(time.Second)))

	return db, nil
}
