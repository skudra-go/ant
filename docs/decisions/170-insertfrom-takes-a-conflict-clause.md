---
id: 170
title: "InsertFrom takes a conflict clause, whose update overwrites only the mapped columns"
area: writes
status: active
tags: [InsertFrom, WithConflict, OnConflict, DoUpdate, DoNothing, upsert, INSERT SELECT, sqlite, mysql]
issues: [231]
related: [38, 89]
changed_by: []
code: [upsert.go, dialect/render.go]
tests: [tests/insert_from_conflict_test.go, dialect/insert_select_test.go, tests/features/writing/bulk_test.go]
---

# 170. InsertFrom takes a conflict clause, whose update overwrites only the mapped columns

**In short:** `InsertFrom(ctx, rows, ant.WithConflict(c))` writes `INSERT ... SELECT ... ON CONFLICT`, as `InsertMany` takes the same option. The default update overwrites only the columns the projection maps, and `DoUpdate` naming another is an error.

## Decision

"Add everyone matching a query to these lists" filtered the pairs already there with `NotExists`, which a concurrent insert could still race into a unique violation (#231). `InsertFrom` now takes `InsertOption`s, and `WithConflict` applies, with the clause `InsertMany` writes: `DoNothing`, `DoUpdate`, `Set`, `UpdatedAt` set to now on the update, and the version moved on.

The row an `INSERT ... SELECT` offers holds the column's default for every column the projection does not map. The default update of an entity's upsert overwrites every updatable column, which here would write those defaults over the stored row. So the default update is limited to the mapped columns, and `DoUpdate` naming an unmapped one returns an error that names the field. `Set` still assigns whatever it names, since its values are the caller's.

Two databases need the statement written their way:

- SQLite reads the `ON` of an `ON CONFLICT` after a `SELECT`'s `FROM` as a join's, so a `SELECT` with no `WHERE` gets `WHERE (1 = 1)`, as SQLite's documentation advises. PostgreSQL gets the same, which changes nothing there.
- MySQL and MariaDB see the `SELECT`'s columns in `ON DUPLICATE KEY UPDATE`, so a list copied from the table it goes into made `user_id = user_id` ambiguous (error 1052). The rows come from a derived table whose columns are named `ant_c1`, `ant_c2` and on, as an upsert that reads its own table already does.

SQL Server has no conflict clause, so `WithConflict` returns `ErrUnsupported` there, as it does for `InsertMany`. `BatchSize` has no effect: `InsertFrom` is one statement.

Rejected:

- A conflict parameter of its own on `InsertFrom`: `WithConflict` already says this for `InsertMany`, and an option keeps existing calls compiling.
- Overwriting every updatable column, as an entity's upsert does: a column the projection leaves out would lose its stored value to the default.
