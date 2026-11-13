# Decisions

This file records where the implementation brief (`task.md`, removed from the repository after v1.0 and still in its history) was ambiguous or needed refining, and what was chosen. Where the brief left a choice open, the option closest to EF Core was taken.

The signatures were compiled with Go 1.27.3 against a stub on 2026-10-31.

## 1. `ant.Ordered` replaces `cmp.Ordered`

The brief declares `Gt[V cmp.Ordered]` and `OrderBy[V cmp.Ordered]`. `time.Time` does not satisfy `cmp.Ordered`, so `OrderBy(&Users.CreatedAt)` and `Gt(&Users.CreatedAt, t)` do not compile, although `ant.Base` supplies those columns. EF Core orders by dates.

Decision: define `type Ordered interface{ cmp.Ordered | time.Time }` and use it for ordering and comparison conditions. Other orderable types, such as UUID or decimal types, remain an open extension.

## 2. `Tx` is a generic method on `Context`

`db.Tx(ctx, func(tx *AppDB) error {...})` must hand back the user's type, but `Tx` is promoted from the embedded `ant.Context`, which does not know that type.

Decision: `func (c *Context) Tx[DB any](ctx context.Context, f func(*DB) error) error`. `DB` is inferred from the closure, and promotion through embedding compiles. A closure with the wrong context type is a runtime error returned by `Tx`.

## 3. `Find` takes a generic key

An `any` key would break the rule against `any` in the query API, and `Table[T]` has no type parameter for the key.

Decision: `func (t *Table[T]) Find[K comparable](ctx context.Context, key K) (*T, error)`. A key of the wrong type is returned as an error at runtime. This is a known limit.

## 4. Conditions are not tied to a model

The target API uses `ant.Gt(&Users.Age, 18)` with no model type, and `Where` takes plain `Cond` values. Adding a model parameter to `Cond` would make condition calls uglier.

Decision: keep `Cond` untyped by model. Probes from the wrong model, and non-probe pointers, are returned as errors at the terminal call. A `go vet` analyzer is already planned for v0.9.

## 5. `Select` closures run client-side

EF Core translates projections to SQL through expression trees. Go has no equivalent, so a closure such as `func(p *Post, u *User) Summary` cannot be inspected.

Decision: load the mapped columns of each table in the join and call the closure once per row. Column pruning needs a separate explicit API, to be proposed on the roadmap.

## 6. Return shapes and tracking

Change tracking requires a stable pointer to the loaded entity, and the target API mutates `user.Age++` after `Find`.

Decision: `Find` and `First` return `*T`. `All` returns `[]T`, and the tracker holds pointers into that slice's backing array. `Each` yields `T` by value and is not tracked. EF Core tracks streamed entities, but a value copy in Go cannot be tracked.

## 7. `Eq` with a nil pointer

`ant.Eq(&Users.Bio, nil)` compiles with `V = *string`.

Decision: nil renders as `IS NULL`, as EF Core does for null comparisons. A non-nil pointer compares the pointed-to value, not the pointer.

## 8. Step 1 file count

Step 1 of the brief says "three Markdown files" and lists four. All four were written.

## 9. Owner review of Step 1

Decisions 4 and 5 were accepted by the project owner on 2026-10-31. In decision 5, only the closure runs in Go. The join, filters, and ordering run in the database.

## 10. Module path

The module path is `github.com/skudra-go/ant`, taken from the repository remote. The owner gave `skudra-go/ant`.

## 11. Migration model

Status: approved by the owner on 2026-10-31. The owner asked for structs as the source of truth, a CLI, two folders, and no lock-in.

- **Source of truth:** the Go structs. A command compares them with a stored snapshot and writes the difference as plain SQL, which the developer can edit.
- **Snapshot, not a shadow database:** like EF Core, diffing uses a snapshot file in source control. Generating a migration needs no database and no permission to create one.
- **Two folders:** `migrations/versions` holds sequential files that run once. `migrations/code` holds repeatable SQL that reruns when its checksum changes, after all versioned files, as in Evolve and Flyway.
- **Plain SQL only:** no proprietary format. Any tool or person can read the files, and leaving ant keeps them usable.
- **Pluggable storage:** the runner reads from an `io/fs.FS` by default, so `go:embed` works. A small interface replaces file handling entirely. Unset options use defaults.
- **The CLI lives in the user's module:** Go cannot load structs from another module at runtime. A small `main` calls `migrate.Main[AppDB]`, so commands run as `go run ./cmd/migrate diff add_bio`.
- **Commands:** `diff`, `up`, `status`, and `check`. `check` fails when models and snapshot differ, for use in CI.
- **Renames:** a diff cannot tell a rename from a drop and add, so it writes a drop and add with a warning comment, and the developer edits the SQL.
- **No down migrations:** rollback is out of scope. To undo a change, write a new forward migration.

## 12. Additions to the `Dialect` contract

The brief lists five `Dialect` methods. `Open` also needs the `database/sql` driver name, so the interface adds `DriverName() string`. The application still imports the driver, so the core has no driver dependency.

## 13. MariaDB shares the `mysql` package

The brief's layout has no `dialect/mariadb`. `mysql.MariaDB` lives beside `mysql.Dialect`. It differs in `SupportsReturning`, which was confirmed against live servers: `INSERT ... RETURNING` works on MariaDB 11.8 and is a syntax error on MySQL 8.4.

## 14. The `Dialect` interface exposes an internal type

`Render` takes a `qb.Statement`, and the brief places `qb` under `internal`. The interface is pluggable inside this module, but a third-party module cannot implement it. The roadmap makes the AST public in v0.4.

## 15. Integration failures do not fail CI yet

The brief expects API tests to fail with `ErrNotImplemented`, so the integration jobs use `continue-on-error`. Remove this when v0.1 starts passing them.

SQLite has its own job, because it needs no service container.

## 16. Test schema is hand-written

`internal/testdb` creates the fixture tables with per-database SQL. Migrations exist now, but only on PostgreSQL, and the harness covers all five databases, so it stays hand-written until v0.4.

## 17. Implemented ahead of the skeleton

Some bodies are real because later behavior or the acceptance criteria depend on them: `Open`, table binding, probe resolution, condition and assignment construction, and builder error accumulation. Everything that runs SQL against a model returns `ErrNotImplemented`.

## 18. Wrong-model detection

`Where`, ordering, relations, and assignments compare each probe's model with the models in the query. A mismatch is `ErrWrongModel` at the terminal call.

## 19. A zero generated key means "assign one"

Decision 8 of the brief forbids reading meaning into zero values, but an integer primary key needs a way to ask for a generated value. As EF Core does, ant treats zero in a generated key as "assign one". This is the only exception. `b.Key(&Tickets.ID).NotGenerated()` removes it for a model, and non-integer keys are never generated.

## 20. Queries are immutable

Every `Query[T]` builder method returns a new query, so a query can safely be the base of several others. GORM's documentation warns about reusing chained statements, and this removes that risk.

A later `OrderBy` replaces earlier ones, and `ThenBy` adds to them, as in EF Core. `ThenBy` with no `OrderBy` is `ErrThenWithoutOrder`, because Go has no equivalent of C#'s `IOrderedQueryable` to catch it at compile time.

## 21. Configuration runs when a context opens

`Configure` cannot run inside `ant.Fields`, because it refers to probes that may not be initialized yet. It runs once, from `Open`, on first query, or from `Raw`. Errors in it stop `Open`.

AST nodes share a pointer to the table name, so a condition built before `Open` still renders the configured table.

## 22. Inserts run one statement per row

Several entities are written one at a time in a transaction, which keeps generated keys matched to entities and makes the call all or nothing. Generated keys are cleared again if the call fails. Faster batched inserts are planned in v0.5.

Until v0.3, `Insert` returns `ErrNotImplemented` if an entity has related entities set, rather than dropping them. `Include`, `IncludeOne`, and `Join` do the same.

## 23. Guarding bulk writes

`Update` and `Delete` return `ErrNoCondition` unless a condition can exclude rows or `Unfiltered` was called. Conditions that cannot exclude a row do not count, such as `ant.And()` or `ant.NotIn` with an empty list, because they usually come from an empty slice. The roadmap's original `q.All()` name was replaced, since `All` is already a terminal call.

## 24. Raw SQL arguments

The rules are in the `dialect.Bind` documentation. PostgreSQL has a `?` jsonb operator, so `?` is only a placeholder when the query has no `$n` and exactly one `?` per argument. A slice expands only when it fills an `IN (...)` list, so `= ANY(@ids)` keeps passing an array. An empty slice becomes `NULL`, so `NOT IN (?)` with an empty slice matches nothing, unlike `ant.NotIn`.

## 25. Scanning

Result columns with no field are ignored, and fields with no column are left alone, so `SELECT name` into a model works. `internal/scan` uses `Rows.Scan`, which calls `sql.ConvertAssign` itself, instead of the direct calls the brief describes.

`ant.Base` timestamps are written as they are until automatic timestamps arrive in v0.6, so a zero time is stored as `0001-01-01`.

## 26. Feature examples run on PostgreSQL only

`tests/features` holds one small example per feature. It uses PostgreSQL SQL for its tables, and skips other databases until v0.4. Each PostgreSQL test gets its own schema, so test packages can run in parallel against one server.

