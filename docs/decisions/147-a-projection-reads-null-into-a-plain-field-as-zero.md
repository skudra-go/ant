---
id: 147
title: "A projection reads NULL into a plain field as its zero value"
area: queries
status: active
tags: [Project, NULL, zero-value, LeftJoin, Max, aggregate, Nullable, sql.Null]
issues: [164]
related: [35, 136]
changed_by: []
code: [projection.go, internal/scan/scan.go, expr.go]
tests: [tests/projection_null_test.go, tests/features/relations/project_test.go]
---

# 147. A projection reads NULL into a plain field as its zero value

**In short:** `ant.Project` reads a NULL into a field whose type gives a NOT NULL column (a number, text, a bool, a time, a uuid, a decimal) as the field's zero value, as `MaxOf` returns one; `ant.Nullable(expr)` gives an `Expr[sql.Null[V]]` that keeps the NULL.

## Decision

`Max` over a left join is NULL for a parent with no rows, and so is any aggregate over no rows, but it is typed `Expr[V]`, and `IntoExpr` wants a `V` field. The compiler made the field plain and the scan then failed: "converting NULL to int is unsupported". A column of a left join's side failed the same way for a row with no match, and there was no typed way to take the NULL (#164). `MaxOf`, `SumOf` and `AvgOf` already return the zero value over no rows.

Now a projection reads NULL into such a field as its zero value. Which fields: those whose Go type ant gives a NOT NULL column (`columnValue`). A pointer, an `sql.Null`, `ant.DeletedAt`, a slice or a map keeps the NULL, and a type only the driver converts (decision 136) is left to the driver. A type with its own `Scanner`, such as `decimal.Decimal`, gets its zero value for a NULL rather than its `Scan(nil)`, which for most such types is an error.

`ant.Nullable(e)` reads `e` as an `sql.Null[V]`, so `ant.IntoExpr(&R.Newest, ant.Nullable(ant.Max(&Posts.ID)))` tells "no post" from a stored zero. A pointer field with an `Expr` of the pointer type does the same.

Only projections change. A model read (`All`, `First`, `Find`, `Raw`) still refuses a NULL in a field that can't hold it: there it means the model and the schema disagree, which is a mistake to report, not a value.

Rejected: refusing with a clearer message only. The NULL comes from the query's shape, not from a mistake, and every LEFT JOIN report would need a wrapper the zero value already says.
