---
id: 47
title: "Hooks run inside the write"
area: hooks
status: active
tags: [hooks, BeforeInsert, AfterInsert, Validate, Upsert, Observer, transaction, bulk]
issues: []
related: [63]
changed_by: []
code: [hooks.go, upsert.go, unit.go, tx.go]
tests: [tests/features/lifecycle/hooks_test.go, tests/features/lifecycle/validate_test.go, tests/features/writing/upsert_test.go, tests/concurrency/callbacks_test.go]
---

# 47. Hooks run inside the write

**In short:** Model hooks run inside the write's transaction so an After hook error rolls back; bulk operations skip them, Upsert runs insert hooks on either branch, Observers run after commit.

## Decision

Hooks are methods on the model, found once per type, so models with none pay nothing. They run inside the unit's transaction, so an error from an After hook undoes the write, and a unit with After hooks always uses a transaction. Bulk `Update`, `Delete`, `InsertMany`, `UpdateMany`, `Merge`, and `Raw` do not run them, because they do not load entities. `Upsert` writes one entity, so it checks the rules and runs `Validate`, `BeforeInsert`, and `AfterInsert`, whichever way the database goes, as GORM runs its create hooks on an upsert: which branch was taken is not known on every database before the write, and guessing it afterwards (`xmax = 0` on PostgreSQL, the row count on MySQL) would run update hooks on some databases and not others. A rolled back `Upsert` takes back its generated key and stamps and stops tracking the entity, as a rolled back `Insert` does.

`Observer`s run after the commit, and never hear about a rollback.
