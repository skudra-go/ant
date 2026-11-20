---
id: 130
title: "How a table is partitioned is part of the table"
area: migrations
status: active
tags: [partitions, PartitionByRange, diff, drift, check, Repartition]
issues: [96]
related: [72]
changed_by: []
code: [schema/diff.go, schema/change.go, schema/schema.go, dialect/postgres/ddl.go]
tests: [tests/features/migrations/modeling_test.go]
---
# 130. How a table is partitioned is part of the table

**In short:** diff, check and drift compare a table's partitioning, and a change to it is a schema.Repartition that warns and writes no SQL, since PostgreSQL can't change it in place.

## Decision

Partitioning was never compared, so adding, removing or changing `PartitionBy...` gave an empty plan, and `check`, `diff` and `drift` all said the database matched (#96). It is compared now, and a difference is a `schema.Repartition` change with a warning to rebuild the table by hand, and no statements, as removing an enum value is. Decision 72 still holds for the partitions themselves: the months come and go, and drift ignores them; it compares how the parent is partitioned.
