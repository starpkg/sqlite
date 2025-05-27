package sqlite

import (
	"database/sql/driver"
	"fmt"
	"sync"
	"time"

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
	return func(ctx *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
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
		i64, _ := v.Int64()
		return i64, nil
	case starlark.Float:
		return float64(v), nil
	case starlark.String:
		return string(v), nil
	case starlark.Bytes:
		return []byte(v), nil
	default:
		// For complex types (dict, list), convert to JSON string
		if isComplexType(val) {
			return convertComplexTypeToJSON(val)
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

// convertComplexTypeToJSON converts complex Starlark types to JSON strings.
func convertComplexTypeToJSON(val starlark.Value) (string, error) {
	// Convert Starlark value to Go value first
	goVal, err := starlarkToGoValue(val)
	if err != nil {
		return "", fmt.Errorf("failed to convert to Go value: %w", err)
	}

	// Then convert Go value to JSON
	return convertGoValueToJSON(goVal)
}

// starlarkToGoValue converts a Starlark value to a Go value.
func starlarkToGoValue(val starlark.Value) (interface{}, error) {
	if val == nil || val == starlark.None {
		return nil, nil
	}

	switch v := val.(type) {
	case starlark.Bool:
		return bool(v), nil
	case starlark.Int:
		i64, _ := v.Int64()
		return i64, nil
	case starlark.Float:
		return float64(v), nil
	case starlark.String:
		return string(v), nil
	case starlark.Bytes:
		return []byte(v), nil
	case *starlark.List:
		return convertStarlarkList(v)
	case starlark.Tuple:
		return convertStarlarkTuple(v)
	case *starlark.Dict:
		return convertStarlarkDict(v)
	default:
		return val.String(), nil
	}
}

// convertStarlarkList converts a Starlark list to a Go slice.
func convertStarlarkList(list *starlark.List) ([]interface{}, error) {
	result := make([]interface{}, list.Len())
	for i := 0; i < list.Len(); i++ {
		item := list.Index(i)
		val, err := starlarkToGoValue(item)
		if err != nil {
			return nil, err
		}
		result[i] = val
	}
	return result, nil
}

// convertStarlarkTuple converts a Starlark tuple to a Go slice.
func convertStarlarkTuple(tuple starlark.Tuple) ([]interface{}, error) {
	result := make([]interface{}, len(tuple))
	for i, item := range tuple {
		val, err := starlarkToGoValue(item)
		if err != nil {
			return nil, err
		}
		result[i] = val
	}
	return result, nil
}

// convertStarlarkDict converts a Starlark dict to a Go map.
func convertStarlarkDict(dict *starlark.Dict) (map[string]interface{}, error) {
	result := make(map[string]interface{})
	for _, item := range dict.Items() {
		keyVal, err := starlarkToGoValue(item.Index(0))
		if err != nil {
			return nil, err
		}
		valueVal, err := starlarkToGoValue(item.Index(1))
		if err != nil {
			return nil, err
		}

		// Convert key to string
		var keyStr string
		switch k := keyVal.(type) {
		case string:
			keyStr = k
		default:
			keyStr = fmt.Sprintf("%v", k)
		}

		result[keyStr] = valueVal
	}
	return result, nil
}

// convertGoValueToJSON converts a Go value to JSON string.
func convertGoValueToJSON(val interface{}) (string, error) {
	// Simple JSON encoding without external dependencies
	switch v := val.(type) {
	case nil:
		return "null", nil
	case bool:
		if v {
			return "true", nil
		}
		return "false", nil
	case int64:
		return fmt.Sprintf("%d", v), nil
	case float64:
		return fmt.Sprintf("%g", v), nil
	case string:
		return fmt.Sprintf("%q", v), nil
	case []byte:
		return fmt.Sprintf("%q", string(v)), nil
	case []interface{}:
		return convertSliceToJSON(v)
	case map[string]interface{}:
		return convertMapToJSON(v)
	default:
		return fmt.Sprintf("%q", fmt.Sprintf("%v", v)), nil
	}
}

// convertSliceToJSON converts a Go slice to JSON array string.
func convertSliceToJSON(slice []interface{}) (string, error) {
	if len(slice) == 0 {
		return "[]", nil
	}

	result := "["
	for i, item := range slice {
		if i > 0 {
			result += ","
		}
		itemJSON, err := convertGoValueToJSON(item)
		if err != nil {
			return "", err
		}
		result += itemJSON
	}
	result += "]"
	return result, nil
}

// convertMapToJSON converts a Go map to JSON object string.
func convertMapToJSON(m map[string]interface{}) (string, error) {
	if len(m) == 0 {
		return "{}", nil
	}

	result := "{"
	first := true
	for key, value := range m {
		if !first {
			result += ","
		}
		first = false

		keyJSON := fmt.Sprintf("%q", key)
		valueJSON, err := convertGoValueToJSON(value)
		if err != nil {
			return "", err
		}
		result += keyJSON + ":" + valueJSON
	}
	result += "}"
	return result, nil
}
