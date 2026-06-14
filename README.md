# 🗃️ `sqlite` - Effortless SQLite operations in Starlark

[![godoc](https://pkg.go.dev/badge/github.com/starpkg/sqlite.svg)](https://pkg.go.dev/github.com/starpkg/sqlite)
[![license](https://img.shields.io/badge/License-MIT-blue.svg)](https://opensource.org/licenses/MIT)
[![Go Report Card](https://goreportcard.com/badge/github.com/starpkg/sqlite)](https://goreportcard.com/report/github.com/starpkg/sqlite)

A comprehensive Go module that brings the power of SQLite database operations to your Starlark scripts. This module provides both low-level SQL execution capabilities and high-level table management functions, making database interactions intuitive and straightforward while maintaining robust security features.

**Custom SQL functions:** Register SQL functions written in Starlark and call them directly in your SQL queries — see [Custom SQL Functions](#custom-sql-functions).

**Transaction error handling:** Transaction methods return `OperationResult` objects for graceful error handling without script termination — see [Advanced Transaction Error Handling](#advanced-transaction-error-handling).

## Features

- ✅ Low-level SQL execution with prepared statements and parameterized queries
- ✅ Batch operations for executing multiple statements in a single transaction
- ✅ High-level table and record operations for common database tasks
- ✅ Transaction management with begin/commit/rollback support and error handling
- ✅ SQL injection prevention through parameterized queries
- ✅ File-based and in-memory databases with flexible connection options
- ✅ ATTACH/DETACH database support for multi-database operations
- ✅ Schema introspection for table information and indices
- ✅ Custom SQL functions for extending SQLite with Starlark logic
- ✅ Automatic type conversion between SQLite and Starlark types
- ✅ Configurable database settings (journal mode, synchronous mode, etc.)
- ✅ Compatible with Go 1.18+ and cross-platform support

## Installation

```bash
go get github.com/starpkg/sqlite
```

## Quick Start

```go
package main

import (
    "github.com/1set/starlet"
    "github.com/starpkg/sqlite"
)

func main() {
    // Create a new sqlite module
    sqliteModule := sqlite.NewModule()
    
    // Create a Starlet interpreter with the module
    interpreter := starlet.New(
        starlet.WithModuleLoader("sqlite", sqliteModule.LoadModule()),
    )
    
    // Run a Starlark script with SQLite operations
    script := `
load("sqlite", "connect", "register_function")

# Register a custom SQL function (before opening database)
# Note: In production, use unique function names to avoid conflicts
register_function("EXAMPLE_DOUBLE", lambda x: x * 2 if x else 0, num_args=1, deterministic=True)

# Connect to an in-memory database
db = connect(":memory:")

# Create a table
db.execute("""
    CREATE TABLE users (
        id INTEGER PRIMARY KEY,
        name TEXT NOT NULL,
        email TEXT UNIQUE,
        score INTEGER
    )
""")

# Insert data using high-level API
db.insert("users", {"name": "Alice", "email": "alice@example.com", "score": 95})

# Query data using custom function
users = db.query("SELECT name, email, EXAMPLE_DOUBLE(score) as doubled_score FROM users")
for user in users:
    print("User:", user["name"], "Score x2:", user["doubled_score"])

# Close the connection
db.close()
`
    
    // Execute the script
    if err := interpreter.ExecScript("example.star", script); err != nil {
        fmt.Println("Error:", err)
    }
}
```

## Configuration

The `sqlite` module can be configured with the following options (all optional, with sensible defaults):

| Option | Type | Default | Environment Variable | Description |
|--------|------|---------|---------------------|-------------|
| `database` | string | `:memory:` | `SQLITE_DATABASE` | Path to SQLite database (use `:memory:` for in-memory) |
| `timeout` | float | 30.0 | `SQLITE_TIMEOUT` | Connection timeout in seconds |
| `busy_timeout` | float | 5.0 | `SQLITE_BUSY_TIMEOUT` | Busy timeout in seconds |
| `foreign_keys` | bool | true | `SQLITE_FOREIGN_KEYS` | Enable foreign key constraints |
| `journal_mode` | string | `DELETE` | `SQLITE_JOURNAL_MODE` | Journal mode (WAL, DELETE, TRUNCATE, PERSIST, MEMORY, OFF) |
| `synchronous` | string | `FULL` | `SQLITE_SYNCHRONOUS` | Synchronous mode (FULL, NORMAL, OFF) |
| `cache_size` | int | -2000 | `SQLITE_CACHE_SIZE` | Cache size in number of pages (negative = default) |

Module options serve as defaults and will be used when corresponding arguments are not provided to connection functions.

### Module Configuration

```go
// Method 1: Use defaults
module := sqlite.NewModule()

// Method 2: Configure via environment variables
// Set SQLITE_DATABASE, SQLITE_TIMEOUT, SQLITE_FOREIGN_KEYS, etc.
module := sqlite.NewModule()

// Method 3: Configure programmatically (using the base module system)
// See base package documentation for advanced configuration
```

## Starlark API

### Connection Management

#### `connect(database?, timeout?, busy_timeout?, foreign_keys?, journal_mode?, synchronous?, cache_size?)`

Creates a new database connection with optional configuration.

**Parameters:**

- `database` (string): Database path or `:memory:` for in-memory (default: uses module config)
- `timeout` (float): Connection timeout in seconds (default: uses module config)
- `busy_timeout` (float): Busy timeout in seconds (default: uses module config)
- `foreign_keys` (bool): Enable foreign key constraints (default: uses module config)
- `journal_mode` (string): Journal mode (default: uses module config)
- `synchronous` (string): Synchronous mode (default: uses module config)
- `cache_size` (int): Cache size in pages (default: uses module config)

**Returns:** Database object for performing operations

**Example:**

```python
# Connect with defaults
db = connect()

# Connect to a file database with custom settings
db = connect(
    database="myapp.db",
    foreign_keys=True,
    journal_mode="WAL",
    synchronous="NORMAL",
    busy_timeout=10.0
)
```

### Database Object Methods

#### Connection Management

##### `close()`

Closes the database connection.

**Parameters:** None

**Returns:** None

**Example:**

```python
db.close()
```

#### Low-Level SQL Execution

##### `execute(query, params?)`

Executes a SQL statement with optional parameters.

**Parameters:**

- `query` (string): SQL statement to execute
- `params` (list): Optional list of parameters for the query

**Returns:** Number of affected rows (int)

**Example:**

```python
# Create a table
rows_affected = db.execute("""
    CREATE TABLE users (
        id INTEGER PRIMARY KEY,
        name TEXT NOT NULL,
        email TEXT UNIQUE
    )
""")

# Insert with parameters
rows_affected = db.execute(
    "INSERT INTO users (name, email) VALUES (?, ?)",
    ["Alice", "alice@example.com"]
)
```

##### `batch(queries)`

Executes multiple SQL statements in a single transaction.

**Parameters:**

- `queries` (list): List of queries to execute. Each item can be:
  - A string (SQL statement without parameters)
  - A list/tuple with [query, params] (SQL statement with parameters)

**Returns:** List of integers representing affected rows for each query

**Example:**

```python
# Simple batch with string queries
results = db.batch([
    "INSERT INTO users (name, email) VALUES ('Alice', 'alice@example.com')",
    "INSERT INTO users (name, email) VALUES ('Bob', 'bob@example.com')",
    "UPDATE users SET active = 1"
])

# Batch with parameterized queries
results = db.batch([
    ["INSERT INTO users (name, email) VALUES (?, ?)", ["Charlie", "charlie@example.com"]],
    ["UPDATE users SET last_login = ? WHERE id = ?", ["2023-08-15", 1]],
    ["DELETE FROM users WHERE active = ?", [0]]
])

# Mixed batch (some with params, some without)
results = db.batch([
    "CREATE INDEX idx_users_email ON users(email)",
    ["INSERT INTO users (name, email) VALUES (?, ?)", ["David", "david@example.com"]],
    "VACUUM"
])

# All operations are executed in a single transaction
# If any operation fails, the entire batch is rolled back
```

##### `query(query, params?)`

Executes a SQL query and returns all results.

**Parameters:**

- `query` (string): SQL query to execute
- `params` (list): Optional list of parameters for the query

**Returns:** List of dictionaries representing rows

**Example:**

```python
# Query all users
users = db.query("SELECT * FROM users")
for user in users:
    print(user["id"], user["name"], user["email"])

# Query with parameters
adult_users = db.query("SELECT * FROM users WHERE age >= ?", [18])
```

##### `query_one(query, params?)`

Executes a SQL query and returns the first row, or None if no rows are found.

**Parameters:**

- `query` (string): SQL query to execute
- `params` (list): Optional list of parameters for the query

**Returns:** Dictionary representing the first row, or None

**Example:**

```python
# Get a specific user
user = db.query_one("SELECT * FROM users WHERE id = ?", [1])
if user:
    print("Found user:", user["name"])
else:
    print("User not found")
```

#### Prepared Statements

##### `prepare(query)`

Creates a prepared statement for repeated execution.

**Parameters:**

- `query` (string): SQL statement to prepare

**Returns:** Prepared statement object

**Example:**

```python
# Create a prepared statement
stmt = db.prepare("INSERT INTO users (name, email) VALUES (?, ?)")

# Execute multiple times
stmt.execute(["Alice", "alice@example.com"])
stmt.execute(["Bob", "bob@example.com"])
stmt.execute(["Charlie", "charlie@example.com"])

# Close when done
stmt.close()
```

##### `prepare_query(query)`

Creates a prepared query statement for repeated querying.

**Parameters:**

- `query` (string): SQL query to prepare

**Returns:** Prepared query statement object

**Example:**

```python
# Create a prepared query
query_stmt = db.prepare_query("SELECT * FROM users WHERE age > ?")

# Execute with different parameters
young_adults = query_stmt.query([18])
seniors = query_stmt.query([65])

# Close when done
query_stmt.close()
```

#### Prepared Statement Object Methods

##### `execute(params?)`

Executes a prepared statement with optional parameters.

**Parameters:**

- `params` (list): Optional list of parameters

**Returns:** Number of affected rows (int)

##### `query(params?)`

Executes a prepared query statement with optional parameters.

**Parameters:**

- `params` (list): Optional list of parameters

**Returns:** List of dictionaries representing rows

##### `query_one(params?)`

Executes a prepared query statement and returns the first row.

**Parameters:**

- `params` (list): Optional list of parameters

**Returns:** Dictionary representing the first row, or None

##### `close()`

Closes the prepared statement.

**Parameters:** None

**Returns:** None

#### Transaction Management

##### `begin()`

Begins a new transaction.

**Parameters:** None

**Returns:** Transaction object

**Example:**

```python
# Begin a transaction
tx = db.begin()

# Execute operations within the transaction with error handling
update1 = tx.execute("UPDATE accounts SET balance = balance - ? WHERE id = ?", [100, 1])
update2 = tx.execute("UPDATE accounts SET balance = balance + ? WHERE id = ?", [100, 2])

# Check if operations succeeded
if not update1.ok or not update2.ok:
    print("Transaction operations failed:")
    if not update1.ok:
        print("- Update 1 error:", update1.error)
    if not update2.ok:
        print("- Update 2 error:", update2.error)
    tx.rollback()
    fail("Transaction failed")

# Commit the transaction
commit_result = tx.commit()
if not commit_result.ok:
    print("Failed to commit transaction:", commit_result.error)
    fail("Commit failed")

print("Transfer successful")

# Example with validation within transaction:
balance_result = tx.query_one("SELECT balance FROM accounts WHERE id = ?", [1])
if not balance_result.ok:
    tx.rollback()
    fail("Failed to check balance: " + balance_result.error)

if not balance_result.value or balance_result.value["balance"] < 100:
    tx.rollback()
    fail("Insufficient funds for transfer")
```

#### Transaction Object Methods

**Note:** Transaction methods now return `OperationResult` objects for better error handling. Each result has:

- `result.ok` (bool): Whether the operation succeeded
- `result.error` (string): Error message if operation failed
- `result.value`: The actual result value if operation succeeded

##### `execute(query, params?)`

Executes a SQL statement within the transaction.

**Parameters:**

- `query` (string): SQL statement to execute
- `params` (list): Optional list of parameters

**Returns:** `OperationResult` with number of affected rows in `.value` property

##### `query(query, params?)`

Executes a SQL query within the transaction.

**Parameters:**

- `query` (string): SQL query to execute
- `params` (list): Optional list of parameters

**Returns:** `OperationResult` with list of dictionaries representing rows in `.value` property

##### `query_one(query, params?)`

Executes a SQL query within the transaction and returns the first row.

**Parameters:**

- `query` (string): SQL query to execute
- `params` (list): Optional list of parameters

**Returns:** `OperationResult` with dictionary representing the first row (or None) in `.value` property

##### `commit()`

Commits the transaction.

**Parameters:** None

**Returns:** `OperationResult` indicating success or failure

##### `rollback()`

Rolls back the transaction.

**Parameters:** None

**Returns:** None

### High-Level Table Operations

#### Table Management

##### `create_table(table, columns, constraints?, indexes?, exist_ok?)`

Creates a new table with specified column definitions, optional table constraints, and indexes.

**Parameters:**

- `table` (string): Name of the table to create
- `columns` (dict): Dictionary mapping column names to their definitions
- `constraints` (list, optional): List of table-level constraint SQL strings
- `indexes` (list, optional): List of indexes to create
- `exist_ok` (bool, optional): If `True`, do not raise an error if the table already exists (default: `False`)

**Column Definitions:**

Columns can be defined in two ways:

1. **Simple string definition** (backward compatible):

   ```python
   "column_name": "DATA_TYPE CONSTRAINTS"
   ```

2. **Structured dictionary definition**:

   ```python
   "column_name": {
       "type": "DATA_TYPE",           # Required: SQLite data type
       "primary_key": True,           # Optional: PRIMARY KEY constraint
       "autoincrement": True,         # Optional: AUTOINCREMENT (INTEGER PRIMARY KEY only)
       "not_null": True,             # Optional: NOT NULL constraint
       "unique": True,               # Optional: UNIQUE constraint
       "default": "value"            # Optional: DEFAULT value
   }
   ```

**Table Constraints:**

Optional list of table-level constraints as SQL strings:

- `"FOREIGN KEY (column) REFERENCES table(column) ON DELETE CASCADE"`
- `"CHECK (condition)"`
- `"UNIQUE (column1, column2)"`

**Indexes:**

Optional list of indexes to create. Each index can be:

- String: Single column name (e.g., `"column_name"`)
- List: Multiple column names for composite index (e.g., `["col1", "col2"]`)

Index names are auto-generated as `idx_table_column` or `idx_table_col1_col2`.

**Returns:** None

**Examples:**

```python
# Simple string definitions (backward compatible)
db.create_table("users", {
    "id": "INTEGER PRIMARY KEY",
    "name": "TEXT NOT NULL",
    "email": "TEXT UNIQUE"
})

# Structured column definitions
db.create_table("users", {
    "id": {
        "type": "INTEGER",
        "primary_key": True,
        "autoincrement": True
    },
    "username": {
        "type": "TEXT",
        "not_null": True,
        "unique": True
    },
    "email": {
        "type": "TEXT",
        "not_null": True
    },
    "age": {
        "type": "INTEGER",
        "default": 0
    },
    "is_active": {
        "type": "BOOLEAN",
        "default": True
    }
})

# With table constraints and indexes
db.create_table("posts", {
    "id": "INTEGER PRIMARY KEY",
    "user_id": "INTEGER NOT NULL",
    "title": "TEXT NOT NULL",
    "content": "TEXT",
    "created_at": "TEXT DEFAULT CURRENT_TIMESTAMP"
}, constraints=[
    "FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE",
    "CHECK (length(title) > 0)"
], indexes=[
    "user_id",                    # Single column index
    "created_at",                 # Another single column index
    ["user_id", "created_at"]     # Composite index
])

# Mixed definitions (string + structured)
db.create_table("products", {
    "id": "INTEGER PRIMARY KEY AUTOINCREMENT",  # String definition
    "name": {                                   # Structured definition
        "type": "TEXT",
        "not_null": True
    },
    "price": "REAL DEFAULT 0.0",               # String definition
    "category": {                              # Structured definition
        "type": "TEXT",
        "default": "general"
    }
})

# Safe table creation (won't fail if table already exists)
db.create_table("users", {
    "id": "INTEGER PRIMARY KEY",
    "name": "TEXT NOT NULL",
    "email": "TEXT UNIQUE"
}, exist_ok=True)

# Idempotent setup scripts - create tables only if they don't exist
db.create_table("settings", {
    "key": "TEXT PRIMARY KEY",
    "value": "TEXT"
}, constraints=[
    "CHECK (length(key) > 0)"
], indexes=["key"], exist_ok=True)
```

##### `drop_table(table)`

Drops (deletes) a table.

**Parameters:**

- `table` (string): Name of the table to drop

**Returns:** None

**Example:**

```python
db.drop_table("old_products")
```

##### `table_exists(table)`

Checks if a table exists in the database.

**Parameters:**

- `table` (string): Name of the table to check

**Returns:** Boolean indicating if the table exists

**Example:**

```python
if db.table_exists("users"):
    print("Users table exists")
else:
    print("Users table does not exist")
```

##### `truncate_table(table)`

Removes all rows from a table (equivalent to DELETE FROM table).

**Parameters:**

- `table` (string): Name of the table to truncate

**Returns:** Number of rows deleted (int)

**Example:**

```python
deleted_rows = db.truncate_table("temporary_data")
print("Deleted {} rows".format(deleted_rows))
```

#### Record Operations

##### `insert(table, values)`

Inserts a single record into a table.

**Parameters:**

- `table` (string): Name of the table
- `values` (dict): Dictionary mapping column names to values

**Returns:** Last insert ID (int) or number of affected rows if last insert ID is not available

**Example:**

```python
# Insert a user
user_id = db.insert("users", {
    "name": "Alice",
    "email": "alice@example.com",
    "age": 30
})
print("Inserted user with ID:", user_id)
```

##### `insert_many(table, values_list)`

Inserts multiple records into a table in a single transaction.

**Parameters:**

- `table` (string): Name of the table
- `values_list` (list): List of dictionaries, each representing a record to insert

**Returns:** Number of rows inserted (int)

**Example:**

```python
# Insert multiple users
rows_inserted = db.insert_many("users", [
    {"name": "Bob", "email": "bob@example.com", "age": 25},
    {"name": "Charlie", "email": "charlie@example.com", "age": 35},
    {"name": "Diana", "email": "diana@example.com", "age": 28}
])
print("Inserted {} users".format(rows_inserted))
```

##### `update(table, values, where?)`

Updates records in a table.

**Parameters:**

- `table` (string): Name of the table
- `values` (dict): Dictionary mapping column names to new values
- `where` (string, list, or None): Optional where clause. Can be:
  - None: Update all records (use with caution!)
  - String: Simple where clause with no parameters (e.g., "age > 18")
  - List: Where clause with parameters as `[condition, param1, param2, ...]`

**Returns:** Number of rows updated (int)

**Example:**

```python
# Update with simple string condition
db.update("users", {"status": "inactive"}, "age < 18")

# Update with parameterized condition (recommended for user input)
rows_updated = db.update("users", 
    {"age": 31}, 
    ["name = ?", "Alice"]
)

# Update multiple conditions
rows_updated = db.update("products", 
    {"price": 19.99, "on_sale": True}, 
    ["category = ? AND price > ?", "electronics", 20.0]
)
```

##### `upsert(table, values, conflict_columns)`

Inserts a record or updates it if it already exists (based on conflict columns).

**Parameters:**

- `table` (string): Name of the table
- `values` (dict): Dictionary mapping column names to values
- `conflict_columns` (list): List of column names that determine conflicts

**Returns:** Number of rows affected (int)

**Example:**

```python
# Upsert based on email uniqueness
db.upsert("users", 
    {"email": "alice@example.com", "name": "Alice Smith", "age": 31},
    ["email"]
)
```

##### `delete(table, where?)`

Deletes records from a table.

**Parameters:**

- `table` (string): Name of the table
- `where` (string, list, or None): Optional where clause. Can be:
  - None: Delete all records (use with extreme caution!)
  - String: Simple where clause with no parameters (e.g., "age < 18")
  - List: Where clause with parameters as `[condition, param1, param2, ...]`

**Returns:** Number of rows deleted (int)

**Example:**

```python
# Delete with simple string condition
rows_deleted = db.delete("users", "age < 18")

# Delete with parameterized condition (recommended for user input)
rows_deleted = db.delete("users", ["name = ?", "Bob"])

# Delete with multiple conditions
rows_deleted = db.delete("products", 
    ["category = ? AND price < ?", "electronics", 10.0]
)
```

##### `select(table, columns?, where?, order_by?, limit?, offset?)`

Selects records from a table with flexible filtering and sorting options.

**Parameters:**

- `table` (string): Name of the table
- `columns` (string or list): Column names to select, "*" for all, or list of column names
- `where` (string, list, or None): Optional where clause. Can be:
  - None: No filtering
  - String: Simple where clause with no parameters (e.g., "age > 18")
  - List: Where clause with parameters as `[condition, param1, param2, ...]`
- `order_by` (string): Optional ORDER BY clause (e.g., "name ASC", "age DESC")
- `limit` (int): Optional maximum number of rows to return
- `offset` (int): Optional number of rows to skip

**Returns:** List of dictionaries representing the selected rows

**Example:**

```python
# Select all users
users = db.select("users")

# Select with simple string condition
adult_users = db.select("users", 
    ["name", "email"], 
    "age >= 18", 
    order_by="name ASC",
    limit=10
)

# Select with parameterized conditions (recommended for user input)
active_users = db.select("users", 
    "*", 
    ["active = ?", True],
    order_by="created_at DESC",
    limit=20, 
    offset=20
)
```

##### `count(table, where?)`

Counts records in a table with optional filtering.

**Parameters:**

- `table` (string): Name of the table
- `where` (string, list, or None): Optional where clause. Can be:
  - None: Count all records
  - String: Simple where clause with no parameters (e.g., "age > 18")
  - List: Where clause with parameters as `[condition, param1, param2, ...]`

**Returns:** Number of matching records (int)

**Example:**

```python
# Count all users
total_users = db.count("users")

# Count with simple string condition (no parameters)
adult_users = db.count("users", "age >= 18")

# Count with parameterized condition (recommended for user input)
active_users = db.count("users", ["status = ?", "active"])

# Count with multiple conditions
premium_users = db.count("users", 
    ["subscription = ? AND age >= ?", "premium", 18]
)
```

### Multi-Database Operations

##### `attach(database, alias)`

Attaches another database with a specified alias.

**Parameters:**

- `database` (string): Path to the database file to attach
- `alias` (string): Alias name for the attached database

**Returns:** None

**Example:**

```python
# Attach an archive database
db.attach("archive.db", "archive")

# Now you can query from the attached database
old_users = db.query("SELECT * FROM archive.old_users")
```

##### `detach(alias)`

Detaches a previously attached database.

**Parameters:**

- `alias` (string): Alias name of the database to detach

**Returns:** None

**Example:**

```python
# Detach the archive database
db.detach("archive")
```

### Schema Information

##### `tables()`

Returns a list of all tables in the database.

**Parameters:** None

**Returns:** List of table names (list of strings)

**Example:**

```python
tables = db.tables()
print("Tables in database:", tables)
for table in tables:
    print("- {}".format(table))
```

##### `table_info(table)`

Returns detailed information about a table's columns.

**Parameters:**

- `table` (string): Name of the table

**Returns:** List of dictionaries containing column information

Each dictionary contains:

- `cid`: Column ID (int)
- `name`: Column name (string)
- `type`: Column type (string)
- `notnull`: Whether column is NOT NULL (bool)
- `dflt_value`: Default value (or None)
- `pk`: Whether column is part of primary key (bool)

**Example:**

```python
columns = db.table_info("users")
print("Columns in users table:")
for col in columns:
    null_str = "NOT NULL" if col["notnull"] else "NULL"
    pk_str = " (PRIMARY KEY)" if col["pk"] else ""
    print("- {} {} {}{}".format(
        col["name"], col["type"], null_str, pk_str
    ))
```

##### `indices(table)`

Returns information about indices on a table.

**Parameters:**

- `table` (string): Name of the table

**Returns:** List of dictionaries containing index information

**Example:**

```python
indices = db.indices("users")
print("Indices on users table:")
for idx in indices:
    print("- {}".format(idx["name"]))
```

## Type Conversion

The module automatically handles type conversion between SQLite and Starlark:

### SQLite → Starlark

| SQLite Type | Starlark Type | Notes |
|-------------|---------------|-------|
| NULL        | None          | |
| INTEGER     | int           | |
| REAL        | float         | |
| TEXT        | string        | |
| BLOB        | bytes         | |

### Starlark → SQLite

| Starlark Type | SQLite Type | Notes |
|---------------|-------------|-------|
| None          | NULL        | |
| int           | INTEGER     | |
| float         | REAL        | |
| string        | TEXT        | |
| bytes         | BLOB        | |
| bool          | INTEGER     | True→1, False→0 |
| dict          | TEXT        | JSON encoded |
| list          | TEXT        | JSON encoded |

## Custom SQL Functions

The SQLite module supports registering custom SQL functions written in Starlark that can be called from SQL queries. This feature allows you to extend SQLite with domain-specific logic and complex data processing functions.

**⚠️ Critical Requirements**:

- Custom functions **MUST** be registered **BEFORE** opening any database connections. Functions are registered globally with the SQLite driver and affect all connections opened after registration.
- **Use unique function names** to avoid conflicts when multiple modules or tests register functions. Consider using prefixes like `APP_`, `MODULE_`, etc.

### Function Registration

#### `register_function(name, func, num_args=None, deterministic=False)`

Registers a custom SQL function that can be called from SQL queries.

**Parameters:**

- `name` (string): The name of the SQL function to register (case-insensitive in SQL)
- `func` (callable): A Starlark function or lambda that implements the custom logic
- `num_args` (int, optional): Number of arguments the function accepts
  - If `None` or not specified: Function is variadic (accepts any number of arguments)
  - If specified: Function accepts exactly that many arguments
  - Use `-1` for explicitly variadic functions
- `deterministic` (bool, optional): Whether the function is deterministic (default: `False`)
  - `True`: Function always returns the same result for identical inputs (enables SQLite optimizations)
  - `False`: Function may return different results (e.g., functions using random values or current time)

**Returns:** `None` on success

**Raises:** Error on failure (invalid parameters, duplicate registration, etc.)

### Registration Timing Requirements

Functions **MUST** be registered before opening database connections:

```python
load("sqlite", "connect", "register_function")

def main():
    # ✅ CORRECT: Register before opening connections
    register_function("MY_FUNC", lambda x: x * 2)
    db = connect("database.db")  # Function available
    
    # ❌ INCORRECT: Register after opening connection
    # db = connect("database.db")
    # register_function("MY_FUNC", lambda x: x * 2)  # Too late!

main()
```

### Basic Examples

#### Simple String Function

```python
load("sqlite", "connect", "register_function")

def main():
    # Register a string trimming function
    register_function("MY_TRIM", lambda s: s.strip() if s else "")
    
    # Open database and use the function
    db = connect(":memory:")
    db.execute("CREATE TABLE users (name TEXT)")
    db.execute("INSERT INTO users VALUES ('  John Doe  ')")
    
    result = db.query("SELECT MY_TRIM(name) as clean_name FROM users")
    print(result)  # [{"clean_name": "John Doe"}]
    
    db.close()

main()
```

#### Mathematical Function

```python
load("sqlite", "connect", "register_function")

def main():
    # Register a deterministic mathematical function
    register_function("SQUARE", lambda x: x * x if x else 0, num_args=1, deterministic=True)
    
    db = connect(":memory:")
    db.execute("CREATE TABLE measurements (side REAL)")
    db.execute("INSERT INTO measurements VALUES (5.0)")
    
    # Can create functional indexes with deterministic functions
    db.execute("CREATE INDEX idx_area ON measurements (SQUARE(side))")
    result = db.query("SELECT SQUARE(side) as area FROM measurements")
    print(result)  # [{"area": 25.0}]
    
    db.close()

main()
```

#### Multi-Argument Function

```python
load("sqlite", "connect", "register_function")

def main():
    # Register a tax calculation function
    register_function("ADD_TAX", lambda price, rate: price * (1.0 + rate), num_args=2)
    
    db = connect(":memory:")
    db.execute("CREATE TABLE products (price REAL)")
    db.execute("INSERT INTO products VALUES (100.0)")
    
    result = db.query("SELECT ADD_TAX(price, 0.08) as total FROM products")
    print(result)  # [{"total": 108.0}]
    
    db.close()

main()
```

#### Variadic Function

```python
load("sqlite", "connect", "register_function")

def main():
    # Register a function that accepts variable arguments
    def greatest(*args):
        valid_args = [arg for arg in args if arg != None]
        return max(valid_args) if valid_args else None
    
    register_function("GREATEST", greatest)  # variadic by default
    
    db = connect(":memory:")
    result = db.query("SELECT GREATEST(1, 5, 3, 9, 2) as max_val")
    print(result)  # [{"max_val": 9}]
    
    db.close()

main()
```

### Advanced Examples

#### Complex Data Processing

```python
load("sqlite", "connect", "register_function")

def main():
    # Register a function that returns JSON statistics
    def get_stats(*args):
        if not args:
            return {}
        
        non_null = [arg for arg in args if arg != None]
        if not non_null:
            return {}
        
        total = sum(non_null)
        return {
            "count": len(non_null),
            "sum": total,
            "avg": total / len(non_null),
            "min": min(non_null),
            "max": max(non_null)
        }
    
    register_function("GET_STATS", get_stats)
    
    db = connect(":memory:")
    result = db.query("SELECT GET_STATS(10.5, 20.3, 15.7) as stats")
    print(result)  # Complex data automatically JSON-encoded
    
    db.close()

main()
```

#### String Manipulation

```python
load("sqlite", "connect", "register_function")

def main():
    # Register multiple string functions
    register_function("REVERSE_STR", lambda s: s[::-1] if s else "", num_args=1)
    register_function("CONCAT_WS", lambda sep, *args: sep.join([str(arg) for arg in args if arg != None]))
    
    db = connect(":memory:")
    db.execute("CREATE TABLE users (first_name TEXT, last_name TEXT)")
    db.execute("INSERT INTO users VALUES ('John', 'Doe')")
    
    # Use functions in SQL queries
    result = db.query("""
        SELECT 
            CONCAT_WS(' ', first_name, last_name) as full_name,
            REVERSE_STR(first_name) as reversed_first
        FROM users
    """)
    print(result)  # [{"full_name": "John Doe", "reversed_first": "nhoJ"}]
    
    db.close()

main()
```

#### Multiple Database Connections

```python
load("sqlite", "connect", "register_function")

def main():
    # Register functions once (before opening any connections)
    # Note: Use unique function names to avoid conflicts with other modules/tests
    register_function("APP_DOUBLE", lambda x: x * 2, num_args=1)
    register_function("APP_CONCAT_WS", lambda sep, *args: sep.join([str(arg) for arg in args if arg != None]))
    
    # Functions are available to ALL connections opened after registration
    db1 = connect(":memory:")
    db2 = connect("app.db")
    
    # Both databases can use the registered functions
    db1.execute("CREATE TABLE test1 (val INTEGER)")
    db1.execute("INSERT INTO test1 VALUES (5)")
    result1 = db1.query("SELECT APP_DOUBLE(val) FROM test1")
    
    db2.execute("CREATE TABLE test2 (first TEXT, last TEXT)")
    db2.execute("INSERT INTO test2 VALUES ('John', 'Doe')")
    result2 = db2.query("SELECT APP_CONCAT_WS(' ', first, last) as fullname FROM test2")
    
    db1.close()
    db2.close()

main()
```

### Error Handling

The module provides comprehensive error handling for custom functions:

#### Registration Errors

```python
load("sqlite", "connect", "register_function")

def main():
    # These will cause registration errors and halt script execution:
    
    # Empty function name
    register_function("", lambda x: x)  # Error: function name cannot be empty
    
    # Non-callable parameter
    register_function("NOT_FUNC", "not a function")  # Error: got string, want callable
    
    # Invalid num_args
    register_function("BAD_ARGS", lambda x: x, num_args=-2)  # Error: num_args must be >= -1
    
    # Duplicate registration
    register_function("TEST", lambda x: x)
    register_function("TEST", lambda x: x * 2)  # Error: function 'TEST' is already registered

main()
```

#### Runtime Errors

When a custom function fails during SQL execution, the error is propagated as a SQL error:

```python
load("sqlite", "connect", "register_function")

def main():
    # Register a function that can fail
    def divide_func(a, b):
        if b == 0:
            fail("Division by zero")
        return a / b
    
    register_function("SAFE_DIVIDE", divide_func, num_args=2)
    
    db = connect(":memory:")
    db.execute("CREATE TABLE test (a REAL, b REAL)")
    db.execute("INSERT INTO test VALUES (10, 0)")  # Will cause division by zero
    
    # This will fail with: "Starlark function execution failed: fail: Division by zero"
    result = db.query("SELECT SAFE_DIVIDE(a, b) FROM test")

main()
```

#### Non-Existent Functions

Calling functions that were never registered results in SQL errors:

```python
load("sqlite", "connect")

def main():
    db = connect(":memory:")
    db.execute("CREATE TABLE test (value INTEGER)")
    db.execute("INSERT INTO test VALUES (42)")
    
    # This will fail with: "no such function: UNDEFINED_FUNC"
    result = db.query("SELECT UNDEFINED_FUNC(value) FROM test")

main()
```

#### Handling Function Errors Gracefully

Since Starlark doesn't have try/catch, validate inputs before function registration:

```python
load("sqlite", "connect", "register_function")

def main():
    # Good: Validate and handle edge cases within the function
    def safe_divide(a, b):
        # Handle None values
        if a == None or b == None:
            return None
        # Handle division by zero
        if b == 0:
            return None  # Return None instead of failing
        return a / b
    
    register_function("SAFE_DIVIDE", safe_divide, num_args=2)
    
    db = connect(":memory:")
    db.execute("CREATE TABLE test (a REAL, b REAL)")
    db.execute("INSERT INTO test VALUES (10, 0)")
    db.execute("INSERT INTO test VALUES (20, 4)")
    db.execute("INSERT INTO test VALUES (NULL, 5)")
    
    # This query will succeed, returning NULL for problematic cases
    result = db.query("SELECT a, b, SAFE_DIVIDE(a, b) as result FROM test")
    for row in result:
        print("Result: {} / {} = {}".format(row["a"], row["b"], row["result"]))
    
    db.close()

main()
```

### Performance Considerations

#### Deterministic Functions

Mark functions as `deterministic=True` when they always return the same result for identical inputs:

```python
# ✅ Good: Pure mathematical functions
register_function("SQUARE", lambda x: x * x, num_args=1, deterministic=True)
register_function("FACTORIAL", factorial_func, num_args=1, deterministic=True)

# ❌ Bad: Functions with side effects or randomness
register_function("RANDOM_ID", lambda: random.randint(1, 1000), deterministic=True)  # Wrong!
register_function("CURRENT_USER", get_current_user, deterministic=True)  # Wrong!
```

Deterministic functions enable SQLite optimizations:

- **Result Caching**: SQLite can cache results for identical inputs
- **Constant Folding**: Evaluation at compile time for constant inputs
- **Functional Indexes**: Can create indexes on function results
- **Query Optimization**: Better query plan generation

#### Memory and Performance Tips

```python
# ✅ Efficient: Use appropriate num_args for validation
register_function("ADD_TWO", lambda a, b: a + b, num_args=2)  # Exactly 2 args

# ✅ Efficient: Mark pure functions as deterministic
register_function("CALC_TAX", lambda price, rate: price * rate, num_args=2, deterministic=True)

# ✅ Efficient: Handle None values early
def safe_math(a, b):
    if a == None or b == None:
        return None
    return a + b

register_function("SAFE_ADD", safe_math, num_args=2, deterministic=True)
```

### Type Conversion

Arguments passed to custom functions are automatically converted from SQLite types to Starlark types, and return values are converted back:

| SQLite → Starlark | Starlark → SQLite | Notes |
|-------------------|-------------------|-------|
| NULL → None       | None → NULL       |       |
| INTEGER → int     | int → INTEGER     |       |
| REAL → float      | float → REAL      |       |
| TEXT → string     | string → TEXT     |       |
| BLOB → bytes      | bytes → BLOB      |       |
|                   | bool → INTEGER    | True→1, False→0 |
|                   | dict → TEXT (JSON)| Automatically serialized |
|                   | list → TEXT (JSON)| Automatically serialized |

#### Complex Type Example

```python
load("sqlite", "connect", "register_function")

def main():
    # Function that processes and returns complex data
    def process_data(value):
        return {
            "original": value,
            "doubled": value * 2,
            "type": type(value).__name__
        }
    
    register_function("PROCESS_DATA", process_data, num_args=1)
    
    db = connect(":memory:")
    db.execute("CREATE TABLE test (value INTEGER)")
    db.execute("INSERT INTO test VALUES (42)")
    
    result = db.query("SELECT PROCESS_DATA(value) as processed FROM test")
    # Returns JSON string: {"doubled":84,"original":42,"type":"int"}
    print(result[0]["processed"])
    
    db.close()

main()
```

### Best Practices

1. **Register functions at startup** before opening any database connections
2. **Use unique function names** with prefixes (e.g., `APP_`, `MODULE_`) to avoid conflicts with other modules or tests
3. **Use descriptive function names** to avoid conflicts with SQLite built-ins
4. **Mark mathematical/pure functions as deterministic** for optimization benefits
5. **Handle None values gracefully** in function implementations
6. **Keep functions simple** - complex logic should be done outside the SQL function
7. **Test error conditions** to ensure robust error handling
8. **Use appropriate num_args** for better performance and validation
9. **Validate inputs within functions** instead of relying on external error handling
10. **Return None for invalid inputs** rather than using fail() when possible
11. **Consider memory usage** for functions that process large data sets

### Complete Example

```python
load("sqlite", "connect", "register_function")

def main():
    # Register multiple functions with different characteristics
    
    # Simple deterministic math function
    register_function("DEMO_SQUARE", lambda x: x * x if x != None else None, 
                     num_args=1, deterministic=True)
    
    # String processing function
    register_function("DEMO_CLEAN_TEXT", lambda s: s.strip().title() if s else "", 
                     num_args=1)
    
    # Variadic function for statistics
    def calculate_average(*args):
        numbers = [arg for arg in args if arg != None and isinstance(arg, (int, float))]
        return sum(numbers) / len(numbers) if numbers else None
    
    register_function("DEMO_AVG_OF", calculate_average, deterministic=True)
    
    # Complex data function
    def create_summary(name, *values):
        if not name:
            return None
        
        numbers = [v for v in values if v != None and isinstance(v, (int, float))]
        return {
            "name": name,
            "count": len(numbers),
            "total": sum(numbers) if numbers else 0,
            "average": sum(numbers) / len(numbers) if numbers else None
        }
    
    register_function("DEMO_SUMMARY", create_summary)
    
    # Now use all functions
    db = connect(":memory:")
    
    db.execute("""CREATE TABLE data (
        id INTEGER PRIMARY KEY,
        name TEXT,
        value1 REAL,
        value2 REAL,
        value3 REAL
    )""")
    
    db.insert_many("data", [
        {"name": "  alice  ", "value1": 10.5, "value2": 20.3, "value3": 15.7},
        {"name": "  bob  ", "value1": 8.2, "value2": 12.1, "value3": 9.8},
        {"name": "  charlie  ", "value1": 15.0, "value2": 25.5, "value3": 20.0}
    ])
    
    # Query using all custom functions
    result = db.query("""
        SELECT 
            DEMO_CLEAN_TEXT(name) as clean_name,
            DEMO_SQUARE(value1) as squared_value1,
            DEMO_AVG_OF(value1, value2, value3) as average,
            DEMO_SUMMARY(DEMO_CLEAN_TEXT(name), value1, value2, value3) as summary
        FROM data
        ORDER BY clean_name
    """)
    
    for row in result:
        print("Name: {}".format(row["clean_name"]))
        print("  Squared Value1: {}".format(row["squared_value1"]))
        print("  Average: {}".format(row["average"]))
        print("  Summary: {}".format(row["summary"]))
        print()
    
    db.close()
    
    print("✓ All custom function examples completed successfully!")

main()
```

This example demonstrates:

- Registration timing (before database connection)
- Different function types (deterministic, variadic, complex)
- Proper error handling with None checks
- Type conversion for complex return values
- Integration with regular SQL operations
- Best practices for performance and reliability

## Examples

### Basic Database Operations

```python
load("sqlite", "connect")

def main():
    # Connect to an in-memory database
    db = connect(":memory:")
    
    # Create a table
    db.execute("""
        CREATE TABLE users (
            id INTEGER PRIMARY KEY,
            name TEXT NOT NULL,
            email TEXT UNIQUE,
            age INTEGER,
            created_at TEXT DEFAULT CURRENT_TIMESTAMP
        )
    """)
    
    # Insert records using high-level API
    user_id = db.insert("users", {
        "name": "Alice",
        "email": "alice@example.com",
        "age": 30
    })
    print("Inserted user with ID:", user_id)
    
    # Insert multiple records
    db.insert_many("users", [
        {"name": "Bob", "email": "bob@example.com", "age": 25},
        {"name": "Charlie", "email": "charlie@example.com", "age": 35}
    ])
    
    # Query records
    users = db.select("users", 
        ["name", "age"], 
        ["age >= ?", 25], 
        order_by="age DESC"
    )
    
    for user in users:
        print("User: {} (age {})".format(user["name"], user["age"]))
    
    # Update a record
    db.update("users", {"age": 31}, ["name = ?", "Alice"])
    
    # Count records
    adult_count = db.count("users", ["age >= ?", 18])
    print("Adult users:", adult_count)
    
    # Delete a record
    db.delete("users", ["name = ?", "Bob"])
    
    db.close()

main()
```

### Advanced Transaction Example

```python
load("sqlite", "connect")

def main():
    # Connect to a file database
    db = connect("bank.db")
    
    # Create accounts table
    if not db.table_exists("accounts"):
        db.create_table("accounts", {
            "id": "INTEGER PRIMARY KEY",
            "account_number": "TEXT UNIQUE NOT NULL",
            "owner": "TEXT NOT NULL",
            "balance": "REAL NOT NULL DEFAULT 0.0"
        })
    
    # Insert initial accounts
    db.insert_many("accounts", [
        {"account_number": "ACC001", "owner": "Alice", "balance": 1000.0},
        {"account_number": "ACC002", "owner": "Bob", "balance": 500.0}
    ])
    
    def transfer_money(from_account, to_account, amount):
        """Transfer money between accounts using a transaction."""
        tx = db.begin()
        
        # Check source account balance
        source_result = tx.query_one(
            "SELECT * FROM accounts WHERE account_number = ?",
            [from_account]
        )
        
        if not source_result.ok:
            tx.rollback()
            return False, "Database error: " + source_result.error
        
        if not source_result.value:
            tx.rollback()
            return False, "Source account not found"
        
        source = source_result.value
        if source["balance"] < amount:
            tx.rollback()
            return False, "Insufficient funds"
        
        # Check destination account exists
        destination_result = tx.query_one(
            "SELECT * FROM accounts WHERE account_number = ?",
            [to_account]
        )
        
        if not destination_result.ok:
            tx.rollback()
            return False, "Database error: " + destination_result.error
        
        if not destination_result.value:
            tx.rollback()
            return False, "Destination account not found"
        
        # Perform the transfer
        debit_result = tx.execute(
            "UPDATE accounts SET balance = balance - ? WHERE account_number = ?",
            [amount, from_account]
        )
        
        credit_result = tx.execute(
            "UPDATE accounts SET balance = balance + ? WHERE account_number = ?",
            [amount, to_account]
        )
        
        # Check if transfer operations succeeded
        if not debit_result.ok or not credit_result.ok:
            tx.rollback()
            return False, "Transfer operations failed"
        
        # Commit the transaction
        commit_result = tx.commit()
        if not commit_result.ok:
            return False, "Failed to commit transaction: " + commit_result.error
        
        return True, "Transfer successful"
    
    # Perform transfers
    success, message = transfer_money("ACC001", "ACC002", 200.0)
    print("Transfer 1:", message)
    
    success, message = transfer_money("ACC002", "ACC001", 1000.0)
    print("Transfer 2:", message)  # Should fail due to insufficient funds
    
    # Check final balances
    accounts = db.select("accounts", ["account_number", "owner", "balance"])
    print("\nFinal balances:")
    for account in accounts:
        print("{}: {} - ${}".format(
            account["account_number"], 
            account["owner"], 
            account["balance"]
        ))
    
    db.close()

main()
```

### Advanced Transaction Error Handling

Transaction operations return result objects that allow you to handle errors gracefully without script termination:

```python
load("sqlite", "connect")

def main():
    # Connect to database
    db = connect(":memory:")
    
    # Create accounts table
    db.create_table("accounts", {
        "id": "INTEGER PRIMARY KEY",
        "name": "TEXT NOT NULL",
        "balance": "REAL NOT NULL DEFAULT 0.0 CHECK (balance >= 0)"
    })
    
    # Insert test accounts
    db.insert_many("accounts", [
        {"name": "Alice", "balance": 1000.0},
        {"name": "Bob", "balance": 500.0}
    ])
    
    def safe_transfer(from_name, to_name, amount):
        """Perform a safe money transfer with comprehensive error handling."""
        tx = db.begin()
        
        # Check source account
        from_result = tx.query_one("SELECT * FROM accounts WHERE name = ?", [from_name])
        if not from_result.ok:
            tx.rollback()
            return False, "Database error checking source account: " + from_result.error
        
        if not from_result.value:
            tx.rollback()
            return False, "Source account '{}' not found".format(from_name)
        
        from_account = from_result.value
        if from_account["balance"] < amount:
            tx.rollback()
            return False, "Insufficient funds: ${} available, ${} requested".format(
                from_account["balance"], amount
            )
        
        # Check destination account
        to_result = tx.query_one("SELECT * FROM accounts WHERE name = ?", [to_name])
        if not to_result.ok:
            tx.rollback()
            return False, "Database error checking destination account: " + to_result.error
        
        if not to_result.value:
            tx.rollback()
            return False, "Destination account '{}' not found".format(to_name)
        
        # Perform transfer operations
        debit_result = tx.execute(
            "UPDATE accounts SET balance = balance - ? WHERE name = ?",
            [amount, from_name]
        )
        
        if not debit_result.ok:
            tx.rollback()
            return False, "Failed to debit source account: " + debit_result.error
        
        credit_result = tx.execute(
            "UPDATE accounts SET balance = balance + ? WHERE name = ?",
            [amount, to_name]
        )
        
        if not credit_result.ok:
            tx.rollback()
            return False, "Failed to credit destination account: " + credit_result.error
        
        # Verify the transfer worked correctly
        verify_result = tx.query(
            "SELECT name, balance FROM accounts WHERE name IN (?, ?) ORDER BY name",
            [from_name, to_name]
        )
        
        if not verify_result.ok:
            tx.rollback()
            return False, "Failed to verify transfer: " + verify_result.error
        
        # Commit the transaction
        commit_result = tx.commit()
        if not commit_result.ok:
            return False, "Failed to commit transaction: " + commit_result.error
        
        return True, "Transfer of ${} from {} to {} completed successfully".format(
            amount, from_name, to_name
        )
    
    # Test successful transfer
    success, message = safe_transfer("Alice", "Bob", 200.0)
    print("Transfer 1:", message)
    
    # Test transfer with insufficient funds
    success, message = safe_transfer("Bob", "Alice", 1000.0)
    print("Transfer 2:", message)
    
    # Test transfer to non-existent account
    success, message = safe_transfer("Alice", "Charlie", 100.0)
    print("Transfer 3:", message)
    
    # Show final balances
    balances = db.query("SELECT name, balance FROM accounts ORDER BY name")
    print("\nFinal balances:")
    for account in balances:
        print("  {}: ${}".format(account["name"], account["balance"]))
    
    db.close()

main()
```

### Batch Operations Example

```python
load("sqlite", "connect")

def main():
    # Connect to an in-memory database
    db = connect(":memory:")
    
    # Create tables using batch operations
    setup_results = db.batch([
        """CREATE TABLE accounts (
            id INTEGER PRIMARY KEY,
            name TEXT NOT NULL,
            balance REAL NOT NULL DEFAULT 0.0
        )""",
        """CREATE TABLE transactions (
            id INTEGER PRIMARY KEY,
            from_account INTEGER,
            to_account INTEGER,
            amount REAL NOT NULL,
            timestamp TEXT DEFAULT CURRENT_TIMESTAMP
        )""",
        "CREATE INDEX idx_accounts_name ON accounts(name)"
    ])
    
    print("Setup completed. Results:", setup_results)
    
    # Insert initial data using batch with parameters
    initial_data = db.batch([
        ["INSERT INTO accounts (name, balance) VALUES (?, ?)", ["Alice", 1000.0]],
        ["INSERT INTO accounts (name, balance) VALUES (?, ?)", ["Bob", 500.0]],
        ["INSERT INTO accounts (name, balance) VALUES (?, ?)", ["Charlie", 750.0]]
    ])
    
    print("Initial data inserted. Results:", initial_data)
    
    # Perform a money transfer using batch operations
    transfer_amount = 200.0
    transfer_results = db.batch([
        ["UPDATE accounts SET balance = balance - ? WHERE name = ?", [transfer_amount, "Alice"]],
        ["UPDATE accounts SET balance = balance + ? WHERE name = ?", [transfer_amount, "Bob"]],
        ["INSERT INTO transactions (from_account, to_account, amount) VALUES (?, ?, ?)", [1, 2, transfer_amount]]
    ])
    
    print("Transfer completed. Results:", transfer_results)
    
    # Mixed batch operations (some with params, some without)
    mixed_results = db.batch([
        "UPDATE accounts SET balance = 1000.0 WHERE id = 3",  # String query
        ["INSERT INTO accounts (name, balance) VALUES (?, ?)", ["David", 300.0]],  # Parameterized
        "DELETE FROM transactions WHERE amount < 50.0"  # String query
    ])
    
    print("Mixed operations completed. Results:", mixed_results)
    
    # Verify the results
    accounts = db.query("SELECT * FROM accounts ORDER BY name")
    print("\nFinal account balances:")
    for account in accounts:
        print("  {}: ${}".format(account["name"], account["balance"]))
    
    # Check transaction history
    transactions = db.query("SELECT * FROM transactions")
    print("\nTransaction history:")
    for tx in transactions:
        print("  From account {} to account {}: ${}".format(
            tx["from_account"], tx["to_account"], tx["amount"]))
    
    # All operations within each batch are executed in a single transaction
    # If any operation fails, the entire batch is rolled back
    
    db.close()
    
    print("\n✓ Batch operations example completed successfully!")

main()
```

### Multi-Database Example

```python
load("sqlite", "connect")

def main():
    # Connect to main database
    db = connect("main.db")
    
    # Create users table
    if not db.table_exists("users"):
        db.create_table("users", {
            "id": "INTEGER PRIMARY KEY",
            "name": "TEXT NOT NULL",
            "email": "TEXT UNIQUE",
            "last_login": "TEXT"
        })
    
    # Insert some test data
    db.insert_many("users", [
        {"name": "Alice", "email": "alice@example.com", "last_login": "2023-12-01"},
        {"name": "Bob", "email": "bob@example.com", "last_login": "2022-06-15"},
        {"name": "Charlie", "email": "charlie@example.com", "last_login": "2023-11-28"}
    ])
    
    # Attach archive database
    db.attach("archive.db", "archive")
    
    # Create archive table in attached database
    db.execute("""
        CREATE TABLE IF NOT EXISTS archive.old_users (
            id INTEGER PRIMARY KEY,
            name TEXT NOT NULL,
            email TEXT,
            last_login TEXT,
            archived_date TEXT DEFAULT CURRENT_TIMESTAMP
        )
    """)
    
    # Move users who haven't logged in since 2023 to archive
    old_users = db.query("""
        SELECT * FROM main.users 
        WHERE last_login < '2023-01-01'
    """)
    
    if old_users:
        print("Archiving {} old users".format(len(old_users)))
        
        # Insert into archive
        for user in old_users:
            db.execute("""
                INSERT INTO archive.old_users (id, name, email, last_login)
                VALUES (?, ?, ?, ?)
            """, [user["id"], user["name"], user["email"], user["last_login"]])
        
        # Delete from main database
        db.execute("DELETE FROM main.users WHERE last_login < '2023-01-01'")
        
        print("Archive complete")
    
    # Check results
    active_users = db.query("SELECT * FROM main.users")
    archived_users = db.query("SELECT * FROM archive.old_users")
    
    print("\nActive users: {}".format(len(active_users)))
    for user in active_users:
        print("- {} ({})".format(user["name"], user["last_login"]))
    
    print("\nArchived users: {}".format(len(archived_users)))
    for user in archived_users:
        print("- {} (archived)".format(user["name"]))
    
    # Detach archive database
    db.detach("archive")
    
    db.close()

main()
```

### Schema Introspection Example

```python
load("sqlite", "connect")

def main():
    # Connect to database
    db = connect("example.db")
    
    # Create some example tables
    db.execute("""
        CREATE TABLE IF NOT EXISTS users (
            id INTEGER PRIMARY KEY,
            name TEXT NOT NULL,
            email TEXT UNIQUE,
            age INTEGER CHECK (age >= 0),
            created_at TEXT DEFAULT CURRENT_TIMESTAMP
        )
    """)
    
    db.execute("""
        CREATE TABLE IF NOT EXISTS posts (
            id INTEGER PRIMARY KEY,
            user_id INTEGER NOT NULL,
            title TEXT NOT NULL,
            content TEXT,
            published_at TEXT DEFAULT CURRENT_TIMESTAMP,
            FOREIGN KEY (user_id) REFERENCES users(id)
        )
    """)
    
    db.execute("CREATE INDEX IF NOT EXISTS idx_posts_user_id ON posts(user_id)")
    
    # Introspect database schema
    print("=== Database Schema Information ===\n")
    
    # List all tables
    tables = db.tables()
    print("Tables in database:")
    for table in tables:
        print("- {}".format(table))
    
    print()
    
    # Get detailed information for each table
    for table in tables:
        print("Table: {}".format(table))
        print("-" * (len(table) + 7))
        
        # Get column information
        columns = db.table_info(table)
        print("Columns:")
        for col in columns:
            pk_marker = " (PK)" if col["pk"] else ""
            notnull_marker = " NOT NULL" if col["notnull"] else ""
            default_info = " DEFAULT {}".format(col["dflt_value"]) if col["dflt_value"] else ""
            
            print("  {} {}{}{}{}\n".format(
                col["name"], 
                col["type"], 
                pk_marker, 
                notnull_marker, 
                default_info
            ))
        
        # Get index information
        indices = db.indices(table)
        if indices:
            print("Indices:")
            for idx in indices:
                print("  - {}".format(idx["name"]))
        else:
            print("No indices")
        
        print()
    
    db.close()

main()
```

## Security Considerations

- ✅ **Always use parameterized queries** to prevent SQL injection
- ✅ **Never concatenate user input directly** into SQL strings
- ✅ **Use the parameter binding feature** for all user-provided values
- ✅ **Validate input data** before database operations when possible

```python
# ✅ GOOD: Using parameters (safe)
users = db.query("SELECT * FROM users WHERE name = ?", [user_input])
db.update("users", {"status": "active"}, ["id = ?", user_id])

# ❌ BAD: String concatenation (vulnerable to SQL injection)
# users = db.query("SELECT * FROM users WHERE name = '" + user_input + "'")
# DON'T DO THIS!
```

## Error Handling

### Database Operations

Most database operations cause the script to fail immediately with a non-zero exit code when errors occur:

```python
load("sqlite", "connect")

def main():
    # Check if database file exists before connecting (if needed)
    db = connect("myapp.db")
    
    # Validate data before operations
    user_name = "Alice"
    if not user_name:
        fail("User name cannot be empty")
    
    # Database operations - any SQL errors will cause script failure
    db.insert("users", {"name": user_name})
    
    # Always close connections
    db.close()
    
    print("Operations completed successfully")

main()
```

### Transaction Error Handling

**New:** Transaction operations return `OperationResult` objects that allow graceful error handling without script termination:

```python
load("sqlite", "connect")

def main():
    db = connect(":memory:")
    
    # Create test table
    db.create_table("accounts", {
        "id": "INTEGER PRIMARY KEY",
        "name": "TEXT NOT NULL",
        "balance": "REAL CHECK (balance >= 0)"
    })
    
    # Start transaction
    tx = db.begin()
    
    # Execute operations with error checking
    insert_result = tx.execute("INSERT INTO accounts (name, balance) VALUES (?, ?)", ["Alice", 1000])
    if not insert_result.ok:
        print("Insert failed:", insert_result.error)
        tx.rollback()
        fail("Transaction aborted")
    
    # Query with error checking
    balance_result = tx.query_one("SELECT balance FROM accounts WHERE name = ?", ["Alice"])
    if not balance_result.ok:
        print("Query failed:", balance_result.error)
        tx.rollback()
        fail("Transaction aborted")
    
    print("Current balance:", balance_result.value["balance"])
    
    # Commit with error checking
    commit_result = tx.commit()
    if not commit_result.ok:
        print("Commit failed:", commit_result.error)
        fail("Transaction commit failed")
    
    print("Transaction completed successfully")
    db.close()

main()
```

## Performance Tips

- Use **batch operations** for multiple related statements in a single transaction
- Use **transactions** for multiple related operations
- Use **prepared statements** for repeated operations
- Consider using **WAL mode** for concurrent access
- Use **appropriate indices** for frequently queried columns
- **Close connections** when done to free resources

```python
# Method 1: Use batch operations for multiple statements (recommended for mixed operations)
db.batch([
    "CREATE TABLE temp_users (id INTEGER, name TEXT)",
    ["INSERT INTO temp_users VALUES (?, ?)", [1, "Alice"]],
    ["INSERT INTO temp_users VALUES (?, ?)", [2, "Bob"]],
    "CREATE INDEX idx_temp_users_name ON temp_users(name)"
])

# Method 2: Use insert_many for bulk inserts (recommended, automatically uses transactions)
db.insert_many("users", [
    {"name": user["name"], "email": user["email"]} 
    for user in large_user_list
])

# Method 3: Use prepared statements for repeated operations
stmt = db.prepare("INSERT INTO users (name, email) VALUES (?, ?)")
for user_data in large_user_list:
    stmt.execute([user_data["name"], user_data["email"]])
stmt.close()

# Method 4: Manual transaction for complex operations
tx = db.begin()
for user_data in large_user_list:
    tx.execute("INSERT INTO users (name, email) VALUES (?, ?)", 
               [user_data["name"], user_data["email"]])
tx.commit()
```

## License

MIT
