# Decisions

Why ant works the way it does. Each decision has a file of its own, `NNN-short-title.md`, named by its number. Numbers never change: code comments, issues, and pull requests cite them as "decision 79".

## Find the ones that apply

- **Before changing a file**, look it up under [By file](#by-file) and read those decisions. A change that contradicts one needs a new decision, not a quiet edit.
- **By topic**, use [By tag](#by-tag), or search the headers: `rg -l '^tags: .*savepoint' docs/decisions`.
- **By area**, use [By area](#by-area).
- **What changed later**: `rg -l '^status: (amended|superseded)' docs/decisions`. Each file says what changed it.
- **What an issue led to**: `rg -l '^issues: .*\b224\b' docs/decisions`.

## How a file reads

```markdown
---
id: 79
title: "A tracking context serves one operation at a time"
area: tracking
status: amended
tags: [ErrConcurrentUse, claims, rollback, hooks, UseTx]
issues: [4]
related: [29, 31, 99]
changed_by: []
code: [ant.go, tx.go, internal/track/track.go]
tests: [tests/concurrency, tests/features/change_tracking/concurrent_use_test.go]
---

# 79. A tracking context serves one operation at a time

**In short:** one sentence on what ant does because of it.

> **Amended.** What no longer holds, and what changed it.

## Decision

The text as it was decided.
```

| Field | Meaning |
|---|---|
| `area` | The group in the index: `models`, `values`, `relations`, `queries`, `writes`, `tracking`, `transactions`, `hooks`, `errors`, `dialects`, `migrations`, `performance`, `observability`, `security`, `testing`, `tooling`, `process` |
| `status` | `active`: the code does this. `amended`: a later decision, or the note, changes part of it. `superseded`: replaced, and kept for its reasons. `historical`: a record of process or history, with no rule the code keeps. `pending`: copied here from a pull request that has not merged yet (`pr` names it) |
| `tags` | Words to search for: API names in their Go form, and the topic |
| `issues` | The GitHub issues the decision cites |
| `related` | Other decisions it builds on or that build on it |
| `changes` | For a pending decision, the decisions it will amend once merged |
| `changed_by` | Later decisions that amended or replaced it |
| `code`, `tests` | The files that carry it out and the tests that hold it, from the repository root |

The `## Decision` text stays as it was decided. When the code moves on, write a new decision, set the old one's `status` and `changed_by`, and add a note under its summary, rather than rewriting its reasons.

## Add a decision

1. Take the next number: `ls docs/decisions | tail -2`. Two branches may take the same one; whichever merges second renumbers.
2. Copy the header above, with `status: active`: the decision merges with the code it describes.
3. Write what was wrong, citing the issue, what ant does now, and each alternative that was rejected and why. Write it for someone who has not seen the issue.
4. Mark any decision it changes: `status: amended` or `superseded`, its number in their `changed_by`, and a note under their summary.
5. Run `go test ./tests/docs -update`, which checks every header and the paths it names, and rewrites the index below.

## History

The first decisions record where the implementation brief (`task.md`, removed from the repository after the first release and still in its history) was ambiguous or needed refining, and what was chosen. Where the brief left a choice open, the option closest to EF Core was taken. The signatures were compiled with Go 1.27.3 against a stub on 2026-10-31.

On 2026-11-20 the decisions moved from one file, `docs/DECISIONS.md`, to a file each, and each was checked against the code. The statuses and notes say what was found. The text of every decision is unchanged.

## Index

<!-- index: written by go test ./tests/docs -update -->

### By area

#### Models and mapping

| # | Decision | Status | Tags |
|---|---|---|---|
| [19](019-a-zero-generated-key-means-assign-one.md) | A zero generated key means "assign one" | active by [150](150-a-zero-key-nothing-fills-is-refused.md) | `generated-key` `NotGenerated` `zero-value` `primary-key` |
| [21](021-configuration-runs-when-a-context-opens.md) | Configuration runs when a context opens | active | `Configure` `Open` `Fields` `probes` `table-name` |
| [41](041-automatic-timestamps.md) | Automatic timestamps | active | `timestamps` `CreatedAt` `UpdatedAt` `utc` `NoTimestamps` `microseconds` |
| [66](066-several-models-can-share-a-table.md) | Several models can share a table | active | `Discriminator` `table-splitting` `b.Table` `ErrBadSchema` `Open` `SchemaOf` `Configure` |
| [67](067-naming-and-conventions-are-global.md) | Naming and conventions are global | active | `WithNaming` `WithConvention` `naming` `global` `Open` `model-registry` |
| [87](087-an-embedded-field-must-be-a-struct-value.md) | An embedded field must be a struct value | active | `embedded` `pointer` `reflection` `meta` `model-error` |
| [100](100-probes-are-named-after-their-model-in-the-plural.md) | Probes are named after their model, in the plural | active | `probe` `naming` `Fields` `convention` `plural` `tests` |
| [101](101-probes-are-pinned.md) | Probes are pinned | active | `probe` `Pinner` `runtime.Pinner` `gc` `Resolve` `field-pointer` |
| [125](125-open-refuses-a-model-it-would-misread.md) | Open refuses a model it would misread | active | `Open` `tags` `ErrBadSchema` `relations` `embedded` `ManyToMany` `PivotColumns` `netip` |
| [139](139-a-field-can-not-ask-for-two-things-ant-can-not-both-do.md) | A field can't ask for two things ant can't both do | active | `Open` `tags` `ErrBadSchema` `readonly` `writeonly` `immutable` `computed` `version` `token` `embedded` `DeletedAt` `Generated` |
| [150](150-a-zero-key-nothing-fills-is-refused.md) | A zero key that nothing fills is refused | active | `primary-key` `uuid` `zero-value` `GenerateKey` `default` `ErrZeroKey` |
| [151](151-deletedat-is-a-time-or-null-in-json.md) | DeletedAt is a time or null in JSON | active | `DeletedAt` `soft-delete` `JSON` `MarshalJSON` |
| [152](152-open-refuses-values-the-driver-cannot-take.md) | Open refuses values the driver can't take | active | `Open` `ErrNoColumnType` `driver` `slice` `map` `struct` `PlainValues` `Table` |
| [168](168-ids-uuidv7-makes-a-uuid-of-any-16-byte-type.md) | ids.UUIDv7 makes a UUID of any 16-byte type | active | `ids.UUIDv7` `ids.UUID` `GenerateKey` `uuid` `keys` `text` |
| [176](176-ants-structs-have-camelcase-json-keys.md) | ant's structs have camelCase JSON keys | active | `json` `Base` `Page` `CursorPage` `Point` `Partition` `metrics` `breaking` |

#### Values and types

| # | Decision | Status | Tags |
|---|---|---|---|
| [42](042-converters-are-scanner-and-valuer.md) | Converters are Scanner and Valuer | active | `Scanner` `valuer` `JSON` `Array` `converter` `custom-types` |
| [84](084-every-time-is-bound-in-utc-where-columns-keep-no-zone.md) | Every time is bound in UTC where columns keep no zone | active | `utc` `time` `sql-null` `nulltime` `DeletedAt` `raw-query` `sqlserver` `sqlite` |
| [95](095-in-means-eq-and-other-value-rules.md) | In means Eq, and other value rules | amended by [134](134-a-type-postgresql-has-a-column-for-maps-to-it-and-reads-back.md), [146](146-ne-matches-null-as-go-does.md) | `In` `NotIn` `Eq` `null` `any-array` `secure.Value` `UnixTime` `utf8` |
| [109](109-numbers-inside-an-interface-read-as-json-number.md) | Numbers inside an interface read as json.Number | active | `json.Number` `UseNumber` `JSON` `secure.Value` `float64` `snowflake` |
| [134](134-a-type-postgresql-has-a-column-for-maps-to-it-and-reads-back.md) | A type PostgreSQL has a column for maps to it, and reads back | active | `ColumnType` `interval` `inet` `cidr` `jsonb` `numeric` `arrays` `time.Duration` `net.IP` `netip` `json.RawMessage` `decimal` `IntervalStyle` `AvgOf` `ErrNoColumnType` |
| [138](138-a-uint64-column-on-postgresql-is-numeric.md) | A uint64 column on PostgreSQL is numeric(20,0), and a uint64 key keeps its value | active | `ColumnType` `uint64` `uint` `numeric` `bigint` `identity` `keys` `foreign keys` `KeyOf` `ManyToMany` |
| [160](160-sqlite-time-aggregates-read-text.md) | SQLite time aggregates read the text their destination can hold | active | `sqlite` `time` `MaxOf` `MinOf` `MaxRelated` `MinRelated` `scan` |

#### Relations

| # | Decision | Status | Tags |
|---|---|---|---|
| [33](033-include-shape-functions.md) | Include shape functions | active | `Include` `ROW_NUMBER` `ErrIncludeInEach` `ErrNoKey` `per-parent-limit` `many-to-many` |
| [34](034-self-joins.md) | Self-joins | active | `self-join` `alias` `Join` `probes` |
| [44](044-many-to-many.md) | Many-to-many | active | `ManyToMany` `join-table` `Toggle` `Include` `on-conflict` `holdlock` `for-update` `pivot` |
| [64](064-through-relations-are-read-only-pivots.md) | Through relations are read-only pivots | active | `HasManyThrough` `HasOneThrough` `read-only` `pivot` `many-to-many` |
| [65](065-scoped-relations-use-the-same-filter-path.md) | Scoped relations use the same filter path | amended by [156](156-scoped-relation-saves-apply-target-eq-values.md) | `Where` `Morph` `Latest` `Oldest` `ErrNoForeignKey` `polymorphic` `Include` `Has` |
| [156](156-scoped-relation-saves-apply-target-eq-values.md) | Scoped relation saves apply target Eq values | active | `Where` `Eq` `SaveChanges` `scoped-relation` |

#### Queries

| # | Decision | Status | Tags |
|---|---|---|---|
| [1](001-ant-ordered-replaces-cmp-ordered.md) | `ant.Ordered` replaces `cmp.Ordered` | superseded by [145](145-helpers-take-a-field-of-any-type.md) | `Ordered` `time` `OrderBy` `Gt` `cmp-ordered` |
| [3](003-find-takes-a-generic-key.md) | `Find` takes a generic key | active | `Find` `generic-key` `comparable` `checkKey` |
| [4](004-conditions-are-not-tied-to-a-model.md) | Conditions are not tied to a model | active | `Cond` `ErrWrongModel` `antvet` `probes` `go-vet` |
| [5](005-select-closures-run-client-side.md) | `Select` closures run client-side | active | `Select` `Join2` `closure` `Project` `client-side` |
| [7](007-eq-with-a-nil-pointer.md) | `Eq` with a nil pointer | amended by [146](146-ne-matches-null-as-go-does.md) | `Eq` `Ne` `nil` `IS-NULL` `pointer` |
| [18](018-wrong-model-detection.md) | Wrong-model detection | active | `ErrWrongModel` `probes` `Where` `terminal-call` |
| [20](020-queries-are-immutable.md) | Queries are immutable | active | `immutable-query` `OrderBy` `ThenBy` `ErrThenWithoutOrder` |
| [24](024-raw-sql-arguments.md) | Raw SQL arguments | active | `Raw` `Bind` `placeholder` `slice-expansion` `In` `named-args` |
| [25](025-scanning.md) | Scanning | amended by [41](041-automatic-timestamps.md) | `scan` `Rows.Scan` `partial-select` `timestamps` `Base` |
| [35](035-typed-expressions.md) | Typed expressions | active | `Expr` `IntoExpr` `Project` `aggregate` `typed-expression` |
| [36](036-derived-tables-keep-the-models-name.md) | Derived tables keep the model's name | amended by [88](088-sql-server-recurses-with-union-all-and-a-path-of-keys.md) | `From` `FromCTE` `FromRaw` `Union` `Recursive` `derived-table` `CTE` |
| [37](037-references-to-outer-queries.md) | References to outer queries | active | `subquery` `outer-reference` `probes` `terminal-call` `Where` |
| [39](039-global-filters-are-lazy.md) | Global filters are lazy | active | `global-filter` `qb.Lazy` `tenant` `Find` `identity-map` `join-on` |
| [81](081-a-set-operations-members-keep-their-own-paging.md) | A set operation's members keep their own paging | active | `Union` `set-operations` `derived-table` `limit` `offset` `Chunk` `ChunkByKey` `Page` `After` `ErrCursor` |
| [112](112-a-locking-read-puts-the-row-into-the-tracked-instance.md) | A locking read puts the row into the tracked instance | active | `ForUpdate` `ForShare` `Reload` `identity-map` `refreshLocked` `ConcurrencyError` |
| [136](136-an-outer-joins-side-reads-what-only-the-driver-converts.md) | An outer join's side reads what only the driver converts | active | `IncludeJoined` `LeftJoin` `Select` `nullableColumn` `netip` `arrays` `Scanner` |
| [141](141-a-statement-inside-an-each-loop-on-one-connection-is-refused.md) | A statement inside an Each loop on one connection is refused | active | `Each` `ErrQueryInEach` `transactions` `UseConn` `UseTx` `Begin` `RunsWhileStreaming` `streaming` |
| [143](143-paging-arguments-out-of-range-are-errpaging.md) | Paging arguments out of range are ErrPaging | active | `Page` `Chunk` `ChunkByKey` `After` `Limit` `Offset` `ErrPaging` `ErrCursor` `errors` |
| [145](145-helpers-take-a-field-of-any-type.md) | Helpers take a field of any type, and the database checks it | active | `Ordered` `Number` `OrderBy` `Gt` `Between` `Like` `Inc` `SumOf` `MaxOf` `Past` `nullable` `decimal` `uuid` |
| [146](146-ne-matches-null-as-go-does.md) | Ne, NotIn and Not of Eq match NULL, as Go's != does | active | `Ne` `NotIn` `Not` `Eq` `In` `null` `nullable` `De-Morgan` `Expr` `sensitive` |
| [147](147-a-projection-reads-null-into-a-plain-field-as-zero.md) | A projection reads NULL into a plain field as its zero value | active | `Project` `NULL` `zero-value` `LeftJoin` `Max` `aggregate` `Nullable` `sql.Null` |
| [163](163-includejoined-applies-the-relations-own-conditions.md) | IncludeJoined applies the relation's own conditions in its join | active | `IncludeJoined` `Morph` `Where` `Latest` `Oldest` `has-one` `scope` |
| [164](164-explain-reads-each-databases-plan-statement.md) | Explain reads each database's plan statement, and plans the statement All runs | active | `Explain` `sqlite` `mysql` `mariadb` `sqlserver` `IncludeJoined` `dialect` |
| [169](169-a-join-and-a-projection-count-and-show-their-sql.md) | A join and a projection count, test for rows, show their SQL and log it, as a query does | active | `joins` `projections` `Count` `Exists` `ToSQL` `Debug` `Distinct` `Offset` `GroupBy` |
| [171](171-after-pages-a-nullable-column-whose-order-places-its-nulls.md) | After pages a column that can hold NULL once the order places its NULLs | active | `After` `keyset` `cursor` `ErrCursor` `NULL` `NullsFirst` `NullsLast` `paging` |

#### Writes

| # | Decision | Status | Tags |
|---|---|---|---|
| [22](022-inserts-run-one-statement-per-row.md) | Inserts run one statement per row | amended by [63](063-savechanges-batches-inserts.md) | `Insert` `transaction` `generated-key` `Batch` `InsertMany` |
| [23](023-guarding-bulk-writes.md) | Guarding bulk writes | active | `ErrNoCondition` `Unfiltered` `Update` `Delete` `bulk-write` `guard` |
| [38](038-bulk-writes.md) | Bulk writes | active | `bulk-write` `Update` `Delete` `subquery` `FromCTE` `InsertMany` `SQL-Server` `MySQL-1093` |
| [63](063-savechanges-batches-inserts.md) | SaveChanges batches inserts | active | `SaveChanges` `batch-insert` `insertBatchLimit` `hooks` `Touch` `WithoutAutoTx` |
| [75](075-generated-values-without-returning.md) | Generated values without RETURNING | active | `LastInsertId` `mysql` `mariadb` `Upsert` `on-duplicate-key` `generated-keys` |
| [80](080-every-write-moves-the-version-on.md) | Every write moves the version on | amended by [89](089-one-column-one-value.md), [91](091-an-upsert-checks-a-copys-version-on-mysql-and-mariadb.md), [98](098-an-explicit-write-refuses-the-columns-ant-keeps.md) | `version` `optimistic-concurrency` `Upsert` `Merge` `DoUpdate` `ConcurrencyError` `soft-delete` `Restore` `UpdatedAt` |
| [89](089-one-column-one-value.md) | One column, one value | active | `assigned-twice` `Upsert` `Merge` `UpdateMany` `InsertFrom` `DoUpdate` `duplicate-column` |
| [90](090-merge-keeps-what-other-writes-keep.md) | Merge keeps what other writes keep | active | `Merge` `created-at` `updated-at` `version` `Discriminator` `generated-key` |
| [91](091-an-upsert-checks-a-copys-version-on-mysql-and-mariadb.md) | An upsert checks a copy's version on MySQL and MariaDB too | active | `Upsert` `version` `mysql` `mariadb` `on-duplicate-key` `ErrConcurrency` `optimistic-concurrency` |
| [98](098-an-explicit-write-refuses-the-columns-ant-keeps.md) | An explicit write refuses the columns ant keeps | active | `immutable` `readonly` `computed` `Update` `Set` `DoUpdate` `UpdateMany` `WhenMatched` |
| [102](102-updatemany-keeps-what-a-bulk-update-keeps.md) | UpdateMany keeps what a bulk Update keeps | active | `UpdateMany` `version` `UpdatedAt` `global-filter` `soft-delete` `tenant-filter` |
| [103](103-an-upserts-update-stamps-updatedat.md) | An upsert's update stamps UpdatedAt | active | `Upsert` `InsertMany` `DoUpdate` `UpdatedAt` `on-conflict` `excluded` |
| [119](119-an-upserts-update-writes-the-zero-a-default-field-holds.md) | An upsert's update writes the zero a default field holds | active | `upsert` `default` `InsertMany` `WithConflict` `drift` `zero-value` |
| [122](122-what-the-database-fills-is-read-back-by-key.md) | What the database fills is read back by the row's key | active | `readonly` `generated` `trigger` `token` `RETURNING` `LastInsertId` `NoKey` `mysql` |
| [124](124-a-conflict-target-repeats-a-partial-indexs-predicate.md) | A conflict target repeats a partial unique index's predicate | active | `Upsert` `OnConflict` `InsertMany` `WithConflict` `partial-index` `soft-delete` |
| [170](170-insertfrom-takes-a-conflict-clause.md) | InsertFrom takes a conflict clause, whose update overwrites only the mapped columns | active | `InsertFrom` `WithConflict` `OnConflict` `DoUpdate` `DoNothing` `upsert` `INSERT SELECT` `sqlite` `mysql` |

#### Change tracking

| # | Decision | Status | Tags |
|---|---|---|---|
| [6](006-return-shapes-and-tracking.md) | Return shapes and tracking | superseded by [27](027-all-returns-t.md) | `Find` `First` `Each` `All` `tracking` `return-shape` |
| [27](027-all-returns-t.md) | `All` returns `[]*T` | active | `All` `[]*T` `identity-map` `Each` `Raw` |
| [28](028-identity-and-relation-slices.md) | Identity and relation slices | amended by [112](112-a-locking-read-puts-the-row-into-the-tracked-instance.md) | `identity-map` `relation-slice` `SaveChanges` `Find` `Snapshot` |
| [29](029-sessions.md) | Sessions | amended by [61](061-sessions-take-options.md), [79](079-a-tracking-context-serves-one-operation-at-a-time.md), [120](120-the-root-does-not-track-and-a-session-does.md) | `Session` `WithTracking` `tracker` `unit-of-work` `DbContext` |
| [30](030-tracking-follows-keys-not-addresses.md) | Tracking follows keys, not addresses | active | `SaveChanges` `relation-reconcile` `unlink` `orphan` `foreign-key` `null` |
| [32](032-navigation-and-foreign-key-changes.md) | Navigation and foreign key changes | amended by [156](156-scoped-relation-saves-apply-target-eq-values.md) | `foreign-key` `navigation` `move-child` `unlink` `SaveChanges` |
| [61](061-sessions-take-options.md) | Sessions take options | amended by [120](120-the-root-does-not-track-and-a-session-does.md) | `Session` `WithTracking` `SaveChanges` `WithReplicas` `WithLogger` `savepoint` `unit-of-work` |
| [79](079-a-tracking-context-serves-one-operation-at-a-time.md) | A tracking context serves one operation at a time | amended by [120](120-the-root-does-not-track-and-a-session-does.md), [121](121-a-rollback-forgets-what-its-transaction-began-tracking.md) | `ErrConcurrentUse` `tracker-claim` `SaveChanges` `WithTracking` `rollback-reversal` `UseTx` `UseConn` `hooks` `concurrency` |
| [92](092-tracked-values-compare-as-stored.md) | Tracked values compare as stored | active | `change-tracking` `Snapshot` `nan` `time` `deepequal` `valuer` `UnixTime` `SaveChanges` |
| [93](093-tracking-follows-what-a-save-wrote.md) | Tracking follows what a save wrote | active | `change-tracking` `relations` `many-to-many` `Include` `SaveChanges` `InsertOnly` `Remove` `version` |
| [96](096-a-forced-update-stamps-updatedat.md) | A forced update stamps UpdatedAt | active | `MarkModified` `TrackGraph` `UpdatedAt` `forced-update` `SaveChanges` |
| [110](110-a-version-the-application-sets-is-the-one-checked.md) | A version the application sets is the one checked | active | `version` `optimistic-concurrency` `ErrConcurrency` `SaveChanges` `ConcurrencyError` `Attach` |
| [111](111-a-replaced-entity-keeps-what-the-body-could-not-give.md) | A replaced entity keeps what the body could not give | active | `UpdatedAt` `foreign-key` `SaveChanges` `PUT` `relation` `replaced-entity` |
| [120](120-the-root-does-not-track-and-a-session-does.md) | The root does not track, and a session does | active | `WithTracking` `Session` `ErrNoTracking` `root-context` `unit-of-work` |
| [140](140-an-update-that-changed-nothing-is-checked-against-the-row.md) | On MySQL, an update that changed nothing is checked against the row | active | `SaveChanges` `ConcurrencyError` `MySQL` `MariaDB` `RowsAffected` `clientFoundRows` `ChangedRowsOnly` `token` `version` `Update` `UpdateMany` |
| [142](142-chunk-lets-go-of-a-group-that-has-nothing-to-save.md) | Chunk lets go of a group that has nothing to save | active | `Chunk` `ChunkByKey` `tracking` `memory` `Session` `SaveChanges` `ForgetAlone` |
| [148](148-a-query-with-computed-fields-stays-tracked.md) | A query that fills computed fields stays tracked | active | `With` `computed` `tracking` `identity-map` `Session` `SaveChanges` |
| [149](149-a-has-one-pointer-is-the-truth-about-its-row.md) | A has-one pointer is the truth about its row | active | `HasOne` `SaveChanges` `orphan` `unlink` `relation-reconcile` `unique` |
| [153](153-changes-saved-through-another-context-are-reported-by-antvet.md) | Changes saved through another context are reported by antvet | active | `SaveChanges` `Session` `Attach` `tracker` `antvet` `HasChanges` |
| [166](166-find-reads-the-row-after-a-bulk-delete.md) | Find reads the row of an instance tracked before a bulk delete | active | `Find` `Delete` `ForceDelete` `DeleteReturning` `Prune` `Merge` `identity map` `tracking` |

#### Transactions

| # | Decision | Status | Tags |
|---|---|---|---|
| [2](002-tx-is-a-generic-method-on-context.md) | `Tx` is a generic method on `Context` | active | `Tx` `generic-method` `Context` `ErrTxType` |
| [31](031-failed-saves.md) | Failed saves | amended by [121](121-a-rollback-forgets-what-its-transaction-began-tracking.md) | `SaveChanges` `savepoint` `ErrTxDone` `ErrConcurrency` `ErrKeyChanged` `WithoutAutoTx` `WithRetry` `RetryEndsTx` |
| [48](048-replicas.md) | Replicas | active | `WithReplicas` `replica` `OnPrimary` `WithStickyWrites` `read-write-split` |
| [70](070-dryrun-really-runs.md) | DryRun really runs | active | `DryRun` `Rollback` `diagnostics` `Statement` `sequences` |
| [99](099-a-retried-attempt-leaves-nothing-in-the-tracker.md) | A retried attempt leaves nothing in the tracker | amended by [121](121-a-rollback-forgets-what-its-transaction-began-tracking.md) | `Attempts` `retry` `deadlock` `tracker` `Rollback` `serialization-failure` |
| [106](106-begin-inside-a-transaction-opens-a-savepoint.md) | Begin inside a transaction opens a savepoint | active | `Begin` `savepoint` `Commit` `Rollback` `nested-tx` `Tx` |
| [121](121-a-rollback-forgets-what-its-transaction-began-tracking.md) | A rollback forgets what its transaction began tracking | active | `rollback` `tracker` `savepoint` `Attempts` `anttest-isolate` `claims` |
| [154](154-the-ctx-carries-the-transaction.md) | The ctx carries the transaction | active | `InTx` `Tx` `hooks` `ctx` `savepoint` `root` `Session` |
| [155](155-a-bound-transaction-settles-what-its-contexts-did.md) | A bound transaction settles what its contexts did | active | `InTx` `tracker` `rollback` `settle` `goroutines` `ErrTxDone` |
| [167](167-sqlite-row-locks-in-a-transaction-that-holds-the-write-lock.md) | SQLite takes row locks as held in a transaction that holds the write lock | active | `sqlite` `ForUpdate` `ForShare` `row locks` `_txlock` `transactions` `dialect` |

#### Hooks and events

| # | Decision | Status | Tags |
|---|---|---|---|
| [40](040-soft-deletes-are-a-filter.md) | Soft deletes are a filter | active | `soft-delete` `DeletedAt` `ForceDelete` `Remove` `DeleteByID` |
| [47](047-hooks-run-inside-the-write.md) | Hooks run inside the write | active | `hooks` `BeforeInsert` `AfterInsert` `Validate` `Upsert` `Observer` `transaction` `bulk` |
| [123](123-an-after-hooks-edit-is-a-change-to-save.md) | An After hook's edit is a change still to save | active | `hooks` `AfterInsert` `AfterUpdate` `snapshot` `SaveChanges` `Upsert` |
| [157](157-insert-validation-leaves-zero-default-fields-to-the-database.md) | Insert validation leaves zero default fields to the database | active | `Validate` `default` `Insert` `Upsert` `Required` `zero-value` |

#### Errors

| # | Decision | Status | Tags |
|---|---|---|---|
| [45](045-errors-are-classified-not-wrapped.md) | Errors are classified, not wrapped | active | `IsUniqueViolation` `SQLState` `IsForeignKeyViolation` `error-classification` |
| [69](069-errors-carry-their-sql.md) | Errors carry their SQL | active | `FailedSQL` `WithDetailedErrors` `exec.Error` `error-message` `placeholders` |

#### Databases and dialects

| # | Decision | Status | Tags |
|---|---|---|---|
| [12](012-additions-to-the-dialect-contract.md) | Additions to the `Dialect` contract | active | `Dialect` `DriverName` `Open` `database-sql` |
| [13](013-mariadb-shares-the-mysql-package.md) | MariaDB shares the `mysql` package | active | `mariadb` `mysql` `SupportsReturning` `RETURNING` |
| [14](014-the-dialect-interface-exposes-an-internal-type.md) | The `Dialect` interface exposes an internal type | superseded by [62](062-the-statement-ast-is-public.md) | `qb` `Dialect` `Render` `public-ast` `third-party-dialect` |
| [43](043-database-specific-helpers.md) | Database-specific helpers | active | `postgres` `SQL` `SQLCond` `dialect-helpers` `neutral-core` |
| [49](049-postgresql-first.md) | PostgreSQL first | active | `postgres` `Dialect` `renderer` `migrations` `feature-suite` |
| [74](074-dialects-state-their-differences.md) | Dialects state their differences | active | `Syntax` `ErrUnsupported` `RegisterErrors` `NoFilter` `NoILike` `FULL-JOIN` `emulation` `UTCTimes` |
| [76](076-one-clock-for-times-without-a-zone.md) | One clock for times without a zone | active | `utc` `UTCTimes` `sqlite` `sqlserver` `datetime2` `time-zone` |
| [85](085-mysql-reads-the-table-it-writes-through-a-derived-table.md) | MySQL reads the table it writes through a derived table | active | `mysql` `error-1093` `derived-table` `no-merge` `self-read` `Upsert` `on-duplicate-key` `lazy` |
| [88](088-sql-server-recurses-with-union-all-and-a-path-of-keys.md) | SQL Server recurses with UNION ALL and a path of keys | active | `sqlserver` `Recursive` `CTE` `union-all` `maxrecursion` `count` `exists` |
| [118](118-a-sqlite-dsn-says-whether-foreign-keys-are-on.md) | A SQLite DSN says whether foreign keys are on | amended by [159](159-a-sqlite-dsn-names-its-lock-behavior.md) | `sqlite` `foreign-keys` `dsn` `CheckDSN` |
| [137](137-stored-tsvector-columns-have-a-separate-match-helper.md) | Stored tsvector columns have a separate Match helper, and Match writes its configuration | active | `postgres` `Match` `MatchVector` `tsvector` `full-text` `regconfig` `indexes` |
| [159](159-a-sqlite-dsn-names-its-lock-behavior.md) | A SQLite DSN names its lock behavior | active | `sqlite` `dsn` `CheckDSN` `busy_timeout` `_txlock` `SQLITE_BUSY` |
| [165](165-sql-server-orders-only-where-it-counts.md) | SQL Server gets an ORDER BY only where it counts, and only from the select list under DISTINCT | active | `sqlserver` `order` `subquery` `derived table` `CTE` `DISTINCT` `NULLS FIRST` `NULLS LAST` `paging` |

#### Migrations and schema

| # | Decision | Status | Tags |
|---|---|---|---|
| [11](011-migration-model.md) | Migration model | amended by [55](055-renames.md) | `migrate` `Snapshot` `diff` `versions` `repeatable` `no-down` `Main` `fs.FS` |
| [50](050-three-layers-for-migrations.md) | Three layers for migrations | active | `schema` `migrate` `Config.Render` `schema.Dialect` `ErrUnsupported` `layers` `postgres` |
| [51](051-modeling-added-for-migrations.md) | Modeling added for migrations | active | `b.Column` `b.Index` `b.IndexExpr` `b.Check` `default` `nullable` `schema.Typed` `sql.Null` |
| [52](052-new-not-null-columns-fill-existing-rows.md) | New NOT NULL columns fill existing rows | amended by [131](131-a-foreign-key-column-follows-its-target.md) | `not-null` `add-column` `default` `zero-value` `gen_random_uuid` `now` `warning` |
| [53](053-names-follow-postgresqls-defaults.md) | Names follow PostgreSQL's defaults | active | `naming` `pkey` `fkey` `index-name` `identifier-limit` `hash` |
| [54](054-foreign-keys.md) | Foreign keys | active | `foreign-key` `OnDelete` `NoAction` `NoIndex` `index` `relations` |
| [55](055-renames.md) | Renames | active | `rename` `--rename` `prompt` `drift` `warning` |
| [56](056-failure.md) | Failure | active | `transaction` `ant:no-transaction` `repair` `RESET ALL` `history` `CONCURRENTLY` |
| [57](057-drift-compares-what-the-server-stored.md) | Drift compares what the server stored | active | `drift` `temp-table` `SameExpression` `pg_get_expr` `Snapshot` `Rollback` |
| [58](058-squashing-needs-no-coordination.md) | Squashing needs no coordination | active | `squash` `ant:replaces` `baseline` `--keep` `migrations` |
| [59](059-adopting-an-existing-database.md) | Adopting an existing database | active | `adopt` `baseline` `drift` `pg_dump` `reverse-engineering` `scaffold` |
| [60](060-one-program-two-jobs.md) | One program, two jobs | active | `Config.Embedded` `embed` `migrate` `Dir` `go-run` |
| [72](072-partitions-are-not-migrations.md) | Partitions are not migrations | amended by [130](130-partitioning-is-part-of-the-table.md) | `partitions` `MonthlyPartitions` `drift` `postgres` `partitioned-table` |
| [73](073-drift-compares-what-models-declare.md) | Drift compares what models declare | active | `drift` `enum` `sequence` `tables` `extension` |
| [78](078-a-replaced-column-takes-its-dependents-along-and-they.md) | A replaced column takes its dependents along, and they come back | active | `generated-column` `DropColumn` `Again` `Replaces` `CONCURRENTLY` `DROP-EXPRESSION` `foreign-key` |
| [82](082-a-rename-must-land-on-a-name-the-models-have.md) | A rename must land on a name the models have | active | `rename` `diff` `schema-diff` `migrations` `Snapshot` `typo` `data-loss` |
| [94](094-migrations-keep-a-columns-dependents-working-while-it.md) | Migrations keep a column's dependents working while it changes | active | `postgres` `migrations` `generated-column` `enum` `rename` `foreign-key` `identity` `NullsFirst` `NullsLast` `IndexBuilder` |
| [113](113-a-missing-versions-folder-is-an-error.md) | A missing versions folder is an error | active | `migrate` `fs.ErrNotExist` `versions-folder` `Config.FS` `embed` `history-table` |
| [114](114-an-identity-columns-default-is-not-ants-to-change.md) | An identity column's default is not ant's to change | active | `identity` `serial` `nextval` `diff` `DROP-DEFAULT` `drift` `postgres` |
| [115](115-a-file-outside-a-transaction-is-recorded-before-it-runs.md) | A file outside a transaction is recorded before it runs | active | `no-transaction` `migrate-up` `history` `crash` `ErrFailed` |
| [116](116-code-files-rerun-after-versioned-migrations.md) | Code files rerun after versioned migrations | active | `repeatable` `code-files` `views` `migrate-up` `outdated` |
| [117](117-the-migrate-program-takes-the-applications-options.md) | The migrate program takes the application's options | active | `WithNaming` `WithConvention` `migrate-config` `SchemaOf` `naming` |
| [126](126-a-migration-runs-as-it-says-or-not-at-all.md) | A migration runs as it says, or not at all | active | `no-transaction` `directives` `BEGIN` `COMMIT` `up` `Render` |
| [127](127-commands-refuse-to-guess-which-migration-is-meant.md) | Commands refuse to guess which migration is meant | active | `baseline` `repair` `diff` `check` `snapshot` `renumber` `ErrChecksum` `ErrStaleSnapshot` |
| [128](128-adopt-leaves-out-roles.md) | adopt leaves out what names a role | active | `adopt` `pg_dump` `OWNER TO` `GRANT` `REVOKE` `baseline` |
| [129](129-status-check-fails-a-deploy-gate.md) | status --check fails while anything waits | active | `status` `ErrPending` `ErrChecksum` `CI` `deploy` |
| [130](130-partitioning-is-part-of-the-table.md) | How a table is partitioned is part of the table | active | `partitions` `PartitionByRange` `diff` `drift` `check` `Repartition` |
| [131](131-a-foreign-key-column-follows-its-target.md) | A new foreign key column follows its target | active | `foreign-keys` `uuid` `Type` `backfill` `NOT NULL` `SET NOT NULL` |
| [132](132-default-names-are-valid-and-distinct.md) | Names ant writes are valid, quoted, and distinct | active | `naming` `IndexName` `UTF-8` `Enum` `Sequence` `quoting` `ErrBadSchema` |
| [133](133-a-concurrent-build-clears-its-leftover.md) | A concurrent index build clears its own leftover | active | `Concurrently` `CREATE INDEX CONCURRENTLY` `invalid index` `drift` `Inspect` |
| [135](135-a-column-ant-made-for-an-earlier-default-is-converted-as-ant-stored-it.md) | A column ant made for an earlier default is converted as ant stored it | active | `AlterColumn` `USING` `interval` `inet` `jsonb` `bigint` `bytea` `Type` `upgrade` |
| [144](144-script-stops-before-a-go-migration.md) | script stops before a Go migration, and starts from a version | active | `script` `Go-migrations` `no-transaction` `from` `guarded` `OutOfOrder` |
| [158](158-drift-compares-two-explicit-serial-defaults.md) | Drift compares two explicit serial defaults | active | `drift` `identity` `serial` `nextval` `sequence` `postgres` |
| [161](161-a-month-is-read-from-its-bounds.md) | A month of a partitioned table is read from its bounds, not its name | active | `partitions` `MonthlyPartitions` `DropBefore` `Ensure` `List` `postgres` `bounds` |
| [162](162-a-failed-migration-names-its-statement.md) | A failed migration names its statement even when the driver sends no position | active | `migration` `failure` `error` `line` `statement` `Explain` `Split` `DDL` |
| [172](172-an-enum-value-renames-in-place.md) | An enum value renames in place | active | `enum` `rename` `RENAME VALUE` `Hint` `diff` `schema-diff` `migrations` |
| [173](173-drift-names-the-types-the-models-use-and-do-not-create.md) | drift names the types the models use and do not create | active | `drift` `adopt` `domain` `extension` `types` `migrations` |
| [174](174-drift-checks-the-views-that-view-models-read.md) | drift checks the views that view models read | active | `drift` `view` `materialized-view` `db-show` `migrations` |

#### Performance

| # | Decision | Status | Tags |
|---|---|---|---|
| [105](105-read-mostly-registries-are-copy-on-write-snapshots.md) | Read-mostly registries are copy-on-write snapshots | active | `atomic.Pointer` `copy-on-write` `registry` `keyring` `Observe` `contention` `lock-free` |
| [107](107-hot-paths-allocate-for-the-operation-not-for-each-row.md) | Hot paths allocate for the operation, not for each row | active | `allocations` `Configure` `dialect.Appender` `qb.ParamOf` `budget` `sync.Pool` `identity-map` |
| [108](108-kept-sql-by-shape-unboxed-keys-compact-snapshots-and.md) | Kept SQL by shape, unboxed keys, compact snapshots, and moved elements followed | active | `shape-cache` `IntKey` `Snapshot` `reconcile` `adopt` `Static` `Include` `identity-map` |

#### Logging and tracing

| # | Decision | Status | Tags |
|---|---|---|---|
| [46](046-interceptors.md) | Interceptors | active | `Interceptor` `WithLogger` `LogValues` `WithInterceptor` `executor` `otel` |
| [97](097-spans-name-the-database-the-dialect-talks-to.md) | Spans name the database the dialect talks to | active | `otel` `db.system` `WithDialect` `WithSystem` `tracing` `Interceptor` |

#### Security

| # | Decision | Status | Tags |
|---|---|---|---|
| [68](068-sensitive-values-are-wrapped-when-bound.md) | Sensitive values are wrapped when bound | active | `Sensitive` `qb.Secret` `redacted` `readsSensitive` `logging` `LogValues` |

#### Testing

| # | Decision | Status | Tags |
|---|---|---|---|
| [16](016-test-schema-is-hand-written.md) | Test schema is hand-written | active | `testdb` `schema` `fixture` `hand-written-ddl` |
| [26](026-feature-examples-run-on-postgresql-only.md) | Feature examples run on PostgreSQL only | active | `features` `postgres` `fixture` `schema-per-test` `parallel` |
| [86](086-combinations-are-tested-against-an-oracle.md) | Combinations are tested against an oracle | active | `matrix` `oracle` `three-valued-logic` `conformance` `sql-snapshots` `known-failures` |

#### Tooling

| # | Decision | Status | Tags |
|---|---|---|---|
| [10](010-module-path.md) | Module path | active | `module-path` `go-mod` `import-path` |
| [15](015-integration-failures-do-not-fail-ci-yet.md) | Integration failures do not fail CI yet | historical | `ci` `github-actions` `continue-on-error` |
| [62](062-the-statement-ast-is-public.md) | The statement AST is public | active | `qb` `AST` `Dialect` `public-package` `stability` |
| [71](071-dependencies-stay-out-of-the-root-package.md) | Dependencies stay out of the root package | active | `dependencies` `pgxext` `ErrInTx` `contrib/otel` `antvet` `Copy` `Batch` |
| [77](077-layout-is-enforced-by-tools.md) | Layout is enforced by tools | active | `make-fmt` `golines` `wsl_v5` `nlreturn` `antfmt` `lint` `layout` |
| [83](083-antvet-judges-only-what-it-can-see.md) | antvet judges only what it can see | amended by [175](175-antvet-follows-a-query-built-up-in-a-variable.md) | `antvet` `analyzer` `go-vet` `types-identical` `Find` `Configure` `subquery` `antvet-ignore` |
| [104](104-a-skill-for-coding-agents-ships-in-the-module.md) | A skill for coding agents ships in the module | active | `skill` `SKILL.md` `llms.txt` `claude-plugin` `agents` `doccheck` |
| [175](175-antvet-follows-a-query-built-up-in-a-variable.md) | antvet follows a query built up in a variable | active | `antvet` `analyzer` `subquery` `variable` |

#### Process and history

| # | Decision | Status | Tags |
|---|---|---|---|
| [8](008-step-1-file-count.md) | Step 1 file count | historical | `brief` `step-1` `history` |
| [9](009-owner-review-of-step-1.md) | Owner review of Step 1 | historical | `owner-review` `Select` `closure` `step-1` |
| [17](017-implemented-ahead-of-the-skeleton.md) | Implemented ahead of the skeleton | historical | `skeleton` `ErrNotImplemented` `history` |

### By file

Before changing a file, read the decisions about it.

| File | Decisions |
|---|---|
| `.claude-plugin` | [104](104-a-skill-for-coding-agents-ships-in-the-module.md) |
| `.github/workflows/pr-ci.yml` | [15](015-integration-failures-do-not-fail-ci-yet.md) |
| `.golangci.yml` | [77](077-layout-is-enforced-by-tools.md) |
| `Makefile` | [77](077-layout-is-enforced-by-tools.md) [83](083-antvet-judges-only-what-it-can-see.md) |
| `aggregate.go` | [134](134-a-type-postgresql-has-a-column-for-maps-to-it-and-reads-back.md) [145](145-helpers-take-a-field-of-any-type.md) [169](169-a-join-and-a-projection-count-and-show-their-sql.md) |
| `ant.go` | [12](012-additions-to-the-dialect-contract.md) [21](021-configuration-runs-when-a-context-opens.md) [29](029-sessions.md) [40](040-soft-deletes-are-a-filter.md) [61](061-sessions-take-options.md) [66](066-several-models-can-share-a-table.md) [67](067-naming-and-conventions-are-global.md) [79](079-a-tracking-context-serves-one-operation-at-a-time.md) [120](120-the-root-does-not-track-and-a-session-does.md) [121](121-a-rollback-forgets-what-its-transaction-began-tracking.md) [125](125-open-refuses-a-model-it-would-misread.md) [154](154-the-ctx-carries-the-transaction.md) [155](155-a-bound-transaction-settles-what-its-contexts-did.md) [167](167-sqlite-row-locks-in-a-transaction-that-holds-the-write-lock.md) [176](176-ants-structs-have-camelcase-json-keys.md) |
| `ant_test.go` | [1](001-ant-ordered-replaces-cmp-ordered.md) [4](004-conditions-are-not-tied-to-a-model.md) [17](017-implemented-ahead-of-the-skeleton.md) [18](018-wrong-model-detection.md) [100](100-probes-are-named-after-their-model-in-the-plural.md) |
| `anttest/anttest.go` | [121](121-a-rollback-forgets-what-its-transaction-began-tracking.md) |
| `assign.go` | [89](089-one-column-one-value.md) [145](145-helpers-take-a-field-of-any-type.md) |
| `builder.go` | [4](004-conditions-are-not-tied-to-a-model.md) [18](018-wrong-model-detection.md) [20](020-queries-are-immutable.md) [34](034-self-joins.md) [37](037-references-to-outer-queries.md) [143](143-paging-arguments-out-of-range-are-errpaging.md) [169](169-a-join-and-a-projection-count-and-show-their-sql.md) |
| `cond.go` | [1](001-ant-ordered-replaces-cmp-ordered.md) [4](004-conditions-are-not-tied-to-a-model.md) [7](007-eq-with-a-nil-pointer.md) [23](023-guarding-bulk-writes.md) [68](068-sensitive-values-are-wrapped-when-bound.md) [95](095-in-means-eq-and-other-value-rules.md) [145](145-helpers-take-a-field-of-any-type.md) [146](146-ne-matches-null-as-go-does.md) |
| `config.go` | [19](019-a-zero-generated-key-means-assign-one.md) [21](021-configuration-runs-when-a-context-opens.md) [41](041-automatic-timestamps.md) [51](051-modeling-added-for-migrations.md) [54](054-foreign-keys.md) [64](064-through-relations-are-read-only-pivots.md) [65](065-scoped-relations-use-the-same-filter-path.md) [119](119-an-upserts-update-writes-the-zero-a-default-field-holds.md) [145](145-helpers-take-a-field-of-any-type.md) |
| `contrib/otel` | [46](046-interceptors.md) [71](071-dependencies-stay-out-of-the-root-package.md) |
| `contrib/otel/otel.go` | [97](097-spans-name-the-database-the-dialect-talks-to.md) |
| `contrib/otel/otel_test.go` | [97](097-spans-name-the-database-the-dialect-talks-to.md) |
| `conventions.go` | [67](067-naming-and-conventions-are-global.md) [117](117-the-migrate-program-takes-the-applications-options.md) |
| `diagnostics.go` | [70](070-dryrun-really-runs.md) |
| `dialect/appender_test.go` | [107](107-hot-paths-allocate-for-the-operation-not-for-each-row.md) |
| `dialect/bind.go` | [24](024-raw-sql-arguments.md) [76](076-one-clock-for-times-without-a-zone.md) [84](084-every-time-is-bound-in-utc-where-columns-keep-no-zone.md) [134](134-a-type-postgresql-has-a-column-for-maps-to-it-and-reads-back.md) |
| `dialect/bind_test.go` | [24](024-raw-sql-arguments.md) |
| `dialect/dialect.go` | [12](012-additions-to-the-dialect-contract.md) [14](014-the-dialect-interface-exposes-an-internal-type.md) [49](049-postgresql-first.md) [140](140-an-update-that-changed-nothing-is-checked-against-the-row.md) [167](167-sqlite-row-locks-in-a-transaction-that-holds-the-write-lock.md) |
| `dialect/dialect_test.go` | [12](012-additions-to-the-dialect-contract.md) [74](074-dialects-state-their-differences.md) |
| `dialect/insert_select_test.go` | [170](170-insertfrom-takes-a-conflict-clause.md) |
| `dialect/mysql/mysql.go` | [13](013-mariadb-shares-the-mysql-package.md) [71](071-dependencies-stay-out-of-the-root-package.md) [74](074-dialects-state-their-differences.md) [75](075-generated-values-without-returning.md) [85](085-mysql-reads-the-table-it-writes-through-a-derived-table.md) [140](140-an-update-that-changed-nothing-is-checked-against-the-row.md) [152](152-open-refuses-values-the-driver-cannot-take.md) [164](164-explain-reads-each-databases-plan-statement.md) |
| `dialect/mysql/mysql_test.go` | [13](013-mariadb-shares-the-mysql-package.md) |
| `dialect/postgres/array_test.go` | [43](043-database-specific-helpers.md) |
| `dialect/postgres/ddl.go` | [50](050-three-layers-for-migrations.md) [52](052-new-not-null-columns-fill-existing-rows.md) [78](078-a-replaced-column-takes-its-dependents-along-and-they.md) [114](114-an-identity-columns-default-is-not-ants-to-change.md) [130](130-partitioning-is-part-of-the-table.md) [131](131-a-foreign-key-column-follows-its-target.md) [133](133-a-concurrent-build-clears-its-leftover.md) [134](134-a-type-postgresql-has-a-column-for-maps-to-it-and-reads-back.md) [135](135-a-column-ant-made-for-an-earlier-default-is-converted-as-ant-stored-it.md) [138](138-a-uint64-column-on-postgresql-is-numeric.md) [172](172-an-enum-value-renames-in-place.md) |
| `dialect/postgres/ddl_test.go` | [134](134-a-type-postgresql-has-a-column-for-maps-to-it-and-reads-back.md) [135](135-a-column-ant-made-for-an-earlier-default-is-converted-as-ant-stored-it.md) [138](138-a-uint64-column-on-postgresql-is-numeric.md) |
| `dialect/postgres/expression.go` | [57](057-drift-compares-what-the-server-stored.md) |
| `dialect/postgres/external.go` | [173](173-drift-names-the-types-the-models-use-and-do-not-create.md) |
| `dialect/postgres/functions.go` | [42](042-converters-are-scanner-and-valuer.md) [43](043-database-specific-helpers.md) [137](137-stored-tsvector-columns-have-a-separate-match-helper.md) [145](145-helpers-take-a-field-of-any-type.md) [176](176-ants-structs-have-camelcase-json-keys.md) |
| `dialect/postgres/inspect.go` | [57](057-drift-compares-what-the-server-stored.md) [72](072-partitions-are-not-migrations.md) [73](073-drift-compares-what-models-declare.md) [114](114-an-identity-columns-default-is-not-ants-to-change.md) [133](133-a-concurrent-build-clears-its-leftover.md) [158](158-drift-compares-two-explicit-serial-defaults.md) [174](174-drift-checks-the-views-that-view-models-read.md) |
| `dialect/postgres/partitions.go` | [72](072-partitions-are-not-migrations.md) [161](161-a-month-is-read-from-its-bounds.md) [176](176-ants-structs-have-camelcase-json-keys.md) |
| `dialect/postgres/pgxext` | [71](071-dependencies-stay-out-of-the-root-package.md) |
| `dialect/postgres/postgres.go` | [43](043-database-specific-helpers.md) [49](049-postgresql-first.md) |
| `dialect/postgres/session.go` | [56](056-failure.md) [174](174-drift-checks-the-views-that-view-models-read.md) |
| `dialect/postgres/split_test.go` | [57](057-drift-compares-what-the-server-stored.md) |
| `dialect/render.go` | [36](036-derived-tables-keep-the-models-name.md) [38](038-bulk-writes.md) [39](039-global-filters-are-lazy.md) [44](044-many-to-many.md) [49](049-postgresql-first.md) [74](074-dialects-state-their-differences.md) [81](081-a-set-operations-members-keep-their-own-paging.md) [84](084-every-time-is-bound-in-utc-where-columns-keep-no-zone.md) [85](085-mysql-reads-the-table-it-writes-through-a-derived-table.md) [88](088-sql-server-recurses-with-union-all-and-a-path-of-keys.md) [91](091-an-upsert-checks-a-copys-version-on-mysql-and-mariadb.md) [95](095-in-means-eq-and-other-value-rules.md) [107](107-hot-paths-allocate-for-the-operation-not-for-each-row.md) [124](124-a-conflict-target-repeats-a-partial-indexs-predicate.md) [165](165-sql-server-orders-only-where-it-counts.md) [170](170-insertfrom-takes-a-conflict-clause.md) |
| `dialect/render_test.go` | [49](049-postgresql-first.md) [74](074-dialects-state-their-differences.md) |
| `dialect/self_reads_test.go` | [38](038-bulk-writes.md) [85](085-mysql-reads-the-table-it-writes-through-a-derived-table.md) |
| `dialect/sqlite/sqlite.go` | [74](074-dialects-state-their-differences.md) [76](076-one-clock-for-times-without-a-zone.md) [118](118-a-sqlite-dsn-says-whether-foreign-keys-are-on.md) [141](141-a-statement-inside-an-each-loop-on-one-connection-is-refused.md) [152](152-open-refuses-values-the-driver-cannot-take.md) [159](159-a-sqlite-dsn-names-its-lock-behavior.md) [164](164-explain-reads-each-databases-plan-statement.md) [167](167-sqlite-row-locks-in-a-transaction-that-holds-the-write-lock.md) |
| `dialect/sqlite/sqlite_test.go` | [118](118-a-sqlite-dsn-says-whether-foreign-keys-are-on.md) [159](159-a-sqlite-dsn-names-its-lock-behavior.md) [167](167-sqlite-row-locks-in-a-transaction-that-holds-the-write-lock.md) |
| `dialect/sqlserver/sqlserver.go` | [74](074-dialects-state-their-differences.md) [76](076-one-clock-for-times-without-a-zone.md) [88](088-sql-server-recurses-with-union-all-and-a-path-of-keys.md) [152](152-open-refuses-values-the-driver-cannot-take.md) [164](164-explain-reads-each-databases-plan-statement.md) [165](165-sql-server-orders-only-where-it-counts.md) |
| `dialect/syntax.go` | [31](031-failed-saves.md) [45](045-errors-are-classified-not-wrapped.md) [74](074-dialects-state-their-differences.md) [76](076-one-clock-for-times-without-a-zone.md) [85](085-mysql-reads-the-table-it-writes-through-a-derived-table.md) [88](088-sql-server-recurses-with-union-all-and-a-path-of-keys.md) [91](091-an-upsert-checks-a-copys-version-on-mysql-and-mariadb.md) [141](141-a-statement-inside-an-each-loop-on-one-connection-is-refused.md) [152](152-open-refuses-values-the-driver-cannot-take.md) [164](164-explain-reads-each-databases-plan-statement.md) [165](165-sql-server-orders-only-where-it-counts.md) |
| `dialect/syntax_test.go` | [74](074-dialects-state-their-differences.md) [88](088-sql-server-recurses-with-union-all-and-a-path-of-keys.md) |
| `dialect/utc_test.go` | [76](076-one-clock-for-times-without-a-zone.md) [84](084-every-time-is-bound-in-utc-where-columns-keep-no-zone.md) |
| `docs/DECISIONS.md` | [8](008-step-1-file-count.md) |
| `entry.go` | [28](028-identity-and-relation-slices.md) [30](030-tracking-follows-keys-not-addresses.md) [79](079-a-tracking-context-serves-one-operation-at-a-time.md) [93](093-tracking-follows-what-a-save-wrote.md) [96](096-a-forced-update-stamps-updatedat.md) [110](110-a-version-the-application-sets-is-the-one-checked.md) [120](120-the-root-does-not-track-and-a-session-does.md) |
| `errors.go` | [17](017-implemented-ahead-of-the-skeleton.md) [18](018-wrong-model-detection.md) [31](031-failed-saves.md) [65](065-scoped-relations-use-the-same-filter-path.md) [110](110-a-version-the-application-sets-is-the-one-checked.md) [120](120-the-root-does-not-track-and-a-session-does.md) [143](143-paging-arguments-out-of-range-are-errpaging.md) [150](150-a-zero-key-nothing-fills-is-refused.md) |
| `errors_kind.go` | [45](045-errors-are-classified-not-wrapped.md) [69](069-errors-carry-their-sql.md) |
| `example_test.go` | [100](100-probes-are-named-after-their-model-in-the-plural.md) |
| `expr.go` | [35](035-typed-expressions.md) [43](043-database-specific-helpers.md) [68](068-sensitive-values-are-wrapped-when-bound.md) [145](145-helpers-take-a-field-of-any-type.md) [146](146-ne-matches-null-as-go-does.md) [147](147-a-projection-reads-null-into-a-plain-field-as-zero.md) |
| `go.mod` | [10](010-module-path.md) |
| `graph.go` | [93](093-tracking-follows-what-a-save-wrote.md) [120](120-the-root-does-not-track-and-a-session-does.md) |
| `hooks.go` | [47](047-hooks-run-inside-the-write.md) [105](105-read-mostly-registries-are-copy-on-write-snapshots.md) [123](123-an-after-hooks-edit-is-a-change-to-save.md) [154](154-the-ctx-carries-the-transaction.md) [157](157-insert-validation-leaves-zero-default-fields-to-the-database.md) |
| `ids/ids.go` | [168](168-ids-uuidv7-makes-a-uuid-of-any-16-byte-type.md) |
| `ids/ids_test.go` | [168](168-ids-uuidv7-makes-a-uuid-of-any-16-byte-type.md) |
| `include.go` | [33](033-include-shape-functions.md) [44](044-many-to-many.md) [65](065-scoped-relations-use-the-same-filter-path.md) [93](093-tracking-follows-what-a-save-wrote.md) [138](138-a-uint64-column-on-postgresql-is-numeric.md) |
| `intercept.go` | [46](046-interceptors.md) |
| `internal/errs/errs.go` | [17](017-implemented-ahead-of-the-skeleton.md) [125](125-open-refuses-a-model-it-would-misread.md) |
| `internal/exec/exec.go` | [69](069-errors-carry-their-sql.md) |
| `internal/exec/observe.go` | [46](046-interceptors.md) |
| `internal/jsontext/jsontext.go` | [109](109-numbers-inside-an-interface-read-as-json-number.md) |
| `internal/liveschema/liveschema.go` | [174](174-drift-checks-the-views-that-view-models-read.md) |
| `internal/meta/interval.go` | [134](134-a-type-postgresql-has-a-column-for-maps-to-it-and-reads-back.md) |
| `internal/meta/key_test.go` | [138](138-a-uint64-column-on-postgresql-is-numeric.md) |
| `internal/meta/meta.go` | [19](019-a-zero-generated-key-means-assign-one.md) [25](025-scanning.md) [40](040-soft-deletes-are-a-filter.md) [41](041-automatic-timestamps.md) [51](051-modeling-added-for-migrations.md) [64](064-through-relations-are-read-only-pivots.md) [65](065-scoped-relations-use-the-same-filter-path.md) [66](066-several-models-can-share-a-table.md) [67](067-naming-and-conventions-are-global.md) [84](084-every-time-is-bound-in-utc-where-columns-keep-no-zone.md) [87](087-an-embedded-field-must-be-a-struct-value.md) [92](092-tracked-values-compare-as-stored.md) [95](095-in-means-eq-and-other-value-rules.md) [98](098-an-explicit-write-refuses-the-columns-ant-keeps.md) [101](101-probes-are-pinned.md) [105](105-read-mostly-registries-are-copy-on-write-snapshots.md) [107](107-hot-paths-allocate-for-the-operation-not-for-each-row.md) [108](108-kept-sql-by-shape-unboxed-keys-compact-snapshots-and.md) [125](125-open-refuses-a-model-it-would-misread.md) [138](138-a-uint64-column-on-postgresql-is-numeric.md) [139](139-a-field-can-not-ask-for-two-things-ant-can-not-both-do.md) [151](151-deletedat-is-a-time-or-null-in-json.md) |
| `internal/meta/meta_test.go` | [66](066-several-models-can-share-a-table.md) [84](084-every-time-is-bound-in-utc-where-columns-keep-no-zone.md) [87](087-an-embedded-field-must-be-a-struct-value.md) [101](101-probes-are-pinned.md) [105](105-read-mostly-registries-are-copy-on-write-snapshots.md) |
| `internal/meta/read.go` | [134](134-a-type-postgresql-has-a-column-for-maps-to-it-and-reads-back.md) [136](136-an-outer-joins-side-reads-what-only-the-driver-converts.md) [152](152-open-refuses-values-the-driver-cannot-take.md) [160](160-sqlite-time-aggregates-read-text.md) |
| `internal/meta/read_test.go` | [134](134-a-type-postgresql-has-a-column-for-maps-to-it-and-reads-back.md) [160](160-sqlite-time-aggregates-read-text.md) |
| `internal/meta/utc.go` | [84](084-every-time-is-bound-in-utc-where-columns-keep-no-zone.md) |
| `internal/scan/scan.go` | [25](025-scanning.md) [84](084-every-time-is-bound-in-utc-where-columns-keep-no-zone.md) [87](087-an-embedded-field-must-be-a-struct-value.md) [134](134-a-type-postgresql-has-a-column-for-maps-to-it-and-reads-back.md) [147](147-a-projection-reads-null-into-a-plain-field-as-zero.md) [160](160-sqlite-time-aggregates-read-text.md) |
| `internal/testdb/schema.go` | [16](016-test-schema-is-hand-written.md) |
| `internal/testdb/testdb.go` | [16](016-test-schema-is-hand-written.md) [26](026-feature-examples-run-on-postgresql-only.md) |
| `internal/track/snapshot.go` | [92](092-tracked-values-compare-as-stored.md) |
| `internal/track/track.go` | [27](027-all-returns-t.md) [28](028-identity-and-relation-slices.md) [30](030-tracking-follows-keys-not-addresses.md) [32](032-navigation-and-foreign-key-changes.md) [79](079-a-tracking-context-serves-one-operation-at-a-time.md) [92](092-tracked-values-compare-as-stored.md) [93](093-tracking-follows-what-a-save-wrote.md) [99](099-a-retried-attempt-leaves-nothing-in-the-tracker.md) [107](107-hot-paths-allocate-for-the-operation-not-for-each-row.md) [108](108-kept-sql-by-shape-unboxed-keys-compact-snapshots-and.md) [121](121-a-rollback-forgets-what-its-transaction-began-tracking.md) [123](123-an-after-hooks-edit-is-a-change-to-save.md) [142](142-chunk-lets-go-of-a-group-that-has-nothing-to-save.md) [166](166-find-reads-the-row-after-a-bulk-delete.md) |
| `internal/track/track_test.go` | [79](079-a-tracking-context-serves-one-operation-at-a-time.md) [92](092-tracked-values-compare-as-stored.md) [121](121-a-rollback-forgets-what-its-transaction-began-tracking.md) |
| `join.go` | [5](005-select-closures-run-client-side.md) [9](009-owner-review-of-step-1.md) [34](034-self-joins.md) [145](145-helpers-take-a-field-of-any-type.md) [169](169-a-join-and-a-projection-count-and-show-their-sql.md) |
| `joined.go` | [136](136-an-outer-joins-side-reads-what-only-the-driver-converts.md) [148](148-a-query-with-computed-fields-stays-tracked.md) [163](163-includejoined-applies-the-relations-own-conditions.md) |
| `json.go` | [42](042-converters-are-scanner-and-valuer.md) [95](095-in-means-eq-and-other-value-rules.md) [109](109-numbers-inside-an-interface-read-as-json-number.md) |
| `json_test.go` | [42](042-converters-are-scanner-and-valuer.md) [95](095-in-means-eq-and-other-value-rules.md) [109](109-numbers-inside-an-interface-read-as-json-number.md) [151](151-deletedat-is-a-time-or-null-in-json.md) |
| `merge.go` | [80](080-every-write-moves-the-version-on.md) [89](089-one-column-one-value.md) [90](090-merge-keeps-what-other-writes-keep.md) [150](150-a-zero-key-nothing-fills-is-refused.md) [166](166-find-reads-the-row-after-a-bulk-delete.md) |
| `metrics/metrics.go` | [176](176-ants-structs-have-camelcase-json-keys.md) |
| `migrate/cli.go` | [11](011-migration-model.md) [55](055-renames.md) [58](058-squashing-needs-no-coordination.md) [59](059-adopting-an-existing-database.md) [116](116-code-files-rerun-after-versioned-migrations.md) [117](117-the-migrate-program-takes-the-applications-options.md) [127](127-commands-refuse-to-guess-which-migration-is-meant.md) [129](129-status-check-fails-a-deploy-gate.md) [144](144-script-stops-before-a-go-migration.md) [172](172-an-enum-value-renames-in-place.md) [174](174-drift-checks-the-views-that-view-models-read.md) |
| `migrate/config.go` | [11](011-migration-model.md) [60](060-one-program-two-jobs.md) [117](117-the-migrate-program-takes-the-applications-options.md) |
| `migrate/engine.go` | [50](050-three-layers-for-migrations.md) [56](056-failure.md) [58](058-squashing-needs-no-coordination.md) [115](115-a-file-outside-a-transaction-is-recorded-before-it-runs.md) [116](116-code-files-rerun-after-versioned-migrations.md) [126](126-a-migration-runs-as-it-says-or-not-at-all.md) [127](127-commands-refuse-to-guess-which-migration-is-meant.md) [162](162-a-failed-migration-names-its-statement.md) |
| `migrate/generate.go` | [55](055-renames.md) [58](058-squashing-needs-no-coordination.md) [59](059-adopting-an-existing-database.md) [82](082-a-rename-must-land-on-a-name-the-models-have.md) [127](127-commands-refuse-to-guess-which-migration-is-meant.md) [128](128-adopt-leaves-out-roles.md) |
| `migrate/history.go` | [115](115-a-file-outside-a-transaction-is-recorded-before-it-runs.md) |
| `migrate/internal_test.go` | [55](055-renames.md) [56](056-failure.md) |
| `migrate/migrate_test.go` | [50](050-three-layers-for-migrations.md) [56](056-failure.md) |
| `migrate/migration.go` | [56](056-failure.md) [58](058-squashing-needs-no-coordination.md) [116](116-code-files-rerun-after-versioned-migrations.md) [126](126-a-migration-runs-as-it-says-or-not-at-all.md) |
| `migrate/operate.go` | [57](057-drift-compares-what-the-server-stored.md) [59](059-adopting-an-existing-database.md) [73](073-drift-compares-what-models-declare.md) [113](113-a-missing-versions-folder-is-an-error.md) [119](119-an-upserts-update-writes-the-zero-a-default-field-holds.md) [127](127-commands-refuse-to-guess-which-migration-is-meant.md) [144](144-script-stops-before-a-go-migration.md) [158](158-drift-compares-two-explicit-serial-defaults.md) [173](173-drift-names-the-types-the-models-use-and-do-not-create.md) [174](174-drift-checks-the-views-that-view-models-read.md) |
| `migrate/registry.go` | [56](056-failure.md) |
| `migrate/source.go` | [11](011-migration-model.md) [60](060-one-program-two-jobs.md) [113](113-a-missing-versions-folder-is-an-error.md) |
| `options.go` | [29](029-sessions.md) [48](048-replicas.md) [61](061-sessions-take-options.md) [69](069-errors-carry-their-sql.md) [120](120-the-root-does-not-track-and-a-session-does.md) [167](167-sqlite-row-locks-in-a-transaction-that-holds-the-write-lock.md) |
| `paging.go` | [81](081-a-set-operations-members-keep-their-own-paging.md) [142](142-chunk-lets-go-of-a-group-that-has-nothing-to-save.md) [143](143-paging-arguments-out-of-range-are-errpaging.md) [171](171-after-pages-a-nullable-column-whose-order-places-its-nulls.md) [176](176-ants-structs-have-camelcase-json-keys.md) |
| `pivot.go` | [44](044-many-to-many.md) [64](064-through-relations-are-read-only-pivots.md) [138](138-a-uint64-column-on-postgresql-is-numeric.md) |
| `projection.go` | [5](005-select-closures-run-client-side.md) [35](035-typed-expressions.md) [136](136-an-outer-joins-side-reads-what-only-the-driver-converts.md) [141](141-a-statement-inside-an-each-loop-on-one-connection-is-refused.md) [147](147-a-projection-reads-null-into-a-plain-field-as-zero.md) [169](169-a-join-and-a-projection-count-and-show-their-sql.md) |
| `qb/aliased_test.go` | [14](014-the-dialect-interface-exposes-an-internal-type.md) [62](062-the-statement-ast-is-public.md) |
| `qb/qb.go` | [14](014-the-dialect-interface-exposes-an-internal-type.md) [39](039-global-filters-are-lazy.md) [62](062-the-statement-ast-is-public.md) [68](068-sensitive-values-are-wrapped-when-bound.md) [88](088-sql-server-recurses-with-union-all-and-a-path-of-keys.md) [107](107-hot-paths-allocate-for-the-operation-not-for-each-row.md) [124](124-a-conflict-target-repeats-a-partial-indexs-predicate.md) |
| `query.go` | [1](001-ant-ordered-replaces-cmp-ordered.md) [3](003-find-takes-a-generic-key.md) [6](006-return-shapes-and-tracking.md) [18](018-wrong-model-detection.md) [20](020-queries-are-immutable.md) [23](023-guarding-bulk-writes.md) [27](027-all-returns-t.md) [28](028-identity-and-relation-slices.md) [33](033-include-shape-functions.md) [36](036-derived-tables-keep-the-models-name.md) [37](037-references-to-outer-queries.md) [38](038-bulk-writes.md) [40](040-soft-deletes-are-a-filter.md) [80](080-every-write-moves-the-version-on.md) [81](081-a-set-operations-members-keep-their-own-paging.md) [98](098-an-explicit-write-refuses-the-columns-ant-keeps.md) [102](102-updatemany-keeps-what-a-bulk-update-keeps.md) [107](107-hot-paths-allocate-for-the-operation-not-for-each-row.md) [112](112-a-locking-read-puts-the-row-into-the-tracked-instance.md) [141](141-a-statement-inside-an-each-loop-on-one-connection-is-refused.md) [145](145-helpers-take-a-field-of-any-type.md) [148](148-a-query-with-computed-fields-stays-tracked.md) [154](154-the-ctx-carries-the-transaction.md) [164](164-explain-reads-each-databases-plan-statement.md) [166](166-find-reads-the-row-after-a-bulk-delete.md) [169](169-a-join-and-a-projection-count-and-show-their-sql.md) |
| `raw.go` | [24](024-raw-sql-arguments.md) [27](027-all-returns-t.md) [87](087-an-embedded-field-must-be-a-struct-value.md) |
| `run.go` | [48](048-replicas.md) [138](138-a-uint64-column-on-postgresql-is-numeric.md) [141](141-a-statement-inside-an-each-loop-on-one-connection-is-refused.md) [154](154-the-ctx-carries-the-transaction.md) [167](167-sqlite-row-locks-in-a-transaction-that-holds-the-write-lock.md) |
| `schema.go` | [21](021-configuration-runs-when-a-context-opens.md) [51](051-modeling-added-for-migrations.md) [54](054-foreign-keys.md) [66](066-several-models-can-share-a-table.md) [94](094-migrations-keep-a-columns-dependents-working-while-it.md) [117](117-the-migrate-program-takes-the-applications-options.md) [125](125-open-refuses-a-model-it-would-misread.md) [131](131-a-foreign-key-column-follows-its-target.md) [132](132-default-names-are-valid-and-distinct.md) [134](134-a-type-postgresql-has-a-column-for-maps-to-it-and-reads-back.md) [138](138-a-uint64-column-on-postgresql-is-numeric.md) [152](152-open-refuses-values-the-driver-cannot-take.md) [174](174-drift-checks-the-views-that-view-models-read.md) |
| `schema/change.go` | [78](078-a-replaced-column-takes-its-dependents-along-and-they.md) [94](094-migrations-keep-a-columns-dependents-working-while-it.md) [130](130-partitioning-is-part-of-the-table.md) [131](131-a-foreign-key-column-follows-its-target.md) [133](133-a-concurrent-build-clears-its-leftover.md) [135](135-a-column-ant-made-for-an-earlier-default-is-converted-as-ant-stored-it.md) [172](172-an-enum-value-renames-in-place.md) |
| `schema/dialect.go` | [50](050-three-layers-for-migrations.md) [57](057-drift-compares-what-the-server-stored.md) [94](094-migrations-keep-a-columns-dependents-working-while-it.md) [173](173-drift-names-the-types-the-models-use-and-do-not-create.md) [174](174-drift-checks-the-views-that-view-models-read.md) |
| `schema/diff.go` | [50](050-three-layers-for-migrations.md) [52](052-new-not-null-columns-fill-existing-rows.md) [55](055-renames.md) [73](073-drift-compares-what-models-declare.md) [78](078-a-replaced-column-takes-its-dependents-along-and-they.md) [82](082-a-rename-must-land-on-a-name-the-models-have.md) [94](094-migrations-keep-a-columns-dependents-working-while-it.md) [114](114-an-identity-columns-default-is-not-ants-to-change.md) [130](130-partitioning-is-part-of-the-table.md) [172](172-an-enum-value-renames-in-place.md) |
| `schema/diff_test.go` | [50](050-three-layers-for-migrations.md) [52](052-new-not-null-columns-fill-existing-rows.md) [53](053-names-follow-postgresqls-defaults.md) [54](054-foreign-keys.md) [55](055-renames.md) [78](078-a-replaced-column-takes-its-dependents-along-and-they.md) [82](082-a-rename-must-land-on-a-name-the-models-have.md) [94](094-migrations-keep-a-columns-dependents-working-while-it.md) [133](133-a-concurrent-build-clears-its-leftover.md) [172](172-an-enum-value-renames-in-place.md) |
| `schema/identity_test.go` | [114](114-an-identity-columns-default-is-not-ants-to-change.md) |
| `schema/naming.go` | [53](053-names-follow-postgresqls-defaults.md) [132](132-default-names-are-valid-and-distinct.md) |
| `schema/naming_test.go` | [132](132-default-names-are-valid-and-distinct.md) |
| `schema/probes_test.go` | [52](052-new-not-null-columns-fill-existing-rows.md) [53](053-names-follow-postgresqls-defaults.md) [78](078-a-replaced-column-takes-its-dependents-along-and-they.md) [94](094-migrations-keep-a-columns-dependents-working-while-it.md) [172](172-an-enum-value-renames-in-place.md) |
| `schema/rename.go` | [55](055-renames.md) [82](082-a-rename-must-land-on-a-name-the-models-have.md) [94](094-migrations-keep-a-columns-dependents-working-while-it.md) [172](172-an-enum-value-renames-in-place.md) |
| `schema/retype_test.go` | [94](094-migrations-keep-a-columns-dependents-working-while-it.md) |
| `schema/schema.go` | [51](051-modeling-added-for-migrations.md) [54](054-foreign-keys.md) [130](130-partitioning-is-part-of-the-table.md) [133](133-a-concurrent-build-clears-its-leftover.md) [174](174-drift-checks-the-views-that-view-models-read.md) |
| `schema/script.go` | [11](011-migration-model.md) [94](094-migrations-keep-a-columns-dependents-working-while-it.md) [131](131-a-foreign-key-column-follows-its-target.md) [132](132-default-names-are-valid-and-distinct.md) [172](172-an-enum-value-renames-in-place.md) |
| `secure/secure.go` | [95](095-in-means-eq-and-other-value-rules.md) [105](105-read-mostly-registries-are-copy-on-write-snapshots.md) [109](109-numbers-inside-an-interface-read-as-json-number.md) |
| `secure/secure_test.go` | [95](095-in-means-eq-and-other-value-rules.md) |
| `sensitive.go` | [68](068-sensitive-values-are-wrapped-when-bound.md) |
| `shape.go` | [108](108-kept-sql-by-shape-unboxed-keys-compact-snapshots-and.md) |
| `shape_internal_test.go` | [108](108-kept-sql-by-shape-unboxed-keys-compact-snapshots-and.md) |
| `skills/ant/SKILL.md` | [104](104-a-skill-for-coding-agents-ships-in-the-module.md) |
| `subquery.go` | [37](037-references-to-outer-queries.md) [81](081-a-set-operations-members-keep-their-own-paging.md) |
| `table.go` | [3](003-find-takes-a-generic-key.md) [6](006-return-shapes-and-tracking.md) [22](022-inserts-run-one-statement-per-row.md) [39](039-global-filters-are-lazy.md) [40](040-soft-deletes-are-a-filter.md) [80](080-every-write-moves-the-version-on.md) [145](145-helpers-take-a-field-of-any-type.md) [154](154-the-ctx-carries-the-transaction.md) [166](166-find-reads-the-row-after-a-bulk-delete.md) |
| `tests/any_column_test.go` | [145](145-helpers-take-a-field-of-any-type.md) [160](160-sqlite-time-aggregates-read-text.md) |
| `tests/api/api_test.go` | [71](071-dependencies-stay-out-of-the-root-package.md) |
| `tests/api/skill_test.go` | [104](104-a-skill-for-coding-agents-ships-in-the-module.md) |
| `tests/benchmarks/budget_test.go` | [107](107-hot-paths-allocate-for-the-operation-not-for-each-row.md) [108](108-kept-sql-by-shape-unboxed-keys-compact-snapshots-and.md) |
| `tests/benchmarks/fake_test.go` | [107](107-hot-paths-allocate-for-the-operation-not-for-each-row.md) |
| `tests/benchmarks/parallel_test.go` | [105](105-read-mostly-registries-are-copy-on-write-snapshots.md) |
| `tests/bound_ctx_test.go` | [154](154-the-ctx-carries-the-transaction.md) [155](155-a-bound-transaction-settles-what-its-contexts-did.md) |
| `tests/bulk_delete_tracking_test.go` | [166](166-find-reads-the-row-after-a-bulk-delete.md) |
| `tests/chunk_tracking_test.go` | [142](142-chunk-lets-go-of-a-group-that-has-nothing-to-save.md) |
| `tests/concurrency/callbacks_test.go` | [46](046-interceptors.md) [47](047-hooks-run-inside-the-write.md) [48](048-replicas.md) [79](079-a-tracking-context-serves-one-operation-at-a-time.md) [105](105-read-mostly-registries-are-copy-on-write-snapshots.md) |
| `tests/concurrency/sessions_test.go` | [29](029-sessions.md) [61](061-sessions-take-options.md) [112](112-a-locking-read-puts-the-row-into-the-tracked-instance.md) |
| `tests/concurrency/tracking_test.go` | [79](079-a-tracking-context-serves-one-operation-at-a-time.md) |
| `tests/concurrency/tx_test.go` | [79](079-a-tracking-context-serves-one-operation-at-a-time.md) [99](099-a-retried-attempt-leaves-nothing-in-the-tracker.md) [106](106-begin-inside-a-transaction-opens-a-savepoint.md) |
| `tests/conformance_test.go` | [16](016-test-schema-is-hand-written.md) [44](044-many-to-many.md) [64](064-through-relations-are-read-only-pivots.md) [81](081-a-set-operations-members-keep-their-own-paging.md) [84](084-every-time-is-bound-in-utc-where-columns-keep-no-zone.md) [85](085-mysql-reads-the-table-it-writes-through-a-derived-table.md) [91](091-an-upsert-checks-a-copys-version-on-mysql-and-mariadb.md) [96](096-a-forced-update-stamps-updatedat.md) [98](098-an-explicit-write-refuses-the-columns-ant-keeps.md) [102](102-updatemany-keeps-what-a-bulk-update-keeps.md) [103](103-an-upserts-update-stamps-updatedat.md) [110](110-a-version-the-application-sets-is-the-one-checked.md) [112](112-a-locking-read-puts-the-row-into-the-tracked-instance.md) [119](119-an-upserts-update-writes-the-zero-a-default-field-holds.md) [140](140-an-update-that-changed-nothing-is-checked-against-the-row.md) [143](143-paging-arguments-out-of-range-are-errpaging.md) |
| `tests/each_stream_test.go` | [141](141-a-statement-inside-an-each-loop-on-one-connection-is-refused.md) |
| `tests/explain_test.go` | [164](164-explain-reads-each-databases-plan-statement.md) |
| `tests/features/README.md` | [26](026-feature-examples-run-on-postgresql-only.md) |
| `tests/features/change_tracking/concurrent_use_test.go` | [79](079-a-tracking-context-serves-one-operation-at-a-time.md) [120](120-the-root-does-not-track-and-a-session-does.md) |
| `tests/features/change_tracking/graph_test.go` | [30](030-tracking-follows-keys-not-addresses.md) |
| `tests/features/change_tracking/graph_tracking_test.go` | [32](032-navigation-and-foreign-key-changes.md) [93](093-tracking-follows-what-a-save-wrote.md) |
| `tests/features/change_tracking/identity_test.go` | [6](006-return-shapes-and-tracking.md) [27](027-all-returns-t.md) [28](028-identity-and-relation-slices.md) |
| `tests/features/change_tracking/probes_test.go` | [92](092-tracked-values-compare-as-stored.md) [93](093-tracking-follows-what-a-save-wrote.md) [108](108-kept-sql-by-shape-unboxed-keys-compact-snapshots-and.md) [111](111-a-replaced-entity-keeps-what-the-body-could-not-give.md) |
| `tests/features/change_tracking/relations_test.go` | [28](028-identity-and-relation-slices.md) [30](030-tracking-follows-keys-not-addresses.md) [32](032-navigation-and-foreign-key-changes.md) [80](080-every-write-moves-the-version-on.md) [93](093-tracking-follows-what-a-save-wrote.md) [111](111-a-replaced-entity-keeps-what-the-body-could-not-give.md) |
| `tests/features/change_tracking/save_changes_test.go` | [31](031-failed-saves.md) [63](063-savechanges-batches-inserts.md) |
| `tests/features/change_tracking/sessions_test.go` | [29](029-sessions.md) [61](061-sessions-take-options.md) [120](120-the-root-does-not-track-and-a-session-does.md) |
| `tests/features/change_tracking/states_test.go` | [93](093-tracking-follows-what-a-save-wrote.md) [96](096-a-forced-update-stamps-updatedat.md) |
| `tests/features/developer_tooling/diagnostics_test.go` | [69](069-errors-carry-their-sql.md) [70](070-dryrun-really-runs.md) |
| `tests/features/developer_tooling/errors_test.go` | [45](045-errors-are-classified-not-wrapped.md) [69](069-errors-carry-their-sql.md) |
| `tests/features/developer_tooling/interceptor_test.go` | [46](046-interceptors.md) |
| `tests/features/developer_tooling/logging_test.go` | [46](046-interceptors.md) |
| `tests/features/developer_tooling/sensitive_test.go` | [68](068-sensitive-values-are-wrapped-when-bound.md) [146](146-ne-matches-null-as-go-does.md) |
| `tests/features/fixture/fixture.go` | [26](026-feature-examples-run-on-postgresql-only.md) |
| `tests/features/keys_and_identity/generated_key_test.go` | [19](019-a-zero-generated-key-means-assign-one.md) |
| `tests/features/keys_and_identity/string_uuid_keys_test.go` | [150](150-a-zero-key-nothing-fills-is-refused.md) |
| `tests/features/keys_and_identity/uuidv7_key_test.go` | [168](168-ids-uuidv7-makes-a-uuid-of-any-16-byte-type.md) |
| `tests/features/lifecycle/concurrency_test.go` | [110](110-a-version-the-application-sets-is-the-one-checked.md) |
| `tests/features/lifecycle/filters_test.go` | [39](039-global-filters-are-lazy.md) |
| `tests/features/lifecycle/hooks_test.go` | [47](047-hooks-run-inside-the-write.md) [123](123-an-after-hooks-edit-is-a-change-to-save.md) |
| `tests/features/lifecycle/soft_delete_test.go` | [40](040-soft-deletes-are-a-filter.md) |
| `tests/features/lifecycle/timestamps_test.go` | [41](041-automatic-timestamps.md) [90](090-merge-keeps-what-other-writes-keep.md) [96](096-a-forced-update-stamps-updatedat.md) [111](111-a-replaced-entity-keeps-what-the-body-could-not-give.md) |
| `tests/features/lifecycle/upsert_version_test.go` | [91](091-an-upsert-checks-a-copys-version-on-mysql-and-mariadb.md) [103](103-an-upserts-update-stamps-updatedat.md) |
| `tests/features/lifecycle/utc_test.go` | [41](041-automatic-timestamps.md) [76](076-one-clock-for-times-without-a-zone.md) [84](084-every-time-is-bound-in-utc-where-columns-keep-no-zone.md) [160](160-sqlite-time-aggregates-read-text.md) |
| `tests/features/lifecycle/validate_test.go` | [47](047-hooks-run-inside-the-write.md) [157](157-insert-validation-leaves-zero-default-fields-to-the-database.md) |
| `tests/features/loading/include_test.go` | [33](033-include-shape-functions.md) |
| `tests/features/loading/shaped_include_test.go` | [33](033-include-shape-functions.md) |
| `tests/features/migrations` | [117](117-the-migrate-program-takes-the-applications-options.md) |
| `tests/features/migrations/adopt_test.go` | [57](057-drift-compares-what-the-server-stored.md) [59](059-adopting-an-existing-database.md) [127](127-commands-refuse-to-guess-which-migration-is-meant.md) [128](128-adopt-leaves-out-roles.md) |
| `tests/features/migrations/diff_test.go` | [11](011-migration-model.md) [114](114-an-identity-columns-default-is-not-ants-to-change.md) [172](172-an-enum-value-renames-in-place.md) |
| `tests/features/migrations/drift_test.go` | [57](057-drift-compares-what-the-server-stored.md) [73](073-drift-compares-what-models-declare.md) [114](114-an-identity-columns-default-is-not-ants-to-change.md) [119](119-an-upserts-update-writes-the-zero-a-default-field-holds.md) [173](173-drift-names-the-types-the-models-use-and-do-not-create.md) [174](174-drift-checks-the-views-that-view-models-read.md) |
| `tests/features/migrations/generated_test.go` | [78](078-a-replaced-column-takes-its-dependents-along-and-they.md) [94](094-migrations-keep-a-columns-dependents-working-while-it.md) |
| `tests/features/migrations/modeling_test.go` | [51](051-modeling-added-for-migrations.md) [72](072-partitions-are-not-migrations.md) [94](094-migrations-keep-a-columns-dependents-working-while-it.md) [130](130-partitioning-is-part-of-the-table.md) [132](132-default-names-are-valid-and-distinct.md) [161](161-a-month-is-read-from-its-bounds.md) |
| `tests/features/migrations/models_test.go` | [51](051-modeling-added-for-migrations.md) |
| `tests/features/migrations/native_types_test.go` | [135](135-a-column-ant-made-for-an-earlier-default-is-converted-as-ant-stored-it.md) |
| `tests/features/migrations/relations_test.go` | [54](054-foreign-keys.md) [131](131-a-foreign-key-column-follows-its-target.md) [174](174-drift-checks-the-views-that-view-models-read.md) |
| `tests/features/migrations/rename_target_test.go` | [55](055-renames.md) [82](082-a-rename-must-land-on-a-name-the-models-have.md) [94](094-migrations-keep-a-columns-dependents-working-while-it.md) |
| `tests/features/migrations/repeatable_test.go` | [11](011-migration-model.md) [116](116-code-files-rerun-after-versioned-migrations.md) |
| `tests/features/migrations/reset_test.go` | [126](126-a-migration-runs-as-it-says-or-not-at-all.md) |
| `tests/features/migrations/script_test.go` | [144](144-script-stops-before-a-go-migration.md) [174](174-drift-checks-the-views-that-view-models-read.md) |
| `tests/features/migrations/sequence_drift_test.go` | [158](158-drift-compares-two-explicit-serial-defaults.md) |
| `tests/features/migrations/source_test.go` | [11](011-migration-model.md) [60](060-one-program-two-jobs.md) [113](113-a-missing-versions-folder-is-an-error.md) |
| `tests/features/migrations/squash_test.go` | [58](058-squashing-needs-no-coordination.md) |
| `tests/features/migrations/team_test.go` | [127](127-commands-refuse-to-guess-which-migration-is-meant.md) |
| `tests/features/migrations/uint64_test.go` | [138](138-a-uint64-column-on-postgresql-is-numeric.md) |
| `tests/features/migrations/up_test.go` | [11](011-migration-model.md) [50](050-three-layers-for-migrations.md) [56](056-failure.md) [115](115-a-file-outside-a-transaction-is-recorded-before-it-runs.md) [126](126-a-migration-runs-as-it-says-or-not-at-all.md) [129](129-status-check-fails-a-deploy-gate.md) [133](133-a-concurrent-build-clears-its-leftover.md) [162](162-a-failed-migration-names-its-statement.md) |
| `tests/features/model_definition/configure_test.go` | [21](021-configuration-runs-when-a-context-opens.md) [66](066-several-models-can-share-a-table.md) |
| `tests/features/model_definition/conflicting_tags_test.go` | [139](139-a-field-can-not-ask-for-two-things-ant-can-not-both-do.md) |
| `tests/features/model_definition/custom_types_test.go` | [42](042-converters-are-scanner-and-valuer.md) |
| `tests/features/model_definition/embedded_test.go` | [87](087-an-embedded-field-must-be-a-struct-value.md) |
| `tests/features/model_definition/field_roles_test.go` | [98](098-an-explicit-write-refuses-the-columns-ant-keeps.md) |
| `tests/features/model_definition/inheritance_test.go` | [66](066-several-models-can-share-a-table.md) [67](067-naming-and-conventions-are-global.md) [117](117-the-migrate-program-takes-the-applications-options.md) |
| `tests/features/model_definition/json_keys_test.go` | [176](176-ants-structs-have-camelcase-json-keys.md) |
| `tests/features/model_definition/nullable_test.go` | [51](051-modeling-added-for-migrations.md) |
| `tests/features/model_definition/refused_test.go` | [125](125-open-refuses-a-model-it-would-misread.md) [132](132-default-names-are-valid-and-distinct.md) |
| `tests/features/model_definition/table_groups_test.go` | [66](066-several-models-can-share-a-table.md) |
| `tests/features/performance/chunk_test.go` | [142](142-chunk-lets-go-of-a-group-that-has-nothing-to-save.md) [143](143-paging-arguments-out-of-range-are-errpaging.md) |
| `tests/features/performance/paging_test.go` | [143](143-paging-arguments-out-of-range-are-errpaging.md) [171](171-after-pages-a-nullable-column-whose-order-places-its-nulls.md) |
| `tests/features/querying/all_test.go` | [27](027-all-returns-t.md) |
| `tests/features/querying/any_column_test.go` | [145](145-helpers-take-a-field-of-any-type.md) |
| `tests/features/querying/cte_test.go` | [36](036-derived-tables-keep-the-models-name.md) |
| `tests/features/querying/derived_joins_test.go` | [36](036-derived-tables-keep-the-models-name.md) |
| `tests/features/querying/each_test.go` | [6](006-return-shapes-and-tracking.md) [27](027-all-returns-t.md) |
| `tests/features/querying/eq_col_test.go` | [18](018-wrong-model-detection.md) |
| `tests/features/querying/eq_test.go` | [7](007-eq-with-a-nil-pointer.md) |
| `tests/features/querying/expressions_test.go` | [35](035-typed-expressions.md) |
| `tests/features/querying/find_test.go` | [3](003-find-takes-a-generic-key.md) |
| `tests/features/querying/in_test.go` | [95](095-in-means-eq-and-other-value-rules.md) |
| `tests/features/querying/is_null_test.go` | [7](007-eq-with-a-nil-pointer.md) [95](095-in-means-eq-and-other-value-rules.md) |
| `tests/features/querying/limit_offset_test.go` | [143](143-paging-arguments-out-of-range-are-errpaging.md) |
| `tests/features/querying/locks_test.go` | [112](112-a-locking-read-puts-the-row-into-the-tracked-instance.md) |
| `tests/features/querying/ne_test.go` | [7](007-eq-with-a-nil-pointer.md) |
| `tests/features/querying/not_in_test.go` | [95](095-in-means-eq-and-other-value-rules.md) |
| `tests/features/querying/order_by_test.go` | [18](018-wrong-model-detection.md) [20](020-queries-are-immutable.md) |
| `tests/features/querying/query_reuse_test.go` | [20](020-queries-are-immutable.md) |
| `tests/features/querying/raw_arguments_test.go` | [24](024-raw-sql-arguments.md) |
| `tests/features/querying/raw_test.go` | [24](024-raw-sql-arguments.md) |
| `tests/features/querying/set_operations_test.go` | [36](036-derived-tables-keep-the-models-name.md) |
| `tests/features/querying/subquery_test.go` | [37](037-references-to-outer-queries.md) |
| `tests/features/relations/advanced_test.go` | [64](064-through-relations-are-read-only-pivots.md) [65](065-scoped-relations-use-the-same-filter-path.md) [156](156-scoped-relation-saves-apply-target-eq-values.md) |
| `tests/features/relations/ambiguous_test.go` | [65](065-scoped-relations-use-the-same-filter-path.md) |
| `tests/features/relations/configuration_test.go` | [125](125-open-refuses-a-model-it-would-misread.md) |
| `tests/features/relations/join_test.go` | [5](005-select-closures-run-client-side.md) [169](169-a-join-and-a-projection-count-and-show-their-sql.md) |
| `tests/features/relations/many_to_many_test.go` | [44](044-many-to-many.md) |
| `tests/features/relations/project_test.go` | [5](005-select-closures-run-client-side.md) [35](035-typed-expressions.md) [147](147-a-projection-reads-null-into-a-plain-field-as-zero.md) |
| `tests/features/relations/self_reference_test.go` | [34](034-self-joins.md) |
| `tests/features/testing/anttest_test.go` | [121](121-a-rollback-forgets-what-its-transaction-began-tracking.md) |
| `tests/features/transactions` | [121](121-a-rollback-forgets-what-its-transaction-began-tracking.md) |
| `tests/features/transactions/replicas_test.go` | [48](048-replicas.md) [61](061-sessions-take-options.md) |
| `tests/features/transactions/retry_policy_test.go` | [99](099-a-retried-attempt-leaves-nothing-in-the-tracker.md) |
| `tests/features/transactions/retry_test.go` | [31](031-failed-saves.md) [99](099-a-retried-attempt-leaves-nothing-in-the-tracker.md) |
| `tests/features/transactions/tx_test.go` | [2](002-tx-is-a-generic-method-on-context.md) [106](106-begin-inside-a-transaction-opens-a-savepoint.md) |
| `tests/features/value_handling/json_test.go` | [42](042-converters-are-scanner-and-valuer.md) [109](109-numbers-inside-an-interface-read-as-json-number.md) |
| `tests/features/value_handling/native_types_test.go` | [134](134-a-type-postgresql-has-a-column-for-maps-to-it-and-reads-back.md) [136](136-an-outer-joins-side-reads-what-only-the-driver-converts.md) |
| `tests/features/value_handling/postgres_test.go` | [43](043-database-specific-helpers.md) [137](137-stored-tsvector-columns-have-a-separate-match-helper.md) |
| `tests/features/value_handling/probes` | [134](134-a-type-postgresql-has-a-column-for-maps-to-it-and-reads-back.md) |
| `tests/features/value_handling/probes/rows/probes_test.go` | [25](025-scanning.md) [119](119-an-upserts-update-writes-the-zero-a-default-field-holds.md) |
| `tests/features/value_handling/uint64_test.go` | [138](138-a-uint64-column-on-postgresql-is-numeric.md) |
| `tests/features/writing` | [80](080-every-write-moves-the-version-on.md) |
| `tests/features/writing/bulk_changes_test.go` | [23](023-guarding-bulk-writes.md) [38](038-bulk-writes.md) [98](098-an-explicit-write-refuses-the-columns-ant-keeps.md) [102](102-updatemany-keeps-what-a-bulk-update-keeps.md) |
| `tests/features/writing/bulk_test.go` | [22](022-inserts-run-one-statement-per-row.md) [63](063-savechanges-batches-inserts.md) [170](170-insertfrom-takes-a-conflict-clause.md) |
| `tests/features/writing/insert_test.go` | [22](022-inserts-run-one-statement-per-row.md) |
| `tests/features/writing/merge_test.go` | [71](071-dependencies-stay-out-of-the-root-package.md) [89](089-one-column-one-value.md) [90](090-merge-keeps-what-other-writes-keep.md) |
| `tests/features/writing/no_condition_guard_test.go` | [23](023-guarding-bulk-writes.md) [38](038-bulk-writes.md) |
| `tests/features/writing/update_test.go` | [89](089-one-column-one-value.md) [102](102-updatemany-keeps-what-a-bulk-update-keeps.md) |
| `tests/features/writing/upsert_test.go` | [47](047-hooks-run-inside-the-write.md) [75](075-generated-values-without-returning.md) [89](089-one-column-one-value.md) [103](103-an-upserts-update-stamps-updatedat.md) |
| `tests/has_one_replace_test.go` | [149](149-a-has-one-pointer-is-the-truth-about-its-row.md) |
| `tests/hook_tx_test.go` | [154](154-the-ctx-carries-the-transaction.md) |
| `tests/insert_from_conflict_test.go` | [170](170-insertfrom-takes-a-conflict-clause.md) |
| `tests/join_terminals_test.go` | [169](169-a-join-and-a-projection-count-and-show-their-sql.md) |
| `tests/joined_relations_test.go` | [163](163-includejoined-applies-the-relations-own-conditions.md) |
| `tests/keyset_nulls_test.go` | [171](171-after-pages-a-nullable-column-whose-order-places-its-nulls.md) |
| `tests/matrix` | [86](086-combinations-are-tested-against-an-oracle.md) |
| `tests/matrix/dims_test.go` | [146](146-ne-matches-null-as-go-does.md) |
| `tests/matrix/matrix_test.go` | [38](038-bulk-writes.md) [49](049-postgresql-first.md) [74](074-dialects-state-their-differences.md) [75](075-generated-values-without-returning.md) [86](086-combinations-are-tested-against-an-oracle.md) |
| `tests/matrix/sql_test.go` | [86](086-combinations-are-tested-against-an-oracle.md) |
| `tests/matrix/testdata/sqlserver.sql` | [88](088-sql-server-recurses-with-union-all-and-a-path-of-keys.md) |
| `tests/nested_order_test.go` | [165](165-sql-server-orders-only-where-it-counts.md) |
| `tests/null_equality_test.go` | [146](146-ne-matches-null-as-go-does.md) |
| `tests/partial_conflict_test.go` | [124](124-a-conflict-target-repeats-a-partial-indexs-predicate.md) |
| `tests/plain_values_test.go` | [152](152-open-refuses-values-the-driver-cannot-take.md) |
| `tests/projection_null_test.go` | [147](147-a-projection-reads-null-into-a-plain-field-as-zero.md) |
| `tests/readback_test.go` | [122](122-what-the-database-fills-is-read-back-by-key.md) |
| `tests/sqlite_locks_test.go` | [167](167-sqlite-row-locks-in-a-transaction-that-holds-the-write-lock.md) |
| `tests/tracking_test.go` | [30](030-tracking-follows-keys-not-addresses.md) |
| `tests/tx_test.go` | [2](002-tx-is-a-generic-method-on-context.md) [31](031-failed-saves.md) [106](106-begin-inside-a-transaction-opens-a-savepoint.md) |
| `tests/wide_keys_test.go` | [138](138-a-uint64-column-on-postgresql-is-numeric.md) |
| `tests/with_tracking_test.go` | [148](148-a-query-with-computed-fields-stays-tracked.md) |
| `tests/zero_key_test.go` | [150](150-a-zero-key-nothing-fills-is-refused.md) |
| `tools/antfmt/format.go` | [77](077-layout-is-enforced-by-tools.md) |
| `tools/antfmt/format_test.go` | [77](077-layout-is-enforced-by-tools.md) |
| `tools/antfmt/main.go` | [77](077-layout-is-enforced-by-tools.md) |
| `tools/antvet` | [71](071-dependencies-stay-out-of-the-root-package.md) |
| `tools/antvet/analyzer` | [4](004-conditions-are-not-tied-to-a-model.md) |
| `tools/antvet/analyzer/analyzer.go` | [83](083-antvet-judges-only-what-it-can-see.md) [175](175-antvet-follows-a-query-built-up-in-a-variable.md) |
| `tools/antvet/analyzer/analyzer_test.go` | [4](004-conditions-are-not-tied-to-a-model.md) [83](083-antvet-judges-only-what-it-can-see.md) |
| `tools/antvet/analyzer/sessions.go` | [153](153-changes-saved-through-another-context-are-reported-by-antvet.md) |
| `tools/antvet/analyzer/testdata` | [83](083-antvet-judges-only-what-it-can-see.md) |
| `tools/antvet/analyzer/testdata/src/example/sessions.go` | [153](153-changes-saved-through-another-context-are-reported-by-antvet.md) |
| `tools/antvet/analyzer/testdata/src/example/variables.go` | [175](175-antvet-follows-a-query-built-up-in-a-variable.md) |
| `tools/antvet/cmd` | [83](083-antvet-judges-only-what-it-can-see.md) |
| `tools/compare` | [71](071-dependencies-stay-out-of-the-root-package.md) |
| `tx.go` | [2](002-tx-is-a-generic-method-on-context.md) [31](031-failed-saves.md) [47](047-hooks-run-inside-the-write.md) [48](048-replicas.md) [61](061-sessions-take-options.md) [70](070-dryrun-really-runs.md) [79](079-a-tracking-context-serves-one-operation-at-a-time.md) [99](099-a-retried-attempt-leaves-nothing-in-the-tracker.md) [106](106-begin-inside-a-transaction-opens-a-savepoint.md) [121](121-a-rollback-forgets-what-its-transaction-began-tracking.md) [141](141-a-statement-inside-an-each-loop-on-one-connection-is-refused.md) [154](154-the-ctx-carries-the-transaction.md) [155](155-a-bound-transaction-settles-what-its-contexts-did.md) [167](167-sqlite-row-locks-in-a-transaction-that-holds-the-write-lock.md) |
| `unit.go` | [19](019-a-zero-generated-key-means-assign-one.md) [22](022-inserts-run-one-statement-per-row.md) [28](028-identity-and-relation-slices.md) [30](030-tracking-follows-keys-not-addresses.md) [31](031-failed-saves.md) [32](032-navigation-and-foreign-key-changes.md) [41](041-automatic-timestamps.md) [47](047-hooks-run-inside-the-write.md) [63](063-savechanges-batches-inserts.md) [75](075-generated-values-without-returning.md) [79](079-a-tracking-context-serves-one-operation-at-a-time.md) [93](093-tracking-follows-what-a-save-wrote.md) [96](096-a-forced-update-stamps-updatedat.md) [99](099-a-retried-attempt-leaves-nothing-in-the-tracker.md) [108](108-kept-sql-by-shape-unboxed-keys-compact-snapshots-and.md) [110](110-a-version-the-application-sets-is-the-one-checked.md) [111](111-a-replaced-entity-keeps-what-the-body-could-not-give.md) [112](112-a-locking-read-puts-the-row-into-the-tracked-instance.md) [120](120-the-root-does-not-track-and-a-session-does.md) [122](122-what-the-database-fills-is-read-back-by-key.md) [123](123-an-after-hooks-edit-is-a-change-to-save.md) [134](134-a-type-postgresql-has-a-column-for-maps-to-it-and-reads-back.md) [140](140-an-update-that-changed-nothing-is-checked-against-the-row.md) [149](149-a-has-one-pointer-is-the-truth-about-its-row.md) [150](150-a-zero-key-nothing-fills-is-refused.md) [154](154-the-ctx-carries-the-transaction.md) [155](155-a-bound-transaction-settles-what-its-contexts-did.md) [156](156-scoped-relation-saves-apply-target-eq-values.md) |
| `upsert.go` | [22](022-inserts-run-one-statement-per-row.md) [38](038-bulk-writes.md) [47](047-hooks-run-inside-the-write.md) [63](063-savechanges-batches-inserts.md) [75](075-generated-values-without-returning.md) [79](079-a-tracking-context-serves-one-operation-at-a-time.md) [80](080-every-write-moves-the-version-on.md) [89](089-one-column-one-value.md) [91](091-an-upsert-checks-a-copys-version-on-mysql-and-mariadb.md) [98](098-an-explicit-write-refuses-the-columns-ant-keeps.md) [102](102-updatemany-keeps-what-a-bulk-update-keeps.md) [103](103-an-upserts-update-stamps-updatedat.md) [119](119-an-upserts-update-writes-the-zero-a-default-field-holds.md) [123](123-an-after-hooks-edit-is-a-change-to-save.md) [124](124-a-conflict-target-repeats-a-partial-indexs-predicate.md) [150](150-a-zero-key-nothing-fills-is-refused.md) [154](154-the-ctx-carries-the-transaction.md) [170](170-insertfrom-takes-a-conflict-clause.md) |

### By tag

Tags on two or more decisions. Search the headers for the rest: `rg -l '^tags: .*word' docs/decisions`.

- `_txlock`: [159](159-a-sqlite-dsn-names-its-lock-behavior.md) [167](167-sqlite-row-locks-in-a-transaction-that-holds-the-write-lock.md)
- `adopt`: [59](059-adopting-an-existing-database.md) [108](108-kept-sql-by-shape-unboxed-keys-compact-snapshots-and.md) [128](128-adopt-leaves-out-roles.md) [173](173-drift-names-the-types-the-models-use-and-do-not-create.md)
- `After`: [81](081-a-set-operations-members-keep-their-own-paging.md) [143](143-paging-arguments-out-of-range-are-errpaging.md) [171](171-after-pages-a-nullable-column-whose-order-places-its-nulls.md)
- `AfterInsert`: [47](047-hooks-run-inside-the-write.md) [123](123-an-after-hooks-edit-is-a-change-to-save.md)
- `aggregate`: [35](035-typed-expressions.md) [147](147-a-projection-reads-null-into-a-plain-field-as-zero.md)
- `All`: [6](006-return-shapes-and-tracking.md) [27](027-all-returns-t.md)
- `analyzer`: [83](083-antvet-judges-only-what-it-can-see.md) [175](175-antvet-follows-a-query-built-up-in-a-variable.md)
- `antvet`: [4](004-conditions-are-not-tied-to-a-model.md) [71](071-dependencies-stay-out-of-the-root-package.md) [83](083-antvet-judges-only-what-it-can-see.md) [153](153-changes-saved-through-another-context-are-reported-by-antvet.md) [175](175-antvet-follows-a-query-built-up-in-a-variable.md)
- `arrays`: [134](134-a-type-postgresql-has-a-column-for-maps-to-it-and-reads-back.md) [136](136-an-outer-joins-side-reads-what-only-the-driver-converts.md)
- `Attach`: [110](110-a-version-the-application-sets-is-the-one-checked.md) [153](153-changes-saved-through-another-context-are-reported-by-antvet.md)
- `Attempts`: [99](099-a-retried-attempt-leaves-nothing-in-the-tracker.md) [121](121-a-rollback-forgets-what-its-transaction-began-tracking.md)
- `Base`: [25](025-scanning.md) [176](176-ants-structs-have-camelcase-json-keys.md)
- `baseline`: [58](058-squashing-needs-no-coordination.md) [59](059-adopting-an-existing-database.md) [127](127-commands-refuse-to-guess-which-migration-is-meant.md) [128](128-adopt-leaves-out-roles.md)
- `Batch`: [22](022-inserts-run-one-statement-per-row.md) [71](071-dependencies-stay-out-of-the-root-package.md)
- `Begin`: [106](106-begin-inside-a-transaction-opens-a-savepoint.md) [141](141-a-statement-inside-an-each-loop-on-one-connection-is-refused.md)
- `bigint`: [135](135-a-column-ant-made-for-an-earlier-default-is-converted-as-ant-stored-it.md) [138](138-a-uint64-column-on-postgresql-is-numeric.md)
- `bulk-write`: [23](023-guarding-bulk-writes.md) [38](038-bulk-writes.md)
- `change-tracking`: [92](092-tracked-values-compare-as-stored.md) [93](093-tracking-follows-what-a-save-wrote.md)
- `check`: [127](127-commands-refuse-to-guess-which-migration-is-meant.md) [130](130-partitioning-is-part-of-the-table.md)
- `CheckDSN`: [118](118-a-sqlite-dsn-says-whether-foreign-keys-are-on.md) [159](159-a-sqlite-dsn-names-its-lock-behavior.md)
- `Chunk`: [81](081-a-set-operations-members-keep-their-own-paging.md) [142](142-chunk-lets-go-of-a-group-that-has-nothing-to-save.md) [143](143-paging-arguments-out-of-range-are-errpaging.md)
- `ChunkByKey`: [81](081-a-set-operations-members-keep-their-own-paging.md) [142](142-chunk-lets-go-of-a-group-that-has-nothing-to-save.md) [143](143-paging-arguments-out-of-range-are-errpaging.md)
- `closure`: [5](005-select-closures-run-client-side.md) [9](009-owner-review-of-step-1.md)
- `ColumnType`: [134](134-a-type-postgresql-has-a-column-for-maps-to-it-and-reads-back.md) [138](138-a-uint64-column-on-postgresql-is-numeric.md)
- `computed`: [98](098-an-explicit-write-refuses-the-columns-ant-keeps.md) [139](139-a-field-can-not-ask-for-two-things-ant-can-not-both-do.md) [148](148-a-query-with-computed-fields-stays-tracked.md)
- `ConcurrencyError`: [80](080-every-write-moves-the-version-on.md) [110](110-a-version-the-application-sets-is-the-one-checked.md) [112](112-a-locking-read-puts-the-row-into-the-tracked-instance.md) [140](140-an-update-that-changed-nothing-is-checked-against-the-row.md)
- `CONCURRENTLY`: [56](056-failure.md) [78](078-a-replaced-column-takes-its-dependents-along-and-they.md)
- `Configure`: [21](021-configuration-runs-when-a-context-opens.md) [66](066-several-models-can-share-a-table.md) [83](083-antvet-judges-only-what-it-can-see.md) [107](107-hot-paths-allocate-for-the-operation-not-for-each-row.md)
- `CTE`: [36](036-derived-tables-keep-the-models-name.md) [88](088-sql-server-recurses-with-union-all-and-a-path-of-keys.md) [165](165-sql-server-orders-only-where-it-counts.md)
- `decimal`: [134](134-a-type-postgresql-has-a-column-for-maps-to-it-and-reads-back.md) [145](145-helpers-take-a-field-of-any-type.md)
- `default`: [51](051-modeling-added-for-migrations.md) [52](052-new-not-null-columns-fill-existing-rows.md) [119](119-an-upserts-update-writes-the-zero-a-default-field-holds.md) [150](150-a-zero-key-nothing-fills-is-refused.md) [157](157-insert-validation-leaves-zero-default-fields-to-the-database.md)
- `Delete`: [23](023-guarding-bulk-writes.md) [38](038-bulk-writes.md) [166](166-find-reads-the-row-after-a-bulk-delete.md)
- `DeletedAt`: [40](040-soft-deletes-are-a-filter.md) [84](084-every-time-is-bound-in-utc-where-columns-keep-no-zone.md) [139](139-a-field-can-not-ask-for-two-things-ant-can-not-both-do.md) [151](151-deletedat-is-a-time-or-null-in-json.md)
- `derived-table`: [36](036-derived-tables-keep-the-models-name.md) [81](081-a-set-operations-members-keep-their-own-paging.md) [85](085-mysql-reads-the-table-it-writes-through-a-derived-table.md)
- `Dialect`: [12](012-additions-to-the-dialect-contract.md) [14](014-the-dialect-interface-exposes-an-internal-type.md) [49](049-postgresql-first.md) [62](062-the-statement-ast-is-public.md)
- `dialect`: [164](164-explain-reads-each-databases-plan-statement.md) [167](167-sqlite-row-locks-in-a-transaction-that-holds-the-write-lock.md)
- `diff`: [11](011-migration-model.md) [82](082-a-rename-must-land-on-a-name-the-models-have.md) [114](114-an-identity-columns-default-is-not-ants-to-change.md) [127](127-commands-refuse-to-guess-which-migration-is-meant.md) [130](130-partitioning-is-part-of-the-table.md) [172](172-an-enum-value-renames-in-place.md)
- `Discriminator`: [66](066-several-models-can-share-a-table.md) [90](090-merge-keeps-what-other-writes-keep.md)
- `DoUpdate`: [80](080-every-write-moves-the-version-on.md) [89](089-one-column-one-value.md) [98](098-an-explicit-write-refuses-the-columns-ant-keeps.md) [103](103-an-upserts-update-stamps-updatedat.md) [170](170-insertfrom-takes-a-conflict-clause.md)
- `drift`: [55](055-renames.md) [57](057-drift-compares-what-the-server-stored.md) [59](059-adopting-an-existing-database.md) [72](072-partitions-are-not-migrations.md) [73](073-drift-compares-what-models-declare.md) [114](114-an-identity-columns-default-is-not-ants-to-change.md) [119](119-an-upserts-update-writes-the-zero-a-default-field-holds.md) [130](130-partitioning-is-part-of-the-table.md) [133](133-a-concurrent-build-clears-its-leftover.md) [158](158-drift-compares-two-explicit-serial-defaults.md) [173](173-drift-names-the-types-the-models-use-and-do-not-create.md) [174](174-drift-checks-the-views-that-view-models-read.md)
- `dsn`: [118](118-a-sqlite-dsn-says-whether-foreign-keys-are-on.md) [159](159-a-sqlite-dsn-names-its-lock-behavior.md)
- `Each`: [6](006-return-shapes-and-tracking.md) [27](027-all-returns-t.md) [141](141-a-statement-inside-an-each-loop-on-one-connection-is-refused.md)
- `embed`: [60](060-one-program-two-jobs.md) [113](113-a-missing-versions-folder-is-an-error.md)
- `embedded`: [87](087-an-embedded-field-must-be-a-struct-value.md) [125](125-open-refuses-a-model-it-would-misread.md) [139](139-a-field-can-not-ask-for-two-things-ant-can-not-both-do.md)
- `enum`: [73](073-drift-compares-what-models-declare.md) [94](094-migrations-keep-a-columns-dependents-working-while-it.md) [172](172-an-enum-value-renames-in-place.md)
- `Eq`: [7](007-eq-with-a-nil-pointer.md) [95](095-in-means-eq-and-other-value-rules.md) [146](146-ne-matches-null-as-go-does.md) [156](156-scoped-relation-saves-apply-target-eq-values.md)
- `ErrBadSchema`: [66](066-several-models-can-share-a-table.md) [125](125-open-refuses-a-model-it-would-misread.md) [132](132-default-names-are-valid-and-distinct.md) [139](139-a-field-can-not-ask-for-two-things-ant-can-not-both-do.md)
- `ErrChecksum`: [127](127-commands-refuse-to-guess-which-migration-is-meant.md) [129](129-status-check-fails-a-deploy-gate.md)
- `ErrConcurrency`: [31](031-failed-saves.md) [91](091-an-upsert-checks-a-copys-version-on-mysql-and-mariadb.md) [110](110-a-version-the-application-sets-is-the-one-checked.md)
- `ErrCursor`: [81](081-a-set-operations-members-keep-their-own-paging.md) [143](143-paging-arguments-out-of-range-are-errpaging.md) [171](171-after-pages-a-nullable-column-whose-order-places-its-nulls.md)
- `ErrNoColumnType`: [134](134-a-type-postgresql-has-a-column-for-maps-to-it-and-reads-back.md) [152](152-open-refuses-values-the-driver-cannot-take.md)
- `ErrTxDone`: [31](031-failed-saves.md) [155](155-a-bound-transaction-settles-what-its-contexts-did.md)
- `ErrUnsupported`: [50](050-three-layers-for-migrations.md) [74](074-dialects-state-their-differences.md)
- `ErrWrongModel`: [4](004-conditions-are-not-tied-to-a-model.md) [18](018-wrong-model-detection.md)
- `Explain`: [162](162-a-failed-migration-names-its-statement.md) [164](164-explain-reads-each-databases-plan-statement.md)
- `Expr`: [35](035-typed-expressions.md) [146](146-ne-matches-null-as-go-does.md)
- `extension`: [73](073-drift-compares-what-models-declare.md) [173](173-drift-names-the-types-the-models-use-and-do-not-create.md)
- `Fields`: [21](021-configuration-runs-when-a-context-opens.md) [100](100-probes-are-named-after-their-model-in-the-plural.md)
- `Find`: [3](003-find-takes-a-generic-key.md) [6](006-return-shapes-and-tracking.md) [28](028-identity-and-relation-slices.md) [39](039-global-filters-are-lazy.md) [83](083-antvet-judges-only-what-it-can-see.md) [166](166-find-reads-the-row-after-a-bulk-delete.md)
- `fixture`: [16](016-test-schema-is-hand-written.md) [26](026-feature-examples-run-on-postgresql-only.md)
- `ForceDelete`: [40](040-soft-deletes-are-a-filter.md) [166](166-find-reads-the-row-after-a-bulk-delete.md)
- `foreign-key`: [30](030-tracking-follows-keys-not-addresses.md) [32](032-navigation-and-foreign-key-changes.md) [54](054-foreign-keys.md) [78](078-a-replaced-column-takes-its-dependents-along-and-they.md) [94](094-migrations-keep-a-columns-dependents-working-while-it.md) [111](111-a-replaced-entity-keeps-what-the-body-could-not-give.md)
- `foreign-keys`: [118](118-a-sqlite-dsn-says-whether-foreign-keys-are-on.md) [131](131-a-foreign-key-column-follows-its-target.md)
- `ForShare`: [112](112-a-locking-read-puts-the-row-into-the-tracked-instance.md) [167](167-sqlite-row-locks-in-a-transaction-that-holds-the-write-lock.md)
- `ForUpdate`: [112](112-a-locking-read-puts-the-row-into-the-tracked-instance.md) [167](167-sqlite-row-locks-in-a-transaction-that-holds-the-write-lock.md)
- `FromCTE`: [36](036-derived-tables-keep-the-models-name.md) [38](038-bulk-writes.md)
- `generated-column`: [78](078-a-replaced-column-takes-its-dependents-along-and-they.md) [94](094-migrations-keep-a-columns-dependents-working-while-it.md)
- `generated-key`: [19](019-a-zero-generated-key-means-assign-one.md) [22](022-inserts-run-one-statement-per-row.md) [90](090-merge-keeps-what-other-writes-keep.md)
- `GenerateKey`: [150](150-a-zero-key-nothing-fills-is-refused.md) [168](168-ids-uuidv7-makes-a-uuid-of-any-16-byte-type.md)
- `global-filter`: [39](039-global-filters-are-lazy.md) [102](102-updatemany-keeps-what-a-bulk-update-keeps.md)
- `go-vet`: [4](004-conditions-are-not-tied-to-a-model.md) [83](083-antvet-judges-only-what-it-can-see.md)
- `Gt`: [1](001-ant-ordered-replaces-cmp-ordered.md) [145](145-helpers-take-a-field-of-any-type.md)
- `history`: [8](008-step-1-file-count.md) [17](017-implemented-ahead-of-the-skeleton.md) [56](056-failure.md) [115](115-a-file-outside-a-transaction-is-recorded-before-it-runs.md)
- `hooks`: [47](047-hooks-run-inside-the-write.md) [63](063-savechanges-batches-inserts.md) [79](079-a-tracking-context-serves-one-operation-at-a-time.md) [123](123-an-after-hooks-edit-is-a-change-to-save.md) [154](154-the-ctx-carries-the-transaction.md)
- `identity`: [94](094-migrations-keep-a-columns-dependents-working-while-it.md) [114](114-an-identity-columns-default-is-not-ants-to-change.md) [138](138-a-uint64-column-on-postgresql-is-numeric.md) [158](158-drift-compares-two-explicit-serial-defaults.md)
- `identity-map`: [27](027-all-returns-t.md) [28](028-identity-and-relation-slices.md) [39](039-global-filters-are-lazy.md) [107](107-hot-paths-allocate-for-the-operation-not-for-each-row.md) [108](108-kept-sql-by-shape-unboxed-keys-compact-snapshots-and.md) [112](112-a-locking-read-puts-the-row-into-the-tracked-instance.md) [148](148-a-query-with-computed-fields-stays-tracked.md)
- `immutable`: [98](098-an-explicit-write-refuses-the-columns-ant-keeps.md) [139](139-a-field-can-not-ask-for-two-things-ant-can-not-both-do.md)
- `In`: [24](024-raw-sql-arguments.md) [95](095-in-means-eq-and-other-value-rules.md) [146](146-ne-matches-null-as-go-does.md)
- `Include`: [33](033-include-shape-functions.md) [44](044-many-to-many.md) [65](065-scoped-relations-use-the-same-filter-path.md) [93](093-tracking-follows-what-a-save-wrote.md) [108](108-kept-sql-by-shape-unboxed-keys-compact-snapshots-and.md)
- `IncludeJoined`: [136](136-an-outer-joins-side-reads-what-only-the-driver-converts.md) [163](163-includejoined-applies-the-relations-own-conditions.md) [164](164-explain-reads-each-databases-plan-statement.md)
- `inet`: [134](134-a-type-postgresql-has-a-column-for-maps-to-it-and-reads-back.md) [135](135-a-column-ant-made-for-an-earlier-default-is-converted-as-ant-stored-it.md)
- `Insert`: [22](022-inserts-run-one-statement-per-row.md) [157](157-insert-validation-leaves-zero-default-fields-to-the-database.md)
- `InsertFrom`: [89](089-one-column-one-value.md) [170](170-insertfrom-takes-a-conflict-clause.md)
- `InsertMany`: [22](022-inserts-run-one-statement-per-row.md) [38](038-bulk-writes.md) [103](103-an-upserts-update-stamps-updatedat.md) [119](119-an-upserts-update-writes-the-zero-a-default-field-holds.md) [124](124-a-conflict-target-repeats-a-partial-indexs-predicate.md)
- `Interceptor`: [46](046-interceptors.md) [97](097-spans-name-the-database-the-dialect-talks-to.md)
- `interval`: [134](134-a-type-postgresql-has-a-column-for-maps-to-it-and-reads-back.md) [135](135-a-column-ant-made-for-an-earlier-default-is-converted-as-ant-stored-it.md)
- `InTx`: [154](154-the-ctx-carries-the-transaction.md) [155](155-a-bound-transaction-settles-what-its-contexts-did.md)
- `JSON`: [42](042-converters-are-scanner-and-valuer.md) [109](109-numbers-inside-an-interface-read-as-json-number.md) [151](151-deletedat-is-a-time-or-null-in-json.md)
- `jsonb`: [134](134-a-type-postgresql-has-a-column-for-maps-to-it-and-reads-back.md) [135](135-a-column-ant-made-for-an-earlier-default-is-converted-as-ant-stored-it.md)
- `keys`: [138](138-a-uint64-column-on-postgresql-is-numeric.md) [168](168-ids-uuidv7-makes-a-uuid-of-any-16-byte-type.md)
- `LastInsertId`: [75](075-generated-values-without-returning.md) [122](122-what-the-database-fills-is-read-back-by-key.md)
- `Latest`: [65](065-scoped-relations-use-the-same-filter-path.md) [163](163-includejoined-applies-the-relations-own-conditions.md)
- `LeftJoin`: [136](136-an-outer-joins-side-reads-what-only-the-driver-converts.md) [147](147-a-projection-reads-null-into-a-plain-field-as-zero.md)
- `LogValues`: [46](046-interceptors.md) [68](068-sensitive-values-are-wrapped-when-bound.md)
- `many-to-many`: [33](033-include-shape-functions.md) [64](064-through-relations-are-read-only-pivots.md) [93](093-tracking-follows-what-a-save-wrote.md)
- `ManyToMany`: [44](044-many-to-many.md) [125](125-open-refuses-a-model-it-would-misread.md) [138](138-a-uint64-column-on-postgresql-is-numeric.md)
- `mariadb`: [13](013-mariadb-shares-the-mysql-package.md) [75](075-generated-values-without-returning.md) [91](091-an-upsert-checks-a-copys-version-on-mysql-and-mariadb.md) [164](164-explain-reads-each-databases-plan-statement.md)
- `MaxOf`: [145](145-helpers-take-a-field-of-any-type.md) [160](160-sqlite-time-aggregates-read-text.md)
- `Merge`: [80](080-every-write-moves-the-version-on.md) [89](089-one-column-one-value.md) [90](090-merge-keeps-what-other-writes-keep.md) [166](166-find-reads-the-row-after-a-bulk-delete.md)
- `migrate`: [11](011-migration-model.md) [50](050-three-layers-for-migrations.md) [60](060-one-program-two-jobs.md) [113](113-a-missing-versions-folder-is-an-error.md)
- `migrate-up`: [115](115-a-file-outside-a-transaction-is-recorded-before-it-runs.md) [116](116-code-files-rerun-after-versioned-migrations.md)
- `migrations`: [49](049-postgresql-first.md) [58](058-squashing-needs-no-coordination.md) [82](082-a-rename-must-land-on-a-name-the-models-have.md) [94](094-migrations-keep-a-columns-dependents-working-while-it.md) [172](172-an-enum-value-renames-in-place.md) [173](173-drift-names-the-types-the-models-use-and-do-not-create.md) [174](174-drift-checks-the-views-that-view-models-read.md)
- `MonthlyPartitions`: [72](072-partitions-are-not-migrations.md) [161](161-a-month-is-read-from-its-bounds.md)
- `Morph`: [65](065-scoped-relations-use-the-same-filter-path.md) [163](163-includejoined-applies-the-relations-own-conditions.md)
- `mysql`: [13](013-mariadb-shares-the-mysql-package.md) [75](075-generated-values-without-returning.md) [85](085-mysql-reads-the-table-it-writes-through-a-derived-table.md) [91](091-an-upsert-checks-a-copys-version-on-mysql-and-mariadb.md) [122](122-what-the-database-fills-is-read-back-by-key.md) [164](164-explain-reads-each-databases-plan-statement.md) [170](170-insertfrom-takes-a-conflict-clause.md)
- `naming`: [53](053-names-follow-postgresqls-defaults.md) [67](067-naming-and-conventions-are-global.md) [100](100-probes-are-named-after-their-model-in-the-plural.md) [117](117-the-migrate-program-takes-the-applications-options.md) [132](132-default-names-are-valid-and-distinct.md)
- `Ne`: [7](007-eq-with-a-nil-pointer.md) [146](146-ne-matches-null-as-go-does.md)
- `netip`: [125](125-open-refuses-a-model-it-would-misread.md) [134](134-a-type-postgresql-has-a-column-for-maps-to-it-and-reads-back.md) [136](136-an-outer-joins-side-reads-what-only-the-driver-converts.md)
- `nextval`: [114](114-an-identity-columns-default-is-not-ants-to-change.md) [158](158-drift-compares-two-explicit-serial-defaults.md)
- `no-transaction`: [115](115-a-file-outside-a-transaction-is-recorded-before-it-runs.md) [126](126-a-migration-runs-as-it-says-or-not-at-all.md) [144](144-script-stops-before-a-go-migration.md)
- `NotIn`: [95](095-in-means-eq-and-other-value-rules.md) [146](146-ne-matches-null-as-go-does.md)
- `NULL`: [147](147-a-projection-reads-null-into-a-plain-field-as-zero.md) [171](171-after-pages-a-nullable-column-whose-order-places-its-nulls.md)
- `null`: [30](030-tracking-follows-keys-not-addresses.md) [95](095-in-means-eq-and-other-value-rules.md) [146](146-ne-matches-null-as-go-does.md)
- `nullable`: [51](051-modeling-added-for-migrations.md) [145](145-helpers-take-a-field-of-any-type.md) [146](146-ne-matches-null-as-go-does.md)
- `NullsFirst`: [94](094-migrations-keep-a-columns-dependents-working-while-it.md) [171](171-after-pages-a-nullable-column-whose-order-places-its-nulls.md)
- `NullsLast`: [94](094-migrations-keep-a-columns-dependents-working-while-it.md) [171](171-after-pages-a-nullable-column-whose-order-places-its-nulls.md)
- `numeric`: [134](134-a-type-postgresql-has-a-column-for-maps-to-it-and-reads-back.md) [138](138-a-uint64-column-on-postgresql-is-numeric.md)
- `Offset`: [143](143-paging-arguments-out-of-range-are-errpaging.md) [169](169-a-join-and-a-projection-count-and-show-their-sql.md)
- `Oldest`: [65](065-scoped-relations-use-the-same-filter-path.md) [163](163-includejoined-applies-the-relations-own-conditions.md)
- `on-conflict`: [44](044-many-to-many.md) [103](103-an-upserts-update-stamps-updatedat.md)
- `on-duplicate-key`: [75](075-generated-values-without-returning.md) [85](085-mysql-reads-the-table-it-writes-through-a-derived-table.md) [91](091-an-upsert-checks-a-copys-version-on-mysql-and-mariadb.md)
- `OnConflict`: [124](124-a-conflict-target-repeats-a-partial-indexs-predicate.md) [170](170-insertfrom-takes-a-conflict-clause.md)
- `Open`: [12](012-additions-to-the-dialect-contract.md) [21](021-configuration-runs-when-a-context-opens.md) [66](066-several-models-can-share-a-table.md) [67](067-naming-and-conventions-are-global.md) [125](125-open-refuses-a-model-it-would-misread.md) [139](139-a-field-can-not-ask-for-two-things-ant-can-not-both-do.md) [152](152-open-refuses-values-the-driver-cannot-take.md)
- `optimistic-concurrency`: [80](080-every-write-moves-the-version-on.md) [91](091-an-upsert-checks-a-copys-version-on-mysql-and-mariadb.md) [110](110-a-version-the-application-sets-is-the-one-checked.md)
- `OrderBy`: [1](001-ant-ordered-replaces-cmp-ordered.md) [20](020-queries-are-immutable.md) [145](145-helpers-take-a-field-of-any-type.md)
- `Ordered`: [1](001-ant-ordered-replaces-cmp-ordered.md) [145](145-helpers-take-a-field-of-any-type.md)
- `orphan`: [30](030-tracking-follows-keys-not-addresses.md) [149](149-a-has-one-pointer-is-the-truth-about-its-row.md)
- `otel`: [46](046-interceptors.md) [97](097-spans-name-the-database-the-dialect-talks-to.md)
- `Page`: [81](081-a-set-operations-members-keep-their-own-paging.md) [143](143-paging-arguments-out-of-range-are-errpaging.md) [176](176-ants-structs-have-camelcase-json-keys.md)
- `paging`: [165](165-sql-server-orders-only-where-it-counts.md) [171](171-after-pages-a-nullable-column-whose-order-places-its-nulls.md)
- `partitions`: [72](072-partitions-are-not-migrations.md) [130](130-partitioning-is-part-of-the-table.md) [161](161-a-month-is-read-from-its-bounds.md)
- `pg_dump`: [59](059-adopting-an-existing-database.md) [128](128-adopt-leaves-out-roles.md)
- `pivot`: [44](044-many-to-many.md) [64](064-through-relations-are-read-only-pivots.md)
- `pointer`: [7](007-eq-with-a-nil-pointer.md) [87](087-an-embedded-field-must-be-a-struct-value.md)
- `postgres`: [26](026-feature-examples-run-on-postgresql-only.md) [43](043-database-specific-helpers.md) [49](049-postgresql-first.md) [50](050-three-layers-for-migrations.md) [72](072-partitions-are-not-migrations.md) [94](094-migrations-keep-a-columns-dependents-working-while-it.md) [114](114-an-identity-columns-default-is-not-ants-to-change.md) [137](137-stored-tsvector-columns-have-a-separate-match-helper.md) [158](158-drift-compares-two-explicit-serial-defaults.md) [161](161-a-month-is-read-from-its-bounds.md)
- `primary-key`: [19](019-a-zero-generated-key-means-assign-one.md) [150](150-a-zero-key-nothing-fills-is-refused.md)
- `probe`: [100](100-probes-are-named-after-their-model-in-the-plural.md) [101](101-probes-are-pinned.md)
- `probes`: [4](004-conditions-are-not-tied-to-a-model.md) [18](018-wrong-model-detection.md) [21](021-configuration-runs-when-a-context-opens.md) [34](034-self-joins.md) [37](037-references-to-outer-queries.md)
- `Project`: [5](005-select-closures-run-client-side.md) [35](035-typed-expressions.md) [147](147-a-projection-reads-null-into-a-plain-field-as-zero.md)
- `qb`: [14](014-the-dialect-interface-exposes-an-internal-type.md) [62](062-the-statement-ast-is-public.md)
- `Raw`: [24](024-raw-sql-arguments.md) [27](027-all-returns-t.md)
- `readonly`: [98](098-an-explicit-write-refuses-the-columns-ant-keeps.md) [122](122-what-the-database-fills-is-read-back-by-key.md) [139](139-a-field-can-not-ask-for-two-things-ant-can-not-both-do.md)
- `Recursive`: [36](036-derived-tables-keep-the-models-name.md) [88](088-sql-server-recurses-with-union-all-and-a-path-of-keys.md)
- `relation-reconcile`: [30](030-tracking-follows-keys-not-addresses.md) [149](149-a-has-one-pointer-is-the-truth-about-its-row.md)
- `relations`: [54](054-foreign-keys.md) [93](093-tracking-follows-what-a-save-wrote.md) [125](125-open-refuses-a-model-it-would-misread.md)
- `Remove`: [40](040-soft-deletes-are-a-filter.md) [93](093-tracking-follows-what-a-save-wrote.md)
- `rename`: [55](055-renames.md) [82](082-a-rename-must-land-on-a-name-the-models-have.md) [94](094-migrations-keep-a-columns-dependents-working-while-it.md) [172](172-an-enum-value-renames-in-place.md)
- `Render`: [14](014-the-dialect-interface-exposes-an-internal-type.md) [126](126-a-migration-runs-as-it-says-or-not-at-all.md)
- `repair`: [56](056-failure.md) [127](127-commands-refuse-to-guess-which-migration-is-meant.md)
- `repeatable`: [11](011-migration-model.md) [116](116-code-files-rerun-after-versioned-migrations.md)
- `RETURNING`: [13](013-mariadb-shares-the-mysql-package.md) [122](122-what-the-database-fills-is-read-back-by-key.md)
- `Rollback`: [57](057-drift-compares-what-the-server-stored.md) [70](070-dryrun-really-runs.md) [99](099-a-retried-attempt-leaves-nothing-in-the-tracker.md) [106](106-begin-inside-a-transaction-opens-a-savepoint.md)
- `rollback`: [121](121-a-rollback-forgets-what-its-transaction-began-tracking.md) [155](155-a-bound-transaction-settles-what-its-contexts-did.md)
- `SaveChanges`: [28](028-identity-and-relation-slices.md) [30](030-tracking-follows-keys-not-addresses.md) [31](031-failed-saves.md) [32](032-navigation-and-foreign-key-changes.md) [61](061-sessions-take-options.md) [63](063-savechanges-batches-inserts.md) [79](079-a-tracking-context-serves-one-operation-at-a-time.md) [92](092-tracked-values-compare-as-stored.md) [93](093-tracking-follows-what-a-save-wrote.md) [96](096-a-forced-update-stamps-updatedat.md) [110](110-a-version-the-application-sets-is-the-one-checked.md) [111](111-a-replaced-entity-keeps-what-the-body-could-not-give.md) [123](123-an-after-hooks-edit-is-a-change-to-save.md) [140](140-an-update-that-changed-nothing-is-checked-against-the-row.md) [142](142-chunk-lets-go-of-a-group-that-has-nothing-to-save.md) [148](148-a-query-with-computed-fields-stays-tracked.md) [149](149-a-has-one-pointer-is-the-truth-about-its-row.md) [153](153-changes-saved-through-another-context-are-reported-by-antvet.md) [156](156-scoped-relation-saves-apply-target-eq-values.md)
- `savepoint`: [31](031-failed-saves.md) [61](061-sessions-take-options.md) [106](106-begin-inside-a-transaction-opens-a-savepoint.md) [121](121-a-rollback-forgets-what-its-transaction-began-tracking.md) [154](154-the-ctx-carries-the-transaction.md)
- `scan`: [25](025-scanning.md) [160](160-sqlite-time-aggregates-read-text.md)
- `Scanner`: [42](042-converters-are-scanner-and-valuer.md) [136](136-an-outer-joins-side-reads-what-only-the-driver-converts.md)
- `schema`: [16](016-test-schema-is-hand-written.md) [50](050-three-layers-for-migrations.md)
- `schema-diff`: [82](082-a-rename-must-land-on-a-name-the-models-have.md) [172](172-an-enum-value-renames-in-place.md)
- `SchemaOf`: [66](066-several-models-can-share-a-table.md) [117](117-the-migrate-program-takes-the-applications-options.md)
- `secure.Value`: [95](095-in-means-eq-and-other-value-rules.md) [109](109-numbers-inside-an-interface-read-as-json-number.md)
- `Select`: [5](005-select-closures-run-client-side.md) [9](009-owner-review-of-step-1.md) [136](136-an-outer-joins-side-reads-what-only-the-driver-converts.md)
- `sequence`: [73](073-drift-compares-what-models-declare.md) [158](158-drift-compares-two-explicit-serial-defaults.md)
- `serial`: [114](114-an-identity-columns-default-is-not-ants-to-change.md) [158](158-drift-compares-two-explicit-serial-defaults.md)
- `Session`: [29](029-sessions.md) [61](061-sessions-take-options.md) [120](120-the-root-does-not-track-and-a-session-does.md) [142](142-chunk-lets-go-of-a-group-that-has-nothing-to-save.md) [148](148-a-query-with-computed-fields-stays-tracked.md) [153](153-changes-saved-through-another-context-are-reported-by-antvet.md) [154](154-the-ctx-carries-the-transaction.md)
- `Snapshot`: [11](011-migration-model.md) [28](028-identity-and-relation-slices.md) [57](057-drift-compares-what-the-server-stored.md) [82](082-a-rename-must-land-on-a-name-the-models-have.md) [92](092-tracked-values-compare-as-stored.md) [108](108-kept-sql-by-shape-unboxed-keys-compact-snapshots-and.md)
- `snapshot`: [123](123-an-after-hooks-edit-is-a-change-to-save.md) [127](127-commands-refuse-to-guess-which-migration-is-meant.md)
- `soft-delete`: [40](040-soft-deletes-are-a-filter.md) [80](080-every-write-moves-the-version-on.md) [102](102-updatemany-keeps-what-a-bulk-update-keeps.md) [124](124-a-conflict-target-repeats-a-partial-indexs-predicate.md) [151](151-deletedat-is-a-time-or-null-in-json.md)
- `sql.Null`: [51](051-modeling-added-for-migrations.md) [147](147-a-projection-reads-null-into-a-plain-field-as-zero.md)
- `sqlite`: [76](076-one-clock-for-times-without-a-zone.md) [84](084-every-time-is-bound-in-utc-where-columns-keep-no-zone.md) [118](118-a-sqlite-dsn-says-whether-foreign-keys-are-on.md) [159](159-a-sqlite-dsn-names-its-lock-behavior.md) [160](160-sqlite-time-aggregates-read-text.md) [164](164-explain-reads-each-databases-plan-statement.md) [167](167-sqlite-row-locks-in-a-transaction-that-holds-the-write-lock.md) [170](170-insertfrom-takes-a-conflict-clause.md)
- `sqlserver`: [76](076-one-clock-for-times-without-a-zone.md) [84](084-every-time-is-bound-in-utc-where-columns-keep-no-zone.md) [88](088-sql-server-recurses-with-union-all-and-a-path-of-keys.md) [164](164-explain-reads-each-databases-plan-statement.md) [165](165-sql-server-orders-only-where-it-counts.md)
- `step-1`: [8](008-step-1-file-count.md) [9](009-owner-review-of-step-1.md)
- `subquery`: [37](037-references-to-outer-queries.md) [38](038-bulk-writes.md) [83](083-antvet-judges-only-what-it-can-see.md) [165](165-sql-server-orders-only-where-it-counts.md) [175](175-antvet-follows-a-query-built-up-in-a-variable.md)
- `tags`: [125](125-open-refuses-a-model-it-would-misread.md) [139](139-a-field-can-not-ask-for-two-things-ant-can-not-both-do.md)
- `terminal-call`: [18](018-wrong-model-detection.md) [37](037-references-to-outer-queries.md)
- `time`: [1](001-ant-ordered-replaces-cmp-ordered.md) [84](084-every-time-is-bound-in-utc-where-columns-keep-no-zone.md) [92](092-tracked-values-compare-as-stored.md) [160](160-sqlite-time-aggregates-read-text.md)
- `timestamps`: [25](025-scanning.md) [41](041-automatic-timestamps.md)
- `token`: [122](122-what-the-database-fills-is-read-back-by-key.md) [139](139-a-field-can-not-ask-for-two-things-ant-can-not-both-do.md) [140](140-an-update-that-changed-nothing-is-checked-against-the-row.md)
- `tracker`: [29](029-sessions.md) [99](099-a-retried-attempt-leaves-nothing-in-the-tracker.md) [121](121-a-rollback-forgets-what-its-transaction-began-tracking.md) [153](153-changes-saved-through-another-context-are-reported-by-antvet.md) [155](155-a-bound-transaction-settles-what-its-contexts-did.md)
- `tracking`: [6](006-return-shapes-and-tracking.md) [142](142-chunk-lets-go-of-a-group-that-has-nothing-to-save.md) [148](148-a-query-with-computed-fields-stays-tracked.md) [166](166-find-reads-the-row-after-a-bulk-delete.md)
- `transaction`: [22](022-inserts-run-one-statement-per-row.md) [47](047-hooks-run-inside-the-write.md) [56](056-failure.md)
- `transactions`: [141](141-a-statement-inside-an-each-loop-on-one-connection-is-refused.md) [167](167-sqlite-row-locks-in-a-transaction-that-holds-the-write-lock.md)
- `Tx`: [2](002-tx-is-a-generic-method-on-context.md) [106](106-begin-inside-a-transaction-opens-a-savepoint.md) [154](154-the-ctx-carries-the-transaction.md)
- `Type`: [131](131-a-foreign-key-column-follows-its-target.md) [135](135-a-column-ant-made-for-an-earlier-default-is-converted-as-ant-stored-it.md)
- `Union`: [36](036-derived-tables-keep-the-models-name.md) [81](081-a-set-operations-members-keep-their-own-paging.md)
- `unit-of-work`: [29](029-sessions.md) [61](061-sessions-take-options.md) [120](120-the-root-does-not-track-and-a-session-does.md)
- `UnixTime`: [92](092-tracked-values-compare-as-stored.md) [95](095-in-means-eq-and-other-value-rules.md)
- `unlink`: [30](030-tracking-follows-keys-not-addresses.md) [32](032-navigation-and-foreign-key-changes.md) [149](149-a-has-one-pointer-is-the-truth-about-its-row.md)
- `Update`: [23](023-guarding-bulk-writes.md) [38](038-bulk-writes.md) [98](098-an-explicit-write-refuses-the-columns-ant-keeps.md) [140](140-an-update-that-changed-nothing-is-checked-against-the-row.md)
- `UpdatedAt`: [41](041-automatic-timestamps.md) [80](080-every-write-moves-the-version-on.md) [96](096-a-forced-update-stamps-updatedat.md) [102](102-updatemany-keeps-what-a-bulk-update-keeps.md) [103](103-an-upserts-update-stamps-updatedat.md) [111](111-a-replaced-entity-keeps-what-the-body-could-not-give.md)
- `UpdateMany`: [89](089-one-column-one-value.md) [98](098-an-explicit-write-refuses-the-columns-ant-keeps.md) [102](102-updatemany-keeps-what-a-bulk-update-keeps.md) [140](140-an-update-that-changed-nothing-is-checked-against-the-row.md)
- `Upsert`: [47](047-hooks-run-inside-the-write.md) [75](075-generated-values-without-returning.md) [80](080-every-write-moves-the-version-on.md) [85](085-mysql-reads-the-table-it-writes-through-a-derived-table.md) [89](089-one-column-one-value.md) [91](091-an-upsert-checks-a-copys-version-on-mysql-and-mariadb.md) [103](103-an-upserts-update-stamps-updatedat.md) [123](123-an-after-hooks-edit-is-a-change-to-save.md) [124](124-a-conflict-target-repeats-a-partial-indexs-predicate.md) [157](157-insert-validation-leaves-zero-default-fields-to-the-database.md)
- `upsert`: [119](119-an-upserts-update-writes-the-zero-a-default-field-holds.md) [170](170-insertfrom-takes-a-conflict-clause.md)
- `UseConn`: [79](079-a-tracking-context-serves-one-operation-at-a-time.md) [141](141-a-statement-inside-an-each-loop-on-one-connection-is-refused.md)
- `UseTx`: [79](079-a-tracking-context-serves-one-operation-at-a-time.md) [141](141-a-statement-inside-an-each-loop-on-one-connection-is-refused.md)
- `utc`: [41](041-automatic-timestamps.md) [76](076-one-clock-for-times-without-a-zone.md) [84](084-every-time-is-bound-in-utc-where-columns-keep-no-zone.md)
- `UTCTimes`: [74](074-dialects-state-their-differences.md) [76](076-one-clock-for-times-without-a-zone.md)
- `uuid`: [131](131-a-foreign-key-column-follows-its-target.md) [145](145-helpers-take-a-field-of-any-type.md) [150](150-a-zero-key-nothing-fills-is-refused.md) [168](168-ids-uuidv7-makes-a-uuid-of-any-16-byte-type.md)
- `Validate`: [47](047-hooks-run-inside-the-write.md) [157](157-insert-validation-leaves-zero-default-fields-to-the-database.md)
- `valuer`: [42](042-converters-are-scanner-and-valuer.md) [92](092-tracked-values-compare-as-stored.md)
- `version`: [80](080-every-write-moves-the-version-on.md) [90](090-merge-keeps-what-other-writes-keep.md) [91](091-an-upsert-checks-a-copys-version-on-mysql-and-mariadb.md) [93](093-tracking-follows-what-a-save-wrote.md) [102](102-updatemany-keeps-what-a-bulk-update-keeps.md) [110](110-a-version-the-application-sets-is-the-one-checked.md) [139](139-a-field-can-not-ask-for-two-things-ant-can-not-both-do.md) [140](140-an-update-that-changed-nothing-is-checked-against-the-row.md)
- `warning`: [52](052-new-not-null-columns-fill-existing-rows.md) [55](055-renames.md)
- `Where`: [18](018-wrong-model-detection.md) [37](037-references-to-outer-queries.md) [65](065-scoped-relations-use-the-same-filter-path.md) [156](156-scoped-relation-saves-apply-target-eq-values.md) [163](163-includejoined-applies-the-relations-own-conditions.md)
- `WithConflict`: [119](119-an-upserts-update-writes-the-zero-a-default-field-holds.md) [124](124-a-conflict-target-repeats-a-partial-indexs-predicate.md) [170](170-insertfrom-takes-a-conflict-clause.md)
- `WithConvention`: [67](067-naming-and-conventions-are-global.md) [117](117-the-migrate-program-takes-the-applications-options.md)
- `WithLogger`: [46](046-interceptors.md) [61](061-sessions-take-options.md)
- `WithNaming`: [67](067-naming-and-conventions-are-global.md) [117](117-the-migrate-program-takes-the-applications-options.md)
- `WithoutAutoTx`: [31](031-failed-saves.md) [63](063-savechanges-batches-inserts.md)
- `WithReplicas`: [48](048-replicas.md) [61](061-sessions-take-options.md)
- `WithTracking`: [29](029-sessions.md) [61](061-sessions-take-options.md) [79](079-a-tracking-context-serves-one-operation-at-a-time.md) [120](120-the-root-does-not-track-and-a-session-does.md)
- `zero-value`: [19](019-a-zero-generated-key-means-assign-one.md) [52](052-new-not-null-columns-fill-existing-rows.md) [119](119-an-upserts-update-writes-the-zero-a-default-field-holds.md) [147](147-a-projection-reads-null-into-a-plain-field-as-zero.md) [150](150-a-zero-key-nothing-fills-is-refused.md) [157](157-insert-validation-leaves-zero-default-fields-to-the-database.md)

<!-- end of index -->
