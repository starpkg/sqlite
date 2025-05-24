package sqlite

import (
	"database/sql"
	"fmt"

	"go.starlark.net/starlark"
)

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
// Parameter Processing Utilities
// ============================================================================

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
