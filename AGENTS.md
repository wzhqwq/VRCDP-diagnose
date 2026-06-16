# Go Toolchain

Use this Go toolchain by default:

```powershell
C:\Users\wzhii\go\go1.25.0\bin\go.exe
```

Use these cache paths for Go commands:

```powershell
$env:GOCACHE='E:\go_cache\build'
$env:GOMODCACHE='E:\go_cache\mod'
$env:GOPROXY='https://proxy.golang.org,direct'
```

# Diagnostics Implementation Direction

Implement diagnostics in package `diagnose`.

Current phase:

- API-only diagnostics manager with `db_vc`-backed SQLite table definitions and storage.
- Primary integration model is request `context.Context` plus `io.ReadSeeker` wrapping for `http.ServeContent`.
- Use `diagnose.BeginHTTP(...)`, `diagnose.WrapReadSeeker(ctx, ...)`, and explicit `diagnose.EndHTTP(...)` in docs and examples.
- Keep manual `BeginRequest`, `RecordChunk`, `EndRequest`, and `WrapReadSeekerForRequest` only as lower-level compatibility APIs.
- Expose `diagnose.Tables` to the host database initialization path and call `diagnose.Init()` after `db_vc.Init(...)`.
- Sessions, pacing profiles, requests, chunk events, window metrics, markers, and glitches are persisted through `db_vc.Table` helpers.
- No frontend assets or HTML UI yet.
- JSONL and ad hoc in-memory persistence are intentionally not used.
- Next likely phase is integration testing against the main project's real SQLite connection and then frontend/timeline polish.

# Version-Controlled Database Interface

Use package `db_vc` for SQLite table definitions and compatibility-aware initialization.

How to define database-backed feature packages:

- Define package-level `*db_vc.Table` values with `db_vc.DefTable("table_name").DefColumns(...)`.
- Define columns with helpers such as `db_vc.NewText`, `db_vc.NewInt`, `db_vc.NewBool`, `db_vc.NewTextId`, and `db_vc.NewIncreasingId`.
- Mark primary keys, indexes, stable sort indexes, decorators, and deprecations through the fluent column/table methods.
- Expose a package-level `Tables []*db_vc.Table` slice containing every table the package owns.
- If the package needs prepared statements, cached SQL strings, services, or other database-dependent singletons, expose an `Init()` method and call it only after `db_vc.Init(...)` has initialized the tables.

Database startup pattern:

```go
db_vc.Init(db, dataVersion, diagnose.Tables...)
diagnose.Init()
```

`db_vc.Init`:

- Reads existing SQLite table names.
- Initializes the internal `compatibility` table.
- Checks stored `minimumDataVersion` and `maximumDataVersion` against the provided `utils.ShortVersion`.
- Creates missing tables or adds missing columns only when an upgrade is needed.
- Synchronizes configured indexes during upgrade.
- Calls fatal logging on incompatible databases or initialization errors.

Query usage notes:

- After initialization, use `Table.Exec`, `Query`, `QueryRow`, `Prepare`, or `Tx`; these panic if the table was not initialized.
- Placeholder count is checked for `Exec`, `Query`, and `QueryRow`; the number of `?` placeholders must match args exactly.
- Prefer the quick builders `Select`, `Insert`, `Update`, and `Delete` for table-owned SQL so column names are checked against the declared schema.
- `Delete().Build()` without `Where(...)` is forbidden and fatal.
- `StableSort` requires a primary key and warns if the stable order is not indexed.
