package sqlite

import (
	"database/sql"
	"database/sql/driver"
	"fmt"
	"sync"
	"time"

	"github.com/1set/starlet/dataconv"
	"go.starlark.net/starlark"
	"modernc.org/sqlite"
)

// ============================================================================
// Global Function Registry
// ============================================================================

// registeredFunction holds information about a registered custom function.
type registeredFunction struct {
	name          string
	starlarkFunc  starlark.Callable
	numArgs       int32 // -1 for variadic
	deterministic bool
}

var (
	// Global registry for custom functions
	registeredFuncs = make(map[string]*registeredFunction)
	funcMutex       sync.RWMutex
)

// localDriverName is the database/sql driver name connect() opens through: a
// funcMutex-serialized wrapper over modernc's "sqlite" driver (see below).
const localDriverName = "sqlite-udf-serialized"

var registerLocalDriverOnce sync.Once

// ensureLocalDriver registers the funcMutex-serialized wrapper over modernc's
// "sqlite" driver, exactly once.
//
// modernc's Driver.Open reads its process-global user-defined-function map
// (d.udfs) with NO lock, while RegisterScalarFunction writes it.
// register_function serializes writes under funcMutex, but a connection's Open —
// called lazily by database/sql, possibly from another host machine running in
// parallel — reads d.udfs unlocked, so a concurrent register_function + connect
// is a data race (potentially a fatal concurrent-map access). Opening through
// this wrapper makes every Open hold funcMutex for reading, serialized against
// register_function's write.
//
// Scope: this serializes THIS module's register_function against THIS module's
// own connections (connect opens through localDriverName). It does not, and
// cannot, protect a different component in the same process that opens the raw
// "sqlite" driver directly, nor concurrent registration of modernc collations /
// connection hooks — this module registers neither, and those globals are
// modernc's own unlocked state. A modernc connection hook that reenters
// register_function while a wrapped Open holds the read lock would deadlock;
// this module registers no connection hooks, so that path is unreachable here.
func ensureLocalDriver() {
	registerLocalDriverOnce.Do(func() {
		// A lazily-opened handle purely to obtain the registered driver instance
		// (Open is not called here, so the empty DSN is never used).
		db, err := sql.Open("sqlite", "")
		if err != nil {
			return // "sqlite" is always registered by the modernc import
		}
		inner := db.Driver()
		_ = db.Close()
		sql.Register(localDriverName, &udfSerializedDriver{inner: inner})
	})
}

// udfSerializedDriver wraps modernc's driver so a connection Open (which reads
// the global UDF map) is serialized against register_function (which writes it).
type udfSerializedDriver struct{ inner driver.Driver }

// Open holds funcMutex for reading while the wrapped driver reads its global UDF
// map. register_function holds it for writing, so the two never run concurrently;
// concurrent Opens still proceed in parallel (a read lock).
func (d *udfSerializedDriver) Open(name string) (driver.Conn, error) {
	funcMutex.RLock()
	defer funcMutex.RUnlock()
	return d.inner.Open(name)
}

// ============================================================================
// Function Registration
// ============================================================================

// registerFunction implements the register_function Starlark builtin.
func registerFunction(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var name string
	var funcVal starlark.Callable
	var numArgs starlark.Value = starlark.None
	var deterministic bool = false

	if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
		"name", &name,
		"func", &funcVal,
		"num_args?", &numArgs,
		"deterministic?", &deterministic,
	); err != nil {
		return nil, err
	}

	// Validate function name
	if name == "" {
		return nil, fmt.Errorf("function name cannot be empty")
	}

	// Validate callable
	if funcVal == nil {
		return nil, fmt.Errorf("function cannot be nil")
	}

	// Parse num_args
	var numArgsInt int32 = -1 // Default to variadic
	if numArgs != starlark.None {
		if intVal, ok := numArgs.(starlark.Int); ok {
			val, _ := intVal.Int64()
			if val < -1 {
				return nil, fmt.Errorf("num_args must be >= -1 (got %d)", val)
			}
			numArgsInt = int32(val)
		} else {
			return nil, fmt.Errorf("num_args must be an integer")
		}
	}

	// Register the function
	if err := doRegisterFunction(name, funcVal, numArgsInt, deterministic); err != nil {
		return nil, err
	}

	return starlark.None, nil
}

