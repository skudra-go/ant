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
| `DeleteReturning` | Yes | No | Yes | Yes | Yes |
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

**Generated keys.** MySQL has no `RETURNING`, so a key comes from `LastInsertId`, and other database-filled columns are read back with a second query. Rows with generated keys go in one at a time there, in both `SaveChanges` and `InsertMany`. That query finds the row by its key, so a model with no key keeps the zero values it was given in its database-filled columns there, and a key the database fills other than through `AUTO_INCREMENT` (a `Default` on the key) is refused with `ErrUnsupported`.

**`ON DUPLICATE KEY` on MySQL and MariaDB.** It has no target: any unique key triggers it, not only the columns you name. If a different key is the one that collides, `Upsert` cannot find the row by your columns, and reports that it wrote nothing. Name the only unique key that can collide. It also takes no `WHERE`, but an `Upsert` of a copy that carries a version checks it all the same: each assignment keeps the stored value unless the version matches, and a copy that is out of date changes nothing and gets `ErrConcurrency`. This reads the affected-row count, so leave the driver's `clientFoundRows` off, as it is by default.

**Writes that read their own table on MySQL.** MySQL refuses an `UPDATE` or `DELETE` whose subquery reads the table being written (error 1093), so ant wraps such a subquery in a derived table there, which MySQL reads first. A subquery with several columns that share a name, or that are expressions, takes MySQL 8.0.19 or later, for the derived table's column list, and a correlated one 8.0.14. An upsert's `Set` that reads its own table is refused even through a derived table, so there ant writes the rows as `INSERT ... SELECT`, which MySQL allows. A `SELECT` has no `DEFAULT`, so `InsertMany` with such a `Set` and a column left to its default is refused with `ErrUnsupported`.

**`OUTPUT` on SQL Server.** SQL Server refuses it on a table with triggers, so a model that needs a key back cannot be inserted into one.

**Upserts on SQL Server.** There is no `ON CONFLICT`. Use `ant.Merge`, which ant writes with `WITH (HOLDLOCK)` there: without it, two `MERGE`s at once can both find no row, and both insert it.

**`MERGE` at once on PostgreSQL.** PostgreSQL's own documentation says a `MERGE` that inserts a row another one inserted meanwhile fails with a unique violation, which the application retries. `Upsert` has no such failure there.

**Retries.** `ant.IsRetryable`, and so `ant.Attempts`, take deadlocks and serialization failures on every database: `40001` and `40P01` on PostgreSQL, deadlocks on MySQL and MariaDB, error 1020 on MariaDB, which from 11.6 rolls a transaction back when a row it reads changed since its snapshot, deadlocks on SQL Server, and a busy database on SQLite.

**A nested read-only `Tx`.** A `Tx` inside another is a savepoint. On PostgreSQL `ant.ReadOnly()` holds there until the savepoint ends; MySQL, MariaDB, SQLite and SQL Server cannot make a transaction read-only once it has begun, so it returns `ant.ErrUnsupported`. `ant.Isolation` on a nested `Tx` is `ant.ErrNestedIsolation` everywhere.

**`FirstOrCreate` in a transaction.** When another writer inserted the row first, it is read `FOR UPDATE`, so changing it next takes no further lock. On MySQL and MariaDB the insert that lost already holds a share lock on that row, so two such transactions can still deadlock there (MariaDB: error 1020); the error is retryable, so run them with `ant.Attempts`.

**Row locks.** `ForUpdate` and `ForShare` lock the rows a query returns. `Count`, `Exists`, the aggregates, and the total of `Page` read no rows to lock, and PostgreSQL refuses a lock on them, so they run without it on every database. With `IncludeJoined` the lock is on the query's own table (`FOR UPDATE OF`), since PostgreSQL refuses to lock the nullable side of a left join. MariaDB has no `OF` and locks the related rows as well.

