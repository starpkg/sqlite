package sqlite

import (
	"fmt"
	"time"

	"github.com/1set/starlet/dataconv"
	startime "go.starlark.net/lib/time"
	"go.starlark.net/starlark"
)

// starlarkToSQLiteValue converts a Starlark value to a Go value suitable for SQLite.
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

// sqliteToStarlarkValue converts a Go value from SQLite to a Starlark value.
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

// sqlQuery contains the query and optional parameters.
type sqlQuery struct {
	query  string
	params []interface{}
}

// newSQLQuery creates a new SQL query from a query string and Starlark parameters.
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
