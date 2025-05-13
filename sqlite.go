// Package sqlite provides a Starlark module for SQLite database operations.
package sqlite

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/1set/starlet"
	"github.com/starpkg/base"
	"go.starlark.net/starlark"
	_ "modernc.org/sqlite"
)

// Module constants
const (
	// ModuleName defines the expected name for this module when used in Starlark's load() function
	ModuleName = "sqlite"

	// Default configuration values
	defaultDatabase    = ":memory:"
	defaultTimeout     = 30.0
	defaultForeignKeys = true
	defaultJournalMode = "WAL"
	defaultSynchronous = "NORMAL"
	defaultCacheSize   = 2000
	defaultBusyTimeout = 5.0
)

// Configuration key constants
const (
	configKeyDatabase    = "database"
	configKeyTimeout     = "timeout"
	configKeyForeignKeys = "foreign_keys"
	configKeyJournalMode = "journal_mode"
	configKeySynchronous = "synchronous"
	configKeyCacheSize   = "cache_size"
	configKeyBusyTimeout = "busy_timeout"
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
	var timeout float64
	var busyTimeout float64
	var foreignKeys bool
	var journalMode string
	var synchronous string
	var cacheSize int

	if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
		"database?", &database,
		"timeout?", &timeout,
		"busy_timeout?", &busyTimeout,
		"foreign_keys?", &foreignKeys,
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
		timeout = m.ext.GetFloat(configKeyTimeout, defaultTimeout)
	}
	if busyTimeout == 0 {
		busyTimeout = m.ext.GetFloat(configKeyBusyTimeout, defaultBusyTimeout)
	}
	if !foreignKeys {
		foreignKeys = m.ext.GetBool(configKeyForeignKeys, defaultForeignKeys)
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
	db, err := openDatabase(database, timeout, busyTimeout, foreignKeys, journalMode, synchronous, cacheSize)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Create and return the database object
	return newDatabaseInstance(db), nil
}

// openDatabase creates a new SQLite database connection with the given options.
func openDatabase(database string, timeout float64, busyTimeout float64, foreignKeys bool, journalMode, synchronous string, cacheSize int) (*sql.DB, error) {
	// Prepare connection string
	connStr := database

	// Open database connection
	db, err := sql.Open("sqlite", connStr)
	if err != nil {
		return nil, err
	}

	// Configure connection options
	// Set busy timeout in milliseconds
	_, err = db.Exec(fmt.Sprintf("PRAGMA busy_timeout = %d", int(busyTimeout*1000)))
	if err != nil {
		db.Close()
		return nil, err
	}

	// Set journal mode
	_, err = db.Exec(fmt.Sprintf("PRAGMA journal_mode = %s", journalMode))
	if err != nil {
		db.Close()
		return nil, err
	}

	// Set synchronous mode
	_, err = db.Exec(fmt.Sprintf("PRAGMA synchronous = %s", synchronous))
	if err != nil {
		db.Close()
		return nil, err
	}

	// Set cache size
	_, err = db.Exec(fmt.Sprintf("PRAGMA cache_size = %d", cacheSize))
	if err != nil {
		db.Close()
		return nil, err
	}

	// Set foreign keys
	var fkValue int
	if foreignKeys {
		fkValue = 1
	}
	_, err = db.Exec(fmt.Sprintf("PRAGMA foreign_keys = %d", fkValue))
	if err != nil {
		db.Close()
		return nil, err
	}

	// Set timeout
	db.SetConnMaxLifetime(time.Duration(timeout * float64(time.Second)))

	return db, nil
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
	case *starlark.Dict, *starlark.List:
		// Convert to JSON
		goValue := starlarkValueToGoValue(v)
		jsonBytes, err := json.Marshal(goValue)
		if err != nil {
			return nil, fmt.Errorf("failed to convert to JSON: %w", err)
		}
		return string(jsonBytes), nil
	default:
		return nil, fmt.Errorf("unsupported type for SQLite: %s", v.Type())
	}
}

// starlarkValueToGoValue converts a Starlark value to a Go value
func starlarkValueToGoValue(v starlark.Value) interface{} {
	switch v := v.(type) {
	case starlark.NoneType:
		return nil
	case starlark.Bool:
		return bool(v)
	case starlark.Int:
		i, _ := v.Int64()
		return i
	case starlark.Float:
		return float64(v)
	case starlark.String:
		return string(v)
	case starlark.Bytes:
		return []byte(v)
	case *starlark.Dict:
		result := make(map[string]interface{})
		for _, item := range v.Items() {
			key, ok := item.Index(0).(starlark.String)
			if !ok {
				continue
			}
			result[string(key)] = starlarkValueToGoValue(item.Index(1))
		}
		return result
	case *starlark.List:
		result := make([]interface{}, 0, v.Len())
		for i := 0; i < v.Len(); i++ {
			item := v.Index(i)
			result = append(result, starlarkValueToGoValue(item))
		}
		return result
	case *starlark.Tuple:
		result := make([]interface{}, 0, v.Len())
		for i := 0; i < v.Len(); i++ {
			result = append(result, starlarkValueToGoValue(v.Index(i)))
		}
		return result
	default:
		return fmt.Sprint(v)
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
