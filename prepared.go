package sqlite

import (
	"database/sql"
	"fmt"

	"github.com/1set/starlet/dataconv"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

// ============================================================================
// Prepared Statement Instance and Creation
// ============================================================================

// preparedStmt represents a prepared statement struct with methods
// directly attached, providing cleaner and more idiomatic Go code.
type preparedStmt struct {
	stmt *sql.Stmt
}

// newPreparedStatementInstance creates a new Starlark prepared statement instance.
func newPreparedStatementInstance(stmt *sql.Stmt) *starlarkstruct.Module {
	ps := &preparedStmt{stmt: stmt}

	// Create dictionary of methods
	dict := starlark.StringDict{
		"execute": starlark.NewBuiltin("execute", ps.execute),
		"close":   starlark.NewBuiltin("close", ps.close),
	}

	return dataconv.MakeModule("prepared_statement", dict)
}

// newPreparedQueryInstance creates a new Starlark prepared query instance.
func newPreparedQueryInstance(stmt *sql.Stmt) *starlarkstruct.Module {
	ps := &preparedStmt{stmt: stmt}

	// Create dictionary of methods
	dict := starlark.StringDict{
		"query":     starlark.NewBuiltin("query", ps.query),
		"query_one": starlark.NewBuiltin("query_one", ps.queryOne),
		"close":     starlark.NewBuiltin("close", ps.close),
	}

	return dataconv.MakeModule("prepared_query", dict)
}

// ============================================================================
// Prepared Statement Methods
// ============================================================================

// execute executes a prepared statement with parameters.
func (ps *preparedStmt) execute(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var params starlark.Sequence

	if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
		"params?", &params); err != nil {
		return nil, err
	}

	// Convert parameters to Go values
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

	// Execute the statement
	result, err := ps.stmt.Exec(goParams...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute statement: %w", err)
	}

	// Get affected rows
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("failed to get rows affected: %w", err)
	}

	return starlark.MakeInt64(rowsAffected), nil
}

// close closes the prepared statement.
func (ps *preparedStmt) close(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if err := starlark.UnpackArgs(fn.Name(), args, kwargs); err != nil {
		return nil, err
	}

	if err := ps.stmt.Close(); err != nil {
		return nil, fmt.Errorf("failed to close statement: %w", err)
	}

	return starlark.None, nil
}

// query executes a prepared query with parameters.
func (ps *preparedStmt) query(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var params starlark.Sequence

	if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
		"params?", &params); err != nil {
		return nil, err
	}

	// Convert parameters to Go values
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

	// Execute the query
	rows, err := ps.stmt.Query(goParams...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	// Get column names
	cols, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("failed to get column names: %w", err)
	}

	// Convert result rows to a Starlark list of dicts
	resultList := &starlark.List{}
	for rows.Next() {
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

// queryOne executes a prepared query and returns the first row, or None if no rows are returned.
func (ps *preparedStmt) queryOne(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var params starlark.Sequence

	if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
		"params?", &params); err != nil {
		return nil, err
	}

	// Convert parameters to Go values
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

	// Execute the query
	rows, err := ps.stmt.Query(goParams...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
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

	// Check for errors after iteration
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error during query iteration: %w", err)
	}

	return rowDict, nil
}
