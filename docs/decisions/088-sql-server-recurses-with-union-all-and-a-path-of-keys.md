---
id: 88
title: "SQL Server recurses with UNION ALL and a path of keys"
area: dialects
status: active
tags: [sqlserver, Recursive, CTE, union-all, maxrecursion, count, exists]
issues: []
related: [36, 38]
changed_by: []
code: [dialect/render.go, dialect/syntax.go, dialect/sqlserver/sqlserver.go, qb/qb.go]
tests: [dialect/syntax_test.go, tests/matrix/testdata/sqlserver.sql]
---

# 88. SQL Server recurses with UNION ALL and a path of keys

**In short:** On SQL Server, Recursive CTEs carry a hex path of keys to stop cycles and duplicates, and use MAXRECURSION 0; Count and Exists lift WITH to the statement start.

## Decision

SQL Server's recursive `WITH` has no `RECURSIVE`, and joins its parts only with `UNION ALL`, which keeps a row each time it is reached: a cycle never ends, and a row reached from two starting rows comes twice. `UNION` stops both elsewhere (decision 36). So `Recursive` gives its CTE the model's key, `qb.CTE.Key`, and on SQL Server, `Syntax.RecursiveUnionAll`, each row carries the path of keys that reached it, as text: each key column's bytes in hex, which neither separator can occur in. A recursive member skips a row whose key is on the path already, which ends a cycle, and the CTE is read through `ROW_NUMBER()` over the key, one row per key. The path also lets the recursion go deeper than SQL Server's default of 100 levels, with `OPTION (MAXRECURSION 0)`. A CTE without a key, as one built by hand, is written as it is, without `RECURSIVE`. A `MAXRECURSION` bound alone was rejected: it errs on deep trees and still repeats rows. `Count` and `Exists`, which put the query in a subquery, lift its `WITH` to the start of the statement, the only place SQL Server takes one, on every database, since the outer query reads nothing but the subquery. Writes after `FromCTE` and `Recursive` stay refused on SQL Server (decision 38).
