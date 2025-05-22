// Package sqlite provides a Starlark module for SQLite database operations.
package sqlite

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/1set/starlet"
	"github.com/1set/starlet/dataconv"
	"github.com/1set/starlet/dataconv/types"
	"github.com/starpkg/base"
	startime "go.starlark.net/lib/time"
	"go.starlark.net/starlark"
	_ "modernc.org/sqlite"
)

// Module constants
const (
	// ModuleName defines the expected name for this module when used in Starlark's load() function
	ModuleName = "sqlite"

	// Default configuration values
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
)

// Module wraps the ConfigurableModule with specific functionality for SQLite operations.
type Module struct {
	cfgMod *base.ConfigurableModule
	ext    *base.ConfigurableModuleExt
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
	)
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
) *Module {
	cm, _ := base.NewConfigurableModuleWithConfigOptions(
		databaseOpt,
		timeoutOpt,
		busyTimeoutOpt,
		foreignKeysOpt,
		journalModeOpt,
		synchronousOpt,
		cacheSizeOpt,
	)
	return &Module{
		cfgMod: cm,
		ext:    cm.Extend(),
	}
}

// LoadModule returns the Starlark module loader with SQLite-specific functions.
func (m *Module) LoadModule() starlet.ModuleLoader {
	// Prepare methods dictionary
	additionalFuncs := starlark.StringDict{
		"connect": starlark.NewBuiltin(ModuleName+".connect", m.connect),
	}

	// Return the module
	return m.cfgMod.LoadModule(ModuleName, additionalFuncs)
}

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

	// Create a new database connection
	db, err := openDatabase(database, timeout.GoFloat(), busyTimeout.GoFloat(), foreignKeysValue, journalMode, synchronous, cacheSize)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Create and return the database object
	return newDatabaseInstance(db), nil
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

// boolToInt converts a boolean value to an integer (1 for true, 0 for false)
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// Convert Starlark value to a Go value suitable for SQLite
func starlarkToSQLiteValue(v starlark.Value) (interface{}, error) {
	switch v := v.(type) {
	case starlark.NoneType:
		return nil, nil
	case starlark.Bool:
		if v {
			return 1, nil
		}
		return 0, nil
	case starlark.Int:
		if i, ok := v.Int64(); ok {
			return i, nil
		}
		return nil, fmt.Errorf("int value too large for SQLite: %v", v)
	case starlark.Float:
		return float64(v), nil
	case starlark.String:
		return string(v), nil
	case starlark.Bytes:
		return []byte(v), nil
	case startime.Time:
		return time.Time(v), nil
	case *starlark.Dict, *starlark.List:
		// Convert to JSON
		return dataconv.MarshalStarlarkJSON(v, 0)
	default:
		return nil, fmt.Errorf("unsupported type for SQLite: %s", v.Type())
	}
}

// Convert a Go value from SQLite to a Starlark value
func sqliteToStarlarkValue(v interface{}) (starlark.Value, error) {
	if v == nil {
		return starlark.None, nil
	}

	switch v := v.(type) {
	case int:
		return starlark.MakeInt(v), nil
	case int64:
		return starlark.MakeInt64(v), nil
	case float64:
		return starlark.Float(v), nil
	case bool:
		return starlark.Bool(v), nil
	case string:
		return starlark.String(v), nil
	case []byte:
		return starlark.Bytes(v), nil
	case time.Time:
		return startime.Time(v), nil
	default:
		return nil, fmt.Errorf("unsupported SQLite type for Starlark: %T", v)
	}
}

// sqlQuery contains the query and optional parameters
type sqlQuery struct {
	query  string
	params []interface{}
}

// newSQLQuery creates a new SQL query from a query string and Starlark parameters
func newSQLQuery(query string, params starlark.Sequence) (*sqlQuery, error) {
	// Get parameters as Go values
	var goParams []interface{}
	if params != nil {
		iter := params.Iterate()
		defer iter.Done()
		var val starlark.Value
		for iter.Next(&val) {
			param, err := starlarkToSQLiteValue(val)
			if err != nil {
				return nil, err
			}
			goParams = append(goParams, param)
		}
	}

	return &sqlQuery{
		query:  query,
		params: goParams,
	}, nil
}
