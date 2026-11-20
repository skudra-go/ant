---
id: 49
title: "PostgreSQL first"
area: dialects
status: active
tags: [postgres, Dialect, renderer, migrations, feature-suite]
issues: [74]
related: [50, 74]
changed_by: []
code: [dialect/render.go, dialect/dialect.go, dialect/postgres/postgres.go]
tests: [dialect/render_test.go, tests/matrix/matrix_test.go]
---

# 49. PostgreSQL first

**In short:** PostgreSQL has the full feature set (including migrations) and all SQL generation goes through the shared renderer and Dialect interface, which the other dialects build on.

## Decision

The owner chose to focus on PostgreSQL, so it has everything, and the feature suite runs on it alone. All SQL generation goes through the shared renderer and the `Dialect` interface, which is what let the other databases follow in [decision 74](#74-dialects-state-their-differences). Migrations for them are still open.
