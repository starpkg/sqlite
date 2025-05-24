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
	goParams, err := convertStarlarkParams(params)
	if err != nil {
		return nil, err
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
	goParams, err := convertStarlarkParams(params)
	if err != nil {
		return nil, err
	}

	// Execute the query
	rows, err := ps.stmt.Query(goParams...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}

	// Use shared utility to process rows
	return processQueryRows(rows)
}

// queryOne executes a prepared query and returns the first row, or None if no rows are returned.
func (ps *preparedStmt) queryOne(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var params starlark.Sequence

	if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
		"params?", &params); err != nil {
		return nil, err
	}

	// Convert parameters to Go values
	goParams, err := convertStarlarkParams(params)
	if err != nil {
		return nil, err
	}

	// Execute the query
	rows, err := ps.stmt.Query(goParams...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}

	// Use shared utility to process first row
	return processQueryOneRow(rows)
}
