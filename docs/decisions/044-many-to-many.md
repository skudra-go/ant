---
id: 44
title: "Many-to-many"
area: relations
status: active
tags: [ManyToMany, join-table, Toggle, Include, on-conflict, holdlock, for-update, pivot]
issues: [89]
related: [74, 75]
changed_by: []
code: [pivot.go, dialect/render.go, include.go]
tests: [tests/features/relations/many_to_many_test.go, tests/conformance_test.go]
---

# 44. Many-to-many

**In short:** ManyToMany goes through a model-less join table; removing from a loaded relation deletes the join row only, with dialect-specific insert-if-missing and Toggle paths.

## Decision

A relation with `b.ManyToMany` goes through a join table with no model. `Include` reads the join table, then the targets. In a loaded relation, removing an element deletes the join row, never the target, unlike a one-to-many relation. For extra columns on the join table, model it, with two one-to-many relations.

Join rows are added with `ON CONFLICT DO NOTHING`, so a pair already there is skipped. SQL Server has no such clause, so there they go in through a `MERGE ... WITH (HOLDLOCK)` that inserts the pairs it does not find, as SQL Server's upsert does (#89). `Toggle` deletes with `RETURNING` to learn which pairs were there; MySQL has no `DELETE ... RETURNING`, so there it reads them with `FOR UPDATE` first, which a second `Toggle` waits on, and deletes what it read. Removing an element from a loaded relation slice writes nothing to the target: when the slice moved another target into its memory, the join row's removal had also sent an `UPDATE` of the removed target with its own values, which MySQL and MariaDB count as no row changed and ant took for a conflict.