**Writers at once on SQLite.** SQLite has one writer at a time. A second connection that writes fails at once with "database is locked" unless the DSN makes it wait, and a transaction that reads and then writes can fail even then, unless it takes the write lock when it begins: use `_pragma=busy_timeout(5000)&_txlock=immediate`. `PRAGMA journal_mode=WAL` lets readers go on while one writes; it stays with the file, so run it once rather than in the DSN, where every new connection would need the write lock to set it.

**Paging on SQL Server.** `OFFSET ... FETCH` needs an `ORDER BY`, so ant adds a meaningless one if the query has none. SQL Server refuses a `Limit` of zero.

**Averages on SQL Server.** `AVG` there returns its argument's type, so the average of an integer column would lose its fraction. ant writes `AVG(CAST(x AS FLOAT))`, and `Avg`, `AvgOf`, and `AvgRelated` give the same number on every database.

**`Recursive` on SQL Server.** A recursive `WITH` there has no `RECURSIVE` and joins its parts only with `UNION ALL`, which keeps a row each time it is reached and never ends a cycle. So each row carries the path of keys that reached it, a row already on its path is not followed again, and the query reads one row per key, as `UNION` gives elsewhere. The path also lets ant lift SQL Server's limit of 100 levels, with `OPTION (MAXRECURSION 0)`. `Count` and `Exists` put the `WITH` at the start of their statement, the only place SQL Server takes one.

**Long `In` lists.** A list of more values than the database takes parameters in one statement goes on PostgreSQL as one array, `= ANY($1)`, when its values are plain and of one type. Elsewhere, and for a list of secrets or of mixed types, the driver refuses it, at 65535 on MySQL and MariaDB, 32766 on SQLite, and about 2100 on SQL Server: use `InSub`, or split the list.

**Values the drivers cannot take.** These are limits of `database/sql` or of a driver, which ant does not work around:

- An `sql.Null[uint64]` above `math.MaxInt64` is refused, though a bare `uint64` field is not: pgx answers `uint64 values with high bit set are not supported`. On PostgreSQL a `uint64` column is `numeric(20,0)`, which holds every value; the other dialects store it in an integer column, where a value past `math.MaxInt64` does not fit.
- `ant.JSON` and `secure.Value` refuse text that is not valid UTF-8, as a text column does: `encoding/json` would store U+FFFD in its place.
- pgx writes a pointer to a nil `json.RawMessage` as the JSON `null`, not as `NULL`.
- pgx writes a 16-byte `net.IP` that holds an IPv4 address, as `net.ParseIP` returns, to an `inet` as `::ffff:10.0.0.1`, which `<<` against an IPv4 network does not match. Call `To4` on it, or use `netip.Addr`.

**Values ant converts.** `database/sql` cannot fill some types from what the drivers give, so ant reads them itself, wherever it scans a row:

- A `time.Duration`, `*time.Duration` or `sql.Null[time.Duration]` reads an integer as nanoseconds, and PostgreSQL's text for an `interval`, which pgx hands over, in each `IntervalStyle`. A year counts 365.25 days, a month left over 30 and a day 24 hours, as `EXTRACT(EPOCH FROM ...)` counts them. An infinite interval, or one past about 292 years, is an error. An interval keeps microseconds, so the nanoseconds below one are cut toward zero when pgx writes it.
- A `net.IP`, `*net.IP` or `sql.Null[net.IP]` reads bytes as they are, and the text of an `inet` or `cidr` as an address. One with a netmask, such as `10.0.0.1/24`, is an error: read it into a `netip.Prefix`.
- A named byte slice, such as `json.RawMessage`, reads `NULL` as nil, as a `[]byte` does.
- A `float64`, `*float64` or `sql.Null[float64]` reads an interval as its nanoseconds, so `Avg`, `AvgOf` and `AvgRelated` of a `time.Duration` give nanoseconds from an `interval` column, whose `AVG` is an interval, as they do from an integer one.
- A map with string keys reads JSON with `json.Number` for the numbers in its interfaces, as `ant.JSON` does, so a large integer keeps its digits.

