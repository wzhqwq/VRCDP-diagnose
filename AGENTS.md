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

- API-only manager skeleton.
- No frontend assets or HTML UI.
- Keep SQLite persistence intentionally blank for now; the main project already owns SQLite integration.
- Do not add JSONL or in-memory persistence as a temporary replacement.
- Non-stats query APIs may return empty JSON placeholders until real storage is wired in.

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
