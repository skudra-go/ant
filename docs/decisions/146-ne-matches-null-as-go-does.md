---
id: 146
title: "Ne, NotIn and Not of Eq match NULL, as Go's != does"
area: queries
status: active
tags: [Ne, NotIn, Not, Eq, In, null, nullable, De-Morgan, Expr, sensitive]
issues: [293]
related: [7, 68, 95]
changed_by: []
code: [cond.go, expr.go]
tests: [tests/null_equality_test.go, tests/matrix/dims_test.go, tests/features/developer_tooling/sensitive_test.go]
---

# 146. Ne, NotIn and Not of Eq match NULL, as Go's != does

**In short:** on a column that can hold NULL, `Ne`, `NotIn` and `Not(Eq(...))` with a value match the NULL rows, as `column <> v OR column IS NULL`; ordering comparisons, and `Not` of them, still follow SQL.

## Decision

`Eq(field, nil)` is `IS NULL` and a nil inside `In` matches NULL (decisions 7 and 95), so conditions read as Go. But `Ne`, `NotIn` and `Not(Eq(...))` with a value were SQL's `<>`, which never matches NULL: "every user whose plan isn't free" left out every user with no plan, and nothing said so (#293). EF Core writes the same `OR column IS NULL` for C#'s `!=`.

Now, on a column that can hold NULL (`canBeNull`: a pointer, an `sql.Null`, `ant.DeletedAt`, or a field `.Nullable()` allows):

- `Ne(field, v)` is `(column <> v OR column IS NULL)`, and `NotIn(field, a, b)` is `(column NOT IN (a, b) OR column IS NULL)`. With a nil among the values, `NotIn` leaves NULL out, as before, since `Ne(field, nil)` is `IS NOT NULL`.
- `Not` of `Eq`, `Ne`, `In` and `NotIn` is the opposite helper, so `Not(Eq(f, v))` is `Ne(f, v)` and `Not(Ne(f, v))` is `Eq(f, v)`. `Not` of `And` and `Or` applies De Morgan when a child has an opposite, so `NoneOf` and `Not(Or(Eq(a), Eq(b)))` match NULL too. Each condition carries a builder for its opposite (`Cond.opposite`); it is set only on a nullable column, so the common `Eq` on a NOT NULL column allocates nothing more and keeps its SQL.
- The typed `Expr` methods follow the same rule: `Col(&f).Ne(v)` on a nullable column, and `Lower(&f).Ne(v)` or any function over one, match NULL. An expression from `SQL` or a subquery says nothing about NULL, so it keeps SQL's `<>`.
- `Gt`, `Lt`, `Between`, `Like` and the other ordering comparisons, and `Not` of them, stay SQL's: a NULL compares as unknown and matches neither way. Go has no order for nil, so there is no Go reading to follow. `Not(Gt(f, v))` leaves out NULL, as `Gt` does.

A column that can't hold NULL keeps its plain `=`, `<>` and `NOT IN`, so its SQL and plans are as they were.

The sensitive rule (decision 68) marks the values inside the new `OR` as secret, and `hideParams` now looks inside `And`, `Or` and `Not`, which also hides the values of `In(field, nil, v)` on a sensitive field. Those reached the log before.

Rejected: SQL's `IS NOT TRUE` for every `Not`, which makes `Not` the complement of anything. SQL Server has no `IS NOT TRUE`, and the `CASE` it needs instead keeps an index from serving the condition.