**Times.** SQLite stores times as text, and SQL Server's `DATETIME2` has no zone, so ant binds UTC on both to keep one clock, whatever holds the time: `*time.Time`, `sql.NullTime`, an `sql.Null` of a time, a pointer to one, or an interface holding one, types built on them such as `ant.DeletedAt`, and arguments of raw queries, each element of an `IN` list included. A `driver.Valuer` of your own is passed as it is, so have it give a time in UTC. MySQL's driver does the same. A time read back is held in UTC on every database, in each of those shapes, though pgx decodes `TIMESTAMPTZ` in the process's zone. `ant.Past` and `ant.Future` compare with the server's own clock, so run the server in UTC. On SQLite and MySQL, `CURRENT_TIMESTAMP` has whole seconds.

**Parameters.** Limits differ: 65,535 on PostgreSQL and MySQL, 32,766 on SQLite, and about 2,100 on SQL Server, where ant stays at 2,000. `InsertMany`, `SaveChanges`, and `Include` split their work to fit. Your own `In` with a long list does not.

**Types.** `postgres.Array`, `postgres.JSONSet`, `postgres.Vector`, and the rest of `dialect/postgres` need PostgreSQL. A `postgres.Array` holds plain values, so reading an array with a NULL element is an error, not the text `NULL`, `false`, or zero. `Collate` quotes its name, which SQL Server does not accept.

**Column types.** On PostgreSQL, `EnsureCreated` and migrations give a type PostgreSQL has a column for that column: `time.Duration` is `interval`, `net.IP` and `netip.Addr` are `inet`, `netip.Prefix` is `cidr`, `json.RawMessage` and a map with string keys are `jsonb`, and `github.com/shopspring/decimal`'s `Decimal` is `numeric`. A slice of a type that maps is an array of it, as `[]string` is `text[]`, unless the slice is a `Valuer` or a `Scanner`, which says itself how it is stored. A `netip.Addr` or `netip.Prefix` column allows `NULL`, which pgx writes for the zero value. A `cidr` refuses a prefix with bits set past its mask: call `Masked` on it, or name the column `inet`.

Before ant mapped them, a `time.Duration` was `bigint` and a `net.IP` and a `json.RawMessage` were `bytea`. The next `diff` changes such a column, and converts its values as ant stored them: nanoseconds to an interval, the bytes of an address to `inet`, and JSON text to `jsonb`. To keep the column as it is, name its type, as `b.Column(&Hooks.Timeout).Type("bigint")`: the values read back from it as before.

## Drivers

Import a driver in your application. ant does not choose one for you, with one exception: `dialect/mysql` imports `go-sql-driver/mysql`, because it has no other way to read the driver's error numbers.

```go
import _ "github.com/jackc/pgx/v5/stdlib"     // PostgreSQL
import _ "github.com/go-sql-driver/mysql"     // MySQL and MariaDB
import _ "modernc.org/sqlite"                 // SQLite
import _ "github.com/microsoft/go-mssqldb"    // SQL Server
```

Use `parseTime=true` in a MySQL or MariaDB DSN: without it times come back as bytes, so `ant.Open` refuses the DSN. A pool passed to `ant.Wrap` is not checked. Use `_pragma=foreign_keys(1)` in a SQLite DSN, or foreign keys are not enforced, and see above for writers at once.

`go-mssqldb`, as of v1.11.2, rolls a transaction back with the context it began with, and if that context has ended, it leaves a goroutine reading the connection after the connection went back to the pool, where the next statement races it. ant begins its own transactions, from `Tx`, `Begin`, `SaveChanges`, and `DryRun`, on a context that does not end, on a connection taken with yours, and rolls them back itself when yours ends, so they never meet this. A transaction you begin yourself and pass to `UseTx` can: on SQL Server, end it before its context does.

## Adding a dialect

A dialect implements `dialect.Dialect`. Its `Render` calls `dialect.Render`, and its `Syntax` method lists what differs from PostgreSQL, so it states only its differences. See `dialect/sqlite/sqlite.go`, which is about 70 lines. `dialect.RegisterErrors` connects its error codes. Migrations are a separate interface, `schema.Dialect`.
