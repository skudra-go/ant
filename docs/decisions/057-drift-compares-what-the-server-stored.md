---
id: 57
title: "Drift compares what the server stored"
area: migrations
status: active
tags: [drift, temp-table, SameExpression, pg_get_expr, Snapshot, Rollback]
issues: []
related: [59, 73]
changed_by: []
code: [dialect/postgres/inspect.go, schema/dialect.go, migrate/operate.go, dialect/postgres/expression.go]
tests: [tests/features/migrations/drift_test.go, tests/features/migrations/adopt_test.go, dialect/postgres/split_test.go]
---

# 57. Drift compares what the server stored

**In short:** Drift builds the expected tables as temporary tables in a rolled-back transaction and reads them back, falling back to SameExpression for text differences, using the snapshot or else the models.

## Decision

PostgreSQL rewrites what it is given: `int` becomes `integer`, and `x IN (1, 2)` becomes `x = ANY (ARRAY[1, 2])`. Matching that by hand would never be complete, and a tool with false alarms gets ignored.

So drift creates the expected tables as temporary tables in a transaction it rolls back, and reads them back, which needs no extra permission and leaves nothing. The roadmap rules out shadow databases for generating migrations. This is narrower: it only spells expressions, only for `drift`.

That is not quite enough, because the server prints one check in two ways, depending on whether it was typed or restored from a dump. When text differs, `SameExpression` compares with casts and parentheses removed. It can miss a change that only regroups an expression, which is the better mistake.

`drift` uses the snapshot when there is one, since that is what migrations produce. With none, it uses the models, which is what makes the adoption loop work.
