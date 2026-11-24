---
id: 165
title: "SQL Server gets an ORDER BY only where it counts, and only from the select list under DISTINCT"
area: dialects
status: active
tags: [sqlserver, order, subquery, derived table, CTE, DISTINCT, NULLS FIRST, NULLS LAST, paging]
issues: [93, 95]
related: [164]
changed_by: []
code: [dialect/render.go, dialect/syntax.go, dialect/sqlserver/sqlserver.go]
tests: [tests/nested_order_test.go]
---

# 165. SQL Server gets an ORDER BY only where it counts, and only from the select list under DISTINCT

**In short:** On SQL Server, a nested query with no paging is written without its `ORDER BY`. Under `DISTINCT`, paging with no order is ordered by `1`, and `NULLS FIRST` or `NULLS LAST` that moves NULLs returns `ErrUnsupported`. On MySQL and MariaDB, as on SQL Server, the `CASE` that emulates `NULLS FIRST` and `NULLS LAST` is written only when the order moves NULLs.

## Decision

SQL Server refuses an `ORDER BY` in a subquery, derived table or CTE unless `TOP`, `OFFSET` or `FOR XML` is there too (error 1033), so a reusable query with an `OrderBy` failed once it was nested in `Exists`, `InSub`, `SubOf`, `From`, `FromCTE` or `JoinSub` (#93). `member()` and `bulkTarget()` already dropped such an order on their own. The renderer now does it for every nested `SELECT` on a dialect with `Syntax.NoNestedOrder`, which keeps the order of the statement itself, of an `INSERT`'s `SELECT`, and of any nested query with a `Limit` or an `Offset`, where it picks the rows. One rule in the renderer covers paths a list of call sites would miss, such as `MERGE`'s source and a recursive CTE's members.

Under `SELECT DISTINCT`, SQL Server takes only `ORDER BY` items from the select list (error 145) (#95). Paging with no order wrote `ORDER BY (SELECT NULL)` for `OFFSET ... FETCH`, and now writes `ORDER BY 1`, the first selected column. MySQL, MariaDB and SQL Server sort NULL below every value, so `Asc().NullsFirst()` and `Desc().NullsLast()` are their own order, and the `CASE` before the value is written only for the two orders that move NULLs. Under `DISTINCT` those two need a `CASE` outside the select list, which SQL Server refuses, so it lacks `dialect.DistinctNullsOrder` and the query returns `ErrUnsupported` before it is sent.

Rejected:

- Dropping the order from nested queries on every database: the others take it, and some callers may read a derived table's rows in that order today.
- Wrapping a `DISTINCT` query in a derived table to order it, as the issue suggested: its columns would need unique names, and every `ORDER BY` reference would need rewriting to the derived table's alias.
- `GROUP BY` in place of `DISTINCT`: it does not take `SELECT *`, aggregates or subqueries in the select list, so it would need its own list of exceptions.
- `OFFSET 0 ROWS` to keep a nested order: SQL Server would sort rows whose order nothing reads.
