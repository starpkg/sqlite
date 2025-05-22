package sqlite

import (
	"fmt"
	"strings"

	"go.starlark.net/starlark"
)

// createTable creates a new table with the specified columns.
func (m *databaseMethods) createTable(_ *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
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
func (m *databaseMethods) dropTable(_ *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
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
func (m *databaseMethods) truncateTable(_ *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
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
func (m *databaseMethods) insert(_ *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
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
		columns = append(columns, quoteName(string(colName))) // Quote column name

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
		strings.Join(columns, ", "), // Already quoted
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
func (m *databaseMethods) insertMany(_ *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
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
	firstRowVal := valuesList.Index(0)

	firstRow, ok := firstRowVal.(*starlark.Dict)
	if !ok {
		return nil, fmt.Errorf("values must be a list of dictionaries")
	}

	// Extract column names from first row
	var originalColumnNames []string // Store original (unquoted) column names
	var quotedColumnNames []string   // Store quoted column names for SQL
	for _, tuple := range firstRow.Items() {
		colName, ok := tuple.Index(0).(starlark.String)
		if !ok {
			return nil, fmt.Errorf("column name must be a string")
		}
		sColName := string(colName)
		originalColumnNames = append(originalColumnNames, sColName)
		quotedColumnNames = append(quotedColumnNames, quoteName(sColName))
	}

	// Begin transaction
	tx, err := m.db.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}

	// Build SQL statement
	placeholders := strings.Repeat("?, ", len(quotedColumnNames)) // Use count of columns
	placeholders = placeholders[:len(placeholders)-2]             // Remove trailing ", "

	sql := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		quoteName(table),
		strings.Join(quotedColumnNames, ", "), // Use quoted names for SQL
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
		rowVal := valuesList.Index(i)

		row, ok := rowVal.(*starlark.Dict)
		if !ok {
			tx.Rollback()
			return nil, fmt.Errorf("values must be a list of dictionaries")
		}

		// Extract values in the same order as originalColumnNames
		var params []interface{}
		for _, originalCol := range originalColumnNames { // Iterate using original names for lookup
			val, found, err := row.Get(starlark.String(originalCol)) // Use original name for Get
			if err != nil {
				tx.Rollback()
				return nil, err
			}
			if !found {
				tx.Rollback()
				return nil, fmt.Errorf("column %s missing in row %d", originalCol, i)
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
func (m *databaseMethods) update(_ *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var table string
	var values *starlark.Dict
	var whereVal starlark.Value

	if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
		"table", &table,
		"values", &values,
		"where?", &whereVal); err != nil {
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
		setClauses = append(setClauses, fmt.Sprintf("%s = ?", quoteName(string(colName)))) // Quote column name

		// Add parameter
		val, err := starlarkToSQLiteValue(tuple.Index(1))
		if err != nil {
			return nil, err
		}
		params = append(params, val)
	}

	// Build SQL statement
	sql := fmt.Sprintf("UPDATE %s SET %s",
		quoteName(table),
		strings.Join(setClauses, ", ")) // Already quoted

	// Parse where clause and parameters
	whereClause, whereParams, err := parseWhereClause(whereVal)
	if err != nil {
		return nil, err
	}

	// Add WHERE clause if provided
	if whereClause != "" {
		sql += " WHERE " + whereClause
		// Add where clause parameters to the param list
		params = append(params, whereParams...)
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
func (m *databaseMethods) upsert(_ *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var table string
	var values *starlark.Dict
	var keyColumnsVal starlark.Value // Changed from keyColumns *starlark.List

	if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
		"table", &table,
		"values", &values,
		"keys", &keyColumnsVal); err != nil { // Changed to keyColumnsVal
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
		columns = append(columns, quoteName(string(colName))) // Quote column name here

		// Add placeholder and value
		placeholders = append(placeholders, "?")
		val, err := starlarkToSQLiteValue(tuple.Index(1))
		if err != nil {
			return nil, err
		}
		params = append(params, val)

		// Add update clause
		updateClauses = append(updateClauses, fmt.Sprintf("%s = excluded.%s",
			quoteName(string(colName)), quoteName(string(colName)))) // Quote column names here
	}

	// Extract key columns using extractColumns
	conflictTarget, err := extractColumns(keyColumnsVal)
	if err != nil {
		return nil, fmt.Errorf("failed to extract key columns: %w", err)
	}

	if len(conflictTarget) == 0 {
		return nil, fmt.Errorf("at least one key column must be specified for upsert")
	}

	// Build SQL statement with UPSERT syntax (INSERT OR REPLACE)
	// Column names in columns, conflictTarget, and updateClauses are already quoted
	sql := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s) ON CONFLICT(%s) DO UPDATE SET %s",
		quoteName(table),
		strings.Join(columns, ", "), // Already quoted
		strings.Join(placeholders, ", "),
		strings.Join(quoteNameList(conflictTarget), ", "), // Use quoteNameList for conflict target
		strings.Join(updateClauses, ", "))                 // Already quoted

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
func (m *databaseMethods) delete(_ *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var table string
	var whereVal starlark.Value

	if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
		"table", &table,
		"where?", &whereVal); err != nil {
		return nil, err
	}

	// Build SQL statement
	sql := fmt.Sprintf("DELETE FROM %s", quoteName(table))

	// Parse where clause and parameters
	whereClause, params, err := parseWhereClause(whereVal)
	if err != nil {
		return nil, err
	}

	// Add WHERE clause if provided
	if whereClause != "" {
		sql += " WHERE " + whereClause
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

// extractColumns extracts column names from a Starlark value,
// which can be either a string, bytes, or a sequence of strings/bytes.
func extractColumns(columnsVal starlark.Value) ([]string, error) {
	var colNames []string

	switch val := columnsVal.(type) {
	case nil, starlark.NoneType:
		// Return empty list for nil or None
		return colNames, nil

	case starlark.String:
		// Handle single string case
		colNames = append(colNames, string(val))
		return colNames, nil

	case starlark.Bytes:
		// Handle single bytes case
		colNames = append(colNames, string(val))
		return colNames, nil

	case starlark.Sequence:
		// Handle sequence case
		iter := val.Iterate()
		defer iter.Done()
		var item starlark.Value
		for iter.Next(&item) {
			switch v := item.(type) {
			case starlark.String:
				colNames = append(colNames, string(v))
			case starlark.Bytes:
				colNames = append(colNames, string(v))
			default:
				return nil, fmt.Errorf("column name must be a string or bytes, got %s", item.Type())
			}
		}
		return colNames, nil
	}

	return nil, fmt.Errorf("columns must be a string, bytes, or sequence of strings/bytes, got %s", columnsVal.Type())
}

// parseWhereClause parses a where clause from a Starlark value.
// The value can be a string or bytes (just the clause) or a sequence where the first
// element is the clause and the rest are parameters.
func parseWhereClause(whereVal starlark.Value) (string, []interface{}, error) {
	switch val := whereVal.(type) {
	case nil, starlark.NoneType:
		// Handle nil case
		return "", nil, nil

	case starlark.String:
		// Handle string case - just the clause, no params
		return string(val), nil, nil

	case starlark.Bytes:
		// Handle bytes case - just the clause, no params
		return string(val), nil, nil

	case starlark.Sequence:
		// Handle sequence case
		if val.Len() == 0 {
			return "", nil, nil
		}

		// Get the first item as the clause
		var clause starlark.Value
		iter := val.Iterate()
		defer iter.Done()
		if !iter.Next(&clause) {
			return "", nil, nil
		}

		// Convert first item to string
		var whereClause string
		switch c := clause.(type) {
		case starlark.String:
			whereClause = string(c)
		case starlark.Bytes:
			whereClause = string(c)
		default:
			return "", nil, fmt.Errorf("where clause must be a string or bytes, got %s", clause.Type())
		}

		// Get remaining items as parameters
		var params []interface{}
		for iter.Next(&clause) {
			sqlVal, err := starlarkToSQLiteValue(clause)
			if err != nil {
				return "", nil, err
			}
			params = append(params, sqlVal)
		}

		return whereClause, params, nil
	default:
		// Handle invalid type
		return "", nil, fmt.Errorf("where must be a string, bytes, or sequence, got %s", whereVal.Type())
	}
}

// quoteNameList quotes a list of names.
func quoteNameList(names []string) []string {
	quotedNames := make([]string, len(names))
	for i, name := range names {
		quotedNames[i] = quoteName(name)
	}
	return quotedNames
}

// selectRecords selects records from a table.
func (m *databaseMethods) selectRecords(_ *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var table string
	var columnsVal starlark.Value
	var whereVal starlark.Value
	var orderBy string
	var limit int
	var offset int

	if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
		"table", &table,
		"columns?", &columnsVal,
		"where?", &whereVal,
		"order_by?", &orderBy,
		"limit?", &limit,
		"offset?", &offset); err != nil {
		return nil, err
	}

	// Extract column names
	colNames, err := extractColumns(columnsVal)
	if err != nil {
		return nil, err
	}

	// Use * if no columns specified
	var colClause string
	if len(colNames) == 0 || (len(colNames) == 1 && colNames[0] == "*") {
		colClause = "*"
	} else {
		// Quote column names for security
		quotedColumns := make([]string, len(colNames))
		for i, col := range colNames {
			quotedColumns[i] = quoteName(col)
		}
		colClause = strings.Join(quotedColumns, ", ")
	}

	// Build SQL statement
	sql := fmt.Sprintf("SELECT %s FROM %s", colClause, quoteName(table))

	// Parse where clause and parameters
	whereClause, params, err := parseWhereClause(whereVal)
	if err != nil {
		return nil, err
	}

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
func (m *databaseMethods) count(_ *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var table string
	var whereVal starlark.Value

	if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
		"table", &table,
		"where?", &whereVal); err != nil {
		return nil, err
	}

	// Build SQL statement
	sql := fmt.Sprintf("SELECT COUNT(*) FROM %s", quoteName(table))

	// Parse where clause and parameters
	whereClause, params, err := parseWhereClause(whereVal)
	if err != nil {
		return nil, err
	}

	// Add WHERE clause if provided
	if whereClause != "" {
		sql += " WHERE " + whereClause
	}

	// Execute the query
	var count int64
	if err := m.db.db.QueryRow(sql, params...).Scan(&count); err != nil {
		return nil, fmt.Errorf("failed to count records: %w", err)
	}

	return starlark.MakeInt64(count), nil
}
