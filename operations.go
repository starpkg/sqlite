package sqlite

import (
	"database/sql"
	"fmt"
	"strings"

	"go.starlark.net/starlark"
)

// createTable creates a new table with the specified columns, optional constraints, and indexes.
func (db *database) createTable(_ *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var table string
	var columns *starlark.Dict
	var constraintsVal starlark.Value
	var indexesVal starlark.Value

	if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
		"table", &table,
		"columns", &columns,
		"constraints?", &constraintsVal,
		"indexes?", &indexesVal); err != nil {
		return nil, err
	}

	// Begin transaction for atomicity
	tx, err := db.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}

	// Build CREATE TABLE statement
	columnDefs, err := buildColumnDefinitions(columns)
	if err != nil {
		tx.Rollback()
		return nil, err
	}

	// Process table-level constraints
	tableConstraints, err := processTableConstraints(constraintsVal)
	if err != nil {
		tx.Rollback()
		return nil, err
	}

	// Combine column definitions and table constraints
	var allDefinitions []string
	allDefinitions = append(allDefinitions, columnDefs...)
	allDefinitions = append(allDefinitions, tableConstraints...)

	// Create SQL statement
	query := fmt.Sprintf("CREATE TABLE %s (%s)", quoteName(table), strings.Join(allDefinitions, ", "))

	// Execute the CREATE TABLE statement
	_, err = tx.Exec(query)
	if err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("failed to create table: %w", err)
	}

	// Process and create indexes
	err = createTableIndexes(tx, table, indexesVal)
	if err != nil {
		tx.Rollback()
		return nil, err
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return starlark.None, nil
}

// dropTable drops a table.
func (db *database) dropTable(_ *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var table string

	if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
		"table", &table); err != nil {
		return nil, err
	}

	// Create SQL statement
	query := fmt.Sprintf("DROP TABLE %s", quoteName(table))

	// Execute the statement
	_, err := db.db.Exec(query)
	if err != nil {
		return nil, fmt.Errorf("failed to drop table: %w", err)
	}

	return starlark.None, nil
}

// truncateTable removes all rows from a table.
func (db *database) truncateTable(_ *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var table string

	if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
		"table", &table); err != nil {
		return nil, err
	}

	// Create SQL statement
	query := fmt.Sprintf("DELETE FROM %s", quoteName(table))

	// Execute the statement
	result, err := db.db.Exec(query)
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
func (db *database) insert(_ *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
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
		columns = append(columns, quoteName(string(colName)))

		// Add placeholder and value
		placeholders = append(placeholders, "?")
		val, err := starlarkToSQLiteValue(tuple.Index(1))
		if err != nil {
			return nil, err
		}
		params = append(params, val)
	}

	// Build SQL statement
	query := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		quoteName(table),
		strings.Join(columns, ", "),
		strings.Join(placeholders, ", "))

	// Execute the statement
	result, err := db.db.Exec(query, params...)
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
func (db *database) insertMany(_ *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
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
	tx, err := db.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}

	// Build SQL statement
	placeholders := strings.Repeat("?, ", len(quotedColumnNames)) // Use count of columns
	placeholders = placeholders[:len(placeholders)-2]             // Remove trailing ", "

	query := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		quoteName(table),
		strings.Join(quotedColumnNames, ", "), // Use quoted names for SQL
		placeholders)

	// Prepare statement
	stmt, err := tx.Prepare(query)
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
func (db *database) update(_ *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
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
		setClauses = append(setClauses, fmt.Sprintf("%s = ?", quoteName(string(colName))))

		// Add parameter
		val, err := starlarkToSQLiteValue(tuple.Index(1))
		if err != nil {
			return nil, err
		}
		params = append(params, val)
	}

	// Build SQL statement
	query := fmt.Sprintf("UPDATE %s SET %s",
		quoteName(table),
		strings.Join(setClauses, ", "))

	// Parse where clause and parameters
	whereClause, whereParams, err := parseWhereClause(whereVal)
	if err != nil {
		return nil, err
	}

	// Add WHERE clause if provided
	if whereClause != "" {
		query += " WHERE " + whereClause
		// Add where clause parameters to the param list
		params = append(params, whereParams...)
	}

	// Execute the statement
	result, err := db.db.Exec(query, params...)
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
func (db *database) upsert(_ *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var table string
	var values *starlark.Dict
	var keyColumnsVal starlark.Value

	if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
		"table", &table,
		"values", &values,
		"keys", &keyColumnsVal); err != nil {
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
		columns = append(columns, quoteName(string(colName)))

		// Add placeholder and value
		placeholders = append(placeholders, "?")
		val, err := starlarkToSQLiteValue(tuple.Index(1))
		if err != nil {
			return nil, err
		}
		params = append(params, val)

		// Add update clause
		updateClauses = append(updateClauses, fmt.Sprintf("%s = excluded.%s",
			quoteName(string(colName)), quoteName(string(colName))))
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
	query := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s) ON CONFLICT(%s) DO UPDATE SET %s",
		quoteName(table),
		strings.Join(columns, ", "),
		strings.Join(placeholders, ", "),
		quoteNames(conflictTarget),
		strings.Join(updateClauses, ", "))

	// Execute the statement
	result, err := db.db.Exec(query, params...)
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
func (db *database) delete(_ *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var table string
	var whereVal starlark.Value

	if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
		"table", &table,
		"where?", &whereVal); err != nil {
		return nil, err
	}

	// Build SQL statement
	query := fmt.Sprintf("DELETE FROM %s", quoteName(table))

	// Parse where clause and parameters
	whereClause, params, err := parseWhereClause(whereVal)
	if err != nil {
		return nil, err
	}

	// Add WHERE clause if provided
	if whereClause != "" {
		query += " WHERE " + whereClause
	}

	// Execute the statement
	result, err := db.db.Exec(query, params...)
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

