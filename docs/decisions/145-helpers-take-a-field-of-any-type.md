---
id: 145
title: "Helpers take a field of any type, and the database checks it"
area: queries
status: active
tags: [Ordered, Number, OrderBy, Gt, Between, Like, Inc, SumOf, MaxOf, Past, nullable, decimal, uuid]
issues: [171, 174]
related: [1, 35]
changed_by: []
code: [cond.go, expr.go, aggregate.go, assign.go, query.go, table.go, join.go, config.go, dialect/postgres/functions.go]
tests: [tests/any_column_test.go, tests/features/querying/any_column_test.go]
---

# 145. Helpers take a field of any type, and the database checks it

**In short:** ordering, comparison, text, date, arithmetic and aggregate helpers take a field of any Go type, so a `*time.Time`, `*string`, `sql.Null[T]`, `uuid.UUID`, `decimal.Decimal` or `ant.DeletedAt` column sorts, compares and adds up like a plain one; `Inc` and `Dec` count a NULL from zero.

## Decision

`OrderBy`, `Gt`, `Between`, `MaxOf` and the rest took `ant.Ordered` (decision 1), `Sum`, `Avg` and `Inc` took `ant.Number`, `Like`, `Lower` and `postgres.Match` took `~string`, and `Past`, `DatePart` and `Latest` took `*time.Time`. Almost every table has a nullable time or text column, a uuid or money in a decimal, so the first "sort by due date", "overdue", "search by nickname" or "revenue" query didn't compile, and the error listed the whole constraint (#171, #174). `ant.Col(...).Asc()` worked, but nothing pointed to it, and `Like` on a `*string`, `Inc` on a `*int`, `Past` on a `*time.Time` and a decimal sum had no typed form at all.

A union constraint can't name these types: Go has no constraint for "a pointer to a `~string`", none for `sql.Null` of every type, and none for a library's uuid or decimal. So each helper takes `V any`, as `Eq`, `IsNull` and `Col` already did, and the database refuses what it can't order or add up, at the terminal call, with its own message. PostgreSQL has no `min(uuid)`, for example. The compiler still checks that a value has the field's type, as it did.

- `Gt`, `Gte`, `Lt`, `Lte` and `Between` compare with a value of the field's type. A nil pointer or an invalid `sql.Null` is bound as NULL and matches nothing, as in SQL. Only `Eq`, `Ne` and `In` turn a NULL value into `IS NULL`.
- `Like` and `ILike` take the pattern as a second type parameter, `P ~string`, so a `*string` field takes a plain string pattern.
- `Inc` and `Dec` take the amount as a second type parameter, `N any`, since a `*int` field adds an `int`. On a field that can hold NULL (`canBeNull`: a pointer, an `sql.Null`, or one `.Nullable()` allows) they write `COALESCE(column, 0) + n`, so a NULL counts from zero. A NOT NULL field's SQL is as it was.
- `SumOf`, `MinOf` and `MaxOf` return the zero value of `V` over no rows, or rows that are all NULL, as they did: nil for a pointer field. `AvgOf` and `Avg` still return a `float64`.
- `ant.Ordered` and `ant.Number` stay, deprecated, for code that names them in its own constraints.

Rejected: an `Expr` form of each helper beside the field one. It doubled the API to work around a constraint that protects nothing the database doesn't check anyway.