## 27. `All` returns `[]*T`

Decision 6 said `All` returns `[]T`. That made `for _, u := range users { u.Age++ }` silently lose the change, because `u` is a copy, and it made an identity map impossible, because one row could not be one instance. `All`, `First`, `Single`, and `Find` now all return pointers. `Each` and `Raw` still return values, and neither is tracked.

## 28. Identity and relation slices

As in EF Core, a tracked query returns the existing instance for a row, and `Find` returns it without a query. The exception is `[]Post` relation fields, whose elements are values. A row in one of those is a second tracked instance with its own snapshot, and changes to either are saved. Field-level updates mean the two do not overwrite each other unless both change the same column. When both are written in one save, the second writes over the version the first gave the row, and a row edited through one instance and dropped through the other is deleted, rather than either being refused as a concurrent change (decision 93).

## 29. Sessions

Applications usually keep one `db` for the life of the process, and a tracker on it would grow forever and mix up concurrent requests. `ant.Session(db)` returns a context with its own tracker on the same pool, which is the Go form of EF Core's short-lived `DbContext`. Use one per unit of work, or `ant.WithTracking(false)` on contexts that only use explicit updates.

## 30. Tracking follows keys, not addresses

Appending to a relation slice can move its elements, and `slices.Delete` shifts them in place, so an address says nothing about which row it holds. Every relation is reconciled by key before anything else in `SaveChanges`. A removed row is deleted or unlinked using a copy, of what its element holds if that is still the row, so an edit made before it left the slice is saved with the unlink, and of its snapshot otherwise, and its key comes from the entry, never from memory. An unlink moves the version and `UpdatedAt` on, as any update does. A child that was detached, or removed, is not orphaned again.

## 31. Failed saves

`SaveChanges` runs in one transaction, or a savepoint inside one, even for a single statement, as EF Core does, since PostgreSQL aborts the whole transaction when one statement fails. On failure nothing is saved, generated keys are cleared, and pending changes remain, so the call can be repeated. A rollback of an enclosing transaction also reverses what the tracker recorded as saved, which EF Core does not do. An entity whose row vanished is `ErrConcurrency`, and a changed primary key is `ErrKeyChanged`, rather than a write to the wrong row.

## 32. Navigation and foreign key changes

A relation that still holds what was loaded does not touch the foreign key, so `post.UserID = 5` works with `post.User` loaded. A row removed from one loaded relation and added to another is moved, not deleted, whether the other relation was loaded or not, and whether its parent is new in the same save or not. A row in a new position only updates its foreign key. A pointer to a parent that held one when last loaded or saved, and is empty now, unlinks; one that was empty leaves the foreign key to the application.

## 33. Include shape functions

`Include` takes an optional function over the related query, instead of separate APIs for filtering, ordering, limiting, and nesting. `Limit` and `Offset` apply per parent, using `ROW_NUMBER`. `Each` refuses includes with `ErrIncludeInEach`, because a second query cannot run on a connection that is streaming rows inside a transaction.

## 34. Self-joins

The second occurrence of a table gets an alias, so each side scans into its own row. Probes cannot tell the two sides apart, so a condition on a self-joined model applies to the first side.

## 35. Typed expressions

`Expr[V]` is the one abstraction for anything with a value: a column, an aggregate, a subquery, or raw SQL. Its methods build conditions and orderings, and `IntoExpr` maps it to a result field, so `V` is checked by the compiler throughout. `Project` is how grouped and computed results are read.

## 36. Derived tables keep the model's name

`From`, `FromCTE`, `FromRaw`, `Union`, and `Recursive` alias their source as the model's table, so probes work on the outer query unchanged. Their results are never tracked, because a derived row may not match a stored row. `Recursive` uses `UNION`, not `UNION ALL`, so a cycle in the data ends.

## 37. References to outer queries

Probe checks run at the terminal call, not as conditions are added. That lets `Where` come before `Join`, and lets a subquery refer to its outer query: unresolved models pass to the outer query, which checks them in turn.

## 38. Bulk writes

SQL has no `LIMIT` on `UPDATE` or `DELETE`, so a limited bulk write selects its rows by key in a subquery. So does a bulk write on a derived source, such as `From`, `FromCTE`, a set operation, or `Recursive`, because `UPDATE` and `DELETE` name only the table: without the subquery, the outer conditions would run against every row of it. The source must select the model's stored key, so a write refuses one narrowed by `Column` or `Omit`, `Values` of a model whose key is `writeonly`, and `FromRaw`, whose columns ant cannot see. A set operation matches columns by position, so its sides must hold the key in the same place. Without that check, SQLite, MySQL, and MariaDB resolve the missing key to the table being written, and change every row. The key subquery drops the ordering unless it pages, because order cannot change which rows are members, and SQL Server refuses it there.

The guard of decision 23 counts a derived source as filtered when its rows are. `From`, `FromCTE`, and `Recursive` take their source's, a union needs both sides filtered, an intersection either, and `EXCEPT` its first, since a second side that matches nothing leaves every row. Replacing the source replaces what it counts for, while the outer query's own conditions still count. SQL Server allows `WITH` only at the start of a statement, so a write after `FromCTE` or `Recursive` returns `ErrUnsupported` there. MySQL refuses a write whose subquery reads the table being written (error 1093), but it materializes a derived table there rather than merging it, so a derived source needs nothing more. Other reads of the table are decision 85's.

`InsertMany` matches generated keys to rows by sorting them, since one statement draws sequence values in row order but `RETURNING` promises no order. It does not copy keys with a conflict clause, where rows may be skipped.

## 39. Global filters are lazy

Filters can read the request context, such as a tenant, but subqueries are built before any context exists. A filter is a `qb.Lazy` node that the renderer resolves with the terminal call's context. So filters reach joins, `Include`, `Has`, and subqueries without anything having to remember them, and the same query object run under two tenants sees different rows. On a joined table, filters go in the `ON` clause, so they do not turn a left join into an inner one.

`Table.Find` skips the identity map for models with filters, because a tracked row may be one the filter now hides.

## 40. Soft deletes are a filter

A field of type `ant.DeletedAt` makes `Delete`, `DeleteByID`, and `Remove` mark rows, and adds a built-in global filter. `ForceDelete` removes rows for real.

## 41. Automatic timestamps

`time.Time` fields named `CreatedAt` and `UpdatedAt` are maintained on any model, as in GORM and Eloquent. They are stored in UTC, truncated to microseconds, so a value reads back exactly as written. A value the application set on insert is kept. `b.NoTimestamps()` opts out.

## 42. Converters are Scanner and Valuer

The roadmap planned `Convert(to, from)` functions. Go's `sql.Scanner` and `driver.Valuer` already do this, work in conditions without extra machinery, and need no ant API, so named types and those two interfaces are the mechanism. `ant.JSON[T]` and `postgres.Array[T]` are built that way.

## 43. Database-specific helpers

The root package stays neutral. PostgreSQL-only conditions and types live in `dialect/postgres`, built from the public `ant.SQL` and `ant.SQLCond`, so a dialect can add helpers without changing the core.

## 44. Many-to-many

A relation with `b.ManyToMany` goes through a join table with no model. `Include` reads the join table, then the targets. In a loaded relation, removing an element deletes the join row, never the target, unlike a one-to-many relation. For extra columns on the join table, model it, with two one-to-many relations.

## 45. Errors are classified, not wrapped

Roadmap v0.4 planned sentinels such as `ErrUniqueViolation`, which would need every error path to wrap driver errors, including those that only appear while reading rows. Instead `ant.IsUniqueViolation(err)` and its siblings read the SQLSTATE from the error chain, so nothing can be missed, and the driver's own error stays intact.

## 46. Interceptors

There is one point where every statement passes: the executor. `ant.Interceptor` wraps it with `Before` and `After`, which is enough for logging, metrics, tracing, and refusing or rewriting statements. `WithLogger` is built on it. OpenTelemetry and Prometheus packages are not included, because they would add dependencies. They are thin interceptors that can be added as sub-packages.

Values stay out of logs unless `ant.LogValues()` is used.

## 47. Hooks run inside the write

Hooks are methods on the model, found once per type, so models with none pay nothing. They run inside the unit's transaction, so an error from an After hook undoes the write, and a unit with After hooks always uses a transaction. Bulk `Update`, `Delete`, `InsertMany`, and `Raw` do not run them, because they do not load entities.

`Observer`s run after the commit, and never hear about a rollback.

## 48. Replicas

`WithReplicas` sends plain reads outside transactions to replicas in turn. A locking read, a transaction, `OnPrimary`, or a recent write with `WithStickyWrites` uses the primary. A failing replica is an error for that read, not a silent fallback.

## 49. PostgreSQL first

