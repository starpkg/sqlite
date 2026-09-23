# 🗃️ `sqlite` — Effortless SQLite operations in Starlark

[![godoc](https://pkg.go.dev/badge/github.com/starpkg/sqlite.svg)](https://pkg.go.dev/github.com/starpkg/sqlite)
[![license](https://img.shields.io/badge/License-MIT-blue.svg)](https://opensource.org/licenses/MIT)
[![Go Report Card](https://goreportcard.com/badge/github.com/starpkg/sqlite)](https://goreportcard.com/report/github.com/starpkg/sqlite)
[![codecov](https://codecov.io/gh/starpkg/sqlite/graph/badge.svg)](https://codecov.io/gh/starpkg/sqlite)
![binary footprint](https://img.shields.io/badge/binary_footprint-%2B5.0_MB-blue)

A Go module that brings SQLite to your Starlark scripts. It offers both
low-level SQL execution and high-level table/record helpers, with parameterized
queries, transactions, prepared statements, schema introspection, and custom
SQL functions — pure Go, no cgo, all platforms.

## Overview

- **Low-level SQL** — `execute`, `query`, `query_one`, and `batch` (multi-statement transactions).
- **High-level helpers** — `create_table`, `insert`, `insert_many`, `update`, `upsert`, `delete`, `select`, `count`, and friends.
- **Transactions** — `begin` returns a transaction whose methods yield `OperationResult` objects for graceful error handling.
- **Prepared statements** — `prepare` / `prepare_query` for repeated execution.
- **Custom SQL functions** — `register_function` runs Starlark logic inside SQL queries.
- **Local & remote** — local files / in-memory via `connect`; remote libSQL (self-hosted `sqld` or Turso Cloud) via `connect_remote`, exposing the same object surface.
- **Parameter binding and host controls** — SQLite ⇄ Starlark type conversion, configurable row limits, and opt-in file/function restrictions (`NewModuleWithFileAccess`, `NewModuleWithHostPolicy`).

For the complete per-builtin reference — signatures, parameters, returns,
errors, examples — and the configuration accessors, see
**[docs/API.md](docs/API.md)**.

## Installation

```bash
go get github.com/starpkg/sqlite
```

## Quick Start

Wire the module into a Starlet interpreter, then `load("sqlite", …)` from a
script:

```go
package main

import (
    "fmt"

    "github.com/1set/starlet"
    "github.com/starpkg/sqlite"
)

func main() {
    sqliteModule := sqlite.NewModule()
    interpreter := starlet.New(
        starlet.WithModuleLoader("sqlite", sqliteModule.LoadModule()),
    )

    script := `
load("sqlite", "connect")

# Connect to an in-memory database
db = connect(":memory:")

# Create a table and insert with the high-level API
db.execute("CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, score INTEGER)")
db.insert("users", {"name": "Alice", "score": 95})

# Query it back
for user in db.query("SELECT name, score FROM users"):
    print(user["name"], user["score"])

db.close()
`

    if err := interpreter.ExecScript("example.star", script); err != nil {
        fmt.Println("Error:", err)
    }
}
```

Register a custom SQL function (before opening any connection) and call it from
SQL:

```python
load("sqlite", "connect", "register_function")

# Use unique names to avoid clashes; register BEFORE connecting
register_function("DOUBLE_SCORE", lambda x: x * 2 if x else 0, num_args=1, deterministic=True)

db = connect(":memory:")
db.execute("CREATE TABLE users (name TEXT, score INTEGER)")
db.insert("users", {"name": "Alice", "score": 95})

rows = db.query("SELECT name, DOUBLE_SCORE(score) AS doubled FROM users")
print(rows)  # [{"name": "Alice", "doubled": 190}]
db.close()
```

## Starlark API at a glance

Top-level builtins (`load("sqlite", …)`):

- `connect(database?, timeout?, busy_timeout?, foreign_keys?, journal_mode?, synchronous?, cache_size?)` — open a local connection.
- `connect_remote(url, auth_token?)` — open a remote libSQL connection (same object API as `connect`).
- `register_function(name, func, num_args?, deterministic?)` — register a Starlark-backed SQL function (before connecting).

Connection object methods:

- `execute(query, params?)` — run a statement; returns affected rows.
- `query(query, params?)` — run a query; returns a list of row dicts.
- `query_one(query, params?)` — return the first row dict, or `None`.
- `batch(queries)` — run many statements in one transaction.
- `prepare(query)` / `prepare_query(query)` — prepared statement / query objects.
- `begin()` — start a transaction (`commit` / `rollback`, result-object methods).
- `create_table(table, columns, constraints?, indexes?, exist_ok?)` — create a table.
- `drop_table(table)` / `truncate_table(table)` — drop a table / delete all its rows.
- `table_exists(table)` — whether a table exists.
- `insert(table, values)` — insert one record; returns last insert ID.
- `insert_many(table, values_list)` — insert many records in one transaction.
- `update(table, values, where?)` — update records.
- `upsert(table, values, conflict_columns)` — insert or update on conflict.
- `delete(table, where?)` — delete records.
- `select(table, columns?, where?, order_by?, limit?, offset?)` — filtered/sorted read (`order_by` is whitelist-validated, not parameterized).
- `count(table, where?)` — count matching records.
- `attach(database, alias)` / `detach(alias)` — multi-database operations.
- `tables()` / `table_info(table)` / `indices(table)` — schema introspection.
- `close()` — close the connection.

Prepared statement / query objects expose `execute(params?)`, `query(params?)`,
`query_one(params?)`, and `close()`. Transaction objects expose `execute`,
`query`, `query_one`, `commit`, and `rollback`.

See **[docs/API.md](docs/API.md)** for the full signatures, return values,
errors, and examples of every builtin and method above.

## Configuration

The module's options (`database`, `timeout`, `busy_timeout`, `foreign_keys`,
`journal_mode`, `synchronous`, `cache_size`, `max_rows`) are configured via
environment variables (`SQLITE_*`) or per-option `get_<key>` / `set_<key>`
accessor builtins, and serve as defaults for `connect` / `connect_remote`.
`NewModuleWithFileAccess(false)` fixes the allowed `database` at construction
and omits `set_database`; later environment changes cannot widen that path.
`max_rows` remains a configurable row limit, with `0` meaning unlimited.
`timeout` bounds each database
operation (a per-query deadline, and a cancellation point tied to the script
thread) — this is what stops an unreachable remote from hanging the host. The
connection PRAGMAs (`foreign_keys`, `journal_mode`, …) are applied to every
pooled connection, and an in-memory database is served from a single connection
so its schema and data persist across queries. See the
[Configuration section of docs/API.md](docs/API.md#configuration) for the full
option table, defaults, accessors, and host-hardening details.

Hosts can opt out of custom SQLite functions while retaining ordinary local SQL:

```go
module := sqlite.NewModuleWithHostPolicy(sqlite.HostPolicy{
    RestrictFileAccess:      true,
    DisableCustomFunctions: true,
})
```

`DisableCustomFunctions` rejects `register_function` and opens local connections
on a driver without process-wide custom functions, including functions registered
by another module. SQLite built-ins, transactions and prepared queries remain
available. A `file:` URL passed to `connect_remote` follows this same local path,
file restrictions and connection PRAGMAs. Real network libSQL endpoints retain
their existing behavior; server functions and network access are controlled by
the host/server separately. No connection is opened until a script requests one.

`NewModule()` and the zero `HostPolicy` retain custom functions and mutable
configuration. Enabled script UDFs still use the global registry and a separate
Starlark thread: they do not inherit the caller's execution budget or cancellation,
and registrations are not released on connection close. The opt-out is suitable
for hosts that do not need UDFs; it is not a process or filesystem sandbox.

## License

MIT
