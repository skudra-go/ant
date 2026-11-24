---
id: 167
title: "SQLite takes row locks as held in a transaction that holds the write lock"
area: transactions
status: active
tags: [sqlite, ForUpdate, ForShare, row locks, _txlock, transactions, dialect]
issues: [313]
related: [159]
changed_by: []
code: [run.go, tx.go, ant.go, options.go, dialect/dialect.go, dialect/sqlite/sqlite.go]
tests: [tests/sqlite_locks_test.go, dialect/sqlite/sqlite_test.go]
---

# 167. SQLite takes row locks as held in a transaction that holds the write lock

**In short:** On SQLite, `ForUpdate` and `ForShare` in a writable transaction that ant began, on a context `Open` made with `_txlock=immediate` or `_txlock=exclusive`, add nothing to the SQL and are accepted. Everywhere else they still return `ErrUnsupported`.

## Decision

SQLite has no row locks, so `ForUpdate` returned `ErrUnsupported` there, and the transactions page's first example, a transfer that locks both rows, failed on the database the quick start opens (#313). With `_txlock=immediate`, which decision 159 has every DSN choose or opt out of, the driver begins a writable transaction with `BEGIN IMMEDIATE`, which takes the database's write lock at once. No other connection can then write the rows, which is what the lock asks for.

`dialect.BeginLocker` reports whether a DSN gives such transactions, and `Open` asks it. A transaction ant begins records whether it holds the lock: a writable one on such a context does, and a read-only one does not, since the driver begins that with a plain `BEGIN` whatever `_txlock` says. A savepoint is in its transaction, so it holds what that does. A query in a transaction that holds the lock has its row lock left out before it is rendered, on a database with no row locks.

Where the lock is not certain the refusal stays: outside a transaction, in a read-only one, with `_txlock=deferred`, on a pool given to `Wrap`, whose DSN ant never sees, and in a transaction given to `UseTx`, whose options ant does not know. `ToSQL` takes no context, so it shows the statement as it is written outside a transaction, and refuses the lock. `ToSQLContext`, `Debug` and `Explain` render what the query runs in the context's transaction, without the lock in one that holds the write lock.

Rejected:

- Accepting row locks on SQLite everywhere: outside such a transaction nothing holds the rows, and the caller asked for a guarantee.
- Documenting the example as PostgreSQL, MySQL and MariaDB only: the quick start's own DSN already gives the guarantee, and the example would fail on it for no reason.
