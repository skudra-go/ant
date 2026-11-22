---
id: 154
title: "The ctx carries the transaction"
area: transactions
status: active
tags: [InTx, Tx, hooks, ctx, savepoint, root, Session]
issues: [144, 146]
related: [2, 121, 155]
changed_by: []
code: [tx.go, ant.go, unit.go, upsert.go, table.go, query.go, run.go, hooks.go]
tests: [tests/hook_tx_test.go, tests/bound_ctx_test.go]
---

# 154. The ctx carries the transaction

**In short:** `ant.InTx(ctx, tx)` binds a ctx to the transaction `tx` is in. Every context on the same connection pool that is in no transaction of its own runs what it is given that ctx for in the transaction: the root, a repository built with it, and a session. Its `SaveChanges`, `Tx` and `Begin` become savepoints there. A hook's ctx is bound to its write's transaction without asking. An unbound ctx behaves as before.

## Decision

Code is built once with the root `*AppDB`, and `Tx` hands its function a context of the transaction but the caller's ctx. So a repository called inside `Tx` wrote outside it, committing on its own, and a write to a row the transaction had locked waited for that lock until the ctx ended (#146). A hook gets only a ctx, and the documented hook used the global root, so its query could not see rows written earlier in its own transaction, and an After hook that wrote its own row waited on the lock its own save held, forever with no deadline (#144).

Threading `tx` through every call is the only safe structure without help, and it reaches neither hooks nor code that cannot take a new parameter. A ctx already goes everywhere. So the ctx carries the transaction: a context value holds the transaction's connection, its savepoint guard if it came from `Begin` inside a transaction, and its count of `Each` loops. A context running on the pool looks for it at every statement, through an executor wrapped around the pool, so every query, write, raw statement and replica read takes it. Where a context begins a transaction of its own, for `Tx`, `Begin`, `SaveChanges` or a write with several statements, it joins the bound one instead, as a context of that transaction, and opens a savepoint. A context already in a transaction, or on a connection from `UseConn`, keeps to it: it was put there on purpose.

`InTx` takes the handle rather than reading the transaction from `Tx`'s arguments, because `Tx`'s signature stays as it is (decision 2). Given a handle in no transaction, it binds the ctx to none, so a hook or a service can write what must stay if the transaction rolls back.

A hook's ctx is bound to the transaction its write runs in: the context's own, the one a bound ctx gives, or the one `SaveChanges`, `Insert` or `Upsert` opens for a write with an After hook. Before hooks run inside that transaction too, `Upsert`'s included, so what they write is undone with the write. With no transaction, as for one `Insert` with only a Before hook, the ctx is left as it came. `AfterLoad` and `AfterInclude` in a transaction get a bound ctx as well. A binding costs two allocations, so it is made only where a hook runs. The transaction ant opens for a write runs its statements as a `Tx` does, so a failure that ends it stops what a hook runs after.

A ctx holds one binding per pool. A hook on one pool binds the ctx to its own transaction and leaves a binding to another pool's in place, so code with two databases keeps each in its own transaction.

What this changes for hook code that worked before:

- A hook that writes through the root what must stay if the write fails, such as a log of attempts, now rolls back with the write. `ant.InTx(ctx, db)`, with `db` in no transaction, binds the ctx to none for that write.
- A `Tx` a hook opens with its ctx is a savepoint in the write's transaction, so `ant.Isolation` there returns `ErrNestedIsolation` and `ant.Attempts` does not retry, as for any `Tx` inside another.
- A goroutine that keeps the hook's ctx and runs after the write has ended gets `ErrTxDone`. Give it a ctx of its own, or `ant.InTx(ctx, db)`.

Rejected: routing by the identity of the ctx `Tx` was called with, which needs `Tx` to remember ctx values and fails for the ctx a caller derives with a timeout. Rejected too: an error when the root writes while a transaction is open on its ctx. It would need the same marking, and it would refuse the code it should fix.
