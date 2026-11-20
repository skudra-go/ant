---
id: 99
title: "A retried attempt leaves nothing in the tracker"
area: transactions
status: amended
tags: [Attempts, retry, deadlock, tracker, Rollback, serialization-failure]
issues: [63]
related: [79]
changed_by: [121]
code: [tx.go, unit.go, internal/track/track.go]
tests: [tests/features/transactions/retry_test.go, tests/features/transactions/retry_policy_test.go, tests/concurrency/tx_test.go]
---

# 99. A retried attempt leaves nothing in the tracker

**In short:** Before an Attempts retry the tracker forgets entries that began during the failed attempt, while entities tracked earlier stay; plain rollback keeps entities pending.

> **Amended.** Decision 121 does on every rollback what this did only before a retry, which this rejected: a rollback forgets the entities its transaction began tracking, since the next save wrote what a rolled back transaction had done (#145, #224). A retried attempt still leaves nothing in the tracker, now through that rollback.

## Decision

`Attempts` runs `f` again after a deadlock or serialization failure, on the same tracker. The rollback put the tracker back as the save found it, but the entities the failed attempt loaded stayed tracked with its edits, so the next attempt's tracked read got the edited instance and changed it again, and an entity it added stayed pending beside the one the next attempt added (#63). Before a retry, the tracker now forgets every entry that began during the failed attempt, which `f` loads or adds again. What was tracked before the transaction stays, with any change made to it, since ant cannot tell the attempt's edits from earlier ones without copying every entity at each attempt; `f` should load what it changes. Doing this on every rollback, not only before a retry, was rejected: a failed transaction's entities stay pending so that `SaveChanges` can be repeated (decision 79).
