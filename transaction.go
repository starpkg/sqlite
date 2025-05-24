package sqlite

import (
	"database/sql"
	"fmt"

	"github.com/1set/starlet/dataconv"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

// transaction represents a database transaction.
type transaction struct {
	tx *sql.Tx
}

// newTransactionInstance creates a new Starlark transaction instance.
func newTransactionInstance(tx *sql.Tx) *starlarkstruct.Module {
	txObj := &transaction{tx: tx}

	// Create dictionary of methods
	dict := starlark.StringDict{
		"execute":   starlark.NewBuiltin("execute", txObj.execute),
		"query":     starlark.NewBuiltin("query", txObj.query),
		"query_one": starlark.NewBuiltin("query_one", txObj.queryOne),
		"commit":    starlark.NewBuiltin("commit", txObj.commit),
		"rollback":  starlark.NewBuiltin("rollback", txObj.rollback),
	}

	return dataconv.MakeModule("transaction", dict)
}

// execute executes a SQL statement within the transaction.
func (tx *transaction) execute(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var query string
	var params starlark.Sequence

	if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
		"query", &query,
		"params?", &params); err != nil {
		return nil, err
	}

	// Parse query and parameters
	sqlQuery, err := newSQLQuery(query, params)
	if err != nil {
		return nil, err
	}

	// Execute the query within transaction
	result, err := tx.tx.Exec(sqlQuery.query, sqlQuery.params...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute transaction query: %w", err)
	}

	// Get affected rows
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("failed to get rows affected: %w", err)
	}

	return starlark.MakeInt64(rowsAffected), nil
}

// query executes a SQL query within the transaction.
func (tx *transaction) query(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var query string
	var params starlark.Sequence

	if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
		"query", &query,
		"params?", &params); err != nil {
		return nil, err
	}

	// Parse query and parameters
	sqlQuery, err := newSQLQuery(query, params)
	if err != nil {
		return nil, err
	}

	// Execute the query within transaction
	rows, err := tx.tx.Query(sqlQuery.query, sqlQuery.params...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute transaction query: %w", err)
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

// queryOne executes a SQL query and returns the first row, or None if no rows are returned.
func (tx *transaction) queryOne(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var query string
	var params starlark.Sequence

	if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
		"query", &query,
		"params?", &params); err != nil {
		return nil, err
	}

	// Parse query and parameters
	sqlQuery, err := newSQLQuery(query, params)
	if err != nil {
		return nil, err
	}

	// Execute the query within transaction
	rows, err := tx.tx.Query(sqlQuery.query, sqlQuery.params...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute transaction query: %w", err)
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

// commit commits the transaction.
func (tx *transaction) commit(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if err := starlark.UnpackArgs(fn.Name(), args, kwargs); err != nil {
		return nil, err
	}

	if err := tx.tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return starlark.None, nil
}

// rollback rolls back the transaction.
func (tx *transaction) rollback(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if err := starlark.UnpackArgs(fn.Name(), args, kwargs); err != nil {
		return nil, err
	}

	if err := tx.tx.Rollback(); err != nil {
		return nil, fmt.Errorf("failed to rollback transaction: %w", err)
	}

	return starlark.None, nil
}
