package sqlite

import (
	"fmt"
	"strings"

	"go.starlark.net/starlark"
)

// createTable creates a new table with the specified columns.
func (m *databaseMethods) createTable(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var table string
	var columns *starlark.Dict

	if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
		"table", &table,
		"columns", &columns); err != nil {
		return nil, err
	}

	// Build CREATE TABLE statement
	var columnDefs []string
	for _, tuple := range columns.Items() {
		colName, ok := tuple.Index(0).(starlark.String)
		if !ok {
			return nil, fmt.Errorf("column name must be a string")
		}
		colType, ok := tuple.Index(1).(starlark.String)
		if !ok {
			return nil, fmt.Errorf("column type must be a string")
		}
		columnDefs = append(columnDefs, fmt.Sprintf("%s %s", quoteName(string(colName)), string(colType)))
	}

	// Create SQL statement
	sql := fmt.Sprintf("CREATE TABLE %s (%s)", quoteName(table), strings.Join(columnDefs, ", "))

	// Execute the statement
	_, err := m.db.db.Exec(sql)
	if err != nil {
		return nil, fmt.Errorf("failed to create table: %w", err)
	}

	return starlark.None, nil
}

// dropTable drops a table.
func (m *databaseMethods) dropTable(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var table string

	if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
		"table", &table); err != nil {
		return nil, err
	}

	// Create SQL statement
	sql := fmt.Sprintf("DROP TABLE %s", quoteName(table))

	// Execute the statement
	_, err := m.db.db.Exec(sql)
	if err != nil {
		return nil, fmt.Errorf("failed to drop table: %w", err)
	}

	return starlark.None, nil
}

// truncateTable removes all rows from a table.
func (m *databaseMethods) truncateTable(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var table string

	if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
		"table", &table); err != nil {
		return nil, err
	}

	// Create SQL statement
	sql := fmt.Sprintf("DELETE FROM %s", quoteName(table))

	// Execute the statement
	result, err := m.db.db.Exec(sql)
	if err != nil {
		return nil, fmt.Errorf("failed to truncate table: %w", err)
	}

	// Get affected rows
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("failed to get rows affected: %w", err)
	}

	return starlark.MakeInt64(rowsAffected), nil
}

// insert inserts a record into a table.
func (m *databaseMethods) insert(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var table string
	var values *starlark.Dict

	if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
		"table", &table,
		"values", &values); err != nil {
		return nil, err
	}

	// Extract column names and values
	var columns []string
	var placeholders []string
	var params []interface{}

	for _, tuple := range values.Items() {
		colName, ok := tuple.Index(0).(starlark.String)
		if !ok {
			return nil, fmt.Errorf("column name must be a string")
		}

		// Add column name
		columns = append(columns, string(colName))

		// Add placeholder and value
		placeholders = append(placeholders, "?")
		val, err := starlarkToSQLiteValue(tuple.Index(1))
		if err != nil {
			return nil, err
		}
		params = append(params, val)
	}

	// Build SQL statement
	sql := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		quoteName(table),
		strings.Join(columns, ", "),
		strings.Join(placeholders, ", "))

	// Execute the statement
	result, err := m.db.db.Exec(sql, params...)
	if err != nil {
		return nil, fmt.Errorf("failed to insert record: %w", err)
	}

	// Get last insert ID if available
	lastID, err := result.LastInsertId()
	if err != nil {
		// If LastInsertId not supported, return affected rows
		rowsAffected, err := result.RowsAffected()
		if err != nil {
			return starlark.None, nil
		}
		return starlark.MakeInt64(rowsAffected), nil
	}

	return starlark.MakeInt64(lastID), nil
}

