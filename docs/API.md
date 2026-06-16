# `sqlite` — Starlark API Reference

The complete reference for every script-facing builtin, object method, and
configuration accessor exposed by the `sqlite` module. For an overview,
installation, and a quickstart, see the [README](../README.md).

The module exposes three top-level builtins via `load("sqlite", …)` —
`connect`, `connect_remote`, and `register_function` — plus a set of
configuration accessors (`get_<key>` / `set_<key>`) generated from the module's
options. A connection object then carries the database methods (`query`,
`execute`, `insert`, transactions, …); libSQL remote connections expose the
**same** object surface as local ones.

## Contents

- [Connection management](#connection-management)
- [Low-level SQL execution](#low-level-sql-execution)
- [Prepared statements](#prepared-statements)
- [Transaction management](#transaction-management)
- [High-level table operations](#high-level-table-operations)
- [Record operations](#record-operations)
- [Multi-database operations](#multi-database-operations)
- [Schema information](#schema-information)
- [Custom SQL functions](#custom-sql-functions)
- [Type conversion](#type-conversion)
- [Configuration](#configuration)

## Connection management

### `connect(database?, timeout?, busy_timeout?, foreign_keys?, journal_mode?, synchronous?, cache_size?)`

Creates a new local database connection with optional configuration. Any
parameter omitted falls back to the corresponding module config option.

**Parameters:**

- `database` (string): Database path or `:memory:` for in-memory (default: module config)
- `timeout` (float): Connection timeout in seconds (default: module config)
- `busy_timeout` (float): Busy timeout in seconds (default: module config)
- `foreign_keys` (bool): Enable foreign key constraints (default: module config)
- `journal_mode` (string): Journal mode (default: module config)
- `synchronous` (string): Synchronous mode (default: module config)
- `cache_size` (int): Cache size in pages (default: module config)

**Returns:** Database object for performing operations.

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

### `connect_remote(url, auth_token?)`

Connects to a **remote libSQL server** — a self-hosted
[`sqld`](https://github.com/tursodatabase/libsql) or
[Turso Cloud](https://turso.tech) — over a pure-Go client (no cgo). The returned
object exposes the **same methods as a local connection** (`query`, `execute`,
`insert`, transactions, …), because libSQL speaks the SQLite dialect.

**Parameters:**

- `url` (string): Server URL, e.g. `libsql://my-db.turso.io`,
  `https://my-db.turso.io`, or `http://localhost:8080` for a local `sqld`.
- `auth_token` (string, optional): Auth token for the server (omit for an
  unauthenticated local `sqld`).

**Returns:** Database object (same API as `connect`).

**Errors:** Fails if `url` is empty, if the connector cannot be created, or if
the initial connection (ping) fails.

**Example:**

```python
# Self-hosted sqld — e.g. docker run -p 8080:8080 ghcr.io/tursodatabase/libsql-server
db = connect_remote("http://localhost:8080")

# Turso Cloud (or any authenticated libSQL server)
db = connect_remote("libsql://my-db.turso.io", auth_token="...")

db.execute("CREATE TABLE IF NOT EXISTS notes (id INTEGER PRIMARY KEY, body TEXT)")
db.execute("INSERT INTO notes (body) VALUES (?)", ["hello from a remote db"])
rows = db.query("SELECT * FROM notes")
db.close()
```

> Note: `connect_remote` is unaffected by the file-access restriction
> (`NewModuleWithFileAccess`) — it opens a network libSQL endpoint, not a local
> file. Gate remote access at the network/credential layer.

### `close()`

Closes the database connection.

**Parameters:** None

**Returns:** None

**Example:**

```python
db.close()
```

## Low-level SQL execution

### `execute(query, params?)`

Executes a SQL statement with optional parameters.

**Parameters:**

- `query` (string): SQL statement to execute
- `params` (list): Optional list of parameters for the query

**Returns:** Number of affected rows (int).

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

### `batch(queries)`

Executes multiple SQL statements in a single transaction. If any operation
fails, the entire batch is rolled back.

**Parameters:**

- `queries` (list): List of queries to execute. Each item can be:
  - A string (SQL statement without parameters)
  - A list/tuple of `[query, params]` (SQL statement with parameters)

**Returns:** List of integers representing affected rows for each query.

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
```

### `query(query, params?)`

Executes a SQL query and returns all results.

**Parameters:**

- `query` (string): SQL query to execute
- `params` (list): Optional list of parameters for the query

**Returns:** List of dictionaries representing rows. (Capped by `max_rows` when
the host sets that limit — see [Configuration](#configuration).)

**Example:**

```python
# Query all users
users = db.query("SELECT * FROM users")
for user in users:
    print(user["id"], user["name"], user["email"])

# Query with parameters
adult_users = db.query("SELECT * FROM users WHERE age >= ?", [18])
```

### `query_one(query, params?)`

Executes a SQL query and returns the first row, or `None` if no rows are found.

**Parameters:**

- `query` (string): SQL query to execute
- `params` (list): Optional list of parameters for the query

**Returns:** Dictionary representing the first row, or `None`.

**Example:**

```python
# Get a specific user
user = db.query_one("SELECT * FROM users WHERE id = ?", [1])
if user:
    print("Found user:", user["name"])
else:
    print("User not found")
```

## Prepared statements

### `prepare(query)`

Creates a prepared statement for repeated execution.

**Parameters:**

- `query` (string): SQL statement to prepare

**Returns:** Prepared statement object.

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

### `prepare_query(query)`

Creates a prepared query statement for repeated querying.

**Parameters:**

- `query` (string): SQL query to prepare

**Returns:** Prepared query statement object.

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

### Prepared statement object methods

A prepared statement / prepared query object exposes the following methods.

#### `execute(params?)`

Executes a prepared statement with optional parameters.

- `params` (list): Optional list of parameters
- **Returns:** Number of affected rows (int).

#### `query(params?)`

Executes a prepared query statement with optional parameters.

- `params` (list): Optional list of parameters
- **Returns:** List of dictionaries representing rows.

#### `query_one(params?)`

Executes a prepared query statement and returns the first row.

- `params` (list): Optional list of parameters
- **Returns:** Dictionary representing the first row, or `None`.

#### `close()`

Closes the prepared statement.

- **Parameters:** None
- **Returns:** None

## Transaction management

### `begin()`

Begins a new transaction.

**Parameters:** None

**Returns:** Transaction object.

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
```

### Transaction object methods

Transaction methods return `OperationResult` objects for graceful error handling
without script termination. Each result exposes:

- `result.ok` (bool): Whether the operation succeeded
- `result.error` (string): Error message if the operation failed
- `result.value`: The actual result value if the operation succeeded

#### `execute(query, params?)`

Executes a SQL statement within the transaction.

- `query` (string): SQL statement to execute
- `params` (list): Optional list of parameters
- **Returns:** `OperationResult` with the number of affected rows in `.value`.

#### `query(query, params?)`

Executes a SQL query within the transaction.

- `query` (string): SQL query to execute
- `params` (list): Optional list of parameters
- **Returns:** `OperationResult` with the list of row dictionaries in `.value`.

#### `query_one(query, params?)`

Executes a SQL query within the transaction and returns the first row.

- `query` (string): SQL query to execute
- `params` (list): Optional list of parameters
- **Returns:** `OperationResult` with the first row dictionary (or `None`) in `.value`.

#### `commit()`

Commits the transaction.

- **Parameters:** None
- **Returns:** `OperationResult` indicating success or failure.

#### `rollback()`

Rolls back the transaction.

- **Parameters:** None
- **Returns:** None

## High-level table operations

### `create_table(table, columns, constraints?, indexes?, exist_ok?)`

Creates a new table with specified column definitions, optional table
constraints, and indexes.

**Parameters:**

- `table` (string): Name of the table to create
- `columns` (dict): Dictionary mapping column names to their definitions
- `constraints` (list, optional): List of table-level constraint SQL strings
- `indexes` (list, optional): List of indexes to create
- `exist_ok` (bool, optional): If `True`, do not raise an error if the table
  already exists (default: `False`)

**Column definitions** can be expressed two ways:

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
       "not_null": True,              # Optional: NOT NULL constraint
       "unique": True,                # Optional: UNIQUE constraint
       "default": "value"             # Optional: DEFAULT value
   }
   ```

**Table constraints** — optional list of table-level constraints as SQL strings:

- `"FOREIGN KEY (column) REFERENCES table(column) ON DELETE CASCADE"`
- `"CHECK (condition)"`
- `"UNIQUE (column1, column2)"`

**Indexes** — optional list of indexes. Each index can be:

- String: Single column name (e.g. `"column_name"`)
- List: Multiple column names for a composite index (e.g. `["col1", "col2"]`)

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
    "id": {"type": "INTEGER", "primary_key": True, "autoincrement": True},
    "username": {"type": "TEXT", "not_null": True, "unique": True},
    "email": {"type": "TEXT", "not_null": True},
    "age": {"type": "INTEGER", "default": 0},
    "is_active": {"type": "BOOLEAN", "default": True}
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

# Idempotent setup scripts — create the table only if it does not exist
db.create_table("settings", {
    "key": "TEXT PRIMARY KEY",
    "value": "TEXT"
}, constraints=["CHECK (length(key) > 0)"], indexes=["key"], exist_ok=True)
```

### `drop_table(table)`

Drops (deletes) a table.

**Parameters:**

- `table` (string): Name of the table to drop

**Returns:** None

**Example:**

```python
db.drop_table("old_products")
```

### `table_exists(table)`

Checks if a table exists in the database.

**Parameters:**

- `table` (string): Name of the table to check

**Returns:** Boolean indicating if the table exists.

**Example:**

```python
if db.table_exists("users"):
    print("Users table exists")
else:
    print("Users table does not exist")
```

### `truncate_table(table)`

Removes all rows from a table (equivalent to `DELETE FROM table`).

**Parameters:**

- `table` (string): Name of the table to truncate

**Returns:** Number of rows deleted (int).

**Example:**

```python
deleted_rows = db.truncate_table("temporary_data")
print("Deleted {} rows".format(deleted_rows))
```

## Record operations

### `insert(table, values)`

Inserts a single record into a table.

**Parameters:**

- `table` (string): Name of the table
- `values` (dict): Dictionary mapping column names to values

**Returns:** Last insert ID (int), or the number of affected rows if the last
insert ID is not available.

**Example:**

```python
user_id = db.insert("users", {
    "name": "Alice",
    "email": "alice@example.com",
    "age": 30
})
print("Inserted user with ID:", user_id)
```

### `insert_many(table, values_list)`

Inserts multiple records into a table in a single transaction.

**Parameters:**

- `table` (string): Name of the table
- `values_list` (list): List of dictionaries, each representing a record to insert

**Returns:** Number of rows inserted (int).

**Example:**

```python
rows_inserted = db.insert_many("users", [
    {"name": "Bob", "email": "bob@example.com", "age": 25},
    {"name": "Charlie", "email": "charlie@example.com", "age": 35},
    {"name": "Diana", "email": "diana@example.com", "age": 28}
])
print("Inserted {} users".format(rows_inserted))
```

### `update(table, values, where?)`

Updates records in a table.

**Parameters:**

- `table` (string): Name of the table
- `values` (dict): Dictionary mapping column names to new values
- `where` (string, list, or None): Optional where clause. Can be:
  - `None`: Update all records (use with caution!)
  - String: Simple where clause with no parameters (e.g. `"age > 18"`)
  - List: Where clause with parameters as `[condition, param1, param2, …]`

**Returns:** Number of rows updated (int).

**Example:**

```python
# Update with simple string condition
db.update("users", {"status": "inactive"}, "age < 18")

# Update with parameterized condition (recommended for user input)
rows_updated = db.update("users", {"age": 31}, ["name = ?", "Alice"])

# Update multiple conditions
rows_updated = db.update("products",
    {"price": 19.99, "on_sale": True},
    ["category = ? AND price > ?", "electronics", 20.0]
)
```

### `upsert(table, values, conflict_columns)`

Inserts a record or updates it if it already exists (based on conflict columns).

**Parameters:**

- `table` (string): Name of the table
- `values` (dict): Dictionary mapping column names to values
- `conflict_columns` (list): List of column names that determine conflicts

**Returns:** Number of rows affected (int).

**Example:**

```python
db.upsert("users",
    {"email": "alice@example.com", "name": "Alice Smith", "age": 31},
    ["email"]
)
```

### `delete(table, where?)`

Deletes records from a table.

**Parameters:**

- `table` (string): Name of the table
- `where` (string, list, or None): Optional where clause. Can be:
  - `None`: Delete all records (use with extreme caution!)
  - String: Simple where clause with no parameters (e.g. `"age < 18"`)
  - List: Where clause with parameters as `[condition, param1, param2, …]`

**Returns:** Number of rows deleted (int).

**Example:**

```python
# Delete with simple string condition
rows_deleted = db.delete("users", "age < 18")

# Delete with parameterized condition (recommended for user input)
rows_deleted = db.delete("users", ["name = ?", "Bob"])

# Delete with multiple conditions
rows_deleted = db.delete("products", ["category = ? AND price < ?", "electronics", 10.0])
```

### `select(table, columns?, where?, order_by?, limit?, offset?)`

Selects records from a table with flexible filtering and sorting options.

**Parameters:**

- `table` (string): Name of the table
- `columns` (string or list): Column names to select, `"*"` for all, or a list of column names
- `where` (string, list, or None): Optional where clause. Can be:
  - `None`: No filtering
  - String: Simple where clause with no parameters (e.g. `"age > 18"`)
  - List: Where clause with parameters as `[condition, param1, param2, …]`
- `order_by` (string): Optional ORDER BY clause (e.g. `"name ASC"`, `"age DESC"`).
  ORDER BY cannot be parameterized, so this value is validated against a strict
  whitelist before use: a comma-separated list of column identifiers (bare,
  dotted `table.col`, or double-quoted/backtick-quoted), each with an optional
  `ASC`/`DESC` and optional `NULLS FIRST`/`NULLS LAST`. Anything else (raw SQL,
  semicolons, parentheses, comments, other keywords) raises
  `invalid order_by clause`.
- `limit` (int): Optional maximum number of rows to return
- `offset` (int): Optional number of rows to skip

**Returns:** List of dictionaries representing the selected rows.

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

### `count(table, where?)`

Counts records in a table with optional filtering.

**Parameters:**

- `table` (string): Name of the table
- `where` (string, list, or None): Optional where clause. Can be:
  - `None`: Count all records
  - String: Simple where clause with no parameters (e.g. `"age > 18"`)
  - List: Where clause with parameters as `[condition, param1, param2, …]`

**Returns:** Number of matching records (int).

**Example:**

```python
# Count all users
total_users = db.count("users")

# Count with simple string condition (no parameters)
adult_users = db.count("users", "age >= 18")

# Count with parameterized condition (recommended for user input)
active_users = db.count("users", ["status = ?", "active"])
```

## Multi-database operations

### `attach(database, alias)`

Attaches another database with a specified alias. (When the host restricts file
access, only in-memory or the configured database may be attached.)

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

### `detach(alias)`

Detaches a previously attached database.

**Parameters:**

- `alias` (string): Alias name of the database to detach

**Returns:** None

**Example:**

```python
db.detach("archive")
```

## Schema information

### `tables()`

Returns a list of all tables in the database.

**Parameters:** None

**Returns:** List of table names (list of strings).

**Example:**

```python
tables = db.tables()
for table in tables:
    print("- {}".format(table))
```

### `table_info(table)`

Returns detailed information about a table's columns.

**Parameters:**

- `table` (string): Name of the table

**Returns:** List of dictionaries containing column information. Each dictionary
contains:

- `cid`: Column ID (int)
- `name`: Column name (string)
- `type`: Column type (string)
- `notnull`: `1` if the column is NOT NULL, else `0` (int)
- `dflt_value`: Default value as a string (or `None`)
- `pk`: `0` if the column is not part of the primary key, otherwise its
  1-based position in the primary key (int)

**Example:**

```python
columns = db.table_info("users")
for col in columns:
    null_str = "NOT NULL" if col["notnull"] else "NULL"
    pk_str = " (PRIMARY KEY)" if col["pk"] else ""
    print("- {} {} {}{}".format(col["name"], col["type"], null_str, pk_str))
```

### `indices(table)`

Returns information about indices on a table.

**Parameters:**

- `table` (string): Name of the table

**Returns:** List of dictionaries containing index information.

**Example:**

```python
indices = db.indices("users")
for idx in indices:
    print("- {}".format(idx["name"]))
```

## Custom SQL functions

The module supports registering custom SQL functions written in Starlark that
can be called from SQL queries, extending SQLite with domain-specific logic.

> **Critical requirements:**
>
> - Custom functions **MUST** be registered **BEFORE** opening any database
>   connection. Functions are registered globally with the SQLite driver and
>   affect all connections opened after registration.
> - **Use unique function names** to avoid conflicts when multiple modules or
>   tests register functions. Consider prefixes like `APP_`, `MODULE_`, etc.

### `register_function(name, func, num_args=None, deterministic=False)`

Registers a custom SQL function that can be called from SQL queries.

**Parameters:**

- `name` (string): Name of the SQL function to register (case-insensitive in SQL)
- `func` (callable): A Starlark function or lambda implementing the logic
- `num_args` (int, optional): Number of arguments the function accepts
  - `None` or unspecified: Function is variadic (accepts any number of arguments)
  - A specific value: Function accepts exactly that many arguments
  - `-1`: Explicitly variadic
- `deterministic` (bool, optional): Whether the function is deterministic
  (default: `False`)
  - `True`: Always returns the same result for identical inputs (enables SQLite
    optimizations such as result caching, constant folding, and functional indexes)
  - `False`: May return different results (e.g. uses randomness or current time)

**Returns:** `None` on success.

**Raises:** Error on failure (empty name, non-callable, `num_args < -1`,
duplicate registration, etc.).

> **Return values:** A function's return value is converted to a SQLite value
> the same way bind parameters are. An integer return that does not fit a
> signed 64-bit value raises `int value too large for SQLite` rather than
> being silently truncated.

**Example:**

```python
load("sqlite", "connect", "register_function")

def main():
    # Register before opening connections
    register_function("MY_TRIM", lambda s: s.strip() if s else "")
    register_function("SQUARE", lambda x: x * x if x else 0, num_args=1, deterministic=True)
    register_function("ADD_TAX", lambda price, rate: price * (1.0 + rate), num_args=2)

    def greatest(*args):
        valid = [a for a in args if a != None]
        return max(valid) if valid else None
    register_function("GREATEST", greatest)  # variadic by default

    db = connect(":memory:")
    db.execute("CREATE TABLE users (name TEXT)")
    db.execute("INSERT INTO users VALUES ('  John Doe  ')")
    result = db.query("SELECT MY_TRIM(name) as clean_name FROM users")
    print(result)  # [{"clean_name": "John Doe"}]
    db.close()

main()
```

### Error handling for custom functions

- **Registration errors** (empty name, non-callable, invalid `num_args`,
  duplicate name) halt script execution at the `register_function` call.
- **Runtime errors** raised inside a custom function (e.g. `fail(...)`) are
  propagated as a SQL error during query execution.
- **Non-existent functions** referenced in SQL produce a SQL error
  (`no such function: …`).

Since Starlark has no try/except, prefer validating inputs inside the function
and returning `None` for invalid inputs rather than calling `fail()`:

```python
def safe_divide(a, b):
    if a == None or b == None:
        return None
    if b == 0:
        return None  # return None instead of failing
    return a / b

register_function("SAFE_DIVIDE", safe_divide, num_args=2)
```

### Best practices

1. Register functions at startup before opening any connection.
2. Use unique, prefixed function names (`APP_`, `MODULE_`) to avoid conflicts.
3. Mark pure/mathematical functions as `deterministic=True` for optimization.
4. Handle `None` values gracefully and return `None` for invalid inputs.
5. Keep functions simple; use an appropriate `num_args` for validation.

## Type conversion

The module automatically handles type conversion between SQLite and Starlark,
both for query results and for arguments/returns of custom SQL functions.

### SQLite to Starlark

| SQLite Type | Starlark Type |
|-------------|---------------|
| NULL        | None          |
| INTEGER     | int           |
| REAL        | float         |
| TEXT        | string        |
| BLOB        | bytes         |
| timestamp   | time          |

### Starlark to SQLite

| Starlark Type | SQLite Type | Notes |
|---------------|-------------|-------|
| None          | NULL        | |
| int           | INTEGER     | |
| float         | REAL        | |
| string        | TEXT        | |
| bytes         | BLOB        | |
| bool          | INTEGER     | True to 1, False to 0 |
| time          | timestamp   | `go.starlark.net/lib/time` value |
| dict          | TEXT        | JSON encoded |
| list          | TEXT        | JSON encoded |

## Configuration

Each module configuration option is exposed to scripts as a pair of generated
accessor builtins (loaded from the `sqlite` module alongside the functions
above):

- **`get_<key>()`** — returns the current value of the option.
- **`set_<key>(value)`** — sets the option (returns `None`).

An option's value resolves in priority order: an explicit `set_<key>` value, the
environment variable, then the default. These options serve as defaults used by
`connect` / `connect_remote` when the corresponding argument is not provided.

None of the `sqlite` options are secret, so every option exposes **both**
`get_<key>` and `set_<key>`. (A secret option would expose only its `set_<key>`
accessor — never a getter — but this module has none.)

| Option | Getter | Setter | Type | Env var | Default | Description |
|--------|--------|--------|------|---------|---------|-------------|
| `database` | `get_database` | `set_database` | string | `SQLITE_DATABASE` | `:memory:` | Path to the SQLite database (use `:memory:` for in-memory) |
| `timeout` | `get_timeout` | `set_timeout` | float | `SQLITE_TIMEOUT` | `30.0` | Connection timeout in seconds |
| `busy_timeout` | `get_busy_timeout` | `set_busy_timeout` | float | `SQLITE_BUSY_TIMEOUT` | `5.0` | Busy timeout in seconds |
| `foreign_keys` | `get_foreign_keys` | `set_foreign_keys` | bool | `SQLITE_FOREIGN_KEYS` | `true` | Enable foreign key constraints |
| `journal_mode` | `get_journal_mode` | `set_journal_mode` | string | `SQLITE_JOURNAL_MODE` | `DELETE` | Journal mode (WAL, DELETE, TRUNCATE, PERSIST, MEMORY, OFF) |
| `synchronous` | `get_synchronous` | `set_synchronous` | string | `SQLITE_SYNCHRONOUS` | `FULL` | Synchronous mode (FULL, NORMAL, OFF) |
| `cache_size` | `get_cache_size` | `set_cache_size` | int | `SQLITE_CACHE_SIZE` | `-2000` | Cache size in number of pages (negative = SQLite default) |
| `max_rows` | `get_max_rows` | `set_max_rows` | int | `SQLITE_MAX_ROWS` | `0` | Max rows a query helper returns; `0` = unlimited (see Host hardening) |

**Example:**

```python
load(
    "sqlite",
    "connect",
    # getters
    "get_database", "get_timeout", "get_busy_timeout", "get_foreign_keys",
    "get_journal_mode", "get_synchronous", "get_cache_size", "get_max_rows",
    # setters
    "set_database", "set_timeout", "set_busy_timeout", "set_foreign_keys",
    "set_journal_mode", "set_synchronous", "set_cache_size", "set_max_rows",
)

set_journal_mode("WAL")
print(get_journal_mode())  # "WAL"

db = connect("app.db")  # opens with journal_mode=WAL
```

### Host hardening (opt-in, host side)

Two **opt-in** levers let the host bound what a script can reach. Both default to
**off**, so existing scripts keep working unchanged. They are configured by the
host in Go, not from a script.

- **Bound result size — `max_rows`.** A query helper materializes every returned
  row into memory. Set `max_rows` (the config option above, or `SQLITE_MAX_ROWS`)
  to cap the number of rows any single query returns; exceeding the cap raises a
  script error instead of allocating without bound. `0` (the default) means
  unlimited.
- **Restrict file access — `NewModuleWithFileAccess`.** By default a script may
  `connect(path)` to any file path (or `attach` any database).
  `NewModuleWithFileAccess(false)` locks this down: scripts may only open
  **in-memory** databases (`:memory:`, `file::memory:…`, `mode=memory`) or the
  **one** `database` configured on the module — any other path, and any `attach`
  of an on-disk database, is rejected. `NewModule()` equals
  `NewModuleWithFileAccess(true)`.

> Note: `connect_remote` is unaffected by `NewModuleWithFileAccess` — it opens a
> network libSQL endpoint, not a local file. Gate remote access at the
> network/credential layer.
