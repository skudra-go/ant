---
id: 106
title: "Begin inside a transaction opens a savepoint"
area: transactions
status: active
tags: [Begin, savepoint, Commit, Rollback, nested-tx, Tx]
issues: [72]
related: []
changed_by: []
code: [tx.go]
tests: [tests/features/transactions/tx_test.go, tests/tx_test.go, tests/concurrency/tx_test.go]
---

# 106. Begin inside a transaction opens a savepoint

**In short:** Begin on a context already in a transaction opens a savepoint whose Commit releases it and Rollback rolls back to it, leaving the outer transaction in control.

## Decision

`Begin` on a context already in a transaction returned that same context, so a helper written as Begin, work, Commit committed its caller's transaction early, and its Rollback rolled back the caller's work too (#72). A nested `Tx` already used a savepoint. `Begin` now does the same: inside a transaction it opens a savepoint and returns a context bound to it, whose `Commit` releases it and whose `Rollback` rolls back to it and takes back the tracker changes since, while the outer transaction goes on. The outer transaction still decides: its rollback undoes what the inner one committed, and observers wait for its commit. Options such as `Isolation` apply to a new transaction only, as with a nested `Tx`. Returning an error instead was rejected, since it would make a helper that begins its own transaction unusable from inside `Tx`.
