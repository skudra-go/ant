---
id: 51
title: "Modeling added for migrations"
area: migrations
status: active
tags: [b.Column, b.Index, b.IndexExpr, b.Check, default, nullable, schema.Typed, sql.Null]
issues: []
related: [8, 52]
changed_by: []
code: [config.go, internal/meta/meta.go, schema.go, schema/schema.go]
tests: [tests/features/migrations/modeling_test.go, tests/features/migrations/models_test.go, tests/features/model_definition/nullable_test.go]
---

# 51. Modeling added for migrations

**In short:** Models carry migration facts via b.Column, b.Index, b.IndexExpr, b.Check and default=expr; nullability follows the Go type and unknown types error unless they implement schema.Typed.

## Decision

Migrations need facts the structs did not hold, so parts of v0.6 arrived early: `b.Column`, a real `b.Index`, `b.IndexExpr`, and `b.Check`. `ant:"default=expr"` and `.Default(expr)` say what the default is. `ant:"default"` with no expression still works at runtime, and is an error when generating, since a column with no default would fail on the first insert.

`.Default` means what `ant:"default"` means: a zero field is left for the database. EF Core's `HasDefaultValue` does the same.

Whether a column allows NULL follows the Go type, including `sql.Null[T]`. A nil slice is written as NULL, so slices are nullable, and `ant.JSON` is not. A type with no obvious column is an error that says what to do. `schema.Typed` lets a type declare its own.
