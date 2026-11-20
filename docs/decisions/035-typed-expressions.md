---
id: 35
title: "Typed expressions"
area: queries
status: active
tags: [Expr, IntoExpr, Project, aggregate, typed-expression]
issues: []
related: []
changed_by: []
code: [expr.go, projection.go]
tests: [tests/features/querying/expressions_test.go, tests/features/relations/project_test.go]
---

# 35. Typed expressions

**In short:** Expr[V] is the single typed abstraction for columns, aggregates, subqueries and raw SQL; IntoExpr and Project read computed results.

## Decision

`Expr[V]` is the one abstraction for anything with a value: a column, an aggregate, a subquery, or raw SQL. Its methods build conditions and orderings, and `IntoExpr` maps it to a result field, so `V` is checked by the compiler throughout. `Project` is how grouped and computed results are read.
