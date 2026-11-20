---
id: 50
title: "Three layers for migrations"
area: migrations
status: active
tags: [schema, migrate, Config.Render, schema.Dialect, ErrUnsupported, layers, postgres]
issues: []
related: [49]
changed_by: []
code: [schema/dialect.go, schema/diff.go, migrate/engine.go, dialect/postgres/ddl.go]
tests: [schema/diff_test.go, migrate/migrate_test.go, tests/features/migrations/up_test.go]
---

# 50. Three layers for migrations

**In short:** schema is public data and diffing with no SQL or driver, dialect/postgres renders and inspects, migrate runs files and the CLI; schema.Dialect is a second interface returning ErrUnsupported otherwise.

## Decision

`schema` describes a database as plain data and works out changes, with no SQL execution and no driver. `dialect/postgres` turns changes into SQL and reads live databases. `migrate` runs files and is the CLI.

`schema` is public, not internal, so a project can replace SQL generation with `Config.Render` and read a plan's changes. Migration support is a second interface, `schema.Dialect`, so the dialects without it still compile and return `schema.ErrUnsupported`.
