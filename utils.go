package sqlite

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/1set/starlet/dataconv"
	startime "go.starlark.net/lib/time"
	"go.starlark.net/starlark"
)

// ============================================================================
// Type Conversion Utilities
// ============================================================================

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

// boolToInt converts a boolean value to an integer (1 for true, 0 for false).
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// formatSQLValue formats a Go value for use in SQL statements.
func formatSQLValue(val interface{}) string {
	switch v := val.(type) {
	case nil:
		return "NULL"
	case string:
		return fmt.Sprintf("'%s'", strings.ReplaceAll(v, "'", "''"))
	case int, int64, float64:
		return fmt.Sprintf("%v", v)
	case bool:
		if v {
			return "1"
		}
		return "0"
	default:
		return fmt.Sprintf("'%v'", v)
	}
}

// ============================================================================
// SQL Query Utilities
// ============================================================================

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

// convertStarlarkParams converts Starlark parameters to Go values for SQL execution.
func convertStarlarkParams(params starlark.Sequence) ([]interface{}, error) {
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
	return goParams, nil
}

// ============================================================================
// Query Processing Utilities
// ============================================================================

// processQueryRows processes SQL query results and converts them to Starlark values.
func processQueryRows(rows *sql.Rows) (starlark.Value, error) {
	defer rows.Close()

	// Get column names
	cols, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("failed to get column names: %w", err)
	}

	// Convert result rows to a Starlark list of dicts
	resultList := &starlark.List{}
	for rows.Next() {
		rowDict, err := scanRowToDict(rows, cols)
		if err != nil {
			return nil, err
		}

		// Append to result list
		if err := resultList.Append(rowDict); err != nil {
			return nil, err
		}
	}

	// Check for errors after iteration
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error during query iteration: %w", err)
	}

	return resultList, nil
}

// processQueryOneRow processes SQL query results and returns the first row or None.
func processQueryOneRow(rows *sql.Rows) (starlark.Value, error) {
	defer rows.Close()

	// Get column names
	cols, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("failed to get column names: %w", err)
	}

	// Check if we have a row
	if !rows.Next() {
		return starlark.None, nil
	}

	rowDict, err := scanRowToDict(rows, cols)
	if err != nil {
		return nil, err
	}

	// Check for errors after iteration
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error during query iteration: %w", err)
	}

	return rowDict, nil
}

// scanRowToDict scans a single row and converts it to a Starlark dictionary.
func scanRowToDict(rows *sql.Rows, cols []string) (*starlark.Dict, error) {
	// Prepare scan targets
	scanTargets := make([]interface{}, len(cols))
	for i := range scanTargets {
		var v interface{}
		scanTargets[i] = &v
	}

	// Scan row data
	if err := rows.Scan(scanTargets...); err != nil {
		return nil, fmt.Errorf("failed to scan row: %w", err)
	}

	// Create row dict
	rowDict := starlark.NewDict(len(cols))
	for i, col := range cols {
		// Get value from scan target
		val := *(scanTargets[i].(*interface{}))
		// Convert to Starlark value
		starVal, err := sqliteToStarlarkValue(val)
		if err != nil {
			return nil, err
		}
		// Add to dict
		if err := rowDict.SetKey(starlark.String(col), starVal); err != nil {
			return nil, err
		}
	}

	return rowDict, nil
}

// ============================================================================
// SQL Identifier Utilities
// ============================================================================

// quoteName quotes a SQL identifier (table or column name).
func quoteName(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// quoteNames quotes a list of SQL identifiers and joins them with commas.
func quoteNames(names []string) string {
	quoted := make([]string, len(names))
	for i, name := range names {
		quoted[i] = quoteName(name)
	}
	return strings.Join(quoted, ", ")
}
