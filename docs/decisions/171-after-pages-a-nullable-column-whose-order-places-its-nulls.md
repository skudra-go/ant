---
id: 171
title: "After pages a column that can hold NULL once the order places its NULLs"
area: queries
status: active
tags: [After, keyset, cursor, ErrCursor, NULL, NullsFirst, NullsLast, paging]
issues: [307]
related: [81, 143, 165]
changed_by: []
code: [paging.go]
tests: [tests/keyset_nulls_test.go, tests/features/performance/paging_test.go]
---

# 171. After pages a column that can hold NULL once the order places its NULLs

**In short:** `After` accepts an ordering by a column that can hold NULL when that order says `NullsFirst` or `NullsLast`, and seeks past the NULL rows with `IS NULL` and `IS NOT NULL`. Without either it still returns `ErrCursor`, and the message now names them.

## Decision

`After` refused every column that can hold NULL (#244), because `a > x` never matches a NULL, so the pages ended where the NULL rows began. That left the most common feed ordering, the newest published first with the drafts after them, without cursor paging (#307).

Once the order says where the NULLs are, the seek condition can say which rows follow the cursor's row. For a column `a` in ascending order:

| Cursor's value | NULLs first | NULLs last |
|---|---|---|
| `x` | `a > x` | `a > x OR a IS NULL` |
| NULL | `a IS NOT NULL` | none: only the next columns decide |

Descending order uses `<`. The earlier columns of the order are matched with `a = x`, or `a IS NULL` when the cursor's value is NULL, and the key, which the order always ends with, breaks the ties among NULLs. The cursor stores a NULL as JSON `null`, and cursors of orderings that were accepted before read as they did.

The order must name a place for the NULLs because the databases differ: PostgreSQL puts them after every value, MySQL, MariaDB, SQLite and SQL Server before, and the seek has to agree with the ORDER BY the database runs. With the place written in the query, the same query gives the same pages on every database. On SQL Server under `Distinct`, `Asc().NullsLast()` and `Desc().NullsFirst()` on such a column still return `ErrUnsupported`, as decision 165 says. A column that cannot hold NULL has no NULLs to place, so `After` leaves its `NullsFirst` or `NullsLast` out of the ORDER BY it sends.

A `Where(IsNotNull(...))` does not stand in for the place (#307 offered either): see the second rejected option below. The error says what to add instead.

Rejected:

- Taking each database's own place for NULLs: the dialects record no such fact (SQLite's default is not in its syntax table), and the same query would page differently per database.
- Treating a column as unable to hold NULL when the query has `IsNotNull` on it: a guess from the conditions, which `Or`, `Not` and raw SQL defeat.
- Ordering by `COALESCE(a, sentinel)`: it needs a value outside the column's range for each type, and the order no longer matches an index on the column.
