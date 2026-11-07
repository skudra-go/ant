# Databases

ant runs on five databases. PostgreSQL has everything. The others run queries, writes, relations, change tracking, and transactions, but have no migrations yet.

| | PostgreSQL | MySQL | MariaDB | SQLite | SQL Server |
| --- | --- | --- | --- | --- | --- |
| Tested on | 13 to 18 | 8.4 | 11.8 | modernc 1.61 | 2022 |
| Dialect | `postgres.Dialect` | `mysql.Dialect` | `mysql.MariaDB` | `sqlite.Dialect` | `sqlserver.Dialect` |
| Driver name | `pgx` | `mysql` | `mysql` | `sqlite` | `sqlserver` |
| Queries, relations, tracking, transactions | Yes | Yes | Yes | Yes | Yes |
| Migrations, `EnsureCreated`, `SchemaOf` | Yes | No | No | No | No |
| Whole feature suite (`tests/features`) | Yes | No | No | No | No |
| Conformance suite (`tests/conformance_test.go`) | Yes | Yes | Yes | Yes | Yes |
| Concurrency suite (`tests/concurrency`) | Yes | Yes | Yes | Yes | Yes |
| Upsert | Yes | Yes | Yes | Yes | No |
| `MERGE` | From 15 | No | No | No | Yes |
| Row locks | Yes | Yes | `ForUpdate` only | No | No |
| `FULL JOIN` | Yes | No | No | Yes | Yes |
| Lateral joins | Yes | Yes | No | No | No |
| `UpdateReturning` | Yes | No | No | Yes | Yes |
| `DeleteReturning`, `Toggle` | Yes | No | Yes | Yes | Yes |
| `Today` | Yes | Yes | Yes | No | No |
| `DatePart` | Yes | No | No | No | No |
| `WITH` in a subquery, as in `Update` or `Delete` after `FromCTE` or `Recursive` | Yes | Yes | Yes | Yes | No |
| `ILike`, `FILTER`, `NULLS FIRST` | Native | Emulated | Emulated | `ILike` emulated | Emulated |

Only PostgreSQL runs the whole feature suite, whose tables are written in its DDL. The conformance suite covers what the dialects write differently, on every database, but it is smaller, so expect rough edges off PostgreSQL that nobody has hit yet.

## When something is missing

ant refuses before sending the SQL, so the error says what is wrong:

```go
_, err := ant.Merge(&db.Stock, source, on).WhenMatched(set).Exec(ctx)
errors.Is(err, ant.ErrUnsupported) // true on MySQL
// ant: not supported by this database: MERGE on mysql
```

To check first, ask the dialect:

```go
if dialect.Supports(db.Dialect(), dialect.Merge) {
    // ...
}
```

Error helpers mean the same everywhere. `ant.IsUniqueViolation` works on all five, and each dialect translates its own error numbers.

## What differs

**Generated keys.** MySQL has no `RETURNING`, so a key comes from `LastInsertId`, and other database-filled columns are read back with a second query. Rows with generated keys go in one at a time there, in both `SaveChanges` and `InsertMany`.

**`ON DUPLICATE KEY` on MySQL and MariaDB.** It has no target: any unique key triggers it, not only the columns you name. If a different key is the one that collides, `Upsert` cannot find the row by your columns, and reports that it wrote nothing. Name the only unique key that can collide. It also takes no `WHERE`, so `Conflict.Where` is an error, and an `Upsert` of a copy that carries a version cannot check that version: the version moves on, but the copy's update goes ahead.

**Writes that read their own table on MySQL.** MySQL refuses an `UPDATE` or `DELETE` whose subquery reads the table being written (error 1093), so ant wraps such a subquery in a derived table there, which MySQL reads first. A subquery with several columns that share a name, or that are expressions, takes MySQL 8.0.19 or later, for the derived table's column list, and a correlated one 8.0.14. An upsert's `Set` cannot read its own table on MySQL at all.

**`OUTPUT` on SQL Server.** SQL Server refuses it on a table with triggers, so a model that needs a key back cannot be inserted into one.

