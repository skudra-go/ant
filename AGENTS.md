# AGENTS.md

Guide for coding agents, and for people, working on ant: a Go ORM where columns are field pointers (`&Users.Age`), models are plain structs, there is no code generation, and migrations are plain SQL generated from the structs. Its siblings are `ant-docs` (the site at skudra.net, which also type-checks every sample) and `ant-examples` (projects that use ant), usually checked out next to this repository.

## Before you change anything

1. Find the decisions about the files you will touch: [`docs/decisions/README.md`](docs/decisions/README.md), section *By file*. Read them. A change that contradicts one needs a new decision (see that README), not a quiet edit.
2. Check [`docs/ROADMAP.md`](docs/ROADMAP.md) for the feature: an unchecked item says why it was skipped, and its *Standing goals* are rules the code keeps.
3. Read [`docs/databases.md`](docs/databases.md) if the change reaches SQL: it is the matrix of what each of the five databases supports, and how ant works around each one's differences.

## Checks

```sh
make fmt                          # lays code out; lint fails on anything it would change
make lint                         # go vet, antvet, antfmt -l, golangci-lint, staticcheck
make test                         # go test ./... and the nested modules; SKIPS every database test
make db-up DATABASES=postgres     # start PostgreSQL in Docker (all five without DATABASES)
make test-pg                      # ./tests/... on PostgreSQL
make test-mysql | test-mariadb | test-sqlite | test-mssql   # a dialect's suite
ANT_TEST_DATABASES=postgres go test ./schema ./migrate .    # packages outside ./tests that use a database
```

