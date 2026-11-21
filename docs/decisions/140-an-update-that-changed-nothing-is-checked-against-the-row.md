---
id: 140
title: "On MySQL, an update that changed nothing is checked against the row"
area: tracking
status: active
tags: [SaveChanges, ConcurrencyError, MySQL, MariaDB, RowsAffected, clientFoundRows, ChangedRowsOnly, token, version, Update, UpdateMany]
issues: [215]
related: [80]
changed_by: []
code: [unit.go, dialect/dialect.go, dialect/mysql/mysql.go]
tests: [tests/conformance_test.go]
---

# 140. On MySQL, an update that changed nothing is checked against the row

**In short:** where the driver counts the rows an `UPDATE` changed, not the rows it matched, a tracked update that counts zero asks whether the row is still there under the same conditions, the version or token included, and saves if it is.

## Decision

ant reads a tracked write that affects no row as a row changed or removed since it was loaded. MySQL and MariaDB count the rows an `UPDATE` changed, so a save of the values a row already holds, as a `PUT` whose body equals the row or two requests making the same edit, counted zero and failed with `ConcurrencyError` there, and nowhere else (#215).

A dialect whose driver counts that way says so through `dialect.ChangedRowsOnly`. For such a dialect, an `UPDATE` that counts zero is followed by a read of the row under the write's own conditions. Those carry the version or token, so a stale copy is still a conflict even when its values are the row's. Inside a transaction the read locks the row: at `REPEATABLE READ` a plain read answers from the transaction's snapshot, which can still hold a row another transaction deleted. A `DELETE` that matched nothing still means the row is gone, as only an `UPDATE` can match a row and change nothing.

A bulk `Update` and `UpdateMany` return the count as the driver gives it, which is the rows they changed on these two, and the docs say so.

Rejected:

- Setting the driver's `clientFoundRows`, which counts matched rows: an upsert on `ON DUPLICATE KEY` reads a count of zero as "the conflict did nothing", which the option would break, and ant can't set it on a pool an application opened.
- Reading zero as success on these two: a row another request deleted would save without a word.
