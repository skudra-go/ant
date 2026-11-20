---
id: 20
title: "Queries are immutable"
area: queries
status: active
tags: [immutable-query, OrderBy, ThenBy, ErrThenWithoutOrder]
issues: []
related: []
changed_by: []
code: [query.go, builder.go]
tests: [tests/features/querying/query_reuse_test.go, tests/features/querying/order_by_test.go]
---

# 20. Queries are immutable

**In short:** Query builder methods return new queries; OrderBy replaces earlier ordering, ThenBy adds, and ThenBy alone is ErrThenWithoutOrder.

## Decision

Every `Query[T]` builder method returns a new query, so a query can safely be the base of several others. GORM's documentation warns about reusing chained statements, and this removes that risk.

A later `OrderBy` replaces earlier ones, and `ThenBy` adds to them, as in EF Core. `ThenBy` with no `OrderBy` is `ErrThenWithoutOrder`, because Go has no equivalent of C#'s `IOrderedQueryable` to catch it at compile time.