- A green `make test` without `ANT_TEST_DATABASES` proves little. Run `make test-pg` for anything that reaches the database, and the target of every other dialect whose code path the change touches. Say which databases ran.
- Default ports are in `docker-compose.yml`. On Windows hosts some may be reserved (`netsh interface ipv4 show excludedportrange protocol=tcp`); then run the server on another port and set `ANT_PG_DSN`, `ANT_MYSQL_DSN`, `ANT_MARIADB_DSN` or `ANT_MSSQL_DSN` (MySQL and MariaDB connect as `root`, with `parseTime=true`). SQLite needs nothing.
- Each test gets its own schema or database and top-level tests run in parallel. A test that changes process-wide state (`WithNaming`, `WithConvention`, `secure.SetKeys`, `slog`'s default, environment variables) calls `testdb.Serial(t)`, or `fixture.Serial(t)` in `tests/features`.
- The race detector needs cgo: `make test TESTFLAGS=-race` with `ANT_TEST_DATABASES` set, as CI does. Without a C compiler on the host, run it in a `golang` container with `--network host`.
- `go build` inside `tools/antfmt` leaves a binary in the tree; use `go build -o /dev/null ./...`.
- Kill processes with `pkill -x name`, never `pkill -f`, which can match your own shell.

## Where the code is

| Path | What it holds |
|---|---|
| `ant.go` | `Open`, `Wrap`, `Session`, the context's state, and tracker claims (`claim`, `ErrConcurrentUse`) |
| `options.go`, `conventions.go` | `With*` options; `WithNaming` and `WithConvention`, which are process-wide |
| `config.go`, `fields.go` | `Configure` and the builders (`ModelBuilder`, `ColumnBuilder`, relations); probes from `ant.Fields` |
| `table.go` | `Table[T]`: `Find`, `Insert`, `DeleteByID`, `Reload`, and query starters |
| `query.go`, `cond.go`, `expr.go`, `aggregate.go` | `Query[T]` and its terminals; conditions; typed expressions; aggregates |
| `join.go`, `subquery.go`, `projection.go`, `paging.go` | joins and lateral joins; `InSub`, `Exists`, CTEs, `FromRaw`; `Project`, `Select`; `Page`, `Chunk`, keyset paging |
| `include.go`, `joined.go`, `pivot.go` | `Include`, `Load`, `Related`; `IncludeJoined`; many-to-many `Link`, `Unlink`, `SyncLinks` |
| `upsert.go`, `merge.go`, `assign.go` | `Upsert`, `InsertMany`, `InsertFrom`, `FirstOrCreate`, `OnConflict`; `Merge`; `Set`, `Inc` |
| `unit.go`, `entry.go`, `graph.go` | `SaveChanges` (the unit of work); `Add`, `Attach`, `Remove`, `Entry`; `TrackGraph` |
| `tx.go` | `Tx`, `Begin`, savepoints, `Attempts`, `UseTx`, and how a rollback settles the tracker |
| `hooks.go`, `events.go`, `intercept.go`, `diagnostics.go` | model hooks and observers; events; interceptors and logging; `DryRun`, `Watch` |
| `errors.go`, `errors_kind.go` | sentinel errors; `IsUniqueViolation` and the other classifiers |
| `raw.go`, `run.go` | `Raw`, `RawFile`; replicas and `OnPrimary` |
| `schema.go` | `SchemaOf`, `EnsureCreated`, `TableOf` |
| `internal/meta` | model metadata: tags, naming, fields, relations |
| `internal/track` | the tracker: entries, snapshots, identity map, claims, rollback histories |
| `internal/exec`, `internal/scan` | the executor every statement goes through; row scanning |
| `qb`, `dialect`, `dialect/<name>` | the SQL AST; rendering, quoting, placeholders and per-database syntax |
| `schema`, `migrate` | DDL model, diff and inspection (PostgreSQL); the migration engine and CLI |
| `anttest`, `secure`, `ids`, `validate`, `metrics` | test helpers; encrypted values; ID generators; validation rules; metrics |
| `contrib/otel`, `tools/antvet`, `tools/antfmt`, `tools/compare` | nested modules: tracing, the analyzer, the formatter, benchmarks against other ORMs |
| `skills/ant/SKILL.md` | the skill that teaches coding agents ant's API |

## Where the tests are

| Path | What it covers |
|---|---|
| `tests/*_test.go` | the cross-database suite (`conformance_test.go` and the rest): runs on every database in `ANT_TEST_DATABASES` |
| `tests/features/<section>/` | one small example test per feature, on PostgreSQL. Users learn ant from these, so they read as documentation. `fixture.Run` opens a context that does not track; `fixture.RunTracked` one that does |
| `tests/matrix` | a grid of source, condition, order, paging and terminal checked against a Go oracle, with SQL snapshots per dialect (`-update`); its `known` list must stay empty |
| `tests/concurrency` | what servers do, under the race detector |
| `tests/api` | `docs/api.txt`, the exported API, and the skill's names; fails on any change |
| `tests/docs` | the decisions' headers and index, and the roadmap's shape |
| `tests/benchmarks` | allocation budgets and benchmarks |
| `internal/testdb` | targets, per-test databases, `ForEach`, `ForOnly`, `Serial` |

## Where bugs hide

A fix is complete when it holds along every axis below that applies. Most review findings are a sibling that was missed.

- **Databases.** PostgreSQL, MySQL, MariaDB, SQLite, SQL Server. Quoting, placeholders (`$1`, `?`, `@p1`), `RETURNING` (PostgreSQL, SQLite, MariaDB) against `OUTPUT` and `LAST_INSERT_ID`, `OFFSET ... FETCH` (SQL Server needs an `ORDER BY`), upsert syntax (`ON CONFLICT`, `ON DUPLICATE KEY`, `MERGE`), `DEFAULT` in a `VALUES` list (not SQLite), booleans and times (UTC everywhere), collations, and MySQL error 1093 when a write reads its own table. PostgreSQL comes first. A bug only on another database, outside the task, becomes its own issue labelled `dialect: <name>`. Migrations exist on PostgreSQL only.
- **Query shape.** `Where`, model filters (`IgnoreFilters`, `Unfiltered`), scopes, `Has`/`HasNone`/`Related`, `InSub`, `Exists`, joins, `Include`/`Load`, `Project`/`Select`/`Pluck`, aggregates, `GroupBy`/`Having`, set operations, CTEs, `FromRaw`/`FromFunc`; and the order they are chained in.
- **Paging.** `Page`, `Pages`, `Chunk`, `ChunkByKey`, `Each`, `First`, `Single`, keyset paging, `Limit`/`Offset`.
- **Write paths.** `Insert`, `Update`, `Delete`, `Upsert`, `Merge`; the bulk ones, `InsertMany`, `UpdateMany`, `InsertFrom`, `PrepareBulk`, split by `BatchSize` at the parameter limit; `*Returning`, `ToSQL`, `DryRun`; `Fields`, `Omit`, `InsertOnly`; `OnConflict`, `DoUpdate`, `DoNothing`; `FirstOrCreate`; soft deletes against `ForceDelete`, `WithDeleted`, `Restore`. The bulk path usually builds its SQL apart from the single one.
- **Tracking.** `SaveChanges` against direct writes, a `Session` against the root that does not track (`ErrNoTracking`), `NoTracking`, `Attach`/`Detach`/`MarkModified`, `TrackGraph`, relations (`Link`, `SyncLinks`, many-to-many), the entity states, timestamps (`NoTimestamps`, `Touch`), versions, and hooks (`WithoutHooks`).
- **Transactions.** Outside one, inside `Tx` or `Begin`, in a savepoint, during an `Attempts` retry, with `WithoutAutoTx`, from `UseTx`, with replicas and `WithStickyWrites`; and what each rollback reverses and forgets in the tracker.
- **Model metadata.** Tags against builder calls, naming and conventions, immutable, readonly, computed, generated and sensitive fields, discriminators, embedded structs, pointer against value fields.
- **Values.** NULL against zero, `*T` against `T`, `sql.Null`, JSON, decimals, UUIDs, byte slices, enums, Unicode, empty `In` lists, parameter limits.

## Conventions

- **Layout.** `make fmt` enforces an airy style: one field or argument per line when a call wraps, and blank lines around blocks and before a `return`. Write it that way from the start.
- **Tests.** A fix starts as a failing test through the public API, and at least one test must fail without the change. Never weaken or skip a test to pass; fix a wrong one openly.
- **Features.** Each gets a short example test under `tests/features/<section>/`.
- **API.** `docs/api.txt` records the exported API. Rewrite it with `go test ./tests/api -update` only for an intended change, and say so in the pull request.
- **Decisions.** Record a choice between behaviours as a new file in `docs/decisions/`, and run `go test ./tests/docs -update`.
- **Roadmap.** An item skipped gets its reason under it, as `  - Skipped: ...`; `tests/docs` checks.
- **Skill.** When the API or its behaviour changes, update `skills/ant/SKILL.md` and type-check it with ant-docs' doccheck (see `CONTRIBUTING.md`).
- **Docs.** User docs live in ant-docs and describe the latest release. List the pages a change affects in the pull request, and change them there once it ships.
- **Migrations.** No down migrations, ever. Plain SQL files the developer owns, with file handling they can replace. No reverse engineering of models from a database: `drift` checks hand-written models instead.
- **Branches.** GitHub Flow on `main`: a branch `fix/<issue>` or `feat/<issue>`, rebased with `git pull --rebase origin main`, never merged, and a pull request into `main`. Pull requests are squash-merged, so the title is the commit, in Conventional Commits (`fix:`, `feat:`, `docs:`, `fix!:` for a breaking change). `main` needs the `lint` and `test` checks, an up-to-date branch, and one approval. Copilot reviews each push.
- **Reviews.** Work only from unresolved review threads, and check each against the code first: some are already fixed.
