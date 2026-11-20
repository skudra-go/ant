---
id: 31
title: "Failed saves"
area: transactions
status: active
tags: [SaveChanges, savepoint, ErrTxDone, ErrConcurrency, ErrKeyChanged, WithoutAutoTx, WithRetry, RetryEndsTx]
issues: [71, 90]
related: [71, 90]
changed_by: []
code: [tx.go, unit.go, dialect/syntax.go, errors.go]
tests: [tests/tx_test.go, tests/features/transactions/retry_test.go, tests/features/change_tracking/save_changes_test.go]
---

# 31. Failed saves

**In short:** SaveChanges runs in a transaction or savepoint, clears generated keys on failure, and marks a transaction lost when the database ends it, so later statements return ErrTxDone.

> **Note.** Pull request #276 would change it, in decision 121, which is pending.

## Decision

`SaveChanges` runs in one transaction, or a savepoint inside one, even for a single statement, as EF Core does, since PostgreSQL aborts the whole transaction when one statement fails. On failure nothing is saved, generated keys are cleared, and pending changes remain, so the call can be repeated. A rollback of an enclosing transaction also reverses what the tracker recorded as saved, which EF Core does not do. An entity whose row vanished is `ErrConcurrency`, and a changed primary key is `ErrKeyChanged`, rather than a write to the wrong row.

With `WithoutAutoTx` and no transaction open, each statement stands once it runs, so a failure partway leaves the earlier rows written. Those keep their generated keys and stamps, and the tracker records them as saved and tells observers, so a retry, by hand or by `WithRetry`, writes only the rest; clearing them, as a rolled back save does, made the retry insert them again (#71). Refusing the retry instead was rejected: the rows are there, and the tracker should say so.

A failed save leaves the transaction usable only where the database rolls back just the failed statement. On MySQL, MariaDB, and SQL Server a deadlock, and on MariaDB a serialization failure, rolls back the whole transaction, so the savepoint is gone and every later statement on the connection ran outside any transaction, while the tracker took the earlier saves as done (#90). Now, when such an error ends a statement on those databases (`dialect.Syntax.RetryEndsTx`), or returning to one of ant's savepoints fails, ant marks the transaction lost: its tracker changes are undone, its observers never run, every later statement returns `ErrTxDone`, and `Tx` and `Commit` roll it back and return that error. `WithRetry` then runs the whole transaction again, as it should.
