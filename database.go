package sqlite

import (
	"database/sql"
	"fmt"

	"github.com/1set/starlet/dataconv"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

// ============================================================================
// Database Instance and Creation
// ============================================================================

// database represents a SQLite database connection.
// All database operations are performed via methods on *database.
type database struct {
	db *sql.DB
}

// newDatabaseInstance creates a new Starlark database instance.
func newDatabaseInstance(db *sql.DB) *starlarkstruct.Module {
	dbi := &database{db: db}

	// Create dictionary of methods
	dict := starlark.StringDict{
		// Basic operations
		"close":     starlark.NewBuiltin("close", dbi.close),
		"execute":   starlark.NewBuiltin("execute", dbi.execute),
		"batch":     starlark.NewBuiltin("batch", dbi.batch),
		"query":     starlark.NewBuiltin("query", dbi.query),
		"query_one": starlark.NewBuiltin("query_one", dbi.queryOne),

		// Prepared statements
		"prepare":       starlark.NewBuiltin("prepare", dbi.prepare),
		"prepare_query": starlark.NewBuiltin("prepare_query", dbi.prepareQuery),

		// Transactions
		"begin": starlark.NewBuiltin("begin", dbi.begin),

		// Schema operations
		"create_table":   starlark.NewBuiltin("create_table", dbi.createTable),
		"drop_table":     starlark.NewBuiltin("drop_table", dbi.dropTable),
		"table_exists":   starlark.NewBuiltin("table_exists", dbi.tableExists),
		"truncate_table": starlark.NewBuiltin("truncate_table", dbi.truncateTable),

		// Data operations
		"insert":      starlark.NewBuiltin("insert", dbi.insert),
		"insert_many": starlark.NewBuiltin("insert_many", dbi.insertMany),
		"update":      starlark.NewBuiltin("update", dbi.update),
		"upsert":      starlark.NewBuiltin("upsert", dbi.upsert),
		"delete":      starlark.NewBuiltin("delete", dbi.delete),
		"select":      starlark.NewBuiltin("select", dbi.selectRecords),
		"count":       starlark.NewBuiltin("count", dbi.count),

		// Database management
		"attach": starlark.NewBuiltin("attach", dbi.attach),
		"detach": starlark.NewBuiltin("detach", dbi.detach),

		// Schema introspection
		"tables":     starlark.NewBuiltin("tables", dbi.tables),
		"table_info": starlark.NewBuiltin("table_info", dbi.tableInfo),
		"indices":    starlark.NewBuiltin("indices", dbi.indices),
	}

	return dataconv.MakeModule("database", dict)
}

// ============================================================================
// Basic Database Operations
// ============================================================================

// close closes the database connection.
func (db *database) close(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if err := starlark.UnpackArgs(fn.Name(), args, kwargs); err != nil {
		return nil, err
	}

	if err := db.db.Close(); err != nil {
		return nil, fmt.Errorf("failed to close database: %w", err)
	}

	return starlark.None, nil
}

// execute executes a SQL statement with optional parameters.
func (db *database) execute(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
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

	// Execute the query
	result, err := db.db.Exec(sqlQuery.query, sqlQuery.params...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}

	// Get affected rows
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("failed to get rows affected: %w", err)
	}

	return starlark.MakeInt64(rowsAffected), nil
}

