package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"runtime"
	"time"

	"github.com/1set/starlet/dataconv"
	"github.com/starpkg/base/util"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

// ============================================================================
// OperationResult - Represents the result of a database operation
// ============================================================================

// OperationResult represents the result of a database operation that can either
// succeed with a value or fail with an error. This allows graceful error handling
// in Starlark scripts without causing script termination.
//
// OperationResult implements starlark.Value, starlark.HasAttrs, and starlark.Indexable
// to provide a natural interface for error handling in Starlark scripts.
type OperationResult struct {
	value starlark.Value // The actual result value (or starlark.None if error)
	err   error          // The error (or nil if success)
}

// Ensure OperationResult implements required Starlark interfaces
var _ starlark.Value = (*OperationResult)(nil)
var _ starlark.HasAttrs = (*OperationResult)(nil)
var _ starlark.Indexable = (*OperationResult)(nil)

// newSuccessResult creates a new OperationResult for a successful operation.
func newSuccessResult(value starlark.Value) *OperationResult {
	return &OperationResult{
		value: value,
		err:   nil,
	}
}

// newErrorResult creates a new OperationResult for a failed operation.
func newErrorResult(err error) *OperationResult {
	return &OperationResult{
		value: starlark.None,
		err:   err,
	}
}

// Starlark interface implementation for OperationResult

// String returns the string representation of the OperationResult.
func (r *OperationResult) String() string {
	if r.err != nil {
		return fmt.Sprintf("OperationResult(ok=false, error=%q)", r.err.Error())
	}
	return fmt.Sprintf("OperationResult(ok=true, value=%s)", r.value.String())
}

// Type returns the Starlark type name.
func (r *OperationResult) Type() string {
	return "OperationResult"
}

// Freeze makes the OperationResult immutable (required by Starlark interface).
func (r *OperationResult) Freeze() {
	if r.value != nil {
		r.value.Freeze()
	}
}

// Truth returns whether the operation was successful.
func (r *OperationResult) Truth() starlark.Bool {
	return starlark.Bool(r.err == nil)
}

// Hash returns a hash for the OperationResult (required by Starlark interface).
func (r *OperationResult) Hash() (uint32, error) {
	if r.err != nil {
		return 0, fmt.Errorf("unhashable type: %s", r.Type())
	}
	return r.value.Hash()
}

// Attr returns the value of the specified attribute.
func (r *OperationResult) Attr(name string) (starlark.Value, error) {
	switch name {
	case "ok":
		return starlark.Bool(r.err == nil), nil
	case "error":
		if r.err != nil {
			return starlark.String(r.err.Error()), nil
		}
		return starlark.String(""), nil
	case "value":
		return r.value, nil
	default:
		return nil, nil // Attribute not found
	}
}

// AttrNames returns the list of available attributes.
func (r *OperationResult) AttrNames() []string {
	return []string{"ok", "error", "value"}
}

// Len returns the length of the result value if it supports length operations.
func (r *OperationResult) Len() int {
	if r.err != nil {
		return 0 // Error results have no length
	}
	if hasLen, ok := r.value.(starlark.Indexable); ok {
		return hasLen.Len()
	}
	return 0 // Non-indexable values have no length
}

// Index provides indexing support for the result value.
func (r *OperationResult) Index(i int) starlark.Value {
	if r.err != nil {
		return starlark.None // Error results can't be indexed
	}
	if indexable, ok := r.value.(starlark.Indexable); ok {
		return indexable.Index(i)
	}
	return starlark.None // Non-indexable values return None
}

// ============================================================================
// Transaction Implementation
// ============================================================================

// transaction represents a database transaction.
type transaction struct {
	tx        *sql.Tx
	maxRows   int
	opTimeout time.Duration
	// conn is the dedicated connection the transaction runs on. begin() acquires
	// it under a deadline (so acquisition can't block the host forever when the
	// single-connection in-memory pool is busy) and hands it here to be released
	// on finish; the transaction itself then runs under a cancellation-only
	// context that a per-statement deadline could not safely bound.
	conn *sql.Conn
	// cancel releases the transaction-lifetime context created in begin(). It is
	// separate from the per-statement contexts: cancelling it aborts the whole
	// transaction, so it is called only once the transaction ends (commit or
	// rollback), never when an individual statement returns.
	cancel context.CancelFunc
}

