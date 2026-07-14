package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/1set/starlet/dataconv"
	"github.com/starpkg/base/util"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

// ============================================================================
// Prepared Statement Instance and Creation
// ============================================================================

// preparedStmt represents a prepared statement struct with methods
// directly attached, providing cleaner and more idiomatic Go code.
type preparedStmt struct {
	stmt      *sql.Stmt
	maxRows   int
	opTimeout time.Duration
}

// opContext derives the context bounding a single operation from the calling
// script thread plus the configured per-operation timeout. The caller must
// invoke the returned cancel func.
func (ps *preparedStmt) opContext(thread *starlark.Thread) (context.Context, context.CancelFunc) {
	return util.OpContext(thread, ps.opTimeout)
}

// newPreparedStatementInstance creates a new Starlark prepared statement instance.
func newPreparedStatementInstance(stmt *sql.Stmt, maxRows int, opTimeout time.Duration) *starlarkstruct.Module {
	ps := &preparedStmt{stmt: stmt, maxRows: maxRows, opTimeout: opTimeout}

	// Create dictionary of methods
	dict := starlark.StringDict{
		"execute": starlark.NewBuiltin("execute", ps.execute),
		"close":   starlark.NewBuiltin("close", ps.close),
	}

	return dataconv.MakeModule("prepared_statement", dict)
}

// newPreparedQueryInstance creates a new Starlark prepared query instance.
func newPreparedQueryInstance(stmt *sql.Stmt, maxRows int, opTimeout time.Duration) *starlarkstruct.Module {
	ps := &preparedStmt{stmt: stmt, maxRows: maxRows, opTimeout: opTimeout}

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
	ctx, cancel := ps.opContext(thread)
	defer cancel()
	result, err := ps.stmt.ExecContext(ctx, goParams...)
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
	ctx, cancel := ps.opContext(thread)
	defer cancel()
	rows, err := ps.stmt.QueryContext(ctx, goParams...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}

	// Use shared utility to process rows
	return processQueryRows(rows, ps.maxRows)
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
	ctx, cancel := ps.opContext(thread)
	defer cancel()
	rows, err := ps.stmt.QueryContext(ctx, goParams...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}

	// Use shared utility to process first row
	return processQueryOneRow(rows)
}
