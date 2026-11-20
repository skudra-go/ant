---
id: 43
title: "Database-specific helpers"
area: dialects
status: active
tags: [postgres, SQL, SQLCond, dialect-helpers, neutral-core]
issues: []
related: [49, 74]
changed_by: []
code: [dialect/postgres/functions.go, dialect/postgres/postgres.go, expr.go]
tests: [tests/features/value_handling/postgres_test.go, dialect/postgres/array_test.go]
---

# 43. Database-specific helpers

**In short:** PostgreSQL-only conditions and types live in dialect/postgres, built from the public ant.SQL and ant.SQLCond, so the root package stays dialect-neutral.

## Decision

The root package stays neutral. PostgreSQL-only conditions and types live in `dialect/postgres`, built from the public `ant.SQL` and `ant.SQLCond`, so a dialect can add helpers without changing the core.
