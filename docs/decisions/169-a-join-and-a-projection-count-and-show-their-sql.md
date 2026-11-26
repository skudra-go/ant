---
id: 169
title: "A join and a projection count, test for rows, show their SQL and log it, as a query does"
area: queries
status: active
tags: [joins, projections, Count, Exists, ToSQL, Debug, Distinct, Offset, GroupBy]
issues: [169]
related: [5, 34, 165, 167]
changed_by: []
code: [join.go, projection.go, aggregate.go, builder.go, query.go]
tests: [tests/join_terminals_test.go, tests/features/relations/join_test.go]
---

# 169. A join and a projection count, test for rows, show their SQL and log it, as a query does

**In short:** `Join2`, `Join3`, `Join4` and `Projection` have `Count`, `Exists`, `ToSQL`, `ToSQLContext` and `Debug`. `Debug` is part of the query's state, so a join or a projection made from a query keeps it, and it logs every SELECT the query runs, not only its rows.

## Decision

A paged list of joined rows needs its total, and the SQL of a join could not be seen, so callers rebuilt the conditions on the root query to count (#169). `Debug` was a field of `Query` alone, so a join made from a debug query dropped it, and even on a query only the statement that reads rows logged: `Count`, `Exists`, `Pluck`, the aggregates and `Each` did not.

- `Debug` moves to the state every query, join, group and projection shares, and every SELECT they run goes through one place that logs it.
- A join's `ToSQL` is the SELECT that its `Select` runs, every column of every model. A projection's is what `All` runs.
- `Count` and `Exists` work as `Query`'s do. Limit, Offset, Distinct and GroupBy decide which rows there are, so with any of them `Count` reads `COUNT(*)` over a subquery of the select list, whose columns are named `ant_1`, `ant_2` and on: the sides of a join share names such as `id`, which a derived table refuses on MySQL, MariaDB and SQL Server. A projection's columns are expressions that can decide the rows too: an aggregate with no GroupBy makes one row of many, or of none, and a set-returning function many of one. So a projection always counts over that subquery.
- `Exists` selects 1 in place of the columns, except for a projection, as above, and where Distinct or GroupBy decide the rows an Offset skips. There it reads from a derived table of the columns. `SELECT DISTINCT 1` leaves one row, which an offset of one skips, and MySQL, MariaDB and SQLite drop a DISTINCT or a GROUP BY from an EXISTS subquery even with an OFFSET, so they counted every row. This also fixes `Query.Exists` with `Distinct` and `Offset`, which had the first of these faults.

Rejected:

- `Count` on a join counting rows of the root model only: that is `Query.Count` with `Has`, and a join's rows are the joined ones, which its pages show.
- Keeping `Debug` to the rows of a query: a debug flag that a `Count` beside it ignores sends the reader looking in the wrong place.