// batch executes multiple SQL statements in a single transaction.
func (db *database) batch(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var queries *starlark.List

	if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
		"queries", &queries); err != nil {
		return nil, err
	}

	// Begin transaction
	tx, err := db.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}

	// Prepare result list
	resultList := &starlark.List{}

	// Execute each query
	for i := 0; i < queries.Len(); i++ {
		queryItem := queries.Index(i)

		var query string
		var params starlark.Sequence

		// Parse query item - can be string or [query, params]
		switch item := queryItem.(type) {
		case starlark.String:
			// Simple string query without parameters
			query = string(item)
			params = nil

		case starlark.Sequence:
			// Sequence with [query, params]
			if item.Len() < 1 {
				tx.Rollback()
				return nil, fmt.Errorf("query %d: empty sequence", i)
			}

			// Get query string
			var queryVal starlark.Value
			var paramsVal starlark.Value

			// Handle both List and Tuple sequences
			switch seq := item.(type) {
			case *starlark.List:
				queryVal = seq.Index(0)
				if seq.Len() > 1 {
					paramsVal = seq.Index(1)
				}
			case starlark.Tuple:
				queryVal = seq.Index(0)
				if seq.Len() > 1 {
					paramsVal = seq.Index(1)
				}
			default:
				tx.Rollback()
				return nil, fmt.Errorf("query %d: unsupported sequence type", i)
			}

			queryStr, ok := queryVal.(starlark.String)
			if !ok {
				tx.Rollback()
				return nil, fmt.Errorf("query %d: first element must be a string", i)
			}
			query = string(queryStr)

			// Get parameters if present
			if paramsVal != nil {
				if paramsSeq, ok := paramsVal.(starlark.Sequence); ok {
					params = paramsSeq
				} else {
					tx.Rollback()
					return nil, fmt.Errorf("query %d: second element must be a sequence of parameters", i)
				}
			}

		default:
			tx.Rollback()
			return nil, fmt.Errorf("query %d: must be a string or sequence [query, params]", i)
		}

		// Parse query and parameters
		sqlQuery, err := newSQLQuery(query, params)
		if err != nil {
			tx.Rollback()
			return nil, fmt.Errorf("query %d: %w", i, err)
		}

		// Execute the query within transaction
		result, err := tx.Exec(sqlQuery.query, sqlQuery.params...)
		if err != nil {
			tx.Rollback()
			return nil, fmt.Errorf("query %d: failed to execute: %w", i, err)
		}

		// Get affected rows
		rowsAffected, err := result.RowsAffected()
		if err != nil {
			tx.Rollback()
			return nil, fmt.Errorf("query %d: failed to get rows affected: %w", i, err)
		}

		// Add result to list
		if err := resultList.Append(starlark.MakeInt64(rowsAffected)); err != nil {
			tx.Rollback()
			return nil, err
		}
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return resultList, nil
}

// ============================================================================
// Query Operations
// ============================================================================

// query executes a SQL query with optional parameters and returns the results.
func (db *database) query(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
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

	// Execute the query
	rows, err := db.db.Query(sqlQuery.query, sqlQuery.params...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}

	// Use shared utility to process rows
	return processQueryRows(rows)
}

// queryOne executes a SQL query and returns the first row, or None if no rows are returned.
func (db *database) queryOne(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
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

	// Execute the query
	rows, err := db.db.Query(sqlQuery.query, sqlQuery.params...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}

	// Use shared utility to process first row
	return processQueryOneRow(rows)
}

// begin starts a new transaction.
func (db *database) begin(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if err := starlark.UnpackArgs(fn.Name(), args, kwargs); err != nil {
		return nil, err
	}

	// Begin transaction
	tx, err := db.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}

	// Create transaction object
	return newTransactionInstance(tx), nil
}

// attach attaches another database with an alias.
func (db *database) attach(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var database string
	var alias string

	if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
		"database", &database,
		"alias", &alias); err != nil {
		return nil, err
	}

	// Execute ATTACH DATABASE statement
	query := fmt.Sprintf("ATTACH DATABASE ? AS %s", quoteName(alias))
	_, err := db.db.Exec(query, database)
	if err != nil {
		return nil, fmt.Errorf("failed to attach database: %w", err)
	}

	return starlark.None, nil
}

// detach detaches a database.
func (db *database) detach(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var alias string

	if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
		"alias", &alias); err != nil {
		return nil, err
	}

	// Execute DETACH DATABASE statement
	query := fmt.Sprintf("DETACH DATABASE %s", quoteName(alias))
	_, err := db.db.Exec(query)
	if err != nil {
		return nil, fmt.Errorf("failed to detach database: %w", err)
	}

	return starlark.None, nil
}

// tables returns a list of tables in the database.
func (db *database) tables(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if err := starlark.UnpackArgs(fn.Name(), args, kwargs); err != nil {
		return nil, err
	}

	// Query tables
	rows, err := db.db.Query("SELECT name FROM sqlite_master WHERE type='table' ORDER BY name")
	if err != nil {
		return nil, fmt.Errorf("failed to query tables: %w", err)
	}
	defer rows.Close()

	// Create result list
	resultList := &starlark.List{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("failed to scan table name: %w", err)
		}
		if err := resultList.Append(starlark.String(name)); err != nil {
			return nil, err
		}
	}

	// Check for errors after iteration
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error during query iteration: %w", err)
	}

	return resultList, nil
}