// insertMany inserts multiple records into a table.
func (m *databaseMethods) insertMany(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var table string
	var valuesList *starlark.List

	if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
		"table", &table,
		"values_list", &valuesList); err != nil {
		return nil, err
	}

	// Ensure we have at least one row
	if valuesList.Len() == 0 {
		return starlark.MakeInt(0), nil
	}

	// Get first row to determine columns
	firstRowVal, err := valuesList.Index(0)
	if err != nil {
		return nil, err
	}

	firstRow, ok := firstRowVal.(*starlark.Dict)
	if !ok {
		return nil, fmt.Errorf("values must be a list of dictionaries")
	}

	// Extract column names from first row
	var columns []string
	for _, tuple := range firstRow.Items() {
		colName, ok := tuple.Index(0).(starlark.String)
		if !ok {
			return nil, fmt.Errorf("column name must be a string")
		}
		columns = append(columns, string(colName))
	}

	// Begin transaction
	tx, err := m.db.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}

	// Build SQL statement
	placeholders := strings.Repeat("?, ", len(columns))
	placeholders = placeholders[:len(placeholders)-2] // Remove trailing ", "

	sql := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		quoteName(table),
		strings.Join(columns, ", "),
		placeholders)

	// Prepare statement
	stmt, err := tx.Prepare(sql)
	if err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	// Insert each row
	var totalRows int64
	for i := 0; i < valuesList.Len(); i++ {
		rowVal, err := valuesList.Index(i)
		if err != nil {
			tx.Rollback()
			return nil, err
		}

		row, ok := rowVal.(*starlark.Dict)
		if !ok {
			tx.Rollback()
			return nil, fmt.Errorf("values must be a list of dictionaries")
		}

		// Extract values in the same order as columns
		var params []interface{}
		for _, col := range columns {
			val, found, err := row.Get(starlark.String(col))
			if err != nil {
				tx.Rollback()
				return nil, err
			}
			if !found {
				tx.Rollback()
				return nil, fmt.Errorf("column %s missing in row %d", col, i)
			}
			sqlVal, err := starlarkToSQLiteValue(val)
			if err != nil {
				tx.Rollback()
				return nil, err
			}
			params = append(params, sqlVal)
		}

		// Execute statement
		result, err := stmt.Exec(params...)
		if err != nil {
			tx.Rollback()
			return nil, fmt.Errorf("failed to insert row %d: %w", i, err)
		}

		// Count affected rows
		rowsAffected, err := result.RowsAffected()
		if err == nil {
			totalRows += rowsAffected
		}
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return starlark.MakeInt64(totalRows), nil
}

// update updates records in a table.
func (m *databaseMethods) update(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var table string
	var values *starlark.Dict
	var whereClause string
	var whereParams starlark.Sequence

	if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
		"table", &table,
		"values", &values,
		"where", &whereClause,
		"params?", &whereParams); err != nil {
		return nil, err
	}

	// Extract column names and values
	var setClauses []string
	var params []interface{}

	for _, tuple := range values.Items() {
		colName, ok := tuple.Index(0).(starlark.String)
		if !ok {
			return nil, fmt.Errorf("column name must be a string")
		}

		// Add column name and placeholder
		setClauses = append(setClauses, fmt.Sprintf("%s = ?", string(colName)))

		// Add parameter
		val, err := starlarkToSQLiteValue(tuple.Index(1))
		if err != nil {
			return nil, err
		}
		params = append(params, val)
	}

	// Add where clause parameters
	if whereParams != nil {
		iter := whereParams.Iterate()
		defer iter.Done()
		var val starlark.Value
		for iter.Next(&val) {
			sqlVal, err := starlarkToSQLiteValue(val)
			if err != nil {
				return nil, err
			}
			params = append(params, sqlVal)
		}
	}

	// Build SQL statement
	sql := fmt.Sprintf("UPDATE %s SET %s",
		quoteName(table),
		strings.Join(setClauses, ", "))

	if whereClause != "" {
		sql += " WHERE " + whereClause
	}

	// Execute the statement
	result, err := m.db.db.Exec(sql, params...)
	if err != nil {
		return nil, fmt.Errorf("failed to update records: %w", err)
	}

	// Get affected rows
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("failed to get rows affected: %w", err)
	}

	return starlark.MakeInt64(rowsAffected), nil
}

// upsert inserts a record if it doesn't exist, or updates it if it does.
func (m *databaseMethods) upsert(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var table string
	var values *starlark.Dict
	var keyColumns *starlark.List

	if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
		"table", &table,
		"values", &values,
		"keys", &keyColumns); err != nil {
		return nil, err
	}

	// Extract column names and values
	var columns []string
	var placeholders []string
	var params []interface{}
	var updateClauses []string

	for _, tuple := range values.Items() {
		colName, ok := tuple.Index(0).(starlark.String)
		if !ok {
			return nil, fmt.Errorf("column name must be a string")
		}

		// Add column name
		columns = append(columns, string(colName))

		// Add placeholder and value
		placeholders = append(placeholders, "?")
		val, err := starlarkToSQLiteValue(tuple.Index(1))
		if err != nil {
			return nil, err
		}
		params = append(params, val)

		// Add update clause
		updateClauses = append(updateClauses, fmt.Sprintf("%s = excluded.%s",
			string(colName), string(colName)))
	}

	// Extract key columns
	var conflictTarget []string
	iter := keyColumns.Iterate()
	defer iter.Done()
	var val starlark.Value
	for iter.Next(&val) {
		colName, ok := val.(starlark.String)
		if !ok {
			return nil, fmt.Errorf("key column name must be a string")
		}
		conflictTarget = append(conflictTarget, string(colName))
	}

	if len(conflictTarget) == 0 {
		return nil, fmt.Errorf("at least one key column must be specified")
	}

	// Build SQL statement with UPSERT syntax (INSERT OR REPLACE)
	sql := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s) ON CONFLICT(%s) DO UPDATE SET %s",
		quoteName(table),
		strings.Join(columns, ", "),
		strings.Join(placeholders, ", "),
		strings.Join(conflictTarget, ", "),
		strings.Join(updateClauses, ", "))

	// Execute the statement
	result, err := m.db.db.Exec(sql, params...)
	if err != nil {
		return nil, fmt.Errorf("failed to upsert record: %w", err)
	}

	// Get affected rows
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("failed to get rows affected: %w", err)
	}

	return starlark.MakeInt64(rowsAffected), nil
}

