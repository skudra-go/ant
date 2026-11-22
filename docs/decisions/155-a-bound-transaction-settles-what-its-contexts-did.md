---
id: 155
title: "A bound transaction settles what its contexts did"
area: transactions
status: active
tags: [InTx, tracker, rollback, settle, goroutines, ErrTxDone]
issues: [144, 146]
related: [79, 121, 154]
changed_by: []
code: [tx.go, ant.go, unit.go]
tests: [tests/bound_ctx_test.go]
---

# 155. A bound transaction settles what its contexts did

**In short:** What a context does to its tracker under a ctx bound to a transaction is settled when that transaction ends, as if the context were in it: a rollback takes back what its saves wrote into entities, leaves their changes pending, and forgets what it began tracking. The transaction is one connection, so goroutines that share a bound ctx take turns, as with `tx`. Once the transaction has ended, statements given the ctx fail with `ErrTxDone` or `sql.ErrTxDone`.

## Decision

A session saved with a bound ctx writes in a savepoint of the bound transaction. If the transaction then rolls back, the rows go, so the tracker must not keep the save as stored: the next `SaveChanges` would write nothing. Each change a write makes to its tracker is recorded with the transaction it ran in, the context's own or the bound one, and that transaction's end keeps or undoes it, as for a session made inside `Tx` (decision 121). An entity a context begins tracking under the bound ctx is forgotten by the rollback, and a generated key or stamp written into a new entity is taken back.

The transaction `SaveChanges` or `Insert` opens for its own statements now settles the same way, so that what a hook did through its bound ctx is kept or undone with the write. Nothing recorded with that transaction before, so for code without such hooks it changes nothing.

A bound ctx is the transaction's connection, which runs one statement at a time. It is not refused from a second goroutine: database/sql makes them take turns, and an `Each` loop on it refuses other statements with `ErrQueryInEach`, as it does on `tx`. A tracking context still serves one operation at a time (decision 79), and a transaction's record of tracker changes is not guarded for writes from two goroutines at once, so the rule is the one `Tx` documents: one goroutine at a time.

A ctx can outlive its transaction, kept by a goroutine or a cache. Its statements are not quietly run on the pool, which would put them outside any transaction: the transaction's connection refuses them with `sql.ErrTxDone`, or ant's `ErrTxDone` for a savepoint from `Begin` that has ended.