// tableInfo returns information about a table's columns.
func (db *database) tableInfo(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var table string

	if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
		"table", &table); err != nil {
		return nil, err
	}

	// Query table info
	rows, err := db.db.Query(fmt.Sprintf("PRAGMA table_info(%s)", quoteName(table)))
	if err != nil {
		return nil, fmt.Errorf("failed to query table info: %w", err)
	}
	defer rows.Close()

	// Create result list
	resultList := &starlark.List{}
	for rows.Next() {
		var cid int
		var name string
		var typeName string
		var notNull int
		var dfltValue interface{}
		var pk int

		if err := rows.Scan(&cid, &name, &typeName, &notNull, &dfltValue, &pk); err != nil {
			return nil, fmt.Errorf("failed to scan column info: %w", err)
		}

		// Create column info dict
		colDict := starlark.NewDict(6)
		colDict.SetKey(starlark.String("cid"), starlark.MakeInt(cid))
		colDict.SetKey(starlark.String("name"), starlark.String(name))
		colDict.SetKey(starlark.String("type"), starlark.String(typeName))
		colDict.SetKey(starlark.String("notnull"), starlark.MakeInt(notNull))

		// Handle default value
		if dfltValue == nil {
			colDict.SetKey(starlark.String("dflt_value"), starlark.None)
		} else {
			dfltStr, ok := dfltValue.(string)
			if ok {
				colDict.SetKey(starlark.String("dflt_value"), starlark.String(dfltStr))
			} else {
				colDict.SetKey(starlark.String("dflt_value"), starlark.String(fmt.Sprint(dfltValue)))
			}
		}

		colDict.SetKey(starlark.String("pk"), starlark.MakeInt(pk))

		// Append to result list
		if err := resultList.Append(colDict); err != nil {
			return nil, err
		}
	}

	// Check for errors after iteration
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error during query iteration: %w", err)
	}

	return resultList, nil
}

// indices returns a list of indices for a table.
func (db *database) indices(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var table string

	if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
		"table", &table); err != nil {
		return nil, err
	}

	// Query indices
	rows, err := db.db.Query("SELECT name, sql FROM sqlite_master WHERE type='index' AND tbl_name=?", table)
	if err != nil {
		return nil, fmt.Errorf("failed to query indices: %w", err)
	}
	defer rows.Close()

	// Create result list
	resultList := &starlark.List{}
	for rows.Next() {
		var name string
		var sqlStmt string
		if err := rows.Scan(&name, &sqlStmt); err != nil {
			return nil, fmt.Errorf("failed to scan index info: %w", err)
		}

		// Create index info dict
		idxDict := starlark.NewDict(2)
		idxDict.SetKey(starlark.String("name"), starlark.String(name))
		idxDict.SetKey(starlark.String("sql"), starlark.String(sqlStmt))

		// Append to result list
		if err := resultList.Append(idxDict); err != nil {
			return nil, err
		}
	}

	// Check for errors after iteration
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error during query iteration: %w", err)
	}

	return resultList, nil
}

// tableExists checks if a table exists.
func (db *database) tableExists(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var table string

	if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
		"table", &table); err != nil {
		return nil, err
	}

	// Query to check if table exists
	var count int
	err := db.db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&count)
	if err != nil {
		return nil, fmt.Errorf("failed to check if table exists: %w", err)
	}

	return starlark.Bool(count > 0), nil
}

// prepare prepares a SQL statement.
func (db *database) prepare(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var query string

	if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
		"query", &query); err != nil {
		return nil, err
	}

	// Prepare statement
	stmt, err := db.db.Prepare(query)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare statement: %w", err)
	}

	// Create prepared statement object
	return newPreparedStatementInstance(stmt), nil
}

// prepareQuery prepares a SQL query statement.
func (db *database) prepareQuery(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var query string

	if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
		"query", &query); err != nil {
		return nil, err
	}

	// Prepare statement
	stmt, err := db.db.Prepare(query)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare query: %w", err)
	}

	// Create prepared query object
	return newPreparedQueryInstance(stmt), nil
}
