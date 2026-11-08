# Roadmap

The full list of features ant plans to implement, derived from feature catalogs of EF Core, Eloquent, and the Go libraries. Overlapping features from different libraries are merged into one line. Anything not scheduled is listed under [Not planned](#not-planned).

API names are targets and may change until the v1.0 freeze. Features with no public API yet have none listed.

## v0.1 Foundations

### Model definition and mapping

- [x] Plain struct models with no required base type
- [x] Embeddable base: `ant.Base`
- [x] Model registry that reflects each struct once
- [x] Field probes: `ant.Fields[T]()`
- [x] Probe resolution by address and type
- [x] Table name convention: `User` to `users`
- [x] Column name convention: `UserID` to `user_id`
- [x] Exclude a field: `ant:"-"`
- [x] Column name override: `ant:"column=name"`
- [x] Table name override: `b.Table("users")`
- [x] Fluent configuration: `Configure(b *ant.ModelBuilder[E])`
- [x] Nullable columns through pointers and `sql.Null[T]`
- [x] Custom types through `sql.Scanner` and `driver.Valuer`

### Keys and identity

- [x] `ID` primary key by convention
- [x] Explicit primary key: `b.Key(&U.Email)`
- [x] Application-assigned integer keys: `b.Key(&T.ID).NotGenerated()`
- [x] Database-generated integer keys
- [x] Explicit key values on insert
- [x] String and UUID keys supplied by the application

### Querying

- [x] Query from a table: `db.Users.Where(...)`
- [x] Equality: `ant.Eq(&U.Age, 18)`
- [x] Inequality: `ant.Ne`
- [x] Comparisons: `ant.Gt`, `ant.Gte`, `ant.Lt`, `ant.Lte`
- [x] Set membership: `ant.In(&U.Age, 18, 21)`
- [x] Negated membership: `ant.NotIn`
- [x] Range: `ant.Between(&U.Age, 18, 65)`
- [x] Pattern match: `ant.Like(&P.Title, "Draft%")`
- [x] Null checks: `ant.IsNull(&U.Bio)`, `ant.IsNotNull`
- [x] Nil pointer in `ant.Eq` renders `IS NULL`
- [x] Logical grouping: `ant.And`, `ant.Or`, `ant.Not`
- [x] Column comparison: `ant.EqCol(&U.ID, &P.UserID)`
- [x] Ordering: `q.OrderBy(&U.Name)`
- [x] Descending and secondary ordering: `q.OrderByDesc`, `q.ThenBy`, `q.ThenByDesc`
- [x] Limit and offset: `q.Limit(10)`, `q.Offset(20)`
- [x] All rows: `q.All(ctx)`
- [x] First row: `q.First(ctx)`
- [x] Exactly one row: `q.Single(ctx)`
- [x] Find by primary key: `db.Users.Find(ctx, id)`
- [x] Find with fallback: `ant.ErrNotFound` from `Find`, `First`, and `Single`
- [x] Raw SQL into structs: `ant.Raw[User](db, sql, args...).All(ctx)`
- [x] Raw scalar and unmapped type results: `ant.Raw[int]`, `ant.Raw[Summary]`
- [x] Raw non-query statements: `ant.Raw[any](db, sql).Exec(ctx)`
- [x] Raw SQL with named arguments: `sql.Named("age", 18)` and `@age`
- [x] Slice expansion in raw SQL arguments: `ant.Raw[User](db, "... IN (?)", ids)`
- [x] Placeholder rewriting for portable raw SQL: `?` to the dialect form
- [x] Raw SQL loaded from a file: `ant.RawFile[T](db, fsys, name)`
- [x] Conditional clauses: `q.When(ok, func(*Query[T]) *Query[T])`

### Writing

- [x] Insert one or several: `db.Users.Insert(ctx, u1, u2)`
- [x] Generated key copied back to the struct
- [x] Bulk update: `q.Update(ctx, ant.Set(&P.Published, true))`
- [x] Bulk delete: `q.Delete(ctx)`
- [x] Rows affected returned from bulk operations
- [x] Block update and delete with no condition: `ant.ErrNoCondition`, overridden by `q.Unfiltered()`
- [x] Delete by primary key: `db.Users.DeleteByID(ctx, id)`
- [x] Refresh a loaded entity: `db.Users.Reload(ctx, u)`

### Transactions and connections

- [x] Open a context: `ant.Open[AppDB](ctx, dialect, dsn)`
- [x] Wrap an existing pool: `ant.Wrap[AppDB](dialect, sqlDB)`
- [x] Close: `db.Close()`
- [x] `context.Context` on every terminal call
- [x] Pool settings through `db.DB()` access to `*sql.DB`

### Lifecycle

- [x] Errors accumulated in builders and returned at terminal calls
- [x] Sentinel errors: `ErrNotFound`, `ErrMultipleRows`, `ErrNotProbe`, `ErrWrongModel`, and others in `ant.go`

### Database support

- [x] `Dialect` interface
- [x] PostgreSQL dialect

### Developer tooling

- [x] Multi-database test harness: `internal/testdb`
- [x] Docker Compose database matrix
- [ ] CI with unit and integration jobs
  - Skipped: Not finished. `.github/workflows/ci.yml` has both jobs, but it has never run on GitHub, and the owner set CI aside for now. Everything here was verified locally instead, so treat the workflow as untested until its first run.

**Exit criteria:** every test in `tests/crud_test.go` and the non-relation tests in `tests/query_test.go` pass on PostgreSQL. No public query API takes `any` except raw SQL arguments.

## v0.2 Relations

### Model definition and mapping

- [x] Relation detection: `[]Post` and `*User` fields are not columns

### Relations

- [x] One-to-many by convention: `Post.UserID`
- [x] Many-to-one by convention: `Post.User`
- [x] One-to-one by convention
- [x] Self-referencing relations
- [x] Explicit relation configuration: `b.HasMany(&U.Posts).ForeignKey(&P.UserID)`
- [x] Explicit one relation: `b.HasOne`, `b.BelongsTo`
- [x] Relation to a non-primary key: `.References(&U.Email)`
- [x] Delete behavior: `.OnDelete(ant.Cascade)`, `ant.SetNull`, `ant.Restrict`, `ant.NoAction`
- [x] Update behavior: `.OnUpdate(...)`
- [x] Default model for a missing relation is a nil pointer

### Querying

- [x] Join through a relation: `q.Join(&P.User)`
- [x] Left join through a relation: `q.LeftJoin(&P.User)`
- [x] Multi-table joins: `Join3`, `Join4`
- [x] Ad hoc join on a condition: `q.JoinOn[J](conds...)`
- [x] Join with `USING`: `q.JoinUsing[J](&U.ID)`
- [x] Right, full, and cross join variants: `q.RightJoinOn`, `q.FullJoinOn`, `q.CrossJoin`
- [x] Filter by relation existence: `ant.Has(&U.Posts, conds...)`, `ant.HasOne`
- [x] Filter by relation absence: `ant.HasNone(&U.Posts, conds...)`
- [x] Projection with `Select`: `j.Select(func(*Post, *User) R)`
- [x] Single-table projection: `ant.Project[R](q, mappings...)`
- [x] Selected columns: `q.Column(&U.ID).Column(&U.Name)`
- [x] Column-level projection API that prunes the `SELECT` list: `ant.Into`, `ant.IntoExpr`
- [x] Omit columns: `q.Omit(&U.Bio)`
- [x] Distinct: `q.Distinct()`

### Loading

- [x] Batched 1:N loading: `q.Include(&U.Posts)`
- [x] Batched N:1 and 1:1 loading: `q.IncludeOne(&P.User)`
- [x] Nested loading: `Include` inside the shape function
- [x] Filtered include: `q.Include(&U.Posts, func(q) { return q.Where(...) })`
- [x] Ordered and limited include, per parent
- [x] Selected columns in an include
- [x] Load into existing entities: `db.Users.Load(ctx, &U.Posts, users...)`, `LoadOne`
- [x] Load only if missing: `db.Users.LoadMissing`
- [x] Single-query loading through a join: `q.IncludeJoined(&P.User)`
- [x] Relation counts: `q.With(&U.PostCount, ant.CountRelated(&U.Posts))`
- [x] Relation aggregates: `ant.SumRelated`, `MinRelated`, `MaxRelated`, `AvgRelated`
- [x] Check whether a relation was loaded: a nil slice is not loaded, and an empty slice is
- [x] Query the related rows of one entity: `ant.Related(db, user, &U.Posts)`, `ant.RelatedOne`

**Exit criteria:** `tests/relations_test.go` passes on PostgreSQL. A query test asserts `Include` runs exactly one extra statement per relation.

## v0.3 Unit of work

### Change tracking and unit of work

- [x] Entity states: detached, added, unchanged, modified, deleted
- [x] Snapshot on load
- [x] Property-level diffing
- [x] `db.SaveChanges(ctx)`
- [x] Minimal `UPDATE` statements
- [x] Unit-of-work ordering across foreign keys
- [x] Identity map
- [x] Opt out per query: `q.NoTracking()`
- [x] Default tracking setting: `ant.WithTracking(false)`
- [x] No-tracking with identity resolution: `q.NoTrackingResolved()`
- [x] Attach an existing entity: `ant.Attach(ctx, db, user)`
- [x] Detach an entity: `ant.Detach(ctx, db, user)`
- [x] Mark for update: `ant.MarkModified(ctx, db, user)`
- [x] Mark for delete: `ant.Remove(ctx, db, user)`
- [x] Clear the tracker: `db.ClearTracked(ctx)`
- [x] Query state: `entry, err := ant.Entry(ctx, db, user)`, then `entry.State()`
- [x] Inspect dirty fields: `entry.Changed()`, `ant.IsDirty(entry, &U.Age)`
- [x] Original values: `ant.Original(entry, &U.Age)`
- [x] Has pending changes: `db.HasChanges(ctx)`
- [x] List tracked entities: `ant.Tracked[User](ctx, db)`
- [x] Change detection runs at `SaveChanges`, so no manual call is needed
- [x] Custom value comparers for slices and maps: `b.Comparer`
- [ ] Relationship fixup across loaded entities
  - Skipped: It conflicts with the rule that a field that was not loaded stays empty, which is what makes a nil slice mean "not loaded". With fixup, a field would fill in or not depending on what else ran earlier, which makes bugs that come and go. Elements of a `[]T` relation are values, so ant could not share one entity between two parents anyway. Use `Include`, which says what you want.
- [x] Change relations through navigations or foreign keys
- [x] Temporary keys are not needed: Go zero keys mark new entities, and parents are inserted first
- [x] Tracker debug output: `db.DebugTracked(ctx)`

### Writing

- [x] Graph insert: `db.Users.Insert(ctx, alice)` inserts `alice.Posts`
- [x] Add related entities through a tracked navigation
- [x] Remove related entities through a tracked navigation
- [x] Cascade delete of tracked entities
- [x] Set-null on orphaned tracked entities
- [x] Saving disconnected graphs: `ant.TrackGraph(ctx, db, root, fn)`
- [x] Skip association saves: `db.Users.InsertOnly(ctx, u)`
- [x] Batched `SaveChanges` statements
  - Inserts only, up to 200 rows a statement. A model with insert hooks, a `.Touch()` relation, or a `WithoutAutoTx` save is written one row at a time, because each of those needs a result per row. Updates stay one statement each, since a shared statement could only apply one set of columns.

### Transactions and connections

- [x] Closure transactions: `db.Tx(ctx, func(tx *AppDB) error)`
- [x] Implicit transaction per `SaveChanges`
- [x] Disable implicit transactions: `ant.WithoutAutoTx()`
- [x] Manual transactions: `ant.Begin(ctx, db)`, `tx.Commit()`, `tx.Rollback()`
- [x] Savepoints: `tx.Savepoint(ctx, name)`, `RollbackTo`, `Release`
- [x] Nested `Tx` as a savepoint
- [x] Isolation levels: `ant.Isolation(sql.LevelSerializable)`, `ant.ReadOnly()`
- [x] Automatic savepoint in `SaveChanges`
- [x] Use an external `*sql.Tx`: `ant.UseTx(db, tx)`
- [x] Use an external `*sql.Conn`: `ant.UseConn(db, conn)`
- [x] Share a transaction across contexts: `ant.CurrentTx` and `ant.UseTx`

**Exit criteria:** `tests/tracking_test.go` and `tests/tx_test.go` pass on PostgreSQL. A fuzz test confirms that zero values are written when changed and nothing is written when unchanged.

## v0.4 Dialects

### Database support

- [x] MySQL dialect
  - Queries, writes, relations, tracking, and transactions, tested on 8.4 by the conformance suite. No migrations, so it stays behind PostgreSQL. See [databases](databases.md).
- [x] MariaDB dialect
  - Queries, writes, relations, tracking, and transactions, tested on 11.8 by the conformance suite. No migrations, so it stays behind PostgreSQL. See [databases](databases.md).
- [x] SQLite dialect
  - Same scope, tested through modernc.org/sqlite.
- [x] SQL Server dialect
  - Same scope, tested on 2022. Upserts are refused: use `ant.Merge`.
- [x] `LastInsertId` key retrieval where `RETURNING` is missing
- [x] SQL Server `OUTPUT` key retrieval
- [x] Dialect-specific paging: `LIMIT`, `OFFSET FETCH`
- [ ] Dialect type mapping
  - Skipped: Go values reach these databases through their drivers without it. Mapping Go types to column types only matters for generating DDL, which is `schema.Dialect`, and the owner chose to do migrations on PostgreSQL first. `EnsureCreated` and `SchemaOf` report `schema.ErrUnsupported` on the others.
- [x] Database error classification: `ant.IsUniqueViolation`, `IsForeignKeyViolation`, `IsNotNullViolation`, `IsCheckViolation`, `IsRetryable`
- [x] Dialect capability flags with clear errors: `dialect.Supports`, `ant.ErrUnsupported`
- [x] Third-party dialects: public `Dialect` and statement AST
  - The AST moved from `internal/qb` to the public `qb` package, so a dialect outside this module can render it. The [compatibility policy](https://github.com/skudra-go/ant-docs/blob/main/docs/reference/compatibility.mdx) keeps it outside the stability promise until such a dialect exists.

**Exit criteria:** the entire integration suite passes on all five databases with no per-dialect test skips other than documented capability differences.

## v0.5 Query power

### Querying

- [x] Aggregates: `q.Count(ctx)`, `ant.SumOf(ctx, q, &P.Views)`, `AvgOf`, `MinOf`, `MaxOf`
- [x] Exists: `q.Exists(ctx)`, `q.DoesNotExist(ctx)`
- [x] Single column values: `ant.Pluck(ctx, q, &U.Name)`
- [x] Group by: `q.GroupBy(&P.UserID)`, `ant.Then(g, &P.Kind)`, `q.GroupByExpr(e)`
- [x] Having: `g.Having(...)`
- [x] Group by with typed keys and aggregates: `ant.Project`, `ant.Count()`, `ant.Sum(&P.Views)`
- [x] Subquery in a condition: `ant.InSub(&U.ID, ant.SubOf(q, &P.UserID))`, `ant.NotInSub`
- [x] Exists condition: `ant.Exists(q)`, `ant.NotExists(q)`
- [x] Scalar subquery in a projection: `ant.SubExpr(q, e).Expr()`
- [x] Subquery ordering
- [x] From subquery: `q.From(sub)`
- [x] Join a subquery: `ant.JoinSub`, `ant.LeftJoinSub`
- [x] Lateral joins: `ant.JoinLateral`, `ant.LeftJoinLateral`
- [x] Unions: `a.Union(b)`, `UnionAll`, `Intersect`, `Except`
- [x] Common table expressions: `q.FromCTE(name, sub)`
- [x] Recursive common table expressions: `q.Recursive(&F.Children)`, `q.RecursiveOne(&F.Parent)`
- [x] Window functions: `ant.RowNumber().Over(w)`, `Rank`, `DenseRank`, `Lag`, `Lead`, `ant.PartitionBy`, `ant.OrderedBy`
- [x] `CASE` expressions: `ant.Case[V]().When(c, v).Else(v)`
- [x] `VALUES` lists: `ant.Values`
- [x] Multi-column `IN`: `ant.InTuples`
- [x] Any, all, and none across columns: `ant.AnyOf`, `AllOf`, `NoneOf`
- [x] Date part conditions: `ant.DatePart`, `ant.Today`, `ant.Past`, `ant.Future`
- [x] Case-insensitive match: `ant.ILike`
- [x] Explicit collation: `ant.Collate(&U.Name, "C")`
- [x] Database function mapping: `ant.Func[R](name, args...)`
- [x] Table-valued functions: `ant.FromFunc`
- [x] Random ordering: `q.OrderByRandom()`
- [x] Clear ordering: `q.ClearOrder()`
- [x] Newest and oldest helpers: `q.Latest(&U.CreatedAt)`, `q.Oldest`
- [x] Raw fragments in typed queries: `ant.SQL[V](sql, args...)`
- [x] Raw where, having, order, and group fragments: `ant.SQLCond`, `ant.SQL[V]`
- [x] Compose typed queries over raw SQL: `ant.FromRaw[T](db, sql)`
- [x] Pessimistic locks: `q.ForUpdate()`, `q.ForShare()`, `NoWait`, `SkipLocked`
- [x] Optimizer and index hints: `q.Hint(...)`
  - PostgreSQL itself ignores hints: it needs the `pg_hint_plan` extension. On MySQL and MariaDB the hint goes after `SELECT`.
- [x] Query comments: `q.Tag("name")`
- [x] Column-to-column update expressions: `ant.SetExpr(&U.Age, ant.Col(&U.Age).Plus(1))`

### Writing

- [x] Batched bulk insert: `db.Users.InsertMany(ctx, users, ant.BatchSize(500))`
- [x] Insert from a query: `db.Users.InsertFrom(ctx, ant.Project[T](q, mappings...))`
- [x] Insert ignoring conflicts: `ant.OnConflict(&U.Email).DoNothing()`
- [x] Upsert on conflict: `db.Users.Upsert(ctx, v, ant.OnConflict(&U.Email))`
- [x] Upsert selected columns: `.DoUpdate(ant.F(&U.Name))`, `.Set(...)`
- [x] Upsert returning rows: the entity receives the stored row
- [x] `MERGE` where supported: `ant.Merge`
  - Needs PostgreSQL 15 or later. `OnConflict` does most of this on older versions.
- [x] Find or create: `db.Users.FirstOrCreate(ctx, v, conds...)`
- [x] Find or initialize without saving: `db.Users.FirstOrNew(ctx, v, conds...)`
- [x] Update or create: `db.Users.Upsert`
- [x] Bulk update of different values per row: `db.Users.UpdateMany(ctx, users, ant.F(&U.Age))`
- [x] Bulk update from a query: `ant.SetExpr` with `ant.SubExpr`
- [x] Bulk delete with a join: `ant.Has`, `ant.InSub`, or `ant.Exists` in `Where`
- [x] Increment and decrement: `ant.Inc(&U.Age, 1)`, `ant.Dec`
- [x] Returning rows from update and delete: `q.UpdateReturning`, `q.DeleteReturning`
- [x] Truncate: `db.Users.Truncate(ctx)`
- [x] Bulk copy: PostgreSQL `COPY` through `pgxext.Copy`
- [x] Batched statements in one round trip: `pgxext.Batch`
  - Moved out of the root package, because the wire protocol feature needs pgx itself, and the root module must not import a driver.
- [x] Replicate an entity without its key: `ant.Clone(user)`

### Performance

- [x] Streaming: `q.Each(ctx)`
- [x] Batched reads: `q.Chunk(ctx, 500, fn)`
- [x] Keyed batches: `q.ChunkByKey(ctx, 500, fn)`
- [x] Offset paging helper: `q.Page(ctx, n, size)` returning items and total
- [x] Simple paging without a total: `q.Limit(n).Offset(m)`
- [x] Keyset paging: `q.After(ctx, cursor, size)`
- [x] Encoded cursors: `page.Next`
- [x] Prepared statement cache: pgx caches statements per connection
- [ ] Reuse of rendered SQL for repeated query shapes
  - Skipped: Measured, and not worth it. Rendering a typical query takes about 7 µs, against hundreds for the round trip, and a cache key built from the same AST would cost about what it saves. pgx already caches prepared statements per connection. See [benchmarks](https://github.com/skudra-go/ant-docs/blob/main/docs/reference/benchmarks.mdx).

**Exit criteria:** feature tests pass on all five databases or return a documented capability error. Paging and chunking are tested with at least 100,000 rows.

## v0.6 Modeling depth

### Model definition and mapping

- [x] Embedded structs: anonymous, or `ant:"embedded"`
- [x] Embedded prefix: `ant:"embedded,prefix=addr_"`
- [x] Owned types: `ant:"embedded,prefix=address_"` on a struct field
  - A tag rather than `b.Owned`, in line with how embedded structs already worked.
- [ ] Collections of owned types in a separate table
  - Skipped: A slice of structs in a table of its own is a has-many relation, which already works and has an ID you can reach. For a list that is only ever read with its parent, `ant.JSON[[]T]` keeps it in one column.
- [x] Nested owned types
- [x] Table splitting
  - Several models on one table. Migrations merge them into one table definition.
- [ ] Entity splitting
  - Skipped: One model across two tables would need a join on every read and two writes on every save, hidden behind a call that looks like one row. A 1:1 relation states that cost, and a view covers reads.
- [x] Keyless models for views and raw results: `b.NoKey()`
- [x] View mapping: `b.View("active_users")`
- [x] Table schema: `b.Schema("sales")`
- [x] Field access control: `ant:"readonly"`, `ant:"writeonly"`
- [x] Single-table inheritance with a discriminator: `b.Discriminator`
- [ ] Table-per-type inheritance
  - Skipped: Go has no inheritance to map, so every query would need joins to rebuild something the language does not have. A 1:1 relation from the specific table to the shared one is the same schema, and says so.
- [x] Table-per-concrete-type inheritance
  - Embed a shared struct in each model. Each gets its own table, and no ant feature is involved.
- [ ] Polymorphic models through type switches on a discriminator
  - Skipped: Query results have one static type, `[]T`, so returning several types would mean `[]any` and giving up what ant is for. Map rows to types with a short `switch` on the field, which `tests/features/model_definition/inheritance_test.go` shows.
- [x] Custom naming strategy: `ant.WithNaming(fn)`
  - Global, set in the first `Open`, because models are reflected once per process.
- [x] Bulk conventions: `ant.WithConvention(fn)`
- [x] Column type, length, precision, scale, and collation: `b.Column(&U.Name).Type(...)`
- [x] Fixed-length strings: `.Fixed(n)`
  - Unicode settings do not apply: PostgreSQL text encoding belongs to the database, not the column.
- [x] Table and column comments: `b.Comment`, `.Comment`
- [x] Column order
  - Columns follow the struct's field order. Reordering later is not offered, since PostgreSQL cannot do it without rebuilding the table.
- [x] Unique constraint tag: `ant:"unique"`
- [x] Indexes: `b.Index(&U.Email)`
- [x] Unique indexes: `.Unique()`
- [x] Composite indexes, sort order, names, filters, and included columns
- [x] Functional, full-text, and spatial indexes: `b.IndexExpr(expr)`, `.Using("gin")`
- [x] Check constraints: `b.Check(name, sql)`
- [ ] Table triggers declaration
  - Skipped: Triggers go in `migrations/code`, where they are plain SQL that the runner reapplies when the file changes. Declaring one in Go would only wrap the same SQL in a string, and `drift` could not compare it. `examples/ledger` in ant-examples does this.
- [x] Mixins through embedded configuration: `ConfigureMixin(m *ant.Mixin)`
- [x] Field validators: `b.Validate(&U.Age, fn)`
- [x] Built-in validators: package `validate`
- [x] Immutable fields: `ant:"immutable"`
- [x] Sensitive fields hidden from logs: `.Sensitive()`
- [ ] Deprecated field annotation
  - Skipped: Go already has this: a `// Deprecated:` comment on the field, which gopls and staticcheck report at every use, including probes.
- [x] Strict mode that rejects unknown result columns: `ant.WithStrict()`
- [x] Discard unknown columns: `ant.WithLooseScan()`

### Keys and identity

- [x] Composite primary keys: `b.CompositeKey(ant.F(&T.A), ant.F(&T.B))`, `db.Table.FindKey(ctx, a, b)`
- [x] Composite foreign keys
  - As a constraint, through `b.ForeignKey`. Relations stay single-column, so a relation on a composite key is marked `.NoForeignKey()`, and joins name both columns.
- [x] Alternate keys: `.References(&U.Email)`
- [x] Application-generated keys: `ant.GenerateKey(b, &U.ID, uuid.NewString)`
- [x] UUID, ULID, and snowflake helpers: package `ids`
- [x] Sequences: `b.Column(&O.Number).Sequence(name)`
- [x] Case-insensitive string keys
  - `.Type("citext")`, or a named type with a `ColumnType` method. That needs the `citext` extension, created in `migrations/code`.
- [x] Value objects as keys

### Relations

- [x] Many-to-many: `b.ManyToMany(&U.Roles).Through("user_roles")`
- [x] Join model with payload: a model with two one-to-many relations
- [x] Self-referencing many-to-many: `PivotColumns`
- [ ] Pivot attributes in results
  - Skipped: `[]T` has nowhere to carry extra columns without a wrapper type on every relation. When a join table holds data of its own, it is a model: make it one, with two has-many relations, and all of this comes for free and typed.
- [ ] Filter and order by pivot columns
  - Skipped: Same reason as pivot attributes: model the join table and query it directly.
- [ ] Pivot timestamps
  - Skipped: Same reason as pivot attributes: a modeled join table gets timestamps like any other model.
- [x] Attach and detach: `ant.Link(ctx, db, user, &U.Roles, role)`, `ant.Unlink`
- [x] Sync and toggle: `ant.SyncLinks`, `ant.Toggle`
- [ ] Update a pivot row: `ant.UpdatePivot`
  - Skipped: Same reason as pivot attributes: update the modeled join table.
- [x] Has one through: `b.HasOneThrough`
- [x] Has many through: `b.HasManyThrough`
- [x] Polymorphic relations: `.ForeignKey(&C.OwnerID).Morph(&C.OwnerType, "post")`
- [ ] Polymorphic many-to-many
  - Skipped: It stacks the costs of polymorphism on those of a pivot, with no foreign key to protect either side. Use a modeled join table with a `.Morph` relation.
- [x] Morph type map
  - The value given to `.Morph`, so renaming a Go type cannot change what is stored.
- [x] Latest or oldest of many: `.Latest`, `.Oldest`
- [x] Touch parent timestamps: `.Touch()`
- [x] Scoped relations: `.Where`
- [x] Relations without navigation on one side
- [x] Automatic parent population on children: `.Inverse`
- [x] Association operations: through tracked slices, `Link`, and `CountRelated`

### Value handling

- [x] Value converters
  - Named types with `sql.Scanner` and `driver.Valuer`, by [decision 42](DECISIONS.md#42-converters-are-scanner-and-valuer).
- [x] Built-in converters
  - `ant.JSON`, `ant.Gob`, `ant.UnixTime`, and `postgres.Array`. Same decision.
- [x] Bulk converters for a type
  - A type that implements the interfaces converts wherever it appears, with no registration. Same decision.
- [x] Enum mapping to string or integer: named Go types, or `Scanner` and `Valuer`
- [x] Enums as native database types: `.Enum`
- [x] JSON columns: `ant.JSON[T]`
- [x] JSON path conditions: `postgres.JSONText(&U.Meta, "a", "b")`
- [x] JSON contains and length conditions: `postgres.JSONContains`, `JSONHasKey`, `JSONLength`
- [x] Update inside a JSON column: `postgres.JSONSet`
- [x] Array columns on PostgreSQL: `postgres.Array[T]`
- [x] Primitive collections stored as JSON: `ant.JSON[[]string]`
- [x] Encrypted fields with key rotation: `secure.Value[T]`
  - A field type rather than `.Encrypt`, so a value cannot be read as plain text by mistake. See [encrypted fields](https://github.com/skudra-go/ant-docs/blob/main/docs/recipes/encrypted-fields.mdx).
- [x] Default values in the database: `ant:"default"`
- [x] Computed columns, stored or virtual: `ant:"readonly"`
- [x] Generated values on insert: `ant:"default"`, `ant:"readonly"`
- [x] Accessors and mutators: Go methods, with `Scanner` and `Valuer` for stored forms
- [x] Binary columns: `[]byte`
- [x] Clock for automatic timestamps: `ant.WithNow(fn)`
- [x] Spatial types: `postgres.Point`, `postgres.Near`
  - Native `point` only. PostGIS has hundreds of functions: `ant.SQL` reaches them.
- [x] Vector columns and similarity conditions: `postgres.Vector`
  - Its test skips on a server without pgvector, as the plain test image is.
- [x] Full-text conditions: `postgres.Match`
- [ ] Hierarchy types on SQL Server through a dialect extension
  - Skipped: SQL Server only. Nothing to attach it to until that dialect has a real user.
- [x] Serializers for JSON, gob, and Unix time: `ant.JSON`, `ant.Gob`, `ant.UnixTime`
  - Field types rather than a tag, so the compiler sees them.

### Writing

- [x] Automatic timestamps for `CreatedAt` and `UpdatedAt`
- [x] Skip timestamps: `b.NoTimestamps()`
- [x] Unix timestamp columns: `ant.UnixTime`
- [x] Soft deletes: `ant.DeletedAt` field
- [x] Include deleted: `q.WithDeleted()`
- [x] Only deleted: `q.OnlyDeleted()`
- [x] Restore: `q.Restore(ctx)`
- [x] Force delete: `q.ForceDelete(ctx)`
- [x] Delete flag alternative for unique indexes
  - `b.Index(&U.Email).Unique().Where("deleted_at IS NULL")`, a partial index. It does what a flag does, with no extra column.
- [x] Pruning old rows: `db.Users.Prune(ctx, cond)`

### Lifecycle

- [x] Global query filters: `b.Filter(name, fn)`
- [x] Multiple named filters
- [x] Disable filters: `q.IgnoreFilters(names...)`
- [x] Filters with context data for multi-tenancy
- [x] Scopes: `q.Scope(fn)`
- [x] Parameterized scopes: a function returning a scope
- [ ] Pending attributes from a scope
  - Skipped: This is how Eloquent presets values on a new model. In Go it is a constructor function.

**Exit criteria:** each modeling feature has an integration test on all five databases or a documented capability error. Composite keys work with `Find`, `Include`, and `SaveChanges`.

## v0.7 Lifecycle and ops

### Lifecycle

- [x] Model hooks: optional `BeforeInsert`, `AfterInsert`, `BeforeUpdate`, `AfterUpdate`, `BeforeDelete`, `AfterDelete`, `AfterLoad`
- [x] Hook context and error cancellation
- [x] Validation hook: optional `Validate(ctx) error`, run on every insert and update
- [x] Hook after eager loading: optional `AfterInclude`
- [x] Skip hooks: `ant.WithoutHooks(ctx)`
- [x] Observers: `ant.Observe(db, ant.Observer[User]{...})`
- [x] Observers after commit
- [x] Query interceptors: `ant.WithInterceptor(i)`
- [x] Command interceptors, through `Before` and `After`
- [x] `SaveChanges` interceptors for auditing: `ant.SaveInterceptor`
- [x] Materialization interceptors: `AfterLoad`
- [ ] Identity resolution interceptors
  - Skipped: EF Core uses this to resolve conflicts when a second instance of an entity is attached. ant returns an error there, and `ant.Detach` handles it, so there is no decision to intercept.
- [x] Row-level policies through global filters
- [x] Context events: `ant.WithEvents`
- [x] Multi-tenancy by filter, with an example in `tests/features/lifecycle`
- [x] Plugin API: an `ant.Option` can install interceptors

### Change tracking and unit of work

- [x] Optimistic concurrency token: `ant:"version"`
- [ ] Native tokens: `rowversion` on SQL Server
  - Skipped: SQL Server only. `ant:"version"` covers optimistic concurrency on every database.
- [x] Application-managed tokens: `ant:"token"`
- [x] `ErrConcurrency` with conflict details: `ant.ConcurrencyError`

### Transactions and connections

- [x] Read and write connections: `ant.WithReplicas(...)`
- [x] Replica load balancing: round robin
- [x] Sticky reads after writes: `ant.WithStickyWrites(d)`
- [x] Force primary: `ant.OnPrimary(ctx)`
- [x] Retries: `ant.WithRetry(ant.Retry{...})`
- [x] Deadlock and serialization retry for `Tx`: `ant.Attempts(5)`
- [x] Commit failure guidance: [recipe](https://github.com/skudra-go/ant-docs/blob/main/docs/recipes/commit-failures.mdx)
- [x] Multiple contexts over one pool: `ant.Session(db)`
- [x] Multi-database routing: [recipe](https://github.com/skudra-go/ant-docs/blob/main/docs/recipes/several-databases.mdx)
- [x] Advisory locks through raw SQL or `postgres` helpers

### Developer tooling

- [x] Logging through `log/slog`: `ant.WithLogger(l)`
- [x] Log levels and slow query threshold: `ant.SlowThreshold(d)`
- [x] Sensitive value redaction by default: `ant.LogValues()` opts in
- [x] Debug one query: `q.Debug()`
- [x] SQL inspection: `q.ToSQL()`
- [x] Dry run: `ant.DryRun(ctx, db, fn)`
  - It runs real statements in a transaction that rolls back, so sequences advance, and side effects outside the database still happen.
- [x] Last executed SQL through an interceptor
- [x] OpenTelemetry tracing: module `contrib/otel`
- [x] Metrics sub-package with a Prometheus exporter: package `metrics`
  - No OpenTelemetry exporter, because importing it would pull its SDK into the root module. `Snapshot` returns the numbers, and a few lines in your own code can publish them through any meter.
- [x] Cumulative query time alert: `ant.WithDiagnostics` with `Budget`
- [x] Warn on cartesian explosion
  - Through `ant.WithDiagnostics`.
- [x] Warn on N+1 patterns from repeated statements
  - `RepeatLimit` in `ant.WithDiagnostics`.
- [x] Detailed query errors with SQL context: `ant.WithDetailedErrors`, `ant.FailedSQL`
- [x] Execution plan helper: `q.Explain(ctx)`

**Exit criteria:** hooks, interceptors, and observers each have ordering tests. Sensitive values never appear in default logs, verified by a test. Tracing adds no dependency to the root package.

## v0.8 Migrations

The model is defined in [decision 11](DECISIONS.md#11-migration-model).

### Schema and migrations

- [x] Schema model built from struct metadata
- [x] Schema snapshot file: `migrations/versions/schema.snapshot.json`
- [x] Diff of models against the snapshot, with no database connection
- [x] CLI entry point: `migrate.Main[AppDB](dialect)`
- [x] Generate a migration: `go run ./cmd/migrate diff <name>`
- [x] Generated migrations as plain, editable SQL
- [x] Versioned folder: `migrations/versions`
- [x] Repeatable folder: `migrations/code`
- [x] Repeatable files rerun on checksum change, after versioned files
- [x] Apply: `go run ./cmd/migrate up`
- [x] Status: `go run ./cmd/migrate status`
- [x] Check for pending model changes in CI: `go run ./cmd/migrate check`
- [x] Empty migration for hand-written changes: `diff --empty <name>`
- [x] History table with checksums, applied time, and success
- [x] Custom history table name and schema
- [x] Checksum validation of applied files
- [x] Repair stored checksums: `migrate repair`
- [x] Migration lock during apply
- [x] Drift detection against a live database: `migrate drift`
- [x] Destructive change warnings in generated SQL
- [x] Rename warning comment in generated SQL
- [x] Default source from `io/fs.FS`, including `go:embed`
- [x] Replaceable storage: `migrate.Source` interface
- [x] Replaceable file naming: `migrate.Config{Name: fn}`
- [x] Apply at runtime: `migrate.Up(ctx, db)`
- [x] Idempotent SQL script export: `migrate script`
- [x] Per-dialect migration directories
- [x] Multi-schema migrations
- [x] Exclude a model from migrations: `b.ExcludeFromMigrations()`
- [x] Exclude foreign keys: `.NoForeignKey()` on a relation
- [x] Create or drop a schema directly in tests: `db.EnsureCreated`, `db.EnsureDeleted`
- [x] Squash into a baseline: `migrate squash`
- [x] Dump the current schema: `migrate schema-dump`
- [x] Team divergence detection for duplicate versions
- [x] Reset local database: `migrate reset`
- [x] Skip a migration through configuration
- [x] Online index creation options: `.Concurrently()`
- [ ] Instant column and DDL locking options on MySQL
  - Skipped: MySQL only. PostgreSQL migrations are shipped, and `schema.Dialect` is where this goes when MySQL migrations exist.
- [x] Seeding: `migrate.Seeder` and `go run ./cmd/migrate seed`
- [x] Seed as repeatable SQL in `migrations/code`
- [x] Mute hooks while seeding
- [x] Data migrations in Go: `migrate.Register(version, fn)`
- [x] Export generated SQL for use with other tools: `migrate script --plain`
- [x] Rename without losing data: `diff --rename users.name=title`, or a prompt in a terminal
- [x] Fill existing rows when adding a `NOT NULL` column
- [x] Start from an existing database: `migrate adopt`, `migrate baseline`
- [x] Accept the models with no migration: `migrate snapshot`
- [x] Replaceable SQL generation: `migrate.Config{Render: fn}`
- [x] Automatic index on foreign keys, removed with `.NoIndex()`

The guide is [migrations](https://github.com/skudra-go/ant-docs/blob/main/docs/migrations/overview.mdx). Everything here works on PostgreSQL. Other dialects implement `schema.Dialect`, and return `schema.ErrUnsupported` until then, so the exit criterion for all five databases waits on v0.4.

**Exit criteria:** a model change produces SQL with no database connection, and that SQL applies cleanly on all five databases. `check` fails CI on drift between models and the snapshot. Replacing `migrate.Source` is covered by a test.

## v0.9 Tooling

### Developer tooling

- [x] `go vet` analyzer that flags non-probe field pointers: module `tools/antvet`
- [x] Analyzer check for probes from the wrong model
- [x] Analyzer check for mismatched `Find` key types
- [x] Fixtures in `anttest`: `anttest.Load(t, db, fs)`
- [x] Factories in `anttest`: `anttest.Factory[T]`
- [x] Factory states and overrides
- [x] Factory sequences
- [x] Factory relations and recycled parents: `anttest.Recycle`
  - No pivot attributes, for the same reason ant has none: a join table with data is a model, with a factory of its own.
- [x] Factory build and create
- [x] Factory callbacks
- [x] Transactional test isolation: `anttest.Isolate(t, db)`
- [x] Testing guide: [testing](https://github.com/skudra-go/ant-docs/blob/main/docs/testing.mdx)
- [ ] Reverse engineering structs from a database: `migrate scaffold`
  - Skipped: Owner decision. A database does not say which integer is an enum, what a relation should be called, or what belongs together, so the output needs reading line by line anyway. Instead, [adopting a database](https://github.com/skudra-go/ant-docs/blob/main/docs/migrations/adopting.mdx) gives an AI assistant a prompt, and `drift` checks the result until nothing differs.
- [x] Model inspection command: `migrate inspect`
- [x] Database summary command: `migrate db-show`
- [x] Documentation site: [ant-docs](https://github.com/skudra-go/ant-docs)
  - Docusaurus, with every Go sample type-checked against ant. Publishing it needs a home, which is the owner's call.
- [ ] Runnable examples for every public API
  - Skipped: Partly done, and left open on purpose. `example_test.go` has runnable examples for the core calls, and `tests/features/` shows every feature against a real database, but most calls need a server, which `go test` examples cannot assume. An example for each of the 900 lines in `docs/api.txt` would mostly repeat those tests.
- [x] Recipe guides: [recipes](https://github.com/skudra-go/ant-docs/blob/main/docs/recipes/)
  - Also partitioning, several databases, and commit failures.

**Exit criteria:** the analyzer runs under `go vet -vettool`. Every exported identifier has an example or a guide link. Fixtures and factories are used by ant's own integration suite.

## v1.0 Stability

### Performance

- [x] Benchmarks against GORM, Bun, and `database/sql`: [benchmarks](https://github.com/skudra-go/ant-docs/blob/main/docs/reference/benchmarks.mdx)
  - Plain `database/sql` stands in for sqlc, whose generated code is the same query string and `Scan`. Adding sqlc itself would need a code generation step in this repository to measure the same thing.
- [x] Allocation budget for query building and scanning
- [x] Probe resolution benchmark

### Developer tooling

- [ ] Public API freeze
  - Skipped: Left to the owner. `docs/api.txt` records the candidate, and `tests/api` fails on any change to it, so freezing is a matter of declaring it. The call should come after using it for real, since `Session` options, `exec.Wrap`, and `anttest.Isolate` all changed in this work.
- [x] Security review: [security](https://github.com/skudra-go/ant-docs/blob/main/docs/reference/security.mdx)
  - Done in-house, with known limits listed on that page. An outside review is still worth having before 1.0.
- [x] Upgrade guide: each GitHub release lists its breaking changes
- [x] Compatibility policy: [compatibility](https://github.com/skudra-go/ant-docs/blob/main/docs/reference/compatibility.mdx)

**Exit criteria:** benchmarks are published and reproducible. The security review has no open high-severity findings. The API has no breaking change for one release candidate.

## Not planned

Each entry is a feature from the reference catalogs that ant does not intend to implement.

### Loading and object model

- Lazy loading through field access, proxies, or injected loaders: conflicts with no hidden I/O.
- Automatic eager loading on access: same reason.
- Auto-include configured on the model: every load stays visible at the call site.
- Active record methods such as `user.Save()`, `push`, and `save` on models: saving goes through the context.
- Dynamic relationship properties and runtime-registered relations: not compatible with typed structs.
- Magic factory and relation methods: same reason.
- Service injection and constructor binding: Go structs have no constructors.
- Backing fields, field access modes, and indexer properties: C# and PHP language features.
- Shadow properties and property bag entities: columns must be struct fields.
- Change-tracking proxies and notification entities: Go has no runtime proxies, and snapshots cover the need.
- Shared-type entity types: no equivalent need with Go generics.

### Query and mapping features tied to another language

- LINQ translation and client evaluation: Go has no expression trees.
- Compiled query delegates and compiled models: only needed for expression-tree cost.
- NativeAOT and precompiled queries: .NET specific. Go is already compiled.
- Dynamic models through `IModelCacheKeyFactory`: .NET specific.
- `async` variants of operators: Go uses goroutines.
- Mass assignment guards: Go code assigns struct fields directly.
- Eloquent collection helpers: use the `slices` and `maps` packages.
- API resources, JSON:API resources, and serialization visibility helpers: HTTP concerns outside an ORM.
- Pagination views and URL generation: HTTP concerns. Paging data is provided.
- String-based relation names and conditions as the main API: conflicts with typed queries.
- `db.Cond`-style map conditions: conflicts with typed queries.
- Struct or map conditions that skip zero values: conflicts with no zero-value guessing.
- Update behavior that skips zero fields, including `OmitZero` and `boil.Infer`: same reason.
- Update behaviors that infer columns from Go zero values in `Save` or `Updates`: same reason.

### Migrations

- Down migrations, undo files, and generated rollback scripts: out of scope by owner decision. Write a forward migration.
- `migrate:rollback`, `refresh`, and `reset` to a past version: same reason.
- `AutoMigrate` at startup: changes are generated and reviewed instead.
- Shadow databases: the snapshot makes them unnecessary.
- Migration code in C#, PHP, Fizz, or HCL: migrations are plain SQL, or Go for data migrations.
- Migration bundles: a Go binary with `go:embed` already covers this.
- Atlas linting, plans, checkpoints, schema tests, and Atlas Cloud: Atlas can consume ant's SQL if wanted.
- Declarative schema apply: ant is migration-file based.
- Database security as code, including roles and grants: outside an ORM.
- Terraform, Kubernetes, Argo CD, and Crossplane integrations: deployment tooling.
- T4 scaffolding templates: .NET specific.

### Code generation

- Code generation for models, clients, or queries: conflicts with the core goal.
- sqlc-style generation from SQL: a different approach. `ant.Raw` covers raw SQL.
- Generator templates, plugins, and multiple language outputs: no generator exists.
- GraphQL and gRPC generators: outside an ORM.
- Generated compatibility tests: no generated code to test.

### Databases

- Azure Cosmos DB, MongoDB, and other NoSQL stores: relational only.
- Gremlin and graph traversal APIs: relational only.
- QL: not in wide use, and not a target database.
- In-memory test provider: tests should use a real database, with SQLite for speed.
- Oracle, ClickHouse, DuckDB, Spanner, GaussDB, Snowflake, Databricks, and Redshift in core: community dialects can add them through the public interface.
- CockroachDB, TiDB, YugabyteDB, Aurora DSQL, and Vitess in core: wire-compatible use may work, but is not tested.
- Provider packages for Azure-specific features: outside the five target databases.

### Infrastructure

- Diagnostic listeners and event counters: interceptors and metrics cover this.
- `ConfigureDbContext` composition: functional options cover this.
- Queueable listeners and event classes: Laravel queue features. Observers can start goroutines.
- Multiple result sets from one statement: use `db.DB()`.
- Data dump and import, as in xorm `DumpAll`: use database tools.
- Starter kits: not part of the library.
- Stringable and anonymous cast classes: PHP specific. Value converters cover the need.
- Connection through a framework URL configuration: a DSN is already a URL.
- Sharding: application or proxy concern.
- Built-in result caching, including xorm's LRU cache: stale data risk. Use a cache outside the database.
- `DbContext` pooling: an ant context is cheap, and `database/sql` pools connections.
- Pooled PostgreSQL connection configuration: handled by the driver or PgBouncer.
- `System.Transactions` and distributed transactions: .NET specific.
- `dotnet ef`, Package Manager Console, and Laravel Artisan commands: replaced by `cmd/migrate`.
- Database CLI launcher: use `psql` and similar tools.
- Connection monitoring events such as `db:monitor`: use database monitoring.
- Windows Forms and WPF data binding through `DbSet.Local`: .NET UI specific.
- Logrus and Zap integrations: use `log/slog` adapters.
- Hosted services such as Uptrace, Atlas Cloud, and `sqlc push`: outside the project.
- Connection string formats for specific .NET app types: .NET specific.
- `Must*` panicking variants, as in sqlx: conflicts with explicit errors.