The owner chose to focus on PostgreSQL, so it has everything, and the feature suite runs on it alone. All SQL generation goes through the shared renderer and the `Dialect` interface, which is what let the other databases follow in [decision 74](#74-dialects-state-their-differences). Migrations for them are still open.

## 50. Three layers for migrations

`schema` describes a database as plain data and works out changes, with no SQL execution and no driver. `dialect/postgres` turns changes into SQL and reads live databases. `migrate` runs files and is the CLI.

`schema` is public, not internal, so a project can replace SQL generation with `Config.Render` and read a plan's changes. Migration support is a second interface, `schema.Dialect`, so the dialects without it still compile and return `schema.ErrUnsupported`.

## 51. Modeling added for migrations

Migrations need facts the structs did not hold, so parts of v0.6 arrived early: `b.Column`, a real `b.Index`, `b.IndexExpr`, and `b.Check`. `ant:"default=expr"` and `.Default(expr)` say what the default is. `ant:"default"` with no expression still works at runtime, and is an error when generating, since a column with no default would fail on the first insert.

`.Default` means what `ant:"default"` means: a zero field is left for the database. EF Core's `HasDefaultValue` does the same.

Whether a column allows NULL follows the Go type, including `sql.Null[T]`. A nil slice is written as NULL, so slices are nullable, and `ant.JSON` is not. A type with no obvious column is an error that says what to do. `schema.Typed` lets a type declare its own.

## 52. New NOT NULL columns fill existing rows

A plain `ADD COLUMN ... NOT NULL` fails on any table with rows, so a generated migration would work in development and break in production. Diff adds the column with its Go zero value as a default, then drops the default, so the column ends up as the model declares it. The value is in the file with a warning, and is easy to change.

`timestamptz` uses `now()`, and `uuid` uses `gen_random_uuid()`, because their zero values would be misleading. A type with no sensible value gets a warning instead.

## 53. Names follow PostgreSQL's defaults

Constraints and indexes are named `users_pkey`, `users_email_key`, `posts_user_id_fkey`, and `posts_user_id_idx`, as PostgreSQL itself names them, so a table written by hand usually needs no configuration to adopt. A name past the identifier limit is shortened with a hash, because the server would cut it silently and two names could become one.

## 54. Foreign keys

`OnDelete` defaults to the database's `NO ACTION`. EF Core cascades required relations by default, which silently deletes rows, and decision 8 prefers saying things out loud. The two sides of a relation are one key, either can configure it, and a contradiction is an error.

Foreign keys get an index unless one already leads with the column, as in EF Core. PostgreSQL does not make one, and a delete on the parent then scans the child. `.NoIndex()` opts out.

## 55. Renames

Decision 11 chose a drop and add with a warning. That stays the default, because a diff cannot know. Three things were added, since a warning alone loses data if missed: a terminal prompt, `--rename table.old=new`, and the exact `RENAME` statement in the warning, for editing the file by hand. Renames of objects follow by themselves: a constraint or index with the same definition under a new name is renamed, not rebuilt.

## 56. Failure

A migration runs in a transaction with its history row, so a crash cannot leave it half done, or done but unrecorded. A failure leaves no trace, and fixing the file is enough.

`-- ant:no-transaction` is for statements PostgreSQL refuses in a transaction, such as `CREATE INDEX CONCURRENTLY`. A failure there can leave work behind, so it is recorded, and `up` refuses to continue until `repair`, as Flyway does.

Connections keep settings past a commit, and a `pg_dump` file clears `search_path`, so each migration ends with `RESET ALL`.

## 57. Drift compares what the server stored

PostgreSQL rewrites what it is given: `int` becomes `integer`, and `x IN (1, 2)` becomes `x = ANY (ARRAY[1, 2])`. Matching that by hand would never be complete, and a tool with false alarms gets ignored.

So drift creates the expected tables as temporary tables in a transaction it rolls back, and reads them back, which needs no extra permission and leaves nothing. The roadmap rules out shadow databases for generating migrations. This is narrower: it only spells expressions, only for `drift`.

That is not quite enough, because the server prints one check in two ways, depending on whether it was typed or restored from a dump. When text differs, `SameExpression` compares with casts and parentheses removed. It can miss a change that only regroups an expression, which is the better mistake.

`drift` uses the snapshot when there is one, since that is what migrations produce. With none, it uses the models, which is what makes the adoption loop work.

## 58. Squashing needs no coordination

`-- ant:replaces 0006` at the top of a baseline says what it replaces. A database that has those migrations records the baseline without running it, and a new one runs it, so nobody has to run a command per environment, and rows for deleted files stay valid. A database partway through is refused, because neither path is right for it.

A squash writes from the models, which loses anything written by hand, so it warns, and `--keep` joins the old files instead.

## 59. Adopting an existing database

Generating Go structs from a live schema is not offered. The mapping is ambiguous, since `text` could be `string`, `*string`, a named type, or an enum, and relations cannot be read from foreign keys alone. A person or an assistant does this better, given a dump and a way to check the work.

So ant provides the check: `drift` works with no snapshot, and says exactly what still differs, so the loop ends when it reports nothing. `adopt` then makes a dump the first migration, removing psql commands that recent `pg_dump` versions write, and `baseline` records it on existing databases. The guide has a prompt to start from. The `migrate scaffold` item in v0.9 needs revisiting in light of this.

## 60. One program, two jobs

`go run ./cmd/migrate` writes files, and a deployed binary has no folder to read. `Config.Embedded` is used only when the folder is missing, so one `main.go` does both, with no flag to forget.

## 61. Sessions take options

`ant.Session(db)` copied a context's settings, including `WithTracking(false)`, so `SaveChanges` on such a session did nothing and returned nil. An application needs tracking off on its shared context, or it grows without end, so this is the normal setup. `Session` now takes options, and `ant.Session(db, ant.WithTracking(true))` is the unit of work. The blog example found this.

## 62. The statement AST is public

`internal/qb` moved to `qb`, because a dialect outside this module has to render it. It stays outside the stability promise until one exists, since only a real one will show whether the shape is right.

## 63. SaveChanges batches inserts

This revises [decision 22](#22-inserts-run-one-statement-per-row). New rows of one model go in together, up to 200 a statement, which turns a thousand round trips into a few. A model with insert hooks or a `.Touch()` relation is written row by row, as is a save with `WithoutAutoTx`, because each of those needs a result per row. Updates stay one statement each: a shared statement could only set one list of columns.

## 64. Through relations are read-only pivots

`HasManyThrough` reuses the many-to-many loader with a join on the intermediate table, marked read-only, because writing through it has no one obvious meaning.

## 65. Scoped relations use the same filter path

`.Where` on a relation, `.Morph`, `.Latest`, and `.Oldest` all add conditions wherever the relation is used, in `Include`, `Has`, and joins. A polymorphic relation has no foreign key, since one column cannot point at several tables.

## 66. Several models can share a table

`b.Discriminator` and table splitting are the same fact to migrations: models on one table merge into one definition, and a column not every model has allows NULL. Query results stay one type per model. There is no `[]any` of mixed subtypes.

## 67. Naming and conventions are global

`WithNaming` and `WithConvention` apply once per model, in the first `Open`, because a model is reflected once per process. Per-connection naming would need a second model registry, for a need nobody has.

## 68. Sensitive values are wrapped when bound

A `.Sensitive()` field's value is wrapped in `qb.Secret` as it enters the statement, so it prints as `[redacted]` wherever it travels. Looking the column up at logging time would miss conditions and raw paths.

## 69. Errors carry their SQL

`exec.Error` holds the statement, with placeholders and no values, so `ant.FailedSQL` can log it. It stays out of the message unless `WithDetailedErrors` is set, because messages are what reach users.

## 70. DryRun really runs

`ant.DryRun` runs your code in a transaction that rolls back, rather than faking results. A simulated run cannot know what a trigger or a constraint does. The cost is stated in its documentation: sequences advance, and anything outside the database still happens.

## 71. Dependencies stay out of the root package

The root package uses the standard library. Code that needs more goes elsewhere: `pgxext` for pgx, and separate modules for OpenTelemetry (`contrib/otel`), the analyzer (`tools/antvet`), and the benchmarks (`tools/compare`). `Batch` left the root package for this reason, as the roadmap's `ant.Batch` needed pgx. `dialect/mysql` imports its driver, as the error numbers are not reachable any other way, and only a program using MySQL compiles it.

## 72. Partitions are not migrations

Migrations create a partitioned parent, and `postgres.MonthlyPartitions` manages its months. Partitions come and go on a schedule, which a file history cannot describe, so `drift` ignores them.

## 73. Drift compares what models declare

`drift` reports enums and sequences only if a model declares them, so a type created by hand, or by an extension, is not reported for removal. Tables work the other way, because a table no model describes is what drift exists to find.

## 74. Dialects state their differences

A dialect's `Syntax` lists how its SQL differs from PostgreSQL's, and the zero value changes nothing, so PostgreSQL's output cannot move when another dialect is added. Differences are data, in one place, rather than a renderer per database.

A feature a database lacks is reported as `ErrUnsupported` before sending anything. Where the meaning can be kept exactly, ant emulates it instead: `FILTER` becomes a `CASE` inside the aggregate, and `ILIKE` a `LIKE` between lowered values. Where it cannot, ant refuses. It does not emulate `FULL JOIN` on MySQL, or upserts on SQL Server, because those would quietly change concurrency behaviour.

Error codes are translated to SQLSTATE values through `dialect.RegisterErrors`, so `IsUniqueViolation` takes no dialect.

## 75. Generated values without RETURNING

MySQL gets keys from `LastInsertId`, which reports one key per statement, so rows with generated keys are written singly there. `ON DUPLICATE KEY` returns the old row even when it changed nothing, so on MySQL and MariaDB, `Upsert` decides from the row count, and reads the row back by the conflict's columns.

## 76. One clock for times without a zone

SQLite compares times as text, and SQL Server's `DATETIME2` drops the zone, so a local time stored there sits hours from a UTC one and compares wrongly with no error. Those dialects bind every time in UTC.

## 77. Layout is enforced by tools

The owner wants code with room in it: a field per line, a blank line before a `return`, and an `if` kept apart from a statement it does not check. Reviewing that by hand does not last, so `make fmt` does it, and `make lint` fails on anything it would change.

`golines` breaks long lines at 100 columns, and `wsl_v5` and `nlreturn` handle blank lines. `tools/antfmt` covers what they leave: they only look at line length, so a short line of fields passes, and `wsl_v5` lets an `if` sit under a statement it has nothing to do with. antfmt only adds line breaks and trailing commas, so it cannot change what code means. It needs no dependencies, so it stays a small module beside `antvet`.

A literal with four or more keyed fields gets one per line, because three short ones read fine on a line, and test tables would grow a lot if they did not. Positional rows stay on one line for the same reason. Long strings and SQL stay whole.


## 78. A replaced column takes its dependents along, and they come back

PostgreSQL 17 can change a generated column's expression in place, with `SET EXPRESSION`, but ant supports 13 and later, and a migration is written without knowing which server will run it. So a new expression, or a column that becomes generated, is a `DROP COLUMN` and an `ADD COLUMN`. The plan writes them as a `DropColumn` with `Again` set, before other columns change, and an `AddColumn` after (decision 94). `AlterColumn.Replaces` still says when, for code that builds its own changes, and the PostgreSQL dialect follows it exactly.

The drop takes indexes, unique and check constraints, the primary key, and the comment with it, and is refused while a foreign key in another table points at the column. A snapshot still lists all of these, so `check` passed while the database had lost them, and only `drift` would have found it. The plan now drops each of them before the column and makes them again after. An object counts as using the column if it names it in its columns, its included columns, an expression, or a predicate. Expressions are matched as text, by whole identifier, in double quotes, or spelled in Unicode escapes such as `U&"sl\0075g"`, and erring toward a match costs only a rebuild that was not needed. That is also why an escaped name is decoded with every character in it as the escape, rather than the one `UESCAPE` picks: comments may stand between the two, and a wrong guess can only find a match too many.

A foreign key depends on the unique key it points at, and PostgreSQL will not drop that key while the foreign key exists. Which key a foreign key chose is only known to the server, so whenever a plan drops a primary key, unique constraint, or unique index on the same columns, in any order, the foreign key is dropped first and made again last. That covers a unique index rebuilt because a replaced column is one of its included columns, and one changed for any other reason.

A plan that replaces a column builds and drops every index without `CONCURRENTLY`, not only those on the column. A single online statement takes the transaction away from the whole file, and half a migration could leave the column replaced with its indexes and constraints gone, which is the failure this decision is about. For indexes on the replaced column nothing is lost, since the replacement holds an exclusive lock on the table until the end anyway. An index elsewhere does block writes while it builds, so the file warns about each one that asked to be concurrent, and says to move it to a migration of its own. Splitting the file automatically was considered and rejected: moving online statements later breaks a foreign key that needs a new unique index, and a later `DROP INDEX` can find the index already gone with its column.

A column that stops being generated is not replaced: `DROP EXPRESSION` (PostgreSQL 13) keeps its values and everything on it. Before this, it was dropped and added empty, which lost the data, or failed on a `NOT NULL` column with rows.

## 79. A tracking context serves one operation at a time

A tracker hands every load the same instance of a row, and `Include` writes the related rows into it. Two requests sharing one tracking context therefore wrote into the same entities at once: each reset Alice's posts and appended its own, so both saw the posts twice, and two writes to the same entry's map of loaded relations crashed the process. Locks around ant's own writes cannot fix this, because the request that loaded Alice reads her posts after its load returns, while the next request writes them.

So a tracking context is one unit of work, as EF Core's `DbContext` is. An operation that loads into the tracker or saves from it claims the tracker, and another operation that starts meanwhile gets `ErrConcurrentUse` rather than waiting, because waiting would hide the mistake behind slow requests. The claimed operations are tracked reads, including `Find` from the identity map, and `SaveChanges`. `Insert`, `Upsert`, `DeleteByID`, `Load`, `LoadMissing`, and `Reload` claim whenever the tracker holds anything, and before they read the entities they are given, whose relations another operation may be loading: with tracking off, `Add` and `Attach` still fill it, and the entity they write may be in it. Untracked work on an empty tracker never claims, so a context opened with `WithTracking(false)` is safe to share, and `ant.Session(db, ant.WithTracking(true))` is the unit of work, as decision 29 says.

A hook runs inside the operation that called it, and its queries share the claim through the `context.Context` the hook receives. The context carries a token for that one operation, under a key for that tracker, so a hook that goes from one context to another and back keeps both claims, and a context kept after its operation ended claims nothing. Goroutines started inside an operation with its context share the claim, so they must not use the tracker at once. A query made with another context, such as `context.Background()`, looks like a second operation and is refused: Go has no goroutine identity to tell the two apart, and the error says what to do. `Chunk` claims each batch separately, so its callback may query. The helpers that read or mark the tracker take a context too, and claim it, as below.

A rollback reverses what `SaveChanges` recorded in the tracker, which writes to the same entries an operation would, so it needs the claim too. It cannot be refused, and it must not wait either: the operation holding the tracker may itself be waiting for the connection the rolling-back transaction holds. So the reversal runs at once if its caller holds the tracker or the tracker is free, and otherwise when the holding operation lets go, or when the next operation claims it, whichever comes first. Each recorded change names its tracker, because a session made inside a transaction has a tracker of its own. A late reversal can find its entry saved again by the operation it waited for, such as a second insert of the same entity, and that write may be in a transaction of its own that rolls back too. So the tracker learns how each write ends: a transaction keeps its changes when it commits and takes them back when it rolls back, and a write outside one is kept at once. Each entry holds the writes to its columns, and those to its relations, that have not ended yet, oldest first, and settles them newest first, each once every write after it has ended: the order one transaction undoes its own writes in, so the result does not depend on the order the transactions end in. A write that rolls back with no kept write after it puts back what it replaced, so two transactions that insert one new entity and both roll back leave it new, and the next save inserts it. One that rolls back under a kept write cannot: that write is in the rows, and an entry it left unchanged becomes `Modified`, since neither write knew what the other left in the database, and the next save writes every column, which is right either way. Settling them in the order they end instead got three writes wrong: with A, B, and C, where only B commits, A's late reversal ran first, and C's restore then brought back what A had taken out, both a member A added and the `Modified` that A's rollback had set. A kept write goes as soon as it is settled, and the write under it records that a kept one came after it, so an entry holds no more than the writes of transactions still open and those below them, and a history whose writes all ended lets go of its array. Every order of up to four writes, each committing or rolling back, is tested against what a single transaction would leave. Within its own transaction a rollback reverses everything, newest first, reads included: a `Reload` records its own reversal, which puts back the state and snapshot it replaced, and each value it replaced unless the application changed it since, since the rows it read go with the rollback, and the relations' bookkeeping comes back as the save found it, since a load inside the transaction read rows the rollback takes back, and one outside read the committed rows it restores. So a `Reload` or `Load` between a save and its rollback leaves an insert pending again, and a removal pending. The values a `Reload` puts back go where the entity lives by then, which a save moves when the entity's slice grows. A save after it does stop the bookkeeping coming back whole, as it does an entry's columns: while that save is open the reversal waits, and once it commits, the reversal takes back only what its own save changed and the newer save left alone, so a post the newer save added stays a member, and taking it out of the slice deletes its row. A deletion is reversed late the same way: each row whose entries the tracker forgot has a history of its deletions, settled as an entry's writes are. A reversal with a kept deletion after it leaves the row untracked, as nothing holds it any more, so a tracked `Find` reads it afresh rather than handing back the deleted entity, and one with no kept deletion after it tracks the entries again once the deletions after it have ended, so two transactions that delete a row and both roll back, the older first, leave the older one's removal pending. Counting deletions instead dropped that removal as soon as a newer deletion began. The history goes once its deletions have ended, so deleting row after row keeps nothing. A removal asked for after a save or a reload outlives its rollback: an entity that was new is forgotten, as its row never stayed, and a stored one stays `Deleted`, so the next save deletes the row the rollback brought back. Releasing a claim lets go of the tracker even if queued work panics, so a bug there cannot leave the tracker held for good, and a tracking callback of the application's that panics leaves the tracker's lock free.

A rollback also takes back what the save wrote into the entities, in the order the tracker undoes the save, and only when no kept write came after it: a key the database or a generator gave the entity, the timestamps, the version it moved on, and the discriminator, unless the application changed one since, and the tracker forgets a generated key it knew the entity by. Before, a rolled back insert left the generated key in the entity, so a post appended to a loaded relation looked stored and the next save never inserted it, and a new entity was inserted again with the old key given explicitly, which SQL Server's `IDENTITY` refuses. The values go where the entity lives by then, as do a reload's, and an entity a save began tracking is let go at that address too.

A transaction from `UseTx` ends where ant cannot see it, so what ant saves in it is kept at once, as outside a transaction, and its observers, which never run, are not queued: holding them grew the tracker's histories and the transaction's lists for as long as the context lived. A savepoint ant opens in it is ant's own, and its rollback is taken back as usual.

A transaction, and a context from `UseConn`, is one connection, which runs one statement at a time, and sharing one between goroutines that run at the same time is not supported; goroutines may hand it from one to the next, as with a `*sql.Tx`. ant does not make the turns for them: statements from two goroutines at once fail in the driver, which is a clear error, but savepoints from two goroutines would interleave on the connection's one stack, so one's rollback would take back the other's work, and serializing them would need every hook that queries to pass on its context, or wait for itself.

The helpers that read or mark the tracker, `Add`, `Attach`, `Remove`, `MarkModified`, `Detach`, `TrackGraph`, `Tracked`, `Entry`, `HasChanges`, `ClearTracked`, and `DebugTracked`, take a `context.Context` and claim the tracker as operations do, so one called while another operation runs gets `ErrConcurrentUse`, and one called from a hook shares its operation's claim. Without a context they could not tell a hook from another goroutine, and they read and wrote the entries that a save was writing. Those that returned nothing return an error now, and `Entry` returns a snapshot taken under the claim, so its `State`, `Changed`, `Original`, and `IsDirty` read nothing the tracker changes later. `HasChanges` plans the save `SaveChanges` would make, without writing or changing an entity, so it also sees an entity added to, taken out of, or moved between loaded relations, which it missed when it looked only at the entries.

The concurrency suite in `tests/concurrency` runs what servers do, on every database and under the race detector: a shared context read and written from many goroutines, small pools, cancelled requests, a session per worker, version conflicts retried, transfers that lock rows, upserts and `FirstOrCreate` racing for one key, transactions side by side, hooks, observers, events, interceptors, replicas, models first used at once, and a tracking context shared by mistake. It found three things on the databases, and one in a driver. go-mssqldb, as of v1.11.2, rolls a transaction back with the context it began with, and when that context has ended, as a cancelled request's has, the driver leaves a goroutine reading the connection after it went back to the pool, where the next statement races it; `database/sql` keeps such a connection, since the driver can reset its session. So ant begins the transactions it ends itself on a context that does not end, on a connection taken with the caller's context, which still bounds the wait for one in a full pool, and rolls the transaction back from a goroutine of its own when the caller's context ends, as `database/sql` would, but on a context the driver can finish on. Statements still run with the caller's context, a commit after that context ended rolls back instead, and the connection goes back to the pool when the transaction ends. A transaction from `UseTx` began outside ant and keeps the driver's behavior. On the databases: SQL Server's `MERGE` let two at once both insert a row neither found, so ant writes it `WITH (HOLDLOCK)`, which locks the key ranges it reads until it ends, since `MERGE` is SQL Server's only upsert. MariaDB from 11.6 reports a row changed since a transaction's snapshot as error 1020 and rolls the transaction back, which is now retryable, as a serialization failure. And `FirstOrCreate` in a transaction re-read the row that stopped its insert with the transaction's snapshot, which at MySQL's default `REPEATABLE READ` predates that row, so it returned the unique violation: the re-read now takes a lock, `FOR SHARE`, or `FOR UPDATE` on MariaDB, which reads the newest row.

`Include` also builds each parent's relation apart and sets it in one write, so a parent never shows half its rows, and drops repeated parents, which `Load(ctx, &Users.Posts, alice, alice)` used to give every post twice.

## 80. Every write moves the version on

An upsert that updated copied every column from the row it tried to insert, the version included, and `Insert` starts a version at 1. So the stored version went back to 1, and a copy loaded at version 1, two saves earlier, matched again and saved over both. `DoUpdate` and `Set` left the version where it was, which hid the upsert from every copy loaded before it. `Merge` did the same, and `InsertMany` with a conflict shares the upsert's clause.

Now each of them moves the stored version on by one when it updates a row, as `SaveChanges` and bulk `Update` do, and a row `Merge` inserts starts at 1 unless the source gives a version. The version is ant's to keep, so naming it in `DoUpdate`, `Set`, or a `Merge` assignment is an error before anything runs.

An entity passed to `Upsert` with a version of its own is a loaded copy, so its update applies only while the stored row still has that version, and otherwise `Upsert` returns a `ConcurrencyError`, as `SaveChanges` would. A zero version is a new entity, with nothing to check. `ON DUPLICATE KEY UPDATE` takes no condition, so on MySQL and MariaDB the version moves on but the copy's update goes ahead. `InsertMany` cannot tell a row's own version from the 1 it stamps on a new one, so it moves versions on without checking them.

## 81. A set operation's members keep their own paging

`Union` built its statement from the left query and appended the right one, so the left query's `ORDER BY`, `LIMIT`, and `OFFSET` were written after the right member, where SQL applies them to the whole set. The youngest two users united with the oldest came back as the youngest of all three, a `Delete` on such a set removed rows outside it, and an ordering by a table's column failed on PostgreSQL, which orders a set only by its output columns. SQLite writes members without parentheses, so there the right member's paging escaped too, and SQL Server and SQLite refuse an ordered member outright.

Each member is now built on its own. A paged member becomes a derived table, `SELECT * FROM (... LIMIT 2) AS users`, which every database reads as one member. A member that orders without paging loses its order, since order cannot change which rows a set holds. `Recursive` takes its starting rows the same way. `TestConformanceSetMembers` checks every pair of six member shapes under each of the four operations, on all five databases.

## 82. A rename must land on a name the models have

`--rename` is there to keep data, but a table rename never checked its new name. `--rename users=peple` renamed the table to the typo, created `people` empty from the models, and dropped `peple` as a table the models no longer had, with every row in it. Column renames checked the models, but not the snapshot, so a column renamed onto one that already existed planned a statement the database refuses.

Now a rename's new name must be a table or column the models have, in the table's own schema, and one the snapshot does not have yet, which also catches two renames onto one name and a rename to the same name. Otherwise `diff` stops before writing anything, and the error lists the tables or columns that are new in the models, since a typo is usually one of them. A rename into another schema is refused for the same reason: the plan would move nothing, and drop the table.

## 83. antvet judges only what it can see

`antvet` flagged valid code in ant's own tests, so nobody could run it on a project and trust the result. Three things caused it. Models were named with `types.TypeString`, which since Go 1.23 keeps an alias's own name, so a query of `type User = fixture.User` was "a query of User" that a probe of `fixture.User` did not match. `Find` keys were checked against a field named `ID`, though `Configure` can make another field the key. And a condition on another model was reported in any query, though a subquery may name its outer query's model, as decision 37 allows.

Now models are compared as types, with `types.Identical`, which sees through aliases at any depth, so `Row[[4]Key]` with `type Key = int64` is `Row[[4]int64]`, and which tells two models of one name apart, such as a type declared again in an inner block. Reports and facts name a model with its aliases resolved, and reports leave packages out, as in `Row[Code]`. Calls through a dot import are recognized like any other. A model's `Configure` method is read for `Key`, `GenerateKey`, `CompositeKey`, and `NoKey`, if it is one ant calls, which takes one `*ModelBuilder` and nothing else, while a `Configure` the model gets from a struct it embeds, which ant finds by reflection too, leaves its key unknown, and unchecked, and the key it sets, which an alias of the model has too, and which a `Find` key is judged against by `Find`'s type argument, given or inferred, as ant judges it, so `Find[any]` is reported, travels to other packages as an analysis fact, the way probes already did, with its whole path, so `Key(&C.Identity.Code)` is not taken for a top-level `Code`. Only a key set by one statement that always runs counts, chained calls such as `.NotGenerated()` included, and written as a method expression too, `(*ant.ModelBuilder[C]).Key(b, &C.Code)`, whose receiver comes first: one set in a branch or a closure, after a `return` or past a `goto` that may skip it, set twice, or possibly set by whatever the builder is handed to, even under another name or as a method value such as `setKey := b.Key[string]`, is unknown, and `Find` is not checked against it. A query is judged only where it plainly is not a subquery: when its chain ends in a call that runs it, leaving ant's builder types, so `GroupBy` and `Join` carry it on, or in a function that runs it, such as `ant.SumOf` or `ant.Pluck`, or when it sits in the arguments of outer queries whose models it may also name, the outermost of which runs. A function such as `ant.Project`, `ant.Merge`, or `ant.JoinSub` makes the query part of what it builds, a grouped query given to `ant.Project` included, so the chain goes on from there, and a method given its type arguments, as `OrderBy[string]` is, is followed past them. A join reads all its models, up to the four of `Join4`, and is an outer query to its subqueries. A join function's first query may name the model it joins, as a condition before a `Join` method may, and a lateral join's subquery may name the query it joins, while `JoinSub`'s subquery stands alone. A subquery in a join's conditions may name either side, and is left alone. Only a function given the query itself runs it or builds on it: one given a condition made of it, as `ant.Exists` makes, uses it as a subquery. Parentheses around a receiver change nothing, nor does a type of one's own that embeds an ant query or table, whose methods are the builder's and are judged as its, and neither does a method expression, which takes the receiver as its first argument and goes on along its chain, as `(*ant.Query[Post]).All(q, ctx)` runs `q`, though a query in one of its other arguments is not taken as a subquery of it. The outer queries end at a statement, except that a query a function literal returns is the query of the call the literal is given to: `When` and `Scope` hand back their own query, with its scope, while an `Include` or `IncludeOne` shape is a query of the related model that ant runs apart, so nothing outside the shape is in its scope, as ant itself refuses. A model whose type argument holds a type parameter, inside an interface too, is left alone until an instantiation fills it in. So a subquery built apart and passed in, inside an `Include` shape too, one inside a grouped query or a relation's scope, such as `Has`, whose model the analyzer does not follow, and one whose outermost query does not run where the analyzer looks, are left alone, since only data flow could say which query they end up in.

A variable holds a probe if everything ever assigned to it is one, however that is done: declared with `var` in a function, assigned with `=`, as a package variable set in `init` is, which other packages learn of as a fact, or copied from another such variable, wherever in the package that comes, and even when copies go round, as `q := p` and then `p = q` do, as long as a probe from `ant.Fields` or another package reaches them. A declaration with no value does not count. A variable that is ever given anything else, an ordinary pointer or one of the values of a call that returns several, may hold that at any use, since the analyzer does not follow the order code runs in, so it counts as an ordinary variable throughout. So does one whose value the analyzer cannot see: a parameter or a receiver, which the caller gives, a range variable, and a variable whose address is taken, through which anything may be stored. A field behind a pointer held in a struct field, as in `&holder.U.Age`, or behind what a call returns may be a probe's, so it is not judged at all. Conditions that `And`, `Or`, and `Not` combine are judged with the query they are given to, and so are `Past`, `Future`, `Today`, `InSub`, and `NotInSub`, which take one of its columns. `IncludeJoined` is not a join here: it reads its row under an alias of its own, which conditions cannot name, so ant refuses them. A model that holds a type parameter is known only where its function is instantiated, so its queries and `Find` keys are left alone. A `Find` key passes as ant's own check passes it: the key's type, any integer for an integer, or a type of the same kind that converts to it, such as `[16]byte` for a named UUID type, and a type argument written out, as in `Find[string]`, is checked too.

Conditions are not the only place a field of another model fails. The fields that ordering and grouping take are judged as conditions are, since a subquery may order by its outer query's columns, and those that `Column`, `Omit`, `With`, the include and join methods, an update's assignments, and the aggregates such as `ant.SumOf` take, by the query's own models alone. `JoinUsing` reads only the column's name, which both sides have, so it may take either's. `AnyOf`, `AllOf`, and `NoneOf` have each of their fields judged. Every field pointer that ant's functions and its builders' methods take must be a probe's, `ant.Into`, `ant.EqCol`, the relation functions, and `ant.Original` included, while a function that takes an entity, such as `ant.Add`, takes any pointer.

Code that is wrong on purpose, as in tests of ant's errors, carries `//antvet:ignore` and a reason, at the end of its line or on a line of its own above it. One at the end of a line covers only that line, so it cannot hide a report on the next. `make lint` builds the analyzer and runs it on ant, so a new false report fails the build where it starts.

## 84. Every time is bound in UTC where columns keep no zone

SQL Server's `DATETIME2` and SQLite's text keep a time's wall clock and drop its zone, so ant binds times in UTC there, as decision 76 says. Only a plain `time.Time` was converted, though. A `*time.Time`, `sql.Null[time.Time]`, `sql.NullTime`, `ant.DeletedAt`, a sensitive field's wrapped value, and every argument of a raw query went in with the application's zone, so a nullable time stored three hours off in a zone three hours from UTC, and a condition on it found nothing.

The renderer and the raw-query binder now share one conversion. It sees through pointers, as many as database/sql would follow, and interfaces anywhere along them, `qb.Secret` and a pointer to one, which stay secrets, as a secret inside a `Valuer` that gives a time does once the `Valuer` is asked, and `sql.NullTime` and an `sql.Null` of a time, of a pointer to one, of a `Valuer` that gives one, or of an interface that holds one when it is bound, and structs built on them, such as `ant.DeletedAt`, and it leaves NULL as NULL. Raw arguments are converted after an `IN` list expands, so each element is. Other `driver.Valuer` types are left alone rather than asked for their value: a JSON or encrypted value would be serialized twice, once to look for a time and once by database/sql. A type built on a time is asked once, and its value, or its error, goes on in its place, so one that overrides `Value` is not asked twice either. A `Valuer` of one's own that gives a time is the application's to give in UTC. A pointer chain that comes back to itself binds an error rather than being followed for ever. `TestConformanceTimeShapes` stores and finds each shape in a zone three hours from UTC on all five databases, and reads the stored clock back through database/sql, past ant.

## 85. MySQL reads the table it writes through a derived table

MySQL refuses an `UPDATE` or `DELETE` whose subquery reads the table being written, with error 1093, at any depth: `IN` a subquery of the same table, `EXISTS` through another table's subquery that reads it, a value in `SET`, a join or nested condition inside the key subquery of decision 38. It allows the read inside a derived table, which it materializes before the write. MariaDB allows all of them.

So on MySQL, while an `UPDATE` or `DELETE` renders its values and conditions, the renderer watches each subquery for the written table, by name in any case, in the same schema, and by name in raw SQL, which it cannot read: as a whole word or a quoted name, while a string literal or a comment that holds it does not count. Raw SQL is split into literals and comments as MySQL splits it, with `#` and `-- ` comments and `/*! */` read as SQL, past a version of five or six digits that may run straight into it, and in every SQL mode, since the renderer cannot know the server's: a backslash escapes a quote unless `NO_BACKSLASH_ESCAPES` is set, and `ANSI_QUOTES` makes double quotes a name. A name that any mode reads as the table counts, because a needless wrapper costs a derived table and a missed one fails the write. A subquery that reads it, through a table or a source named like it, is wrapped once, as a whole: `SELECT /*+ NO_MERGE(materialized) */ * FROM (...) AS materialized`. A derived table needs unique column names. MySQL makes one up for a column with none, from its SQL, so a single column is always unique, and so are several whose names the renderer can tell apart. Otherwise, where two columns share a name, or one of several is an expression or raw SQL, the derived table names its columns with a list, `AS materialized(ant_c1, ant_c2)`, which `IN`, `EXISTS`, and a value do not mind, as they read by position. The subquery itself is left as it was either way, so what it refers to by name, as `ORDER BY` and `HAVING` can, keeps its meaning: an earlier version renamed columns inside the subquery instead, and every name it gave could be one the query read. A raw expression in the select list is one column, and one that holds several fails with MySQL's error 1353, as the list does not match. A star in `EXISTS` becomes `1` when nothing reads its columns: no grouping, ordering, offset, or set, while `DISTINCT` cannot change whether a row exists. A star over one table stays, with the table's own names and no list, since its columns cannot be counted. A star that has to stay beside other columns, or over a join or a set, may name a column twice, which no derived table takes, so a write whose subquery plainly reads the table returns `ErrUnsupported`. Only a read the renderer is sure of refuses a write, though: a table of exactly the written table's name and schema, or with no schema on either side, outside any derived table. A name in raw SQL, a source name, which may be a CTE's, a name that differs only in case, which is another table unless `lower_case_table_names` folds them, a table where only one side gives a schema, which is the same table only if the other is in that schema, and a read inside a derived table of the subquery, which MySQL materializes before the write, may need the wrapper or not, so such a subquery is wrapped where it can be, and otherwise left as it was, for MySQL to judge. A table in another schema than the written table's is another table. In raw SQL any spelling of the table counts, whatever schema qualifies it, since raw SQL is not parsed, and a wrapper the subquery does not need costs only a temporary table. The subquery is written twice, once to find the read and once inside the wrapper, so the `Lazy` conditions it holds, such as global filters, are recorded the first time and replayed, and each is built once. The column list needs MySQL 8.0.19, and a correlated subquery, which the derived table refers to the outer row from, needs 8.0.14. ant is tested on 8.4, and `TestConformanceWritesReadingTheirTable`, which runs each shape, the column lists included, and checks the rows it changed, passes on 8.0.46 too. MySQL 8.0 reached its end of life in April 2026. Before those versions such a write failed with error 1093 anyway, so no write that worked stops working. A read already inside a derived table of the subquery is wrapped too: MySQL's manual says the optimizer may merge such a derived table into the outer query, which brings error 1093 back unless its materialization is forced, while 8.4 does not merge it, so the wrapper costs a temporary table at most, and needs no column list when the subquery's names are its own. The hint keeps the optimizer from merging the wrapper back, whatever `derived_merge` says. A subquery that reads only other tables stays as it was, since wrapping it would cost a temporary table for nothing, and a `LIMIT`ed subquery is a derived table already. Raw SQL directly in the `WHERE`, outside any subquery, is the application's own.

`ON DUPLICATE KEY UPDATE` refuses a read of its own table even through a derived table, and accepts it only from `INSERT ... SELECT`. So an upsert whose `Set` may read the table, by the same watch, writes its rows as a `SELECT` from a derived table of `UNION ALL` rows, with columns named `ant_c1` and on, so the update reads none of them by mistake, and `VALUES(col)` still names the row's values. The `Set` is written once to see, its `Lazy` conditions recorded and replayed, as above. A `SELECT` has no `DEFAULT`, so `InsertMany` with a column left to its default and a sure read is refused with `ErrUnsupported`, and with a read that only may be one it keeps `VALUES`, for MySQL to judge. MariaDB takes the `VALUES` form.

## 86. Combinations are tested against an oracle

Most bugs found lately came from one feature meeting another: a bulk write on a derived source, a paged member of a union, a nullable time on a database without zones, a relation load inside a transaction that rolls back. Hand-written tests check features one at a time, so the combinations went unchecked.

`tests/matrix` builds queries from a grid of dimensions and checks every point against an oracle: the same combination worked out in Go, on the same seeded rows, with SQL's three-valued logic. It runs on every database, about 40,000 checks each, in a minute on PostgreSQL. Checking rows, not SQL, is what finds wrong results; SQL snapshots per dialect add a review of every change to generated SQL, with no database needed. A known failure is listed with its issue and skips, and an entry whose combinations all pass fails the run, so the list tracks what is still broken. The grid grows by adding a point to a dimension, with its meaning in Go beside it.

## 87. An embedded field must be a struct value

`ant:"embedded"` on a pointer made opening a context panic, deep in reflection. Supporting one would need its columns nullable, and a rule for when columns that are all NULL mean a nil pointer rather than a zero struct, so ant refuses it instead, with an error that names the field, as it does a scalar, or a struct that holds such a field. The model keeps that error, and every use of it returns it: opening a context, a query, and a raw query or a projection that scans into the struct, which would otherwise read the fields ant did walk and leave those after the pointer empty.

## 88. SQL Server recurses with UNION ALL and a path of keys

SQL Server's recursive `WITH` has no `RECURSIVE`, and joins its parts only with `UNION ALL`, which keeps a row each time it is reached: a cycle never ends, and a row reached from two starting rows comes twice. `UNION` stops both elsewhere (decision 36). So `Recursive` gives its CTE the model's key, `qb.CTE.Key`, and on SQL Server, `Syntax.RecursiveUnionAll`, each row carries the path of keys that reached it, as text: each key column's bytes in hex, which neither separator can occur in. A recursive member skips a row whose key is on the path already, which ends a cycle, and the CTE is read through `ROW_NUMBER()` over the key, one row per key. The path also lets the recursion go deeper than SQL Server's default of 100 levels, with `OPTION (MAXRECURSION 0)`. A CTE without a key, as one built by hand, is written as it is, without `RECURSIVE`. A `MAXRECURSION` bound alone was rejected: it errs on deep trees and still repeats rows. `Count` and `Exists`, which put the query in a subquery, lift its `WITH` to the start of the statement, the only place SQL Server takes one, on every database, since the outer query reads nothing but the subquery. Writes after `FromCTE` and `Recursive` stay refused on SQL Server (decision 38).

## 89. One column, one value

PostgreSQL and SQL Server refuse a write that gives a column two values, and MySQL keeps the last, so the same code did different things by database. ant refuses it before it runs, with the field's name: an upsert's `DoUpdate` and `Set`, a bulk `Update`, `UpdateMany`, a merge's `WhenMatched` and `WhenNotMatched`, and `InsertFrom`'s mappings. A bulk `Update` that assigns the version is refused too, as an upsert's and a merge's are (decision 80). `UpdatedAt` stays the exception it was: one the write gives wins over ant's.

## 90. Merge keeps what other writes keep

`Merge` builds its statement from what it is given, so it left out what `Insert` and `Update` fill in. Now a matched update sets `UpdatedAt`, and an insert sets `CreatedAt`, `UpdatedAt`, the version, and the model's discriminator, unless a mapping gives the column. A key from a Go generator is not filled: one statement inserts many rows, and a value made once would be the same for all of them.

## 91. An upsert checks a copy's version on MySQL and MariaDB too

`ON DUPLICATE KEY UPDATE` takes no condition, so a stale copy's update went ahead there (decision 80). Now each of its assignments keeps the stored value unless the version is the copy's: `col = CASE WHEN version = ? THEN new ELSE col END`. MySQL and MariaDB run them in order, each seeing what came before, and the version's comes last, so every one reads the version the row had. A stale copy then changes nothing, which the affected-row count shows, and the upsert returns `ErrConcurrency`, as on PostgreSQL. This needs the driver's `clientFoundRows` off, its default, which detecting `DoNothing` needed already.

## 92. Tracked values compare as stored

A float field holding NaN was never equal to its snapshot, and an `sql.Null[time.Time]` or `ant.UnixTime` at the same instant in another zone counted as changed, so every save wrote an update that changed nothing. Comparison now follows `reflect.DeepEqual` except that NaN equals NaN and a time equals another at the same instant, at any depth, as a `time.Time` field already did. Nil and empty still differ, as NULL and empty do. A snapshot copies what an `any` holds, so an edit inside a map or slice that JSON decoded is seen. A time in a key is the instant in UTC, to the microsecond, which PostgreSQL and MySQL keep and ant's own timestamps hold, so a row read back is the instance already tracked; two keys less than a microsecond apart are one row to the tracker, which SQL Server and SQLite could store as two. A slice type that is a `driver.Valuer` and an `sql.Scanner` is a column, not a has-many relation, and a type whose `Value` has a pointer receiver is bound through a pointer, so its method runs.

## 93. Tracking follows what a save wrote

Several gaps in change tracking came from relations being remembered only when loaded:

- An entity inserted with its relations is tracked as if loaded with them, as EF Core does, so a later save does not write its join rows again, and a child removed from its slice is deleted. `InsertOnly`, which ignores relations, records none.
- A pointer to a parent is remembered once a save writes it, and remembered empty once cleared, so setting it again and clearing it is seen.
- A child moved to a parent added in the same save, or to one whose relation was never loaded, is moved, not deleted.
- An element of a many-to-many slice set to another target unlinks the old one and lets go of its entry, as the element's memory now holds the new one.
- Relations loaded below a slice element, by a nested `Include`, are recorded on the element's entry.
- A row put back into a loaded relation after this context deleted it is written, so the save fails rather than doing nothing, since only the database knows the row is gone.
- `Remove` of an entity that was added and never saved cancels its insert.
- Two instances of one versioned row written in one save carry the version forward from one to the other.

A later `Include` reads a tracked parent's relation afresh, as `Load` does, and lets go of the entries of the elements it replaced, so what the slice shows is what a save writes. Keeping pending relation changes through a query was tried, and rejected: it skipped the query's nested includes, and hid rows other writers added. Save first to keep a pending change. Replacing children with new ones under the same unique values still fails with a unique violation and changes nothing: inserts run before deletes, so a child can move to a parent saved in the same unit, and a constraint the model does not declare is not ant's to see. Saving the removal first works.

## 94. Migrations keep a column's dependents working while it changes

The plan's order now follows what PostgreSQL needs while a column changes:

- A default is dropped before a type change and set again after, since the old one may not cast, and its text keeps the old type's cast, which `drift` would see and which would hold an enum the plan drops. An identity is dropped before a default is set, and added after `NOT NULL` and after the default goes. An enum casts to another type through text.
- A generated column is dropped before a column it reads is dropped or changes type, and added after, as is one that becomes generated or gets a new expression; one that stops being generated drops its expression first, keeping its values. New generated columns come after the columns they read, whatever the declaration order.
- A foreign key on a column whose type changes, on either side, is dropped first and made again last.
- Index and constraint renames run after the drops, so a name can move from one that goes, and renames that swap names go through a spare one. Table and column renames are applied as a batch: table renames first, so a column can be named under its table's old or new name, swaps through a spare name, a name with a dot found as it is or quoted, and `schema.table` as a new name moving a table to a schema the models have, which a hint now suggests.
- Renaming a column rewrites the expressions that name it in the snapshot, as PostgreSQL does in the database, so nothing is rebuilt for it. What the rewrite misses is rebuilt, as before.
- A new enum value goes `BEFORE` the value that follows it in the models. One the plan uses, in a default, a check, a generated column, or an index, makes the file run without a transaction, since PostgreSQL lets a transaction use a value it added only once committed; the literal is matched as text, and erring toward a match costs only the transaction. An order PostgreSQL cannot change is a warning, as a removed value is.
- A sequence's increment and start change in place, keeping its current value.
- A new `NOT NULL` column of `time`, `timetz`, `interval`, `inet`, `cidr`, `macaddr`, `money`, `bit(n)`, or an enum gets a zero value for existing rows, the enum its first value the plan can use.
- An index part says where NULLs go when that is not its direction's default, `IndexBuilder.NullsFirst` and `NullsLast`, and `Inspect` reads it back, with quoted names unescaped.
- A name longer than the database keeps is refused when the script is written, rather than cut, where two names could become one.
- Comments in a script escape line breaks, which a name may hold, and which would end a comment and make the rest SQL.

## 95. In means Eq, and other value rules

`Eq(nil)` matches NULL, but `In(nil, x)` followed SQL, where NULL is in nothing. Now `In` and `NotIn` are `Eq` and `Ne` of each value, folded: a nil value adds `IS NULL` to `In`, and `IS NOT NULL` to `NotIn`, whose other values exclude NULL rows as SQL does. The typed `Expr` methods follow the same rule: `Col(&f).Eq(nil)` is `IS NULL` and `Ne(nil)` is `IS NOT NULL`, where they used to bind `= NULL` and match nothing (#94).

A list longer than the parameters one statement takes goes on PostgreSQL as one array, `= ANY($1)` or `<> ALL($1)`, when its values are plain and of one type, which is what PostgreSQL makes of `IN` anyway. A list that fits stays a list, and other databases, which take no arrays, leave it to the driver to refuse.

`ant.JSON` and `secure.Value` refuse text that is not valid UTF-8, as a text column does: `encoding/json` would store U+FFFD in its place. Changing `secure.Value`'s encoding to keep the bytes was rejected, as it would not read the values stored before.

`ant.UnixTime` stores the zero time as 0, as schemas that keep times as integers use 0 for no time, so 0, and the Unix epoch with it, reads back as the zero time. Storing the zero time as its true count of seconds was rejected, as it would read rows written before as 1970.

Some limits are `database/sql`'s or a driver's, and are documented in `docs/databases.md` rather than worked around: an `sql.Null[uint64]` above `math.MaxInt64`, `NULL` into a named `[]byte`, and an `INTERVAL` into a `time.Duration`.

## 96. A forced update stamps UpdatedAt

`MarkModified`, and `TrackGraph` deciding `Modified`, write every updatable field, since there is no snapshot to compare with. `UpdatedAt` was one of them, so it counted as set by the application and was saved as the struct held it: the zero time, for a struct decoded from a request body (#60). A forced update now leaves `UpdatedAt` to ant, which stamps it as on any other update. An application that wants its own value there writes it with a bulk `Update`, where the one the write gives wins (decision 89).

## 97. Spans name the database the dialect talks to

`contrib/otel` set `db.system` to `postgresql` on every span, with no way to change it, so traces from the other four databases were mislabelled (#61). An interceptor never learns the context's dialect, so `New` takes options: `WithDialect` maps ant's dialect names to OpenTelemetry's values (`postgresql`, `mysql`, `mariadb`, `sqlite`, `mssql`), and passes another dialect's name through, and `WithSystem` sets the value as given. Having `ant.Open` tell interceptors the dialect was rejected: it would add a hidden interface to the core for one attribute. The default stays `postgresql`, so existing setups keep their spans.

## 98. An explicit write refuses the columns ant keeps

`SaveChanges` never updates an `immutable`, `readonly`, or `computed` field, but a bulk `Update` with `ant.Set`, an upsert's `DoUpdate` or `Set`, a merge's `WhenMatched`, and `UpdateMany`'s field list wrote whatever they named, so an immutable column changed after all (#62). Each of them now refuses such a field by name before anything runs, as it refuses the version (decisions 80 and 89), and `UpdateMany` now refuses the version too. Inserts are unchanged, since an immutable field is written on insert. Letting an explicit write win, as an explicit `UpdatedAt` does, was rejected: the tag is the model's promise, and a field that must change sometimes should not carry it.

## 99. A retried attempt leaves nothing in the tracker

`Attempts` runs `f` again after a deadlock or serialization failure, on the same tracker. The rollback put the tracker back as the save found it, but the entities the failed attempt loaded stayed tracked with its edits, so the next attempt's tracked read got the edited instance and changed it again, and an entity it added stayed pending beside the one the next attempt added (#63). Before a retry, the tracker now forgets every entry that began during the failed attempt, which `f` loads or adds again. What was tracked before the transaction stays, with any change made to it, since ant cannot tell the attempt's edits from earlier ones without copying every entity at each attempt; `f` should load what it changes. Doing this on every rollback, not only before a retry, was rejected: a failed transaction's entities stay pending so that `SaveChanges` can be repeated (decision 79).

## 100. Probes are named after their model, in the plural

The docs, examples and tests named probes with one or two letters, such as `U` for `User`. Two models that start with the same letter needed made-up names (`Pl` for `Plan`, `G` for `Page`, `St` for both `Student` and `Seat`), and a reader could not guess them.

Decision: name a package-level probe after its model in the plural, with the rule ant uses for default table names: `Users` for `User`, `Categories` for `Category`, `Persons` for `Person`. Type names are unique, so probe names are too, and `&Users.Age` reads like the column `users.age`. It is a convention, not a rule: ant accepts any name. A probe declared inside a function keeps a short local name, because a local `users` would be shadowed by `users, err := ...`. `ant_test.go` keeps `U` and `P`, since `example_test.go` in the same package already declares `Users` and `Posts` for other models. A probe of a model whose name is an adjective reads oddly (`Taggeds`); renaming such test models is left for later.

## 101. Probes are pinned

A field pointer resolves to its column by its address inside the model's probe, so the probe must not move for the life of the process. That held only because Go's collector does not move heap objects today, which the language does not promise.

Decision: `Probe` pins each probe with `runtime.Pinner` when it allocates it, under the registry's lock, since a `Pinner` is not safe for concurrent use. The probe's address now stays fixed even under a moving collector, and the pin tells the current collector about the pointer arithmetic ant does on it. The `Pinner` is a package variable and is never unpinned: probes live as long as the process, and the runtime panics if a `Pinner` that still holds pinned objects becomes unreachable. Pinning happens once per model type, so resolving a field pointer costs the same as before. `TestProbeSurvivesGC` still checks that a probe's address survives collections.

## 102. UpdateMany keeps what a bulk Update keeps

`UpdateMany` wrote only the named columns with a key condition, so it left the version where it was, never set `UpdatedAt`, and wrote soft-deleted rows; a copy loaded before it then saved over its value with no conflict (#86). Each statement now moves the version on (decision 80), sets `UpdatedAt` unless a field names it (decision 89), and carries the model's global filters, as a bulk `Update` does, so a soft-deleted row or one a tenant filter hides is not counted or changed. The entities passed in are left as they are, since ant does not know the stored version to move them to; a caller that keeps one should load it again. Applying only the soft-delete filter was rejected: a tenant filter guards a write at least as much as a read.

## 103. An upsert's update stamps UpdatedAt

`Update`, `SaveChanges` and `Merge` set `UpdatedAt` on every update, but an upsert's update branch did not: `DoUpdate` left the column as it was, and the default clause wrote `excluded.updated_at`, the struct's value, which for a loaded copy is the time it was last saved (#84). The conflict update now sets `UpdatedAt` to ant's clock, on `Upsert` and on `InsertMany` with a conflict, unless `DoUpdate` or `Set` assigns it, in which case the clause's value wins, as with a bulk `Update` (decision 89). The default clause no longer copies the column from the inserted row, since that value is the one being replaced.

## 104. A skill for coding agents ships in the module

Models trained before ant existed write GORM when asked for ant code: string conditions, `Preload`, `AutoMigrate`. The docs site serves itself as Markdown (`llms.txt`, `llms-full.txt` and a `.md` copy of each page), which an assistant can read if someone points it there.

Decision: the repo carries a skill, `skills/ant/SKILL.md`: the rules an assistant gets wrong when it guesses, in a few hundred lines, with links to the Markdown docs for the rest. It lives in this repo rather than in ant-docs so it is versioned with the code and lands in the module cache with every `go get`, where the package doc points to it. The same directory is a Claude Code plugin (`.claude-plugin/`) and the layout `npx skills add` reads, so installing it is one command for most agents. Nothing installs it by itself: a library can't write into a user's agent settings, and shouldn't. `tests/api` fails when the skill names a function or method the API no longer has; its samples are type-checked with ant-docs' doccheck, by hand, since that tool lives in the other repo.

## 105. Read-mostly registries are copy-on-write snapshots

The model registry, the probe list, the `secure` keyring and a context's observers change a handful of times, mostly at startup, but are read on every query, every encrypted value and every write. Each read took a shared `sync.RWMutex`. Read locks do not block one another, but each takes and releases a counter on one cache line that every core then fights over: resolving a field pointer took 19 ns on one core and 26 ns per call on twelve, less total throughput than one core (#135). `notify` also called observers while it held its read lock, so an observer that called `Observe` deadlocked.

Decision: each is an immutable value behind an `atomic.Pointer`. A reader loads it and takes no lock. A writer copies it, changes the copy and stores it, under a plain mutex where two writers could otherwise lose each other's change (the registry, which also guards the `Pinner` of decision 101, and the observers); `SetKeys` replaces the whole keyring, so it needs none. Copying costs a write O(n), once per model type or registration. Resolving a field pointer now takes 2.8 ns per call on twelve cores. `sync.Map` was rejected: `Resolve` scans the probe list by address range, which a map cannot index, and one mechanism for all four is easier to check. The naming conventions (read once per model) and `dialect.StateOf` (read only on errors) keep their locks, since they are off the hot path.

`tests/benchmarks/parallel_test.go` runs resolution, query building and a read from every core, so contention shows in the numbers. Those runs also showed that building a query scales poorly past four cores because of allocation and GC, not locks; that is separate work.

## 106. Begin inside a transaction opens a savepoint

`Begin` on a context already in a transaction returned that same context, so a helper written as Begin, work, Commit committed its caller's transaction early, and its Rollback rolled back the caller's work too (#72). A nested `Tx` already used a savepoint. `Begin` now does the same: inside a transaction it opens a savepoint and returns a context bound to it, whose `Commit` releases it and whose `Rollback` rolls back to it and takes back the tracker changes since, while the outer transaction goes on. The outer transaction still decides: its rollback undoes what the inner one committed, and observers wait for its commit. Options such as `Isolation` apply to a new transaction only, as with a nested `Tx`. Returning an error instead was rejected, since it would make a helper that begins its own transaction unusable from inside `Tx`.
