# 🗃️ `sqlite` - Effortless SQLite operations in Starlark

A comprehensive Go module that brings the power of SQLite database operations to your Starlark scripts. This module provides both low-level SQL execution capabilities and high-level table management functions, making database interactions intuitive and straightforward while maintaining robust security features.

[![Go Report Card](https://goreportcard.com/badge/github.com/starpkg/sqlite)](https://goreportcard.com/report/github.com/starpkg/sqlite)
[![GoDoc](https://pkg.go.dev/badge/github.com/starpkg/sqlite)](https://pkg.go.dev/github.com/starpkg/sqlite)

## Features

- ✅ Low-level SQL execution API
- ✅ High-level table and record operations
- ✅ Prepared statements and parameterized queries
- ✅ Transaction management (begin/commit/rollback)
- ✅ SQL injection prevention
- ✅ Support for file-based and in-memory databases
- ✅ ATTACH/DETACH database support
- ✅ Automatic type conversion between SQLite and Starlark
- ✅ Compatible with Go 1.18+

## Installation

```bash
go get github.com/starpkg/sqlite
```

## Configuration

The `sqlite` module can be configured with the following options (all optional, it will use the default values if not provided):

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `database` | string | `:memory:` | Path to SQLite database (use `:memory:` for in-memory) |
| `timeout` | float | 30.0 | Connection timeout in seconds |
| `busy_timeout` | float | 5.0 | Busy timeout in seconds |
| `foreign_keys` | bool | true | Enable foreign key constraints |
| `journal_mode` | string | `WAL` | Journal mode (WAL, DELETE, TRUNCATE, PERSIST, MEMORY, OFF) |
| `synchronous` | string | `NORMAL` | Synchronous mode (FULL, NORMAL, OFF) |
| `cache_size` | int | 2000 | Cache size in number of pages |

Module options will act if the corresponding argument is not provided in the `connect` or other functions.

## Module API

### Connection Management

```python
# Connect to a database (returns a database object)
db = sqlite.connect("path/to/database.db", timeout=30, foreign_keys=True)
db = sqlite.connect(":memory:")  # In-memory database

# Connect with custom configuration
db = sqlite.connect(
    database="data.db",
    foreign_keys=True,
    journal_mode="WAL",
    synchronous="NORMAL",
    cache_size=5000,
    busy_timeout=10.0
)

# Close the database connection
db.close()
```

### Low-Level SQL Execution

```python
# Execute SQL statements (returns number of affected rows)
rows_affected = db.execute("CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT)")
rows_affected = db.execute("INSERT INTO users (name) VALUES ('Alice')")

# Query with results
rows = db.query("SELECT * FROM users")
for row in rows:
    print(row["id"], row["name"])

# Query with parameters
rows = db.query("SELECT * FROM users WHERE id > ?", [5])
users = db.query("SELECT * FROM users WHERE name = ?", ["Alice"])

# Get a single row
user = db.query_one("SELECT * FROM users WHERE id = ?", [1])
if user:
    print(user["name"])
```

### Prepared Statements

```python
# Create a prepared statement
stmt = db.prepare("INSERT INTO users (name) VALUES (?)")

# Execute with different parameters
stmt.execute(["Alice"])
stmt.execute(["Bob"])
stmt.execute(["Charlie"])

# Close the statement when done
stmt.close()

# Prepared query statements
query_stmt = db.prepare_query("SELECT * FROM users WHERE id > ?")
rows1 = query_stmt.query([5])
rows2 = query_stmt.query([10])
query_stmt.close()
```

### Transaction Management

```python
# Begin a transaction
tx = db.begin()

# Execute statements within the transaction
tx.execute("INSERT INTO users (name) VALUES ('Alice')")
tx.execute("UPDATE products SET stock = stock - 1 WHERE id = 1")

# Commit or rollback
tx.commit()  # or tx.rollback()
```

### High-Level Table Operations

```python
# Create a table with column definitions
db.create_table("products", {
    "id": "INTEGER PRIMARY KEY",
    "name": "TEXT NOT NULL",
    "price": "REAL DEFAULT 0.0",
    "description": "TEXT",
    "created_at": "TEXT DEFAULT CURRENT_TIMESTAMP"
})

# Drop a table
db.drop_table("products")

# Check if a table exists
exists = db.table_exists("products")

# Truncate a table (delete all records)
db.truncate_table("products")
```

### High-Level Record Operations

```python
# Insert a record
db.insert("users", {"name": "Dave", "email": "dave@example.com"})

# Insert multiple records
db.insert_many("users", [
    {"name": "Eve", "email": "eve@example.com"},
    {"name": "Frank", "email": "frank@example.com"}
])

# Update records
db.update("users", {"status": "active"}, "id = ?", [1])
db.update("products", {"price": 29.99}, "category = ?", ["electronics"])

# Upsert (insert or update if exists)
db.upsert("users", {"id": 1, "name": "Alice", "email": "alice@example.com"}, ["id"])

# Delete records
db.delete("users", "id = ?", [1])
db.delete("products", "category = ? AND price < ?", ["electronics", 10])

# Select records
users = db.select("users", ["id", "name", "email"], "status = ?", ["active"])
products = db.select("products", ["*"], "price > ?", [20])

# Count records
count = db.count("users", "status = ?", ["active"])
```

### ATTACH/DETACH Databases

```python
# Attach another database with an alias
db.attach("archive.db", "archive")

# Use tables from the attached database
db.execute("INSERT INTO archive.old_users SELECT * FROM main.users WHERE created_at < ?", ["2020-01-01"])

# Query from attached database
archived_users = db.query("SELECT * FROM archive.old_users")

# Detach the database when done
db.detach("archive")
```

### Schema Information

```python
# Get list of tables
tables = db.tables()

# Get table schema
columns = db.table_info("users")
for col in columns:
    print(col["name"], col["type"], col["notnull"], col["pk"])

# Get list of indices
indices = db.indices("users")
```

## Type Conversion

The module automatically handles type conversion between SQLite and Starlark:

| SQLite Type | Starlark Type |
|-------------|---------------|
| NULL        | None          |
| INTEGER     | int           |
| REAL        | float         |
| TEXT        | string        |
| BLOB        | bytes         |

And from Starlark to SQLite:

| Starlark Type | SQLite Type |
|---------------|-------------|
| None          | NULL        |
| int           | INTEGER     |
| float         | REAL        |
| string        | TEXT        |
| bytes         | BLOB        |
| dict          | TEXT (JSON) |
| list          | TEXT (JSON) |
| bool          | INTEGER (0/1) |

## Example Usage

### Basic Example

```python
# Connect to an in-memory database
db = sqlite.connect(":memory:")

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

# Insert records
db.insert("users", {"name": "Alice", "email": "alice@example.com", "age": 30})
db.insert("users", {"name": "Bob", "email": "bob@example.com", "age": 25})

# Query records
users = db.query("SELECT * FROM users ORDER BY age DESC")
for user in users:
    print(user["name"], user["age"], user["created_at"])

# Update a record
db.update("users", {"age": 31}, "name = ?", ["Alice"])

# Delete a record
db.delete("users", "name = ?", ["Bob"])
```

### Transaction Example

```python
# Connect to a file database
db = sqlite.connect("myapp.db")

# Create tables if they don't exist
if not db.table_exists("accounts"):
    db.create_table("accounts", {
        "id": "INTEGER PRIMARY KEY",
        "user_id": "INTEGER NOT NULL",
        "balance": "REAL NOT NULL DEFAULT 0.0",
        "FOREIGN KEY (user_id) REFERENCES users(id)"
    })

# Perform a money transfer in a transaction
def transfer_money(from_id, to_id, amount):
    tx = db.begin()
    try:
        # Check if source account has enough funds
        source = tx.query_one("SELECT balance FROM accounts WHERE id = ?", [from_id])
        if not source or source["balance"] < amount:
            tx.rollback()
            return False, "Insufficient funds"
        
        # Update account balances
        tx.execute("UPDATE accounts SET balance = balance - ? WHERE id = ?", [amount, from_id])
        tx.execute("UPDATE accounts SET balance = balance + ? WHERE id = ?", [amount, to_id])
        
        # Commit transaction
        tx.commit()
        return True, "Transfer successful"
    except Exception as e:
        tx.rollback()
        return False, "Transfer failed: " + str(e)

# Use the function
success, message = transfer_money(101, 202, 50.0)
print(message)
```

### Multiple Database Example

```python
# Connect to main database
db = sqlite.connect("app.db")

# Attach an archive database
db.attach("archive.db", "archive")

# Move old records to archive
db.execute("""
    INSERT INTO archive.old_users 
    SELECT * FROM main.users 
    WHERE last_login < date('now', '-1 year')
""")

# Delete archived records from main database
db.execute("DELETE FROM main.users WHERE last_login < date('now', '-1 year')")

# Detach the archive database
db.detach("archive")
```

## Security Considerations

- The module uses parameterized queries to prevent SQL injection attacks
- Never concatenate user input directly into SQL strings
- Use the parameter binding feature for all user-provided values:

```python
# GOOD: Using parameters (safe)
db.query("SELECT * FROM users WHERE name = ?", [user_input])

# BAD: Concatenating strings (vulnerable to SQL injection)
db.query("SELECT * FROM users WHERE name = '" + user_input + "'")  # DON'T DO THIS!
```

## License

MIT