**Upserts on SQL Server.** There is no `ON CONFLICT`. Use `ant.Merge`, which ant writes with `WITH (HOLDLOCK)` there: without it, two `MERGE`s at once can both find no row, and both insert it.

**`MERGE` at once on PostgreSQL.** PostgreSQL's own documentation says a `MERGE` that inserts a row another one inserted meanwhile fails with a unique violation, which the application retries. `Upsert` has no such failure there.

**Retries.** `ant.IsRetryable`, and so `ant.Attempts`, take deadlocks and serialization failures on every database: `40001` and `40P01` on PostgreSQL, deadlocks on MySQL and MariaDB, error 1020 on MariaDB, which from 11.6 rolls a transaction back when a row it reads changed since its snapshot, deadlocks on SQL Server, and a busy database on SQLite.

**Writers at once on SQLite.** SQLite has one writer at a time. A second connection that writes fails at once with "database is locked" unless the DSN makes it wait, and a transaction that reads and then writes can fail even then, unless it takes the write lock when it begins: use `_pragma=busy_timeout(5000)&_txlock=immediate`. `PRAGMA journal_mode=WAL` lets readers go on while one writes; it stays with the file, so run it once rather than in the DSN, where every new connection would need the write lock to set it.

**Paging on SQL Server.** `OFFSET ... FETCH` needs an `ORDER BY`, so ant adds a meaningless one if the query has none. SQL Server refuses a `Limit` of zero.

**Times.** SQLite stores times as text, and SQL Server's `DATETIME2` has no zone, so ant binds UTC on both to keep one clock, whatever holds the time: `*time.Time`, `sql.NullTime`, an `sql.Null` of a time, a pointer to one, or an interface holding one, types built on them such as `ant.DeletedAt`, and arguments of raw queries, each element of an `IN` list included. A `driver.Valuer` of your own is passed as it is, so have it give a time in UTC. MySQL's driver does the same. `ant.Past` and `ant.Future` compare with the server's own clock, so run the server in UTC. On SQLite and MySQL, `CURRENT_TIMESTAMP` has whole seconds.

**Parameters.** Limits differ: 65,535 on PostgreSQL and MySQL, 32,766 on SQLite, and about 2,100 on SQL Server, where ant stays at 2,000. `InsertMany`, `SaveChanges`, and `Include` split their work to fit. Your own `In` with a long list does not.

**Types.** `postgres.Array`, `postgres.JSONSet`, `postgres.Vector`, and the rest of `dialect/postgres` need PostgreSQL. `Collate` quotes its name, which SQL Server does not accept.

## Drivers

Import a driver in your application. ant does not choose one for you, with one exception: `dialect/mysql` imports `go-sql-driver/mysql`, because it has no other way to read the driver's error numbers.

```go
import _ "github.com/jackc/pgx/v5/stdlib"     // PostgreSQL
import _ "github.com/go-sql-driver/mysql"     // MySQL and MariaDB
import _ "modernc.org/sqlite"                 // SQLite
import _ "github.com/microsoft/go-mssqldb"    // SQL Server
```

Use `parseTime=true` in a MySQL DSN, or times come back as bytes. Use `_pragma=foreign_keys(1)` in a SQLite DSN, or foreign keys are not enforced, and see above for writers at once.

`go-mssqldb`, as of v1.11.2, rolls a transaction back with the context it began with, and if that context has ended, it leaves a goroutine reading the connection after the connection went back to the pool, where the next statement races it. ant begins its own transactions, from `Tx`, `Begin`, `SaveChanges`, and `DryRun`, on a context that does not end, on a connection taken with yours, and rolls them back itself when yours ends, so they never meet this. A transaction you begin yourself and pass to `UseTx` can: on SQL Server, end it before its context does.

## Adding a dialect

A dialect implements `dialect.Dialect`. Its `Render` calls `dialect.Render`, and its `Syntax` method lists what differs from PostgreSQL, so it states only its differences. See `dialect/sqlite/sqlite.go`, which is about 70 lines. `dialect.RegisterErrors` connects its error codes. Migrations are a separate interface, `schema.Dialect`.