// selectRecords selects records from a table.
func (db *database) selectRecords(_ *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
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
	query := fmt.Sprintf("SELECT %s FROM %s", colClause, quoteName(table))

	// Parse where clause and parameters
	whereClause, params, err := parseWhereClause(whereVal)
	if err != nil {
		return nil, err
	}

	// Add WHERE clause if provided
	if whereClause != "" {
		query += " WHERE " + whereClause
	}

	// Add ORDER BY clause if provided
	if orderBy != "" {
		query += " ORDER BY " + orderBy
	}

	// Add LIMIT clause if provided
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}

	// Add OFFSET clause if provided
	if offset > 0 {
		query += fmt.Sprintf(" OFFSET %d", offset)
	}

	// Execute the query
	rows, err := db.db.Query(query, params...)
	if err != nil {
		return nil, fmt.Errorf("failed to select records: %w", err)
	}

	// Use shared utility to process rows
	return processQueryRows(rows)
}

// count counts records in a table.
func (db *database) count(_ *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var table string
	var whereVal starlark.Value

	if err := starlark.UnpackArgs(fn.Name(), args, kwargs,
		"table", &table,
		"where?", &whereVal); err != nil {
		return nil, err
	}

	// Build SQL statement
	query := fmt.Sprintf("SELECT COUNT(*) FROM %s", quoteName(table))

	// Parse where clause and parameters
	whereClause, params, err := parseWhereClause(whereVal)
	if err != nil {
		return nil, err
	}

	// Add WHERE clause if provided
	if whereClause != "" {
		query += " WHERE " + whereClause
	}

	// Execute the query
	var count int64
	if err := db.db.QueryRow(query, params...).Scan(&count); err != nil {
		return nil, fmt.Errorf("failed to count records: %w", err)
	}

	return starlark.MakeInt64(count), nil
}

// ============================================================================
// Enhanced Create Table Helper Functions
// ============================================================================

// buildColumnDefinitions processes column definitions from a Starlark dictionary.
// Supports both simple string definitions and structured dictionaries.
func buildColumnDefinitions(columns *starlark.Dict) ([]string, error) {
	var columnDefs []string

	for _, tuple := range columns.Items() {
		colName, ok := tuple.Index(0).(starlark.String)
		if !ok {
			return nil, fmt.Errorf("column name must be a string")
		}

		colNameStr := string(colName)
		colValue := tuple.Index(1)

		// Handle both string and dictionary column definitions
		var colDef string
		var err error

		switch v := colValue.(type) {
		case starlark.String:
			// Simple string definition: "INTEGER PRIMARY KEY"
			colDef = fmt.Sprintf("%s %s", quoteName(colNameStr), string(v))

		case *starlark.Dict:
			// Structured dictionary definition
			colDef, err = buildStructuredColumnDefinition(colNameStr, v)
			if err != nil {
				return nil, fmt.Errorf("failed to build column definition for %s: %w", colNameStr, err)
			}

		default:
			return nil, fmt.Errorf("column definition for %s must be a string or dictionary, got %s", colNameStr, colValue.Type())
		}

		columnDefs = append(columnDefs, colDef)
	}

	return columnDefs, nil
}