// finish aborts the transaction context and returns its connection to the pool
// (idempotent). Cancelling first makes database/sql roll back any still-open
// transaction before the connection is released.
func (tx *transaction) finish() {
	if tx.cancel != nil {
		tx.cancel()
	}
	if tx.conn != nil {
		tx.conn.Close()
		tx.conn = nil
	}
}

// opContext derives the context bounding a single operation from the calling
// script thread plus the configured per-operation timeout. The caller must
// invoke the returned cancel func.
func (tx *transaction) opContext(thread *starlark.Thread) (context.Context, context.CancelFunc) {
	return util.OpContext(thread, tx.opTimeout)
}

// newTransactionInstance creates a new Starlark transaction instance.
func newTransactionInstance(tx *sql.Tx, maxRows int, opTimeout time.Duration, conn *sql.Conn, cancel context.CancelFunc) *starlarkstruct.Module {
	txObj := &transaction{tx: tx, maxRows: maxRows, opTimeout: opTimeout, conn: conn, cancel: cancel}
	// Safety net for a transaction the script begins but never commits/rolls back:
	// the host Machine may run under an uncancelled context.Background(), so
	// without this the lifetime context (and the connection it pins on a
	// single-connection in-memory database) would leak until process exit. When
	// the abandoned object is collected, cancelling its context makes database/sql
	// roll the transaction back and return the connection to the pool.
	runtime.SetFinalizer(txObj, func(t *transaction) { t.finish() })

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

// execute executes a SQL statement within the transaction and returns an OperationResult.
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
		return newErrorResult(err), nil
	}

	// Execute the query within transaction
	ctx, cancel := tx.opContext(thread)
	defer cancel()
	result, err := tx.tx.ExecContext(ctx, sqlQuery.query, sqlQuery.params...)
	if err != nil {
		return newErrorResult(fmt.Errorf("failed to execute transaction query: %w", err)), nil
	}

	// Get affected rows
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return newErrorResult(fmt.Errorf("failed to get rows affected: %w", err)), nil
	}

	return newSuccessResult(starlark.MakeInt64(rowsAffected)), nil
}

// query executes a SQL query within the transaction and returns an OperationResult.
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
		return newErrorResult(err), nil
	}

	// Execute the query within transaction
	ctx, cancel := tx.opContext(thread)
	defer cancel()
	rows, err := tx.tx.QueryContext(ctx, sqlQuery.query, sqlQuery.params...)
	if err != nil {
		return newErrorResult(fmt.Errorf("failed to execute transaction query: %w", err)), nil
	}

	// Use shared utility to process rows
	result, err := processQueryRows(rows, tx.maxRows)
	if err != nil {
		return newErrorResult(err), nil
	}

	return newSuccessResult(result), nil
}

// queryOne executes a SQL query and returns the first row in an OperationResult, or None if no rows are returned.
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
		return newErrorResult(err), nil
	}

	// Execute the query within transaction
	ctx, cancel := tx.opContext(thread)
	defer cancel()
	rows, err := tx.tx.QueryContext(ctx, sqlQuery.query, sqlQuery.params...)
	if err != nil {
		return newErrorResult(fmt.Errorf("failed to execute transaction query: %w", err)), nil
	}

	// Use shared utility to process first row
	result, err := processQueryOneRow(rows)
	if err != nil {
		return newErrorResult(err), nil
	}

	return newSuccessResult(result), nil
}

// commit commits the transaction and returns an OperationResult.
func (tx *transaction) commit(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if err := starlark.UnpackArgs(fn.Name(), args, kwargs); err != nil {
		return nil, err
	}

	defer tx.finish()
	if err := tx.tx.Commit(); err != nil {
		return newErrorResult(fmt.Errorf("failed to commit transaction: %w", err)), nil
	}

	return newSuccessResult(starlark.None), nil
}

// rollback rolls back the transaction.
// Note: This method still uses the old behavior (returns error) because rollback failures
// are rare and usually indicate database corruption where script termination is appropriate.
func (tx *transaction) rollback(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if err := starlark.UnpackArgs(fn.Name(), args, kwargs); err != nil {
		return nil, err
	}

	defer tx.finish()
	if err := tx.tx.Rollback(); err != nil {
		return nil, fmt.Errorf("failed to rollback transaction: %w", err)
	}

	return starlark.None, nil
}
