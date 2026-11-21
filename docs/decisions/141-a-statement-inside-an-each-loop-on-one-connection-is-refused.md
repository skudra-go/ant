---
id: 141
title: "A statement inside an Each loop on one connection is refused"
area: queries
status: active
tags: [Each, ErrQueryInEach, transactions, UseConn, UseTx, Begin, RunsWhileStreaming, streaming]
issues: [163]
related: [33, 79]
changed_by: []
code: [run.go, tx.go, query.go, projection.go, dialect/syntax.go, dialect/sqlite/sqlite.go]
tests: [tests/each_stream_test.go]
---

# 141. A statement inside an Each loop on one connection is refused

**In short:** in a transaction, or on a context from `UseConn`, a statement inside an `Each` loop returns `ErrQueryInEach` before it reaches the driver, except on SQLite, whose connection can run it.

## Decision

`Each` streams its rows on one connection until the loop ends. Outside a transaction a statement in the loop takes another connection from the pool, but a transaction, a context from `Begin`, a savepoint, `UseTx` and `UseConn` have only theirs. There PostgreSQL, MySQL and MariaDB failed the statement with `driver: bad connection`, which reads like a network fault, and the transaction rolled back (#163). SQL Server looked able to run it while the rows fit in the driver's buffer, and past that the statement waited for good, as did a `UseConn` context on PostgreSQL. Decision 33 refuses `Include` in `Each` for the same reason.

Each such connection now counts the `Each` loops reading from it, shared by every context bound to it: a session, a savepoint, and a transaction begun on a `UseConn` context. While the count is above zero, every statement on that connection, and beginning a transaction on a `UseConn` one, returns `ErrQueryInEach` without reaching the driver, so the transaction can go on once the loop ends. A loop that breaks, returns or panics lets go when the iterator returns.

`dialect.Syntax.RunsWhileStreaming` says a connection can run a statement while it streams. SQLite's can, and it reads every row of such a loop, so it refuses nothing, which keeps code that works there working. Refusing on every database, as decision 33 does for `Include`, was rejected: it would break loops that work on SQLite today.

Not done: outside a transaction, a pool limited to one connection still waits until the context ends, since another goroutine could give a connection back. The `Each` docs say to collect what the loop needs and act after it, or read with `All` or `Chunk`.
