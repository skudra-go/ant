---
id: 121
title: "A rollback forgets what its transaction began tracking"
area: transactions
status: active
tags: [rollback, tracker, savepoint, Attempts, anttest-isolate, claims]
issues: [145, 224]
related: [31, 79, 99, 106]
changed_by: []
code: [tx.go, ant.go, internal/track/track.go, anttest/anttest.go]
tests: [tests/features/transactions, tests/features/testing/anttest_test.go, internal/track/track_test.go]
---

# 121. A rollback forgets what its transaction began tracking

**In short:** Every rollback forgets the entities its transaction's operations began tracking, attributed through tracker claims; entities tracked before keep their changes.

## Decision

A rollback reversed what the tracker had recorded as saved, so the changes became pending again and `SaveChanges` could be repeated (decisions 31 and 79), and decision 99 forgot what a failed attempt had loaded only before `Attempts` ran it again. But after a rollback the application chose, the next save on the same context wrote what the transaction had done: a transfer refused by returning an error was applied by the audit row saved next (#145), and after `anttest.Isolate` or `Begin` and `Rollback`, rows loaded in a nested `Tx` survived in the tracker, so the next test's `SaveChanges` updated rows the rollback had taken back and failed with `ErrConcurrency` (#224). Now every rollback forgets the entities its transaction began tracking, by a load, an `Add`, an insert, or a relation, including those a released savepoint handed up: a rollback of `Tx` or a panic in it, `Rollback` after `Begin`, a failed commit, a savepoint's rollback, which forgets only what began after it, and the rollback before a retry, which this replaces. An operation that tracks holds its tracker's claim while it runs, so the transaction notes the entries numbered between its operations' claims and releases, and another goroutine's operation on the same tracker in the meantime keeps what it tracked. The forgetting waits for the claim as the reversal does, and runs after it. An entity tracked before the transaction stays, with its changes, including those made inside, since the tracker cannot tell them apart without copying every entity at each transaction, so the docs say to load inside the transaction what it changes. `RollbackTo` a named savepoint still rewinds nothing in the tracker, as it documents, and a transaction from `UseTx` outside ant's savepoints notes nothing, since ant never learns how it ends.
