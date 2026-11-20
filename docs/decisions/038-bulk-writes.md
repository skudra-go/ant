---
id: 38
title: "Bulk writes"
area: writes
status: active
tags: [bulk-write, Update, Delete, subquery, FromCTE, InsertMany, SQL-Server, MySQL-1093]
issues: []
related: [23, 85]
changed_by: []
code: [query.go, upsert.go, dialect/render.go]
tests: [tests/features/writing/bulk_changes_test.go, tests/matrix/matrix_test.go, tests/features/writing/no_condition_guard_test.go, dialect/self_reads_test.go]
---

# 38. Bulk writes

**In short:** Limited or derived-source bulk writes select rows by key in a subquery, require the source to select the stored key, and InsertMany matches generated keys by sorting.

## Decision

SQL has no `LIMIT` on `UPDATE` or `DELETE`, so a limited bulk write selects its rows by key in a subquery. So does a bulk write on a derived source, such as `From`, `FromCTE`, a set operation, or `Recursive`, because `UPDATE` and `DELETE` name only the table: without the subquery, the outer conditions would run against every row of it. The source must select the model's stored key, so a write refuses one narrowed by `Column` or `Omit`, `Values` of a model whose key is `writeonly`, and `FromRaw`, whose columns ant cannot see. A set operation matches columns by position, so its sides must hold the key in the same place. Without that check, SQLite, MySQL, and MariaDB resolve the missing key to the table being written, and change every row. The key subquery drops the ordering unless it pages, because order cannot change which rows are members, and SQL Server refuses it there.

The guard of decision 23 counts a derived source as filtered when its rows are. `From`, `FromCTE`, and `Recursive` take their source's, a union needs both sides filtered, an intersection either, and `EXCEPT` its first, since a second side that matches nothing leaves every row. Replacing the source replaces what it counts for, while the outer query's own conditions still count. SQL Server allows `WITH` only at the start of a statement, so a write after `FromCTE` or `Recursive` returns `ErrUnsupported` there. MySQL refuses a write whose subquery reads the table being written (error 1093), but it materializes a derived table there rather than merging it, so a derived source needs nothing more. Other reads of the table are decision 85's.

`InsertMany` matches generated keys to rows by sorting them, since one statement draws sequence values in row order but `RETURNING` promises no order. It does not copy keys with a conflict clause, where rows may be skipped.
