---
id: 174
title: "drift checks the views that view models read"
area: migrations
status: active
tags: [drift, view, materialized-view, db-show, migrations]
issues: [232]
related: [116, 173]
changed_by: []
code: [migrate/operate.go, migrate/cli.go, schema.go, schema/schema.go, schema/dialect.go, dialect/postgres/inspect.go, dialect/postgres/session.go, internal/liveschema/liveschema.go]
tests: [tests/features/migrations/drift_test.go, tests/features/migrations/relations_test.go, tests/features/migrations/script_test.go]
---

# 174. drift checks the views that view models read

**In short:** `drift` reports a view that a `b.View` model reads and the database lacks, and each column the model reads that the view lacks. Each one is a difference, so `drift` fails. The views come from the models, never from the snapshot, which leaves them out. `db-show` lists materialized views after the tables.

## Decision

Migrations do not create views: their SQL lives in repeatable migrations. So `drift` left view models out, and a field renamed on one passed `drift` and failed at the first query (#232). The models know which columns they read, and only the database knows which columns its view has, so `drift` asks it.

- `ant.SchemaOf` records each read-only model as a `schema.View` in `Schema.Views`, with the columns its queries read: computed and write-only fields are left out, and two models of one view share one entry.
- Migrations do not make views, so `Encode` leaves them out of the snapshot. A snapshot file is the same as before, and an old one still reads. `drift` always takes the views from the models, even when it compares the tables with the snapshot: a field renamed on a view model changes no table, so `diff` writes no new snapshot.
- A dialect that can read a relation's columns implements `schema.RelationColumns`. PostgreSQL resolves the name with `to_regclass` through the search path, as a query of the model does. It finds a table, a view, a materialized view or a foreign table alike, so `b.View` on any of them works.
- A read-only model of a table the models create reads that table, so its columns are checked against what migrations make of the table, and a table not created yet is reported once, as a table.
- A view may have more columns than the models read. `Config.Ignore` leaves out a view by name and a column as `view.column`, as it does for tables, and a view in the connection's default schema matches a pattern without the schema.
- `db-show` lists materialized views in a block of their own, only when there are some, so the output of a database without one is unchanged. A plain view holds no rows, so it is not listed.

Rejected:

- Comparing views with the snapshot: renaming a view model's field writes no snapshot, so the snapshot would keep the old column and the check would miss it.
- Keeping views in the snapshot: it would change the snapshot file of every project with a view model, for data that migrations never use.
- Reading every view in `Inspect`: a view in a schema that no table uses would not be inspected, and widening the schemas would report that schema's tables as drift.
- A key on an existing unique constraint, the third gap in #232, is left to the owner: it needs a builder API.
