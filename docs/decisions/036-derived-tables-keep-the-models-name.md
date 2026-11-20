---
id: 36
title: "Derived tables keep the model's name"
area: queries
status: amended
tags: [From, FromCTE, FromRaw, Union, Recursive, derived-table, CTE]
issues: []
related: [88]
changed_by: [88]
code: [query.go, dialect/render.go]
tests: [tests/features/querying/cte_test.go, tests/features/querying/set_operations_test.go, tests/features/querying/derived_joins_test.go]
---

# 36. Derived tables keep the model's name

**In short:** From, FromCTE, FromRaw, Union and Recursive alias their source as the model's table and are untracked; Recursive uses UNION to end cycles.

> **Amended.** Decision 88: SQL Server cannot use UNION in a recursive CTE, so it renders UNION ALL with a path of keys (dialect/render.go:926).

## Decision

`From`, `FromCTE`, `FromRaw`, `Union`, and `Recursive` alias their source as the model's table, so probes work on the outer query unchanged. Their results are never tracked, because a derived row may not match a stored row. `Recursive` uses `UNION`, not `UNION ALL`, so a cycle in the data ends.
