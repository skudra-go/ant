---
id: 89
title: "One column, one value"
area: writes
status: active
tags: [assigned-twice, Upsert, Merge, UpdateMany, InsertFrom, DoUpdate, duplicate-column]
issues: []
related: [80]
changed_by: []
code: [assign.go, upsert.go, merge.go]
tests: [tests/features/writing/upsert_test.go, tests/features/writing/update_test.go, tests/features/writing/merge_test.go]
---

# 89. One column, one value

**In short:** ant refuses a write that assigns the same column twice, with the field's name, across upsert, bulk update, merge and InsertFrom, except UpdatedAt.

## Decision

PostgreSQL and SQL Server refuse a write that gives a column two values, and MySQL keeps the last, so the same code did different things by database. ant refuses it before it runs, with the field's name: an upsert's `DoUpdate` and `Set`, a bulk `Update`, `UpdateMany`, a merge's `WhenMatched` and `WhenNotMatched`, and `InsertFrom`'s mappings. A bulk `Update` that assigns the version is refused too, as an upsert's and a merge's are (decision 80). `UpdatedAt` stays the exception it was: one the write gives wins over ant's.
