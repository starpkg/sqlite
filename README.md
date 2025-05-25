# 🗃️ `sqlite` - Effortless SQLite operations in Starlark

A comprehensive Go module that brings the power of SQLite database operations to your Starlark scripts. This module provides both low-level SQL execution capabilities and high-level table management functions, making database interactions intuitive and straightforward while maintaining robust security features.

[![Go Report Card](https://goreportcard.com/badge/github.com/starpkg/sqlite)](https://goreportcard.com/report/github.com/starpkg/sqlite)
[![GoDoc](https://pkg.go.dev/badge/github.com/starpkg/sqlite)](https://pkg.go.dev/github.com/starpkg/sqlite)

## Features

- ✅ Low-level SQL execution with prepared statements and parameterized queries
- ✅ High-level table and record operations for common database tasks
- ✅ Transaction management with begin/commit/rollback support
- ✅ SQL injection prevention through parameterized queries
- ✅ File-based and in-memory databases with flexible connection options
- ✅ ATTACH/DETACH database support for multi-database operations
- ✅ Schema introspection for table information and indices
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
load("sqlite", "connect")

# Connect to an in-memory database
db = connect(":memory:")

# Create a table
db.execute("""
    CREATE TABLE users (
        id INTEGER PRIMARY KEY,
        name TEXT NOT NULL,
        email TEXT UNIQUE
    )
""")

# Insert data using high-level API
db.insert("users", {"name": "Alice", "email": "alice@example.com"})

# Query data
users = db.query("SELECT * FROM users")
for user in users:
    print("User:", user["name"], user["email"])

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

try:
    # Execute operations within the transaction
    tx.execute("UPDATE accounts SET balance = balance - ? WHERE id = ?", [100, 1])
    tx.execute("UPDATE accounts SET balance = balance + ? WHERE id = ?", [100, 2])
    
    # Commit the transaction
    tx.commit()
    print("Transfer successful")
except Exception as e:
    # Rollback on error
    tx.rollback()
    print("Transfer failed:", str(e))
```

#### Transaction Object Methods

##### `execute(query, params?)`

Executes a SQL statement within the transaction.

**Parameters:**

- `query` (string): SQL statement to execute
- `params` (list): Optional list of parameters

**Returns:** Number of affected rows (int)

##### `query(query, params?)`

Executes a SQL query within the transaction.

**Parameters:**

- `query` (string): SQL query to execute
- `params` (list): Optional list of parameters

**Returns:** List of dictionaries representing rows

##### `query_one(query, params?)`

Executes a SQL query within the transaction and returns the first row.

**Parameters:**

- `query` (string): SQL query to execute
- `params` (list): Optional list of parameters

**Returns:** Dictionary representing the first row, or None

##### `commit()`

Commits the transaction.

**Parameters:** None

**Returns:** None

##### `rollback()`

Rolls back the transaction.

**Parameters:** None

**Returns:** None

### High-Level Table Operations

#### Table Management

##### `create_table(table, columns)`

Creates a new table with specified column definitions.

**Parameters:**

- `table` (string): Name of the table to create
- `columns` (dict): Dictionary mapping column names to their definitions

**Returns:** None

**Example:**

```python
db.create_table("products", {
    "id": "INTEGER PRIMARY KEY",
    "name": "TEXT NOT NULL",
    "price": "REAL DEFAULT 0.0",
    "description": "TEXT",
    "created_at": "TEXT DEFAULT CURRENT_TIMESTAMP"
})
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
        
        try:
            # Check source account balance
            source = tx.query_one(
                "SELECT * FROM accounts WHERE account_number = ?",
                [from_account]
            )
            
            if not source:
                raise Exception("Source account not found")
            
            if source["balance"] < amount:
                raise Exception("Insufficient funds")
            
            # Check destination account exists
            destination = tx.query_one(
                "SELECT * FROM accounts WHERE account_number = ?",
                [to_account]
            )
            
            if not destination:
                raise Exception("Destination account not found")
            
            # Perform the transfer
            tx.execute(
                "UPDATE accounts SET balance = balance - ? WHERE account_number = ?",
                [amount, from_account]
            )
            
            tx.execute(
                "UPDATE accounts SET balance = balance + ? WHERE account_number = ?",
                [amount, to_account]
            )
            
            # Commit the transaction
            tx.commit()
            return True, "Transfer successful"
            
        except Exception as e:
            # Rollback on any error
            tx.rollback()
            return False, str(e)
    
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

The module provides clear error messages for common issues:

```python
load("sqlite", "connect")

def main():
    try:
        db = connect("readonly.db")
        
        # Database operations
        db.insert("users", {"name": "Alice"})
        
    except Exception as e:
        print("Database error:", str(e))
        # Handle the error appropriately
    finally:
        if 'db' in locals():
            db.close()

main()
```

## Performance Tips

- Use **transactions** for multiple related operations
- Use **prepared statements** for repeated operations
- Consider using **WAL mode** for concurrent access
- Use **appropriate indices** for frequently queried columns
- **Close connections** when done to free resources

```python
# Method 1: Use insert_many for bulk inserts (recommended, automatically uses transactions)
db.insert_many("users", [
    {"name": user["name"], "email": user["email"]} 
    for user in large_user_list
])

# Method 2: Use prepared statements for repeated operations
stmt = db.prepare("INSERT INTO users (name, email) VALUES (?, ?)")
for user_data in large_user_list:
    stmt.execute([user_data["name"], user_data["email"]])
stmt.close()

# Method 3: Manual transaction for complex operations
tx = db.begin()
for user_data in large_user_list:
    tx.execute("INSERT INTO users (name, email) VALUES (?, ?)", 
               [user_data["name"], user_data["email"]])
tx.commit()
```

## License

MIT
