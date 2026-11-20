---
id: 126
title: "A migration runs as it says, or not at all"
area: migrations
status: active
tags: [no-transaction, directives, BEGIN, COMMIT, up, Render]
issues: [181, 182]
related: [56, 115]
changed_by: []
code: [migrate/migration.go, migrate/engine.go]
tests: [tests/features/migrations/reset_test.go, tests/features/migrations/up_test.go]
---
# 126. A migration runs as it says, or not at all

**In short:** Before a file runs, up refuses a -- ant:no-transaction after the first statement, and a top-level BEGIN, START, COMMIT, END, ABORT or ROLLBACK in a file that runs in ant's transaction.

## Decision

Directives are read from the comments at the top of a file, so a `SET` a `Render` put first made `-- ant:no-transaction` a plain comment, and the file ran in a transaction, where its `CREATE INDEX CONCURRENTLY` failed (#182). ant runs each file in a transaction of its own, which a `COMMIT` in the file ends, so when a later statement failed the statements before it stayed, though the error said the file was rolled back (#181). Both are now `ErrBadMigration` before anything of the file runs. They are checked when a file is about to run, not when it is read, so a file applied before the checks still loads. `ROLLBACK TO` a savepoint stays inside the transaction and is allowed, and a file with `-- ant:no-transaction` may manage its own. A late directive that the top already gives, as a squash that keeps the files has inside, changes nothing and is allowed. Stripping the statements was rejected as rewriting the developer's SQL.