// doRegisterFunction performs the actual function registration.
func doRegisterFunction(name string, funcVal starlark.Callable, numArgs int32, deterministic bool) error {
	funcMutex.Lock()
	defer funcMutex.Unlock()

	// Check if function already exists
	if _, exists := registeredFuncs[name]; exists {
		return fmt.Errorf("function '%s' is already registered", name)
	}

	// Store in global registry
	regFunc := &registeredFunction{
		name:          name,
		starlarkFunc:  funcVal,
		numArgs:       numArgs,
		deterministic: deterministic,
	}
	registeredFuncs[name] = regFunc

	// Create Go wrapper function
	wrapper := createGoFunctionWrapper(regFunc)

	// Register with SQLite driver
	var err error
	if deterministic {
		err = sqlite.RegisterDeterministicScalarFunction(name, numArgs, wrapper)
	} else {
		err = sqlite.RegisterScalarFunction(name, numArgs, wrapper)
	}

	if err != nil {
		// Remove from registry if SQLite registration failed
		delete(registeredFuncs, name)
		return fmt.Errorf("failed to register function with SQLite driver: %w", err)
	}

	return nil
}

// ============================================================================
// Go Function Wrapper Creation
// ============================================================================

// createGoFunctionWrapper creates a Go function that bridges Starlark to SQLite.
func createGoFunctionWrapper(regFunc *registeredFunction) func(ctx *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
	return func(ctx *sqlite.FunctionContext, args []driver.Value) (retVal driver.Value, retErr error) {
		// A custom function runs arbitrary Starlark inside the SQLite driver's
		// scalar-function callback. Recover any panic and surface it as an error
		// so script input can never crash the host (hardening invariant).
		defer func() {
			if r := recover(); r != nil {
				retVal = nil
				retErr = fmt.Errorf("custom function %q panicked: %v", regFunc.name, r)
			}
		}()

		// Create new thread for this function call
		thread := &starlark.Thread{
			Name: fmt.Sprintf("custom_function_%s", regFunc.name),
		}

		// Convert arguments from driver.Value to Starlark values
		starlarkArgs := make([]starlark.Value, len(args))
		for i, arg := range args {
			val, err := driverValueToStarlark(arg)
			if err != nil {
				return nil, fmt.Errorf("failed to convert argument %d: %w", i, err)
			}
			starlarkArgs[i] = val
		}

		// Call the Starlark function
		argTuple := starlark.Tuple(starlarkArgs)
		result, err := starlark.Call(thread, regFunc.starlarkFunc, argTuple, nil)
		if err != nil {
			return nil, fmt.Errorf("Starlark function execution failed: %w", err)
		}

		// Convert result back to driver.Value
		driverResult, err := starlarkToDriverValue(result)
		if err != nil {
			return nil, fmt.Errorf("failed to convert result: %w", err)
		}

		return driverResult, nil
	}
}

// ============================================================================
// Type Conversion Functions
// ============================================================================

// driverValueToStarlark converts a SQLite driver.Value to a Starlark value.
func driverValueToStarlark(val driver.Value) (starlark.Value, error) {
	if val == nil {
		return starlark.None, nil
	}

	switch v := val.(type) {
	case bool:
		return starlark.Bool(v), nil
	case int64:
		return starlark.MakeInt64(v), nil
	case float64:
		return starlark.Float(v), nil
	case string:
		return starlark.String(v), nil
	case []byte:
		return starlark.Bytes(v), nil
	case time.Time:
		// Convert time.Time to string representation
		return starlark.String(v.String()), nil
	default:
		// Try to handle other types by converting to string
		return starlark.String(fmt.Sprintf("%v", v)), nil
	}
}

// starlarkToDriverValue converts a Starlark value to a SQLite driver.Value.
func starlarkToDriverValue(val starlark.Value) (driver.Value, error) {
	if val == nil || val == starlark.None {
		return nil, nil
	}

	switch v := val.(type) {
	case starlark.Bool:
		return bool(v), nil
	case starlark.Int:
		// Mirror the bind-parameter path (starlarkToSQLiteValue): an int that
		// does not fit int64 must surface a clear error, never silently
		// truncate to a wrong driver value.
		i64, ok := v.Int64()
		if !ok {
			return nil, fmt.Errorf("int value too large for SQLite: %v", v)
		}
		return i64, nil
	case starlark.Float:
		return float64(v), nil
	case starlark.String:
		return string(v), nil
	case starlark.Bytes:
		return []byte(v), nil
	default:
		// For complex types (dict, list, tuple), use the existing dataconv utility
		// which converts them to JSON strings
		if isComplexType(val) {
			jsonStr, err := dataconv.MarshalStarlarkJSON(val, 0)
			if err != nil {
				return nil, fmt.Errorf("failed to convert complex type to JSON: %w", err)
			}
			return jsonStr, nil
		}
		// For other types, convert to string
		return val.String(), nil
	}
}

// isComplexType checks if a Starlark value is a complex type that should be JSON-encoded.
func isComplexType(val starlark.Value) bool {
	switch val.(type) {
	case *starlark.Dict, *starlark.List, starlark.Tuple:
		return true
	default:
		return false
	}
}