// delete deletes records from a table.
func (m *databaseMethods) delete(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var table string
	var whereClause string
	var whereParams starlark.Sequence

	if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
		"table", &table,
		"where", &whereClause,
		"params?", &whereParams); err != nil {
		return nil, err
	}

	// Build SQL statement
	sql := fmt.Sprintf("DELETE FROM %s", quoteName(table))

	if whereClause != "" {
		sql += " WHERE " + whereClause
	}

	// Extract parameters
	var params []interface{}
	if whereParams != nil {
		iter := whereParams.Iterate()
		defer iter.Done()
		var val starlark.Value
		for iter.Next(&val) {
			sqlVal, err := starlarkToSQLiteValue(val)
			if err != nil {
				return nil, err
			}
			params = append(params, sqlVal)
		}
	}

	// Execute the statement
	result, err := m.db.db.Exec(sql, params...)
	if err != nil {
		return nil, fmt.Errorf("failed to delete records: %w", err)
	}

	// Get affected rows
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("failed to get rows affected: %w", err)
	}

	return starlark.MakeInt64(rowsAffected), nil
}

// selectRecords selects records from a table.
func (m *databaseMethods) selectRecords(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var table string
	var columns *starlark.List
	var whereClause string
	var whereParams starlark.Sequence
	var orderBy string
	var limit int
	var offset int

	if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
		"table", &table,
		"columns", &columns,
		"where?", &whereClause,
		"params?", &whereParams,
		"order_by?", &orderBy,
		"limit?", &limit,
		"offset?", &offset); err != nil {
		return nil, err
	}

	// Extract column names
	var colNames []string
	iter := columns.Iterate()
	defer iter.Done()
	var val starlark.Value
	for iter.Next(&val) {
		colName, ok := val.(starlark.String)
		if !ok {
			return nil, fmt.Errorf("column name must be a string")
		}
		colNames = append(colNames, string(colName))
	}

	// Use * if no columns specified
	var colClause string
	if len(colNames) == 0 || (len(colNames) == 1 && colNames[0] == "*") {
		colClause = "*"
	} else {
		colClause = strings.Join(colNames, ", ")
	}

	// Build SQL statement
	sql := fmt.Sprintf("SELECT %s FROM %s", colClause, quoteName(table))

	// Add WHERE clause if provided
	if whereClause != "" {
		sql += " WHERE " + whereClause
	}

	// Add ORDER BY clause if provided
	if orderBy != "" {
		sql += " ORDER BY " + orderBy
	}

	// Add LIMIT clause if provided
	if limit > 0 {
		sql += fmt.Sprintf(" LIMIT %d", limit)
	}

	// Add OFFSET clause if provided
	if offset > 0 {
		sql += fmt.Sprintf(" OFFSET %d", offset)
	}

	// Extract parameters
	var params []interface{}
	if whereParams != nil {
		iter := whereParams.Iterate()
		defer iter.Done()
		var val starlark.Value
		for iter.Next(&val) {
			sqlVal, err := starlarkToSQLiteValue(val)
			if err != nil {
				return nil, err
			}
			params = append(params, sqlVal)
		}
	}

	// Execute the query
	rows, err := m.db.db.Query(sql, params...)
	if err != nil {
		return nil, fmt.Errorf("failed to select records: %w", err)
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

// count counts records in a table.
func (m *databaseMethods) count(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var table string
	var whereClause string
	var whereParams starlark.Sequence

	if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
		"table", &table,
		"where?", &whereClause,
		"params?", &whereParams); err != nil {
		return nil, err
	}

	// Build SQL statement
	sql := fmt.Sprintf("SELECT COUNT(*) FROM %s", quoteName(table))

	if whereClause != "" {
		sql += " WHERE " + whereClause
	}

	// Extract parameters
	var params []interface{}
	if whereParams != nil {
		iter := whereParams.Iterate()
		defer iter.Done()
		var val starlark.Value
		for iter.Next(&val) {
			sqlVal, err := starlarkToSQLiteValue(val)
			if err != nil {
				return nil, err
			}
			params = append(params, sqlVal)
		}
	}

	// Execute the query
	var count int64
	err := m.db.db.QueryRow(sql, params...).Scan(&count)
	if err != nil {
		return nil, fmt.Errorf("failed to count records: %w", err)
	}

	return starlark.MakeInt64(count), nil
}
