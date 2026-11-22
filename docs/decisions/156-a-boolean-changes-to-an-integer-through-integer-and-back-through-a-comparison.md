---
id: 156
title: "A boolean changes to an integer through integer, and back through a comparison"
area: migrations
status: active
tags: [AlterColumn, USING, boolean, integer, smallint, bigint, Type]
issues: [312]
related: [135]
changed_by: []
code: [dialect/postgres/ddl.go]
tests: [dialect/postgres/ddl_test.go, schema/retype_test.go]
---

# 156. A boolean changes to an integer through integer, and back through a comparison

**In short:** PostgreSQL casts a boolean to integer only, so a column that becomes a smallint or a bigint goes through integer, and one that becomes a boolean reads as a comparison with zero. Both keep NULL a NULL.

## Decision

A flag that becomes a counter or a status (`Active bool` to `Active int`) gave a migration PostgreSQL cannot run, on any data and on an empty table: `USING "active"::bigint` fails, because there is no cast from boolean to bigint, and `USING "active"::boolean` fails the same way back. The warning said only that the change may fail or lose data, though the statement could never work.

`ALTER COLUMN ... TYPE` now converts the two directions PostgreSQL has no cast for: a boolean to smallint or bigint as `"active"::integer::bigint`, through the one cast it has, and a smallint, integer or bigint to boolean as `"active" <> 0`. A boolean to integer keeps the plain cast it already had.

`CASE WHEN "active" THEN 1 ELSE 0 END` was rejected for the same conversion: a NULL boolean is neither true nor false, so the `ELSE` would turn it into 0, and a nullable column would gain values it never held. The chained cast and the comparison both read NULL as NULL, so a nullable column keeps its NULLs.
