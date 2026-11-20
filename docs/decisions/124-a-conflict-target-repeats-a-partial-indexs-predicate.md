---
id: 124
title: "A conflict target repeats a partial unique index's predicate"
area: writes
status: active
tags: [Upsert, OnConflict, InsertMany, WithConflict, partial-index, soft-delete]
issues: [150]
related: [40, 119]
changed_by: []
code: [upsert.go, qb/qb.go, dialect/render.go]
tests: [tests/partial_conflict_test.go]
---
# 124. A conflict target repeats a partial unique index's predicate

**In short:** When the conflict's columns are exactly those of one partial unique index the model declares, and of no key or full unique index, ON CONFLICT repeats that index's predicate.

## Decision

A soft-deleted model keeps a column unique with a partial index, `WHERE deleted_at IS NULL`, so a deleted value can be used again. PostgreSQL and SQLite match a partial index to `ON CONFLICT` only when the statement repeats its predicate, so `Upsert` and `InsertMany` with `WithConflict` by that column failed with "no unique or exclusion constraint matching" (#150). Now the conflict target takes the predicate from the model: when its columns are exactly those of one partial unique index the model declares, and no key or full unique index covers them, the statement reads `ON CONFLICT (cols) WHERE predicate`. `qb.Conflict` has a `Target` field for it. An option on `Conflict` to write the predicate by hand was rejected as new API for what the model already says. Two partial indexes on the same columns are left for the database to refuse, since which one is meant is a guess. MySQL and MariaDB have no partial indexes, and `ON DUPLICATE KEY` names no target.