// buildStructuredColumnDefinition builds a column definition from a structured dictionary.
func buildStructuredColumnDefinition(colName string, colDict *starlark.Dict) (string, error) {
	// Extract type (required)
	typeVal, found, err := colDict.Get(starlark.String("type"))
	if err != nil {
		return "", err
	}
	if !found {
		return "", fmt.Errorf("column type is required")
	}

	typeStr, ok := typeVal.(starlark.String)
	if !ok {
		return "", fmt.Errorf("column type must be a string")
	}

	// Start building the definition
	def := fmt.Sprintf("%s %s", quoteName(colName), string(typeStr))

	// Process optional attributes in order
	attrs := []struct {
		key    string
		sqlStr string
	}{
		{"primary_key", "PRIMARY KEY"},
		{"autoincrement", "AUTOINCREMENT"},
		{"not_null", "NOT NULL"},
		{"unique", "UNIQUE"},
	}

	for _, attr := range attrs {
		if val, found, err := colDict.Get(starlark.String(attr.key)); err != nil {
			return "", err
		} else if found {
			if boolVal, ok := val.(starlark.Bool); ok && bool(boolVal) {
				def += " " + attr.sqlStr
			}
		}
	}

	// Handle default value
	if defaultVal, found, err := colDict.Get(starlark.String("default")); err != nil {
		return "", err
	} else if found {
		sqlVal, err := starlarkToSQLiteValue(defaultVal)
		if err != nil {
			return "", fmt.Errorf("invalid default value: %w", err)
		}
		def += fmt.Sprintf(" DEFAULT %v", formatSQLValue(sqlVal))
	}

	return def, nil
}

// processTableConstraints processes table-level constraints from a Starlark value.
func processTableConstraints(constraintsVal starlark.Value) ([]string, error) {
	if constraintsVal == nil || constraintsVal == starlark.None {
		return nil, nil
	}

	// Handle list of constraints
	constraintsList, ok := constraintsVal.(*starlark.List)
	if !ok {
		return nil, fmt.Errorf("constraints must be a list of strings")
	}

	var constraints []string
	for i := 0; i < constraintsList.Len(); i++ {
		item := constraintsList.Index(i)
		constraintStr, ok := item.(starlark.String)
		if !ok {
			return nil, fmt.Errorf("constraint %d must be a string", i)
		}
		constraints = append(constraints, string(constraintStr))
	}

	return constraints, nil
}

// createTableIndexes creates indexes for a table from a Starlark value.
func createTableIndexes(tx *sql.Tx, tableName string, indexesVal starlark.Value) error {
	if indexesVal == nil || indexesVal == starlark.None {
		return nil
	}

	// Handle list of indexes
	indexesList, ok := indexesVal.(*starlark.List)
	if !ok {
		return fmt.Errorf("indexes must be a list")
	}

	for i := 0; i < indexesList.Len(); i++ {
		item := indexesList.Index(i)

		var indexSQL string
		var err error

		switch v := item.(type) {
		case starlark.String:
			// Simple column name: "user_id"
			colName := string(v)
			indexName := fmt.Sprintf("idx_%s_%s", tableName, colName)
			indexSQL = fmt.Sprintf("CREATE INDEX %s ON %s (%s)",
				quoteName(indexName), quoteName(tableName), quoteName(colName))

		case *starlark.List:
			// List of column names: ["user_id", "created_at"]
			var columns []string
			for j := 0; j < v.Len(); j++ {
				colItem := v.Index(j)
				colStr, ok := colItem.(starlark.String)
				if !ok {
					return fmt.Errorf("index column %d must be a string", j)
				}
				columns = append(columns, quoteName(string(colStr)))
			}

			if len(columns) == 0 {
				return fmt.Errorf("index must have at least one column")
			}

			indexName := fmt.Sprintf("idx_%s_%s", tableName, strings.Join(columns, "_"))
			// Remove quotes for index name generation
			indexName = strings.ReplaceAll(indexName, `"`, "")
			indexSQL = fmt.Sprintf("CREATE INDEX %s ON %s (%s)",
				quoteName(indexName), quoteName(tableName), strings.Join(columns, ", "))

		default:
			return fmt.Errorf("index %d must be a string (column name) or list of strings (column names)", i)
		}

		// Execute the index creation
		_, err = tx.Exec(indexSQL)
		if err != nil {
			return fmt.Errorf("failed to create index: %w", err)
		}
	}

	return nil
}
