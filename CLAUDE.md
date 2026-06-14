# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`starpkg/sqlite` is an **L4 domain module** of the Star\* ecosystem: it exposes SQLite to Starlark scripts. A script imports the module, opens a connection, and runs queries/DDL/transactions with Go data marshalled to and from Starlark values.

It is pure Go — **no cgo, all platforms** — and speaks SQLite two ways through one object surface:

- **Local** databases via `modernc.org/sqlite` (the `"sqlite"` `database/sql` driver) — files or in-memory.
- **Remote/online** databases via `github.com/tursodatabase/libsql-client-go` (the `"libsql"` driver) — a self-hosted [`sqld`](https://github.com/tursodatabase/libsql) or Turso Cloud. libSQL speaks the SQLite dialect, so a remote connection exposes the **same methods** as a local one.

Layer position: depends downward on `starpkg/base` (the module/config system), `1set/starlet` (the Machine + `go_idiomatic`), and transitively `1set/starlight` + `go.starlark.net`. Nothing in the ecosystem depends on it.

## Dev commands

Pure Go library with a Makefile. From this repo:

```bash
make test                                  # -race -cover, the working bar
make ci                                    # -race -cover profile + bench compile (what CI runs)
go test ./... -run TestMaxRows             # a single test
go test ./... -run xxx -bench . -count 3   # benchmarks
gofmt -l . && go vet ./...                 # must be clean before commit
```

**Verify on the go floor in Docker** — this repo's floor is **go 1.20** (see Release discipline), newer than the local toolchain. Behavior on the floor must be checked in a container:

```bash
docker run --rm -v "$PWD":/src -v "$HOME/go/pkg/mod":/go/pkg/mod -w /src golang:1.20 go test -race -count=1 ./...
```

Remote tests (`TestConnectRemote`) self-skip unless `SQLITE_REMOTE_URL` + `SQLITE_REMOTE_AUTH_TOKEN` are set; never commit credentials. Integration scripts under `../test/sqlite/*.star` live in the **private `starpkg/test` repo** and auto-skip when that directory is absent (e.g. in CI).

## Architecture (the part that spans files)

The module is a **two-driver, one-surface bridge**: any connection — local or remote — becomes the same Starlark object with the same ~24 methods, because both drivers yield a `*sql.DB`.

- **`sqlite.go`** — the module entry. `Module` holds a `base.ConfigurableModule`; `NewModule()` / `NewModuleWithFileAccess(allowed)` construct it. `LoadModule()` exposes three builtins: **`connect`** (local), **`connect_remote`** (libSQL), **`register_function`** (custom scalar funcs). Config keys + defaults + the `restrictFileAccess` flag + `isInMemoryDSN` live here.
- **`database.go`** — `newDatabaseInstance(db, maxRows, restrictFileAccess)` wraps a `*sql.DB` into the Starlark object and registers every method (`query`, `query_one`, `execute`, `select`, `insert`, `insert_many`, `update`, `delete`, `upsert`, `count`, `batch`, `begin`, `prepare`, `prepare_query`, `attach`/`detach`, `create_table`/`drop_table`/`truncate_table`, `tables`/`table_exists`/`table_info`/`indices`, `close`).
- **`operations.go`** — the largest file: the actual SQL behind those methods (statement building, arg binding, result shaping).
- **`transaction.go`** — `begin` returns a transaction object mirroring the connection's methods plus `commit`/`rollback`.
- **`prepared.go`** — prepared statement / prepared query objects from `prepare`/`prepare_query`.
- **`function.go`** — `register_function`: registers a Go-backed scalar function into the engine.
- **`utils.go`** — row materialization + type conversion; `processQueryRows(rows, maxRows)` is the single point that turns rows into Starlark values and enforces the row cap.

## Hardening invariants (preserve when editing)

Tier-2 hardening landed in v0.1.0; the iron rule is **opt-in / default-off so old scripts run identically**.

1. **No host panics from script input.** `register_function` wraps the user callback in a `recover()` (`function.go`): a panicking custom function becomes a script-level error, never a host crash. Don't remove the deferred recover.
2. **Bounded results.** Every query helper routes materialization through `processQueryRows(rows, maxRows)` (`utils.go`). `max_rows` (config / `SQLITE_MAX_ROWS`, default `0` = unlimited) caps rows so a hostile `SELECT *` can't exhaust host memory. New query paths must go through this function, not raw `rows.Scan` loops.
3. **Opt-in file-access gate.** `NewModuleWithFileAccess(false)` sets `restrictFileAccess`; `connect` and `attach` then reject any on-disk path except the configured `database`, allowing only in-memory DSNs. `isInMemoryDSN` must parse `mode=memory` as a real query parameter (via `net/url`), **not** as a substring — a loose `strings.Contains(s, ":memory:")` was a real bypass (`file:/etc/passwd?x=:memory:`). When touching DSN classification, keep it parse-based.
4. **Backward compatibility.** `NewModule()` == `NewModuleWithFileAccess(true)`, `max_rows` defaults to unlimited. Any new safety lever must default to the historical behavior.
5. **PRAGMA config.** `journal_mode` / `synchronous` / `foreign_keys` / `busy_timeout` / `cache_size` are standard SQLite PRAGMAs applied at connect — they are not custom invented defaults.

## Test organization

Group by functional goal — **do not add one `*_test.go` per fix.** `example_test.go` is the home: `TestStarlarkScripts` (the `../test/sqlite` integration harness), the `TestMaxRows*` / `TestFileAccessRestriction` / `TestPanickingRegisteredFunctionReturnsError` hardening sections, `TestConnectRemote`, and `TestExamples`. Add a new test as a **section here**, not a new file. Tests are table/example-driven; no third-party test framework. Keep functions under ~50 lines (Codacy's `nloc` rule — this is why `TestMaxRows` is split into four).

## Documentation

Three layers must stay in sync (enforced by the doc standard, `plan/starpkg文档标准（DOC-STD）`):
- **`README.md`** — every script-facing builtin and object method documented as a backtick whole-word; host levers (`max_rows`, `NewModuleWithFileAccess`) under *Host hardening*. Function names/signatures must match the code.
- **GoDoc** — package comment + a doc comment on every exported symbol (gated by `revive`'s `exported` rule in CI).

## Release discipline

- **Floor = go 1.20**, registered under **ENG-09 SEP** (`plan/ENG-09-SEP…`): the libSQL remote client requires go 1.20; `modernc.org/sqlite` is capped at **v1.31.x** because v1.32+ uses `runtime.Pinner` (a go 1.21 API → declares go 1.20 but won't build on it). Don't bump modernc past v1.31.x without raising the floor.
- **CI matrix** = `[1.20.x, 1.25.x]` via the centralized reusable workflow in `1set/meta`.
- **Releases so far**: `v0.1.0` (local modernc, Tier-2 hardened) → `v0.2.0` (added remote libSQL `connect_remote`). The libSQL dependency adds **~6 MB / +70%** to a built binary vs modernc-only — a candidate for build-tag isolation in a future version.
- **Bumping the version, the go floor, or tagging are user-confirmed actions** — never tag autonomously; default to patch bumps; published tags are immutable.
